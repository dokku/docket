package tasks

import (
	"reflect"
	"strings"
	"testing"
)

func TestSchedulerK3sProfileTaskMissingName(t *testing.T) {
	task := SchedulerK3sProfileTask{Role: "worker", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when name is empty")
	}
	if !strings.Contains(result.Error.Error(), "name is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sProfileTaskMissingRole(t *testing.T) {
	task := SchedulerK3sProfileTask{Name: "edge-pool", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when role is empty")
	}
	if !strings.Contains(result.Error.Error(), "role is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sProfileTaskInvalidRole(t *testing.T) {
	task := SchedulerK3sProfileTask{Name: "edge-pool", Role: "controlplane", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when role is invalid")
	}
	if !strings.Contains(result.Error.Error(), "role must be 'server' or 'worker'") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sProfileTaskInvalidKubeletArg(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "empty entry", args: []string{""}, want: "must not be empty"},
		{name: "missing equals", args: []string{"max-pods"}, want: "key=value form"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := SchedulerK3sProfileTask{
				Name:        "edge-pool",
				Role:        "worker",
				KubeletArgs: tc.args,
				State:       StatePresent,
			}
			result := task.Execute()
			if result.Error == nil {
				t.Fatalf("expected error for kubelet_args=%v", tc.args)
			}
			if !strings.Contains(result.Error.Error(), tc.want) {
				t.Errorf("unexpected error: %v", result.Error)
			}
		})
	}
}

