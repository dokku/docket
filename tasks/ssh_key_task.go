package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
	"golang.org/x/crypto/ssh"
)

// SshKeyTask manages an SSH public key for git push access via dokku's
// core ssh-keys plugin.
type SshKeyTask struct {
	// Name identifies the key in dokku
	Name string `required:"true" yaml:"name" description:"Name that identifies the key in dokku"`

	// Key is the public key contents. Required when state is present.
	Key string `required:"false" yaml:"key,omitempty" description:"Public key contents. Required when state is present."`

	// State is the desired state of the SSH key
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the SSH key"`
}

// SshKeyTaskExample contains an example of a SshKeyTask
type SshKeyTaskExample struct {
	// Name is the task name holding the SshKeyTask description
	Name string `yaml:"-"`

	// SshKeyTask is the SshKeyTask configuration
	SshKeyTask SshKeyTask `yaml:"dokku_ssh_key"`
}

// GetName returns the name of the example
func (e SshKeyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the ssh key task
func (t SshKeyTask) Doc() string {
	return "Manages an SSH public key for git push access via dokku's ssh-keys plugin"
}

// ExportSupport reports how docket export handles this task.
func (t SshKeyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the ssh key task
func (t SshKeyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]SshKeyTaskExample{
		{
			Name: "Add a deploy key",
			SshKeyTask: SshKeyTask{
				Name: "deploy-bot",
				Key:  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINioKrRalhe/VF8s43pjp8jpl6LGwv6tF0F5FvKPjUer deploy-bot",
			},
		},
		{
			Name: "Remove a key by name",
			SshKeyTask: SshKeyTask{
				Name:  "deploy-bot",
				State: StateAbsent,
			},
		},
	})
}

// Execute adds or removes the SSH key
func (t SshKeyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the SshKeyTask would produce.
func (t SshKeyTask) Plan() PlanResult {
	if t.Name == "" {
		return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'name' is required")}
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			if t.Key == "" {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'key' is required when state is 'present'")}
			}
			pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(t.Key))
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("invalid public key: %v", err)}
			}
			keys, err := sshKeysList()
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			addInput := subprocess.ExecCommandInput{
				Command: "dokku",
				Args:    []string{"--quiet", "ssh-keys:add", t.Name},
				Stdin:   strings.NewReader(t.Key + "\n"),
			}
			if existing, found := keys[t.Name]; found {
				if sshKeyMatches(existing, pub) {
					return PlanResult{InSync: true, Status: PlanStatusOK}
				}
				// The name is taken by a different key; ssh-keys:add refuses to
				// clobber, so remove the stale entry first then re-add.
				inputs := []subprocess.ExecCommandInput{
					{Command: "dokku", Args: []string{"--quiet", "ssh-keys:remove", t.Name}},
					addInput,
				}
				return PlanResult{
					InSync:    false,
					Status:    PlanStatusModify,
					Reason:    "key contents changed",
					Mutations: []string{fmt.Sprintf("rotate ssh key %s", t.Name)},
					Commands:  resolveCommands(inputs),
					apply: func() TaskOutputState {
						return runExecInputs(TaskOutputState{State: StatePresent}, StatePresent, inputs)
					},
				}
			}
			inputs := []subprocess.ExecCommandInput{addInput}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "key missing",
				Mutations: []string{fmt.Sprintf("add ssh key %s", t.Name)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			keys, err := sshKeysList()
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if _, found := keys[t.Name]; !found {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{
				{Command: "dokku", Args: []string{"--quiet", "ssh-keys:remove", t.Name}},
			}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "key present",
				Mutations: []string{fmt.Sprintf("remove ssh key %s", t.Name)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// sshKeyEntry is the subset of `dokku ssh-keys:list --format json` this task
// reads. dokku emits one object per key with name, fingerprint, and the raw
// public key (see dokku/sshcommand's list output).
type sshKeyEntry struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"public-key"`
}

// sshKeysList returns the installed ssh keys keyed by name. A dokku-level
// non-zero exit (e.g. no keys configured) is treated as an empty set; only a
// transport-level failure (*subprocess.SSHError) is propagated.
func sshKeysList() (map[string]sshKeyEntry, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "ssh-keys:list", "--format", "json"},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return map[string]sshKeyEntry{}, nil
	}

	var entries []sshKeyEntry
	if err := json.Unmarshal(result.StdoutBytes(), &entries); err != nil {
		return map[string]sshKeyEntry{}, nil
	}

	out := make(map[string]sshKeyEntry, len(entries))
	for _, e := range entries {
		out[e.Name] = e
	}
	return out, nil
}

// sshKeyMatches reports whether a dokku-reported entry refers to the same
// public key as pub. The raw key is compared first (comment-insensitive via
// Marshal); when absent it falls back to dokku's SHA256 fingerprint.
func sshKeyMatches(entry sshKeyEntry, pub ssh.PublicKey) bool {
	if raw := strings.TrimSpace(entry.PublicKey); raw != "" {
		if reported, _, _, _, err := ssh.ParseAuthorizedKey([]byte(raw)); err == nil {
			return string(reported.Marshal()) == string(pub.Marshal())
		}
	}
	return strings.TrimSpace(entry.Fingerprint) == ssh.FingerprintSHA256(pub)
}

// init registers the SshKeyTask with the task registry
func init() {
	RegisterTask(&SshKeyTask{})
}
