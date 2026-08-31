package tasks

import "context"

// TraefikPropertyTask manages the traefik configuration for a given dokku application
type TraefikPropertyTask PropertyFields

// TraefikPropertyTaskExample contains an example of a TraefikPropertyTask
type TraefikPropertyTaskExample struct {
	// Name is the task name holding the TraefikPropertyTask description
	Name string `yaml:"-"`

	// TraefikPropertyTask is the TraefikPropertyTask configuration
	TraefikPropertyTask TraefikPropertyTask `yaml:"dokku_traefik_property"`
}

// GetName returns the name of the example
func (e TraefikPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the traefik property task
func (t TraefikPropertyTask) Doc() string {
	return "Manages the traefik configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t TraefikPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t TraefikPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbePartial, Caveat: "the mapped properties are probed; the dynamic `dns-provider-*` family has no report key and plans as drift on every run"}
}

// Examples returns the examples for the traefik property task
func (t TraefikPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]TraefikPropertyTaskExample{
		{
			Name: "Setting the letsencrypt email globally",
			TraefikPropertyTask: TraefikPropertyTask{
				Global:   true,
				Property: "letsencrypt-email",
				Value:    "admin@example.com",
			},
		},
		{
			Name: "Setting the log level globally",
			TraefikPropertyTask: TraefikPropertyTask{
				Global:   true,
				Property: "log-level",
				Value:    "INFO",
			},
		},
		{
			Name: "Clearing the letsencrypt email globally",
			TraefikPropertyTask: TraefikPropertyTask{
				Global:   true,
				Property: "letsencrypt-email",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the traefik property
func (t TraefikPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// traefikPropertyTable maps traefik property names to the JSON keys emitted
// by `dokku traefik:report --format json` on dokku 0.38.8+. All properties
// are global-only. The `dns-provider-*` family is dynamic and handled by
// isDynamicProperty without a map entry.
var traefikPropertyTable = PropertyTable{
	Subcommand: "traefik:set",
	Keys: map[string]PropertyKeys{
		"api-enabled":             {PerApp: "", Global: "global-api-enabled"},
		"api-entry-point":         {PerApp: "", Global: "global-api-entry-point"},
		"api-entry-point-address": {PerApp: "", Global: "global-api-entry-point-address"},
		"api-vhost":               {PerApp: "", Global: "global-api-vhost"},
		"basic-auth-password":     {PerApp: "", Global: "global-basic-auth-password", Sensitive: true},
		"basic-auth-username":     {PerApp: "", Global: "global-basic-auth-username"},
		"challenge-mode":          {PerApp: "", Global: "global-challenge-mode"},
		"dashboard-enabled":       {PerApp: "", Global: "global-dashboard-enabled"},
		"dns-provider":            {PerApp: "", Global: "global-dns-provider"},
		"http-entry-point":        {PerApp: "", Global: "global-http-entry-point"},
		"https-entry-point":       {PerApp: "", Global: "global-https-entry-point"},
		"image":                   {PerApp: "", Global: "global-image"},
		"letsencrypt-email":       {PerApp: "", Global: "global-letsencrypt-email"},
		"letsencrypt-server":      {PerApp: "", Global: "global-letsencrypt-server"},
		"log-level":               {PerApp: "", Global: "global-log-level"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t TraefikPropertyTask) PropertyTable() PropertyTable {
	return traefikPropertyTable
}

// Validate checks the TraefikPropertyTask's inputs without contacting the server.
func (t TraefikPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the TraefikPropertyTask would produce.
func (t TraefikPropertyTask) Plan(ctx context.Context) PlanResult {
	return planProperty(ctx, t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t TraefikPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return TraefikPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t TraefikPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return TraefikPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the TraefikPropertyTask with the task registry
func init() {
	RegisterTask(&TraefikPropertyTask{})
}
