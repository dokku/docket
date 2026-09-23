package tasks

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// The tests below cover the authoritative states #527 added. They read the
// server back through the report themselves rather than through the task's own
// probe: a bug in the probe would otherwise be masked by the assertion that is
// meant to catch it.

// reportedSchedulerK3sPairs reads one (process_type, resource_type) scope out
// of an unfiltered `scheduler-k3s:<kind>:report`, so the assertion sees every
// scope the server holds and picks the one it wants, not the one the probe
// asked for.
func reportedSchedulerK3sPairs(t *testing.T, kind, target, processType, resourceType string) map[string]string {
	t.Helper()
	result, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "scheduler-k3s:" + kind + ":report", target, "--format", "json"},
	})
	if err != nil {
		t.Fatalf("scheduler-k3s:%s:report %s: %v", kind, target, err)
	}

	payload := map[string]string{}
	if err := json.Unmarshal(result.StdoutBytes(), &payload); err != nil {
		t.Fatalf("parse scheduler-k3s:%s:report json: %v", kind, err)
	}

	rendered := processType
	if rendered == "" {
		rendered = "global"
	}
	prefix := rendered + "." + resourceType + "."
	pairs := map[string]string{}
	for composed, value := range payload {
		if strings.HasPrefix(composed, prefix) {
			pairs[strings.TrimPrefix(composed, prefix)] = value
		}
	}
	return pairs
}

// reportedSchedulerK3sTriggerMetadata reads one trigger's metadata out of
// `scheduler-k3s:autoscaling-auth:report`.
func reportedSchedulerK3sTriggerMetadata(t *testing.T, target, trigger string) map[string]string {
	t.Helper()
	result, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "scheduler-k3s:autoscaling-auth:report", target, "--format", "json"},
	})
	if err != nil {
		t.Fatalf("scheduler-k3s:autoscaling-auth:report %s: %v", target, err)
	}

	payload := map[string]string{}
	if err := json.Unmarshal(result.StdoutBytes(), &payload); err != nil {
		t.Fatalf("parse scheduler-k3s:autoscaling-auth:report json: %v", err)
	}

	prefix := trigger + "."
	metadata := map[string]string{}
	for composed, value := range payload {
		if strings.HasPrefix(composed, prefix) {
			metadata[strings.TrimPrefix(composed, prefix)] = value
		}
	}
	return metadata
}

// reportedSchedulerK3sNodeSysctls reads one scope's stored map out of an
// unfiltered `scheduler-k3s:node-sysctls:report --stored`, keyed by profile
// name or "--global". Unlike the probe it does not narrow the report, so it
// sees every scope the server holds and picks the one it wants.
func reportedSchedulerK3sNodeSysctls(t *testing.T, scope string) map[string]string {
	t.Helper()
	result, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "scheduler-k3s:node-sysctls:report", "--stored", "--format", "json"},
	})
	if err != nil {
		t.Fatalf("scheduler-k3s:node-sysctls:report: %v", err)
	}

	payload := map[string]map[string]string{}
	if err := json.Unmarshal(result.StdoutBytes(), &payload); err != nil {
		t.Fatalf("parse scheduler-k3s:node-sysctls:report json: %v", err)
	}
	sysctls := map[string]string{}
	for name, value := range payload[scope] {
		sysctls[name] = value
	}
	return sysctls
}

// authoritativeStateCase drives the set/clear round trip every map task shares:
// seed an additive map, declare a different one with state 'set', check the
// server holds exactly that, re-run for idempotency, clear it, and clear again
// expecting no change.
type authoritativeStateCase struct {
	label string
	// seed writes the starting map with state 'present'.
	seed Task
	// set declares a map that both changes a seeded entry and drops another.
	set Task
	// want is what the server must hold after set.
	want map[string]string
	// clear empties the collection.
	clear Task
	// reported reads the collection back out of band.
	reported func(t *testing.T) map[string]string
}

