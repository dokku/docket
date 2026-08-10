package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
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
		// A probeable dynamic property only gets a report row once it holds a
		// value, so an absent row means the property is unset rather than that
		// the key map is stale. The entry is compared against the synthesized
		// one so a caller that passed a map without it - where lookup would be
		// the empty string - still takes the error path (#449).
		if dyn, dynamic := dynamicPropertyKeys(plugin, property); dynamic && entry == dyn {
			return "", nil
		}
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
//
// A probeable dynamic family cannot be enumerated in keys, so the properties the
// payload happens to carry are synthesized into it first; without that a set
// letsencrypt `dns-provider-<KEY>` credential is dropped from the export (#449).
func exportProperties(app, subcommand string, keys map[string]PropertyKeys, factory func(app, property, value string) interface{}) ([]interface{}, error) {
	plugin := pluginFromSubcommand(subcommand)
	payload, err := readPropertyReport(plugin, app, false)
	if err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, nil
	}
	keys = withDynamicProperties(plugin, keys, dynamicPropertiesFromReport(plugin, payload, false))

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

// exportGlobalProperties reconstructs the explicitly-set global properties of a
// property plugin, mirroring exportProperties for the --global scope: it reads
// `<plugin>:report --global --format json` once and, for each property whose
// Global key is present and non-empty, emits a task body (built by factory). A
// property with no global form (an empty Global key) is skipped, so
// globally-scoped state (for example scheduler-k3s bootstrap keys) is captured
// instead of silently dropped (#327). Probeable dynamic properties are
// synthesized into keys the same way exportProperties does it.
func exportGlobalProperties(subcommand string, keys map[string]PropertyKeys, factory func(property, value string) interface{}) ([]interface{}, error) {
	plugin := pluginFromSubcommand(subcommand)
	payload, err := readPropertyReport(plugin, "", true)
	if err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, nil
	}
	keys = withDynamicProperties(plugin, keys, dynamicPropertiesFromReport(plugin, payload, true))

	props := make([]string, 0, len(keys))
	for prop := range keys {
		props = append(props, prop)
	}
	sort.Strings(props)

	var out []interface{}
	for _, prop := range props {
		key := keys[prop].Global
		if key == "" {
			continue
		}
		value, ok := payload[key]
		if !ok || value == "" {
			continue
		}
		out = append(out, factory(prop, value))
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

// unknownPropertyWarning returns a diagnostic (and true) when the probe
// identifies the property as unknown, either because the JSON payload had no
// matching key (likely a stale map or a dokku version mismatch) or because the
// :report invocation rejected `--format json` itself (older plugin versions).
// It returns ok=false for every other error, since callers already propagate
// those through PlanResult.Reason. The returned message is raw; planProperty
// attaches it to PlanResult.Warnings and the emitter masks it at output time.
func unknownPropertyWarning(plugin, property string, err error) (PlanWarning, bool) {
	if err == nil {
		return PlanWarning{}, false
	}

	var unknown *errUnknownProperty
	if errors.As(err, &unknown) {
		// Skip the warning for known dynamic-property families (e.g.
		// traefik dns-provider-*) where missing-from-report is the
		// normal pre-set state, not a typo.
		if isDynamicProperty(plugin, property) {
			return PlanWarning{}, false
		}
		return PlanWarning{
			Reason: WarnReasonUnknownProperty,
			Message: fmt.Sprintf("dokku %s:report has no key %q for property %q (available keys: %s)",
				plugin, unknown.lookedFor, property, strings.Join(unknown.validKeys, ", ")),
		}, true
	}

	var execErr *subprocess.ExecError
	if !errors.As(err, &execErr) {
		return PlanWarning{}, false
	}
	stderr := strings.TrimSpace(execErr.Response.Stderr)
	if !strings.Contains(stderr, "Invalid flag passed, valid flags:") {
		return PlanWarning{}, false
	}
	return PlanWarning{
		Reason:  WarnReasonProbeRejected,
		Message: fmt.Sprintf("dokku %s:report rejected probe for property %q: %s", plugin, property, stderr),
	}, true
}

// isDynamicProperty reports whether a (plugin, property) pair represents a
// dynamic property family whose JSON keys only appear after the property is
// set. Examples:
//   - dokku-letsencrypt `dns-provider-*` (arbitrary env var names)
//   - dokku traefik `dns-provider-*` (same shape, per-provider env vars)
//
// Dynamic property names are validated by `:set`, not the report schema, so
// this is what lets validateProperty accept a name that cannot be enumerated in
// the key map. It says nothing about whether the property can be probed: a
// family whose plugin reports its keys is probed through dynamicPropertyKeys,
// and only the families that helper does not recognize skip the probe.
//
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

// dynamicPropertyKeys returns the report keys for a dynamic property whose
// plugin surfaces the family in its `:report` payload. dokku-letsencrypt
// 0.25.0+ emits a `dns-provider-<KEY>` row for the app scope and a
// `global-dns-provider-<KEY>` row for the global one per property that is set,
// so the keys can be synthesized from the property name and probed like any
// mapped property (#449). The values are DNS provider API credentials, hence
// Sensitive: planProperty registers the probed value with the masker before it
// can reach a `(was %q)` drift reason.
//
// traefik's `dns-provider-*` family has the same shape but is still absent from
// `traefik:report` (dokku/dokku#8928, tracked for docket in #450), so it stays
// on the unprobed path.
func dynamicPropertyKeys(plugin, property string) (PropertyKeys, bool) {
	if plugin != "letsencrypt" || !isDynamicProperty(plugin, property) {
		return PropertyKeys{}, false
	}
	return PropertyKeys{PerApp: property, Global: "global-" + property, Sensitive: true}, true
}

// propertyEntry returns the PropertyKeys a plugin uses for one property,
// falling back to the synthesized entry when the property belongs to a
// probeable dynamic family the map cannot enumerate. Reading the map directly
// instead would report a `dns-provider-<KEY>` credential as non-Sensitive and
// export it in cleartext (#451).
func propertyEntry(plugin, property string, keys map[string]PropertyKeys) PropertyKeys {
	if entry, ok := keys[property]; ok {
		return entry
	}
	entry, _ := dynamicPropertyKeys(plugin, property)
	return entry
}

// withDynamicProperties returns keys plus a synthesized entry for every probeable
// dynamic property in props. The map is copied only when there is something to
// add, since the caller's map is a package-level singleton shared by every task
// of that plugin.
func withDynamicProperties(plugin string, keys map[string]PropertyKeys, props []string) map[string]PropertyKeys {
	var merged map[string]PropertyKeys
	for _, property := range props {
		entry, ok := dynamicPropertyKeys(plugin, property)
		if !ok {
			continue
		}
		if merged == nil {
			merged = make(map[string]PropertyKeys, len(keys)+len(props))
			for k, v := range keys {
				merged[k] = v
			}
		}
		merged[property] = entry
	}
	if merged == nil {
		return keys
	}
	return merged
}

// dynamicPropertiesFromReport returns the probeable dynamic property names a
// report payload carries for the given scope, sorted. In the app scope the
// payload also carries the `global-` and `computed-` variants of a key; only the
// bare row is the app's own value, so a key is kept only when the scope's
// synthesized lookup round-trips back to it.
func dynamicPropertiesFromReport(plugin string, payload map[string]string, global bool) []string {
	var props []string
	for key := range payload {
		property := key
		if global {
			property = strings.TrimPrefix(key, "global-")
			if property == key {
				continue
			}
		}
		entry, ok := dynamicPropertyKeys(plugin, property)
		if !ok {
			continue
		}
		lookup := entry.PerApp
		if global {
			lookup = entry.Global
		}
		if lookup != key {
			continue
		}
		props = append(props, property)
	}
	sort.Strings(props)
	return props
}

// planProperty is the shared Plan() implementation for property tasks. It
// probes the current value via getProperty using the task's PropertyKeys
// map, returns InSync when current matches desired, and otherwise embeds an
// apply closure that runs the underlying `dokku <subcommand>` call.
// ExecutePlan is the only invoker.
//
// When the probe errors (other than SSH transport failures), the apply
// closure runs the set/unset unconditionally. Diagnostic warnings are
// attached to PlanResult.Warnings via unknownPropertyWarning for typos and
// unsupported plugin versions (the run loop routes them through the emitter);
// other probe failures are recorded in PlanResult.Reason and the apply still
// runs, matching pre-probe behavior.
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

	// A dynamic property has no static map entry. When its plugin reports the
	// family, synthesize the entry so the probe path below runs against it; the
	// families that stay unreported keep falling through to the unprobed path.
	// This has to happen before the Sensitive read below, which is what
	// registers a synthesized credential with the masker.
	keys = withDynamicProperties(plugin, keys, []string{property})

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
			var warnings []PlanWarning
			if probeErr != nil {
				var sshErr *subprocess.SSHError
				if errors.As(probeErr, &sshErr) {
					return PlanResult{Status: PlanStatusError, Error: probeErr}
				}
				if w, ok := unknownPropertyWarning(plugin, property, probeErr); ok {
					warnings = append(warnings, w)
				}
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
				Warnings:  warnings,
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
			var warnings []PlanWarning
			if probeErr != nil {
				var sshErr *subprocess.SSHError
				if errors.As(probeErr, &sshErr) {
					return PlanResult{Status: PlanStatusError, Error: probeErr}
				}
				if w, ok := unknownPropertyWarning(plugin, property, probeErr); ok {
					warnings = append(warnings, w)
				}
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
				Warnings:  warnings,
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
// dynamic properties that have no probe key (e.g. traefik dns-provider-*).
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
