package tasks

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
	yaml "gopkg.in/yaml.v3"
)

// export_plays_test.go covers ExportResult.Plays, the structured way out of an
// export that #425 asks for. Before it, the only exits were MarshalRecipe and
// MarshalVars, so a Go caller had to marshal to YAML and parse it straight
// back - or call ExportApp on a task directly and reimplement the ordering,
// warning collection and sensitive-value handling the engine already does.

// TestExportPlaysReturnsTypedTaskBodies is the point of the accessor: a caller
// reads fields off the task's own type rather than off a decoded map.
func TestExportPlaysReturnsTypedTaskBodies(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	res, err := ExportRecipe(ctx, ExportOptions{Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	plays := res.Plays()
	if len(plays) == 0 {
		t.Fatal("expected at least one play")
	}

	var found bool
	for _, play := range plays {
		if play.Name != "app-one" {
			continue
		}
		for _, task := range play.Tasks {
			if task.Type != "dokku_config" {
				continue
			}
			cfg, ok := As[ConfigTask](task)
			if !ok {
				t.Fatalf("dokku_config body is %T, want ConfigTask", task.Body)
			}
			if cfg.App != "app-one" {
				t.Errorf("ConfigTask.App = %q, want %q", cfg.App, "app-one")
			}
			if got := cfg.Config["SECRET_KEY"]; got != "s3cr3t" {
				t.Errorf("ConfigTask.Config[SECRET_KEY] = %q, want the value read off the server", got)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("no dokku_config task found on the app-one play; got %+v", plays)
	}
}

// TestExportPlaysMatchTheMarshalledRecipe is the reason the plays are stored as
// these values rather than converted into them: MarshalRecipe renders the same
// slice Plays returns, so the two cannot describe different exports.
func TestExportPlaysMatchTheMarshalledRecipe(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	res, err := ExportRecipe(ctx, ExportOptions{Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	recipe, err := res.MarshalRecipe("yaml")
	if err != nil {
		t.Fatalf("MarshalRecipe: %v", err)
	}

	var fromRecipe []struct {
		Name  string                   `yaml:"name"`
		Tasks []map[string]interface{} `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(recipe, &fromRecipe); err != nil {
		t.Fatalf("unmarshal recipe: %v", err)
	}

	plays := res.Plays()
	if len(plays) != len(fromRecipe) {
		t.Fatalf("Plays has %d plays, the recipe has %d", len(plays), len(fromRecipe))
	}
	for i, play := range plays {
		if play.Name != fromRecipe[i].Name {
			t.Errorf("play %d: name = %q, recipe says %q", i, play.Name, fromRecipe[i].Name)
		}
		if len(play.Tasks) != len(fromRecipe[i].Tasks) {
			t.Errorf("play %q: %d tasks, the recipe has %d", play.Name, len(play.Tasks), len(fromRecipe[i].Tasks))
			continue
		}
		for j, task := range play.Tasks {
			if _, ok := fromRecipe[i].Tasks[j][task.Type]; !ok {
				t.Errorf("play %q task %d: type %q is not the key the recipe used (%v)",
					play.Name, j, task.Type, keysOf(fromRecipe[i].Tasks[j]))
			}
		}
	}
}

// TestExportPlaysKeepsVarsLiftingOutOfTheBodies pins that the structured view
// shows what the recipe shows: in file mode a lifted value is an interpolation
// in the body, not the secret, and the secret lives in Vars.
func TestExportPlaysKeepsVarsLiftingOutOfTheBodies(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	for _, play := range res.Plays() {
		for _, task := range play.Tasks {
			cfg, ok := As[ConfigTask](task)
			if !ok {
				continue
			}
			for key, value := range cfg.Config {
				if value == "s3cr3t" {
					t.Errorf("%s: %s holds the literal secret; file mode lifts it into Vars", play.Name, key)
				}
			}
		}
	}
	if len(res.Vars) == 0 {
		t.Error("expected the lifted value in Vars")
	}
	var lifted bool
	for _, v := range res.Vars {
		if v == "s3cr3t" {
			lifted = true
		}
	}
	if !lifted {
		t.Errorf("expected the secret in Vars; got %v", keysOfStrings(res.Vars))
	}
}

// exportedConfigTask returns app-one's dokku_config task from an inline export
// of exportFixture.
func exportedConfigTask(t *testing.T) ExportedTask {
	t.Helper()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))
	res, err := ExportRecipe(ctx, ExportOptions{Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	for _, play := range res.Plays() {
		if play.Name != "app-one" {
			continue
		}
		for _, task := range play.Tasks {
			if task.Type == "dokku_config" {
				return task
			}
		}
	}
	t.Fatalf("no dokku_config task on the app-one play; got %+v", res.Plays())
	return ExportedTask{}
}

// TestAsReturnsTheTypedBody is the accessor #566 asks for: the body comes back
// as the task's own type without a hand-written assertion.
func TestAsReturnsTheTypedBody(t *testing.T) {
	t.Parallel()
	cfg, ok := As[ConfigTask](exportedConfigTask(t))
	if !ok {
		t.Fatal("As[ConfigTask] returned false for a dokku_config task")
	}
	if cfg.App != "app-one" || cfg.Config["SECRET_KEY"] != "s3cr3t" {
		t.Errorf("As[ConfigTask] = %+v, want the populated app-one config", cfg)
	}
}

// TestAsRejectsAnotherTaskType pins that asking for the wrong type is a false
// rather than a panic.
func TestAsRejectsAnotherTaskType(t *testing.T) {
	t.Parallel()
	app, ok := As[AppTask](exportedConfigTask(t))
	if ok {
		t.Fatal("As[AppTask] returned true for a dokku_config task")
	}
	if !reflect.DeepEqual(app, AppTask{}) {
		t.Errorf("As[AppTask] = %+v, want the zero value", app)
	}
}

// TestAsRejectsAPointerBody pins the value-only contract: As does not reach
// through a pointer, which is why the engine never lets one into a play.
func TestAsRejectsAPointerBody(t *testing.T) {
	t.Parallel()
	if _, ok := As[ConfigTask](ExportedTask{Type: "dokku_config", Body: &ConfigTask{}}); ok {
		t.Error("As[ConfigTask] returned true for a *ConfigTask body")
	}
}

// TestAppendBodiesRejectsAPointerBody pins that a pointer body is dropped with
// a warning before processBody sees it. processBody only recognises value
// types, so a *ConfigTask would otherwise skip processConfig and reach the
// recipe with its values inline instead of lifted into Vars.
func TestAppendBodiesRejectsAPointerBody(t *testing.T) {
	t.Parallel()
	res := &ExportResult{Vars: map[string]string{}, usedVarNames: map[string]bool{}}
	body := &ConfigTask{App: "app-one", Config: map[string]string{"K": "v"}}

	exported, inputs := res.appendBodies("app-one", "dokku_config", []interface{}{body}, ExportOptions{})
	if len(exported) != 0 || len(inputs) != 0 {
		t.Errorf("appendBodies kept the pointer body: tasks %+v, inputs %+v", exported, inputs)
	}
	if len(res.Vars) != 0 {
		t.Errorf("appendBodies lifted values from a dropped body: %v", res.Vars)
	}
	want := "app-one: dokku_config: exporter returned *tasks.ConfigTask, want tasks.ConfigTask"
	if !reflect.DeepEqual(res.Report.Warnings, []string{want}) {
		t.Errorf("warnings = %q, want [%q]", res.Report.Warnings, want)
	}
}

// TestAppendBodiesRejectsTheWrongTaskType pins that a body filed under another
// task's type-key is dropped rather than emitted under the wrong key.
func TestAppendBodiesRejectsTheWrongTaskType(t *testing.T) {
	t.Parallel()
	res := &ExportResult{Vars: map[string]string{}, usedVarNames: map[string]bool{}}

	exported, _ := res.appendBodies("global", "dokku_config", []interface{}{AppTask{App: "app-one"}}, ExportOptions{})
	if len(exported) != 0 {
		t.Errorf("appendBodies kept an AppTask under dokku_config: %+v", exported)
	}
	want := "global: dokku_config: exporter returned tasks.AppTask, want tasks.ConfigTask"
	if !reflect.DeepEqual(res.Report.Warnings, []string{want}) {
		t.Errorf("warnings = %q, want [%q]", res.Report.Warnings, want)
	}
}

// TestExportFixtureRaisesNoBodyTypeWarnings pins that the check accepts what
// real exporters return.
func TestExportFixtureRaisesNoBodyTypeWarnings(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))
	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	for _, w := range res.Report.Warnings {
		if strings.Contains(w, "exporter returned") {
			t.Errorf("unexpected body type warning: %s", w)
		}
	}
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysOfStrings(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
