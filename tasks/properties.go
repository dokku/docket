package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// PropertyKeys carries the JSON keys that dokku <plugin>:report --format json
// emits for one property on dokku 0.38.8+. An empty string means the property
// has no form in that scope (so probing it in that scope is rejected at plan
// time, matching dokku's own CLI rejection).
//
// Values point at the canonical bare-key shape (no <plugin>- prefix). The
// legacy <plugin>-prefixed keys remain emitted during the 0.38.x deprecation
// window but are ignored by the lookup.
//
// Sensitive marks a property whose value is a secret (e.g. a password or a
// cluster token). When set, planProperty registers both the desired value and
// the server-probed current value with the masker, so the `(was %q)` drift
// reason and the command echo are masked. It is a per-property flag because a
// plugin usually has a mix of secret and benign properties.
type PropertyKeys struct {
	PerApp    string
	Global    string
	Sensitive bool
}

// pluginFromSubcommand returns the plugin component of a colon-separated
// subcommand. For example, "nginx:set" -> "nginx", "buildpacks:set-property" ->
// "buildpacks", "app-json:set" -> "app-json".
func pluginFromSubcommand(subcommand string) string {
	return strings.SplitN(subcommand, ":", 2)[0]
}

// errUnknownProperty is returned by getProperty when the JSON :report payload
// has no entry for the key the task asked us to look up.
type errUnknownProperty struct {
	plugin    string
	property  string
	lookedFor string
	validKeys []string
}

func (e *errUnknownProperty) Error() string {
	return fmt.Sprintf("dokku %s:report has no key %q for property %q", e.plugin, e.lookedFor, e.property)
}

// getProperty reads the current value of a property via
// `dokku <plugin>:report [<app>|--global] --format json`. The JSON payload is
// parsed and the value is read from the JSON key the task specifies via its
// PropertyKeys map.
//
// Returns:
//   - (value, nil) when the looked-up key exists in the JSON payload
//   - ("", *errUnknownProperty) when the JSON parsed but the key was absent
//   - ("", err) when the exec or JSON parse failed
func getProperty(subcommand, app string, global bool, property string, keys map[string]PropertyKeys) (string, error) {
	plugin := pluginFromSubcommand(subcommand)
	args := getPropertyArgs(plugin, app, global)

	response, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    args,
	})
	if err != nil {
		return "", err
	}

	payload := map[string]string{}
	if err := json.Unmarshal(response.StdoutBytes(), &payload); err != nil {
		return "", fmt.Errorf("parse %s:report json: %w", plugin, err)
	}

	entry := keys[property]
	lookup := entry.PerApp
	if global {
		lookup = entry.Global
	}

	value, ok := payload[lookup]
	if !ok {
		keyList := make([]string, 0, len(payload))
		for k := range payload {
			keyList = append(keyList, k)
		}
		sort.Strings(keyList)
		return "", &errUnknownProperty{
			plugin:    plugin,
			property:  property,
			lookedFor: lookup,
			validKeys: keyList,
		}
	}
	return value, nil
}

// exportProperties reconstructs the explicitly-set properties of a property
// plugin for an app. It reads `<plugin>:report --format json` once and, for
// each property in keys, emits a task body (built by factory) when the raw
// per-app key is present and non-empty. dokku only includes the raw key in the
// report when the property has been set (unset properties appear only under a
// `computed-` key), so this naturally captures the non-default settings without
// a defaults table. Read-only/computed keys are skipped because they are not in
// keys.
func exportProperties(app, subcommand string, keys map[string]PropertyKeys, factory func(app, property, value string) interface{}) ([]interface{}, error) {
	plugin := pluginFromSubcommand(subcommand)
	payload, err := readPropertyReport(plugin, app, false)
	if err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, nil
	}

	props := make([]string, 0, len(keys))
	for prop := range keys {
		props = append(props, prop)
	}
	sort.Strings(props)

	var out []interface{}
	for _, prop := range props {
		key := keys[prop].PerApp
		if key == "" {
			continue
		}
		value, ok := payload[key]
		if !ok || value == "" {
			continue
		}
		out = append(out, factory(app, prop, value))
	}
	return out, nil
}

