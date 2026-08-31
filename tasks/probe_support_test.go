package tasks

import (
	"context"
	"testing"
)

// probelessTask is a Task that does not implement ProbeDocer, standing in for a
// task written before ProbeSupport() existed. TaskProbeSupport must report it
// as undeclared rather than inventing a status, which is what lets the docs
// generator omit the section and the coverage test name the offender.
type probelessTask struct{}

func (t probelessTask) Doc() string                                 { return "" }
func (t probelessTask) Examples() ([]Doc, error)                    { return nil, nil }
func (t probelessTask) Plan(ctx context.Context) PlanResult         { return PlanResult{} }
func (t probelessTask) Execute(ctx context.Context) TaskOutputState { return TaskOutputState{} }

func TestTaskProbeSupportUndeclared(t *testing.T) {
	support, ok := TaskProbeSupport(probelessTask{})
	if ok {
		t.Errorf("expected ok=false for a task without ProbeSupport(), got %+v", support)
	}
	if support != (ProbeSupport{}) {
		t.Errorf("expected zero ProbeSupport for an undeclared task, got %+v", support)
	}
}

func TestTaskProbeSupportSupported(t *testing.T) {
	support, ok := TaskProbeSupport(&AppTask{})
	if !ok {
		t.Fatal("expected AppTask to declare ProbeSupport()")
	}
	if support.Status != ProbeSupported {
		t.Errorf("expected AppTask to be %q, got %q", ProbeSupported, support.Status)
	}
	if support.Caveat != "" {
		t.Errorf("expected no caveat on a fully probed task, got %q", support.Caveat)
	}
}

func TestTaskProbeSupportUnsupportedCarriesCaveat(t *testing.T) {
	support, ok := TaskProbeSupport(&GitAuthTask{})
	if !ok {
		t.Fatal("expected GitAuthTask to declare ProbeSupport()")
	}
	if support.Status != ProbeUnsupported {
		t.Errorf("expected GitAuthTask to be %q, got %q", ProbeUnsupported, support.Status)
	}
	if !contains(support.Caveat, "no read command") {
		t.Errorf("expected the caveat to explain the missing read command, got %q", support.Caveat)
	}
}

func TestTaskProbeSupportPartialCarriesCaveat(t *testing.T) {
	support, ok := TaskProbeSupport(&ServiceBackupTask{})
	if !ok {
		t.Fatal("expected ServiceBackupTask to declare ProbeSupport()")
	}
	if support.Status != ProbePartial {
		t.Errorf("expected ServiceBackupTask to be %q, got %q", ProbePartial, support.Status)
	}
	if !contains(support.Caveat, "encryption passphrase") {
		t.Errorf("expected the caveat to name the unprobed fields, got %q", support.Caveat)
	}
}
