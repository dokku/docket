// Package dokkutest is a stateful fake dokku server for tests that drive
// docket's tasks through the sdk package without a real server.
//
// A Server holds apps, their config, domains, port mappings, buildpacks,
// process scale and plugin properties, and datastore services with their info
// and links. It answers the commands docket's tasks send in the formats their
// probes parse, applies the changes their applies make, and records every
// call. A test seeds the state it starts from, runs tasks, and asserts on the
// state they leave, not on the command lines they ran:
//
//	server := dokkutest.New(t).SeedApp("web")
//	ctx := server.Context(context.Background())
//
//	task := sdk.ConfigTask{App: "web", Config: map[string]string{"KEY": "value"}, State: sdk.StatePresent}
//	if state := task.Execute(ctx); state.Error != nil {
//		t.Fatal(state.Error)
//	}
//	if got := server.Config("web")["KEY"]; got != "value" {
//		t.Errorf("KEY = %q", got)
//	}
//
// # Coverage
//
// The Server models the commands of dokku_app, dokku_config, dokku_domains,
// dokku_ports, dokku_buildpacks, dokku_ps_scale, every dokku_*_property task
// that manages a plugin's property table, dokku_service_create and
// dokku_service_link, along with the version gate and the probes a read makes
// for the app or service a task depends on. It fails any other command: the
// call returns an error naming the command, and the test the Server was made
// with fails. Answer such a command with Handle, HandlePrefix or Fallback.
//
// It keeps no history beyond its state, runs no builds or deploys, and
// creates no domains or links on its own: an app starts with nothing, and a
// service link writes the service's DSN into the app's config under the key
// the datastore plugin uses (DATABASE_URL for postgres and mysql,
// <SERVICE>_URL otherwise). An app or service that does not exist gets the
// error and exit code dokku gives.
//
// # Failures
//
// Handle answers one command with a Handler instead of the model, which
// neither reads nor changes the state. Exit fails a command the way dokku
// does, with an exit code and stderr; Unavailable fails one that never ran;
// Respond answers with fixed output.
//
// The types are aliases of docket's own fake, so the Server's methods are
// documented on internal/fakedokku.Server.
package dokkutest

import (
	"sync"
	"testing"

	"github.com/dokku/docket/internal/fakedokku"
	"github.com/dokku/docket/sdk"
)

// Server is a fake dokku server. Make one with New.
type Server = fakedokku.Server

// Call is one command a Server was sent.
type Call = fakedokku.Call

// Handler answers one command in place of a Server's model. It has the shape
// of sdk.ExecRunner.
type Handler = fakedokku.Handler

// PortMapping is one port mapping, as a Server holds it.
type PortMapping = fakedokku.PortMapping

// DefaultVersion is the dokku version a new Server reports.
const DefaultVersion = fakedokku.DefaultVersion

// New returns an empty Server: dokku at DefaultVersion with its core plugins,
// no apps and no services. t fails the test on a command the Server does not
// model; it may be nil, as in an example, and the call still returns an error.
//
// The Server answers for the property table of every dokku_*_property task in
// sdk.Catalog().
func New(t testing.TB) *Server {
	return fakedokku.New(t, catalogProperties())
}

// catalogProperties is read once: the catalog does not change while a test
// binary runs.
var catalogProperties = sync.OnceValue(readCatalogProperties)

// Respond is a Handler that succeeds and prints stdout.
func Respond(stdout string) Handler {
	return fakedokku.Respond(stdout)
}

// Exit is a Handler for a command that ran and exited with code, printing
// stderr, the way the real runner reports it: the response carries the exit
// code, and the error is an *sdk.ExecError with Ran set.
func Exit(code int, stderr string) Handler {
	return fakedokku.Exit(code, stderr)
}

// Unavailable is a Handler for a command that could not run at all, such as a
// dokku binary that is not installed. A probe reports it as an error rather
// than as the resource being absent.
func Unavailable(err error) Handler {
	return fakedokku.Unavailable(err)
}

// readCatalogProperties reads the property tables from the catalog, which
// every registered task builds without error, so a failure here is a docket
// bug.
func readCatalogProperties() []fakedokku.PropertyPlugin {
	catalog, err := sdk.Catalog()
	if err != nil {
		panic("dokkutest: read the task catalog: " + err.Error())
	}
	byPlugin := map[string]*fakedokku.PropertyPlugin{}
	var plugins []fakedokku.PropertyPlugin
	var order []string
	for _, task := range catalog.Tasks {
		schema := task.PropertySchema
		if schema == nil {
			continue
		}
		p, ok := byPlugin[schema.Plugin]
		if !ok {
			p = &fakedokku.PropertyPlugin{Plugin: schema.Plugin, Subcommand: schema.Subcommand}
			byPlugin[schema.Plugin] = p
			order = append(order, schema.Plugin)
		}
		for _, prop := range schema.Properties {
			p.Properties = append(p.Properties, fakedokku.Property{Name: prop.Name, AppKey: prop.AppReportKey, GlobalKey: prop.GlobalReportKey})
		}
	}
	for _, plugin := range order {
		plugins = append(plugins, *byPlugin[plugin])
	}
	return plugins
}