// readPropertyReport runs `dokku <plugin>:report [<app>|--global] --format json`
// and returns the decoded payload, distinguishing a plugin that is not installed
// (returns nil, nil - a quiet skip) from one that is installed but whose report
// cannot be read (returns an error the export surfaces as a warning). An SSH
// transport failure always propagates. Any other exec failure is a quiet skip
// only when the plugin is not installed; when it is installed, the failure is an
// error. A JSON parse failure is always an error, since the exec succeeded so the
// plugin responded with something unparseable (for example a deprecation line
// printed before the JSON payload) rather than being absent (#329).
func readPropertyReport(plugin, app string, global bool) (map[string]string, error) {
	response, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    getPropertyArgs(plugin, app, global),
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		if installed, ierr := pluginInstalled(plugin); ierr != nil || !installed {
			return nil, nil
		}
		return nil, fmt.Errorf("dokku %s:report failed: %w", plugin, err)
	}

	payload := map[string]string{}
	if err := json.Unmarshal(response.StdoutBytes(), &payload); err != nil {
		return nil, fmt.Errorf("parse %s:report json: %w", plugin, err)
	}
	return payload, nil
}

// getPropertyArgs builds the `dokku <plugin>:report ... --format json` arg list.
func getPropertyArgs(plugin, app string, global bool) []string {
	args := []string{"--quiet", plugin + ":report"}
	if global {
		args = append(args, "--global")
	} else {
		args = append(args, app)
	}
	return append(args, "--format", "json")
}

// warnIfUnknownProperty surfaces a diagnostic when the probe identifies the
// property as unknown, either because the JSON payload had no matching key
// (likely a stale map or a dokku version mismatch) or because the :report
// invocation rejected `--format json` itself (older plugin versions). Other
// errors are silent because callers already propagate them through
// PlanResult.Reason.
func warnIfUnknownProperty(plugin, property string, err error) {
	if err == nil {
		return
	}

	var unknown *errUnknownProperty
	if errors.As(err, &unknown) {
		// Skip the warning for known dynamic-property families (e.g.
		// letsencrypt dns-provider-*) where missing-from-report is the
		// normal pre-set state, not a typo.
		if isDynamicProperty(plugin, property) {
			return
		}
		log.Printf("warning: dokku %s:report has no key %q for property %q (available keys: %s)",
			plugin, unknown.lookedFor, property,
			strings.Join(unknown.validKeys, ", "))
		return
	}

	var execErr *subprocess.ExecError
	if !errors.As(err, &execErr) {
		return
	}
	stderr := strings.TrimSpace(execErr.Response.Stderr)
	if !strings.Contains(stderr, "Invalid flag passed, valid flags:") {
		return
	}
	log.Printf("warning: dokku %s:report rejected probe for property %q: %s", plugin, property, stderr)
}

// isDynamicProperty reports whether a (plugin, property) pair represents a
// dynamic property family whose JSON keys only appear after the property is
// set. Examples:
//   - dokku-letsencrypt `dns-provider-*` (arbitrary env var names)
//   - dokku traefik `dns-provider-*` (same shape, per-provider env vars)
//
// Dynamic property names are validated by `:set`, not the report schema.
// scheduler-k3s `chart.*.*` used to live here but moved to the dedicated
// dokku_scheduler_k3s_chart task; SchedulerK3sPropertyTask.Plan rejects
// chart.* before reaching this helper.
func isDynamicProperty(plugin, property string) bool {
	switch plugin {
	case "letsencrypt", "traefik":
		return strings.HasPrefix(property, "dns-provider-")
	}
	return false
}

