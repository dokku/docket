package tasks

import (
	"fmt"
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

// Plan reports the drift the PluginTask would produce.
func (t PluginTask) Plan() PlanResult {
	if t.Name == "" {
		return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'name' is required")}
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			if t.URL == "" {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'url' is required when state is 'present'")}
			}
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
