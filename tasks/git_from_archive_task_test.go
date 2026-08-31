package tasks

import (
	"strings"
	"testing"
)

func TestGitFromArchiveTaskInvalidState(t *testing.T) {
	task := GitFromArchiveTask{App: "test-app", ArchiveURL: "https://example.com/a.tar", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestGitFromArchiveTaskMissingApp(t *testing.T) {
	task := GitFromArchiveTask{ArchiveURL: "https://example.com/a.tar", State: "deployed"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGitFromArchiveTaskMissingArchiveURL(t *testing.T) {
	task := GitFromArchiveTask{App: "test-app", State: "deployed"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without archive_url should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'archive_url' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGitFromArchiveTaskInvalidArchiveType(t *testing.T) {
	task := GitFromArchiveTask{
		App:         "test-app",
		ArchiveURL:  "https://example.com/a.bz2",
		ArchiveType: "bz2",
		State:       "deployed",
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error for invalid archive_type")
	}
	if !strings.Contains(result.Error.Error(), "'archive_type' must be one of") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGitFromArchiveTaskOnlyGitUsername(t *testing.T) {
	task := GitFromArchiveTask{
		App:         "test-app",
		ArchiveURL:  "https://example.com/a.tar",
		GitUsername: "deploy-bot",
		State:       "deployed",
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when git_username is set without git_email")
	}
	if !strings.Contains(result.Error.Error(), "'git_username' and 'git_email' must be set together") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGitFromArchiveTaskOnlyGitEmail(t *testing.T) {
	task := GitFromArchiveTask{
		App:        "test-app",
		ArchiveURL: "https://example.com/a.tar",
		GitEmail:   "deploy@example.com",
		State:      "deployed",
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when git_email is set without git_username")
	}
	if !strings.Contains(result.Error.Error(), "'git_username' and 'git_email' must be set together") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGetTasksGitFromArchiveTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy archive
      dokku_git_from_archive:
        app: test-app
        archive_url: https://example.com/release.tar.gz
        archive_type: tar.gz
        git_username: deploy-bot
        git_email: deploy@example.com
        state: deployed
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("deploy archive")
	if task == nil {
		t.Fatal("task 'deploy archive' not found")
	}

	archTask, ok := task.(*GitFromArchiveTask)
	if !ok {
		t.Fatalf("task is not a GitFromArchiveTask (type is %T)", task)
	}
	if archTask.App != "test-app" {
		t.Errorf("App = %q, want %q", archTask.App, "test-app")
	}
	if archTask.ArchiveURL != "https://example.com/release.tar.gz" {
		t.Errorf("ArchiveURL = %q", archTask.ArchiveURL)
	}
	if archTask.ArchiveType != "tar.gz" {
		t.Errorf("ArchiveType = %q, want %q", archTask.ArchiveType, "tar.gz")
	}
	if archTask.GitUsername != "deploy-bot" {
		t.Errorf("GitUsername = %q", archTask.GitUsername)
	}
	if archTask.GitEmail != "deploy@example.com" {
		t.Errorf("GitEmail = %q", archTask.GitEmail)
	}
}
