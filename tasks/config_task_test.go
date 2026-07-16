package tasks

import (
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
	_ "github.com/gliderlabs/sigil/builtin"
)

func TestConfigTaskInvalidState(t *testing.T) {
	task := ConfigTask{
		App:    "test-app",
		Config: map[string]string{"KEY": "VALUE"},
		State:  "invalid",
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestGetTasksConfigTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: set config
      dokku_config:
        app: test-app
        restart: false
        config:
          KEY1: val1
          KEY2: val2
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("set config")
	if task == nil {
		t.Fatal("task 'set config' not found")
	}

	configTask, ok := task.(*ConfigTask)
	if !ok {
		// tasks may be stored as value types depending on reflection
		ct, ok2 := task.(ConfigTask)
		if !ok2 {
			t.Fatalf("task is not a ConfigTask (type is %T)", task)
		}
		configTask = &ct
	}

	if configTask.App != "test-app" {
		t.Errorf("App = %q, want %q", configTask.App, "test-app")
	}
	// Restart is a *bool, so an explicit restart: false survives decoding: it is
	// no longer clobbered back to true by defaults.SetDefaults (go-defaults
	// leaves pointer fields untouched).
	if configTask.Restart == nil {
		t.Fatal("Restart is nil, want an explicit false")
	}
	if *configTask.Restart {
		t.Error("Restart = true, want false (explicit restart: false must be preserved)")
	}
	if len(configTask.Config) != 2 {
		t.Fatalf("expected 2 config keys, got %d", len(configTask.Config))
	}
	if configTask.Config["KEY1"] != "val1" {
		t.Errorf("Config[KEY1] = %q, want %q", configTask.Config["KEY1"], "val1")
	}
	if configTask.Config["KEY2"] != "val2" {
		t.Errorf("Config[KEY2] = %q, want %q", configTask.Config["KEY2"], "val2")
	}
}

// TestConfigTaskRestartFlag pins the --no-restart flag to the Restart pointer
// across both config:set and config:unset. An explicit restart: false must emit
// --no-restart; an omitted restart (nil) defaults to true and must not.
func TestConfigTaskRestartFlag(t *testing.T) {
	cases := []struct {
		name         string
		restart      *bool
		state        State
		current      string // stdout of the config:export probe
		wantVerb     string
		wantNoRestrt bool
	}{
		{"set explicit false emits --no-restart", boolPtr(false), StatePresent, "{}", "config:set", true},
		{"set omitted defaults to restart", nil, StatePresent, "{}", "config:set", false},
		{"set explicit true keeps restart", boolPtr(true), StatePresent, "{}", "config:set", false},
		{"unset explicit false emits --no-restart", boolPtr(false), StateAbsent, `{"KEY":"val"}`, "config:unset", true},
		{"unset omitted defaults to restart", nil, StateAbsent, `{"KEY":"val"}`, "config:unset", false},
		{"unset explicit true keeps restart", boolPtr(true), StateAbsent, `{"KEY":"val"}`, "config:unset", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer subprocess.SetExecRunner(fakeDokku(map[string]string{
				"--quiet config:export --format json test-app": tc.current,
			}))()

			plan := ConfigTask{
				App:     "test-app",
				Restart: tc.restart,
				Config:  map[string]string{"KEY": "val"},
				State:   tc.state,
			}.Plan()
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if len(plan.Commands) == 0 {
				t.Fatalf("expected a command, got none (plan=%#v)", plan)
			}
			cmd := plan.Commands[0]
			if !strings.Contains(cmd, tc.wantVerb) {
				t.Errorf("command %q does not contain %q", cmd, tc.wantVerb)
			}
			if got := strings.Contains(cmd, "--no-restart"); got != tc.wantNoRestrt {
				t.Errorf("command %q: --no-restart present = %v, want %v", cmd, got, tc.wantNoRestrt)
			}
		})
	}
}

// TestConfigTaskRestartFalseEndToEnd exercises the full parse + SetDefaults
// pipeline: a recipe with restart: false must still yield a --no-restart
// command. This is the regression the go-defaults clobber produced.
func TestConfigTaskRestartFalseEndToEnd(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: set config
      dokku_config:
        app: test-app
        restart: false
        config:
          KEY: val
`)
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet config:export --format json test-app": "{}",
	}))()

	parsed, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	task := parsed.Get("set config")
	if task == nil {
		t.Fatal("task 'set config' not found")
	}
	plan := task.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if len(plan.Commands) == 0 {
		t.Fatalf("expected a command, got none")
	}
	if !strings.Contains(plan.Commands[0], "--no-restart") {
		t.Errorf("restart: false should emit --no-restart, got %q", plan.Commands[0])
	}
}
