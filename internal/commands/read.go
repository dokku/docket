package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/dokku/docket/internal/subprocess"
	"github.com/dokku/docket/internal/tasks"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/posener/complete"
	flag "github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// readExitAbsent is the exit code for a read that succeeded and found nothing
// at the address, so a script can tell "not there" from "could not read"
// without parsing the output.
const readExitAbsent = 2

// readExitObjectMissing is the exit code for a read whose app or service is
// missing from the server, so a script can tell "the parent is gone" from a
// resource that is not there and from a read that failed.
const readExitObjectMissing = 3

// ReadCommand reads one resource from its identity address and prints it.
//
// It is the CLI face of tasks.ReadAddress: the address alone names what to
// read, every member the resource has is read through the task's own probe,
// and the result prints as a one-task recipe - the same codecs export writes
// with - or, under --json, as an object carrying whether it exists.
//
// Sensitive values the read returns are masked unless --show-sensitive is
// passed. The address's own keys are the user's arguments, not server data,
// so they print as given.
type ReadCommand struct {
	command.Meta

	// Ctx is the process signal context from main.go; nil means background.
	Ctx context.Context

	// Stdout is where the read is written. Populated from main.go; nil
	// writes to the process's standard output. These writes bypass Ui on
	// purpose - it is wrapped in a log formatter.
	Stdout io.Writer

	formatFlag        string
	jsonOutput        bool
	showSensitive     bool
	host              string
	sudo              bool
	acceptNewHostKeys bool
	sshIdentityFile   string
	sshOptions        []string
	ignoreVersion     bool
}

func (c *ReadCommand) Name() string {
	return "read"
}

func (c *ReadCommand) Synopsis() string {
	return "Reads one resource from its identity address"
}

func (c *ReadCommand) Help() string {
	return command.CommandHelp(c)
}

func (c *ReadCommand) Examples() map[string]string {
	appName := os.Getenv("CLI_APP_NAME")
	return map[string]string{
		"Read an app's buildpacks":              fmt.Sprintf("%s %s 'dokku_buildpacks[app=node-js-app]'", appName, c.Name()),
		"Read a global property":                fmt.Sprintf("%s %s 'dokku_nginx_property[global=true,property=bind-address-ipv4]'", appName, c.Name()),
		"Read as JSON with the existence flag":  fmt.Sprintf("%s %s --json 'dokku_config[app=node-js-app]'", appName, c.Name()),
		"Read from a remote server as HCL":      fmt.Sprintf("%s %s --host dokku@dokku.example.com --format hcl 'dokku_domains[app=node-js-app]'", appName, c.Name()),
		"Print sensitive values instead of ***": fmt.Sprintf("%s %s --show-sensitive 'dokku_config[app=node-js-app]'", appName, c.Name()),
	}
}

func (c *ReadCommand) Arguments() []command.Argument {
	return []command.Argument{{
		Name:        "address",
		Description: "identity address of the resource, e.g. 'dokku_config[app=node-js-app]'",
		Optional:    false,
		Type:        command.ArgumentString,
	}}
}

