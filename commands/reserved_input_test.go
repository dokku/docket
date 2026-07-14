package commands

import (
	"testing"

	"github.com/dokku/docket/tasks"

	flag "github.com/spf13/pflag"
)

// flagSetter is implemented by every command whose FlagSet may register
// dynamic recipe-input flags on top of its built-ins.
type flagSetter interface {
	FlagSet() *flag.FlagSet
}

// TestReservedInputNamesCoversBuiltinFlags keeps tasks.ReservedInputNames
// in sync with the real flag sets: every built-in flag on an
// input-accepting command must be reserved so a recipe input with that
// name is rejected offline instead of panicking pflag with "flag
// redefined" (#302). Adding a new built-in flag without reserving it
// fails this test.
func TestReservedInputNamesCoversBuiltinFlags(t *testing.T) {
	cmds := map[string]flagSetter{
		"apply":    &ApplyCommand{},
		"plan":     &PlanCommand{},
		"validate": &ValidateCommand{},
	}
	for name, c := range cmds {
		c.FlagSet().VisitAll(func(f *flag.Flag) {
			if !tasks.ReservedInputNames[f.Name] {
				t.Errorf("%s built-in flag %q is not in tasks.ReservedInputNames; a recipe input with that name would collide", name, f.Name)
			}
		})
	}
}

// TestRegisterInputFlagsSkipsReservedNames verifies that building a
// command's flag set with a reserved input name does not panic (pflag
// would otherwise abort with "flag redefined") - the input is silently
// skipped and the offline validator is left to report it.
func TestRegisterInputFlagsSkipsReservedNames(t *testing.T) {
	data := []byte(`---
- inputs:
    - name: verbose
      default: x
  tasks:
    - dokku_app:
        app: my-app
`)
	f := flag.NewFlagSet("apply", flag.ContinueOnError)
	f.Bool("verbose", false, "built-in verbose flag")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registerInputFlags panicked on a reserved input name: %v", r)
		}
	}()
	if _, err := registerInputFlags(f, data, tasks.FormatYAML); err != nil {
		t.Fatalf("registerInputFlags returned error: %v", err)
	}
}
