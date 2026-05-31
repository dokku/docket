package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// SchedulerK3sChartTask manages helm chart value overrides for one of
// dokku's bundled scheduler-k3s charts via the dokku
// `scheduler-k3s:charts:set` / `:charts:report` subcommands. Chart values
// are global by design in dokku, so the task carries no app/global toggle.
type SchedulerK3sChartTask struct {
	// Chart is the helm chart whose values to manage. Dokku validates the
	// chart name against its known HelmCharts list at apply time.
	Chart string `required:"true" yaml:"chart" description:"Name of the helm chart whose values to set (validated by dokku against its bundled HelmCharts list)."`

	// Values is the desired set of helm value overrides. It accepts either
	// a flat map of dotted Helm property paths (e.g. resources.limits.cpu)
	// or a nested tree (resources: { limits: { cpu: 200m } }). Both shapes
	// flatten to the same dotted form before being handed to dokku. See
	// flattenChartValues for the exact coalescing rules, including the
	// nested-segment escaping that lets dotted leaf keys survive Helm's
	// strvals parser.
	Values map[string]any `required:"true" yaml:"values,omitempty" description:"Helm-style values for the chart. Accepts a flat map of dotted property paths or a nested tree; both coalesce to the same key/value form before being applied. In nested form, literal dots in a key segment are escaped to \\. so they reach Helm as a single annotation/label key."`

	// State is the desired state of the chart values.
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the chart values."`
}

// SchedulerK3sChartTaskExample contains an example of a SchedulerK3sChartTask
type SchedulerK3sChartTaskExample struct {
	// Name is the task name holding the SchedulerK3sChartTask description
	Name string `yaml:"-"`

	// SchedulerK3sChartTask is the SchedulerK3sChartTask configuration
	SchedulerK3sChartTask SchedulerK3sChartTask `yaml:"dokku_scheduler_k3s_chart"`
}

// GetName returns the name of the example
func (e SchedulerK3sChartTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the scheduler-k3s chart task
func (t SchedulerK3sChartTask) Doc() string {
	return "Manages helm chart value overrides for a dokku scheduler-k3s bundled chart"
}

// Examples returns the examples for the scheduler-k3s chart task
func (t SchedulerK3sChartTask) Examples() ([]Doc, error) {
	return MarshalExamples([]SchedulerK3sChartTaskExample{
		{
			Name: "Set chart values via a flat map of dotted paths",
			SchedulerK3sChartTask: SchedulerK3sChartTask{
				Chart: "ingress-nginx",
				Values: map[string]any{
					"controller.replicaCount":      "3",
					"controller.resources.limits.cpu": "200m",
				},
			},
		},
		{
			Name: "Set chart values via a nested tree (Helm values.yaml style)",
			SchedulerK3sChartTask: SchedulerK3sChartTask{
				Chart: "ingress-nginx",
				Values: map[string]any{
					"controller": map[string]any{
						"replicaCount": "3",
						"resources": map[string]any{
							"limits": map[string]any{
								"cpu": "200m",
							},
						},
					},
				},
			},
		},
		{
			Name: "Mix nested and flat; nested dotted leaves are escaped for Helm",
			SchedulerK3sChartTask: SchedulerK3sChartTask{
				Chart: "traefik",
				Values: map[string]any{
					"service": map[string]any{
						"annotations": map[string]any{
							"service.beta.kubernetes.io/aws-load-balancer-type": "nlb",
						},
					},
					"ports.web.redirectTo.port": "websecure",
				},
			},
		},
		{
			Name: "Clear specific chart values",
			SchedulerK3sChartTask: SchedulerK3sChartTask{
				Chart: "ingress-nginx",
				Values: map[string]any{
					"controller.replicaCount": "",
				},
				State: StateAbsent,
			},
		},
	})
}

// Execute sets or clears the configured chart values
func (t SchedulerK3sChartTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the SchedulerK3sChartTask would produce.
func (t SchedulerK3sChartTask) Plan() PlanResult {
	if t.Chart == "" {
		return PlanResult{Status: PlanStatusError, Error: errors.New("chart is required")}
	}
	if len(t.Values) == 0 {
		state := t.State
		if state == "" {
			state = StatePresent
		}
		return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'values' must not be empty for state '%s'", state)}
	}

	desired, err := flattenChartValues(t.Values)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	currentFn := func() (map[string]string, error) {
		return getSchedulerK3sChartValues(t.Chart)
	}
	commandFn := func(key, value string) subprocess.ExecCommandInput {
		return subprocess.ExecCommandInput{
			Command: "dokku",
			Args: []string{
				"--quiet",
				"scheduler-k3s:charts:set",
				t.Chart + "." + key,
				value,
			},
		}
	}

	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planPairsSet("chart value", desired, currentFn, commandFn) },
		StateAbsent:  func() PlanResult { return planPairsUnset("chart value", desired, currentFn, commandFn) },
	})
}

