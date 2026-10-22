package tasks

import (
	"reflect"
	"strings"
	"testing"
)

// noteOf returns the note of the given kind, or nil when the task has none.
func noteOf(notes []NoteSchema, kind NoteKind) *NoteSchema {
	for i := range notes {
		if notes[i].Kind == kind {
			return &notes[i]
		}
	}
	return nil
}

// schemaWithStates is a TaskSchema whose `state` field offers states, plus
// whatever the caller sets on it.
func schemaWithStates(states ...string) TaskSchema {
	return TaskSchema{Fields: []FieldSchema{{Name: "state", Type: TypeString, Choices: states}}}
}

// TestFieldNoteSuffixes pins each field note, its wording and its order, and
// that the suffix is appended to the description as is (#651). The cases are
// the ones the docs generator pinned before the prose moved here.
func TestFieldNoteSuffixes(t *testing.T) {
	ordered, unordered := true, false
	items := &ItemSchema{Type: TypeObject, Fields: []FieldSchema{{Name: "username"}, {Name: "password"}}}
	cases := []struct {
		field FieldSchema
		want  string
	}{
		{FieldSchema{Description: "Path to a file.", RunnerFile: true, Sensitive: true}, "Path to a file. (read from the machine running docket) (sensitive)"},
		{FieldSchema{Description: "Registry password", WriteOnly: true, Sensitive: true}, "Registry password (not returned when reading state) (sensitive)"},
		{FieldSchema{Description: "Restart the app", ApplyOnly: true}, "Restart the app (applied only, not read back)"},
		{FieldSchema{Description: "List of buildpack URLs", Ordered: &ordered}, "List of buildpack URLs (order is significant)"},
		{FieldSchema{Description: "Networks", Ordered: &ordered, Sensitive: true}, "Networks (order is significant) (sensitive)"},
		{FieldSchema{Description: "List of domain names", Ordered: &unordered}, "List of domain names"},
		{FieldSchema{Description: "Name of the app"}, "Name of the app"},
		{FieldSchema{Description: "Netrc username", RequiredWhen: []string{"present"}}, "Netrc username (required when state is present)"},
		{FieldSchema{Description: "Domains", RequiredWhen: []string{"present", "absent"}, ForbiddenWhen: []string{"clear"}}, "Domains (required when state is present or absent) (must not be set when state is clear)"},
		{FieldSchema{Description: "Buildpacks", Ordered: &ordered, RequiredWhen: []string{"present"}}, "Buildpacks (order is significant) (required when state is present)"},
		{FieldSchema{Description: "Password", ForbiddenWhen: []string{"absent", "set", "clear"}, Sensitive: true}, "Password (must not be set when state is absent, set or clear) (sensitive)"},
		{FieldSchema{Description: "Users", Type: TypeList, Item: items, Ordered: &unordered}, "Users. Each item has: username, password."},
		{FieldSchema{Description: "Users.", Type: TypeList, Item: items, Ordered: &ordered}, "Users. Each item has: username, password. (order is significant)"},
		{FieldSchema{Type: TypeList, Item: items}, "Each item has: username, password."},
		{FieldSchema{Sensitive: true}, "(sensitive)"},
		{FieldSchema{Description: "Values", Type: TypeDict, Item: &ItemSchema{Type: TypeObject, Fields: []FieldSchema{{Name: "a"}}}}, "Values"},
	}
	for _, tc := range cases {
		if got := tc.field.Description + fieldNoteSuffix(tc.field, ""); got != tc.want {
			t.Errorf("description with note = %q; want %q", got, tc.want)
		}
	}
}

