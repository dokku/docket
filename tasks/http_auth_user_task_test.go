package tasks

import (
	"context"
	"io"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestHttpAuthUserTaskInvalidState(t *testing.T) {
	t.Parallel()
	task := HttpAuthUserTask{App: "test-app", Users: []HttpAuthUser{{Username: "admin", Password: "secret"}}, State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestHttpAuthUserTaskPresentMissingApp(t *testing.T) {
	t.Parallel()
	task := HttpAuthUserTask{Users: []HttpAuthUser{{Username: "admin", Password: "secret"}}, State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthUserTaskAbsentMissingApp(t *testing.T) {
	t.Parallel()
	task := HttpAuthUserTask{State: StateAbsent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthUserTaskPresentEmptyUsers(t *testing.T) {
	t.Parallel()
	task := HttpAuthUserTask{App: "test-app", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with empty users and state=present should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'users' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthUserTaskPresentWithoutCredential(t *testing.T) {
	t.Parallel()
	task := HttpAuthUserTask{App: "test-app", Users: []HttpAuthUser{{Username: "admin"}}, State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when a present user has neither password nor hash")
	}
	if !strings.Contains(result.Error.Error(), "one of 'password' or 'hash' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthUserTaskSetWithoutCredential(t *testing.T) {
	t.Parallel()
	task := HttpAuthUserTask{App: "test-app", Users: []HttpAuthUser{{Username: "admin"}}, State: StateSet}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when a set user has neither password nor hash")
	}
	if !strings.Contains(result.Error.Error(), "one of 'password' or 'hash' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthUserTaskSetEmptyUsers(t *testing.T) {
	t.Parallel()
	task := HttpAuthUserTask{App: "test-app", State: StateSet}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when state=set has no users")
	}
	if !strings.Contains(result.Error.Error(), "'users' must not be empty for state 'set'") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

// TestHttpAuthUserTaskPasswordAndHashAreExclusive pins the either/or that the
// generated parameter table cannot express: the two credential forms dispatch
// to different dokku commands, so a user carrying both has no defined meaning.
func TestHttpAuthUserTaskPasswordAndHashAreExclusive(t *testing.T) {
	t.Parallel()
	task := HttpAuthUserTask{
		App:   "test-app",
		Users: []HttpAuthUser{{Username: "admin", Password: "secret", Hash: "$6$abc$def"}},
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when a user carries both password and hash")
	}
	if !strings.Contains(result.Error.Error(), "mutually exclusive") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthUserTaskDuplicateUsernameRejected(t *testing.T) {
	t.Parallel()
	// Once the credential forms split across import-users and add-user, the
	// winner for a repeated username would depend on nothing but list order.
	task := HttpAuthUserTask{
		App: "test-app",
		Users: []HttpAuthUser{
			{Username: "admin", Hash: "$6$abc$def"},
			{Username: "admin", Password: "secret"},
		},
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when a username is listed twice")
	}
	if !strings.Contains(result.Error.Error(), "listed more than once") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

// TestHttpAuthUserTaskHashFramingRejected covers the guard on the stdin stream
// docket frames itself: a colon in the username or a newline in either half
// would write a corrupt htpasswd through http-auth:import-users.
func TestHttpAuthUserTaskHashFramingRejected(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		user HttpAuthUser
		want string
	}{
		{
			name: "colon in username",
			user: HttpAuthUser{Username: "ad:min", Hash: "$6$abc$def"},
			want: "must not contain ':'",
		},
		{
			name: "newline in hash",
			user: HttpAuthUser{Username: "admin", Hash: "$6$abc$def\nroot:$6$evil"},
			want: "must not contain newlines",
		},
		{
			name: "newline in username",
			user: HttpAuthUser{Username: "admin\nroot", Hash: "$6$abc$def"},
			want: "must not contain newlines",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := HttpAuthUserTask{App: "test-app", Users: []HttpAuthUser{tc.user}}
			result := task.Execute(testCtx())
			if result.Error == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(result.Error.Error(), tc.want) {
				t.Errorf("unexpected error: %v", result.Error)
			}
		})
	}
}

// TestHttpAuthUserTaskPasswordUsernameColonAllowed pins that the framing guard
// is scoped to the hash form. A password goes on argv, never into the htpasswd
// stream docket writes, so no recipe that validates today starts failing.
func TestHttpAuthUserTaskPasswordUsernameColonAllowed(t *testing.T) {
	t.Parallel()
	task := HttpAuthUserTask{App: "test-app", Users: []HttpAuthUser{{Username: "ad:min", Password: "secret"}}}
	if err := task.Validate(); err != nil {
		t.Errorf("password users should not be subject to the stdin framing guard, got: %v", err)
	}
}

func TestHttpAuthUserTaskMissingUsername(t *testing.T) {
	t.Parallel()
	task := HttpAuthUserTask{App: "test-app", Users: []HttpAuthUser{{Password: "secret"}}, State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when a user has no username")
	}
	if !strings.Contains(result.Error.Error(), "'username' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthUserTaskSensitiveValues(t *testing.T) {
	t.Parallel()
	task := HttpAuthUserTask{
		App: "test-app",
		Users: []HttpAuthUser{
			{Username: "admin", Password: "secret"},
			{Username: "ops", Password: "hunter2"},
			{Username: "deploy", Hash: "$6$salt$hash"},
			{Username: "guest"},
		},
	}
	got := task.SensitiveValues()
	sort.Strings(got)
	// The hash reproduces the user without the password behind it, so it is
	// masked alongside the cleartext values.
	want := []string{"$6$salt$hash", "hunter2", "secret"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SensitiveValues() = %v, want %v", got, want)
	}
}

// httpAuthUserFixture is the canned server the hash plan tests share: alice and
// bob exist with the given hashes, and both the report and export-users answer
// consistently.
func httpAuthUserFixture(entries string, usernames string) map[string]string {
	return map[string]string{
		"http-auth:report test-app --format json": `{"enabled":"true","users":"` + usernames + `","allowed-ips":"","domains":""}`,
		"--quiet http-auth:export-users test-app": entries,
	}
}

const (
	aliceHash = "$6$aaaa$AAAA"
	bobHash   = "$6$bbbb$BBBB"
)

// TestHttpAuthUserTaskHashInSync is the property the cleartext path could never
// have: because http-auth:export-users reads the stored hash back, a task that
// already matches the server plans nothing.
func TestHttpAuthUserTaskHashInSync(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthUserFixture(
		"alice:"+aliceHash+"\nbob:"+bobHash+"\n", "alice bob")))

	plan := HttpAuthUserTask{
		App:   "test-app",
		Users: []HttpAuthUser{{Username: "alice", Hash: aliceHash}},
		State: StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in-sync for a matching hash, got status %v reason %q", plan.Status, plan.Reason)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("expected no commands when in sync, got %v", plan.Commands)
	}
}

// TestHttpAuthUserTaskHashIgnoresUpdatePassword guards the branch ordering: the
// stored-hash comparison has to win over the update_password arm, or a matching
// hash user would be re-imported on every run and Changed would never be false.
func TestHttpAuthUserTaskHashIgnoresUpdatePassword(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthUserFixture(
		"alice:"+aliceHash+"\n", "alice")))

	plan := HttpAuthUserTask{
		App:            "test-app",
		Users:          []HttpAuthUser{{Username: "alice", Hash: aliceHash}},
		UpdatePassword: boolPtr(true),
		State:          StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Errorf("update_password must not disturb an already-matching hash, got status %v reason %q", plan.Status, plan.Reason)
	}
}

func TestHttpAuthUserTaskHashDriftPlansImport(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthUserFixture(
		"alice:"+aliceHash+"\n", "alice")))

	plan := HttpAuthUserTask{
		App:   "test-app",
		Users: []HttpAuthUser{{Username: "alice", Hash: "$6$rotated$ROTATED"}},
		State: StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the stored hash differs")
	}
	if len(plan.Commands) != 1 || !strings.HasSuffix(plan.Commands[0], "http-auth:import-users test-app") {
		t.Fatalf("expected a single import-users command, got %v", plan.Commands)
	}
	if !reflect.DeepEqual(plan.Mutations, []string{"update alice"}) {
		t.Errorf("Mutations = %v, want [update alice]", plan.Mutations)
	}
	// The hash rides on stdin, so neither the rendered command nor the
	// itemized mutations may carry it.
	for _, s := range append(append([]string{}, plan.Commands...), plan.Mutations...) {
		if strings.Contains(s, "ROTATED") {
			t.Errorf("hash leaked into plan output: %q", s)
		}
	}
}

// TestHttpAuthUserTaskMixedPlansImportFirst pins the ordering: import-users
// validates its whole stream before writing, so it runs before any add-user
// that could otherwise land first and leave a partial apply behind.
func TestHttpAuthUserTaskMixedPlansImportFirst(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthUserFixture("", "")))

	plan := HttpAuthUserTask{
		App: "test-app",
		Users: []HttpAuthUser{
			{Username: "carol", Password: "secret"},
			{Username: "alice", Hash: aliceHash},
		},
		State: StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if len(plan.Commands) != 2 {
		t.Fatalf("expected two commands, got %v", plan.Commands)
	}
	if !strings.Contains(plan.Commands[0], "http-auth:import-users") {
		t.Errorf("import-users must run first, got %v", plan.Commands)
	}
	if !strings.Contains(plan.Commands[1], "http-auth:add-user") {
		t.Errorf("add-user must follow the import, got %v", plan.Commands)
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("Status = %v, want create for an app with no users", plan.Status)
	}
}

// TestHttpAuthUserTaskPasswordOnlySkipsExportUsers keeps the cleartext path on
// exactly one report round trip: export-users is a hash overlay, not a
// replacement for the membership probe, so a password-only recipe still works
// against a plugin that predates the command.
func TestHttpAuthUserTaskPasswordOnlySkipsExportUsers(t *testing.T) {
	t.Parallel()
	asked := false
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		joined := strings.Join(in.Args, " ")
		if strings.Contains(joined, "export-users") {
			asked = true
		}
		if strings.Contains(joined, "http-auth:report") {
			return subprocess.ExecCommandResponse{Stdout: `{"enabled":"true","users":"alice","allowed-ips":"","domains":""}`}, nil
		}
		return subprocess.ExecCommandResponse{}, nil
	})

	HttpAuthUserTask{
		App:   "test-app",
		Users: []HttpAuthUser{{Username: "alice", Password: "secret"}},
		State: StatePresent,
	}.Plan(ctx)
	if asked {
		t.Error("a password-only task must not probe http-auth:export-users")
	}
}

// TestHttpAuthUserTaskExecuteStreamsEntries asserts the payload that never
// appears in plan output: resolveCommands ignores Stdin, so the only way to see
// what import-users receives is to read the reader off the exec input.
func TestHttpAuthUserTaskExecuteStreamsEntries(t *testing.T) {
	t.Parallel()
	var payload string
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		joined := strings.Join(in.Args, " ")
		if strings.Contains(joined, "http-auth:report") {
			return subprocess.ExecCommandResponse{Stdout: `{"enabled":"true","users":"alice bob","allowed-ips":"","domains":""}`}, nil
		}
		if strings.Contains(joined, "export-users") {
			return subprocess.ExecCommandResponse{Stdout: "alice:" + aliceHash + "\nbob:" + bobHash + "\n"}, nil
		}
		if in.Stdin != nil {
			// Read once: the reader is single-use, so a second Execute in this
			// test would see an empty stream.
			b, err := io.ReadAll(in.Stdin)
			if err != nil {
				t.Fatalf("read stdin: %v", err)
			}
			payload = string(b)
		}
		return subprocess.ExecCommandResponse{}, nil
	})

	result := HttpAuthUserTask{
		App: "test-app",
		Users: []HttpAuthUser{
			{Username: "alice", Hash: aliceHash},
			{Username: "bob", Hash: "$6$rotated$ROTATED"},
			{Username: "carol", Hash: "$6$cccc$CCCC"},
		},
		State: StatePresent,
	}.Execute(ctx)
	if result.Error != nil {
		t.Fatalf("Execute: %v", result.Error)
	}
	// alice already matches, so she is absent from the stream.
	want := "bob:$6$rotated$ROTATED\ncarol:$6$cccc$CCCC\n"
	if payload != want {
		t.Errorf("import-users stdin = %q, want %q", payload, want)
	}
}

