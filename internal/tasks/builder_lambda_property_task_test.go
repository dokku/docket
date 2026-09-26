package tasks

import (
	"strings"
	"testing"
)

func TestBuilderLambdaPropertyTaskInvalidState(t *testing.T) {
	task := BuilderLambdaPropertyTask{App: "test-app", Property: "lambdayml-path", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestBuilderLambdaPropertyTaskMissingApp(t *testing.T) {
	task := BuilderLambdaPropertyTask{Property: "lambdayml-path", Value: "lambda.yml", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestBuilderLambdaPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := BuilderLambdaPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "lambdayml-path",
		Value:    "lambda.yml",
		State:    StatePresent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when both global and app are set")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when 'global' is set to true") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestBuilderLambdaPropertyTaskPresentWithoutValue(t *testing.T) {
	task := BuilderLambdaPropertyTask{
		App:      "test-app",
		Property: "lambdayml-path",
		Value:    "",
		State:    StatePresent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when present state has no value")
	}
	if !strings.Contains(result.Error.Error(), "invalid without a value") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestBuilderLambdaPropertyTaskAbsentWithValue(t *testing.T) {
	task := BuilderLambdaPropertyTask{
		App:      "test-app",
		Property: "lambdayml-path",
		Value:    "lambda.yml",
		State:    StateAbsent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when absent state has a value")
	}
	if !strings.Contains(result.Error.Error(), "invalid with a value") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}