// TestFieldNotesByState pins how a state-dependent field note reads for a
// consumer that drives one state: the condition becomes a plain
// "(required)" or "(must not be set)" in a state it names, and drops out in
// any other. A note that does not depend on the state carries no states.
func TestFieldNotesByState(t *testing.T) {
	states := []State{StatePresent, StateAbsent, StateSet, StateClear}
	fields := []FieldSchema{
		{Name: "domains", Description: "Domains", RequiredWhen: []string{"present", "absent"}, ForbiddenWhen: []string{"clear"}, Sensitive: true},
		{Name: "password", Description: "Password", Sensitive: true},
		{Name: "app", Description: "Name of the app"},
		{Name: "users", Type: TypeList, Item: &ItemSchema{Type: TypeObject, Fields: []FieldSchema{
			{Name: "hash", Description: "Hash", RequiredWhen: []string{"set"}},
		}}},
	}
	applyFieldNotes(fields, states)

	want := map[string]string{
		"present": " (required) (sensitive)",
		"absent":  " (required) (sensitive)",
		"set":     " (sensitive)",
		"clear":   " (must not be set) (sensitive)",
	}
	if got := fields[0].Note; got == nil || got.Suffix != " (required when state is present or absent) (must not be set when state is clear) (sensitive)" || !reflect.DeepEqual(got.States, want) {
		t.Errorf("domains note = %+v; want suffix for every state and states %v", got, want)
	}
	if got := fields[1].Note; got == nil || got.Suffix != " (sensitive)" || got.States != nil {
		t.Errorf("password note = %+v; want a suffix with no states", got)
	}
	if got := fields[2].Note; got != nil {
		t.Errorf("app note = %+v; want none", got)
	}
	hash := fields[3].Item.Fields[0].Note
	wantHash := map[string]string{"present": "", "absent": "", "set": " (required)", "clear": ""}
	if hash == nil || !reflect.DeepEqual(hash.States, wantHash) {
		t.Errorf("users[].hash note = %+v; want states %v keyed by the task's states", hash, wantHash)
	}
}

// TestRunnerRequirementsNote covers the note the catalog's runner_file flag
// renders into (#568), including an element field, which the Parameters
// table only lists by name and so has no other place to be called out.
func TestRunnerRequirementsNote(t *testing.T) {
	if got := runnerRequirementsNote(TaskSchema{Fields: []FieldSchema{{Name: "app"}}}, ""); got != "" {
		t.Errorf("untagged fields rendered %q; want no note", got)
	}

	got := runnerRequirementsNote(TaskSchema{Fields: []FieldSchema{
		{Name: "app"},
		{Name: "tarball", RunnerFile: true},
		{Name: "pages", Type: TypeList, Item: &ItemSchema{
			Type:   TypeObject,
			Fields: []FieldSchema{{Name: "name"}, {Name: "file", RunnerFile: true}},
		}},
	}}, "")
	want := "## Runner requirements\n\n" +
		"- `tarball` is read from the machine running docket, not the dokku server, so the file must exist there even when docket drives the server over `--host`.\n" +
		"- `pages[].file` is read from the machine running docket, not the dokku server, so the file must exist there even when docket drives the server over `--host`."
	if got != want {
		t.Errorf("runnerRequirementsNote =\n%s\nwant\n%s", got, want)
	}
}

// constraintsFixture declares one constraint of every kind, two of them
// limited to states, and two on a structured list's element.
func constraintsFixture() TaskSchema {
	schema := schemaWithStates("present", "absent", "set", "clear")
	schema.Constraints = []Constraint{
		{Kind: ConstraintExactlyOneOf, Fields: []string{"app", "global"}},
		{Kind: ConstraintAtLeastOneOf, Fields: []string{"cert", "cert_content"}, States: []State{StatePresent}},
		{Kind: ConstraintAllOrNoneOf, Fields: []string{"git_username", "git_email"}},
		{Kind: ConstraintConflictsWith, Field: "mounts", Fields: []string{"entry_name", "host_dir", "container_dir"}},
		{Kind: ConstraintRequiredWith, Field: "endpoint_url", Fields: []string{"aws_signature_version"}, States: []State{StatePresent, StateSet}},
	}
	schema.Fields = append(schema.Fields, FieldSchema{Name: "users", Item: &ItemSchema{Type: TypeObject, Constraints: []Constraint{
		{Kind: ConstraintAtLeastOneOf, Fields: []string{"password", "hash"}},
		{Kind: ConstraintConflictsWith, Field: "password", Fields: []string{"hash"}, States: []State{StateClear}},
	}}})
	return schema
}