func TestHttpAuthUserTaskSetAllHashesUsesReplace(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthUserFixture(
		"alice:"+aliceHash+"\nbob:"+bobHash+"\n", "alice bob")))

	plan := HttpAuthUserTask{
		App:   "test-app",
		Users: []HttpAuthUser{{Username: "alice", Hash: aliceHash}},
		State: StateSet,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift while bob is still on the app")
	}
	if len(plan.Commands) != 1 || !strings.HasSuffix(plan.Commands[0], "http-auth:import-users test-app --replace") {
		t.Fatalf("expected a single --replace import, got %v", plan.Commands)
	}
	if !reflect.DeepEqual(plan.Mutations, []string{"remove bob"}) {
		t.Errorf("Mutations = %v, want [remove bob]", plan.Mutations)
	}
	// A whole-set replacement is one operation, matching the domains and ports
	// exporters, so removals alone still read as a modify.
	if plan.Status != PlanStatusModify {
		t.Errorf("Status = %v, want modify", plan.Status)
	}
}

// TestHttpAuthUserTaskSetWithPasswordAvoidsReplace covers the fallback: a
// cleartext password cannot be written into the htpasswd stream, so the exact
// set is reached by removing the strays and upserting instead.
func TestHttpAuthUserTaskSetWithPasswordAvoidsReplace(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthUserFixture(
		"alice:"+aliceHash+"\nbob:"+bobHash+"\n", "alice bob")))

	plan := HttpAuthUserTask{
		App: "test-app",
		Users: []HttpAuthUser{
			{Username: "alice", Hash: aliceHash},
			{Username: "carol", Password: "secret"},
		},
		State: StateSet,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if len(plan.Commands) != 2 {
		t.Fatalf("expected remove + add, got %v", plan.Commands)
	}
	if !strings.HasSuffix(plan.Commands[0], "http-auth:remove-user test-app bob") {
		t.Errorf("removals must lead, got %v", plan.Commands)
	}
	if !strings.HasSuffix(plan.Commands[1], "http-auth:add-user test-app carol secret") {
		t.Errorf("expected add-user for the password user, got %v", plan.Commands)
	}
	for _, c := range plan.Commands {
		if strings.Contains(c, "--replace") {
			t.Errorf("--replace cannot express a set containing a cleartext password, got %v", plan.Commands)
		}
	}
}

