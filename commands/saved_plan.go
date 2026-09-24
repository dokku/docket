package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/dokku/docket/subprocess"
	"github.com/dokku/docket/tasks"
)

// savedPlanFormat is the `format` marker every saved plan carries, so apply
// can tell a saved plan from any other JSON document handed to --plan.
const savedPlanFormat = "docket-plan"

// savedPlanVersion is the saved plan's own schema version, independent of the
// event stream's. docs/schemas/plan-v1.schema.json describes it.
const savedPlanVersion = 1

// savedPlanMode is the mode a saved plan is written with. It holds the recipe
// and every resolved input value in the clear, sensitive ones included, so its
// only reader should be the user who applies it - the same reasoning as
// export's varsFileMode.
const savedPlanMode = 0o600

// savedPlanStaleLimit caps how many differing events the stale report lists.
// One is usually enough to see why; a whole recipe of them is noise.
const savedPlanStaleLimit = 5

// savedPlan is what `docket plan --output` writes and `docket apply --plan`
// reads back (#408). It freezes everything that decides what a run does -
// the recipe bytes, the resolved inputs, the filters and the target - plus
// the plan's own events, which apply compares against a fresh probe before it
// runs anything.
type savedPlan struct {
	Format        string                    `json:"format"`
	Version       int                       `json:"version"`
	DocketVersion string                    `json:"docket_version"`
	CreatedAt     string                    `json:"created_at"`
	Recipe        savedPlanRecipe           `json:"recipe"`
	Inputs        map[string]savedPlanInput `json:"inputs"`
	Play          string                    `json:"play"`
	Tags          []string                  `json:"tags"`
	SkipTags      []string                  `json:"skip_tags"`
	Target        savedPlanTarget           `json:"target"`
	Events        []savedPlanEvent          `json:"events"`
}

// savedPlanRecipe is the recipe the plan was made from. Data is parsed on
// apply, so a URL or stdin recipe is never read a second time; Source is only
// ever displayed.
type savedPlanRecipe struct {
	Source string `json:"source"`
	Format string `json:"format"`
	Data   string `json:"data"`
}

// savedPlanInput is one resolved input. Value is the string spelling of the
// value the recipe saw, re-parsed on load according to Type so an int does not
// come back from JSON as a float64. Sensitive, HasDefault and UserSet carry the
// rest of what buildInputContext needs to decide what the masker registers.
type savedPlanInput struct {
	Type       string `json:"type"`
	Value      string `json:"value"`
	Sensitive  bool   `json:"sensitive"`
	HasDefault bool   `json:"has_default"`
	UserSet    bool   `json:"user_set"`
}

// savedPlanTarget is the run-wide target the plan probed. A play's own
// `host:` / `sudo:` override still comes from the recipe.
type savedPlanTarget struct {
	Host              string `json:"host"`
	Sudo              bool   `json:"sudo"`
	AcceptNewHostKeys bool   `json:"accept_new_host_keys"`
}

