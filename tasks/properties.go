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

// PropertyContext represents the context for a property
type PropertyContext struct {
	// App is the name of the app
	App string `required:"true" yaml:"app"`

	// Global is a flag indicating if the property should be applied globally
	Global bool `required:"false" yaml:"global"`

	// Property is the name of the property to set
	Property string `required:"true" yaml:"property"`

	// Value is the value of the property to set
	Value string `required:"false" yaml:"value"`
}

// pluginFromSubcommand returns the plugin component of a colon-separated
// subcommand. For example, "nginx:set" -> "nginx", "buildpacks:set-property" ->
// "buildpacks", "app-json:set" -> "app-json".
func pluginFromSubcommand(subcommand string) string {
	return strings.SplitN(subcommand, ":", 2)[0]
}

// errUnknownProperty is returned by getProperty when the JSON :report payload
// has no key matching the requested property. The available keys are carried
// so warnIfUnknownProperty can render them in the diagnostic.
type errUnknownProperty struct {
	plugin    string
	property  string
	global    bool
	validKeys []string
}

func (e *errUnknownProperty) Error() string {
	return fmt.Sprintf("dokku %s:report has no key for property %q", e.plugin, e.property)
}

// getProperty reads the current value of a property via
// `dokku <plugin>:report [<app>|--global] --format json`. The JSON payload is
// parsed and the value is looked up via resolvePropertyKey, which knows the
// per-plugin naming conventions for raw vs. global vs. computed keys.
//
// Returns:
//   - (value, nil) when the property exists in the JSON payload
//   - ("", *errUnknownProperty) when the JSON parsed but no candidate key matched
//   - ("", err) when the exec or JSON parse failed
func getProperty(subcommand, app string, global bool, property string) (string, error) {
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

	value, ok := resolvePropertyKey(payload, plugin, property, global)
	if !ok {
		keys := make([]string, 0, len(payload))
		for k := range payload {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return "", &errUnknownProperty{plugin: plugin, property: property, global: global, validKeys: keys}
	}
	return value, nil
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

// resolvePropertyKey looks up the property value in a parsed :report JSON
// payload, trying the candidate keys returned by candidateKeys in order.
// The first match wins.
func resolvePropertyKey(payload map[string]string, plugin, property string, global bool) (string, bool) {
	for _, k := range candidateKeys(plugin, property, global) {
		if v, ok := payload[k]; ok {
			return v, true
		}
	}
	return "", false
}

// candidateKeys returns the ordered list of JSON keys to try for a given
// (plugin, property, global) tuple. The list matches the empirically-verified
// naming conventions in dokku >= 0.38.5 and dokku-letsencrypt >= 0.25.0.
func candidateKeys(plugin, property string, global bool) []string {
	if !global {
		return []string{
			property,
			plugin + "-" + property,
		}
	}
	candidates := []string{
		"global-" + property,
		plugin + "-global-" + property,
		property,
		plugin + "-" + property,
	}
	// Plugins with grouped subsystems (e.g. logs vector-image, logs
	// vector-networks) use <plugin>-<group>-global-<rest> instead of
	// <plugin>-global-<group>-<rest>. Filed as dokku/dokku#8632.
	if dashIdx := strings.Index(property, "-"); dashIdx > 0 {
		group := property[:dashIdx]
		rest := property[dashIdx+1:]
		candidates = append(candidates, plugin+"-"+group+"-global-"+rest)
	}
	return candidates
}

// warnIfUnknownProperty surfaces a diagnostic when the probe identifies the
// property as unknown, either because the JSON payload had no matching key
// (likely a typo) or because the :report invocation rejected `--format json`
// itself (older plugin versions). Other errors are silent because callers
// already propagate them through PlanResult.Reason.
func warnIfUnknownProperty(plugin, property string, global bool, err error) {
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
		log.Printf("warning: dokku %s:report has no key for property %q (looked for: %s; available keys: %s)",
			plugin, property,
			strings.Join(candidateKeys(plugin, property, global), ", "),
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
// set. The dokku-letsencrypt plugin uses `dns-provider-*` for arbitrary
// per-credential env var names (e.g. `dns-provider-NAMECHEAP_API_USER`); the
// names are validated by `:set`, not the report schema.
func isDynamicProperty(plugin, property string) bool {
	return plugin == "letsencrypt" && strings.HasPrefix(property, "dns-provider-")
}

// planProperty is the shared Plan() implementation for property tasks. It
// probes the current value via getProperty, returns InSync when current
// matches desired, and otherwise embeds an apply closure that runs the
// underlying `dokku <subcommand>` call. ExecutePlan is the only invoker.
//
// When the probe errors (other than SSH transport failures), the apply
// closure runs the set/unset unconditionally. Diagnostic warnings are
// emitted via warnIfUnknownProperty for typos and unsupported plugin
// versions; other probe failures are recorded in PlanResult.Reason and
// the apply still runs, matching pre-probe behavior.
func planProperty(state State, app string, global bool, property, value, subcommand string) PlanResult {
	if !global && app == "" {
		return PlanResult{
			Status: PlanStatusError,
			Error:  errors.New("app is required when global is false"),
		}
	}
	if global && app != "" {
		return PlanResult{
			Status: PlanStatusError,
			Error:  fmt.Errorf("'app' must not be set when 'global' is set to true"),
		}
	}

	plugin := pluginFromSubcommand(subcommand)
	target := app
	if global {
		target = "--global"
	}

	return DispatchPlan(state, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			if value == "" {
				return PlanResult{
					Status: PlanStatusError,
					Error:  fmt.Errorf("setting a state of 'present' is invalid without a value for 'value'"),
				}
			}

			// Probe; treat dokku-level failure as "drift, must mutate"
			// (matches pre-probe behavior for unsupported plugins) but
			// surface SSH transport failures so the user sees `! ssh:`.
			current, probeErr := getProperty(subcommand, app, global, property)
			if probeErr != nil {
				var sshErr *subprocess.SSHError
				if errors.As(probeErr, &sshErr) {
					return PlanResult{Status: PlanStatusError, Error: probeErr}
				}
				warnIfUnknownProperty(plugin, property, global, probeErr)
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
			if value != "" {
				return PlanResult{
					Status: PlanStatusError,
					Error:  fmt.Errorf("setting a state of 'absent' is invalid with a value for 'value'"),
				}
			}

			current, probeErr := getProperty(subcommand, app, global, property)
			if probeErr != nil {
				var sshErr *subprocess.SSHError
				if errors.As(probeErr, &sshErr) {
					return PlanResult{Status: PlanStatusError, Error: probeErr}
				}
				warnIfUnknownProperty(plugin, property, global, probeErr)
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