func runAuthoritativeStateTest(t *testing.T, c authoritativeStateCase) {
	t.Helper()

	if result := c.seed.Execute(testCtx()); result.Error != nil {
		t.Fatalf("%s: seed: %v", c.label, result.Error)
	}

	result := c.set.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("%s: set: %v", c.label, result.Error)
	}
	if !result.Changed {
		t.Errorf("%s: set should have reported a change", c.label)
	}
	if got := c.reported(t); !reflect.DeepEqual(got, c.want) {
		t.Fatalf("%s: after set the server holds %v, want %v", c.label, got, c.want)
	}

	// The entry the seed wrote and the set did not declare is gone, which is
	// exactly what state 'present' could not express (#527).
	if result := c.set.Execute(testCtx()); result.Error != nil {
		t.Fatalf("%s: set re-run: %v", c.label, result.Error)
	} else if result.Changed {
		t.Errorf("%s: set is not idempotent; second run reported a change", c.label)
	}

	if result := c.clear.Execute(testCtx()); result.Error != nil {
		t.Fatalf("%s: clear: %v", c.label, result.Error)
	} else if !result.Changed {
		t.Errorf("%s: clear should have reported a change", c.label)
	}
	if got := c.reported(t); len(got) != 0 {
		t.Errorf("%s: after clear the server still holds %v", c.label, got)
	}

	if result := c.clear.Execute(testCtx()); result.Error != nil {
		t.Fatalf("%s: clear re-run: %v", c.label, result.Error)
	} else if result.Changed {
		t.Errorf("%s: clear is not idempotent; second run reported a change", c.label)
	}
}

func TestIntegrationSchedulerK3sScopedPairsSetAndClear(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	appName := "docket-test-scheduler-k3s-authoritative"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	t.Run("annotations/per-app/default-process-type", func(t *testing.T) {
		runAuthoritativeStateTest(t, authoritativeStateCase{
			label: "annotations per-app",
			seed: SchedulerK3sAnnotationsTask{
				App:          appName,
				ResourceType: "deployment",
				Annotations:  map[string]string{"keep": "old", "stale": "gone"},
				State:        StatePresent,
			},
			set: SchedulerK3sAnnotationsTask{
				App:          appName,
				ResourceType: "deployment",
				Annotations:  map[string]string{"keep": "new", "added": "yes"},
				State:        StateSet,
			},
			want: map[string]string{"keep": "new", "added": "yes"},
			clear: SchedulerK3sAnnotationsTask{
				App:          appName,
				ResourceType: "deployment",
				State:        StateClear,
			},
			reported: func(t *testing.T) map[string]string {
				return reportedSchedulerK3sPairs(t, "annotations", appName, "", "deployment")
			},
		})
	})

	t.Run("labels/per-app/explicit-process-type", func(t *testing.T) {
		runAuthoritativeStateTest(t, authoritativeStateCase{
			label: "labels per-app web",
			seed: SchedulerK3sLabelsTask{
				App:          appName,
				ProcessType:  "web",
				ResourceType: "deployment",
				Labels:       map[string]string{"tier": "edge", "stale": "gone"},
				State:        StatePresent,
			},
			set: SchedulerK3sLabelsTask{
				App:          appName,
				ProcessType:  "web",
				ResourceType: "deployment",
				Labels:       map[string]string{"tier": "core"},
				State:        StateSet,
			},
			want: map[string]string{"tier": "core"},
			clear: SchedulerK3sLabelsTask{
				App:          appName,
				ProcessType:  "web",
				ResourceType: "deployment",
				State:        StateClear,
			},
			reported: func(t *testing.T) map[string]string {
				return reportedSchedulerK3sPairs(t, "labels", appName, "web", "deployment")
			},
		})
	})

	t.Run("annotations/global-scope", func(t *testing.T) {
		runAuthoritativeStateTest(t, authoritativeStateCase{
			label: "annotations global",
			seed: SchedulerK3sAnnotationsTask{
				Global:       true,
				ResourceType: "deployment",
				Annotations:  map[string]string{"managed-by": "ansible", "stale": "gone"},
				State:        StatePresent,
			},
			set: SchedulerK3sAnnotationsTask{
				Global:       true,
				ResourceType: "deployment",
				Annotations:  map[string]string{"managed-by": "docket"},
				State:        StateSet,
			},
			want: map[string]string{"managed-by": "docket"},
			clear: SchedulerK3sAnnotationsTask{
				Global:       true,
				ResourceType: "deployment",
				State:        StateClear,
			},
			reported: func(t *testing.T) map[string]string {
				return reportedSchedulerK3sPairs(t, "annotations", "--global", "", "deployment")
			},
		})
	})
}