// TestConstraintsNote pins the sentence each constraint kind renders, task
// constraints first and then each structured list's (#620).
func TestConstraintsNote(t *testing.T) {
	want := "## Constraints\n\n" +
		"- Exactly one of `app` or `global` must be set.\n" +
		"- At least one of `cert` or `cert_content` must be set when `state` is `present`.\n" +
		"- `git_username` and `git_email` must be set together or not at all.\n" +
		"- `mounts` must not be set together with `entry_name`, `host_dir` or `container_dir`.\n" +
		"- `endpoint_url` requires `aws_signature_version` when `state` is `present` or `set`.\n" +
		"- Each `users` item: at least one of `password` or `hash` must be set.\n" +
		"- Each `users` item: `password` must not be set together with `hash` when `state` is `clear`."
	if got := constraintsNote(constraintsFixture(), ""); got != want {
		t.Errorf("constraintsNote =\n%s\nwant\n%s", got, want)
	}
	if got := constraintsNote(TaskSchema{}, ""); got != "" {
		t.Errorf("constraintsNote with no constraints = %q, want empty", got)
	}
}

// TestConstraintsNoteByState pins the per-state rendering: only the rules
// that apply in the state, without their state clause.
func TestConstraintsNoteByState(t *testing.T) {
	note := noteOf(buildTaskNotes(constraintsFixture()), NoteConstraints)
	if note == nil {
		t.Fatal("no constraints note")
	}
	want := map[string]string{
		"present": "## Constraints\n\n" +
			"- Exactly one of `app` or `global` must be set.\n" +
			"- At least one of `cert` or `cert_content` must be set.\n" +
			"- `git_username` and `git_email` must be set together or not at all.\n" +
			"- `mounts` must not be set together with `entry_name`, `host_dir` or `container_dir`.\n" +
			"- `endpoint_url` requires `aws_signature_version`.\n" +
			"- Each `users` item: at least one of `password` or `hash` must be set.",
		"absent": "## Constraints\n\n" +
			"- Exactly one of `app` or `global` must be set.\n" +
			"- `git_username` and `git_email` must be set together or not at all.\n" +
			"- `mounts` must not be set together with `entry_name`, `host_dir` or `container_dir`.\n" +
			"- Each `users` item: at least one of `password` or `hash` must be set.",
		"set": "## Constraints\n\n" +
			"- Exactly one of `app` or `global` must be set.\n" +
			"- `git_username` and `git_email` must be set together or not at all.\n" +
			"- `mounts` must not be set together with `entry_name`, `host_dir` or `container_dir`.\n" +
			"- `endpoint_url` requires `aws_signature_version`.\n" +
			"- Each `users` item: at least one of `password` or `hash` must be set.",
		"clear": "## Constraints\n\n" +
			"- Exactly one of `app` or `global` must be set.\n" +
			"- `git_username` and `git_email` must be set together or not at all.\n" +
			"- `mounts` must not be set together with `entry_name`, `host_dir` or `container_dir`.\n" +
			"- Each `users` item: at least one of `password` or `hash` must be set.\n" +
			"- Each `users` item: `password` must not be set together with `hash`.",
	}
	for state, text := range want {
		if got := note.States[state]; got != text {
			t.Errorf("constraints in state %s =\n%s\nwant\n%s", state, got, text)
		}
	}
	if len(note.States) != len(want) {
		t.Errorf("constraints states = %v; want one per state", note.States)
	}

	// A state no rule applies in renders as empty, so the consumer omits it.
	only := schemaWithStates("present", "absent")
	only.Constraints = []Constraint{{Kind: ConstraintAtLeastOneOf, Fields: []string{"a", "b"}, States: []State{StatePresent}}}
	note = noteOf(buildTaskNotes(only), NoteConstraints)
	if note == nil || note.States["absent"] != "" || note.States["present"] == "" {
		t.Errorf("state-limited constraints note = %+v; want an empty absent rendering", note)
	}
}

