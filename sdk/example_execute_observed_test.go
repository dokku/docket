package sdk_test

import (
	"context"
	"fmt"

	"github.com/dokku/docket/sdk"
	"github.com/dokku/docket/sdk/dokkutest"
)

// Storing a resource's state after creating it, without reading it back: the
// returned state carries the task as the server now holds it, including the
// image dokku picked for a service the task does not pin.
func ExampleExecutePlan_observed() {
	ctx := dokkutest.New(nil).SeedDatastore("postgres", "postgres:17.2").Context(context.Background())

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
	server := dokkutest.New(nil).SeedDatastore("postgres", "postgres:17.2")
	ctx := server.Context(context.Background())

	task := sdk.ServiceCreateTask{Service: "postgres", Name: "db", State: sdk.StatePresent}
	state := task.Execute(sdk.ContextWithoutObserved(ctx))
	fmt.Println(state.Changed, state.Observed == nil, server.Lines())
	// Output:
	// true true [--quiet postgres:exists db --quiet postgres:create db]
}