func TestHttpAuthUserTaskSetInSync(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthUserFixture(
		"alice:"+aliceHash+"\n", "alice")))

	plan := HttpAuthUserTask{
		App:   "test-app",
		Users: []HttpAuthUser{{Username: "alice", Hash: aliceHash}},
		State: StateSet,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Errorf("expected in-sync when the set already matches, got status %v reason %q", plan.Status, plan.Reason)
	}
}

func TestHttpAuthUserTaskSetReportsSetState(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthUserFixture("", "")))

	result := HttpAuthUserTask{
		App:   "test-app",
		Users: []HttpAuthUser{{Username: "alice", Hash: aliceHash}},
		State: StateSet,
	}.Execute(ctx)
	if result.Error != nil {
		t.Fatalf("Execute: %v", result.Error)
	}
	// commands/apply.go relies on State matching DesiredState, which
	// DispatchPlan stamps as "set".
	if result.State != StateSet {
		t.Errorf("State = %q, want %q", result.State, StateSet)
	}
	if result.DesiredState != StateSet {
		t.Errorf("DesiredState = %q, want %q", result.DesiredState, StateSet)
	}
}

// TestGetHttpAuthUserHashesSkipsMalformedLines guards the export path: anything
// this parser lets through lands in a recipe, and a credential-less user would
// make `docket export` emit a body its own Validate rejects.
func TestGetHttpAuthUserHashesSkipsMalformedLines(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet http-auth:export-users test-app": "# a comment\n\nalice:" + aliceHash + "\ntruncated:\nnoseparator\nbob:$6$b:b$BB\n",
	}))

	got, err := getHttpAuthUserHashes(ctx, "test-app")
	if err != nil {
		t.Fatalf("getHttpAuthUserHashes: %v", err)
	}
	// bob's hash contains a colon, which only the first-colon split survives.
	want := map[string]string{"alice": aliceHash, "bob": "$6$b:b$BB"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("getHttpAuthUserHashes() = %v, want %v", got, want)
	}
}

