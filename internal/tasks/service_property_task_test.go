package tasks

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

func TestServicePropertyTaskInvalidState(t *testing.T) {
	task := ServicePropertyTask{Service: "redis", Name: "test-service", Property: "restart-policy", Value: "always", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestServicePropertyTaskPresentRequiresValue(t *testing.T) {
	task := ServicePropertyTask{Service: "redis", Name: "test-service", Property: "restart-policy"}
	result := task.Plan(testCtx())
	if result.Error == nil {
		t.Fatal("Plan with present state and no value should return an error")
	}
}

func TestServicePropertyTaskAbsentRejectsValue(t *testing.T) {
	task := ServicePropertyTask{Service: "redis", Name: "test-service", Property: "restart-policy", Value: "always", State: StateAbsent}
	result := task.Plan(testCtx())
	if result.Error == nil {
		t.Fatal("Plan with absent state and a value should return an error")
	}
}

func TestServicePropertyTaskRequiresProperty(t *testing.T) {
	task := ServicePropertyTask{Service: "redis", Name: "test-service", Value: "always"}
	result := task.Plan(testCtx())
	if result.Error == nil {
		t.Fatal("Plan with no property should return an error")
	}
}

func TestGetTasksServicePropertyTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: set postgres restart policy
      dokku_service_property:
        service: postgres
        name: my-db
        property: restart-policy
        value: always
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("set postgres restart policy")
	if task == nil {
		t.Fatal("task 'set postgres restart policy' not found")
	}

	spTask, ok := task.(*ServicePropertyTask)
	if !ok {
		st, ok2 := task.(ServicePropertyTask)
		if !ok2 {
			t.Fatalf("task is not a ServicePropertyTask (type is %T)", task)
		}
		spTask = &st
	}

	if spTask.Service != "postgres" {
		t.Errorf("Service = %q, want %q", spTask.Service, "postgres")
	}
	if spTask.Name != "my-db" {
		t.Errorf("Name = %q, want %q", spTask.Name, "my-db")
	}
	if spTask.Property != "restart-policy" {
		t.Errorf("Property = %q, want %q", spTask.Property, "restart-policy")
	}
	if spTask.Value != "always" {
		t.Errorf("Value = %q, want %q", spTask.Value, "always")
	}
	if spTask.State != StatePresent {
		t.Errorf("expected default state 'present', got %q", spTask.State)
	}
}

// planServiceProperty plans a property task against a redis service named cache
// whose info reports the given payload.
func planServiceProperty(t *testing.T, task ServicePropertyTask, info map[string]string) PlanResult {
	t.Helper()
	task.Service, task.Name = "redis", "cache"
	if task.State == "" {
		task.State = StatePresent
	}
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(infoFixture("redis", "cache", info)))
	ctx = subprocess.ContextWithMasker(ctx, subprocess.NewMasker(task.SensitiveValues()...))
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	return plan
}

func TestServicePropertyTaskPlan(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		task    ServicePropertyTask
		info    map[string]string
		inSync  bool
		status  PlanStatus
		reason  string
		command string
	}{
		{
			name:   "set and matching",
			task:   ServicePropertyTask{Property: "restart-policy", Value: "always"},
			info:   map[string]string{"restart-policy": "always"},
			inSync: true,
			status: PlanStatusOK,
		},
		{
			name:    "set to another value",
			task:    ServicePropertyTask{Property: "restart-policy", Value: "always"},
			info:    map[string]string{"restart-policy": "unless-stopped"},
			status:  PlanStatusModify,
			reason:  `restart-policy drift on redis service cache (was "unless-stopped")`,
			command: "dokku --quiet redis:set cache restart-policy always",
		},
		{
			name:    "unset",
			task:    ServicePropertyTask{Property: "restart-policy", Value: "always"},
			info:    map[string]string{"restart-policy": ""},
			status:  PlanStatusCreate,
			reason:  "restart-policy missing on redis service cache",
			command: "dokku --quiet redis:set cache restart-policy always",
		},
		{
			name:   "absent and unset",
			task:   ServicePropertyTask{Property: "restart-policy", State: StateAbsent},
			info:   map[string]string{"restart-policy": ""},
			inSync: true,
			status: PlanStatusOK,
		},
		{
			name:    "absent and set",
			task:    ServicePropertyTask{Property: "restart-policy", State: StateAbsent},
			info:    map[string]string{"restart-policy": "always"},
			status:  PlanStatusDestroy,
			reason:  `would unset restart-policy on redis service cache (was "always")`,
			command: "dokku --quiet redis:set cache restart-policy",
		},
		{
			name:   "custom-env in another order",
			task:   ServicePropertyTask{Property: "custom-env", Value: "A=one;B=two"},
			info:   map[string]string{"custom-env": "B=two;A=one"},
			inSync: true,
			status: PlanStatusOK,
		},
		{
			name:   "custom-env with another value",
			task:   ServicePropertyTask{Property: "custom-env", Value: "A=one;B=two"},
			info:   map[string]string{"custom-env": "A=one;B=three"},
			status: PlanStatusModify,
			reason: "custom-env on redis service cache differs for B",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := planServiceProperty(t, tc.task, tc.info)
			if plan.InSync != tc.inSync || plan.Status != tc.status {
				t.Fatalf("InSync=%v status=%q, want InSync=%v status=%q", plan.InSync, plan.Status, tc.inSync, tc.status)
			}
			if tc.reason != "" && plan.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", plan.Reason, tc.reason)
			}
			if tc.command != "" && !slices.Equal(plan.Commands, []string{tc.command}) {
				t.Errorf("commands = %v, want [%q]", plan.Commands, tc.command)
			}
			if len(plan.Warnings) != 0 {
				t.Errorf("expected no warnings, got %v", plan.Warnings)
			}
		})
	}
}

