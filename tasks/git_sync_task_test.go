package tasks

import (
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestGitSyncTaskInvalidState(t *testing.T) {
	task := GitSyncTask{
		App:    "test-app",
		Remote: "https://example.com/repo",
		State:  "invalid",
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestGitSyncInSyncOnRemoteAndDeployBranch(t *testing.T) {
	// The stored metadata SHA differs from the pinned ref, but the remote and
	// the persisted deploy-branch match, so the app is in sync.
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"apps:report test-app --format json": `{"app-deploy-source":"git-sync","app-deploy-source-metadata":"https://github.com/org/repo.git#abc123def456"}`,
		"git:report test-app --format json":  `{"deploy-branch":"main"}`,
	}))()

	plan := GitSyncTask{App: "test-app", Remote: "https://github.com/org/repo.git", GitRef: "main", Build: true, State: StatePresent}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in-sync when remote and deploy-branch match, got %#v", plan)
	}
}

func TestGitSyncDriftOnRefChange(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"apps:report test-app --format json": `{"app-deploy-source":"git-sync","app-deploy-source-metadata":"https://github.com/org/repo.git#abc123"}`,
		"git:report test-app --format json":  `{"deploy-branch":"main"}`,
	}))()

	plan := GitSyncTask{App: "test-app", Remote: "https://github.com/org/repo.git", GitRef: "develop", Build: true, State: StatePresent}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the pinned ref differs from the stored deploy-branch")
	}
}

func TestGitSyncDriftOnRemoteChange(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"apps:report test-app --format json": `{"app-deploy-source":"git-sync","app-deploy-source-metadata":"https://github.com/org/other.git#abc123"}`,
	}))()

	plan := GitSyncTask{App: "test-app", Remote: "https://github.com/org/repo.git", GitRef: "main", State: StatePresent}.Plan()
	if plan.InSync {
		t.Fatal("expected drift when the remote differs")
	}
}

func TestGitSyncInSyncWithoutRef(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"apps:report test-app --format json": `{"app-deploy-source":"git-sync","app-deploy-source-metadata":"https://github.com/org/repo.git#abc123"}`,
	}))()

	plan := GitSyncTask{App: "test-app", Remote: "https://github.com/org/repo.git", State: StatePresent}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in-sync on a remote match when no ref is pinned, got %#v", plan)
	}
}

func TestGitSyncSkipDeployBranchMatchesOnRemote(t *testing.T) {
	// With skip_deploy_branch the ref is not persisted, so a matching remote is
	// treated as in sync rather than re-syncing forever.
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"apps:report test-app --format json": `{"app-deploy-source":"git-sync","app-deploy-source-metadata":"https://github.com/org/repo.git#abc123"}`,
	}))()

	plan := GitSyncTask{App: "test-app", Remote: "https://github.com/org/repo.git", GitRef: "main", SkipDeployBranch: true, State: StatePresent}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in-sync via remote-only match when skip_deploy_branch is set, got %#v", plan)
	}
}

func TestGitSyncDriftWhenNotGitSyncSource(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"apps:report test-app --format json": `{"app-deploy-source":"","app-deploy-source-metadata":""}`,
	}))()

	plan := GitSyncTask{App: "test-app", Remote: "https://github.com/org/repo.git", GitRef: "main", State: StatePresent}.Plan()
	if plan.InSync {
		t.Fatal("expected drift when the app has no git-sync deploy source")
	}
}

func TestGitSyncExportUsesDeployBranchAsRef(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"apps:report test-app --format json": `{"app-deploy-source":"git-sync","app-deploy-source-metadata":"https://github.com/org/repo.git#abc123def"}`,
		"git:report test-app --format json":  `{"deploy-branch":"main"}`,
	}))()

	bodies, err := GitSyncTask{}.ExportApp("test-app")
	if err != nil {
		t.Fatalf("ExportApp error: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 exported task, got %d", len(bodies))
	}
	got, ok := bodies[0].(GitSyncTask)
	if !ok {
		t.Fatalf("unexpected exported type %T", bodies[0])
	}
	if got.Remote != "https://github.com/org/repo.git" {
		t.Errorf("Remote = %q, want the metadata remote", got.Remote)
	}
	if got.GitRef != "main" {
		t.Errorf("GitRef = %q, want the deploy-branch 'main' rather than the SHA", got.GitRef)
	}
}
