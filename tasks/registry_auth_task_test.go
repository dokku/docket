package tasks

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestRegistryAuthTaskInvalidState(t *testing.T) {
	task := RegistryAuthTask{App: "test-app", Server: "docker.io", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestRegistryAuthTaskMissingApp(t *testing.T) {
	task := RegistryAuthTask{Server: "docker.io", Username: "u", Password: "p", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestRegistryAuthTaskGlobalWithApp(t *testing.T) {
	task := RegistryAuthTask{App: "test-app", Global: true, Server: "docker.io", Username: "u", Password: "p", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when both global and app are set")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when 'global' is set to true") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestRegistryAuthTaskMissingServer(t *testing.T) {
	for _, st := range []State{StatePresent, StateAbsent} {
		task := RegistryAuthTask{App: "test-app", Username: "u", Password: "p", State: st}
		result := task.Execute(testCtx())
		if result.Error == nil {
			t.Fatalf("expected error with empty server (state=%s)", st)
		}
		if !strings.Contains(result.Error.Error(), "'server' is required") {
			t.Errorf("state=%s: unexpected error: %v", st, result.Error)
		}
	}
}

func TestRegistryAuthTaskPresentMissingUsername(t *testing.T) {
	task := RegistryAuthTask{App: "test-app", Server: "docker.io", Password: "p", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without username should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'username' and 'password' are required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestRegistryAuthTaskPresentMissingPassword(t *testing.T) {
	task := RegistryAuthTask{App: "test-app", Server: "docker.io", Username: "u", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without password should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'username' and 'password' are required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGetTasksRegistryAuthTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: log in to ghcr
      dokku_registry_auth:
        app: test-app
        server: ghcr.io
        username: deploy-bot
        password: ghp_examplepat
        state: present
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("log in to ghcr")
	if task == nil {
		t.Fatal("task 'log in to ghcr' not found")
	}

	authTask, ok := task.(*RegistryAuthTask)
	if !ok {
		t.Fatalf("task is not a RegistryAuthTask (type is %T)", task)
	}
	if authTask.App != "test-app" {
		t.Errorf("App = %q, want %q", authTask.App, "test-app")
	}
	if authTask.Server != "ghcr.io" {
		t.Errorf("Server = %q, want %q", authTask.Server, "ghcr.io")
	}
	if authTask.Username != "deploy-bot" {
		t.Errorf("Username = %q, want %q", authTask.Username, "deploy-bot")
	}
	if authTask.Password != "ghp_examplepat" {
		t.Errorf("Password = %q, want %q", authTask.Password, "ghp_examplepat")
	}
	if authTask.State != StatePresent {
		t.Errorf("State = %q, want %q", authTask.State, StatePresent)
	}
}

func TestRegistryAuthTaskPresentWhitespacePassword(t *testing.T) {
	task := RegistryAuthTask{App: "test-app", Server: "docker.io", Username: "u", Password: "   ", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with an all-whitespace password should return an error")
	}
	if !strings.Contains(result.Error.Error(), "must not be entirely whitespace") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

// registryAuthExits stubs every dokku call as a clean exit with the given code
// and stderr, which is how registry:auth-status answers. fakeDokku always exits
// 0, so it can only express the in-sync case.
func registryAuthExits(code int, stderr string) func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	return func(_ context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{ExitCode: code, Stderr: stderr}, nil
	}
}

func registryAuthPresentTask() RegistryAuthTask {
	return RegistryAuthTask{
		App:      "api",
		Server:   "ghcr.io",
		Username: "deploy-bot",
		Password: "ghp_examplepat",
		State:    StatePresent,
	}
}

func TestRegistryAuthTaskPresentInSync(t *testing.T) {
	t.Parallel()
	plan := registryAuthPresentTask().Plan(subprocess.ContextWithRunner(testCtx(), fakeDokku(nil)))
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Fatalf("plan = {InSync:%v Status:%q}, want an in-sync ok", plan.InSync, plan.Status)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("an in-sync plan must issue no commands, got %v", plan.Commands)
	}
	if len(plan.Warnings) != 0 {
		t.Errorf("an in-sync plan must raise no warnings, got %v", plan.Warnings)
	}
}

// TestRegistryAuthTaskPresentDrifts walks every answer registry:auth-status can
// give the present-state question. The three that matter are the split between
// a server that has no credential (a create) and one whose credential differs
// (a replacement), and the indeterminate case, where dokku found a credential
// it cannot read: docket applies rather than assuming a match, and says why.
func TestRegistryAuthTaskPresentDrifts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		code        int
		stderr      string
		wantStatus  PlanStatus
		wantReason  string
		wantWarning bool
	}{
		{
			name:       "no credential stored is a create",
			code:       1,
			wantStatus: PlanStatusCreate,
			wantReason: "no registry credential stored",
		},
		{
			name:       "a missing app is a create",
			code:       20,
			stderr:     " !     App api does not exist",
			wantStatus: PlanStatusCreate,
			wantReason: "no registry credential stored",
		},
		{
			name:       "a differing credential is a modify",
			code:       2,
			wantStatus: PlanStatusModify,
			wantReason: "registry credential does not match",
		},
		{
			name:        "an unreadable credential is a modify with a warning",
			code:        4,
			stderr:      " !     Unable to compare the credential for ghcr.io: the osxkeychain credential helper holds it",
			wantStatus:  PlanStatusModify,
			wantReason:  "registry credential could not be compared",
			wantWarning: true,
		},
		{
			name:        "an older dokku falls back to the unprobed wording",
			code:        1,
			stderr:      " !     `registry:auth-status` is not a dokku command.\n !     See `dokku help` for a list of available commands.",
			wantStatus:  PlanStatusModify,
			wantReason:  "registry login state not probed",
			wantWarning: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := subprocess.ContextWithRunner(testCtx(), registryAuthExits(tc.code, tc.stderr))
			plan := registryAuthPresentTask().Plan(ctx)
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if plan.InSync || plan.Status != tc.wantStatus {
				t.Fatalf("plan = {InSync:%v Status:%q}, want %q", plan.InSync, plan.Status, tc.wantStatus)
			}
			if plan.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", plan.Reason, tc.wantReason)
			}
			if !reflect.DeepEqual(plan.Mutations, []string{"registry:login api ghcr.io as deploy-bot"}) {
				t.Errorf("Mutations = %v, want [registry:login api ghcr.io as deploy-bot]", plan.Mutations)
			}
			if len(plan.Commands) != 1 || !strings.HasSuffix(plan.Commands[0], "registry:login --password-stdin api ghcr.io deploy-bot") {
				t.Fatalf("expected a single registry:login command, got %v", plan.Commands)
			}
			if got := len(plan.Warnings) > 0; got != tc.wantWarning {
				t.Fatalf("warnings = %v, want a warning: %v", plan.Warnings, tc.wantWarning)
			}
			if tc.wantWarning {
				if plan.Warnings[0].Reason != WarnReasonProbeIndeterminate {
					t.Errorf("warning reason = %q, want %q", plan.Warnings[0].Reason, WarnReasonProbeIndeterminate)
				}
				// The ` !     ` prefix dokku puts in front of a failure, and
				// the second line pointing at `dokku help`, are both noise in
				// a warning that already names the task.
				if strings.Contains(plan.Warnings[0].Message, "!") || strings.Contains(plan.Warnings[0].Message, "dokku help") {
					t.Errorf("warning message carries dokku's framing: %q", plan.Warnings[0].Message)
				}
			}
			// The password rides on stdin, so neither the rendered command nor
			// the itemized mutations may carry it.
			for _, s := range append(append([]string{}, plan.Commands...), plan.Mutations...) {
				if strings.Contains(s, "ghp_examplepat") {
					t.Errorf("password leaked into plan output: %q", s)
				}
			}
		})
	}
}

func TestRegistryAuthTaskAbsentInSync(t *testing.T) {
	t.Parallel()
	// Exit 0 is "no credential exists", the question an absent-state probe
	// asks. Exit 20 is an app that does not exist, which can hold no
	// credential either - and where planning a logout would guarantee a
	// failure, since registry:logout verifies the app name.
	for _, code := range []int{0, 20} {
		ctx := subprocess.ContextWithRunner(testCtx(), registryAuthExits(code, ""))
		plan := RegistryAuthTask{App: "api", Server: "ghcr.io", State: StateAbsent}.Plan(ctx)
		if plan.Error != nil {
			t.Fatalf("exit %d: unexpected plan error: %v", code, plan.Error)
		}
		if !plan.InSync || plan.Status != PlanStatusOK {
			t.Fatalf("exit %d: plan = {InSync:%v Status:%q}, want an in-sync ok", code, plan.InSync, plan.Status)
		}
		if len(plan.Commands) != 0 {
			t.Errorf("exit %d: an in-sync plan must issue no commands, got %v", code, plan.Commands)
		}
	}
}

func TestRegistryAuthTaskAbsentDrifts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		code        int
		stderr      string
		wantReason  string
		wantWarning bool
	}{
		{
			name:       "a stored credential is a destroy",
			code:       2,
			wantReason: "registry credential present",
		},
		{
			// checkAuthStatus answers 4 from a config.json it cannot parse
			// before it ever looks at the username, so the absent question
			// reaches it too.
			name:        "an unreadable config is a destroy with a warning",
			code:        4,
			stderr:      " !     Unable to parse the docker config for ghcr.io: unexpected end of JSON input",
			wantReason:  "registry credential could not be compared",
			wantWarning: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := subprocess.ContextWithRunner(testCtx(), registryAuthExits(tc.code, tc.stderr))
			plan := RegistryAuthTask{App: "api", Server: "ghcr.io", State: StateAbsent}.Plan(ctx)
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if plan.InSync || plan.Status != PlanStatusDestroy {
				t.Fatalf("plan = {InSync:%v Status:%q}, want a destroy", plan.InSync, plan.Status)
			}
			if plan.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", plan.Reason, tc.wantReason)
			}
			if len(plan.Commands) != 1 || !strings.HasSuffix(plan.Commands[0], "registry:logout api ghcr.io") {
				t.Fatalf("expected a single registry:logout command, got %v", plan.Commands)
			}
			if got := len(plan.Warnings) > 0; got != tc.wantWarning {
				t.Errorf("warnings = %v, want a warning: %v", plan.Warnings, tc.wantWarning)
			}
		})
	}
}