// TestIntegrationSchedulerK3sClearLeavesOtherScopesAlone is the regression test
// for the failure mode that makes state 'clear' dangerous: dokku reads an
// omitted --process-type on :clear as "no filter", so a clear that did not name
// one would empty every process type's map for the resource type.
func TestIntegrationSchedulerK3sClearLeavesOtherScopesAlone(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	appName := "docket-test-scheduler-k3s-clear-scope"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	base := SchedulerK3sAnnotationsTask{App: appName, ResourceType: "deployment"}

	defaultScope := base
	defaultScope.Annotations = map[string]string{"scope": "default"}
	defaultScope.State = StatePresent
	if result := defaultScope.Execute(testCtx()); result.Error != nil {
		t.Fatalf("seed default process type: %v", result.Error)
	}

	webScope := base
	webScope.ProcessType = "web"
	webScope.Annotations = map[string]string{"scope": "web"}
	webScope.State = StatePresent
	if result := webScope.Execute(testCtx()); result.Error != nil {
		t.Fatalf("seed web process type: %v", result.Error)
	}

	// A different resource type under the default process type, which the
	// clear below also must not touch.
	ingressScope := SchedulerK3sAnnotationsTask{
		App:          appName,
		ResourceType: "ingress",
		Annotations:  map[string]string{"scope": "ingress"},
		State:        StatePresent,
	}
	if result := ingressScope.Execute(testCtx()); result.Error != nil {
		t.Fatalf("seed ingress resource type: %v", result.Error)
	}

	clear := base
	clear.State = StateClear
	if result := clear.Execute(testCtx()); result.Error != nil {
		t.Fatalf("clear default process type: %v", result.Error)
	}

	if got := reportedSchedulerK3sPairs(t, "annotations", appName, "", "deployment"); len(got) != 0 {
		t.Errorf("the cleared scope still holds %v", got)
	}
	if got := reportedSchedulerK3sPairs(t, "annotations", appName, "web", "deployment"); !reflect.DeepEqual(got, map[string]string{"scope": "web"}) {
		t.Errorf("clear emptied the web process type as well: %v", got)
	}
	if got := reportedSchedulerK3sPairs(t, "annotations", appName, "", "ingress"); !reflect.DeepEqual(got, map[string]string{"scope": "ingress"}) {
		t.Errorf("clear emptied the ingress resource type as well: %v", got)
	}
}

// TestIntegrationSchedulerK3sSetStoresAnEmptyValue pins the capability state
// 'set' adds over state 'present': dokku's :set --replace stores an empty value
// and the report reads it back, where the per-key form reads one as a delete.
func TestIntegrationSchedulerK3sSetStoresAnEmptyValue(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	appName := "docket-test-scheduler-k3s-empty-value"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	task := SchedulerK3sAnnotationsTask{
		App:          appName,
		ResourceType: "deployment",
		Annotations:  map[string]string{"blank": "", "filled": "yes"},
		State:        StateSet,
	}
	if result := task.Execute(testCtx()); result.Error != nil {
		t.Fatalf("set: %v", result.Error)
	}

	want := map[string]string{"blank": "", "filled": "yes"}
	if got := reportedSchedulerK3sPairs(t, "annotations", appName, "", "deployment"); !reflect.DeepEqual(got, want) {
		t.Fatalf("server holds %v, want %v", got, want)
	}

	// The stored empty value reads back, so the same recipe converges rather
	// than planning the same write on every run.
	if result := task.Execute(testCtx()); result.Error != nil {
		t.Fatalf("set re-run: %v", result.Error)
	} else if result.Changed {
		t.Error("a stored empty value should converge; second run reported a change")
	}
}

func TestIntegrationSchedulerK3sAutoscalingAuthSetAndClear(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	appName := "docket-test-scheduler-k3s-auth-authoritative"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	runAuthoritativeStateTest(t, authoritativeStateCase{
		label: "autoscaling auth per-app",
		seed: SchedulerK3sAutoscalingAuthTask{
			App:      appName,
			Trigger:  "datadog",
			Metadata: map[string]string{"apiKey": "old", "datadogSite": "gone"},
			State:    StatePresent,
		},
		set: SchedulerK3sAutoscalingAuthTask{
			App:      appName,
			Trigger:  "datadog",
			Metadata: map[string]string{"apiKey": "new", "appKey": "added"},
			State:    StateSet,
		},
		want: map[string]string{"apiKey": "new", "appKey": "added"},
		clear: SchedulerK3sAutoscalingAuthTask{
			App:     appName,
			Trigger: "datadog",
			State:   StateClear,
		},
		reported: func(t *testing.T) map[string]string {
			return reportedSchedulerK3sTriggerMetadata(t, appName, "datadog")
		},
	})
}

