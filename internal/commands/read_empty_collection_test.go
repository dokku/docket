package commands

import (
	"encoding/json"
	"testing"
)

// TestReadCommandEmptyCollectionExitsZero pins #639: a whole collection with
// no members exists, so it prints in state clear and exits 0 rather than 2.
func TestReadCommandEmptyCollectionExitsZero(t *testing.T) {
	t.Parallel()
	c, stdout, ui := readWithRunner(absentRunner(map[string]string{
		"--quiet config:export --format json web": `{}`,
	}, "web"))
	if code := c.Run([]string{"--json", "dokku_config[app=web]"}); code != 0 {
		t.Fatalf("exit = %d, want 0; stderr:\n%s", code, ui.ErrorWriter.String())
	}
	var got readJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, stdout.String())
	}
	if !got.Exists || got.Task["state"] != "clear" {
		t.Errorf("read = %+v, want an empty config that exists", got)
	}
}
