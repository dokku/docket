package tasks

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/dokku/docket/internal/subprocess"
)

// ConfigTask manages the configuration for a given dokku application
type ConfigTask struct {
	// App is the name of the app
	App string `required:"true" identity:"key" yaml:"app" description:"Name of the app"`

	// Restart is a flag indicating if the app should be restarted. It is a
	// *bool so an explicit `restart: false` is distinguishable from an omitted
	// key: nil defaults to true via boolValue(t.Restart, true). The default:"true"
	// tag only drives the generated docs table; go-defaults leaves pointer fields
	// untouched, which is what fixes the silent override of an explicit false.
	Restart *bool `yaml:"restart,omitempty" default:"true" description:"Flag indicating if the app should be restarted"`

	// Config is a map of configuration key-value pairs
	Config map[string]string `identity:"collection" yaml:"config,omitempty" description:"Map of configuration key-value pairs; omit for state 'clear'"`

	// Preserve lists keys that state 'set' and 'clear' leave in place
	Preserve []string `required:"false" yaml:"preserve,omitempty" description:"Keys that state 'set' and 'clear' leave untouched, on top of the service-link keys, NO_VHOST, and the git rev-env-var key they always keep; only valid for state 'set' or 'clear'"`

	// State is the desired state of the configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent,set,clear" description:"Desired state of the configuration: 'present' sets the named keys and 'absent' unsets them, leaving every other key alone; 'set' declares the app's entire config, unsetting every key it does not name; 'clear' unsets every key. Both 'set' and 'clear' keep keys a service link wrote, NO_VHOST, the git rev-env-var key, and any key listed in 'preserve'"`
}

// configNoVhostKey is the config key `domains:disable` and `domains:enable`
// write, which dokku_domains_toggle manages rather than dokku_config.
const configNoVhostKey = "NO_VHOST"

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

// ExportSupport reports how docket export handles this task.
func (t ConfigTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportPartial, Caveat: "config values are written to the companion vars-file"}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t ConfigTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the config task
func (t ConfigTask) Examples() ([]Doc, error) {
	return MarshalExamples([]ConfigTaskExample{
		{
			Name: "set KEY=VALUE",
			ConfigTask: ConfigTask{
				App:     "hello-world",
				Restart: boolPtr(true),
				Config: map[string]string{
					"KEY": "VALUE_1",
				},
			},
		},
		{
			Name: "set KEY=VALUE without restart",
			ConfigTask: ConfigTask{
				App:     "hello-world",
				Restart: boolPtr(false),
				Config: map[string]string{
					"KEY": "VALUE_1",
				},
			},
		},
		{
			Name: "set the app's entire config, keeping a key written outside the recipe",
			ConfigTask: ConfigTask{
				App:     "hello-world",
				Restart: boolPtr(true),
				Config: map[string]string{
					"KEY":       "VALUE_1",
					"LOG_LEVEL": "info",
				},
				Preserve: []string{"SECRET_KEY_BASE"},
				State:    StateSet,
			},
		},
		{
			Name: "clear the app's config",
			ConfigTask: ConfigTask{
				App:     "hello-world",
				Restart: boolPtr(true),
				State:   StateClear,
			},
		},
	})
}

// Validate checks the config map and preserve list against the state before
// any subprocess call.
func (t ConfigTask) Validate() error {
	state := t.State
	if state == "" {
		state = StatePresent
	}
	switch state {
	case StateSet:
		// An empty map is not a request to unset everything: 'clear' owns that,
		// and config:import refuses an empty payload anyway.
		if len(t.Config) == 0 {
			return errors.New("'config' must not be empty for state 'set'")
		}
		for _, k := range t.Preserve {
			if _, ok := t.Config[k]; ok {
				return fmt.Errorf("key %q must not be in both 'config' and 'preserve'", k)
			}
		}
	case StateClear:
		if len(t.Config) > 0 {
			return errors.New("'config' must not be set for state 'clear'")
		}
	case StatePresent, StateAbsent:
		// The additive states never touch an undeclared key, so a preserve
		// list would do nothing.
		if len(t.Preserve) > 0 {
			return fmt.Errorf("'preserve' is only valid for state 'set' or 'clear', got state '%s'", state)
		}
	}
	return nil
}

