package tasks

import (
	"testing"
)

func TestMaintenanceTaskInvalidState(t *testing.T) {
	task := MaintenanceTask{App: "test-app", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}
