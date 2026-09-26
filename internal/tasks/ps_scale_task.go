package tasks

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/dokku/docket/internal/subprocess"
)

// PsScaleTask manages the process scale for a given dokku application.
//
// The two states differ in what happens to a process type the recipe does not
// name. `present` leaves it alone, merging the declared quantities into
// whatever formation the app already runs. `set` declares the whole formation
// and scales every undeclared process type to zero.
//
// Two dokku behaviours bound what `set` can promise, and neither is detectable
// offline. An app whose app.json carries a `formations` key refuses manual
// scaling outright, and `skip_deploy` writes the formation without touching any
// containers, so an undeclared process type keeps running until the next
// deploy.
type PsScaleTask struct {
	// App is the name of the app
	App string `required:"true" identity:"key" yaml:"app" description:"Name of the app"`

	// Scale is a map of process types to quantities
	Scale map[string]int `required:"true" identity:"collection" yaml:"scale" description:"Map of process types to quantities"`

	// SkipDeploy skips the corresponding deploy. It is a *bool so the value
	// survives decoding unchanged; nil defaults to false.
	SkipDeploy *bool `yaml:"skip_deploy,omitempty" default:"false" description:"Skip the corresponding deploy"`

	// State is the desired state of the process scale
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,set" description:"Desired state of the process scale; 'set' declares the whole formation"`
}

// PsScaleTaskExample contains an example of a PsScaleTask
type PsScaleTaskExample struct {
	// Name is the task name holding the PsScaleTask description
	Name string `yaml:"-"`

	// PsScaleTask is the PsScaleTask configuration
	PsScaleTask PsScaleTask `yaml:"dokku_ps_scale"`
}

// GetName returns the name of the example
func (e PsScaleTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the ps scale task
func (t PsScaleTask) Doc() string {
	return "Manages the process scale for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t PsScaleTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t PsScaleTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// examples returns the examples for the ps scale task
func (t PsScaleTask) examples() ([]Doc, error) {
	return MarshalExamples([]PsScaleTaskExample{
		{
			Name: "Scale web and worker processes",
			PsScaleTask: PsScaleTask{
				App: "hello-world",
				Scale: map[string]int{
					"web":    2,
					"worker": 1,
				},
			},
		},
		{
			Name: "Scale web and worker processes without deploy",
			PsScaleTask: PsScaleTask{
				App:        "hello-world",
				SkipDeploy: boolPtr(true),
				Scale: map[string]int{
					"web":    4,
					"worker": 4,
				},
			},
		},
		{
			Name: "Declare the whole process formation",
			PsScaleTask: PsScaleTask{
				App: "hello-world",
				Scale: map[string]int{
					"web": 2,
				},
				State: StateSet,
			},
		},
	})
}

// Execute sets the process scale for a given dokku application
func (t PsScaleTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the PsScaleTask's inputs without contacting the server.
func (t PsScaleTask) Validate() error {
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	if t.State == StatePresent && len(t.Scale) == 0 {
		return fmt.Errorf("'scale' must not be empty for state 'present'")
	}
	// ps:scale --replace refuses a call carrying no process tuples, so state
	// 'set' needs a formation to declare just as much as state 'present' does.
	if t.State == StateSet && len(t.Scale) == 0 {
		return fmt.Errorf("'scale' must not be empty for state 'set'")
	}
	for _, proctype := range sortedScaleKeys(t.Scale) {
		if proctype == "" {
			return fmt.Errorf("'scale' must not contain an empty process type")
		}
		// getPsScale reads the formation back out of the ps:scale report, which
		// collapses every run of whitespace and splits each line on its first
		// colon, and dokku splits each tuple it is handed on the first '='. A
		// process type carrying any of those is writable but not readable, so
		// the task would plan the same change on every run instead of ever
		// reporting in sync. dokku only trims the tuple, so this is docket's
		// check to make.
		if strings.ContainsAny(proctype, "=:") || strings.ContainsFunc(proctype, unicode.IsSpace) {
			return fmt.Errorf("'scale' process types must not contain whitespace, '=' or ':', got %q", proctype)
		}
		// dokku parses the quantity with strconv.Atoi and stores whatever comes
		// back, so a negative count is written, reported, and handed to the
		// scheduler as-is. Refuse it here rather than converge on a formation
		// no scheduler can honor (dokku/dokku#9048).
		if qty := t.Scale[proctype]; qty < 0 {
			return fmt.Errorf("'scale' quantities must not be negative, got %d for scale[%q]", qty, proctype)
		}
	}
	return nil
}

// Plan reports the drift the PsScaleTask would produce.
func (t PsScaleTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planPsScalePresent(ctx, t) },
		StateSet:     func() PlanResult { return planPsScaleSet(ctx, t) },
	})
}

