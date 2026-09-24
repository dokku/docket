package tasks

import (
	"reflect"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestIntegrationConfigSetAndUnset(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-config"

	// ensure clean state
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// set config
	setTask := ConfigTask{
		App:     appName,
		Restart: boolPtr(false),
		Config:  map[string]string{"TEST_KEY": "test_value"},
		State:   StatePresent,
	}
	result := setTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to set config: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for new config")
	}

	// setting same config again should be idempotent
	result = setTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent set failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for unchanged config")
	}

	// unset config
	unsetTask := ConfigTask{
		App:     appName,
		Restart: boolPtr(false),
		Config:  map[string]string{"TEST_KEY": ""},
		State:   StateAbsent,
	}
	result = unsetTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to unset config: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for config removal")
	}

	// unset again should be idempotent
	result = unsetTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent unset failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for already-unset config")
	}
}

func TestIntegrationConfigMultipleKeys(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-multiconfig"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// set 3 keys
	setTask := ConfigTask{
		App:     appName,
		Restart: boolPtr(false),
		Config:  map[string]string{"KEY_A": "val_a", "KEY_B": "val_b", "KEY_C": "val_c"},
		State:   StatePresent,
	}
	result := setTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to set config: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for new config keys")
	}

	// update one key, keep others the same
	updateTask := ConfigTask{
		App:     appName,
		Restart: boolPtr(false),
		Config:  map[string]string{"KEY_A": "val_a", "KEY_B": "val_b_updated", "KEY_C": "val_c"},
		State:   StatePresent,
	}
	result = updateTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to update config: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for partial config update")
	}

	// set same values again (idempotent)
	result = updateTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent update failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for unchanged config")
	}

	// unset all keys
	unsetTask := ConfigTask{
		App:     appName,
		Restart: boolPtr(false),
		Config:  map[string]string{"KEY_A": "", "KEY_B": "", "KEY_C": ""},
		State:   StateAbsent,
	}
	result = unsetTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to unset config: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for config removal")
	}
}

// integrationConfig reads an app's config back for the set/clear tests.
func integrationConfig(t *testing.T, appName string) map[string]string {
	t.Helper()
	config, err := getConfig(testCtx(), ConfigTask{App: appName})
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	return config
}

func TestIntegrationConfigSetAndClear(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-config-set"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// NO_VHOST comes from domains:disable, and KEEP_ME is named in preserve;
	// state set must leave both in place while it drops STALE.
	if _, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "domains:disable", appName},
	}); err != nil {
		t.Fatalf("failed to disable domains: %v", err)
	}
	seed := ConfigTask{
		App:     appName,
		Restart: boolPtr(false),
		Config:  map[string]string{"STALE": "old", "KEEP_ME": "kept", "SHARED": "before"},
		State:   StatePresent,
	}
	if result := seed.Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed to seed config: %v", result.Error)
	}

	setTask := ConfigTask{
		App:      appName,
		Restart:  boolPtr(false),
		Config:   map[string]string{"SHARED": "after", "ADDED": "new value with spaces & $symbols"},
		Preserve: []string{"KEEP_ME"},
		State:    StateSet,
	}
	result := setTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to set config: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for the replacement")
	}
	want := map[string]string{
		"SHARED":   "after",
		"ADDED":    "new value with spaces & $symbols",
		"KEEP_ME":  "kept",
		"NO_VHOST": "1",
	}
	if got := integrationConfig(t, appName); !reflect.DeepEqual(got, want) {
		t.Errorf("config after set = %v, want %v", got, want)
	}

	result = setTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent set failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for an unchanged config")
	}

	clearTask := ConfigTask{App: appName, Restart: boolPtr(false), State: StateClear}
	result = clearTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to clear config: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for the clear")
	}
	if got := integrationConfig(t, appName); !reflect.DeepEqual(got, map[string]string{"NO_VHOST": "1"}) {
		t.Errorf("config after clear = %v, want only NO_VHOST", got)
	}

	result = clearTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent clear failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false once only kept keys remain")
	}
}

func TestIntegrationConfigSetKeepsServiceLink(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")
	skipIfDockerLinkUnsupportedT(t)

	appName := "docket-test-config-link-app"
	serviceName := "docket-test-config-link-svc"
	serviceType := "redis"

	destroyApp(testCtx(), appName)
	destroyService(testCtx(), serviceType, serviceName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	if result := (ServiceCreateTask{Service: serviceType, Name: serviceName, State: StatePresent}).Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed to create service: %v", result.Error)
	}
	linkTask := ServiceLinkTask{App: appName, Service: serviceType, Name: serviceName, State: StatePresent}
	defer func() {
		(ServiceLinkTask{App: appName, Service: serviceType, Name: serviceName, State: StateAbsent}).Execute(testCtx())
		destroyService(testCtx(), serviceType, serviceName)
	}()
	if result := linkTask.Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed to link service: %v", result.Error)
	}
	linkedURL := integrationConfig(t, appName)["REDIS_URL"]
	if linkedURL == "" {
		t.Fatal("expected the link to write REDIS_URL")
	}

	result := (ConfigTask{
		App:     appName,
		Restart: boolPtr(false),
		Config:  map[string]string{"KEY": "val"},
		State:   StateSet,
	}).Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to set config: %v", result.Error)
	}
	if got := integrationConfig(t, appName)["REDIS_URL"]; got != linkedURL {
		t.Errorf("REDIS_URL after set = %q, want the link's %q", got, linkedURL)
	}
	if result := linkTask.Execute(testCtx()); result.Error != nil || result.Changed {
		t.Errorf("link should still be in place after set: %+v", result)
	}
}