// savedPlanEvent is one entry in a plan's fingerprint: the plan stream in
// order, without timing and warnings, and masked the way the stream is.
// Error is only ever set on an event recorded while apply checks a saved
// plan - a plan with errors is never saved - so the stale report can say
// what went wrong.
type savedPlanEvent struct {
	Type      string   `json:"type"`
	Play      string   `json:"play,omitempty"`
	Name      string   `json:"name"`
	Host      string   `json:"host,omitempty"`
	When      string   `json:"when,omitempty"`
	Status    string   `json:"status,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	Mutations []string `json:"mutations,omitempty"`
	Commands  []string `json:"commands,omitempty"`
	Phase     string   `json:"phase,omitempty"`
	Group     bool     `json:"group,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// recordingEmitter forwards every event to the emitter it wraps and records
// the plan events as a saved plan's fingerprint. Plan wraps its visible
// emitter in one to write --output; apply wraps a discardEmitter in one to
// check a saved plan without printing the probe.
type recordingEmitter struct {
	EventEmitter
	masker *subprocess.Masker
	events []savedPlanEvent
}

// newRecordingEmitter wraps inner, masking recorded text with masker.
func newRecordingEmitter(inner EventEmitter, masker *subprocess.Masker) *recordingEmitter {
	return &recordingEmitter{EventEmitter: inner, masker: masker}
}

// PlayStart records the play header and forwards it.
func (r *recordingEmitter) PlayStart(name, host string) {
	r.events = append(r.events, savedPlanEvent{
		Type: "play_start",
		Name: r.masker.String(name),
		Host: host,
	})
	r.EventEmitter.PlayStart(name, host)
}

// PlaySkipped records the skipped play and forwards it.
func (r *recordingEmitter) PlaySkipped(name, whenSrc string) {
	r.events = append(r.events, savedPlanEvent{
		Type: "play_skipped",
		Name: r.masker.String(name),
		When: r.masker.String(whenSrc),
	})
	r.EventEmitter.PlaySkipped(name, whenSrc)
}

// PlanTask records the task's verdict and forwards it. The fields follow the
// JSON plan event's, so what a saved plan fingerprints is what `plan --json`
// shows.
func (r *recordingEmitter) PlanTask(ev PlanTaskEvent) {
	rec := savedPlanEvent{
		Type:  "task",
		Play:  r.masker.String(ev.Play),
		Name:  r.masker.String(ev.Name),
		Phase: ev.Phase,
		Group: ev.Group,
	}
	switch {
	case ev.WhenError != nil:
		rec.Status = "error"
		rec.Error = r.masker.String(ev.WhenError.Error())
	case ev.Skipped:
		rec.Status = "skipped"
	case ev.Result.Error != nil:
		rec.Status = "error"
		rec.Error = r.masker.String(PrefixErrorMessage(ev.Result.Error))
	case ev.Result.InSync:
		rec.Status = string(tasks.PlanStatusOK)
	default:
		rec.Status = string(ev.Result.Status)
		if rec.Status == "" {
			rec.Status = string(tasks.PlanStatusModify)
		}
		rec.Reason = r.masker.String(ev.Result.Reason)
		rec.Mutations = maskedStrings(r.masker, ev.Result.Mutations)
		rec.Commands = maskedStrings(r.masker, ev.Result.Commands)
	}
	r.events = append(r.events, rec)
	r.EventEmitter.PlanTask(ev)
}

// discardEmitter drops every event. Apply checks a saved plan through it, so
// the probe that decides whether the plan is stale prints nothing of its own.
type discardEmitter struct{}

func (discardEmitter) PlayStart(string, string)                   {}
func (discardEmitter) PlaySkipped(string, string)                 {}
func (discardEmitter) ApplyTask(ApplyTaskEvent)                   {}
func (discardEmitter) PlanTask(PlanTaskEvent)                     {}
func (discardEmitter) TaskWarning(string, string, string, string) {}
func (discardEmitter) ApplySummary(ApplyCounts, time.Duration)    {}
func (discardEmitter) PlanSummary(PlanCounts, time.Duration)      {}

// newSavedPlan assembles the document plan --output writes.
func newSavedPlan(version string, recipe recipeSource, arguments map[string]*Argument, userSet map[string]bool, play string, tags, skipTags []string, target subprocess.Target, events []savedPlanEvent) *savedPlan {
	inputs := make(map[string]savedPlanInput, len(arguments))
	for name, arg := range arguments {
		inputs[name] = savedPlanInput{
			Type:       arg.Type,
			Value:      contextValueSpelling(arg.ContextValue()),
			Sensitive:  arg.Sensitive,
			HasDefault: arg.HasDefault,
			UserSet:    userSet[name],
		}
	}
	if events == nil {
		events = []savedPlanEvent{}
	}
	return &savedPlan{
		Format:        savedPlanFormat,
		Version:       savedPlanVersion,
		DocketVersion: version,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Recipe: savedPlanRecipe{
			Source: recipe.Display,
			Format: recipe.Format,
			Data:   string(recipe.Data),
		},
		Inputs:   inputs,
		Play:     play,
		Tags:     nonNilStrings(tags),
		SkipTags: nonNilStrings(skipTags),
		Target: savedPlanTarget{
			Host:              target.Host,
			Sudo:              target.Sudo,
			AcceptNewHostKeys: target.AcceptNewHostKeys,
		},
		Events: events,
	}
}

// contextValueSpelling renders an input's resolved value as the string a
// saved plan stores. Floats use the shortest spelling that parses back to the
// same value, so the round trip is exact.
func contextValueSpelling(v interface{}) string {
	switch v := v.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

// nonNilStrings returns s, or an empty slice for nil, so a saved plan writes
// `[]` rather than `null`.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// arguments rebuilds the argument set the plan resolved, and which of its
// inputs the user supplied. Handing both to buildInputContext resolves the
// render context and the masker's values by exactly the rules plan used.
func (p *savedPlan) arguments() (map[string]*Argument, map[string]bool, error) {
	arguments := make(map[string]*Argument, len(p.Inputs))
	userSet := make(map[string]bool, len(p.Inputs))
	for name, in := range p.Inputs {
		arg := &Argument{Sensitive: in.Sensitive, HasDefault: in.HasDefault, Type: in.Type}
		switch in.Type {
		case "int":
			v, ok := tasks.ParseInputInt(in.Value)
			if !ok {
				return nil, nil, fmt.Errorf("saved plan input %q: %q is not an int", name, in.Value)
			}
			arg.SetIntValue(&v)
		case "float":
			v, ok := tasks.ParseInputFloat(in.Value)
			if !ok {
				return nil, nil, fmt.Errorf("saved plan input %q: %q is not a float", name, in.Value)
			}
			arg.SetFloatValue(&v)
		case "bool":
			v, ok := tasks.ParseInputBool(in.Value)
			if !ok {
				return nil, nil, fmt.Errorf("saved plan input %q: %q is not a bool", name, in.Value)
			}
			arg.SetBoolValue(&v)
		case "string":
			v := in.Value
			arg.SetStringValue(&v)
		default:
			return nil, nil, fmt.Errorf("saved plan input %q has unknown type %q", name, in.Type)
		}
		arguments[name] = arg
		if in.UserSet {
			userSet[name] = true
		}
	}
	return arguments, userSet, nil
}

// target returns the run-wide target the plan probed.
func (p *savedPlan) target() subprocess.Target {
	return subprocess.Target{
		Host:              p.Target.Host,
		Sudo:              p.Target.Sudo,
		AcceptNewHostKeys: p.Target.AcceptNewHostKeys,
	}
}

// recipe returns the saved recipe in the shape loadRecipe would have.
func (p *savedPlan) recipe() recipeSource {
	return recipeSource{
		Path:    p.Recipe.Source,
		Display: p.Recipe.Source,
		Data:    []byte(p.Recipe.Data),
		Format:  p.Recipe.Format,
	}
}

// marshalSavedPlan renders p as indented JSON with a trailing newline. HTML
// escaping is off: the recipe is shown to nobody's browser, and `<`, `>` and
// `&` are common in shell commands.
func marshalSavedPlan(p *savedPlan) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeSavedPlan writes p to path at savedPlanMode. Like export's vars-file,
// the mode is set on the descriptor before the first byte lands: O_CREATE's
// mode only applies to a file the call creates, so a --force over an existing
// 0o644 file would otherwise keep that mode. A filesystem that cannot hold the
// bits warns rather than failing, since the plan is still what was asked for.
func writeSavedPlan(path string, p *savedPlan, warn func(string)) error {
	data, err := marshalSavedPlan(p)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, savedPlanMode)
	if err != nil {
		return err
	}
	if err := f.Chmod(savedPlanMode); err != nil {
		warn(fmt.Sprintf("warning: could not set mode %#o on %s: %v; it holds secrets in the clear", savedPlanMode, path, err))
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// readSavedPlan loads and checks the saved plan at path, resolved against
// baseDir; messages name path as the user typed it. version is the running
// docket's own version: a plan is applied only by the build that wrote it,
// because what a task probes and how it applies can change between any two
// releases. The returned warnings are for the caller to print.
func readSavedPlan(baseDir, path, version string) (*savedPlan, []string, error) {
	data, err := os.ReadFile(inDir(baseDir, path))
	if err != nil {
		return nil, nil, fmt.Errorf("read error: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p savedPlan
	if err := dec.Decode(&p); err != nil {
		return nil, nil, fmt.Errorf("%s is not a docket saved plan: %v", path, err)
	}
	if p.Format != savedPlanFormat {
		return nil, nil, fmt.Errorf("%s is not a docket saved plan (format %q, want %q)", path, p.Format, savedPlanFormat)
	}
	if p.Version != savedPlanVersion {
		return nil, nil, fmt.Errorf("%s is saved plan version %d; this docket reads version %d", path, p.Version, savedPlanVersion)
	}
	if p.DocketVersion != version {
		return nil, nil, fmt.Errorf("%s was saved by docket %s, but this is docket %s; run docket plan again with this version", path, displayDocketVersion(p.DocketVersion), displayDocketVersion(version))
	}
	return &p, savedPlanWarnings(inDir(baseDir, path), path), nil
}

// docketVersion returns the running docket's version: the one a command was
// handed, or else the CLI_VERSION cli-skeleton exports at startup.
func docketVersion(v string) string {
	if v != "" {
		return v
	}
	return os.Getenv("CLI_VERSION")
}

// displayDocketVersion names a docket version in a message. A build without
// one (go run, go test) has an empty version.
func displayDocketVersion(v string) string {
	if v == "" {
		return "(unversioned build)"
	}
	return v
}

// savedPlanWarnings warns when the saved plan at resolved is readable by users
// other than its owner. A saved plan always holds the recipe's resolved
// inputs, so unlike varsFileWarnings the trigger is the mode alone. It stays
// a warning for the same reason: the file is the user's, and apply only reads
// it. The message names path, the spelling the user typed.
func savedPlanWarnings(resolved, path string) []string {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	perm := info.Mode().Perm()
	if perm&0o077 == 0 {
		return nil
	}
	return []string{fmt.Sprintf(
		"warning: saved plan %s holds secrets and is readable by other users (mode %04o); chmod 600 %s",
		path, perm, shellQuotePath(path))}
}

// staleSavedPlanDiff compares the events a saved plan recorded with the ones
// a fresh probe just produced, and describes up to savedPlanStaleLimit of the
// differences. An empty result means the plan still holds.
func staleSavedPlanDiff(saved, now []savedPlanEvent) []string {
	var out []string
	n := len(saved)
	if len(now) > n {
		n = len(now)
	}
	for i := 0; i < n; i++ {
		var s, c *savedPlanEvent
		if i < len(saved) {
			s = &saved[i]
		}
		if i < len(now) {
			c = &now[i]
		}
		if s != nil && c != nil && reflect.DeepEqual(*s, *c) {
			continue
		}
		out = append(out, describeEventDiff(s, c))
	}
	if len(out) > savedPlanStaleLimit {
		more := len(out) - savedPlanStaleLimit
		out = append(out[:savedPlanStaleLimit], fmt.Sprintf("... and %d more", more))
	}
	return out
}

// describeEventDiff renders one differing pair as
// `<label>: planned "<verdict>", now "<verdict>"`. A nil side is an event
// one run produced and the other did not.
func describeEventDiff(saved, now *savedPlanEvent) string {
	labelled := saved
	if labelled == nil {
		labelled = now
	}
	label := eventLabel(labelled)
	planned, current := eventVerdict(saved), eventVerdict(now)
	if planned == current {
		return fmt.Sprintf("%s: %q, but its mutations or commands differ", label, planned)
	}
	return fmt.Sprintf("%s: planned %q, now %q", label, planned, current)
}

// eventLabel names the event a diff line is about.
func eventLabel(ev *savedPlanEvent) string {
	switch ev.Type {
	case "play_start", "play_skipped":
		return "play " + ev.Name
	}
	name := ev.Name
	if ev.Phase != "" {
		name = fmt.Sprintf("[%s] %s", ev.Phase, name)
	}
	return ev.Play + "/" + name
}

// eventVerdict summarises an event for a diff line.
func eventVerdict(ev *savedPlanEvent) string {
	if ev == nil {
		return "absent"
	}
	switch ev.Type {
	case "play_start":
		if ev.Host != "" {
			return "runs on " + ev.Host
		}
		return "runs"
	case "play_skipped":
		return "skipped"
	}
	switch {
	case ev.Error != "":
		return "error: " + ev.Error
	case ev.Reason != "":
		return ev.Status + " " + ev.Reason
	}
	return ev.Status
}

// planFlagFromArgs reports whether argv carries apply's --plan flag. Like
// tasksFormatFromArgs it runs before pflag parses, so apply can skip reading
// a recipe to register input flags from: a saved plan brings its inputs with
// it, and an input flag typed beside --plan should fail as unknown rather than
// be quietly ignored.
func planFlagFromArgs(s []string) bool {
	for _, arg := range s {
		if arg == "--" {
			return false
		}
		if arg == "--plan" || strings.HasPrefix(arg, "--plan=") {
			return true
		}
	}
	return false
}