// TestObjectsNote pins how a task page names the objects a task changes, and
// each condition a restart can carry.
func TestObjectsNote(t *testing.T) {
	schema := TaskSchema{Objects: []ObjectSchema{
		{Kind: ObjectApp, Keys: []string{"app"}, Creates: true, Restarts: &Restart{Unless: []string{"skip_deploy"}}},
		{Kind: ObjectService, Keys: []string{"service", "name"}, Restarts: &Restart{}},
		{Kind: ObjectApp, Keys: []string{"app"}, Restarts: &Restart{When: []string{"build", "build_if_changes"}, States: []State{StatePresent}}},
	}}
	want := "## Objects\n\n" +
		"- The app named by `app`. Creates it, and destroys it in state `absent`. Can restart or redeploy it unless `skip_deploy` is true.\n" +
		"- The datastore service named by `service` and `name`. Can restart or redeploy it.\n" +
		"- The app named by `app`. Can restart or redeploy it when `build` or `build_if_changes` is true, and when `state` is `present`.\n\n" +
		"Tasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently)."
	if got := objectsNote(schema, ""); got != want {
		t.Errorf("objectsNote =\n%s\nwant\n%s", got, want)
	}
	if got := objectsNote(TaskSchema{}, ""); got != "" {
		t.Errorf("objectsNote with no objects = %q, want empty", got)
	}
}

// TestObjectsNoteByState pins the per-state rendering: a creating task says
// which of creating and destroying it does, and a restart limited to other
// states drops out while one in this state loses its state clause.
func TestObjectsNoteByState(t *testing.T) {
	schema := schemaWithStates("present", "absent")
	schema.Objects = []ObjectSchema{
		{Kind: ObjectApp, Keys: []string{"app"}, Creates: true},
		{Kind: ObjectService, Keys: []string{"service", "name"}, Restarts: &Restart{When: []string{"restart"}, States: []State{StatePresent}}},
	}
	note := noteOf(buildTaskNotes(schema), NoteObjects)
	if note == nil {
		t.Fatal("no objects note")
	}
	tail := "\n\nTasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently)."
	want := map[string]string{
		"present": "## Objects\n\n- The app named by `app`. Creates it.\n- The datastore service named by `service` and `name`. Can restart or redeploy it when `restart` is true." + tail,
		"absent":  "## Objects\n\n- The app named by `app`. Destroys it.\n- The datastore service named by `service` and `name`." + tail,
	}
	if !reflect.DeepEqual(note.States, want) {
		t.Errorf("objects states =\n%v\nwant\n%v", note.States, want)
	}
}

// TestTaskNotesWithoutStateDependence asserts a note whose text is the same
// in every state carries no per-state copy, so `states` marks exactly the
// notes a consumer driving one state has to choose between.
func TestTaskNotesWithoutStateDependence(t *testing.T) {
	schema := schemaWithStates("present", "absent")
	schema.Deprecation = "Use dokku_other instead."
	schema.MinimumDokkuVersion = "0.38.0"
	schema.Export = ExportSupport{Status: ExportSupported}
	schema.Probe = ProbeSupport{Status: ProbePartial, Caveat: "reads only <service>:info"}
	schema.Identity = IdentitySchema{Keys: []string{"app"}}
	schema.Objects = []ObjectSchema{{Kind: ObjectApp, Keys: []string{"app"}, Restarts: &Restart{}}}

	notes := buildTaskNotes(schema)
	var kinds []NoteKind
	for _, note := range notes {
		kinds = append(kinds, note.Kind)
		if note.States != nil {
			t.Errorf("%s note carries states %v; want none", note.Kind, note.States)
		}
	}
	wantKinds := []NoteKind{NoteDeprecation, NoteRequirements, NoteExport, NoteProbe, NoteIdentity, NoteObjects}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Errorf("kinds = %v; want %v in page order", kinds, wantKinds)
	}
	if probe := noteOf(notes, NoteProbe); probe == nil || probe.Markdown != "## Probe support\n\nPartial - reads only &lt;service>:info." {
		t.Errorf("probe note = %+v; want the caveat escaped as the page prints it", probe)
	}
}

