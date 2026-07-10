package tasks

import (
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
	_ "github.com/gliderlabs/sigil/builtin"
)

func TestNetworkTaskInvalidState(t *testing.T) {
	task := NetworkTask{Name: "test-network", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestGetTasksNetworkTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: create test network
      dokku_network:
        name: test-network
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("create test network")
	if task == nil {
		t.Fatal("task 'create test network' not found")
	}

	netTask, ok := task.(*NetworkTask)
	if !ok {
		nt, ok2 := task.(NetworkTask)
		if !ok2 {
			t.Fatalf("task is not a NetworkTask (type is %T)", task)
		}
		netTask = &nt
	}

	if netTask.Name != "test-network" {
		t.Errorf("Name = %q, want %q", netTask.Name, "test-network")
	}
	if netTask.State != StatePresent {
		t.Errorf("expected default state 'present', got %q", netTask.State)
	}
}

// networkListFixture is a network:list --format json payload mixing dokku-created
// networks (DokkuManaged true) with Docker built-ins and a compose *_default
// network (DokkuManaged false), deliberately unsorted to exercise the sort.
const networkListFixture = `[
	{"Name":"host","DokkuManaged":false},
	{"Name":"web-net","DokkuManaged":true},
	{"Name":"bridge","DokkuManaged":false},
	{"Name":"app-net","DokkuManaged":true},
	{"Name":"none","DokkuManaged":false},
	{"Name":"myproject_default","DokkuManaged":false}
]`

func TestNetworkExportGlobalOnlyDokkuManaged(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet network:list --format json": networkListFixture,
	}))()

	bodies, err := NetworkTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}

	got := make([]string, len(bodies))
	for i, b := range bodies {
		nt, ok := b.(NetworkTask)
		if !ok {
			t.Fatalf("export body %d is %T, want NetworkTask", i, b)
		}
		if nt.State != "" {
			t.Errorf("exported network %q sets state %q, want it omitted", nt.Name, nt.State)
		}
		got[i] = nt.Name
	}

	// Only the two dokku-created networks come back, sorted by name; the
	// built-ins and the compose *_default network are dropped.
	want := []string{"app-net", "web-net"}
	if !equalStrings(got, want) {
		t.Errorf("exported networks = %v, want %v", got, want)
	}
}

func TestExportNetworksBecomeTasks(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet network:list --format json": networkListFixture,
	}))()

	res, err := ExportRecipe(ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	// A network name is not sensitive, so nothing is lifted into the vars map.
	if res.HasVars() {
		t.Errorf("expected no lifted vars, got %v", res.Vars)
	}

	recipe, err := res.MarshalRecipe("yaml")
	if err != nil {
		t.Fatalf("MarshalRecipe: %v", err)
	}
	out := string(recipe)

	// The dokku-created networks land in the leading global play, keyed by
	// dokku_network, with the state omitted (the loader defaults it to present).
	for _, want := range []string{
		"name: global",
		"dokku_network:",
		"name: app-net",
		"name: web-net",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "state:") {
		t.Errorf("exported network should omit state:\n%s", out)
	}

	// Docker built-ins and the compose *_default network are never emitted.
	for _, unwanted := range []string{"name: bridge", "name: host", "name: none", "name: myproject_default"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("recipe should not contain %q:\n%s", unwanted, out)
		}
	}

	// A network name is emitted inline, never lifted into a templated input.
	if strings.Contains(out, "{{") {
		t.Errorf("recipe should not template a network name:\n%s", out)
	}
}
