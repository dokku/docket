package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	// One signal handler for the process, installed here rather than around
	// every subprocess call. Cancelling this context is what makes an
	// interrupt abort the run instead of only the task in flight, and
	// NotifyContext restores the default disposition afterwards so a second
	// Ctrl-C still kills a wedged process.
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGTERM)
	defer stop()

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
			return &commands.ApplyCommand{Meta: meta, Ctx: ctx}, nil
		},
		"export": func() (cli.Command, error) {
			return &commands.ExportCommand{Meta: meta, Ctx: ctx}, nil
		},
		"fmt": func() (cli.Command, error) {
			return &commands.FmtCommand{Meta: meta}, nil
		},
		"init": func() (cli.Command, error) {
			return &commands.InitCommand{Meta: meta}, nil
		},
		"plan": func() (cli.Command, error) {
			return &commands.PlanCommand{Meta: meta, Ctx: ctx}, nil
		},
		"schema": func() (cli.Command, error) {
			return &commands.SchemaCommand{Meta: meta}, nil
		},
		"validate": func() (cli.Command, error) {
			return &commands.ValidateCommand{Meta: meta}, nil
		},
		"version": func() (cli.Command, error) {
			return &command.VersionCommand{Meta: meta}, nil
		},
	}
}