func (c *ReadCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

func (c *ReadCommand) ParsedArguments(args []string) (map[string]command.Argument, error) {
	return command.ParseArguments(args, c.Arguments())
}

func (c *ReadCommand) FlagSet() *flag.FlagSet {
	f := c.Meta.FlagSet(c.Name(), command.FlagSetClient)
	f.StringVar(&c.formatFlag, "format", "", "print the resource as a one-task recipe in this format ("+recipeFormatList()+"); defaults to "+tasks.DefaultCodec().Name())
	f.BoolVar(&c.jsonOutput, "json", false, "print a JSON object with the address, task type, whether the resource exists, and the task")
	f.BoolVar(&c.showSensitive, "show-sensitive", false, "print sensitive values read from the server instead of masking them as ***")
	f.StringVar(&c.host, "host", "", "remote [user@]host[:port] to read over SSH; overrides DOKKU_HOST")
	f.BoolVar(&c.sudo, "sudo", false, "run dokku as root via sudo -n, remotely with --host and locally without")
	f.BoolVar(&c.acceptNewHostKeys, "accept-new-host-keys", false, "trust an unknown SSH host key on first connect")
	f.StringVar(&c.sshIdentityFile, "ssh-identity-file", "", "authenticate over SSH with this private key only; overrides DOKKU_SSH_IDENTITY_FILE")
	f.StringArrayVar(&c.sshOptions, "ssh-option", nil, "pass an extra ssh option as -o <option> (repeatable)")
	f.BoolVar(&c.ignoreVersion, "ignore-version", false, "read even when Dokku or a required plugin is older than the task declares")
	return f
}

func (c *ReadCommand) AutocompleteFlags() complete.Flags {
	return command.MergeAutocompleteFlags(
		c.Meta.AutocompleteFlags(command.FlagSetClient),
		complete.Flags{
			"--format":               recipeFormatAutocomplete(),
			"--json":                 complete.PredictNothing,
			"--show-sensitive":       complete.PredictNothing,
			"--host":                 complete.PredictNothing,
			"--sudo":                 complete.PredictNothing,
			"--accept-new-host-keys": complete.PredictNothing,
			"--ssh-identity-file":    complete.PredictFiles("*"),
			"--ssh-option":           complete.PredictAnything,
			"--ignore-version":       complete.PredictNothing,
		},
	)
}

// readJSON is what --json prints.
type readJSON struct {
	Address string                 `json:"address"`
	Type    string                 `json:"type"`
	Exists  bool                   `json:"exists"`
	Task    map[string]interface{} `json:"task"`
}

// Run reads the resource and prints it. Exit codes:
//
//	0 - the resource exists and was printed
//	1 - flag or argument error, an address that names no single resource, a
//	    version refusal, a read error, or a task that cannot read its state
//	2 - the read succeeded and the resource is not on the server; it is
//	    still printed, in the state that says so
//	3 - the app or service the resource depends on is not on the server;
//	    nothing is printed but the error
func (c *ReadCommand) Run(args []string) int {
	flags := c.FlagSet()
	flags.Usage = func() { c.Ui.Output(c.Help()) }
	if err := flags.Parse(args); err != nil {
		c.Ui.Error(err.Error())
		c.Ui.Error(command.CommandErrorText(c))
		return 1
	}
	rest := flags.Args()
	if len(rest) != 1 {
		c.Ui.Error(fmt.Sprintf("read takes exactly one resource address, got %d", len(rest)))
		c.Ui.Error(command.CommandErrorText(c))
		return 1
	}
	address := rest[0]

	format, err := parseRecipeFormatFlag("--format", c.formatFlag)
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}
	if c.jsonOutput && format != "" {
		c.Ui.Error("--json and --format cannot be combined; --json prints its own object")
		return 1
	}

	target, err := resolveSshFlags(os.Getenv, c.host, c.sudo, c.acceptNewHostKeys, c.sshIdentityFile, c.sshOptions)
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	masker := subprocess.NewMasker()
	var sessionOpts []subprocess.SessionOption
	if !c.ignoreVersion {
		sessionOpts = append(sessionOpts, tasks.SessionVersionGate())
	}
	session := subprocess.NewSession(masker, sessionOpts...)
	defer func() { _ = session.Close() }()
	ctx := session.Context(runContext(c.Ctx), target)

	got, exists, err := tasks.ReadAddress(ctx, address)
	if err != nil {
		// The address is the user's own argument and prints as typed; only
		// what the server said is masked (#488).
		c.Ui.Error(masker.String(ignoreVersionHint(err).Error()))
		if errors.Is(err, tasks.ErrObjectMissing) {
			return readExitObjectMissing
		}
		return 1
	}

	if !c.showSensitive {
		masker.Add(tasks.TaskSensitiveValues(got)...)
		got = maskTaskFields(masker, got)
	}

	typeKey, _, _ := tasks.ParseIdentityAddress(address)
	out, err := c.render(address, typeKey, got, exists, format)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("failed to render the read: %v", err))
		return 1
	}
	if _, err := commandStdout(c.Stdout).Write(out); err != nil {
		c.Ui.Error(fmt.Sprintf("write error: %v", err))
		return 1
	}
	if !exists {
		return readExitAbsent
	}
	return 0
}

// render formats the read as --json's object or as a one-task recipe.
func (c *ReadCommand) render(address, typeKey string, task tasks.Task, exists bool, format string) ([]byte, error) {
	if c.jsonOutput {
		body, err := taskAsMap(task)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(readJSON{Address: address, Type: typeKey, Exists: exists, Task: body}); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	return tasks.CodecFor(format).Marshal([]tasks.ExportedPlay{{
		Name:  address,
		Tasks: []tasks.ExportedTask{{Type: typeKey, Body: task}},
	}})
}

// taskAsMap returns a task's fields keyed by their recipe names, the way a
// recipe spells them, empty fields left out.
func taskAsMap(task tasks.Task) (map[string]interface{}, error) {
	raw, err := yaml.Marshal(task)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	if err := yaml.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// maskTaskFields returns a copy of task with every registered value masked in
// its fields, identity keys excepted: a key is the user's own address echoed
// back, and masking it could only hide which resource was read. Masking the
// value rather than the rendered text means a secret the codec escapes - a
// quote, a backslash - is still caught.
func maskTaskFields(m *subprocess.Masker, task tasks.Task) tasks.Task {
	rv := reflect.ValueOf(task)
	cp := reflect.New(rv.Type()).Elem()
	cp.Set(rv)
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() || field.Tag.Get("identity") == "key" {
			continue
		}
		cp.Field(i).Set(maskReflect(m, cp.Field(i)))
	}
	masked, ok := cp.Interface().(tasks.Task)
	if !ok {
		return task
	}
	return masked
}

// maskReflect returns v with every string it holds masked, walking pointers,
// slices, maps, and structs.
func maskReflect(m *subprocess.Masker, v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.String:
		out := reflect.New(v.Type()).Elem()
		out.SetString(m.String(v.String()))
		return out
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(maskReflect(m, v.Elem()))
		return out
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(maskReflect(m, v.Elem()))
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(maskReflect(m, v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), maskReflect(m, iter.Value()))
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				out.Field(i).Set(maskReflect(m, v.Field(i)))
			}
		}
		return out
	}
	return v
}
