package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

// ExportSupport reports how docket export handles this task.
func (t DockerOptionsTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ExportApp reconstructs the app's docker options as one task per individual
// option, per (phase, process type). Options are read from the structured
// `<phase>-list` report keys (dokku/dokku#8799), so each stored option -
// including values that contain spaces - is emitted as a discrete task without
// whitespace splitting.
func (t DockerOptionsTask) ExportApp(app string) ([]interface{}, error) {
	scoped, err := getDockerOptions(app)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}

	scopes := make([]dockerOptionsScope, 0, len(scoped))
	for scope := range scoped {
		scopes = append(scopes, scope)
	}
	sort.Slice(scopes, func(i, j int) bool {
		if scopes[i].Phase != scopes[j].Phase {
			return scopes[i].Phase < scopes[j].Phase
		}
		return scopes[i].ProcessType < scopes[j].ProcessType
	})

	var out []interface{}
	for _, scope := range scopes {
		for _, option := range scoped[scope] {
			out = append(out, DockerOptionsTask{
				App:         app,
				Phase:       scope.Phase,
				ProcessType: scope.ProcessType,
				Option:      option,
			})
		}
	}
	return out, nil
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

// Validate checks the DockerOptionsTask's inputs without contacting the server.
func (t DockerOptionsTask) Validate() error {
	if err := validateDockerOptionsTask(t); err != nil {
		return err
	}
	return nil
}

// Plan reports the drift the DockerOptionsTask would produce.
func (t DockerOptionsTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
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
// (phase, process type). dokku 0.38.22+ emits structured `<phase>-list` and
// `<phase>.<process_type>-list` companion keys whose values are JSON arrays
// with one element per stored option (dokku/dokku#8799); these are the source
// of truth here, so options are recovered as discrete entries even when their
// values contain spaces. The space-joined shorthand keys and the legacy
// `docker-options-*` duplicates are ignored.
func getDockerOptions(app string) (map[dockerOptionsScope][]string, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "docker-options:report", app, "--format", "json"},
	})
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{}
	if err := json.Unmarshal(result.StdoutBytes(), &payload); err != nil {
		return nil, fmt.Errorf("parse docker-options:report json: %w", err)
	}

	return parseDockerOptionsPayload(payload), nil
}

// parseDockerOptionsPayload turns the report JSON map into scope-keyed option
// lists. Only the structured `<key>-list` companions (dokku/dokku#8799) are
// consumed: each carries a JSON array with one element per stored option, so
// options are indexed as discrete entries in dokku's stored order. The
// space-joined shorthand keys are ignored (a non `-list` suffix), and a
// `docker-options-` prefix is dropped defensively - dokku ships no
// `docker-options-*-list` companion today, matching the "drop unknown keys so
// future report additions do not surface as drift" posture.
func parseDockerOptionsPayload(payload map[string]interface{}) map[dockerOptionsScope][]string {
	out := map[dockerOptionsScope][]string{}
	for key, value := range payload {
		shorthand, ok := strings.CutSuffix(key, "-list")
		if !ok {
			continue
		}
		if strings.HasPrefix(shorthand, "docker-options-") {
			continue
		}
		items, ok := value.([]interface{})
		if !ok {
			continue
		}
		phase, processType, ok := splitDockerOptionsKey(shorthand)
		if !ok {
			continue
		}
		options := make([]string, 0, len(items))
		for _, item := range items {
			if str, ok := item.(string); ok {
				options = append(options, str)
			}
		}
		out[dockerOptionsScope{Phase: phase, ProcessType: processType}] = options
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

// optionPresent returns true if option appears as a discrete stored entry in
// existing. Options are compared by exact string equality because dokku's
// `<phase>-list` report keys expose each option as a discrete element
// (dokku/dokku#8799); no token splitting is required.
func optionPresent(existing []string, option string) bool {
	for _, entry := range existing {
		if entry == option {
			return true
		}
	}
	return false
}

// init registers the DockerOptionsTask with the task registry
func init() {
	RegisterTask(&DockerOptionsTask{})
}
