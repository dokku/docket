package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// PluginTask installs or uninstalls a third-party dokku plugin.
//
// plugin:install and plugin:uninstall are root-level operations: over the SSH
// transport dokku wraps privilege server-side, and run locally docket must
// already be root. Idempotency is by plugin name only - a changed url or
// committish on an already-installed plugin is not detected.
type PluginTask struct {
	// Name is the plugin name as it appears in plugin:list
	Name string `required:"true" yaml:"name" description:"Plugin name as it appears in plugin:list"`

	// URL is the git URL to install from. Required when state is present.
	URL string `required:"false" yaml:"url,omitempty" description:"Git URL to install the plugin from. Required when state is present."`

	// Committish is an optional git ref (branch, tag, or commit) to install
	Committish string `required:"false" yaml:"committish,omitempty" description:"Optional git ref (branch, tag, or commit) to install"`

	// State is the desired state of the plugin
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the plugin"`
}

// PluginTaskExample contains an example of a PluginTask
type PluginTaskExample struct {
	// Name is the task name holding the PluginTask description
	Name string `yaml:"-"`

	// PluginTask is the PluginTask configuration
	PluginTask PluginTask `yaml:"dokku_plugin"`
}

// GetName returns the name of the example
func (e PluginTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the plugin task
func (t PluginTask) Doc() string {
	return "Installs or uninstalls a third-party dokku plugin. Installation is a " +
		"root-level operation, so this task must run over the SSH transport (where " +
		"dokku wraps privilege server-side) or as root locally. Idempotency is by " +
		"plugin name only - a changed `url` or `committish` on an already-installed " +
		"plugin is not detected."
}

// ExportSupport reports how docket export handles this task.
func (t PluginTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the plugin task
func (t PluginTask) Examples() ([]Doc, error) {
	return MarshalExamples([]PluginTaskExample{
		{
			Name: "Install a plugin from a git URL",
			PluginTask: PluginTask{
				Name: "redis",
				URL:  "https://github.com/dokku/dokku-redis.git",
			},
		},
		{
			Name: "Install a plugin pinned to a committish",
			PluginTask: PluginTask{
				Name:       "letsencrypt",
				URL:        "https://github.com/dokku/dokku-letsencrypt.git",
				Committish: "0.25.0",
			},
		},
		{
			Name: "Uninstall a plugin",
			PluginTask: PluginTask{
				Name:  "redis",
				State: StateAbsent,
			},
		},
	})
}

// Execute installs or uninstalls the plugin
func (t PluginTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Validate checks the PluginTask's inputs without contacting the server.
func (t PluginTask) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("'name' is required")
	}
	if t.State == StatePresent && t.URL == "" {
		return fmt.Errorf("'url' is required when state is 'present'")
	}
	return nil
}

// Plan reports the drift the PluginTask would produce.
func (t PluginTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			installed, err := pluginInstalled(t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if installed {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			args := []string{"--quiet", "plugin:install", t.URL}
			if t.Committish != "" {
				args = append(args, "--committish", t.Committish)
			}
			args = append(args, t.Name)
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "plugin not installed",
				Mutations: []string{fmt.Sprintf("install plugin %s", t.Name)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			installed, err := pluginInstalled(t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !installed {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "plugin:uninstall", t.Name},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "plugin installed",
				Mutations: []string{fmt.Sprintf("uninstall plugin %s", t.Name)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// pluginListEntry is one record of `dokku plugin:list --format json`. Only the
// fields the exporter needs are decoded; version, enabled, and description are
// ignored.
type pluginListEntry struct {
	Name       string `json:"name"`
	Core       bool   `json:"core"`
	SourceURL  string `json:"source_url"`
	Committish string `json:"committish"`
	Branch     string `json:"branch"`
}

// ExportGlobal reconstructs the server's third-party plugins from
// `plugin:list --format json`, which exposes each plugin's install source URL,
// followed branch, and checked-out commit. Core plugins ship with dokku (and
// plugin:uninstall rejects them), and plugins without a git source (a tarball or
// local path) cannot be reinstalled declaratively, so both are skipped.
func (t PluginTask) ExportGlobal() ([]interface{}, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "plugin:list", "--format", "json"},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}

	var entries []pluginListEntry
	if err := json.Unmarshal(result.StdoutBytes(), &entries); err != nil {
		return nil, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	var out []interface{}
	for _, e := range entries {
		if e.Core || e.SourceURL == "" {
			continue
		}
		// Prefer the followed branch so the export mirrors the install intent; a
		// detached checkout (a pinned commit or tag) reports no branch, so fall
		// back to the exact commit.
		committish := e.Branch
		if committish == "" {
			committish = e.Committish
		}
		out = append(out, PluginTask{
			Name:       e.Name,
			URL:        e.SourceURL,
			Committish: committish,
			State:      StatePresent,
		})
	}
	return out, nil
}

// pluginInstalled reports whether a plugin is installed by parsing
// `dokku plugin:list`, whose first whitespace-delimited field per line is the
// plugin name (mirrors dokku's own plugin:installed check).
func pluginInstalled(name string) (bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "plugin:list"},
	})
	if err != nil {
		return false, err
	}

	for _, line := range strings.Split(result.StdoutContents(), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == name {
			return true, nil
		}
	}
	return false, nil
}

// init registers the PluginTask with the task registry
func init() {
	RegisterTask(&PluginTask{})
}