// planProperty is the shared Plan() implementation for property tasks. It
// probes the current value via getProperty using the task's PropertyKeys
// map, returns InSync when current matches desired, and otherwise embeds an
// apply closure that runs the underlying `dokku <subcommand>` call.
// ExecutePlan is the only invoker.
//
// When the probe errors (other than SSH transport failures), the apply
// closure runs the set/unset unconditionally. Diagnostic warnings are
// emitted via warnIfUnknownProperty for typos and unsupported plugin
// versions; other probe failures are recorded in PlanResult.Reason and
// the apply still runs, matching pre-probe behavior.
// validatePropertyInput checks a property task's inputs without probing the
// server: app/global scoping, that the property is supported for the target
// scope, and that a value is supplied only in the state that allows it. Both
// planProperty and each property task's Validate() call it so plan and
// validate report the same errors.
func validatePropertyInput(state State, app string, global bool, property, value, subcommand string, keys map[string]PropertyKeys) error {
	if !global && app == "" {
		return errors.New("app is required when global is false")
	}
	if global && app != "" {
		return fmt.Errorf("'app' must not be set when 'global' is set to true")
	}
	if err := validateProperty(pluginFromSubcommand(subcommand), property, global, keys); err != nil {
		return err
	}
	if state == StatePresent && value == "" {
		return fmt.Errorf("setting a state of 'present' is invalid without a value for 'value'")
	}
	if state == StateAbsent && value != "" {
		return fmt.Errorf("setting a state of 'absent' is invalid with a value for 'value'")
	}
	return nil
}

func planProperty(state State, app string, global bool, property, value, subcommand string, keys map[string]PropertyKeys) PlanResult {
	if err := validatePropertyInput(state, app, global, property, value, subcommand, keys); err != nil {
		return planErr(err)
	}

	plugin := pluginFromSubcommand(subcommand)
	target := app
	if global {
		target = "--global"
	}

	// A property flagged Sensitive carries a secret value. Register the desired
	// value now so the command echo is masked; empties are dropped. The
	// server-probed current value is registered after each probe below, since
	// it is not known from the recipe (a hand-written recipe never tags it, and
	// the `(was %q)` drift reason would otherwise leak the live server secret).
	sensitive := keys[property].Sensitive
	if sensitive {
		subprocess.AddGlobalSensitive(value)
	}

	return DispatchPlan(state, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			// Dynamic property families have no probe key; treat as drift
			// and run the mutation unconditionally.
			if _, mapped := keys[property]; !mapped && isDynamicProperty(plugin, property) {
				return runUnprobedSet(subcommand, target, property, value)
			}

			// Probe; treat dokku-level failure as "drift, must mutate"
			// (matches pre-probe behavior for unsupported plugins) but
			// surface SSH transport failures so the user sees `! ssh:`.
			current, probeErr := getProperty(subcommand, app, global, property, keys)
			if sensitive {
				subprocess.AddGlobalSensitive(current)
			}
			if probeErr != nil {
				var sshErr *subprocess.SSHError
				if errors.As(probeErr, &sshErr) {
					return PlanResult{Status: PlanStatusError, Error: probeErr}
				}
				warnIfUnknownProperty(plugin, property, probeErr)
			}
			if probeErr == nil && current == value {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}

			status := PlanStatusModify
			reason := fmt.Sprintf("would set %s on %s", property, target)
			if probeErr != nil {
				reason = fmt.Sprintf("would set %s on %s (probe failed: %v)", property, target, probeErr)
			} else if current == "" {
				status = PlanStatusCreate
				reason = fmt.Sprintf("%s missing on %s", property, target)
			} else {
				reason = fmt.Sprintf("%s drift on %s (was %q)", property, target, current)
			}

			inputs := propertySetInputs(subcommand, target, property, value)
			return PlanResult{
				InSync:    false,
				Status:    status,
				Reason:    reason,
				Mutations: []string{fmt.Sprintf("set %s=%s", property, value)},
				Commands:  resolveCommands(inputs),
				apply:     applyPropertySet(subcommand, target, property, value),
			}
		},
		StateAbsent: func() PlanResult {
			if _, mapped := keys[property]; !mapped && isDynamicProperty(plugin, property) {
				return runUnprobedUnset(subcommand, target, property)
			}

			current, probeErr := getProperty(subcommand, app, global, property, keys)
			if sensitive {
				subprocess.AddGlobalSensitive(current)
			}
			if probeErr != nil {
				var sshErr *subprocess.SSHError
				if errors.As(probeErr, &sshErr) {
					return PlanResult{Status: PlanStatusError, Error: probeErr}
				}
				warnIfUnknownProperty(plugin, property, probeErr)
			}
			if probeErr == nil && current == "" {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}

			reason := fmt.Sprintf("would unset %s on %s", property, target)
			if probeErr != nil {
				reason = fmt.Sprintf("would unset %s on %s (probe failed: %v)", property, target, probeErr)
			} else {
				reason = fmt.Sprintf("would unset %s on %s (was %q)", property, target, current)
			}

			inputs := propertyUnsetInputs(subcommand, target, property)
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    reason,
				Mutations: []string{fmt.Sprintf("unset %s", property)},
				Commands:  resolveCommands(inputs),
				apply:     applyPropertyUnset(subcommand, target, property),
			}
		},
	})
}

