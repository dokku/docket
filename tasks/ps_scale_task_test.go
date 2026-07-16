package tasks

import (
	"testing"

	_ "github.com/gliderlabs/sigil/builtin"
)

func TestPsScaleTaskInvalidState(t *testing.T) {
	task := PsScaleTask{App: "test-app", Scale: map[string]int{"web": 1}, State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestPsScaleTaskEmptyScale(t *testing.T) {
	task := PsScaleTask{App: "test-app", Scale: map[string]int{}, State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with empty scale and state=present should return an error")
	}
}

func TestPsScaleTaskNilScale(t *testing.T) {
	task := PsScaleTask{App: "test-app", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with nil scale and state=present should return an error")
	}
}

func TestGetTasksPsScaleTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: scale processes
      dokku_ps_scale:
        app: test-app
        scale:
          web: 2
          worker: 1
        skip_deploy: true
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("scale processes")
	if task == nil {
		t.Fatal("task 'scale processes' not found")
	}

	psTask, ok := task.(*PsScaleTask)
	if !ok {
		pt, ok2 := task.(PsScaleTask)
		if !ok2 {
			t.Fatalf("task is not a PsScaleTask (type is %T)", task)
		}
		psTask = &pt
	}

	if psTask.App != "test-app" {
		t.Errorf("App = %q, want %q", psTask.App, "test-app")
	}
	if len(psTask.Scale) != 2 {
		t.Fatalf("expected 2 scale entries, got %d", len(psTask.Scale))
	}
	if psTask.Scale["web"] != 2 {
		t.Errorf("Scale[web] = %d, want %d", psTask.Scale["web"], 2)
	}
	if psTask.Scale["worker"] != 1 {
		t.Errorf("Scale[worker] = %d, want %d", psTask.Scale["worker"], 1)
	}
	// SkipDeploy is a *bool, so an explicit skip_deploy: true survives decoding.
	if psTask.SkipDeploy == nil || !*psTask.SkipDeploy {
		t.Error("SkipDeploy = false, want true (YAML value should be preserved)")
	}
}
