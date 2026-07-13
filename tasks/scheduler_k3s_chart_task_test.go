package tasks

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestSchedulerK3sChartTaskInvalidState(t *testing.T) {
	task := SchedulerK3sChartTask{
		Chart:  "ingress-nginx",
		Values: map[string]any{"replicaCount": "3"},
		State:  "invalid",
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestSchedulerK3sChartTaskMissingChart(t *testing.T) {
	task := SchedulerK3sChartTask{
		Values: map[string]any{"replicaCount": "3"},
		State:  StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when chart is empty")
	}
	if !strings.Contains(result.Error.Error(), "chart is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sChartTaskPresentWithoutValues(t *testing.T) {
	task := SchedulerK3sChartTask{
		Chart: "ingress-nginx",
		State: StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when present state has no values")
	}
	if !strings.Contains(result.Error.Error(), "'values' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sChartTaskAbsentWithoutValues(t *testing.T) {
	task := SchedulerK3sChartTask{
		Chart: "ingress-nginx",
		State: StateAbsent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when absent state has no values")
	}
	if !strings.Contains(result.Error.Error(), "'values' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sChartTaskPresentEmptyValueRejected(t *testing.T) {
	task := SchedulerK3sChartTask{
		Chart:  "ingress-nginx",
		Values: map[string]any{"controller.replicaCount": ""},
		State:  StatePresent,
	}
	err := task.Validate()
	if err == nil {
		t.Fatal("expected error when a present-state chart value is empty")
	}
	if !strings.Contains(err.Error(), "must not be empty for state 'present'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSchedulerK3sChartTaskAbsentEmptyValueAllowed(t *testing.T) {
	task := SchedulerK3sChartTask{
		Chart:  "ingress-nginx",
		Values: map[string]any{"controller.replicaCount": ""},
		State:  StateAbsent,
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("absent-state empty value should be allowed (clears the value), got %v", err)
	}
}

func TestSchedulerK3sChartTaskRejectsListValue(t *testing.T) {
	task := SchedulerK3sChartTask{
		Chart:  "ingress-nginx",
		Values: map[string]any{"hosts": []any{"a", "b"}},
		State:  StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when value is a list")
	}
	if !strings.Contains(result.Error.Error(), "lists are not supported") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestFlattenChartValuesScalarCoercion(t *testing.T) {
	in := map[string]any{
		"replicaCount":   3,
		"resources.cpu":  "200m",
		"installCRDs":    false,
		"timeout":        30.5,
		"explicitString": "hello",
	}
	out, err := flattenChartValues(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		"replicaCount":   "3",
		"resources.cpu":  "200m",
		"installCRDs":    "false",
		"timeout":        "30.5",
		"explicitString": "hello",
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("flattenChartValues mismatch\n got: %v\nwant: %v", out, want)
	}
}

func TestFlattenChartValuesNestedMap(t *testing.T) {
	in := map[string]any{
		"resources": map[string]any{
			"limits": map[string]any{
				"cpu":    "200m",
				"memory": "256Mi",
			},
			"requests": map[string]any{
				"cpu": "100m",
			},
		},
	}
	out, err := flattenChartValues(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		"resources.limits.cpu":    "200m",
		"resources.limits.memory": "256Mi",
		"resources.requests.cpu":  "100m",
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("flatten mismatch\n got: %v\nwant: %v", out, want)
	}
}

func TestFlattenChartValuesDottedLeafEscapes(t *testing.T) {
	in := map[string]any{
		"service": map[string]any{
			"annotations": map[string]any{
				"prometheus.io/scrape": "true",
				"prometheus.io/port":   "9090",
			},
		},
	}
	out, err := flattenChartValues(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		`service.annotations.prometheus\.io/scrape`: "true",
		`service.annotations.prometheus\.io/port`:   "9090",
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("escape mismatch\n got: %v\nwant: %v", out, want)
	}
}

func TestFlattenChartValuesTopLevelDottedPassthrough(t *testing.T) {
	in := map[string]any{
		"resources.limits.cpu":    "200m",
		"resources.limits.memory": "256Mi",
	}
	out, err := flattenChartValues(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		"resources.limits.cpu":    "200m",
		"resources.limits.memory": "256Mi",
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("passthrough mismatch\n got: %v\nwant: %v", out, want)
	}
}

func TestFlattenChartValuesMixedTopDotAndNestedEscape(t *testing.T) {
	in := map[string]any{
		"service.annotations": map[string]any{
			"foo.bar": "baz",
		},
		"ports.web.redirectTo.port": "websecure",
	}
	out, err := flattenChartValues(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		`service.annotations.foo\.bar`: "baz",
		"ports.web.redirectTo.port":    "websecure",
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("mixed mismatch\n got: %v\nwant: %v", out, want)
	}
}

func TestFlattenChartValuesDuplicateKeyConflict(t *testing.T) {
	in := map[string]any{
		"resources.limits.cpu": "200m",
		"resources": map[string]any{
			"limits": map[string]any{
				"cpu": "500m",
			},
		},
	}
	_, err := flattenChartValues(in)
	if err == nil {
		t.Fatal("expected duplicate-key error")
	}
	if !strings.Contains(err.Error(), "duplicate chart value key") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFlattenChartValuesIdenticalDuplicateAllowed(t *testing.T) {
	// Two paths producing the same key with the same value is harmless;
	// the helper should fold them rather than rejecting.
	in := map[string]any{
		"resources.limits.cpu": "200m",
		"resources": map[string]any{
			"limits": map[string]any{
				"cpu": "200m",
			},
		},
	}
	out, err := flattenChartValues(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["resources.limits.cpu"] != "200m" {
		t.Errorf("expected resources.limits.cpu=200m, got %v", out)
	}
}

func TestFlattenChartValuesNilLeafBecomesEmptyString(t *testing.T) {
	in := map[string]any{"replicaCount": nil}
	out, err := flattenChartValues(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, ok := out["replicaCount"]; !ok || got != "" {
		t.Errorf("expected replicaCount=\"\", got %v (ok=%v)", got, ok)
	}
}

func TestFlattenChartValuesLegacyMapAnyAny(t *testing.T) {
	// yaml.v2 unmarshals nested mappings into map[any]any; verify the
	// converter handles it.
	in := map[string]any{
		"resources": map[any]any{
			"limits": map[any]any{
				"cpu": "200m",
			},
		},
	}
	out, err := flattenChartValues(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["resources.limits.cpu"] != "200m" {
		t.Errorf("expected resources.limits.cpu=200m, got %v", out)
	}
}

func TestFlattenChartValuesEmptyNestedMapRejected(t *testing.T) {
	in := map[string]any{"resources": map[string]any{}}
	_, err := flattenChartValues(in)
	if err == nil {
		t.Fatal("expected error for empty nested map")
	}
}

func TestFlattenChartValuesEmptyNestedKeyRejected(t *testing.T) {
	in := map[string]any{"resources": map[string]any{"": "x"}}
	_, err := flattenChartValues(in)
	if err == nil {
		t.Fatal("expected error for empty nested map key")
	}
}

func TestSchedulerK3sChartTaskCommandShapeSet(t *testing.T) {
	// Use the plan helpers directly to verify the per-key command shape
	// without standing up a fake dokku.
	current := map[string]string{}
	desired := map[string]string{"replicaCount": "3"}
	chart := "ingress-nginx"
	commandFn := schedulerK3sChartsCommandFor(t, chart)
	plan := planPairsSet("chart value", desired,
		func() (map[string]string, error) { return current, nil },
		commandFn,
	)
	if len(plan.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(plan.Commands))
	}
	want := "dokku --quiet scheduler-k3s:charts:set ingress-nginx.replicaCount 3"
	if plan.Commands[0] != want {
		t.Errorf("command mismatch\n got: %q\nwant: %q", plan.Commands[0], want)
	}
}

func TestSchedulerK3sChartTaskCommandShapeClear(t *testing.T) {
	current := map[string]string{"replicaCount": "3"}
	desired := map[string]string{"replicaCount": "ignored"}
	chart := "ingress-nginx"
	commandFn := schedulerK3sChartsCommandFor(t, chart)
	plan := planPairsUnset("chart value", desired,
		func() (map[string]string, error) { return current, nil },
		commandFn,
	)
	if len(plan.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(plan.Commands))
	}
	// Empty trailing value indicates a clear. The masked command string
	// should still carry the empty arg as a quoted empty token.
	if !strings.Contains(plan.Commands[0], "scheduler-k3s:charts:set ingress-nginx.replicaCount") {
		t.Errorf("unexpected command: %q", plan.Commands[0])
	}
}

// schedulerK3sChartsCommandFor returns the per-key command builder a
// real SchedulerK3sChartTask uses; mirrors the closure inside Plan() so
// the command-shape tests assert against the exact production command.
func schedulerK3sChartsCommandFor(t *testing.T, chart string) pairsCommandFunc {
	t.Helper()
	return func(key, value string) subprocess.ExecCommandInput {
		return subprocess.ExecCommandInput{
			Command: "dokku",
			Args: []string{
				"--quiet",
				"scheduler-k3s:charts:set",
				chart + "." + key,
				value,
			},
		}
	}
}
