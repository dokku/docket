package tasks

import (
	"github.com/dokku/docket/subprocess"
	"strings"
	"testing"
)

func TestIntegrationPsScale(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-psscale"

	// ensure clean state
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// deploy the smoke test app so we have running containers to scale
	deployTask := GitFromImageTask{
		App:   appName,
		Image: "dokku/smoke-test-app:dockerfile",
		State: StateDeployed,
	}
	deployResult := deployTask.Execute()
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
	inspectResult, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
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
	result := scaleTask.Execute()
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
		inspectResult, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
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
	result = scaleTask.Execute()
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
	result = scaleDownTask.Execute()
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
	inspectResult, err = subprocess.CallExecCommand(subprocess.ExecCommandInput{
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

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// scale with skip_deploy on an undeployed app
	scaleTask := PsScaleTask{
		App:        appName,
		Scale:      map[string]int{"web": 2, "worker": 1},
		SkipDeploy: boolPtr(true),
		State:      StatePresent,
	}
	result := scaleTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to scale with skip_deploy: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for initial scale")
	}

	// verify the scale was set correctly
	scale, err := getPsScale(appName)
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
	result = scaleTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent scale failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for unchanged scale")
	}
}