// TestRegistryAuthTaskInvalidArgumentsError pins that an invalid-arguments exit
// is reported rather than acted on. Every argument is docket's own and the
// recipe-level ones are checked offline, so reaching it means dokku could not
// answer the question that was asked. The stderr and exit code ride on the
// PlanResult so a failed_when predicate can read them.
func TestRegistryAuthTaskInvalidArgumentsError(t *testing.T) {
	t.Parallel()
	for _, state := range []State{StatePresent, StateAbsent} {
		task := RegistryAuthTask{App: "Not A Name", Server: "ghcr.io", State: state}
		if state == StatePresent {
			task.Username, task.Password = "deploy-bot", "ghp_examplepat"
		}
		ctx := subprocess.ContextWithRunner(testCtx(), registryAuthExits(3, " !     App name must not contain spaces"))
		plan := task.Plan(ctx)
		if plan.Error == nil || plan.Status != PlanStatusError {
			t.Fatalf("state %q: plan = {Status:%q Error:%v}, want a plan error", state, plan.Status, plan.Error)
		}
		if plan.ExitCode != 3 {
			t.Errorf("state %q: ExitCode = %d, want 3", state, plan.ExitCode)
		}
		if !strings.Contains(plan.Stderr, "must not contain spaces") {
			t.Errorf("state %q: Stderr = %q, want dokku's message", state, plan.Stderr)
		}
		if len(plan.Commands) != 0 {
			t.Errorf("state %q: a probe error must issue no commands, got %v", state, plan.Commands)
		}
	}
}

