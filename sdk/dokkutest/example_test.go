package dokkutest_test

import (
	"context"
	"fmt"

	"github.com/dokku/docket/sdk"
	"github.com/dokku/docket/sdk/dokkutest"
)

// Creating a resource, refreshing it and destroying it, then asserting on the
// state the server is left with rather than on the commands that ran.
func Example() {
	// A test passes its *testing.T, which fails on a command the fake does
	// not model.
	server := dokkutest.New(nil).SeedApp("web")
	ctx := server.Context(context.Background())

	task := sdk.DomainsTask{App: "web", Domains: []string{"web.example.com"}, State: sdk.StateSet}
	if state := task.Execute(ctx); state.Error != nil {
		fmt.Println(state.Error)
		return
	}
	fmt.Println(server.Domains("web"))

	read, exists, err := sdk.ReadAddress(ctx, "dokku_domains[app=web]")
	fmt.Println(read.(sdk.DomainsTask).Domains, exists, err)

	task.State, task.Domains = sdk.StateClear, nil
	task.Execute(ctx)
	fmt.Println(len(server.Domains("web")))
	// Output:
	// [web.example.com]
	// [web.example.com] true <nil>
	// 0
}

// Failing one command the way dokku does, to test how a caller handles it.
// The handler answers in place of the fake's model, so the config stays as it
// was.
func ExampleServer_Handle() {
	server := dokkutest.New(nil).SeedApp("web")
	server.HandlePrefix("--quiet config:set", dokkutest.Exit(1, "config is locked"))
	ctx := server.Context(context.Background())

	task := sdk.ConfigTask{App: "web", Config: map[string]string{"KEY": "value"}, State: sdk.StatePresent}
	state := sdk.ExecutePlan(ctx, task.Plan(ctx))
	fmt.Println(state.ExitCode, state.Message, len(server.Config("web")))
	// Output:
	// 1 config is locked 0
}