// TestIntegrationSchedulerK3sAutoscalingAuthAbsentKeepsSurvivors checks the
// rework of state 'absent' from a wipe-and-restore pair into one --replace
// call: the keys the task does not name must still be there afterwards.
func TestIntegrationSchedulerK3sAutoscalingAuthAbsentKeepsSurvivors(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	appName := "docket-test-scheduler-k3s-auth-absent"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	seed := SchedulerK3sAutoscalingAuthTask{
		App:      appName,
		Trigger:  "datadog",
		Metadata: map[string]string{"apiKey": "keep-me", "datadogSite": "drop-me"},
		State:    StatePresent,
	}
	if result := seed.Execute(testCtx()); result.Error != nil {
		t.Fatalf("seed: %v", result.Error)
	}

	absent := SchedulerK3sAutoscalingAuthTask{
		App:      appName,
		Trigger:  "datadog",
		Metadata: map[string]string{"datadogSite": ""},
		State:    StateAbsent,
	}
	result := absent.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("absent: %v", result.Error)
	}
	if len(result.Commands) != 1 {
		t.Errorf("absent should run exactly one command, got %v", result.Commands)
	}

	want := map[string]string{"apiKey": "keep-me"}
	if got := reportedSchedulerK3sTriggerMetadata(t, appName, "datadog"); !reflect.DeepEqual(got, want) {
		t.Fatalf("server holds %v, want %v", got, want)
	}

	if result := absent.Execute(testCtx()); result.Error != nil {
		t.Fatalf("absent re-run: %v", result.Error)
	} else if result.Changed {
		t.Error("absent is not idempotent; second run reported a change")
	}

	cleanup := SchedulerK3sAutoscalingAuthTask{App: appName, Trigger: "datadog", State: StateClear}
	cleanup.Execute(testCtx())
}

func TestIntegrationSchedulerK3sNodeSysctlsAll(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	cleanup := SchedulerK3sNodeSysctlsTask{Global: true, State: StateClear}
	cleanup.Execute(testCtx())
	defer cleanup.Execute(testCtx())

	runAuthoritativeStateTest(t, authoritativeStateCase{
		label: "node sysctls global",
		seed: SchedulerK3sNodeSysctlsTask{
			Global:  true,
			Sysctls: map[string]string{"vm.swappiness": "60", "vm.max_map_count": "65530"},
			State:   StatePresent,
		},
		set: SchedulerK3sNodeSysctlsTask{
			Global:  true,
			Sysctls: map[string]string{"vm.swappiness": "10"},
			State:   StateSet,
		},
		want: map[string]string{"vm.swappiness": "10"},
		clear: SchedulerK3sNodeSysctlsTask{
			Global: true,
			State:  StateClear,
		},
		reported: func(t *testing.T) map[string]string {
			return reportedSchedulerK3sNodeSysctls(t, "--global")
		},
	})
}

// TestIntegrationSchedulerK3sNodeSysctlsPresentAndAbsent covers the additive
// states, which go through the per-key form rather than --replace.
func TestIntegrationSchedulerK3sNodeSysctlsPresentAndAbsent(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	cleanup := SchedulerK3sNodeSysctlsTask{Global: true, State: StateClear}
	cleanup.Execute(testCtx())
	defer cleanup.Execute(testCtx())

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label: "scheduler-k3s node sysctls global",
		setTask: SchedulerK3sNodeSysctlsTask{
			Global:  true,
			Sysctls: map[string]string{"vm.swappiness": "10"},
			State:   StatePresent,
		},
		unsetTask: SchedulerK3sNodeSysctlsTask{
			Global:  true,
			Sysctls: map[string]string{"vm.swappiness": ""},
			State:   StateAbsent,
		},
	})
}

// schedulerK3sNodeSysctlsTestProfile is the node profile the profile-scoped
// node sysctls tests write to.
const schedulerK3sNodeSysctlsTestProfile = "docket-test-sysctls"

// withSchedulerK3sNodeSysctlsProfile creates the test profile and seeds the
// global scope with sysctls the profile inherits, so every profile-scoped
// assertion runs with inherited values present in the resolved report - the
// case #555 could not converge. It returns the cleanup.
func withSchedulerK3sNodeSysctlsProfile(t *testing.T) func() {
	t.Helper()
	profile := SchedulerK3sProfileTask{Name: schedulerK3sNodeSysctlsTestProfile, Role: "worker", State: StatePresent}
	global := SchedulerK3sNodeSysctlsTask{Global: true, State: StateClear}
	cleanup := func() {
		global.Execute(testCtx())
		// profiles:remove deletes the profile's stored sysctls too.
		SchedulerK3sProfileTask{Name: schedulerK3sNodeSysctlsTestProfile, Role: "worker", State: StateAbsent}.Execute(testCtx())
	}
	cleanup()

	if result := profile.Execute(testCtx()); result.Error != nil {
		t.Fatalf("create profile: %v", result.Error)
	}
	seed := SchedulerK3sNodeSysctlsTask{
		Global:  true,
		Sysctls: map[string]string{"vm.swappiness": "20", "vm.overcommit_memory": "1"},
		State:   StateSet,
	}
	if result := seed.Execute(testCtx()); result.Error != nil {
		cleanup()
		t.Fatalf("seed global sysctls: %v", result.Error)
	}
	return cleanup
}