// TestRegistryAuthTaskProbeArgs pins the argv of every probe shape. The
// present-state probe names the username so registry:auth-status has something
// to compare, and must stop there: a third positional would put the password in
// the dokku host's process table, which is the whole reason it goes over stdin.
// The absent-state probe names no username at all, which is how the command
// spells "assert nothing is stored".
func TestRegistryAuthTaskProbeArgs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		task RegistryAuthTask
		want string
	}{
		{
			name: "app present compares the credentials",
			task: RegistryAuthTask{App: "api", Server: "ghcr.io", Username: "deploy-bot", Password: "ghp_examplepat", State: StatePresent},
			want: "--quiet registry:auth-status --password-stdin api ghcr.io deploy-bot",
		},
		{
			name: "global present compares the credentials",
			task: RegistryAuthTask{Global: true, Server: "docker.io", Username: "deploy-bot", Password: "examplepassword", State: StatePresent},
			want: "--quiet registry:auth-status --password-stdin --global docker.io deploy-bot",
		},
		{
			name: "app absent asks whether any credential exists",
			task: RegistryAuthTask{App: "api", Server: "ghcr.io", State: StateAbsent},
			want: "--quiet registry:auth-status api ghcr.io",
		},
		{
			name: "global absent asks whether any credential exists",
			task: RegistryAuthTask{Global: true, Server: "docker.io", State: StateAbsent},
			want: "--quiet registry:auth-status --global docker.io",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls []string
			ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(nil, &calls))
			tc.task.Plan(ctx)
			if len(calls) != 1 {
				t.Fatalf("expected exactly one probe call, got %v", calls)
			}
			if calls[0] != tc.want {
				t.Errorf("probe args = %q, want %q", calls[0], tc.want)
			}
		})
	}
}

