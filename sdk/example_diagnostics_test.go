package sdk_test

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dokku/docket/sdk"
)

// Showing why a read failed: the error becomes a short summary to show and a
// detail carrying the rest, both masked with the session's masker.
func ExampleDiagnostics() {
	session := sdk.NewSession(sdk.NewMasker())
	defer func() { _ = session.Close() }()
	ctx := sdk.ContextWithRunner(session.Context(context.Background(), sdk.Target{}), func(_ context.Context, in sdk.ExecCommandInput) (sdk.ExecCommandResponse, error) {
		if strings.Join(in.Args, " ") == "--quiet apps:exists web" {
			return sdk.ExecCommandResponse{ExitCode: 1}, nil
		}
		res := sdk.ExecCommandResponse{Stderr: "App web does not exist", ExitCode: 1}
		return res, &sdk.ExecError{Response: res, Err: errors.New(res.Stderr), Ran: true}
	})

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
	ctx := sdk.ContextWithRunner(session.Context(context.Background(), sdk.Target{}), func(_ context.Context, in sdk.ExecCommandInput) (sdk.ExecCommandResponse, error) {
		if strings.Join(in.Args, " ") == "--quiet apps:exists web" {
			return sdk.ExecCommandResponse{}, nil
		}
		if in.Args[1] == "config:set" {
			res := sdk.ExecCommandResponse{Stdout: "-----> Setting config vars", Stderr: "refusing to set TOKEN=hunter2", ExitCode: 1}
			return res, &sdk.ExecError{Response: res, Err: errors.New(res.Stderr), Ran: true}
		}
		return sdk.ExecCommandResponse{Stdout: "{}"}, nil
	})

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