// TestCatalogNotesCoverEveryState asserts, across the real catalog, that
// every per-state rendering is keyed by exactly the task's states, that a
// state-dependent note never renders text in one state that it lacks overall,
// and that every note kind appears at most once and in page order.
func TestCatalogNotesCoverEveryState(t *testing.T) {
	order := map[NoteKind]int{}
	for i, kind := range taskNoteKinds {
		order[kind.kind] = i
	}
	for _, schema := range wholeCatalog(t).Tasks {
		states := noteStates(schema.Fields)
		checkStates := func(where string, byState map[string]string) {
			if byState == nil {
				return
			}
			if len(byState) != len(states) {
				t.Errorf("%s: %s states %v; want one per state %v", schema.Type, where, byState, states)
			}
			for _, state := range states {
				if _, ok := byState[string(state)]; !ok {
					t.Errorf("%s: %s has no rendering for state %s", schema.Type, where, state)
				}
			}
		}

		last := -1
		for _, note := range schema.Notes {
			if note.Markdown == "" || strings.HasSuffix(note.Markdown, "\n") {
				t.Errorf("%s: %s note %q is empty or ends in a newline", schema.Type, note.Kind, note.Markdown)
			}
			position, known := order[note.Kind]
			if !known || position <= last {
				t.Errorf("%s: %s note is unknown, repeated or out of page order", schema.Type, note.Kind)
			}
			last = position
			checkStates(string(note.Kind)+" note", note.States)
		}

		var walk func(prefix string, fields []FieldSchema)
		walk = func(prefix string, fields []FieldSchema) {
			for _, field := range fields {
				if field.Note != nil {
					if field.Note.Suffix == "" {
						t.Errorf("%s: %s%s has an empty note", schema.Type, prefix, field.Name)
					}
					checkStates(prefix+field.Name+" note", field.Note.States)
				}
				if field.Item != nil {
					walk(prefix+field.Name+"[].", field.Item.Fields)
				}
			}
		}
		walk("", schema.Fields)
	}
}

// TestCatalogPublishesStateKeyedNotes spot-checks real tasks whose notes
// depend on the state.
func TestCatalogPublishesStateKeyedNotes(t *testing.T) {
	catalog, err := CatalogFor([]string{"dokku_app", "dokku_domains", "dokku_domains_toggle"})
	if err != nil {
		t.Fatalf("CatalogFor: %v", err)
	}
	byType := map[string]TaskSchema{}
	for _, schema := range catalog.Tasks {
		byType[schema.Type] = schema
	}

	app := noteOf(byType["dokku_app"].Notes, NoteObjects)
	if app == nil || !strings.Contains(app.States["present"], "Creates it.") || !strings.Contains(app.States["absent"], "Destroys it.") {
		t.Errorf("dokku_app objects note = %+v; want Creates in present and Destroys in absent", app)
	}

	toggle := noteOf(byType["dokku_domains_toggle"].Notes, NoteObjects)
	if toggle == nil || !strings.Contains(toggle.States["present"], "Can restart or redeploy it.") || strings.Contains(toggle.States["absent"], "restart") {
		t.Errorf("dokku_domains_toggle objects note = %+v; want the restart only in present", toggle)
	}

	for _, field := range byType["dokku_domains"].Fields {
		if field.Name != "domains" {
			continue
		}
		if field.Note == nil || field.Note.States["present"] != " (required)" || field.Note.States["clear"] != " (must not be set)" {
			t.Errorf("dokku_domains domains note = %+v; want state-keyed suffixes", field.Note)
		}
	}
}

func TestEscapeProseEscapesBareAngleBrackets(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"reconciled with <service>:upgrade", "reconciled with &lt;service>:upgrade"},
		{"and <service>:set refuses the rest", "and &lt;service>:set refuses the rest"},
		{"recorded as `<remote>#<sha>`", "recorded as `<remote>#<sha>`"},
		{"a <service> starts and a `dokku <service>:info` follows", "a &lt;service> starts and a `dokku <service>:info` follows"},
		// A comparison is not an HTML tag, and escaping it would turn
		// Requirement lines such as "dokku-acl plugin >= 2.0.1" into entities.
		{"dokku-acl plugin >= 2.0.1", "dokku-acl plugin >= 2.0.1"},
		{"a <!-- comment --> and a <?pi ?>", "a &lt;!-- comment --> and a &lt;?pi ?>"},
		{"no brackets", "no brackets"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := EscapeProse(tc.in); got != tc.want {
			t.Errorf("EscapeProse(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
