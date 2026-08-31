package tasks

import (
	"testing"
)

func TestAppTaskInvalidState(t *testing.T) {
	task := AppTask{App: "test-app", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}
