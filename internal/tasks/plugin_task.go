package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/internal/subprocess"
)

// PluginTask installs or uninstalls a third-party dokku plugin.
//
// plugin:install and plugin:uninstall are root-level operations: over the SSH
// transport dokku wraps privilege server-side, and run locally docket must
// already be root. Idempotency is by plugin name only - a changed url or
// committish on an already-installed plugin is not detected.
type PluginTask struct {
	// Name is the plugin name as it appears in plugin:list
	Name string `required:"true" identity:"key" yaml:"name" description:"Plugin name as it appears in plugin:list"`

	// URL is the git URL to install from. Required when state is present.
	URL string `required:"false" required_when:"present" yaml:"url,omitempty" description:"Git URL to install the plugin from"`

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

// ProbeSupport reports whether Plan() can read this task's current state.
func (t PluginTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku this task's probe parses.
// The floor is 0.38.31, the release the integration suite runs; the probe is `plugin:list --format json` (dokku 0.38.22+).
// committish remains how a recipe pins the plugin this task installs.
func (t PluginTask) MinimumVersions() VersionSupport {
	return VersionSupport{Dokku: "0.38.31"}
}

// examples returns the examples for the plugin task
func (t PluginTask) examples() ([]Doc, error) {
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
func (t PluginTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the PluginTask's inputs without contacting the server.
func (t PluginTask) Validate() error {
	if err := checkConstraints(t); err != nil {
		return err
	}
	if t.Name == "" {
		return fmt.Errorf("'name' is required")
	}
	return nil
}

// Plan reports the drift the PluginTask would produce.
func (t PluginTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	// seen attaches the plugin as plugin:list reports it. Drift is decided by
	// name alone; the source and committish are what export would emit.
	seen := func(r PlanResult, entry pluginListEntry, installed bool) PlanResult {
		if !installed {
			return observed(r, PluginTask{Name: t.Name, State: StateAbsent}, false)
		}
		return observed(r, entry.task(), true)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			entry, installed, err := pluginListed(ctx, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if installed {
				return seen(PlanResult{InSync: true, Status: PlanStatusOK}, entry, installed)
			}
			args := []string{"--quiet", "plugin:install", t.URL}
			if t.Committish != "" {
				args = append(args, "--committish", t.Committish)
			}
			args = append(args, t.Name)
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return seen(PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "plugin not installed",
				Mutations: []string{fmt.Sprintf("install plugin %s", t.Name)},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runPluginChange(ctx, StateAbsent, StatePresent, inputs)
				},
			}, entry, installed)
		},
		StateAbsent: func() PlanResult {
			entry, installed, err := pluginListed(ctx, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !installed {
				return seen(PlanResult{InSync: true, Status: PlanStatusOK}, entry, installed)
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "plugin:uninstall", t.Name},
			}}
			return seen(PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "plugin installed",
				Mutations: []string{fmt.Sprintf("uninstall plugin %s", t.Name)},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runPluginChange(ctx, StatePresent, StateAbsent, inputs)
				},
			}, entry, installed)
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

// task returns the dokku_plugin task that reinstalls the plugin as listed.
// The followed branch is preferred so the task mirrors the install intent; a
// detached checkout (a pinned commit or tag) reports no branch, so the exact
// commit stands in for it.
func (e pluginListEntry) task() PluginTask {
	committish := e.Branch
	if committish == "" {
		committish = e.Committish
	}
	return PluginTask{
		Name:       e.Name,
		URL:        e.SourceURL,
		Committish: committish,
		State:      StatePresent,
	}
}

// readPluginList runs `plugin:list --format json`, which exposes each plugin's
// install source URL, followed branch, and checked-out commit.
func readPluginList(ctx context.Context) ([]pluginListEntry, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "plugin:list", "--format", "json"},
	})
	if err != nil {
		return nil, err
	}
	var entries []pluginListEntry
	if err := json.Unmarshal(result.StdoutBytes(), &entries); err != nil {
		return nil, fmt.Errorf("parse plugin:list json: %w", err)
	}
	return entries, nil
}

// pluginListed returns the plugin:list entry for name and whether it is
// installed. Plan reads it rather than pluginInstalled so the plan's observed
// state carries the same source and committish ExportGlobal emits.
func pluginListed(ctx context.Context, name string) (pluginListEntry, bool, error) {
	entries, err := readPluginList(ctx)
	if err != nil {
		return pluginListEntry{}, false, err
	}
	for _, e := range entries {
		if e.Name == name {
			return e, true, nil
		}
	}
	return pluginListEntry{}, false, nil
}

// ExportGlobal reconstructs the server's third-party plugins from
// `plugin:list --format json`, which exposes each plugin's install source URL,
// followed branch, and checked-out commit. Core plugins ship with dokku (and
// plugin:uninstall rejects them), and plugins without a git source (a tarball or
// local path) cannot be reinstalled declaratively, so both are skipped.
func (t PluginTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	entries, err := readPluginList(ctx)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	var out []interface{}
	for _, e := range entries {
		if e.Core || e.SourceURL == "" {
			continue
		}
		out = append(out, e.task())
	}
	return out, nil
}

// pluginInstalled reports whether a plugin is installed by parsing
// `dokku plugin:list`, whose first whitespace-delimited field per line is the
// plugin name (mirrors dokku's own plugin:installed check).
func pluginInstalled(ctx context.Context, name string) (bool, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
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

// runPluginChange installs or uninstalls a plugin, then drops the cached
// plugin list for this target. A later task on the same host has to see the
// plugin this one just changed; a list read before the change would still
// say it was missing or old. A failed change leaves the cache alone.
func runPluginChange(ctx context.Context, prior, desired State, inputs []subprocess.ExecCommandInput) TaskOutputState {
	state := runExecInputs(ctx, TaskOutputState{State: prior}, desired, inputs)
	if state.Error == nil {
		InvalidatePluginVersions(ctx)
	}
	return state
}

// init registers the PluginTask with the task registry
func init() {
	RegisterTask(&PluginTask{})
}