// TestServicePropertyTaskNormalizesDefaults covers the keys info reports with
// a default standing in for an unset value: declaring the default is in sync
// with an unset key, and clearing the key is in sync while it reads the
// default.
func TestServicePropertyTaskNormalizesDefaults(t *testing.T) {
	t.Parallel()
	for property, unset := range map[string]string{
		"expose-mode":      "ambassador",
		"backup-timestamp": "true",
		"backup-use-iam":   "false",
		"memory":           "0",
	} {
		t.Run(property, func(t *testing.T) {
			for _, reported := range []string{"", unset} {
				present := planServiceProperty(t, ServicePropertyTask{Property: property, Value: unset}, map[string]string{property: reported})
				if !present.InSync {
					t.Errorf("present %q with info %q: expected in sync, got %+v", unset, reported, present)
				}
				absent := planServiceProperty(t, ServicePropertyTask{Property: property, State: StateAbsent}, map[string]string{property: reported})
				if !absent.InSync {
					t.Errorf("absent with info %q: expected in sync, got %+v", reported, absent)
				}
			}
		})
	}

	plan := planServiceProperty(t, ServicePropertyTask{Property: "expose-mode", Value: "direct"}, map[string]string{"expose-mode": "ambassador"})
	if plan.InSync || plan.Status != PlanStatusCreate {
		t.Errorf("expose-mode direct over the default: expected a create, got InSync=%v status=%q", plan.InSync, plan.Status)
	}
	plan = planServiceProperty(t, ServicePropertyTask{Property: "expose-mode", State: StateAbsent}, map[string]string{"expose-mode": "direct"})
	if plan.InSync || plan.Status != PlanStatusDestroy {
		t.Errorf("clearing expose-mode direct: expected a destroy, got InSync=%v status=%q", plan.InSync, plan.Status)
	}
}

// TestServicePropertyTaskComparesCredentialsByFingerprint walks each backup
// secret through its fingerprint: the stored value is never read back, so the
// declared value is compared by digest, and neither one is printed.
func TestServicePropertyTaskComparesCredentialsByFingerprint(t *testing.T) {
	t.Parallel()
	const secret = "s3cr3t-value"
	for property, key := range map[string]string{
		"backup-access-key-id":         "backup-access-key-id-fingerprint",
		"backup-secret-access-key":     "backup-secret-access-key-fingerprint",
		"backup-encryption-passphrase": "backup-encryption-fingerprint",
	} {
		t.Run(property, func(t *testing.T) {
			matching := planServiceProperty(t, ServicePropertyTask{Property: property, Value: secret}, map[string]string{key: backupFingerprint(secret)})
			if !matching.InSync {
				t.Errorf("matching fingerprint: expected in sync, got %+v", matching)
			}

			stored := backupFingerprint("another-value")
			differing := planServiceProperty(t, ServicePropertyTask{Property: property, Value: secret}, map[string]string{key: stored})
			if differing.InSync || differing.Status != PlanStatusModify {
				t.Fatalf("differing fingerprint: expected a modify, got InSync=%v status=%q", differing.InSync, differing.Status)
			}
			if want := property + " on redis service cache differs from the stored value"; differing.Reason != want {
				t.Errorf("reason = %q, want %q", differing.Reason, want)
			}
			if want := "redis:set cache " + property; !slices.Equal(differing.Mutations, []string{want}) {
				t.Errorf("mutations = %v, want [%q]", differing.Mutations, want)
			}
			for _, text := range append(append([]string{differing.Reason}, differing.Mutations...), differing.Commands...) {
				if strings.Contains(text, secret) || strings.Contains(text, stored) || strings.Contains(text, backupFingerprint(secret)) {
					t.Errorf("a secret or a fingerprint leaked into %q", text)
				}
			}

			missing := planServiceProperty(t, ServicePropertyTask{Property: property, Value: secret}, map[string]string{key: ""})
			if missing.InSync || missing.Status != PlanStatusCreate {
				t.Errorf("nothing stored: expected a create, got InSync=%v status=%q", missing.InSync, missing.Status)
			}

			absent := planServiceProperty(t, ServicePropertyTask{Property: property, State: StateAbsent}, map[string]string{key: ""})
			if !absent.InSync {
				t.Errorf("absent with an empty fingerprint: expected in sync, got %+v", absent)
			}
			clearing := planServiceProperty(t, ServicePropertyTask{Property: property, State: StateAbsent}, map[string]string{key: stored})
			if clearing.InSync || clearing.Status != PlanStatusDestroy {
				t.Errorf("absent with a stored value: expected a destroy, got InSync=%v status=%q", clearing.InSync, clearing.Status)
			}
			if want := "would unset " + property + " on redis service cache"; clearing.Reason != want {
				t.Errorf("reason = %q, want %q", clearing.Reason, want)
			}
		})
	}
}

