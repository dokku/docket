package tasks

import (
	"github.com/dokku/docket/internal/subprocess"
	"strings"
	"testing"
)

func TestIntegrationPsScale(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-psscale"

	// ensure clean state
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// deploy the smoke test app so we have running containers to scale
	deployTask := GitFromImageTask{
		App:   appName,
		Image: "dokku/smoke-test-app:dockerfile",
		State: StateDeployed,
	}
	deployResult := deployTask.Execute(testCtx())
	if deployResult.Error != nil {
		t.Fatalf("failed to deploy app: %v", deployResult.Error)
	}

	// verify initial web container count is 1 via docker ps
	initialContainers, err := getCurrentContainerIDs(appName, "web")
	if err != nil {
		t.Fatalf("failed to list containers: %v", err)
	}
	if len(initialContainers) != 1 {
		t.Fatalf("expected 1 initial web container, got %d", len(initialContainers))
	}

	// verify the initial container is running via docker inspect
	inspectResult, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"inspect", "--format", "{{.State.Running}}", initialContainers[0]},
	})
	if err != nil {
		t.Fatalf("failed to inspect initial container: %v", err)
	}
	if strings.TrimSpace(inspectResult.StdoutContents()) != "true" {
		t.Errorf("expected initial container to be running")
	}

	// scale web to 2
	scaleTask := PsScaleTask{
		App:   appName,
		Scale: map[string]int{"web": 2},
		State: StatePresent,
	}
	result := scaleTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to scale app: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for scaling up")
	}

	// clean up old containers and verify 2 web containers via docker ps
	scaledContainers, err := getCurrentContainerIDs(appName, "web")
	if err != nil {
		t.Fatalf("failed to list containers after scale: %v", err)
	}
	if len(scaledContainers) != 2 {
		t.Fatalf("expected 2 web containers after scaling, got %d", len(scaledContainers))
	}

	// verify each container is running via docker inspect
	for _, containerID := range scaledContainers {
		inspectResult, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
			Command: "docker",
			Args:    []string{"inspect", "--format", "{{.State.Running}}", containerID},
		})
		if err != nil {
			t.Fatalf("failed to inspect container %s: %v", containerID, err)
		}
		if strings.TrimSpace(inspectResult.StdoutContents()) != "true" {
			t.Errorf("expected container %s to be running", containerID)
		}
	}

	// scaling again should be idempotent
	result = scaleTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent scale failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for unchanged scale")
	}

	// scale back to 1
	scaleDownTask := PsScaleTask{
		App:   appName,
		Scale: map[string]int{"web": 1},
		State: StatePresent,
	}
	result = scaleDownTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to scale down: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for scaling down")
	}

	// clean up old containers and verify 1 web container after scale down
	finalContainers, err := getCurrentContainerIDs(appName, "web")
	if err != nil {
		t.Fatalf("failed to list containers after scale down: %v", err)
	}
	if len(finalContainers) != 1 {
		t.Fatalf("expected 1 web container after scale down, got %d", len(finalContainers))
	}

	// verify the final container is running via docker inspect
	inspectResult, err = subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"inspect", "--format", "{{.State.Running}}", finalContainers[0]},
	})
	if err != nil {
		t.Fatalf("failed to inspect final container: %v", err)
	}
	if strings.TrimSpace(inspectResult.StdoutContents()) != "true" {
		t.Errorf("expected final container to be running")
	}
}

func TestIntegrationPsScaleSkipDeploy(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-psscale-sd"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// scale with skip_deploy on an undeployed app
	scaleTask := PsScaleTask{
		App:        appName,
		Scale:      map[string]int{"web": 2, "worker": 1},
		SkipDeploy: boolPtr(true),
		State:      StatePresent,
	}
	result := scaleTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to scale with skip_deploy: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for initial scale")
	}

	// verify the scale was set correctly
	scale, err := getPsScale(testCtx(), appName)
	if err != nil {
		t.Fatalf("failed to get ps scale: %v", err)
	}
	if scale["web"] != 2 {
		t.Errorf("expected web=2, got web=%d", scale["web"])
	}
	if scale["worker"] != 1 {
		t.Errorf("expected worker=1, got worker=%d", scale["worker"])
	}

	// idempotent
	result = scaleTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent scale failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for unchanged scale")
	}
}

func TestIntegrationPsScaleSet(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-psscale-set"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// Build a formation the recipe will not fully declare. skip_deploy keeps
	// this cheap, and it is also the harder path: without a deploy dokku never
	// clears the scale.old property, so the zeroed process type stays visible
	// in ps:scale and the plan has to read it as "not running" rather than as
	// drift.
	seedTask := PsScaleTask{
		App:        appName,
		Scale:      map[string]int{"web": 2, "worker": 1},
		SkipDeploy: boolPtr(true),
		State:      StatePresent,
	}
	result := seedTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to seed the formation: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for the initial scale")
	}

	// Declare web as the whole formation; worker is not named, so it goes to zero.
	setTask := PsScaleTask{
		App:        appName,
		Scale:      map[string]int{"web": 2},
		SkipDeploy: boolPtr(true),
		State:      StateSet,
	}
	result = setTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to set the formation: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true when an undeclared process type is running")
	}
	if result.State != StateSet {
		t.Errorf("expected state 'set', got '%s'", result.State)
	}

	scale, err := getPsScale(testCtx(), appName)
	if err != nil {
		t.Fatalf("failed to get ps scale: %v", err)
	}
	if scale["web"] != 2 {
		t.Errorf("expected web=2, got web=%d", scale["web"])
	}
	// The entry is still reported, at zero, because no deploy has run to delete
	// the scale.old property it was moved to.
	if scale["worker"] != 0 {
		t.Errorf("expected worker=0 after the replacement, got worker=%d", scale["worker"])
	}

	// The zeroed process type must not read as drift, or state 'set' would
	// never converge on an app scaled with skip_deploy.
	result = setTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent set failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for an unchanged formation")
	}
}
