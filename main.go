package main

import (
	"context"
	"fmt"
	"os"

	"github.com/dokku/docket/commands"

	_ "github.com/gliderlabs/sigil/builtin"
	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

var AppName = "docket"

var Version string

func main() {
	os.Exit(Run(os.Args[1:]))
}

func Run(args []string) int {
	ctx := context.Background()
	commandMeta := command.SetupRun(ctx, AppName, Version, args)
	commandMeta.Ui = command.HumanZerologUiWithFields(commandMeta.Ui, make(map[string]interface{}, 0))
	c := cli.NewCLI(AppName, Version)
	c.Args = os.Args[1:]
	c.Commands = command.Commands(ctx, commandMeta, Commands)
	exitCode, err := c.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error executing CLI: %s\n", err.Error())
		return 1
	}

	return exitCode
}

func Commands(ctx context.Context, meta command.Meta) map[string]cli.CommandFactory {
	return map[string]cli.CommandFactory{
		"apply": func() (cli.Command, error) {
			return &commands.ApplyCommand{Meta: meta}, nil
		},
		"export": func() (cli.Command, error) {
			return &commands.ExportCommand{Meta: meta}, nil
		},
		"fmt": func() (cli.Command, error) {
			return &commands.FmtCommand{Meta: meta}, nil
		},
		"init": func() (cli.Command, error) {
			return &commands.InitCommand{Meta: meta}, nil
		},
		"plan": func() (cli.Command, error) {
			return &commands.PlanCommand{Meta: meta}, nil
		},
		"validate": func() (cli.Command, error) {
			return &commands.ValidateCommand{Meta: meta}, nil
		},
		"version": func() (cli.Command, error) {
			return &command.VersionCommand{Meta: meta}, nil
		},
	}
}
