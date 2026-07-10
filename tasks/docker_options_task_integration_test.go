package tasks

import (
	"strings"
	"testing"
)

func TestIntegrationDockerOptions(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-docker-options"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	option := "-v /tmp/docket-test:/tmp/docket-test"
	phase := "deploy"
	scope := dockerOptionsScope{Phase: phase}

	// initial state - option not present
	current, err := getDockerOptions(appName)
	if err != nil {
		t.Fatalf("getDockerOptions failed: %v", err)
	}
	if optionPresent(current[scope], option) {
		t.Fatalf("expected option not to be present initially")
	}

	// add option
	addTask := DockerOptionsTask{App: appName, Phase: phase, Option: option, State: StatePresent}
	result := addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to add option: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first add")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	current, err = getDockerOptions(appName)
	if err != nil {
		t.Fatalf("getDockerOptions failed: %v", err)
	}
	if !optionPresent(current[scope], option) {
		t.Errorf("expected option to be present after add (got %q for scope %+v)", current[scope], scope)
	}

	// add same option - idempotent
	result = addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second add: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent add")
	}

	// remove option
	removeTask := DockerOptionsTask{App: appName, Phase: phase, Option: option, State: StateAbsent}
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to remove option: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first remove")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	current, err = getDockerOptions(appName)
	if err != nil {
		t.Fatalf("getDockerOptions failed: %v", err)
	}
	if optionPresent(current[scope], option) {
		t.Errorf("expected option not to be present after remove (got %q for scope %+v)", current[scope], scope)
	}

	// remove again - idempotent
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second remove: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent remove")
	}
}

// TestIntegrationDockerOptionsExportRoundTrip verifies the exporter recovers
// each stored option as a discrete task from the structured -list report keys
// (dokku/dokku#8799), including an option whose value contains a space, and
// that re-planning the exported bodies reports no drift.
func TestIntegrationDockerOptionsExportRoundTrip(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-docker-options-export"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	phase := "deploy"
	plainOption := "-v /tmp/docket-test:/tmp/docket-test"
	// A value containing a space: the space-joined report string cannot split
	// this back, but the -list companion carries it as one element.
	spacedOption := "--label 'com.example.description=my app'"

	for _, option := range []string{plainOption, spacedOption} {
		add := DockerOptionsTask{App: appName, Phase: phase, Option: option, State: StatePresent}
		if r := add.Execute(); r.Error != nil {
			t.Fatalf("failed to add option %q: %v", option, r.Error)
		}
	}

	bodies, err := DockerOptionsTask{}.ExportApp(appName)
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}

	// The space-bearing option must surface as exactly one task, not tokenized
	// into separate tasks per whitespace-separated word.
	var spaced []DockerOptionsTask
	for _, body := range bodies {
		task, ok := body.(DockerOptionsTask)
		if !ok {
			t.Fatalf("exported body is not a DockerOptionsTask: %T", body)
		}
		if strings.Contains(task.Option, "com.example.description=my app") {
			spaced = append(spaced, task)
		}
	}
	if len(spaced) != 1 {
		t.Fatalf("expected exactly one exported task carrying the space value, got %d: %+v", len(spaced), bodies)
	}

	// Every exported body must re-plan without drift, proving the round-trip.
	for _, body := range bodies {
		task := body.(DockerOptionsTask)
		task.State = StatePresent
		if plan := task.Plan(); !plan.InSync {
			t.Errorf("exported task %+v should report no drift, got status %v reason %q", task, plan.Status, plan.Reason)
		}
	}
}

func TestIntegrationDockerOptionsProcessType(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-docker-options-proc"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	option := "--memory=512m"
	phase := "deploy"
	processType := "web"
	procScope := dockerOptionsScope{Phase: phase, ProcessType: processType}
	defaultScope := dockerOptionsScope{Phase: phase}

	// initial state - option not present in either scope
	current, err := getDockerOptions(appName)
	if err != nil {
		t.Fatalf("getDockerOptions failed: %v", err)
	}
	if optionPresent(current[procScope], option) {
		t.Fatalf("expected option not to be present initially in %+v", procScope)
	}

	// add option scoped to the web process
	addTask := DockerOptionsTask{
		App:         appName,
		Phase:       phase,
		ProcessType: processType,
		Option:      option,
		State:       StatePresent,
	}
	result := addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to add scoped option: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first add")
	}

	current, err = getDockerOptions(appName)
	if err != nil {
		t.Fatalf("getDockerOptions failed: %v", err)
	}
	if !optionPresent(current[procScope], option) {
		t.Errorf("expected option in %+v after add (got %q)", procScope, current[procScope])
	}
	// The default scope must not have the option - they are independent buckets.
	if optionPresent(current[defaultScope], option) {
		t.Errorf("expected option NOT in default scope after process-scoped add (got %q)", current[defaultScope])
	}

	// adding the same scoped option is idempotent
	result = addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second scoped add: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent scoped add")
	}

	// adding the same option to the default scope is not a no-op because the
	// scopes are independent
	defaultAddTask := DockerOptionsTask{
		App:    appName,
		Phase:  phase,
		Option: option,
		State:  StatePresent,
	}
	result = defaultAddTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to add default-scope option: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true when adding the same option in the default scope")
	}

	current, err = getDockerOptions(appName)
	if err != nil {
		t.Fatalf("getDockerOptions failed: %v", err)
	}
	if !optionPresent(current[defaultScope], option) {
		t.Errorf("expected option in default scope after add (got %q)", current[defaultScope])
	}
	if !optionPresent(current[procScope], option) {
		t.Errorf("expected option still in %+v (got %q)", procScope, current[procScope])
	}

	// remove the scoped option; default scope must be unaffected
	removeScopedTask := DockerOptionsTask{
		App:         appName,
		Phase:       phase,
		ProcessType: processType,
		Option:      option,
		State:       StateAbsent,
	}
	result = removeScopedTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to remove scoped option: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first scoped remove")
	}

	current, err = getDockerOptions(appName)
	if err != nil {
		t.Fatalf("getDockerOptions failed: %v", err)
	}
	if optionPresent(current[procScope], option) {
		t.Errorf("expected option NOT in %+v after remove (got %q)", procScope, current[procScope])
	}
	if !optionPresent(current[defaultScope], option) {
		t.Errorf("expected option still in default scope after scoped remove (got %q)", current[defaultScope])
	}

	// idempotent scoped remove
	result = removeScopedTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second scoped remove: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent scoped remove")
	}
}