func TestGetTasksHttpAuthUserTaskParsedCorrectly(t *testing.T) {
	t.Parallel()
	data := []byte(`---
- tasks:
    - name: add http auth users
      dokku_http_auth_user:
        app: test-app
        update_password: true
        users:
          - username: admin
            password: secret
          - username: ops
            password: hunter2
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("add http auth users")
	if task == nil {
		t.Fatal("task 'add http auth users' not found")
	}

	hauTask, ok := task.(*HttpAuthUserTask)
	if !ok {
		ht, ok2 := task.(HttpAuthUserTask)
		if !ok2 {
			t.Fatalf("task is not an HttpAuthUserTask (type is %T)", task)
		}
		hauTask = &ht
	}

	if hauTask.App != "test-app" {
		t.Errorf("App = %q, want %q", hauTask.App, "test-app")
	}
	// UpdatePassword is a *bool, so an explicit update_password: true survives decoding.
	if hauTask.UpdatePassword == nil || !*hauTask.UpdatePassword {
		t.Error("UpdatePassword = false, want true")
	}
	if len(hauTask.Users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(hauTask.Users))
	}
	if hauTask.Users[0].Username != "admin" || hauTask.Users[0].Password != "secret" {
		t.Errorf("Users[0] = %+v, want {admin secret}", hauTask.Users[0])
	}
	if hauTask.Users[1].Username != "ops" || hauTask.Users[1].Password != "hunter2" {
		t.Errorf("Users[1] = %+v, want {ops hunter2}", hauTask.Users[1])
	}
	if hauTask.State != StatePresent {
		t.Errorf("expected default state 'present', got %q", hauTask.State)
	}
}
