package sdk_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/dokku/docket/sdk"
)

// A session installs the masker and target a run needs, and closes the SSH
// connections made under it.
func ExampleNewSession() {
	session := sdk.NewSession(sdk.NewMasker("s3cr3t"))
	defer func() { _ = session.Close() }()

	ctx := session.Context(context.Background(), sdk.Target{
		Host: "deploy@dokku.example.com",
		Sudo: true,
	})
	_ = ctx
}

// A session given the version gate refuses tasks the server is too old for,
// and reads the server's versions once however many calls it serves, each
// with a context of its own.
func ExampleWithVersionGate() {
	session := sdk.NewSession(nil, sdk.WithVersionGate())
	defer func() { _ = session.Close() }()

	serve := func(reqCtx context.Context) {
		ctx := session.Context(reqCtx, sdk.Target{Host: "deploy@dokku.example.com"})
		_ = ctx
	}
	serve(context.Background())
	serve(context.Background())
}

func ExampleNewTask() {
	task, err := sdk.NewTask("dokku_app")
	if err != nil {
		panic(err)
	}
	app := task.(*sdk.AppTask)
	app.App = "api"

	fmt.Println(sdk.IdentityAddress("dokku_app", app), app.State)
	// Output: dokku_app[app=api] present
}

// Reading a server back and picking out one task type.
func ExampleExportRecipe() {
	session := sdk.NewSession(nil)
	defer func() { _ = session.Close() }()
	ctx := session.Context(context.Background(), sdk.Target{Host: "dokku@dokku.example.com"})

	res, err := sdk.ExportRecipe(ctx, sdk.ExportOptions{Inline: true})
	if err != nil {
		panic(err)
	}

	// Register what the export read before printing anything it produced.
	masker := sdk.NewMasker(res.SensitiveValues()...)
	for _, w := range res.Report.Warnings {
		fmt.Fprintln(os.Stderr, masker.String(w))
	}

	for _, play := range res.Plays() {
		for _, task := range play.Tasks {
			cfg, ok := sdk.As[sdk.ConfigTask](task)
			if !ok {
				continue
			}
			fmt.Println(play.Name, cfg.App, len(cfg.Config))
		}
	}
}

// Planning and applying a task against a fake dokku in a test, with no server.
func ExampleContextWithRunner() {
	var calls []string
	ctx := sdk.ContextWithRunner(context.Background(), func(_ context.Context, in sdk.ExecCommandInput) (sdk.ExecCommandResponse, error) {
		calls = append(calls, strings.Join(in.Args, " "))
		if len(in.Args) > 1 && in.Args[1] == "apps:exists" {
			// The app is absent: the command ran and exited non-zero.
			return sdk.ExecCommandResponse{ExitCode: 1}, nil
		}
		return sdk.ExecCommandResponse{}, nil
	})

	task, _ := sdk.NewTask("dokku_app")
	app := task.(*sdk.AppTask)
	app.App = "api"

	plan := app.Plan(ctx)
	fmt.Println(plan.Reason, plan.Commands)

	state := sdk.ExecutePlan(ctx, plan)
	fmt.Println(state.Changed, calls)
	// Output:
	// app missing [dokku --quiet apps:create api]
	// true [--quiet apps:exists api --quiet apps:create api]
}

// Reading a resource known only by its identity address, such as one being
// imported, reads every member it has.
func ExampleReadAddress() {
	ctx := sdk.ContextWithRunner(context.Background(), func(_ context.Context, in sdk.ExecCommandInput) (sdk.ExecCommandResponse, error) {
		switch strings.Join(in.Args, " ") {
		case "version":
			return sdk.ExecCommandResponse{Stdout: "dokku version 0.38.31"}, nil
		case "--quiet buildpacks:list api":
			return sdk.ExecCommandResponse{Stdout: "=====> api buildpack urls\nhttps://github.com/heroku/heroku-buildpack-nodejs.git"}, nil
		}
		return sdk.ExecCommandResponse{}, nil
	})
	ctx = sdk.ContextWithVersionGate(ctx)

	current, exists, err := sdk.ReadAddress(ctx, "dokku_buildpacks[app=api]")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(exists, current.(sdk.BuildpacksTask).Buildpacks)
	// Output:
	// true [https://github.com/heroku/heroku-buildpack-nodejs.git]
}

// Reading one resource's current state through its task, without exporting
// the server.
func ExampleReadTask() {
	ctx := sdk.ContextWithRunner(context.Background(), func(_ context.Context, in sdk.ExecCommandInput) (sdk.ExecCommandResponse, error) {
		switch strings.Join(in.Args, " ") {
		case "version":
			return sdk.ExecCommandResponse{Stdout: "dokku version 0.38.31"}, nil
		case "--quiet config:export --format json api":
			return sdk.ExecCommandResponse{Stdout: `{"LOG_LEVEL":"debug","OTHER":"x"}`}, nil
		}
		return sdk.ExecCommandResponse{}, nil
	})
	ctx = sdk.ContextWithVersionGate(ctx)

	task, _ := sdk.NewTask("dokku_config")
	cfg := task.(*sdk.ConfigTask)
	cfg.App = "api"
	cfg.Config = map[string]string{"LOG_LEVEL": "info"}

	current, exists, err := sdk.ReadTask(ctx, cfg)
	var verr *sdk.VersionError
	if errors.As(err, &verr) {
		fmt.Println("server too old for", verr.TaskType)
		return
	}
	if err != nil {
		fmt.Println(err)
		return
	}
	observed := current.(sdk.ConfigTask)
	fmt.Println(exists, observed.Config)
	// Output:
	// true map[LOG_LEVEL:debug]
}

func ExampleCatalogFor() {
	catalog, err := sdk.CatalogFor([]string{"dokku_app"})
	if err != nil {
		panic(err)
	}
	for _, schema := range catalog.Tasks {
		fmt.Println(schema.Type, "-", schema.Synopsis)
	}
	// Output: dokku_app - Creates or destroys an app
}

func ExampleTaskTypes() {
	for _, typeKey := range sdk.TaskTypes() {
		task, _ := sdk.Lookup(typeKey)
		if typeKey == "dokku_app" {
			fmt.Println(typeKey, "-", sdk.TaskSynopsis(task))
		}
	}
	// Output: dokku_app - Creates or destroys an app
}

// Building a masker for one task before planning it, so nothing rendered for
// it carries a secret.
func ExampleTaskSensitiveValues() {
	task, err := sdk.DecodeTask("dokku_http_auth_user", []byte(`
app: api
users:
  - username: admin
    password: s3cr3t
`))
	if err != nil {
		panic(err)
	}

	masker := sdk.NewMasker(sdk.TaskSensitiveValues(task)...)
	fmt.Println(masker.String("http-auth:add-user api admin s3cr3t"))
	// Output: http-auth:add-user api admin ***
}
