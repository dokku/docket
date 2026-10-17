package tasks

import (
	"reflect"
	"slices"
	"testing"
)

// collectionStep is one apply in a set/clear sequence: the task, whether it
// must change the server, and a check of what the server holds afterwards.
type collectionStep struct {
	label   string
	task    Task
	changed bool
	check   func(t *testing.T, label string)
}

// runCollectionSteps applies each step in order, failing on an error, on a
// Changed that differs from the step's, or on a final state other than the
// one the task declared.
func runCollectionSteps(t *testing.T, steps []collectionStep) {
	t.Helper()
	for _, step := range steps {
		result := step.task.Execute(testCtx())
		if result.Error != nil {
			t.Fatalf("%s: %v", step.label, result.Error)
		}
		if result.Changed != step.changed {
			t.Errorf("%s: Changed = %v, want %v", step.label, result.Changed, step.changed)
		}
		if result.State != result.DesiredState {
			t.Errorf("%s: State = %q, DesiredState = %q", step.label, result.State, result.DesiredState)
		}
		if step.check != nil {
			step.check(t, step.label)
		}
	}
}

// TestIntegrationBuildpacksSetClear drives set, an empty set, and clear
// against a real app (#619).
func TestIntegrationBuildpacksSetClear(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-buildpacks-set"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	nodejs := "https://github.com/heroku/heroku-buildpack-nodejs.git"
	nginx := "https://github.com/heroku/heroku-buildpack-nginx.git"
	want := func(list ...string) func(t *testing.T, label string) {
		return func(t *testing.T, label string) {
			t.Helper()
			got, err := getOrderedBuildpacks(testCtx(), appName)
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			if !slices.Equal(got, list) {
				t.Errorf("%s: buildpacks = %v, want %v", label, got, list)
			}
		}
	}

	runCollectionSteps(t, []collectionStep{
		{"set", BuildpacksTask{App: appName, Buildpacks: []string{nodejs, nginx}, State: StateSet}, true, want(nodejs, nginx)},
		{"set again", BuildpacksTask{App: appName, Buildpacks: []string{nodejs, nginx}, State: StateSet}, false, want(nodejs, nginx)},
		{"set fewer", BuildpacksTask{App: appName, Buildpacks: []string{nginx}, State: StateSet}, true, want(nginx)},
		{"set empty", BuildpacksTask{App: appName, State: StateSet}, true, want()},
		{"set", BuildpacksTask{App: appName, Buildpacks: []string{nodejs}, State: StateSet}, true, want(nodejs)},
		{"clear", BuildpacksTask{App: appName, State: StateClear}, true, want()},
		{"clear again", BuildpacksTask{App: appName, State: StateClear}, false, want()},
	})
}

// TestIntegrationResourceLimitSetClear checks that set clears an undeclared
// limit in the same call that writes the declared ones (#619).
func TestIntegrationResourceLimitSetClear(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-resource-limit-set"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	want := func(set map[string]string) func(t *testing.T, label string) {
		return func(t *testing.T, label string) {
			t.Helper()
			current, err := getResources(testCtx(), "resource:limit", ResourceContext{App: appName})
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			got := map[string]string{}
			for k, v := range current {
				if resourceValueIsSet(v) {
					got[k] = v
				}
			}
			if !reflect.DeepEqual(got, set) {
				t.Errorf("%s: limits = %v, want %v", label, got, set)
			}
		}
	}

	runCollectionSteps(t, []collectionStep{
		{"present", ResourceLimitTask{App: appName, Resources: map[string]string{"cpu": "100", "memory": "256"}, State: StatePresent}, true, want(map[string]string{"cpu": "100", "memory": "256"})},
		{"set", ResourceLimitTask{App: appName, Resources: map[string]string{"memory": "512"}, State: StateSet}, true, want(map[string]string{"memory": "512"})},
		{"set again", ResourceLimitTask{App: appName, Resources: map[string]string{"memory": "512"}, State: StateSet}, false, want(map[string]string{"memory": "512"})},
		{"clear", ResourceLimitTask{App: appName, State: StateClear}, true, want(map[string]string{})},
		{"clear again", ResourceLimitTask{App: appName, State: StateClear}, false, want(map[string]string{})},
		{"set", ResourceLimitTask{App: appName, Resources: map[string]string{"cpu": "100"}, State: StateSet}, true, want(map[string]string{"cpu": "100"})},
		{"set empty", ResourceLimitTask{App: appName, State: StateSet}, true, want(map[string]string{})},
	})
}

// TestIntegrationResourceReserveSetClear is the reservation counterpart of
// TestIntegrationResourceLimitSetClear, on a named process type.
func TestIntegrationResourceReserveSetClear(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-resource-reserve-set"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	want := func(set map[string]string) func(t *testing.T, label string) {
		return func(t *testing.T, label string) {
			t.Helper()
			current, err := getResources(testCtx(), "resource:reserve", ResourceContext{App: appName, ProcessType: "web"})
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			got := map[string]string{}
			for k, v := range current {
				if resourceValueIsSet(v) {
					got[k] = v
				}
			}
			if !reflect.DeepEqual(got, set) {
				t.Errorf("%s: reservations = %v, want %v", label, got, set)
			}
		}
	}

	runCollectionSteps(t, []collectionStep{
		{"set", ResourceReserveTask{App: appName, ProcessType: "web", Resources: map[string]string{"cpu": "100", "memory": "256"}, State: StateSet}, true, want(map[string]string{"cpu": "100", "memory": "256"})},
		{"set fewer", ResourceReserveTask{App: appName, ProcessType: "web", Resources: map[string]string{"memory": "256"}, State: StateSet}, true, want(map[string]string{"memory": "256"})},
		{"set again", ResourceReserveTask{App: appName, ProcessType: "web", Resources: map[string]string{"memory": "256"}, State: StateSet}, false, want(map[string]string{"memory": "256"})},
		{"clear", ResourceReserveTask{App: appName, ProcessType: "web", State: StateClear}, true, want(map[string]string{})},
		{"clear again", ResourceReserveTask{App: appName, ProcessType: "web", State: StateClear}, false, want(map[string]string{})},
	})
}

