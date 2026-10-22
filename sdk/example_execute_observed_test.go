package sdk_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/dokku/docket/sdk"
)

// Storing a resource's state after creating it, without reading it back: the
// returned state carries the task as the server now holds it, including the
// image dokku picked for a service the task does not pin.
func ExampleExecutePlan_observed() {
	created := false
	ctx := sdk.ContextWithRunner(context.Background(), func(_ context.Context, in sdk.ExecCommandInput) (sdk.ExecCommandResponse, error) {
		switch strings.Join(in.Args, " ") {
		case "--quiet postgres:exists db":
			if !created {
				return sdk.ExecCommandResponse{ExitCode: 1}, nil
			}
		case "--quiet postgres:create db":
			created = true
		case "--quiet postgres:info db --format json":
			return sdk.ExecCommandResponse{Stdout: `{"version":"postgres:17.2"}`}, nil
		}
		return sdk.ExecCommandResponse{}, nil
	})

	task := sdk.ServiceCreateTask{Service: "postgres", Name: "db", State: sdk.StatePresent}
	state := sdk.ExecutePlan(ctx, task.Plan(ctx))
	if state.Error != nil {
		fmt.Println(state.Error)
		return
	}
	observed := state.Observed.(sdk.ServiceCreateTask)
	fmt.Println(state.Changed, state.Exists, observed.Image, observed.ImageVersion)
	// Output:
	// true true postgres 17.2
}

// Skipping the observed state when nothing reads it, so an apply never probes
// the server a second time.
func ExampleContextWithoutObserved() {
	var calls []string
	ctx := sdk.ContextWithRunner(context.Background(), func(_ context.Context, in sdk.ExecCommandInput) (sdk.ExecCommandResponse, error) {
		calls = append(calls, strings.Join(in.Args, " "))
		if strings.Join(in.Args, " ") == "--quiet postgres:exists db" {
			return sdk.ExecCommandResponse{ExitCode: 1}, nil
		}
		return sdk.ExecCommandResponse{}, nil
	})

	task := sdk.ServiceCreateTask{Service: "postgres", Name: "db", State: sdk.StatePresent}
	state := task.Execute(sdk.ContextWithoutObserved(ctx))
	fmt.Println(state.Changed, state.Observed == nil, calls)
	// Output:
	// true true [--quiet postgres:exists db --quiet postgres:create db]
}
