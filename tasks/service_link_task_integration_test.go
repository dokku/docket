package tasks

import (
	"github.com/dokku/docket/subprocess"
	"fmt"
	"strings"
	"testing"
)

func TestIntegrationServiceLinkAndUnlink(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")
	skipIfDockerLinkUnsupportedT(t)

	appName := "docket-test-link-app"
	serviceName := "docket-test-link-svc"
	serviceType := "redis"

	// ensure clean state
	destroyApp(testCtx(), appName)
	destroyService(testCtx(), serviceType, serviceName)

	// create prerequisites
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	createTask := ServiceCreateTask{Service: serviceType, Name: serviceName, State: StatePresent}
	createResult := createTask.Execute(testCtx())
	if createResult.Error != nil {
		t.Fatalf("failed to create service: %v", createResult.Error)
	}
	defer func() {
		// unlink before destroying service
		unlinkTask := ServiceLinkTask{App: appName, Service: serviceType, Name: serviceName, State: StateAbsent}
		unlinkTask.Execute(testCtx())
		destroyService(testCtx(), serviceType, serviceName)
	}()

	// verify service container is running via docker inspect
	containerName := fmt.Sprintf("dokku.%s.%s", serviceType, serviceName)
	inspectResult, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"inspect", "--format", "{{.State.Running}}", containerName},
	})
	if err != nil {
		t.Fatalf("failed to inspect service container: %v", err)
	}
	if strings.TrimSpace(inspectResult.StdoutContents()) != "true" {
		t.Errorf("expected service container %q to be running", containerName)
	}

	// link service to app
	linkTask := ServiceLinkTask{App: appName, Service: serviceType, Name: serviceName, State: StatePresent}
	result := linkTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to link service: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for new service link")
	}

	// verify REDIS_URL config var was set by the link
	configResult, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"config:get", appName, "REDIS_URL"},
	})
	if err != nil {
		t.Fatalf("failed to get REDIS_URL after link: %v", err)
	}
	redisURL := strings.TrimSpace(configResult.StdoutContents())
	if redisURL == "" {
		t.Error("expected REDIS_URL to be set after linking service")
	}
	if !strings.HasPrefix(redisURL, "redis://") {
		t.Errorf("expected REDIS_URL to start with 'redis://', got %q", redisURL)
	}

	// verify the service container exposes the expected network alias via docker inspect
	aliasResult, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"inspect", "--format", "{{.Config.Hostname}}", containerName},
	})
	if err != nil {
		t.Fatalf("failed to inspect service container hostname: %v", err)
	}
	if strings.TrimSpace(aliasResult.StdoutContents()) == "" {
		t.Error("expected service container to have a hostname set")
	}

	// deploy the smoke test app so we can verify the link inside a running container
	deployTask := GitFromImageTask{
		App:   appName,
		Image: "dokku/smoke-test-app:dockerfile",
		State: StateDeployed,
	}
	deployResult := deployTask.Execute(testCtx())
	if deployResult.Error != nil {
		t.Fatalf("failed to deploy app: %v", deployResult.Error)
	}

	// find the running app container
	appContainerResult, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"ps", "--filter", fmt.Sprintf("label=com.dokku.app-name=%s", appName), "--filter", "label=com.dokku.process-type=web", "--format", "{{.ID}}"},
	})
	if err != nil {
		t.Fatalf("failed to find app container: %v", err)
	}
	appContainerID := strings.TrimSpace(appContainerResult.StdoutContents())
	if appContainerID == "" {
		t.Fatal("expected at least one running app container after deploy")
	}
	// take the first container if multiple lines
	appContainerIDs := strings.Split(appContainerID, "\n")
	appContainerID = appContainerIDs[0]

	// verify the app container is running via docker inspect
	appInspectResult, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"inspect", "--format", "{{.State.Running}}", appContainerID},
	})
	if err != nil {
		t.Fatalf("failed to inspect app container: %v", err)
	}
	if strings.TrimSpace(appInspectResult.StdoutContents()) != "true" {
		t.Error("expected app container to be running")
	}

	// verify REDIS_URL is present inside the running container via docker exec
	execResult, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"exec", appContainerID, "env"},
	})
	if err != nil {
		t.Fatalf("failed to exec env in app container: %v", err)
	}
	envOutput := execResult.StdoutContents()
	if !strings.Contains(envOutput, "REDIS_URL=redis://") {
		t.Error("expected REDIS_URL=redis://... to be present in app container environment")
	}

	// linking again should be idempotent
	result = linkTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent link failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for existing service link")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}

	// unlink service from app
	unlinkTask := ServiceLinkTask{App: appName, Service: serviceType, Name: serviceName, State: StateAbsent}
	result = unlinkTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to unlink service: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for service unlink")
	}

	// verify REDIS_URL config var was removed by the unlink
	configResult, err = subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"config:get", appName, "REDIS_URL"},
	})
	if err == nil && strings.TrimSpace(configResult.StdoutContents()) != "" {
		t.Error("expected REDIS_URL to be unset after unlinking service")
	}

	// unlinking again should be idempotent
	result = unlinkTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent unlink failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for already-unlinked service")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
}
