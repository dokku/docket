package tasks

import (
	"context"
	"testing"
)

// undocumentedTask is a Task that implements only Plan and Execute. That it
// satisfies Task at all is the point: documentation is not part of running a
// task. TaskSynopsis and TaskExamples must report it as undocumented rather
// than failing, and the catalog must still describe it.
type undocumentedTask struct{}

func (t undocumentedTask) Plan(ctx context.Context) PlanResult         { return PlanResult{} }
func (t undocumentedTask) Execute(ctx context.Context) TaskOutputState { return TaskOutputState{} }

func TestTaskDocumentationUndeclared(t *testing.T) {
	if synopsis := TaskSynopsis(undocumentedTask{}); synopsis != "" {
		t.Errorf("expected an empty synopsis for an undocumented task, got %q", synopsis)
	}

	examples, err := TaskExamples(undocumentedTask{})
	if err != nil {
		t.Errorf("expected no error for an undocumented task, got %v", err)
	}
	if examples != nil {
		t.Errorf("expected no examples for an undocumented task, got %+v", examples)
	}

	schema, err := TaskSchemaOf("dokku_undocumented", undocumentedTask{})
	if err != nil {
		t.Fatalf("TaskSchemaOf returned error: %v", err)
	}
	if schema.Synopsis != "" {
		t.Errorf("expected an empty catalog synopsis, got %q", schema.Synopsis)
	}
	if len(schema.Examples) != 0 {
		t.Errorf("expected no catalog examples, got %+v", schema.Examples)
	}
}

func TestTaskDocumentationDeclared(t *testing.T) {
	task := &AppTask{}
	if synopsis := TaskSynopsis(task); synopsis != task.Doc() {
		t.Errorf("TaskSynopsis = %q, want %q", synopsis, task.Doc())
	}

	examples, err := TaskExamples(task)
	if err != nil {
		t.Fatalf("TaskExamples returned error: %v", err)
	}
	want, err := task.Examples()
	if err != nil {
		t.Fatalf("Examples returned error: %v", err)
	}
	if len(examples) == 0 || len(examples) != len(want) {
		t.Errorf("TaskExamples returned %d examples, want %d", len(examples), len(want))
	}
}
