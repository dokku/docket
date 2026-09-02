package tasks

import (
	"testing"

	"github.com/dokku/docket/subprocess"
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
			cfg, ok := task.Body.(ConfigTask)
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
			cfg, ok := task.Body.(ConfigTask)
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
