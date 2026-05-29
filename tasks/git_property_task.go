package tasks

// GitPropertyTask manages the git configuration for a given dokku application
type GitPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the git configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the git configuration should be applied globally"`

	// Property is the name of the git property to set
	Property string `required:"true" yaml:"property" description:"Name of the git property to set"`

	// Value is the value to set for the git property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the git property"`

	// State is the desired state of the git configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the git configuration"`
}

// GitPropertyTaskExample contains an example of a GitPropertyTask
type GitPropertyTaskExample struct {
	// Name is the task name holding the GitPropertyTask description
	Name string `yaml:"-"`

	// GitPropertyTask is the GitPropertyTask configuration
	GitPropertyTask GitPropertyTask `yaml:"dokku_git_property"`
}

// GetName returns the name of the example
func (e GitPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the git property task
func (t GitPropertyTask) Doc() string {
	return "Manages the git configuration for a given dokku application"
}

// Examples returns the examples for the git property task
func (t GitPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]GitPropertyTaskExample{
		{
			Name: "Setting the deploy branch for an app",
			GitPropertyTask: GitPropertyTask{
				App:      "node-js-app",
				Property: "deploy-branch",
				Value:    "main",
			},
		},
		{
			Name: "Keeping the .git directory during builds",
			GitPropertyTask: GitPropertyTask{
				App:      "node-js-app",
				Property: "keep-git-dir",
				Value:    "true",
			},
		},
		{
			Name: "Setting the rev env var globally",
			GitPropertyTask: GitPropertyTask{
				Global:   true,
				Property: "rev-env-var",
				Value:    "GIT_REV",
			},
		},
		{
			Name: "Clearing a git property",
			GitPropertyTask: GitPropertyTask{
				App:      "node-js-app",
				Property: "deploy-branch",
			},
		},
	})
}

// Execute sets or unsets the git property
func (t GitPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// gitPropertyKeys maps git property names to the JSON keys emitted by
// `dokku git:report --format json` on dokku 0.38.9+. archive-max-files and
// archive-max-size only surface a global key; rev-env-var and source-image
// only surface a per-app key.
var gitPropertyKeys = map[string]PropertyKeys{
	"archive-max-files": {PerApp: "", Global: "global-archive-max-files"},
	"archive-max-size":  {PerApp: "", Global: "global-archive-max-size"},
	"deploy-branch":     {PerApp: "deploy-branch", Global: "global-deploy-branch"},
	"keep-git-dir":      {PerApp: "keep-git-dir", Global: "global-keep-git-dir"},
	"rev-env-var":       {PerApp: "rev-env-var", Global: ""},
	"source-image":      {PerApp: "source-image", Global: ""},
}

// Plan reports the drift the GitPropertyTask would produce.
func (t GitPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "git:set", gitPropertyKeys)
}

// init registers the GitPropertyTask with the task registry
func init() {
	RegisterTask(&GitPropertyTask{})
}
