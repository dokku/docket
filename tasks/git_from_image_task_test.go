package tasks

import (
	"strings"
	"testing"
)

func TestGitFromImageTaskInvalidState(t *testing.T) {
	task := GitFromImageTask{App: "test-app", Image: "nginx", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestGitFromImageTaskValidate(t *testing.T) {
	tests := []struct {
		name    string
		task    GitFromImageTask
		wantErr string
	}{
		{
			name: "app and image only is valid",
			task: GitFromImageTask{App: "web", Image: "org/app:1.0"},
		},
		{
			name: "username and email together is valid",
			task: GitFromImageTask{App: "web", Image: "org/app:1.0", GitUsername: "deploy", GitEmail: "deploy@example.com"},
		},
		{
			name:    "email without username is rejected",
			task:    GitFromImageTask{App: "web", Image: "org/app:1.0", GitEmail: "deploy@example.com"},
			wantErr: "'git_username' and 'git_email' must be set together",
		},
		{
			name:    "username without email is rejected",
			task:    GitFromImageTask{App: "web", Image: "org/app:1.0", GitUsername: "deploy"},
			wantErr: "'git_username' and 'git_email' must be set together",
		},
		{
			name:    "missing image is rejected",
			task:    GitFromImageTask{App: "web"},
			wantErr: "'image' is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.task.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestGitFromImageTaskNonDeployedStates(t *testing.T) {
	tests := []struct {
		name  string
		state State
	}{
		{"present state", StatePresent},
		{"absent state", StateAbsent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := GitFromImageTask{App: "test-app", Image: "nginx", State: tt.state}
			result := task.Execute()
			if result.Error == nil {
				t.Fatal("expected error for non-deployed state")
			}
			if !strings.Contains(result.Error.Error(), "invalid state") {
				t.Errorf("expected 'invalid state' error, got: %v", result.Error)
			}
		})
	}
}