// Execute sets or unsets the configuration for a given dokku application
func (t ConfigTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
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
func (t ConfigTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planConfigSet(ctx, t) },
		StateAbsent:  func() PlanResult { return planConfigUnset(ctx, t) },
		StateSet:     func() PlanResult { return planConfigReplace(ctx, t) },
		StateClear:   func() PlanResult { return planConfigClear(ctx, t) },
	})
}

// configKeysToSet returns keys whose desired value differs from the current
// value. The slice is sorted so Plan output is stable across runs.
func configKeysToSet(current, desired map[string]string) []string {
	keys := []string{}
	for k, v := range desired {
		if cur, ok := current[k]; !ok || cur != v {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// configKeysToUnset returns keys present in desired that exist in current.
// The slice is sorted so Plan output is stable across runs.
func configKeysToUnset(current, desired map[string]string) []string {
	keys := []string{}
	for k := range desired {
		if _, ok := current[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// planConfigSet probes current config once, computes the diff, and embeds
// an apply closure that runs `dokku config:set` with only the changed keys.
func planConfigSet(ctx context.Context, t ConfigTask) PlanResult {
	currentConfig, err := getConfig(ctx, t)
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
	if !boolValue(t.Restart, true) {
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
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}
}

// planConfigUnset probes current config once, computes the diff, and embeds
// an apply closure that runs `dokku config:unset` with only existing keys.
func planConfigUnset(ctx context.Context, t ConfigTask) PlanResult {
	currentConfig, err := getConfig(ctx, t)
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
	if !boolValue(t.Restart, true) {
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
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
		},
	}
}

// planConfigReplace makes the declared map the app's entire config, apart from
// the keys configKeptKeys says the recipe does not own. It runs a single
// `config:import --replace`, which clears the app's env file before writing,
// so the kept keys ride along in the payload with their current values rather
// than being lost.
func planConfigReplace(ctx context.Context, t ConfigTask) PlanResult {
	currentConfig, err := getConfig(ctx, t)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	kept, err := configKeptKeys(ctx, t.App, currentConfig, t.Config, t.Preserve)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	owned := 0
	toUnset := []string{}
	for k := range currentConfig {
		if kept[k] {
			continue
		}
		owned++
		if _, ok := t.Config[k]; !ok {
			toUnset = append(toUnset, k)
		}
	}
	sort.Strings(toUnset)
	toSet := configKeysToSet(currentConfig, t.Config)
	if len(toSet) == 0 && len(toUnset) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}

	mutations := make([]string, 0, len(toSet)+len(toUnset))
	for _, k := range toSet {
		if _, ok := currentConfig[k]; ok {
			mutations = append(mutations, fmt.Sprintf("set %s (was set)", k))
		} else {
			mutations = append(mutations, fmt.Sprintf("set %s (new)", k))
		}
	}
	for _, k := range toUnset {
		mutations = append(mutations, fmt.Sprintf("unset %s", k))
	}
	status := PlanStatusModify
	if owned == 0 {
		status = PlanStatusCreate
	}

	payload := make(map[string]string, len(t.Config)+len(kept))
	for k := range kept {
		payload[k] = currentConfig[k]
	}
	for k, v := range t.Config {
		payload[k] = v
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	// Flags go before the app: the subcommand stops reading flags at its
	// first positional argument. `--quiet` also keeps config:import from
	// echoing each value it sets.
	args := []string{"--quiet", "config:import", "--replace", "--format", "json"}
	if !boolValue(t.Restart, true) {
		args = append(args, "--no-restart")
	}
	args = append(args, t.App, "-")
	inputs := []subprocess.ExecCommandInput{{
		Command: "dokku",
		Args:    args,
		// The payload never reaches argv, so neither the declared values nor
		// the kept ones (service DSNs among them) show up in the plan output,
		// the trace log, or the remote process table.
		Stdin: bytes.NewReader(body),
	}}
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d key change(s)", len(mutations)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StateSet, inputs)
		},
	}
}

// planConfigClear unsets every key the recipe owns in one `config:unset`.
// config:clear would also wipe the keys configKeptKeys keeps, and
// config:import refuses an empty payload, so neither can express this.
func planConfigClear(ctx context.Context, t ConfigTask) PlanResult {
	currentConfig, err := getConfig(ctx, t)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	kept, err := configKeptKeys(ctx, t.App, currentConfig, nil, t.Preserve)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	keys := []string{}
	for k := range currentConfig {
		if !kept[k] {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	sort.Strings(keys)
	mutations := make([]string, 0, len(keys))
	for _, k := range keys {
		mutations = append(mutations, fmt.Sprintf("unset %s", k))
	}
	args := []string{"--quiet", "config:unset"}
	if !boolValue(t.Restart, true) {
		args = append(args, "--no-restart")
	}
	args = append(args, t.App)
	args = append(args, keys...)
	inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("clear %d key(s)", len(keys)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateClear, inputs)
		},
	}
}

// configKeptKeys returns the keys in current that are not the recipe's to own,
// which state 'set' and 'clear' leave in place and export leaves out:
//
//   - a key a service link wrote (see isLinkedServiceValue). dokku_service_link
//     owns it, and removing it would break the link;
//   - NO_VHOST, which dokku_domains_toggle owns through domains:disable and
//     domains:enable;
//   - the key named by the app's git rev-env-var property (GIT_REV unless
//     changed), which dokku rewrites on every git build;
//   - any key listed in preserve.
//
// A key the recipe declares is never kept, so a declared value always wins.
// When every current key is declared there is nothing to decide, and the
// service and git probes are skipped.
func configKeptKeys(ctx context.Context, app string, current, declared map[string]string, preserve []string) (map[string]bool, error) {
	kept := map[string]bool{}
	undeclared := false
	for k := range current {
		if _, ok := declared[k]; !ok {
			undeclared = true
			break
		}
	}
	if !undeclared {
		return kept, nil
	}

	revVar, err := gitRevEnvVar(ctx, app)
	if err != nil {
		return nil, err
	}
	dsns, err := linkedServiceDSNs(ctx, app)
	if err != nil {
		return nil, err
	}
	preserved := make(map[string]bool, len(preserve))
	for _, k := range preserve {
		preserved[k] = true
	}
	for k, v := range current {
		if _, ok := declared[k]; ok {
			continue
		}
		if preserved[k] || k == configNoVhostKey || (revVar != "" && k == revVar) || isLinkedServiceValue(v, dsns) {
			kept[k] = true
		}
	}
	return kept, nil
}

// gitRevEnvVar returns the config key dokku writes the deployed git revision
// to, read from `git:report <app> --git-rev-env-var`. dokku reports GIT_REV
// when the property is unset; empty output means no key is written.
func gitRevEnvVar(ctx context.Context, app string) (string, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"git:report", app, "--git-rev-env-var"},
	})
	if err != nil {
		return "", err
	}
	return result.StdoutContents(), nil
}

