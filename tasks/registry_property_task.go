package tasks

// RegistryPropertyTask manages the registry configuration for a given dokku application
type RegistryPropertyTask PropertyFields

// RegistryPropertyTaskExample contains an example of a RegistryPropertyTask
type RegistryPropertyTaskExample struct {
	// Name is the task name holding the RegistryPropertyTask description
	Name string `yaml:"-"`

	// RegistryPropertyTask is the RegistryPropertyTask configuration
	RegistryPropertyTask RegistryPropertyTask `yaml:"dokku_registry_property"`
}

// GetName returns the name of the example
func (e RegistryPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the registry property task
func (t RegistryPropertyTask) Doc() string {
	return "Manages the registry configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t RegistryPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t RegistryPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the registry property task
func (t RegistryPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]RegistryPropertyTaskExample{
		{
			Name: "Setting the image repo for an app",
			RegistryPropertyTask: RegistryPropertyTask{
				App:      "node-js-app",
				Property: "image-repo",
				Value:    "registry.example.com/node-js-app",
			},
		},
		{
			Name: "Enabling push-on-release for an app",
			RegistryPropertyTask: RegistryPropertyTask{
				App:      "node-js-app",
				Property: "push-on-release",
				Value:    "true",
			},
		},
		{
			Name: "Setting the registry server globally",
			RegistryPropertyTask: RegistryPropertyTask{
				Global:   true,
				Property: "server",
				Value:    "registry.example.com",
			},
		},
		{
			Name: "Clearing the image repo for an app",
			RegistryPropertyTask: RegistryPropertyTask{
				App:      "node-js-app",
				Property: "image-repo",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the registry property
func (t RegistryPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// registryPropertyTable maps registry property names to the JSON keys
// emitted by `dokku registry:report --format json` on dokku 0.38.8+.
// image-repo is per-app only; tag-version is read-only (managed by build).
var registryPropertyTable = PropertyTable{
	Subcommand: "registry:set",
	Keys: map[string]PropertyKeys{
		"image-repo":          {PerApp: "image-repo", Global: ""},
		"image-repo-template": {PerApp: "image-repo-template", Global: "global-image-repo-template"},
		"push-extra-tags":     {PerApp: "push-extra-tags", Global: "global-push-extra-tags"},
		"push-on-release":     {PerApp: "push-on-release", Global: "global-push-on-release"},
		"server":              {PerApp: "server", Global: "global-server"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t RegistryPropertyTask) PropertyTable() PropertyTable {
	return registryPropertyTable
}

// Validate checks the RegistryPropertyTask's inputs without contacting the server.
func (t RegistryPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the RegistryPropertyTask would produce.
func (t RegistryPropertyTask) Plan() PlanResult {
	return planProperty(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t RegistryPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(t, app, func(app, property, value string) interface{} {
		return RegistryPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t RegistryPropertyTask) ExportGlobal() ([]interface{}, error) {
	return exportGlobalProperties(t, func(property, value string) interface{} {
		return RegistryPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the RegistryPropertyTask with the task registry
func init() {
	RegisterTask(&RegistryPropertyTask{})
}