// TestIntegrationServiceExposeSetClear drives set, an empty set, and clear
// against a real service (#619).
func TestIntegrationServiceExposeSetClear(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")

	serviceType := "redis"
	serviceName := "docket-test-expose-set-svc"
	destroyService(testCtx(), serviceType, serviceName)
	if r := (ServiceCreateTask{Service: serviceType, Name: serviceName, State: StatePresent}).Execute(testCtx()); r.Error != nil {
		t.Fatalf("failed to create service: %v", r.Error)
	}
	defer destroyService(testCtx(), serviceType, serviceName)

	want := func(ports ...string) func(t *testing.T, label string) {
		return func(t *testing.T, label string) {
			t.Helper()
			got, err := serviceExposedPortList(testCtx(), serviceType, serviceName)
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			if !slices.Equal(got, ports) {
				t.Errorf("%s: exposed ports = %v, want %v", label, got, ports)
			}
		}
	}
	task := func(state State, ports ...string) ServiceExposeTask {
		return ServiceExposeTask{Service: serviceType, Name: serviceName, Ports: ports, State: state}
	}

	runCollectionSteps(t, []collectionStep{
		{"set", task(StateSet, "11112"), true, want("11112")},
		{"set again", task(StateSet, "11112"), false, want("11112")},
		{"set other port", task(StateSet, "11113"), true, want("11113")},
		{"set empty", task(StateSet), true, want()},
		{"set", task(StateSet, "11112"), true, want("11112")},
		{"clear", task(StateClear), true, want()},
		{"clear again", task(StateClear), false, want()},
	})
}

// TestIntegrationHttpAuthUserClear drives clear and an empty set against a
// real app's users (#619).
func TestIntegrationHttpAuthUserClear(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "http-auth")

	appName := "docket-test-http-auth-user-clear"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	if result := (HttpAuthTask{App: appName, State: StatePresent}).Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed to enable http auth: %v", result.Error)
	}

	want := func(users ...string) func(t *testing.T, label string) {
		return func(t *testing.T, label string) {
			t.Helper()
			current, err := getHttpAuthUsers(testCtx(), appName)
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			got := sortedSetKeys(current)
			if len(got) == 0 {
				got = nil
			}
			if !slices.Equal(got, users) {
				t.Errorf("%s: users = %v, want %v", label, got, users)
			}
		}
	}
	two := []HttpAuthUser{{Username: "alice", Password: "a-secret"}, {Username: "bob", Password: "b-secret"}}

	runCollectionSteps(t, []collectionStep{
		{"set", HttpAuthUserTask{App: appName, Users: two, State: StateSet}, true, want("alice", "bob")},
		{"clear", HttpAuthUserTask{App: appName, State: StateClear}, true, want()},
		{"clear again", HttpAuthUserTask{App: appName, State: StateClear}, false, want()},
		{"set", HttpAuthUserTask{App: appName, Users: two, State: StateSet}, true, want("alice", "bob")},
		{"set empty", HttpAuthUserTask{App: appName, State: StateSet}, true, want()},
	})
}

// TestIntegrationSchedulerK3sChartSetClear drives set, which unsets the values
// it does not declare one key at a time, and clear against a real chart
// (#619).
func TestIntegrationSchedulerK3sChartSetClear(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	chart := "traefik"
	clearChart := func() {
		SchedulerK3sChartTask{Chart: chart, State: StateClear}.Execute(testCtx())
	}
	clearChart()
	defer clearChart()

	want := func(values map[string]string) func(t *testing.T, label string) {
		return func(t *testing.T, label string) {
			t.Helper()
			got, err := getSchedulerK3sChartValues(testCtx(), chart)
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			if !reflect.DeepEqual(got, values) {
				t.Errorf("%s: values = %v, want %v", label, got, values)
			}
		}
	}

	runCollectionSteps(t, []collectionStep{
		{"set", SchedulerK3sChartTask{Chart: chart, Values: map[string]any{"deployment.replicas": "1", "logs.general.level": "INFO"}, State: StateSet}, true, want(map[string]string{"deployment.replicas": "1", "logs.general.level": "INFO"})},
		{"set again", SchedulerK3sChartTask{Chart: chart, Values: map[string]any{"deployment.replicas": "1", "logs.general.level": "INFO"}, State: StateSet}, false, want(map[string]string{"deployment.replicas": "1", "logs.general.level": "INFO"})},
		{"set fewer", SchedulerK3sChartTask{Chart: chart, Values: map[string]any{"deployment.replicas": "1"}, State: StateSet}, true, want(map[string]string{"deployment.replicas": "1"})},
		{"clear", SchedulerK3sChartTask{Chart: chart, State: StateClear}, true, want(map[string]string{})},
		{"clear again", SchedulerK3sChartTask{Chart: chart, State: StateClear}, false, want(map[string]string{})},
	})
}