func TestIntegrationSchedulerK3sNodeSysctlsProfileAll(t *testing.T) {
	skipUnlessSchedulerK3sT(t)
	defer withSchedulerK3sNodeSysctlsProfile(t)()

	runAuthoritativeStateTest(t, authoritativeStateCase{
		label: "node sysctls profile",
		seed: SchedulerK3sNodeSysctlsTask{
			Profile: schedulerK3sNodeSysctlsTestProfile,
			Sysctls: map[string]string{"vm.swappiness": "60", "vm.max_map_count": "65530"},
			State:   StatePresent,
		},
		set: SchedulerK3sNodeSysctlsTask{
			Profile: schedulerK3sNodeSysctlsTestProfile,
			Sysctls: map[string]string{"vm.swappiness": "10"},
			State:   StateSet,
		},
		want: map[string]string{"vm.swappiness": "10"},
		clear: SchedulerK3sNodeSysctlsTask{
			Profile: schedulerK3sNodeSysctlsTestProfile,
			State:   StateClear,
		},
		reported: func(t *testing.T) map[string]string {
			return reportedSchedulerK3sNodeSysctls(t, schedulerK3sNodeSysctlsTestProfile)
		},
	})

	// The global scope is untouched by every profile-scoped write.
	want := map[string]string{"vm.swappiness": "20", "vm.overcommit_memory": "1"}
	if got := reportedSchedulerK3sNodeSysctls(t, "--global"); !reflect.DeepEqual(got, want) {
		t.Errorf("global scope holds %v, want %v", got, want)
	}
}

// TestIntegrationSchedulerK3sNodeSysctlsProfilePresentAndAbsent covers the
// additive states on a profile. The profile's own vm.swappiness shadows the
// global one, and clearing it must converge even though the profile still
// inherits the global value.
func TestIntegrationSchedulerK3sNodeSysctlsProfilePresentAndAbsent(t *testing.T) {
	skipUnlessSchedulerK3sT(t)
	defer withSchedulerK3sNodeSysctlsProfile(t)()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label: "scheduler-k3s node sysctls profile",
		setTask: SchedulerK3sNodeSysctlsTask{
			Profile: schedulerK3sNodeSysctlsTestProfile,
			Sysctls: map[string]string{"vm.swappiness": "60"},
			State:   StatePresent,
		},
		unsetTask: SchedulerK3sNodeSysctlsTask{
			Profile: schedulerK3sNodeSysctlsTestProfile,
			Sysctls: map[string]string{"vm.swappiness": ""},
			State:   StateAbsent,
		},
	})
}

// TestIntegrationSchedulerK3sNodeSysctlsProfileExportRoundTrips checks a
// profile's exported body carries only its own map, not the sysctls it
// inherits, and plans in sync straight back against the server.
func TestIntegrationSchedulerK3sNodeSysctlsProfileExportRoundTrips(t *testing.T) {
	skipUnlessSchedulerK3sT(t)
	defer withSchedulerK3sNodeSysctlsProfile(t)()

	set := SchedulerK3sNodeSysctlsTask{
		Profile: schedulerK3sNodeSysctlsTestProfile,
		Sysctls: map[string]string{"vm.swappiness": "60"},
		State:   StateSet,
	}
	if result := set.Execute(testCtx()); result.Error != nil {
		t.Fatalf("set: %v", result.Error)
	}

	bodies, err := SchedulerK3sNodeSysctlsTask{}.ExportGlobal(testCtx())
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	var found *SchedulerK3sNodeSysctlsTask
	for _, body := range bodies {
		if b := body.(SchedulerK3sNodeSysctlsTask); b.Profile == schedulerK3sNodeSysctlsTestProfile {
			found = &b
		}
	}
	if found == nil {
		t.Fatalf("no exported body for profile %s in %+v", schedulerK3sNodeSysctlsTestProfile, bodies)
	}
	if !reflect.DeepEqual(found.Sysctls, set.Sysctls) {
		t.Errorf("exported sysctls = %v, want %v", found.Sysctls, set.Sysctls)
	}
	if plan := found.Plan(testCtx()); !plan.InSync {
		t.Errorf("exported body should report no drift, got %v", plan.Mutations)
	}
}