// TestRegistryAuthTaskStreamsPasswordToProbeAndApply pins that the password
// reaches both dokku calls over stdin. The two must build separate readers: an
// io.Reader is single-use, so a probe sharing the apply's reader would drain it
// and leave registry:login with an empty password.
func TestRegistryAuthTaskStreamsPasswordToProbeAndApply(t *testing.T) {
	t.Parallel()
	var stdins []string
	runner := func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		if in.Stdin != nil {
			b, _ := io.ReadAll(in.Stdin)
			stdins = append(stdins, string(b))
		} else {
			stdins = append(stdins, "")
		}
		// non-zero so the probe reports drift and the apply runs too
		return subprocess.ExecCommandResponse{ExitCode: 2}, nil
	}

	ctx := subprocess.ContextWithRunner(testCtx(), runner)
	result := registryAuthPresentTask().Execute(ctx)
	if result.Error != nil {
		t.Fatalf("unexpected execute error: %v", result.Error)
	}
	if !reflect.DeepEqual(stdins, []string{"ghp_examplepat", "ghp_examplepat"}) {
		t.Errorf("stdin per call = %q, want the password on both the probe and the apply", stdins)
	}
}

func TestRegistryAuthTaskAbsentSendsNoStdin(t *testing.T) {
	t.Parallel()
	var sawStdin bool
	runner := func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		if in.Stdin != nil {
			sawStdin = true
		}
		return subprocess.ExecCommandResponse{ExitCode: 2}, nil
	}

	ctx := subprocess.ContextWithRunner(testCtx(), runner)
	RegistryAuthTask{App: "api", Server: "ghcr.io", State: StateAbsent}.Plan(ctx)
	if sawStdin {
		t.Error("the absent-state probe must send no stdin; there is no password to compare")
	}
}

// TestRegistryAuthTaskProbeTransportFailure pins that a probe which never ran
// is an error rather than drift. Reading an unreachable host as "the credential
// does not match" would have every plan against it report a login to run.
func TestRegistryAuthTaskProbeTransportFailure(t *testing.T) {
	t.Parallel()
	runner := func(_ context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{}, &subprocess.SSHError{Host: "dokku@example.com"}
	}

	for _, state := range []State{StatePresent, StateAbsent} {
		task := RegistryAuthTask{App: "api", Server: "ghcr.io", State: state}
		if state == StatePresent {
			task.Username, task.Password = "deploy-bot", "ghp_examplepat"
		}
		plan := task.Plan(subprocess.ContextWithRunner(testCtx(), runner))
		if plan.Error == nil || plan.Status != PlanStatusError {
			t.Errorf("state %q: plan = {Status:%q Error:%v}, want a plan error", state, plan.Status, plan.Error)
		}
	}
}
