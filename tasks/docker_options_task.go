package tasks

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// defaultDockerOptionsProcessType is the sentinel dokku uses for options that
// apply to every container in an app. It is rejected as an explicit
// --process value, so the task accepts an empty ProcessType to target the
// default scope and rejects this literal.
const defaultDockerOptionsProcessType = "_default_"

// DockerOptionsTask manages docker-options for a given dokku application
type DockerOptionsTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Phase is the deployment phase the option applies to
	Phase string `required:"true" yaml:"phase" options:"build,deploy,run" description:"Deployment phase the option applies to"`

	// ProcessType scopes the option to a specific Procfile process type.
	// Only valid for the deploy phase; empty applies to the default scope
	// (every container in the app).
	ProcessType string `required:"false" yaml:"process_type,omitempty" description:"Process type the option is scoped to (deploy phase only). Empty applies to the default scope (every container)."`

	// Option is the docker option string (e.g. "-v /var/run/docker.sock:/var/run/docker.sock")
	Option string `required:"true" yaml:"option" description:"Docker option string (e.g. '-v /var/run/docker.sock:/var/run/docker.sock')"`

	// State is the desired state of the docker option
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the docker option"`
}

// DockerOptionsTaskExample contains an example of a DockerOptionsTask
type DockerOptionsTaskExample struct {
	// Name is the task name holding the DockerOptionsTask description
	Name string `yaml:"-"`

	// DockerOptionsTask is the DockerOptionsTask configuration
	DockerOptionsTask DockerOptionsTask `yaml:"dokku_docker_options"`
}