// TestServicePropertyTaskWarnsOnAKeyInfoDoesNotReport pins the property-task
// convention for a key the payload does not carry: say so, and set it anyway,
// leaving `<service>:set` to reject a typo itself.
func TestServicePropertyTaskWarnsOnAKeyInfoDoesNotReport(t *testing.T) {
	t.Parallel()
	for _, state := range []State{StatePresent, StateAbsent} {
		task := ServicePropertyTask{Property: "made-up-key", State: state}
		if state == StatePresent {
			task.Value = "1"
		}
		plan := planServiceProperty(t, task, map[string]string{"restart-policy": ""})
		if plan.InSync || len(plan.Commands) != 1 {
			t.Errorf("%s: expected the set to be planned, got InSync=%v commands=%v", state, plan.InSync, plan.Commands)
		}
		if len(plan.Warnings) != 1 || plan.Warnings[0].Reason != WarnReasonUnknownProperty {
			t.Errorf("%s: expected one %s warning, got %v", state, WarnReasonUnknownProperty, plan.Warnings)
		}
	}
}

func TestServicePropertyTaskMissingServiceIsAnError(t *testing.T) {
	t.Parallel()
	task := ServicePropertyTask{Service: "redis", Name: "cache", Property: "restart-policy", Value: "always", State: StatePresent}
	plan := task.Plan(subprocess.ContextWithRunner(testCtx(), serviceMissing()))
	if plan.Error == nil || !strings.Contains(plan.Error.Error(), "does not exist") {
		t.Errorf("expected a missing-service error, got %v", plan.Error)
	}
	if !errors.Is(plan.Error, ErrObjectMissing) {
		t.Errorf("expected an error matching ErrObjectMissing, got %v", plan.Error)
	}
}

func TestServicePropertyTaskSensitiveValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		property string
		value    string
		want     []string
	}{
		{"backup-access-key-id", "AKIAEXAMPLE", []string{"AKIAEXAMPLE"}},
		{"backup-secret-access-key", "topsecret", []string{"topsecret"}},
		{"backup-encryption-passphrase", "hunter2", []string{"hunter2"}},
		{"custom-env", "TOKEN=abc;MODE=fast", []string{"TOKEN=abc;MODE=fast", "abc", "fast"}},
		{"restart-policy", "always", nil},
	}
	for _, tc := range cases {
		got := ServicePropertyTask{Property: tc.property, Value: tc.value}.SensitiveValues()
		slices.Sort(got)
		want := slices.Clone(tc.want)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: SensitiveValues() = %v, want %v", tc.property, got, want)
		}
	}
}

// TestServicePropertyTaskMasksReadBackCustomEnv pins that the custom-env
// values the server holds are registered with the masker before anything can
// echo them.
func TestServicePropertyTaskMasksReadBackCustomEnv(t *testing.T) {
	t.Parallel()
	task := ServicePropertyTask{Service: "redis", Name: "cache", Property: "custom-env", Value: "TOKEN=recipe", State: StatePresent}
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(infoFixture("redis", "cache", map[string]string{"custom-env": "TOKEN=on-the-server"})))
	ctx = subprocess.ContextWithMasker(ctx, subprocess.NewMasker())
	plan := task.Plan(ctx)
	if strings.Contains(plan.Reason, "on-the-server") {
		t.Errorf("the read-back value leaked into the reason: %q", plan.Reason)
	}
	if got := subprocess.MaskerFromContext(ctx).String("on-the-server"); got == "on-the-server" {
		t.Error("the read-back custom-env value was not registered with the masker")
	}
}

// TestBackupFingerprintMatchesUpstream pins backupFingerprint to the digest
// dokku-datastore documents: `printf '%s' "$value" | sha256sum` for one value.
func TestBackupFingerprintMatchesUpstream(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"abc": "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
	}
	for value, want := range cases {
		if got := backupFingerprint(value); got != want {
			t.Errorf("backupFingerprint(%q) = %q, want %q", value, got, want)
		}
	}
	// Several values are joined by newlines with no trailing one.
	if got, want := backupFingerprint("a", "b"), backupFingerprint("a\nb"); got != want {
		t.Errorf("backupFingerprint(a, b) = %q, want the digest of \"a\\nb\" %q", got, want)
	}
}
