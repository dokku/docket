package sdk_test

import (
	"context"
	"fmt"
	"os"

	"github.com/dokku/docket/sdk"
)

// A session installs the masker and target a run needs, and closes the SSH
// connections made under it.
func ExampleNewSession() {
	session := sdk.NewSession(sdk.NewMasker("s3cr3t"))
	defer session.Close()

	ctx := session.Context(context.Background(), sdk.Target{
		Host: "deploy@dokku.example.com",
		Sudo: true,
	})
	_ = ctx
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
	defer session.Close()
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
