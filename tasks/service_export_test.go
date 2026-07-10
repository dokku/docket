package tasks

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestListServicesParsesTypeAndName(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		// well-formed lines, plus blank/malformed ones that must be dropped
		"--quiet plugin:trigger service-list": "redis:cache\npostgres:my-db\npostgres:analytics\n\nmalformed-line\n:bad\nbad:",
	}))()

	services, err := listServices()
	if err != nil {
		t.Fatalf("listServices: %v", err)
	}
	// sorted by (type, name); malformed lines dropped
	want := []serviceInstance{
		{Type: "postgres", Name: "analytics"},
		{Type: "postgres", Name: "my-db"},
		{Type: "redis", Name: "cache"},
	}
	if !reflect.DeepEqual(services, want) {
		t.Errorf("listServices = %+v, want %+v", services, want)
	}
}

func TestExportServiceCreateEnumeratesInstances(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet plugin:trigger service-list": "postgres:my-db\nredis:cache",
	}))()

	bodies, err := ServiceCreateTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 create tasks, got %d", len(bodies))
	}
	got := map[string]string{}
	for _, b := range bodies {
		c := b.(ServiceCreateTask)
		got[c.Name] = c.Service
		if c.State != StatePresent {
			t.Errorf("expected present state, got %q", c.State)
		}
	}
	if got["my-db"] != "postgres" || got["cache"] != "redis" {
		t.Errorf("unexpected create tasks: %+v", got)
	}
}

func TestServiceExposedPortListParsesHostSide(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		want   []string
	}{
		{"single", "5432->5432", []string{"5432"}},
		{"interface-bound", "5432->127.0.0.1:5433", []string{"127.0.0.1:5433"}},
		{"multi", "5432->5432 6379->6380", []string{"5432", "6380"}},
		{"not-exposed", "-", nil},
		{"empty", "", nil},
		{"plain-fallback", "5432", []string{"5432"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer subprocess.SetExecRunner(fakeDokku(map[string]string{
				"--quiet postgres:info svc --exposed-ports": tc.stdout,
			}))()
			got, err := serviceExposedPortList("postgres", "svc")
			if err != nil {
				t.Fatalf("serviceExposedPortList: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("serviceExposedPortList(%q) = %v, want %v", tc.stdout, got, tc.want)
			}
		})
	}
}

func TestExportServiceExposeReadsHostPorts(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet plugin:trigger service-list":         "postgres:my-db\nredis:cache",
		"--quiet postgres:info my-db --exposed-ports": "5432->5433",
		"--quiet redis:info cache --exposed-ports":    "-",
	}))()

	bodies, err := ServiceExposeTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 expose task (redis not exposed), got %d", len(bodies))
	}
	e := bodies[0].(ServiceExposeTask)
	if e.Service != "postgres" || e.Name != "my-db" {
		t.Errorf("unexpected expose target: %+v", e)
	}
	if len(e.Ports) != 1 || e.Ports[0] != "5433" {
		t.Errorf("expected host port 5433, got %v", e.Ports)
	}
}

func TestParseBackupSchedule(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		service  string
		schedule string
		bucket   string
		useIam   bool
		ok       bool
	}{
		{"standard-iam", "0 3 * * * dokku /usr/bin/dokku postgres:backup my-db my-bucket --use-iam", "postgres", "0 3 * * *", "my-bucket", true, true},
		{"standard-no-iam", "0 3 * * * dokku /usr/bin/dokku postgres:backup my-db my-bucket", "postgres", "0 3 * * *", "my-bucket", false, true},
		{"named-schedule", "@daily dokku /usr/bin/dokku redis:backup cache backups", "redis", "@daily", "backups", false, true},
		{"empty", "", "postgres", "", "", false, false},
		{"no-marker", "0 3 * * * something else entirely here", "postgres", "", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schedule, bucket, useIam, ok := parseBackupSchedule(tc.content, tc.service)
			if ok != tc.ok || schedule != tc.schedule || bucket != tc.bucket || useIam != tc.useIam {
				t.Errorf("parseBackupSchedule(%q, %q) = (%q, %q, %v, %v), want (%q, %q, %v, %v)",
					tc.content, tc.service, schedule, bucket, useIam, ok, tc.schedule, tc.bucket, tc.useIam, tc.ok)
			}
		})
	}
}

