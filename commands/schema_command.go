package commands

import (
	"bytes"
	"fmt"
	"os"

	"github.com/dokku/docket/tasks"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/posener/complete"
	flag "github.com/spf13/pflag"
)

// SchemaCommand prints the machine-readable catalog of task types.
//
// docket knows the full shape of every task it registers - fields, types,
// defaults, choices, which values are secrets, what each task addresses, and
// for a property task the exact set of property names it accepts. Until this
// command existed, the only way to get at that was to read the generated
// markdown under docs/tasks/ or to vendor the reflection (#422). The catalog is
// the same data the reference pages are rendered from, so the two can never
// disagree.
//
// It is offline by contract: no subprocess, no server, and no recipe. The
// answer depends only on which docket binary is running, which is why the
// catalog is not a committed file - it cannot go stale.
//
// --task narrows it to named task types (#459). The document keeps its shape -
// a version and a tasks array - so a consumer parses one format either way and
// the published JSON Schema covers both.
type SchemaCommand struct {
	command.Meta

	output    string
	taskTypes []string
}

func (c *SchemaCommand) Name() string {
	return "schema"
}

func (c *SchemaCommand) Synopsis() string {
	return "Prints the machine-readable catalog of task types"
}

func (c *SchemaCommand) Help() string {
	return command.CommandHelp(c)
}

func (c *SchemaCommand) Examples() map[string]string {
	appName := os.Getenv("CLI_APP_NAME")
	return map[string]string{
		"Print the catalog to stdout":          fmt.Sprintf("%s %s", appName, c.Name()),
		"Write the catalog to a file":          fmt.Sprintf("%s %s --output catalog.json", appName, c.Name()),
		"List every task type":                 fmt.Sprintf("%s %s | jq -r '.tasks[].type'", appName, c.Name()),
		"Narrow the catalog to two task types": fmt.Sprintf("%s %s --task dokku_config --task dokku_domains", appName, c.Name()),
		"Show one task's fields":               fmt.Sprintf("%s %s | jq '.tasks[] | select(.type==\"dokku_config\") | .fields'", appName, c.Name()),
		"List a property task's settings":      fmt.Sprintf("%s %s | jq -r '.tasks[] | select(.type==\"dokku_nginx_property\") | .property_schema.properties[].name'", appName, c.Name()),
	}
}

func (c *SchemaCommand) Arguments() []command.Argument {
	return []command.Argument{}
}

func (c *SchemaCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

func (c *SchemaCommand) ParsedArguments(args []string) (map[string]command.Argument, error) {
	return command.ParseArguments(args, c.Arguments())
}

func (c *SchemaCommand) FlagSet() *flag.FlagSet {
	f := c.Meta.FlagSet(c.Name(), command.FlagSetClient)
	f.StringVar(&c.output, "output", "-", "path to write the catalog to; - writes to stdout")
	f.StringArrayVar(&c.taskTypes, "task", nil, "restrict the catalog to the named task type, e.g. dokku_config (repeatable); the whole catalog is emitted when omitted")
	return f
}

func (c *SchemaCommand) AutocompleteFlags() complete.Flags {
	return command.MergeAutocompleteFlags(
		c.Meta.AutocompleteFlags(command.FlagSetClient),
		complete.Flags{
			"--output": complete.PredictFiles("*.json"),
			"--task":   complete.PredictSet(tasks.RegisteredTaskNames()...),
		},
	)
}

// validateTaskTypeFilter checks every --task value against the registry before
// any work happens, so a typo fails before the catalog is built and, with
// --output, before anything touches the disk.
//
// An unknown type names the closest registered one the same way an unknown task
// type in a recipe address does (tasks.ParseResourceSelectors). Matching is
// exact: type keys are case-sensitive at every other lookup, so a case-folded
// match here would be the only place the vocabulary bent.
func validateTaskTypeFilter(values []string) error {
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("invalid --task %q: a task type is required", value)
		}
		if _, ok := tasks.RegisteredTasks[value]; ok {
			continue
		}
		msg := fmt.Sprintf("unknown task type %q", value)
		if near := nearestInputName(value, tasks.RegisteredTaskNames()); near != "" {
			msg += fmt.Sprintf(" (did you mean %q?)", near)
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// Run builds the catalog and writes it. Exit codes:
//
//	0 - catalog written
//	1 - flag parse error, unexpected positional argument, unknown --task type,
//	    or IO error
func (c *SchemaCommand) Run(args []string) int {
	flags := c.FlagSet()
	flags.Usage = func() { c.Ui.Output(c.Help()) }
	if err := flags.Parse(args); err != nil {
		c.Ui.Error(err.Error())
		c.Ui.Error(command.CommandErrorText(c))
		return 1
	}
	if rest := flags.Args(); len(rest) > 0 {
		msg := fmt.Sprintf("unexpected argument %q: schema takes no arguments", rest[0])
		// A bare task type is the mistake --task invites, so point at the
		// flag rather than leaving the reader at a dead end.
		if _, ok := tasks.RegisteredTasks[rest[0]]; ok {
			msg += fmt.Sprintf("; did you mean --task %s?", rest[0])
		}
		c.Ui.Error(msg)
		c.Ui.Error(command.CommandErrorText(c))
		return 1
	}

	// No CommandErrorText here: --help cannot list task types, so pointing at
	// it would be noise. Same as export's unknown --resource task type.
	if err := validateTaskTypeFilter(c.taskTypes); err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	catalog, err := tasks.CatalogFor(c.taskTypes)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("failed to build the task catalog: %v", err))
		return 1
	}

	// Encode into memory first so a serialization failure cannot leave a
	// half-written document on stdout or a truncated file on disk.
	var buf bytes.Buffer
	if err := catalog.Encode(&buf); err != nil {
		c.Ui.Error(fmt.Sprintf("failed to encode the task catalog: %v", err))
		return 1
	}

	// The catalog goes straight to stdout rather than through c.Ui, which is
	// wrapped in a human log formatter. Same reason init and export write
	// their rendered output directly.
	if c.output == "-" {
		if _, err := os.Stdout.Write(buf.Bytes()); err != nil {
			c.Ui.Error(fmt.Sprintf("write error: %v", err))
			return 1
		}
		return 0
	}

	if err := os.WriteFile(c.output, buf.Bytes(), 0o644); err != nil {
		c.Ui.Error(fmt.Sprintf("write error: %v", err))
		return 1
	}
	return 0
}
