package tasks

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/dokku/docket/subprocess"
)

// ConfigTask manages the configuration for a given dokku application
type ConfigTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app"`

	// Restart is a flag indicating if the app should be restarted
	Restart bool `yaml:"restart" default:"true"`

	// Config is a map of configuration key-value pairs
	Config map[string]string `yaml:"config"`

	// State is the desired state of the configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// ConfigTaskExample contains an example of a ConfigTask
type ConfigTaskExample struct {
	// Name is the task name holding the ConfigTask description
	Name string `yaml:"-"`

	// ConfigTask is the ConfigTask configuration
	ConfigTask ConfigTask `yaml:"dokku_config"`
}

// GetName returns the name of the example
func (e ConfigTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the config task
func (t ConfigTask) Doc() string {
	return "Manages the configuration for a given dokku application"
}

// Examples returns the examples for the config task
func (t ConfigTask) Examples() ([]Doc, error) {
	return MarshalExamples([]ConfigTaskExample{
		{
			Name: "set KEY=VALUE",
			ConfigTask: ConfigTask{
				App:     "hello-world",
				Restart: true,
				Config: map[string]string{
					"KEY": "VALUE_1",
				},
			},
		},
		{
			Name: "set KEY=VALUE without restart",
			ConfigTask: ConfigTask{
				App:     "hello-world",
				Restart: false,
				Config: map[string]string{
					"KEY": "VALUE_1",
				},
			},
		},
	})
}

// Execute sets or unsets the configuration for a given dokku application
func (t ConfigTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// SensitiveValues returns every non-empty value in the Config map together
// with its base64 encoding. The base64 form is included because the apply
// path passes config values to `dokku config:set --encoded` as
// `KEY=<base64(value)>`, so the literal value never appears on argv but the
// encoded form does. Masking both means the verbose command echo and the
// DOKKU_TRACE log scrub the secret in either representation.
func (t ConfigTask) SensitiveValues() []string {
	out := make([]string, 0, 2*len(t.Config))
	for _, v := range t.Config {
		if v == "" {
			continue
		}
		out = append(out, v, base64.StdEncoding.EncodeToString([]byte(v)))
	}
	return out
}

// Plan reports the drift the ConfigTask would produce.
func (t ConfigTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planConfigSet(t) },
		StateAbsent:  func() PlanResult { return planConfigUnset(t) },
	})
}

// configKeysToSet returns keys whose desired value differs from the current value.
func configKeysToSet(current, desired map[string]string) []string {
	keys := []string{}
	for k, v := range desired {
		if cur, ok := current[k]; !ok || cur != v {
			keys = append(keys, k)
		}
	}
	return keys
}

// configKeysToUnset returns keys present in desired that exist in current.
func configKeysToUnset(current, desired map[string]string) []string {
	keys := []string{}
	for k := range desired {
		if _, ok := current[k]; ok {
			keys = append(keys, k)
		}
	}
	return keys
}

// planConfigSet probes current config once, computes the diff, and embeds
// an apply closure that runs `dokku config:set` with only the changed keys.
func planConfigSet(t ConfigTask) PlanResult {
	currentConfig, err := getConfig(t)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	keys := configKeysToSet(currentConfig, t.Config)
	if len(keys) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	mutations := make([]string, 0, len(keys))
	status := PlanStatusModify
	allNew := true
	for _, k := range keys {
		if _, ok := currentConfig[k]; ok {
			mutations = append(mutations, fmt.Sprintf("set %s (was set)", k))
			allNew = false
		} else {
			mutations = append(mutations, fmt.Sprintf("set %s (new)", k))
		}
	}
	if allNew {
		status = PlanStatusCreate
	}
	args := []string{"--quiet", "config:set", "--encoded"}
	if !t.Restart {
		args = append(args, "--no-restart")
	}
	args = append(args, t.App)
	for _, k := range keys {
		args = append(args, fmt.Sprintf("%s=%s", k, base64.StdEncoding.EncodeToString([]byte(t.Config[k]))))
	}
	inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d key(s) to set", len(keys)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}
}

// planConfigUnset probes current config once, computes the diff, and embeds
// an apply closure that runs `dokku config:unset` with only existing keys.
func planConfigUnset(t ConfigTask) PlanResult {
	currentConfig, err := getConfig(t)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	keys := configKeysToUnset(currentConfig, t.Config)
	if len(keys) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	mutations := make([]string, 0, len(keys))
	for _, k := range keys {
		mutations = append(mutations, fmt.Sprintf("unset %s", k))
	}
	args := []string{"--quiet", "config:unset"}
	if !t.Restart {
		args = append(args, "--no-restart")
	}
	args = append(args, t.App)
	args = append(args, keys...)
	inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d key(s) to unset", len(keys)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
		},
	}
}

// getConfig retrieves the current configuration for a given dokku application
func getConfig(t ConfigTask) (map[string]string, error) {
	var config map[string]string
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			"config:export",
			"--format",
			"json",
			t.App,
		},
		WorkingDirectory: "/tmp",
	})
	if err != nil {
		return config, err
	}

	err = json.Unmarshal(result.StdoutBytes(), &config)
	if err != nil {
		return config, err
	}
	return config, nil
}

// init registers the ConfigTask with the task registry
func init() {
	RegisterTask(&ConfigTask{})
}