// GetName returns the name of the example
func (e DockerOptionsTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the docker options task
func (t DockerOptionsTask) Doc() string {
	return "Manages docker-options for a given dokku application"
}

// Examples returns the examples for the docker options task
func (t DockerOptionsTask) Examples() ([]Doc, error) {
	return MarshalExamples([]DockerOptionsTaskExample{
		{
			Name: "Mount the docker socket at deploy",
			DockerOptionsTask: DockerOptionsTask{
				App:    "node-js-app",
				Phase:  "deploy",
				Option: "-v /var/run/docker.sock:/var/run/docker.sock",
			},
		},
		{
			Name: "Scope a deploy option to the web process type",
			DockerOptionsTask: DockerOptionsTask{
				App:         "node-js-app",
				Phase:       "deploy",
				ProcessType: "web",
				Option:      "--memory=512m",
			},
		},
		{
			Name: "Remove a docker option from the deploy phase",
			DockerOptionsTask: DockerOptionsTask{
				App:    "node-js-app",
				Phase:  "deploy",
				Option: "-v /var/run/docker.sock:/var/run/docker.sock",
				State:  StateAbsent,
			},
		},
	})
}

// Execute manages the docker option
func (t DockerOptionsTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the DockerOptionsTask would produce.
func (t DockerOptionsTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			if err := validateDockerOptionsTask(t); err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			current, err := getDockerOptions(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			scope := dockerOptionsScope{Phase: t.Phase, ProcessType: t.ProcessType}
			if optionPresent(current[scope], t.Option) {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    dockerOptionsCommandArgs("docker-options:add", t),
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    fmt.Sprintf("missing on %s", describeDockerOptionsScope(scope)),
				Mutations: []string{fmt.Sprintf("add %s option %q", describeDockerOptionsScope(scope), t.Option)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			if err := validateDockerOptionsTask(t); err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			current, err := getDockerOptions(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			scope := dockerOptionsScope{Phase: t.Phase, ProcessType: t.ProcessType}
			if !optionPresent(current[scope], t.Option) {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    dockerOptionsCommandArgs("docker-options:remove", t),
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    fmt.Sprintf("present on %s", describeDockerOptionsScope(scope)),
				Mutations: []string{fmt.Sprintf("remove %s option %q", describeDockerOptionsScope(scope), t.Option)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

var dockerOptionPhases = map[string]bool{"build": true, "deploy": true, "run": true}

// dockerOptionsProcessScopedPhases mirrors dokku's processScopedPhases: only
// the deploy phase accepts a --process value. Build runs once per app and run
// is invoked outside the Procfile process-type context.
var dockerOptionsProcessScopedPhases = map[string]bool{"deploy": true}

// dockerOptionsScope identifies one (phase, process type) bucket in dokku's
// docker-options storage. An empty ProcessType means the default scope.
type dockerOptionsScope struct {
	Phase       string
	ProcessType string
}

// describeDockerOptionsScope renders the scope for use in Reason/Mutations
// strings. Default scope shows just the phase; a process-scoped value names
// the process so plan output makes the scope visible.
func describeDockerOptionsScope(scope dockerOptionsScope) string {
	if scope.ProcessType == "" {
		return fmt.Sprintf("%s phase", scope.Phase)
	}
	return fmt.Sprintf("%s phase for %s process", scope.Phase, scope.ProcessType)
}

// dockerOptionsCommandArgs builds the dokku argument list for
// docker-options:add / :remove. --process is placed before positionals
// because the subcommand uses pflag with SetInterspersed(false).
func dockerOptionsCommandArgs(subcommand string, t DockerOptionsTask) []string {
	args := []string{"--quiet", subcommand}
	if t.ProcessType != "" {
		args = append(args, "--process", t.ProcessType)
	}
	return append(args, t.App, t.Phase, t.Option)
}

// validateDockerOptionsTask validates the docker options task parameters
func validateDockerOptionsTask(t DockerOptionsTask) error {
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	if !dockerOptionPhases[t.Phase] {
		return fmt.Errorf("'phase' must be one of build, deploy, run")
	}
	if strings.TrimSpace(t.Option) == "" {
		return fmt.Errorf("'option' is required")
	}
	if t.ProcessType != "" {
		if t.ProcessType == defaultDockerOptionsProcessType {
			return fmt.Errorf("'process_type' must not be %q (reserved sentinel)", defaultDockerOptionsProcessType)
		}
		if !dockerOptionsProcessScopedPhases[t.Phase] {
			return fmt.Errorf("'process_type' is only supported for the deploy phase, got %q", t.Phase)
		}
	}
	return nil
}

// getDockerOptions reads the docker-options JSON report and indexes it by
// (phase, process type). The JSON payload carries one entry per
// `<phase>` and one per `<phase>.<process_type>` for any process-scoped
// option, plus legacy `docker-options-*` duplicates emitted during the
// 0.38.x deprecation window which we discard.
func getDockerOptions(app string) (map[dockerOptionsScope]string, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "docker-options:report", app, "--format", "json"},
	})
	if err != nil {
		return nil, err
	}

	payload := map[string]string{}
	if err := json.Unmarshal(result.StdoutBytes(), &payload); err != nil {
		return nil, fmt.Errorf("parse docker-options:report json: %w", err)
	}

	return parseDockerOptionsPayload(payload), nil
}

// parseDockerOptionsPayload turns the report JSON map into scope-keyed
// options. Legacy plugin-prefixed keys and any unknown keys are dropped so
// that future report additions do not surface as drift.
func parseDockerOptionsPayload(payload map[string]string) map[dockerOptionsScope]string {
	out := map[dockerOptionsScope]string{}
	for key, value := range payload {
		if strings.HasPrefix(key, "docker-options-") {
			continue
		}
		phase, processType, ok := splitDockerOptionsKey(key)
		if !ok {
			continue
		}
		out[dockerOptionsScope{Phase: phase, ProcessType: processType}] = value
	}
	return out
}

// splitDockerOptionsKey parses a JSON key like "deploy" or "deploy.web" into
// its phase and process-type components. Returns ok=false for keys whose head
// is not a recognized phase.
func splitDockerOptionsKey(key string) (phase, processType string, ok bool) {
	if idx := strings.Index(key, "."); idx > 0 {
		phase = key[:idx]
		processType = key[idx+1:]
	} else {
		phase = key
	}
	if !dockerOptionPhases[phase] {
		return "", "", false
	}
	return phase, processType, true
}

// optionPresent returns true if option appears as a contiguous token sequence in existing
func optionPresent(existing, option string) bool {
	optionTokens := strings.Fields(option)
	if len(optionTokens) == 0 {
		return false
	}
	existingTokens := strings.Fields(existing)
	for i := 0; i+len(optionTokens) <= len(existingTokens); i++ {
		match := true
		for j, tok := range optionTokens {
			if existingTokens[i+j] != tok {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// init registers the DockerOptionsTask with the task registry
func init() {
	RegisterTask(&DockerOptionsTask{})
}
