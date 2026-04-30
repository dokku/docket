package tasks

import (
	"testing"
)

func TestIntegrationProxyProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-proxy-prop"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// set proxy type
	setTask := ProxyPropertyTask{
		App:      appName,
		Property: "type",
		Value:    "nginx",
		State:    StatePresent,
	}
	result := setTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to set proxy property: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}

	// re-applying the same value should be a no-op
	result = setTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to re-apply proxy property: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected re-apply to report Changed=false")
	}

	// unset proxy type
	unsetTask := ProxyPropertyTask{
		App:      appName,
		Property: "type",
		State:    StateAbsent,
	}
	result = unsetTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to unset proxy property: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}

	// re-applying the absent state should be a no-op
	result = unsetTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to re-apply absent proxy property: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected re-apply absent to report Changed=false")
	}
}
