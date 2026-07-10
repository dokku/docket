package tasks

import (
	"strings"
	"testing"
)

func TestDockerOptionsTaskInvalidState(t *testing.T) {
	task := DockerOptionsTask{App: "test-app", Phase: "deploy", Option: "-v /a:/a", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestDockerOptionsTaskMissingApp(t *testing.T) {
	task := DockerOptionsTask{Phase: "deploy", Option: "-v /a:/a", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestDockerOptionsTaskInvalidPhase(t *testing.T) {
	for _, phase := range []string{"", "start", "any"} {
		task := DockerOptionsTask{App: "test-app", Phase: phase, Option: "-v /a:/a", State: StatePresent}
		result := task.Execute()
		if result.Error == nil {
			t.Fatalf("Execute with invalid phase %q should return an error", phase)
		}
		if !strings.Contains(result.Error.Error(), "'phase' must be one of") {
			t.Errorf("phase=%q: unexpected error: %v", phase, result.Error)
		}
	}
}

func TestDockerOptionsTaskMissingOption(t *testing.T) {
	task := DockerOptionsTask{App: "test-app", Phase: "deploy", Option: "  ", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without option should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'option' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestDockerOptionsTaskProcessTypeRejectsDefaultSentinel(t *testing.T) {
	task := DockerOptionsTask{
		App:         "test-app",
		Phase:       "deploy",
		ProcessType: "_default_",
		Option:      "-v /a:/a",
		State:       StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with process_type='_default_' should return an error")
	}
	if !strings.Contains(result.Error.Error(), "reserved sentinel") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestDockerOptionsTaskProcessTypeRejectsNonDeployPhase(t *testing.T) {
	for _, phase := range []string{"build", "run"} {
		task := DockerOptionsTask{
			App:         "test-app",
			Phase:       phase,
			ProcessType: "web",
			Option:      "-v /a:/a",
			State:       StatePresent,
		}
		result := task.Execute()
		if result.Error == nil {
			t.Fatalf("Execute with process_type set and phase=%q should return an error", phase)
		}
		if !strings.Contains(result.Error.Error(), "only supported for the deploy phase") {
			t.Errorf("phase=%q: unexpected error: %v", phase, result.Error)
		}
	}
}

func TestDockerOptionsCommandArgs(t *testing.T) {
	tests := []struct {
		name string
		task DockerOptionsTask
		want []string
	}{
		{
			name: "default scope",
			task: DockerOptionsTask{App: "app", Phase: "deploy", Option: "-v /a:/a"},
			want: []string{"--quiet", "docker-options:add", "app", "deploy", "-v /a:/a"},
		},
		{
			name: "with process type",
			task: DockerOptionsTask{App: "app", Phase: "deploy", ProcessType: "web", Option: "--memory=512m"},
			want: []string{"--quiet", "docker-options:add", "--process", "web", "app", "deploy", "--memory=512m"},
		},
	}
	for _, tt := range tests {
		got := dockerOptionsCommandArgs("docker-options:add", tt.task)
		if len(got) != len(tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%s: arg %d got %q, want %q", tt.name, i, got[i], tt.want[i])
			}
		}
	}
}

func TestParseDockerOptionsPayload(t *testing.T) {
	payload := map[string]interface{}{
		// Space-joined shorthand keys are ignored in favor of the structured
		// -list companions below.
		"build":                     "",
		"deploy":                    "-v /a:/a --label com.example.description=my app",
		"run":                       "",
		"deploy.web":                "--memory=512m",
		"deploy.worker":             "--cpus=1",
		"docker-options-build":      "",
		"docker-options-deploy":     "-v /a:/a",
		"docker-options-deploy.web": "--memory=512m",
		"some-unrelated-key":        "ignored",
		// dokku 0.38.22+ structured-list companions (dokku/dokku#8799): one
		// array element per stored option, consumed as discrete entries. A
		// value with a space stays a single element (no whitespace splitting).
		"build-list":         []interface{}{},
		"deploy-list":        []interface{}{"-v /a:/a", "--label 'com.example.description=my app'"},
		"run-list":           []interface{}{},
		"deploy.web-list":    []interface{}{"--memory=512m"},
		"deploy.worker-list": []interface{}{"--cpus=1"},
	}
	got := parseDockerOptionsPayload(payload)

	want := map[dockerOptionsScope][]string{
		{Phase: "build"}:                         {},
		{Phase: "deploy"}:                        {"-v /a:/a", "--label 'com.example.description=my app'"},
		{Phase: "run"}:                           {},
		{Phase: "deploy", ProcessType: "web"}:    {"--memory=512m"},
		{Phase: "deploy", ProcessType: "worker"}: {"--cpus=1"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d (got: %#v)", len(got), len(want), got)
	}
	for scope, expected := range want {
		gotOptions, ok := got[scope]
		if !ok {
			t.Errorf("scope %+v: missing from result", scope)
			continue
		}
		if len(gotOptions) != len(expected) {
			t.Errorf("scope %+v: got %#v, want %#v", scope, gotOptions, expected)
			continue
		}
		for i := range expected {
			if gotOptions[i] != expected[i] {
				t.Errorf("scope %+v: element %d got %q, want %q", scope, i, gotOptions[i], expected[i])
			}
		}
	}
}

func TestSplitDockerOptionsKey(t *testing.T) {
	tests := []struct {
		key             string
		wantPhase       string
		wantProcessType string
		wantOk          bool
	}{
		{"deploy", "deploy", "", true},
		{"build", "build", "", true},
		{"run", "run", "", true},
		{"deploy.web", "deploy", "web", true},
		{"deploy.web-worker", "deploy", "web-worker", true},
		{"unknown", "", "", false},
		{"deploy.web.extra", "deploy", "web.extra", true},
	}
	for _, tt := range tests {
		phase, processType, ok := splitDockerOptionsKey(tt.key)
		if ok != tt.wantOk || phase != tt.wantPhase || processType != tt.wantProcessType {
			t.Errorf("splitDockerOptionsKey(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.key, phase, processType, ok, tt.wantPhase, tt.wantProcessType, tt.wantOk)
		}
	}
}

func TestOptionPresent(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		option   string
		want     bool
	}{
		{"empty list", nil, "-v /a:/a", false},
		{"single match", []string{"-v /a:/a"}, "-v /a:/a", true},
		{"match second entry", []string{"-v /a:/a", "-p 80:80"}, "-p 80:80", true},
		{"match first entry", []string{"-v /a:/a", "-p 80:80"}, "-v /a:/a", true},
		{"no substring match", []string{"-v /a:/aa"}, "-v /a:/a", false},
		{"distinct entries", []string{"-p 8080:80"}, "-p 80:80", false},
		// A discrete entry whose value contains a space matches only as a whole,
		// never by any single whitespace-separated token.
		{"space value whole match", []string{"--label 'com.example.description=my app'"}, "--label 'com.example.description=my app'", true},
		{"space value token no match", []string{"--label 'com.example.description=my app'"}, "--label", false},
	}
	for _, tt := range tests {
		got := optionPresent(tt.existing, tt.option)
		if got != tt.want {
			t.Errorf("%s: optionPresent(%#v, %q) = %v, want %v", tt.name, tt.existing, tt.option, got, tt.want)
		}
	}
}

func TestGetTasksDockerOptionsTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: add docker option
      dokku_docker_options:
        app: test-app
        phase: deploy
        option: "-v /var/run/docker.sock:/var/run/docker.sock"
        state: present
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("add docker option")
	if task == nil {
		t.Fatal("task 'add docker option' not found")
	}

	doTask, ok := task.(*DockerOptionsTask)
	if !ok {
		t.Fatalf("task is not a DockerOptionsTask (type is %T)", task)
	}
	if doTask.App != "test-app" {
		t.Errorf("App = %q, want %q", doTask.App, "test-app")
	}
	if doTask.Phase != "deploy" {
		t.Errorf("Phase = %q, want %q", doTask.Phase, "deploy")
	}
	if doTask.Option != "-v /var/run/docker.sock:/var/run/docker.sock" {
		t.Errorf("Option = %q, want %q", doTask.Option, "-v /var/run/docker.sock:/var/run/docker.sock")
	}
	if doTask.State != StatePresent {
		t.Errorf("State = %q, want %q", doTask.State, StatePresent)
	}
}

func TestGetTasksDockerOptionsTaskParsesProcessType(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: add web option
      dokku_docker_options:
        app: test-app
        phase: deploy
        process_type: web
        option: "--memory=512m"
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("add web option")
	if task == nil {
		t.Fatal("task 'add web option' not found")
	}

	doTask, ok := task.(*DockerOptionsTask)
	if !ok {
		t.Fatalf("task is not a DockerOptionsTask (type is %T)", task)
	}
	if doTask.ProcessType != "web" {
		t.Errorf("ProcessType = %q, want %q", doTask.ProcessType, "web")
	}
	if doTask.Option != "--memory=512m" {
		t.Errorf("Option = %q, want %q", doTask.Option, "--memory=512m")
	}
}
