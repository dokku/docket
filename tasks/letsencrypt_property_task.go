package tasks

import "context"

// LetsencryptPropertyTask manages the letsencrypt configuration for a given dokku application
type LetsencryptPropertyTask SensitivePropertyFields

// LetsencryptPropertyTaskExample contains an example of a LetsencryptPropertyTask
type LetsencryptPropertyTaskExample struct {
	// Name is the task name holding the LetsencryptPropertyTask description
	Name string `yaml:"-"`

	// LetsencryptPropertyTask is the LetsencryptPropertyTask configuration
	LetsencryptPropertyTask LetsencryptPropertyTask `yaml:"dokku_letsencrypt_property"`
}

// GetName returns the name of the example
func (e LetsencryptPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the letsencrypt property task
func (t LetsencryptPropertyTask) Doc() string {
	return "Manages the letsencrypt configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t LetsencryptPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
//
// Supported without a caveat because dokku-letsencrypt 0.25.0+ reports the
// dynamic `dns-provider-*` family alongside the mapped properties, so every
// property this task manages is readable and converges (#449).
func (t LetsencryptPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Requirements lists the non-core dokku plugins this task depends on. The
// version floor is the release that surfaced `dns-provider-*` properties in
// `letsencrypt:report`; below it those properties are absent from the report
// and a `state: absent` task would read an unset property as already gone.
func (t LetsencryptPropertyTask) Requirements() []string {
	return []string{"dokku-letsencrypt plugin >= 0.25.0"}
}

// Examples returns the examples for the letsencrypt property task
func (t LetsencryptPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]LetsencryptPropertyTaskExample{
		{
			Name: "Setting the letsencrypt email for an app",
			LetsencryptPropertyTask: LetsencryptPropertyTask{
				App:      "node-js-app",
				Property: "email",
				Value:    "admin@example.com",
			},
		},
		{
			Name: "Setting the dns provider for an app",
			LetsencryptPropertyTask: LetsencryptPropertyTask{
				App:      "node-js-app",
				Property: "dns-provider",
				Value:    "namecheap",
			},
		},
		{
			Name: "Setting a dns-provider-* env var globally",
			LetsencryptPropertyTask: LetsencryptPropertyTask{
				Global:   true,
				Property: "dns-provider-NAMECHEAP_API_USER",
				Value:    "deploy-bot",
			},
		},
		{
			Name: "Clearing the letsencrypt email for an app",
			LetsencryptPropertyTask: LetsencryptPropertyTask{
				App:      "node-js-app",
				Property: "email",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the letsencrypt property
func (t LetsencryptPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// letsencryptPropertyTable maps letsencrypt property names to the JSON keys
// emitted by `dokku letsencrypt:report --format json` on
// dokku-letsencrypt v0.20.4+. The `dns-provider-*` family takes an arbitrary
// provider env var name, so it cannot be enumerated here; its keys are
// synthesized per property by dynamicPropertyKeys, which v0.25.0+ reports.
var letsencryptPropertyTable = PropertyTable{
	Subcommand: "letsencrypt:set",
	Keys: map[string]PropertyKeys{
		"dns-provider":        {PerApp: "dns-provider", Global: "global-dns-provider"},
		"email":               {PerApp: "email", Global: "global-email"},
		"graceperiod":         {PerApp: "graceperiod", Global: "global-graceperiod"},
		"lego-args":           {PerApp: "lego-args", Global: "global-lego-args"},
		"lego-docker-options": {PerApp: "lego-docker-options", Global: "global-lego-docker-options"},
		"server":              {PerApp: "server", Global: "global-server"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t LetsencryptPropertyTask) PropertyTable() PropertyTable {
	return letsencryptPropertyTable
}

// Validate checks the LetsencryptPropertyTask's inputs without contacting the server.
func (t LetsencryptPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the LetsencryptPropertyTask would produce.
func (t LetsencryptPropertyTask) Plan(ctx context.Context) PlanResult {
	return planProperty(ctx, t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t LetsencryptPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return LetsencryptPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t LetsencryptPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return LetsencryptPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the LetsencryptPropertyTask with the task registry
func init() {
	RegisterTask(&LetsencryptPropertyTask{})
}
