package tasks

import (
	"strings"
	"testing"
)

func TestProxyPropertyTaskInvalidState(t *testing.T) {
	task := ProxyPropertyTask{App: "test-app", Property: "type", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestProxyPropertyTaskMissingApp(t *testing.T) {
	task := ProxyPropertyTask{Property: "type", Value: "nginx", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestProxyPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := ProxyPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "type",
		Value:    "nginx",
		State:    StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when both global and app are set")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when 'global' is set to true") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestProxyPropertyTaskPresentWithoutValue(t *testing.T) {
	task := ProxyPropertyTask{
		App:      "test-app",
		Property: "type",
		Value:    "",
		State:    StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when present state has no value")
	}
	if !strings.Contains(result.Error.Error(), "invalid without a value") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestProxyPropertyTaskAbsentWithValue(t *testing.T) {
	task := ProxyPropertyTask{
		App:      "test-app",
		Property: "type",
		Value:    "nginx",
		State:    StateAbsent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when absent state has a value")
	}
	if !strings.Contains(result.Error.Error(), "invalid with a value") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}
