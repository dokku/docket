package sdk_test

import (
	"fmt"

	"github.com/dokku/docket/sdk"
)

// ExampleTaskInState tears down a process formation: dokku_ps_scale applies
// with set and tears down with clear, which refuses scale, so the teardown
// copy leaves it out.
func ExampleTaskInState() {
	task, err := sdk.NewTask("dokku_ps_scale")
	if err != nil {
		panic(err)
	}
	applied := task.(*sdk.PsScaleTask)
	applied.App = "api"
	applied.Scale = map[string]int{"web": 2}

	lifecycle := sdk.TaskLifecycle(applied)
	applied.State = lifecycle.Apply

	moved, err := sdk.TaskInState(applied, lifecycle.Teardown)
	if err != nil {
		panic(err)
	}
	destroy := moved.(*sdk.PsScaleTask)
	fmt.Println(lifecycle.Apply, lifecycle.Teardown)
	fmt.Println(destroy.State, len(destroy.Scale), len(applied.Scale))
	fmt.Println(len(sdk.ConstraintViolations(destroy)))
	// Output:
	// set clear
	// clear 0 1
	// 0
}