// planPsScalePresent reports drift for the present-state additive scale. Only
// the process types the recipe names are considered, so anything else the app
// runs keeps the quantity it has.
func planPsScalePresent(ctx context.Context, t PsScaleTask) PlanResult {
	existing, err := getPsScale(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	toScale := []string{}
	mutations := []string{}
	for _, proctype := range sortedScaleKeys(t.Scale) {
		qty := t.Scale[proctype]
		// A process type the report does not carry is not running, so a
		// declared quantity of zero is already true. Reading the missing entry
		// as zero rather than as "unknown" is what lets `web: 0` converge:
		// dokku files a zeroed process type the Procfile does not name under
		// the `scale.old` property, which the next deploy deletes, after which
		// the report stops mentioning it entirely.
		cur, reported := existing[proctype]
		if cur == qty {
			continue
		}
		toScale = append(toScale, fmt.Sprintf("%s=%d", proctype, qty))
		if reported {
			mutations = append(mutations, fmt.Sprintf("scale %s=%d (was %d)", proctype, qty, cur))
		} else {
			mutations = append(mutations, fmt.Sprintf("scale %s=%d (new)", proctype, qty))
		}
	}
	if len(toScale) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	inputs := psScaleInputs(t.App, false, boolValue(t.SkipDeploy, false), toScale)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusModify,
		Reason:    fmt.Sprintf("%d process scale change(s)", len(mutations)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}
}

// planPsScaleSet reports drift for the set-state whole-formation replacement.
//
// The command carries every declared tuple rather than only the drifted ones.
// `ps:scale --replace` rebuilds the stored formation out of the tuples it is
// handed and zeroes every process type it is not, so sending the drifted subset
// would zero the declared types that happened to already match.
//
// A process type reported at a quantity of zero counts as absent, on both
// sides. dokku keeps a zeroed process type visible in `ps:scale`: one the
// Procfile names is written back into the formation at zero, and one it does
// not moves to the `scale.old` property, which the report merges back in until
// a deploy clears it - and no deploy runs under `skip_deploy` or on an app that
// was never deployed. Counting a zeroed process type as drift would leave
// `state: set` planning the same change forever on those apps.
func planPsScaleSet(ctx context.Context, t PsScaleTask) PlanResult {
	existing, err := getPsScale(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	tuples := make([]string, 0, len(t.Scale))
	mutations := []string{}
	for _, proctype := range sortedScaleKeys(t.Scale) {
		qty := t.Scale[proctype]
		tuples = append(tuples, fmt.Sprintf("%s=%d", proctype, qty))
		cur, reported := existing[proctype]
		if cur == qty {
			continue
		}
		if reported {
			mutations = append(mutations, fmt.Sprintf("scale %s=%d (was %d)", proctype, qty, cur))
		} else {
			mutations = append(mutations, fmt.Sprintf("scale %s=%d (new)", proctype, qty))
		}
	}
	undeclared := []string{}
	for proctype, cur := range existing {
		if _, declared := t.Scale[proctype]; declared || cur == 0 {
			continue
		}
		undeclared = append(undeclared, proctype)
	}
	// existing is a map, so this sort is what makes the mutation list
	// deterministic across runs rather than cosmetic (issue #341).
	sort.Strings(undeclared)
	for _, proctype := range undeclared {
		mutations = append(mutations, fmt.Sprintf("scale %s=0 (was %d, undeclared)", proctype, existing[proctype]))
	}
	if len(mutations) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	inputs := psScaleInputs(t.App, true, boolValue(t.SkipDeploy, false), tuples)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusModify,
		Reason:    fmt.Sprintf("%d process scale change(s)", len(mutations)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StateSet, inputs)
		},
	}
}

// psScaleInputs builds the ps:scale call a plan applies. Flags precede the app
// name because ps:scale parses with Go's flag package, which stops at the first
// positional argument - `ps:scale <app> --replace` would be read as a process
// tuple rather than as a flag. Unlike the probe the call omits --quiet, since
// ps:scale streams the deploy it triggers.
func psScaleInputs(app string, replace bool, skipDeploy bool, tuples []string) []subprocess.ExecCommandInput {
	args := []string{"ps:scale"}
	if replace {
		args = append(args, "--replace")
	}
	if skipDeploy {
		args = append(args, "--skip-deploy")
	}
	args = append(args, app)
	args = append(args, tuples...)
	return []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
}

// sortedScaleKeys returns a scale map's process types in sorted order so the
// ps:scale args and the mutation list are deterministic across runs (#341).
func sortedScaleKeys(scale map[string]int) []string {
	proctypes := make([]string, 0, len(scale))
	for proctype := range scale {
		proctypes = append(proctypes, proctype)
	}
	sort.Strings(proctypes)
	return proctypes
}

// ExportApp reads the app's process scale and returns a dokku_ps_scale task
// declaring the whole formation, or nil when nothing is scaled above zero (an
// undeployed app has nothing to set).
//
// Zero-quantity process types are dropped rather than emitted: under state
// 'set' an omitted process type already means zero, and the zeroes dokku
// reports are not stable. A process type scaled down under skip_deploy sits in
// the `scale.old` property until the next deploy deletes it, so emitting them
// would make two exports of the same server differ on deploy timing alone.
func (t PsScaleTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	scale, err := getPsScale(ctx, app)
	if err != nil {
		return nil, err
	}
	running := map[string]int{}
	for proctype, qty := range scale {
		if qty > 0 {
			running[proctype] = qty
		}
	}
	if len(running) == 0 {
		return nil, nil
	}
	return []interface{}{PsScaleTask{App: app, Scale: running, State: StateSet}}, nil
}

// getPsScale retrieves the current process scale for a given dokku application
func getPsScale(ctx context.Context, app string) (map[string]int, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "ps:scale", app},
	})
	if err != nil {
		return nil, err
	}

	scale := map[string]int{}
	for _, line := range strings.Split(result.StdoutContents(), "\n") {
		// strip all whitespace from the line, matching the upstream ansible module
		line = strings.Join(strings.Fields(line), "")
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		qty, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		scale[parts[0]] = qty
	}
	return scale, nil
}

// init registers the PsScaleTask with the task registry
func init() {
	RegisterTask(&PsScaleTask{})
}