func TestSchedulerK3sProfileTaskInvalidState(t *testing.T) {
	task := SchedulerK3sProfileTask{Name: "edge-pool", Role: "worker", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when state is invalid")
	}
	if !strings.Contains(result.Error.Error(), "invalid state") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sProfileSetCommandRoleOnly(t *testing.T) {
	task := SchedulerK3sProfileTask{Name: "edge-pool", Role: "worker"}
	got := schedulerK3sProfileSetCommand(task)
	want := []string{"--quiet", "scheduler-k3s:profiles:add", "edge-pool", "--role", "worker"}
	if got.Command != "dokku" {
		t.Errorf("expected command 'dokku', got %q", got.Command)
	}
	if !reflect.DeepEqual(got.Args, want) {
		t.Errorf("args mismatch\n got: %v\nwant: %v", got.Args, want)
	}
}

func TestSchedulerK3sProfileSetCommandAllFlags(t *testing.T) {
	task := SchedulerK3sProfileTask{
		Name:              "edge-pool",
		Role:              "server",
		KubeletArgs:       []string{"max-pods=64", "eviction-hard=memory.available<5%"},
		TaintScheduling:   true,
		AllowUnknownHosts: true,
	}
	got := schedulerK3sProfileSetCommand(task)
	want := []string{
		"--quiet", "scheduler-k3s:profiles:add", "edge-pool",
		"--role", "server",
		"--taint-scheduling",
		"--insecure-allow-unknown-hosts",
		"--kubelet-args", "max-pods=64",
		"--kubelet-args", "eviction-hard=memory.available<5%",
	}
	if !reflect.DeepEqual(got.Args, want) {
		t.Errorf("args mismatch\n got: %v\nwant: %v", got.Args, want)
	}
}

// TestSchedulerK3sProfileSetCommandAlwaysHasRole guards the full-replace
// invariant: omitting --role from `profiles:add` lets dokku default the
// role back to "worker", which would silently flip an existing "server"
// profile on every re-apply.
func TestSchedulerK3sProfileSetCommandAlwaysHasRole(t *testing.T) {
	task := SchedulerK3sProfileTask{Name: "edge-pool", Role: "server", TaintScheduling: true}
	got := schedulerK3sProfileSetCommand(task)
	roleIdx := -1
	for i, a := range got.Args {
		if a == "--role" {
			roleIdx = i
			break
		}
	}
	if roleIdx == -1 || roleIdx+1 >= len(got.Args) {
		t.Fatalf("expected --role <value> in args, got %v", got.Args)
	}
	if got.Args[roleIdx+1] != "server" {
		t.Errorf("expected --role server, got --role %q", got.Args[roleIdx+1])
	}
}

func TestSchedulerK3sProfileMatchesIgnoresKubeletArgOrder(t *testing.T) {
	current := schedulerK3sProfileEntry{
		Name:        "edge-pool",
		Role:        "worker",
		KubeletArgs: []string{"b=2", "a=1"},
	}
	desired := SchedulerK3sProfileTask{
		Name:        "edge-pool",
		Role:        "worker",
		KubeletArgs: []string{"a=1", "b=2"},
	}
	if !profileMatches(current, desired) {
		t.Error("expected reordered kubelet_args to match")
	}
}

func TestSchedulerK3sProfileMatchesDetectsDrift(t *testing.T) {
	current := schedulerK3sProfileEntry{
		Name:        "edge-pool",
		Role:        "worker",
		KubeletArgs: []string{"a=1"},
	}
	cases := []struct {
		name    string
		desired SchedulerK3sProfileTask
	}{
		{
			name: "extra kubelet arg",
			desired: SchedulerK3sProfileTask{
				Name: "edge-pool", Role: "worker",
				KubeletArgs: []string{"a=1", "b=2"},
			},
		},
		{
			name: "different role",
			desired: SchedulerK3sProfileTask{
				Name: "edge-pool", Role: "server",
				KubeletArgs: []string{"a=1"},
			},
		},
		{
			name: "taint_scheduling differs",
			desired: SchedulerK3sProfileTask{
				Name: "edge-pool", Role: "worker",
				KubeletArgs:     []string{"a=1"},
				TaintScheduling: true,
			},
		},
		{
			name: "allow_unknown_hosts differs",
			desired: SchedulerK3sProfileTask{
				Name: "edge-pool", Role: "worker",
				KubeletArgs:       []string{"a=1"},
				AllowUnknownHosts: true,
			},
		},
		{
			name: "different kubelet arg value with same length",
			desired: SchedulerK3sProfileTask{
				Name: "edge-pool", Role: "worker",
				KubeletArgs: []string{"a=2"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if profileMatches(current, tc.desired) {
				t.Errorf("expected drift, got match (desired=%+v)", tc.desired)
			}
		})
	}
}

func TestParseSchedulerK3sProfileEmpty(t *testing.T) {
	entry, found, err := parseSchedulerK3sProfile([]byte("[]"), "edge-pool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Errorf("expected found=false for empty list, got entry=%+v", entry)
	}
}

func TestParseSchedulerK3sProfileNilInput(t *testing.T) {
	entry, found, err := parseSchedulerK3sProfile(nil, "edge-pool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Errorf("expected found=false for nil input, got entry=%+v", entry)
	}
}

func TestParseSchedulerK3sProfileSingleMatch(t *testing.T) {
	raw := []byte(`[{"name":"edge-pool","role":"worker","kubelet_args":["max-pods=64"]}]`)
	entry, found, err := parseSchedulerK3sProfile(raw, "edge-pool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if entry.Role != "worker" {
		t.Errorf("role mismatch: got %q", entry.Role)
	}
	if !reflect.DeepEqual(entry.KubeletArgs, []string{"max-pods=64"}) {
		t.Errorf("kubelet_args mismatch: got %v", entry.KubeletArgs)
	}
	if entry.TaintScheduling || entry.AllowUnknownHosts {
		t.Errorf("expected booleans false when omitted from JSON, got taint=%v allow=%v",
			entry.TaintScheduling, entry.AllowUnknownHosts)
	}
}

func TestParseSchedulerK3sProfileMatchInMiddle(t *testing.T) {
	raw := []byte(`[
		{"name":"first","role":"worker"},
		{"name":"edge-pool","role":"server","taint_scheduling":true,"allow_unknown_hosts":true},
		{"name":"last","role":"worker","kubelet_args":["x=1"]}
	]`)
	entry, found, err := parseSchedulerK3sProfile(raw, "edge-pool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if entry.Role != "server" || !entry.TaintScheduling || !entry.AllowUnknownHosts {
		t.Errorf("entry mismatch: %+v", entry)
	}
}

func TestParseSchedulerK3sProfileNoMatch(t *testing.T) {
	raw := []byte(`[{"name":"other","role":"worker"}]`)
	_, found, err := parseSchedulerK3sProfile(raw, "edge-pool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected found=false")
	}
}

func TestParseSchedulerK3sProfileMalformed(t *testing.T) {
	_, _, err := parseSchedulerK3sProfile([]byte("not json"), "edge-pool")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse scheduler-k3s:profiles:list json") {
		t.Errorf("unexpected error: %v", err)
	}
}
