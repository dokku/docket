package tasks

import (
	"errors"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func chartCommandStub(key, value string) subprocess.ExecCommandInput {
	return subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "scheduler-k3s:charts:set", "demo." + key, value},
	}
}

func staticCurrent(pairs map[string]string) pairsCurrentFunc {
	return func() (map[string]string, error) { return pairs, nil }
}

func TestPlanPairsSetInSync(t *testing.T) {
	desired := map[string]string{"foo": "1", "bar": "2"}
	current := map[string]string{"foo": "1", "bar": "2", "extra": "z"}
	plan := planPairsSet("chart value", desired, staticCurrent(current), chartCommandStub)
	if !plan.InSync {
		t.Fatalf("expected InSync, got %#v", plan)
	}
	if plan.Status != PlanStatusOK {
		t.Errorf("expected PlanStatusOK, got %v", plan.Status)
	}
}

func TestPlanPairsSetAllNewYieldsCreate(t *testing.T) {
	desired := map[string]string{"foo": "1", "bar": "2"}
	plan := planPairsSet("chart value", desired, staticCurrent(map[string]string{}), chartCommandStub)
	if plan.InSync {
		t.Fatal("expected drift")
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("expected PlanStatusCreate, got %v", plan.Status)
	}
	if !strings.Contains(plan.Reason, "2 chart value(s) to set") {
		t.Errorf("unexpected reason: %q", plan.Reason)
	}
	if len(plan.Mutations) != 2 {
		t.Errorf("expected 2 mutations, got %d", len(plan.Mutations))
	}
	if len(plan.Commands) != 2 {
		t.Errorf("expected 2 commands, got %d", len(plan.Commands))
	}
}

func TestPlanPairsSetPartialDriftYieldsModify(t *testing.T) {
	desired := map[string]string{"foo": "1", "bar": "2"}
	current := map[string]string{"foo": "old"}
	plan := planPairsSet("chart value", desired, staticCurrent(current), chartCommandStub)
	if plan.Status != PlanStatusModify {
		t.Errorf("expected PlanStatusModify, got %v", plan.Status)
	}
	if len(plan.Mutations) != 2 {
		t.Fatalf("expected 2 mutations, got %d", len(plan.Mutations))
	}
}

func TestPlanPairsSetProbeError(t *testing.T) {
	probeErr := errors.New("boom")
	fn := func() (map[string]string, error) { return nil, probeErr }
	plan := planPairsSet("chart value", map[string]string{"k": "v"}, fn, chartCommandStub)
	if plan.Error != probeErr {
		t.Fatalf("expected probe error to propagate, got %v", plan.Error)
	}
	if plan.Status != PlanStatusError {
		t.Errorf("expected PlanStatusError, got %v", plan.Status)
	}
}

func TestPlanPairsUnsetSkipsMissingKeys(t *testing.T) {
	desired := map[string]string{"foo": "", "missing": ""}
	current := map[string]string{"foo": "1", "other": "2"}
	plan := planPairsUnset("chart value", desired, staticCurrent(current), chartCommandStub)
	if plan.InSync {
		t.Fatal("expected drift (one key still exists)")
	}
	if plan.Status != PlanStatusDestroy {
		t.Errorf("expected PlanStatusDestroy, got %v", plan.Status)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly 1 clear command (the present key), got %d", len(plan.Commands))
	}
	if !strings.Contains(plan.Commands[0], "demo.foo") {
		t.Errorf("expected command to target 'demo.foo', got %q", plan.Commands[0])
	}
}

func TestPlanPairsUnsetInSyncWhenNothingMatches(t *testing.T) {
	desired := map[string]string{"foo": ""}
	current := map[string]string{"bar": "1"}
	plan := planPairsUnset("chart value", desired, staticCurrent(current), chartCommandStub)
	if !plan.InSync {
		t.Fatalf("expected InSync when no desired keys exist on the server, got %#v", plan)
	}
}
