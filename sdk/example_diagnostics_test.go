package sdk_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/dokku/docket/sdk"
	"github.com/dokku/docket/sdk/dokkutest"
)

// Showing why a read failed: the error becomes a short summary to show and a
// detail carrying the rest, both masked with the session's masker.
func ExampleDiagnostics() {
	session := sdk.NewSession(sdk.NewMasker())
	defer func() { _ = session.Close() }()
	// The server holds no app web.
	ctx := dokkutest.New(nil).Context(session.Context(context.Background(), sdk.Target{}))

	task := sdk.ConfigTask{App: "web", Config: map[string]string{"LOG_LEVEL": "info"}, State: sdk.StatePresent}
	_, _, err := sdk.ReadTask(ctx, task)
	for _, d := range sdk.Diagnostics(ctx, err) {
		fmt.Println(d.Severity, d.Reason, d.Summary)
	}
	// Output:
	// error object_missing app web does not exist
}

// A failed apply's command output is in the detail, with every secret the
// task declares masked.
func ExampleDiagnostics_failedCommand() {
	session := sdk.NewSession(sdk.NewMasker())
	defer func() { _ = session.Close() }()
	server := dokkutest.New(nil).SeedApp("web")
	server.HandlePrefix("--quiet config:set", func(context.Context, sdk.ExecCommandInput) (sdk.ExecCommandResponse, error) {
		res := sdk.ExecCommandResponse{Stdout: "-----> Setting config vars", Stderr: "refusing to set TOKEN=hunter2", ExitCode: 1}
		return res, &sdk.ExecError{Response: res, Err: errors.New(res.Stderr), Ran: true}
	})
	ctx := server.Context(session.Context(context.Background(), sdk.Target{}))

	task := sdk.ConfigTask{App: "web", Config: map[string]string{"TOKEN": "hunter2"}, State: sdk.StatePresent}
	state := sdk.ExecutePlan(ctx, task.Plan(ctx))
	for _, d := range sdk.Diagnostics(ctx, state) {
		fmt.Println(d.Summary)
		fmt.Println(d.Detail)
	}
	// Output:
	// the command failed with exit code 1
	// refusing to set TOKEN=***
	//
	// stdout:
	// -----> Setting config vars
}