// getSchedulerK3sChartValues reads the helm value overrides currently
// stored for the given chart. The dokku command returns a flat JSON map
// keyed `<chart>.<override-key>`; this strips the `<chart>.` prefix to
// return a map keyed by the override key alone (matching the form
// callers hand back when setting).
func getSchedulerK3sChartValues(chart string) (map[string]string, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			"scheduler-k3s:charts:report",
			chart,
			"--format", "json",
		},
	})
	if err != nil {
		return nil, err
	}

	payload := map[string]string{}
	if err := json.Unmarshal(result.StdoutBytes(), &payload); err != nil {
		return nil, fmt.Errorf("parse scheduler-k3s:charts:report json: %w", err)
	}

	prefix := chart + "."
	values := map[string]string{}
	for composedKey, value := range payload {
		if !strings.HasPrefix(composedKey, prefix) {
			continue
		}
		values[strings.TrimPrefix(composedKey, prefix)] = value
	}
	return values, nil
}

// flattenChartValues coalesces a Values map (mix of flat dotted keys and
// nested maps) into the dotted property/value form dokku's
// `scheduler-k3s:charts:set` accepts.
//
// Rules:
//   - Scalar leaf (string, int, float, bool) -> rendered via "%v".
//   - nil -> "".
//   - Nested map[string]any (or legacy map[any]any from yaml.v2) -> recurse,
//     joining segments with ".". Each nested segment has any literal "."
//     escaped to "\." so dotted annotation/label keys survive Helm's
//     strvals parser as a single key.
//   - Top-level keys are passed through verbatim (no escaping). This
//     preserves the dokku CLI convention of writing "resources.limits.cpu"
//     as a single dotted Helm path.
//   - When a top-level key contains "." and its value is a nested map,
//     the top-level prefix is unescaped (Helm path separator) and the
//     nested-segment escaping applies inside the value.
//   - Lists/arrays are rejected (Helm `--set` supports indexed flattening
//     but it is out of scope here).
//   - Two source paths that flatten to the same key produce a typed error
//     so the caller (and the user) can fix the recipe rather than relying
//     on map-iteration order.
func flattenChartValues(in map[string]any) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range in {
		if err := flattenInto(out, key, value); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// flattenInto writes one or more entries into out for the (path, value)
// pair. Leaves call assignChartValue once; nested maps recurse with a
// joined, escaped child path.
func flattenInto(out map[string]string, path string, value any) error {
	switch v := value.(type) {
	case nil:
		return assignChartValue(out, path, "")
	case string:
		return assignChartValue(out, path, v)
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return assignChartValue(out, path, fmt.Sprintf("%v", v))
	case map[string]any:
		return flattenNestedMap(out, path, v)
	case map[any]any:
		converted, err := convertLegacyMap(path, v)
		if err != nil {
			return err
		}
		return flattenNestedMap(out, path, converted)
	case []any:
		return fmt.Errorf("chart value %q: lists are not supported (Helm --set indexed syntax can be added later if needed)", path)
	default:
		return fmt.Errorf("chart value %q: unsupported value type %T", path, value)
	}
}

// flattenNestedMap walks a nested map, escaping each child segment and
// recursing with the joined path. Keys are sorted so the output is
// deterministic across map-iteration orders.
func flattenNestedMap(out map[string]string, path string, nested map[string]any) error {
	if len(nested) == 0 {
		return fmt.Errorf("chart value %q: nested map must contain at least one entry", path)
	}
	keys := make([]string, 0, len(nested))
	for k := range nested {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "" {
			return fmt.Errorf("chart value %q: nested map key must not be empty", path)
		}
		childPath := path + "." + escapeChartSegment(k)
		if err := flattenInto(out, childPath, nested[k]); err != nil {
			return err
		}
	}
	return nil
}

// convertLegacyMap upgrades a map[any]any (yaml.v2 default for nested
// mappings) to map[string]any. Non-string keys produce a typed error
// rather than silently coercing.
func convertLegacyMap(path string, in map[any]any) (map[string]any, error) {
	out := make(map[string]any, len(in))
	for k, v := range in {
		ks, ok := k.(string)
		if !ok {
			return nil, fmt.Errorf("chart value %q: nested map keys must be strings, got %T", path, k)
		}
		out[ks] = v
	}
	return out, nil
}

// assignChartValue writes (key, value) to out, erroring if key is already
// present with a different value (catches collisions between flat and
// nested forms of the same path).
func assignChartValue(out map[string]string, key, value string) error {
	if key == "" {
		return errors.New("chart value key must not be empty")
	}
	if existing, ok := out[key]; ok {
		if existing == value {
			return nil
		}
		return fmt.Errorf("duplicate chart value key %q (set via both nested and flat forms with different values: %q vs %q)", key, existing, value)
	}
	out[key] = value
	return nil
}

// escapeChartSegment doubles every "." in a single nested-map key segment
// to "\." so Helm's strvals parser keeps the segment as one key.
func escapeChartSegment(segment string) string {
	if !strings.Contains(segment, ".") {
		return segment
	}
	return strings.ReplaceAll(segment, ".", `\.`)
}

// init registers the SchedulerK3sChartTask with the task registry
func init() {
	RegisterTask(&SchedulerK3sChartTask{})
}