func TestExportServiceBackupParsesSchedule(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet plugin:trigger service-list":        "postgres:my-db\nredis:cache",
		"--quiet postgres:backup-schedule-cat my-db": "0 3 * * * dokku /usr/bin/dokku postgres:backup my-db my-bucket --use-iam",
		// redis has no schedule: unmapped -> empty content -> parse fails -> skipped
	}))()

	bodies, err := ServiceBackupTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 backup task, got %d", len(bodies))
	}
	b := bodies[0].(ServiceBackupTask)
	if b.Service != "postgres" || b.Name != "my-db" {
		t.Errorf("unexpected backup target: %+v", b)
	}
	if b.Schedule != "0 3 * * *" || b.Bucket != "my-bucket" || !b.UseIam {
		t.Errorf("unexpected schedule fields: %+v", b)
	}
	// Write-only secrets are never read back.
	if b.AwsSecretAccessKey != "" || b.EncryptionPassphrase != "" || b.AwsAccessKeyID != "" {
		t.Errorf("backup export should not include credentials: %+v", b)
	}
}

func TestExportServiceLinkEnumeratesForApp(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet plugin:trigger service-list": "postgres:my-db\nredis:cache",
		"--quiet postgres:links my-db":        "web\nworker",
		"--quiet redis:links cache":           "worker",
	}))()

	bodies, err := ServiceLinkTask{}.ExportApp("web")
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 link for web, got %d", len(bodies))
	}
	l := bodies[0].(ServiceLinkTask)
	if l.App != "web" || l.Service != "postgres" || l.Name != "my-db" || l.State != StatePresent {
		t.Errorf("unexpected link task: %+v", l)
	}

	bodies, err = ServiceLinkTask{}.ExportApp("worker")
	if err != nil {
		t.Fatalf("ExportApp worker: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 links for worker, got %d", len(bodies))
	}
}

func TestExportAclServiceReadsUsers(t *testing.T) {
	responses := map[string]string{
		"--quiet plugin:trigger service-list": "postgres:my-db\nredis:cache",
	}
	// acl:list-service emits one username per line on STDERR, not stdout.
	stderr := map[string]string{
		"--quiet acl:list-service postgres my-db": "bob\nalice",
		"--quiet acl:list-service redis cache":    "",
	}
	defer subprocess.SetExecRunner(func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		key := strings.Join(in.Args, " ")
		return subprocess.ExecCommandResponse{Stdout: responses[key], Stderr: stderr[key]}, nil
	})()

	bodies, err := AclServiceTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 acl task (redis has none), got %d", len(bodies))
	}
	a := bodies[0].(AclServiceTask)
	// Field inversion: Service holds the instance name, Type the datastore type.
	if a.Service != "my-db" || a.Type != "postgres" {
		t.Errorf("acl field inversion wrong: %+v", a)
	}
	// sortedSetKeys yields deterministic, sorted membership.
	if !reflect.DeepEqual(a.Users, []string{"alice", "bob"}) {
		t.Errorf("acl users = %v, want [alice bob]", a.Users)
	}
}

func TestExportConfigExcludesLinkedServiceDSNs(t *testing.T) {
	dsn := "postgres://postgres:pw@dokku-postgres-my-db:5432/my_db"
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet apps:list":                       "web",
		"--quiet config:export --format json web": `{"DATABASE_URL":"` + dsn + `","SECRET_KEY":"s3cr3t"}`,
		"--quiet plugin:trigger service-list":     "postgres:my-db",
		"--quiet postgres:links my-db":            "web",
		"--quiet postgres:info my-db --dsn":       dsn,
	}))()

	bodies, err := ConfigTask{}.ExportApp("web")
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 config task, got %d", len(bodies))
	}
	c := bodies[0].(ConfigTask)
	if _, ok := c.Config["DATABASE_URL"]; ok {
		t.Errorf("linked-service DSN should be excluded from config export: %+v", c.Config)
	}
	if c.Config["SECRET_KEY"] != "s3cr3t" {
		t.Errorf("non-link config should be kept: %+v", c.Config)
	}
}

func TestExportRecipeIncludesServiceTasks(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet apps:list":                           "web",
		"--quiet config:export --format json web":     `{}`,
		"domains:report web --domains-app-vhosts":     "",
		"--quiet plugin:trigger service-list":         "postgres:my-db",
		"--quiet postgres:info my-db --exposed-ports": "5432->5432",
		"--quiet postgres:links my-db":                "web",
		"--quiet postgres:info my-db --dsn":           "postgres://postgres:pw@dokku-postgres-my-db:5432/my_db",
	}))()

	res, err := ExportRecipe(ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	recipe, err := res.MarshalRecipe("yaml")
	if err != nil {
		t.Fatalf("MarshalRecipe: %v", err)
	}
	out := string(recipe)
	for _, want := range []string{
		"name: global",
		"dokku_service_create",
		"dokku_service_expose",
		"dokku_service_link",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
}