// ExportApp reads the app's config and returns a dokku_config task carrying
// the current values as state 'set', or nil when there are none. Restart
// mirrors the task default so the emitted recipe matches apply's behaviour;
// the engine lifts the values into the companion vars-file.
//
// The keys configKeptKeys keeps are dropped, so the exported map is exactly
// what state 'set' owns and a round trip agrees with itself. Config vars a
// datastore service link injected are among them: dokku_service_link
// recreates them on apply with the new server's credentials, so re-exporting
// the stale value would clobber the fresh one. NO_VHOST comes back through
// dokku_domains_toggle's own export, and the git rev key through the next
// build. The exclusion happens here, before the engine lifts values, so those
// values never reach the vars-file.
func (t ConfigTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	config, err := getConfig(ctx, ConfigTask{App: app})
	if err != nil {
		return nil, err
	}
	if len(config) == 0 {
		return nil, nil
	}
	kept, err := configKeptKeys(ctx, app, config, nil, nil)
	if err != nil {
		return nil, err
	}
	for k := range kept {
		delete(config, k)
	}
	if len(config) == 0 {
		return nil, nil
	}
	return []interface{}{ConfigTask{App: app, Restart: boolPtr(true), Config: config, State: StateSet}}, nil
}

// getConfig retrieves the current configuration for a given dokku application
func getConfig(ctx context.Context, t ConfigTask) (map[string]string, error) {
	var config map[string]string
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			"config:export",
			"--format",
			"json",
			t.App,
		},
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
