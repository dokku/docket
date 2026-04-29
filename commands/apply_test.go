package commands

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/tasks"
	pflag "github.com/spf13/pflag"
)

func TestApplyCommandMetadata(t *testing.T) {
	c := &ApplyCommand{}
	if c.Name() != "apply" {
		t.Errorf("Name = %q, want \"apply\"", c.Name())
	}
	if c.Synopsis() == "" {
		t.Error("Synopsis must not be empty")
	}
}

func TestApplyCommandHelpDoesNotPanic(t *testing.T) {
	c := &ApplyCommand{}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("FlagSet panicked without tasks.yml on disk: %v", r)
		}
	}()
	_ = c.FlagSet()
}

// TestEnvelopeExprContextSkipsNonLoopEnvelopes ensures the per-envelope
// expr context only injects `.item` / `.index` for loop expansions; a
// non-loop envelope evaluates `when:` against the file-level inputs only.
func TestEnvelopeExprContextSkipsNonLoopEnvelopes(t *testing.T) {
	base := map[string]interface{}{"env": "prod"}
	env := &tasks.TaskEnvelope{Name: "x"}
	got := envelopeExprContext(base, env, nil, nil)
	if !reflect.DeepEqual(got, base) {
		t.Errorf("non-loop envelope must return base context unchanged; got %v", got)
	}
}

func TestEnvelopeExprContextInjectsLoopVars(t *testing.T) {
	base := map[string]interface{}{"env": "prod"}
	env := &tasks.TaskEnvelope{Name: "x", IsLoopExpansion: true, LoopItem: "api", LoopIndex: 2}
	got := envelopeExprContext(base, env, nil, nil)
	if got["item"] != "api" {
		t.Errorf("item = %v, want api", got["item"])
	}
	if got["index"] != 2 {
		t.Errorf("index = %v, want 2", got["index"])
	}
	if got["env"] != "prod" {
		t.Errorf("env should be inherited; got %v", got["env"])
	}
	if base["item"] != nil {
		t.Errorf("base must not be mutated by envelopeExprContext")
	}
}

// TestEnvelopeExprContextExposesResultAndRegistered pins the new keys
// added by #210: `.result` only when the caller passes a non-nil
// result, and `.registered` only when the registered map is
// non-empty.
func TestEnvelopeExprContextExposesResultAndRegistered(t *testing.T) {
	base := map[string]interface{}{"env": "prod"}
	env := &tasks.TaskEnvelope{Name: "x"}

	got := envelopeExprContext(base, env, nil, nil)
	if _, ok := got["result"]; ok {
		t.Errorf("result should be omitted when nil; got %v", got)
	}
	if _, ok := got["registered"]; ok {
		t.Errorf("registered should be omitted when empty; got %v", got)
	}

	state := tasks.TaskOutputState{Changed: true}
	registered := map[string]tasks.RegisteredValue{"foo": {TaskOutputState: state}}
	got = envelopeExprContext(base, env, state, registered)
	if got["result"] == nil {
		t.Errorf("result should be exposed when non-nil; got %v", got)
	}
	if got["registered"] == nil {
		t.Errorf("registered should be exposed when non-empty; got %v", got)
	}
}

// TestFilterByTagsThroughCommandsLayer covers the apply / plan path's
// reliance on tasks.FilterByTags. Routing the flags through the helper
// in the tasks package keeps the command-side wiring trivial; this test
// pins the behaviour the commands rely on.
func TestFilterByTagsThroughCommandsLayer(t *testing.T) {
	m := tasks.OrderedStringEnvelopeMap{}
	m.Set("api", &tasks.TaskEnvelope{Name: "api", Tags: []string{"api"}})
	m.Set("worker", &tasks.TaskEnvelope{Name: "worker", Tags: []string{"worker"}})
	m.Set("untagged", &tasks.TaskEnvelope{Name: "untagged"})

	if got := tasks.FilterByTags(m, []string{"api"}, nil); !reflect.DeepEqual(got, []string{"api"}) {
		t.Errorf("tags=[api] got %v, want [api]", got)
	}
	if got := tasks.FilterByTags(m, nil, []string{"api"}); !reflect.DeepEqual(got, []string{"worker", "untagged"}) {
		t.Errorf("skip-tags=[api] got %v, want [worker untagged]", got)
	}
	if got := tasks.FilterByTags(m, nil, nil); len(got) != 3 {
		t.Errorf("no flags: got %v, want all 3", got)
	}
}

// TestFilterPlaysByName covers the --play filter helper used by both
// apply and plan. An empty target returns the slice unchanged; a hit
// returns just the matched play; a miss returns an error that names the
// available plays.
func TestFilterPlaysByName(t *testing.T) {
	plays := []*tasks.Play{
		{Name: "api"},
		{Name: "worker"},
	}

	out, err := filterPlaysByName(plays, "")
	if err != nil || len(out) != 2 {
		t.Errorf("empty target should pass through; got len=%d err=%v", len(out), err)
	}

	out, err = filterPlaysByName(plays, "api")
	if err != nil || len(out) != 1 || out[0].Name != "api" {
		t.Errorf(`--play "api" got len=%d names=%v err=%v`, len(out), playNames(out), err)
	}

	_, err = filterPlaysByName(plays, "missing")
	if err == nil {
		t.Fatal("expected error for unknown play")
	}
	for _, want := range []string{`--play "missing"`, `"api"`, `"worker"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q\nfull: %v", want, err)
		}
	}
}

func playNames(plays []*tasks.Play) []string {
	out := make([]string, len(plays))
	for i, p := range plays {
		out[i] = p.Name
	}
	return out
}

// TestUserSetKeysMergesCLIAndVarsFile pins the precedence-tracking
// helper that #208 needs to layer per-play inputs correctly. CLI-set
// flags and vars-file keys both contribute; non-input flags are filtered
// out so internal flags (--tasks etc.) do not pollute the per-play
// override set.
func TestUserSetKeysMergesCLIAndVarsFile(t *testing.T) {
	flags := pflag.NewFlagSet("t", pflag.ContinueOnError)
	flags.String("app", "default", "")
	flags.String("env", "default", "")
	flags.String("tasks", "tasks.yml", "")
	if err := flags.Parse([]string{"--app=cli-app", "--tasks=other.yml"}); err != nil {
		t.Fatalf("flags.Parse: %v", err)
	}

	args := map[string]*Argument{
		"app": argFor(t, "string", ""),
		"env": argFor(t, "string", ""),
	}
	varsFileKeys := map[string]bool{"env": true}

	got := userSetKeys(flags, varsFileKeys, args)
	want := map[string]bool{"app": true, "env": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("userSetKeys = %v, want %v (tasks should drop because not in arguments)", got, want)
	}
}

// TestUserSetKeysWithoutFlagSet covers the nil-flags edge case. Vars-file
// keys still flow through; no panic when callers haven't registered a
// FlagSet yet.
func TestUserSetKeysWithoutFlagSet(t *testing.T) {
	got := userSetKeys(nil, map[string]bool{"env": true}, nil)
	if !reflect.DeepEqual(got, map[string]bool{"env": true}) {
		t.Errorf("userSetKeys(nil, ...) = %v, want only env", got)
	}
}
