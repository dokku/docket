package commands

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestReadCommandJSONPrintsEveryField pins that --json's task is
// tasks.TaskValues of the read (#644): every field the task declares is
// present, an empty collection prints empty rather than missing, and an
// apply-only *bool prints the default the task coerces nil to.
func TestReadCommandJSONPrintsEveryField(t *testing.T) {
	t.Parallel()
	c, stdout, ui := readWithRunner(absentRunner(map[string]string{
		"--quiet config:export --format json web": `{}`,
	}, "web"))
	if code := c.Run([]string{"--json", "dokku_config[app=web]"}); code != 0 {
		t.Fatalf("exit = %d; stderr:\n%s", code, ui.ErrorWriter.String())
	}
	var got readJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, stdout.String())
	}
	want := map[string]interface{}{
		"app":      "web",
		"config":   map[string]interface{}{},
		"preserve": []interface{}{},
		"restart":  true,
		"state":    "clear",
	}
	if !reflect.DeepEqual(got.Task, want) {
		t.Errorf("task = %#v, want %#v", got.Task, want)
	}
}