// validateProperty rejects unsupported properties or scope mismatches before
// any subprocess call. Dynamic property families bypass validation since they
// can't be enumerated in the map.
func validateProperty(plugin, property string, global bool, keys map[string]PropertyKeys) error {
	entry, ok := keys[property]
	if !ok {
		if isDynamicProperty(plugin, property) {
			return nil
		}
		supported := make([]string, 0, len(keys))
		for k := range keys {
			supported = append(supported, k)
		}
		sort.Strings(supported)
		return fmt.Errorf("dokku %s: unsupported property %q (supported: %s)", plugin, property, strings.Join(supported, ", "))
	}
	if global && entry.Global == "" {
		return fmt.Errorf("property %q on plugin %s has no global form", property, plugin)
	}
	if !global && entry.PerApp == "" {
		return fmt.Errorf("property %q on plugin %s has no per-app form", property, plugin)
	}
	return nil
}

// runUnprobedSet returns a PlanResult that runs `:set` unconditionally for
// dynamic properties that have no probe key (e.g. letsencrypt dns-provider-*).
func runUnprobedSet(subcommand, target, property, value string) PlanResult {
	inputs := propertySetInputs(subcommand, target, property, value)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusModify,
		Reason:    fmt.Sprintf("would set %s on %s (no probe key)", property, target),
		Mutations: []string{fmt.Sprintf("set %s=%s", property, value)},
		Commands:  resolveCommands(inputs),
		apply:     applyPropertySet(subcommand, target, property, value),
	}
}

// runUnprobedUnset returns a PlanResult that runs `:set` (no value, the unset
// form) unconditionally for dynamic properties with no probe key.
func runUnprobedUnset(subcommand, target, property string) PlanResult {
	inputs := propertyUnsetInputs(subcommand, target, property)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("would unset %s on %s (no probe key)", property, target),
		Mutations: []string{fmt.Sprintf("unset %s", property)},
		Commands:  resolveCommands(inputs),
		apply:     applyPropertyUnset(subcommand, target, property),
	}
}

// propertySetInputs returns the subprocess inputs that set a property.
func propertySetInputs(subcommand, target, property, value string) []subprocess.ExecCommandInput {
	return []subprocess.ExecCommandInput{
		{Command: "dokku", Args: []string{"--quiet", subcommand, target, property, value}},
	}
}

// propertyUnsetInputs returns the subprocess inputs that unset a property.
func propertyUnsetInputs(subcommand, target, property string) []subprocess.ExecCommandInput {
	return []subprocess.ExecCommandInput{
		{Command: "dokku", Args: []string{"--quiet", subcommand, target, property}},
	}
}

// applyPropertySet returns a closure that runs `dokku <subcommand> <target>
// <property> <value>` and converts the result into a TaskOutputState.
func applyPropertySet(subcommand, target, property, value string) func() TaskOutputState {
	inputs := propertySetInputs(subcommand, target, property, value)
	return func() TaskOutputState {
		return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
	}
}

// applyPropertyUnset returns a closure that runs `dokku <subcommand> <target>
// <property>` (no value, which dokku interprets as unset) and converts the
// result into a TaskOutputState.
func applyPropertyUnset(subcommand, target, property string) func() TaskOutputState {
	inputs := propertyUnsetInputs(subcommand, target, property)
	return func() TaskOutputState {
		return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
	}
}
