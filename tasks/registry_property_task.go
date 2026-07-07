package tasks

// RegistryPropertyTask manages the registry configuration for a given dokku application
type RegistryPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the registry configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the registry configuration should be applied globally"`

	// Property is the name of the registry property to set
	Property string `required:"true" yaml:"property" description:"Name of the registry property to set"`

	// Value is the value to set for the registry property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the registry property"`

	// State is the desired state of the registry configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the registry configuration"`
}

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
			},
		},
	})
}

// Execute sets or unsets the registry property
func (t RegistryPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// registryPropertyKeys maps registry property names to the JSON keys
// emitted by `dokku registry:report --format json` on dokku 0.38.8+.
// image-repo is per-app only; tag-version is read-only (managed by build).
var registryPropertyKeys = map[string]PropertyKeys{
	"image-repo":          {PerApp: "image-repo", Global: ""},
	"image-repo-template": {PerApp: "image-repo-template", Global: "global-image-repo-template"},
	"push-extra-tags":     {PerApp: "push-extra-tags", Global: "global-push-extra-tags"},
	"push-on-release":     {PerApp: "push-on-release", Global: "global-push-on-release"},
	"server":              {PerApp: "server", Global: "global-server"},
}

// Plan reports the drift the RegistryPropertyTask would produce.
func (t RegistryPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "registry:set", registryPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t RegistryPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "registry:set", registryPropertyKeys, func(app, property, value string) interface{} {
		return RegistryPropertyTask{App: app, Property: property, Value: value}
	})
}

// init registers the RegistryPropertyTask with the task registry
func init() {
	RegisterTask(&RegistryPropertyTask{})
}
