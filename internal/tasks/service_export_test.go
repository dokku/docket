package tasks

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

func TestListServicesParsesTypeAndName(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		// well-formed lines, plus blank/malformed ones that must be dropped
		"--quiet plugin:trigger service-list": "redis:cache\npostgres:my-db\npostgres:analytics\n\nmalformed-line\n:bad\nbad:",
	}))

	services, err := listServices(ctx)
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

// mergeFixtures folds fakeDokku response maps into one.
func mergeFixtures(fixtures ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, f := range fixtures {
		for k, v := range f {
			out[k] = v
		}
	}
	return out
}

func TestExportServiceCreateEnumeratesInstances(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(mergeFixtures(
		map[string]string{"--quiet plugin:trigger service-list": "postgres:my-db\nredis:cache"},
		infoFixture("postgres", "my-db", map[string]string{"version": "postgis/postgis:13-master"}),
		infoFixture("redis", "cache", map[string]string{"version": "redis:7.2.5"}),
	)))

	bodies, err := ServiceCreateTask{}.ExportGlobal(ctx)
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 create tasks, got %d", len(bodies))
	}
	got := map[string]ServiceCreateTask{}
	for _, b := range bodies {
		c := b.(ServiceCreateTask)
		got[c.Name] = c
		if c.State != StatePresent {
			t.Errorf("expected present state, got %q", c.State)
		}
	}
	if got["my-db"].Service != "postgres" || got["cache"].Service != "redis" {
		t.Errorf("unexpected create tasks: %+v", got)
	}
	// The image a service is actually running is pinned, so recreating it on
	// another server does not silently pick up that server's plugin default.
	if got["my-db"].Image != "postgis/postgis" || got["my-db"].ImageVersion != "13-master" {
		t.Errorf("postgres image = %q:%q, want postgis/postgis:13-master", got["my-db"].Image, got["my-db"].ImageVersion)
	}
	if got["cache"].Image != "redis" || got["cache"].ImageVersion != "7.2.5" {
		t.Errorf("redis image = %q:%q, want redis:7.2.5", got["cache"].Image, got["cache"].ImageVersion)
	}
}

// TestExportServiceCreateOmitsUnreadableImage covers a service whose container
// is gone: info reports an empty version, and the export must leave both
// image fields empty rather than emitting a half-formed pin.
func TestExportServiceCreateOmitsUnreadableImage(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(mergeFixtures(
		map[string]string{"--quiet plugin:trigger service-list": "redis:cache"},
		infoFixture("redis", "cache", map[string]string{"version": ""}),
	)))

	bodies, err := ServiceCreateTask{}.ExportGlobal(ctx)
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 create task, got %d", len(bodies))
	}
	c := bodies[0].(ServiceCreateTask)
	if c.Image != "" || c.ImageVersion != "" {
		t.Errorf("expected no image, got %q:%q", c.Image, c.ImageVersion)
	}
}

// TestExportServiceCreateReadsCreateTimeOptions pins that the options info
// reports come back as the fields that would recreate them, with an unset
// memory limit left out rather than exported as 0.
func TestExportServiceCreateReadsCreateTimeOptions(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(mergeFixtures(
		map[string]string{"--quiet plugin:trigger service-list": "postgres:my-db\nredis:cache"},
		infoFixture("postgres", "my-db", map[string]string{
			"version":             "postgres:17.2",
			"config-options":      "--max-connections 200",
			"custom-env":          "POSTGRES_INITDB_ARGS=--data-checksums;TZ=UTC",
			"memory":              "512",
			"shm-size":            "128m",
			"initial-network":     "net-a",
			"post-create-network": "net-b,net-c",
			"post-start-network":  "net-d",
		}),
		infoFixture("redis", "cache", map[string]string{"version": "redis:7.2.5", "memory": "0"}),
	)))

	bodies, err := ServiceCreateTask{}.ExportGlobal(ctx)
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	got := map[string]ServiceCreateTask{}
	for _, b := range bodies {
		c := b.(ServiceCreateTask)
		got[c.Name] = c
	}
	want := ServiceCreateTask{
		Service:           "postgres",
		Name:              "my-db",
		Image:             "postgres",
		ImageVersion:      "17.2",
		ConfigOptions:     "--max-connections 200",
		CustomEnv:         map[string]string{"POSTGRES_INITDB_ARGS": "--data-checksums", "TZ": "UTC"},
		Memory:            512,
		ShmSize:           "128m",
		InitialNetwork:    "net-a",
		PostCreateNetwork: []string{"net-b", "net-c"},
		PostStartNetwork:  []string{"net-d"},
		State:             StatePresent,
	}
	if !reflect.DeepEqual(got["my-db"], want) {
		t.Errorf("postgres create = %#v, want %#v", got["my-db"], want)
	}
	if c := got["cache"]; c.Memory != 0 || c.ConfigOptions != "" || len(c.CustomEnv) != 0 || c.PostCreateNetwork != nil {
		t.Errorf("redis create should carry no options, got %#v", c)
	}
}

func TestSplitImageRef(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ref         string
		wantImage   string
		wantVersion string
	}{
		{"postgres:17.2", "postgres", "17.2"},
		{"postgis/postgis:13-master", "postgis/postgis", "13-master"},
		// The colon here is a registry port, not a tag.
		{"registry.example.com:5000/postgres", "registry.example.com:5000/postgres", ""},
		{"registry.example.com:5000/postgres:17.2", "registry.example.com:5000/postgres", "17.2"},
		{"postgres", "postgres", ""},
		{"", "", ""},
		// A digest reference has no tag to find, and the colon the splitter
		// lands on belongs to the digest. Neither half is usable, which is why
		// the image-drift upgrade path refuses to build a reference out of one
		// (see TestServiceCreateTaskImageDriftUpgradeRefusesADigestRef) rather
		// than trusting this result.
		{"postgres@sha256:abc123", "postgres@sha256", "abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.ref, func(t *testing.T) {
			image, version := splitImageRef(tc.ref)
			if image != tc.wantImage || version != tc.wantVersion {
				t.Errorf("splitImageRef(%q) = %q, %q; want %q, %q", tc.ref, image, version, tc.wantImage, tc.wantVersion)
			}
		})
	}
}

func TestServiceExposedPortListParsesHostSide(t *testing.T) {
	t.Parallel()
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
			ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
				"--quiet postgres:info svc --exposed-ports": tc.stdout,
			}))
			got, err := serviceExposedPortList(ctx, "postgres", "svc")
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
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet plugin:trigger service-list":         "postgres:my-db\nredis:cache",
		"--quiet postgres:info my-db --exposed-ports": "5432->5433",
		"--quiet redis:info cache --exposed-ports":    "-",
	}))

	bodies, err := ServiceExposeTask{}.ExportGlobal(ctx)
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

func TestExportServiceBackupReadsSchedule(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(mergeFixtures(
		map[string]string{"--quiet plugin:trigger service-list": "postgres:my-db\nredis:cache"},
		infoFixture("postgres", "my-db", map[string]string{
			"backup-schedule":                      "0 3 * * *",
			"backup-bucket":                        "my-bucket",
			"backup-use-iam":                       "true",
			"backup-access-key-id-fingerprint":     backupFingerprint("AKIAEXAMPLE"),
			"backup-secret-access-key-fingerprint": backupFingerprint("secret"),
			"backup-default-region":                "us-east-1",
			"backup-encryption-fingerprint":        backupFingerprint("passphrase"),
		}),
		// redis has no schedule, so it is skipped.
		infoFixture("redis", "cache", map[string]string{"backup-schedule": "", "backup-bucket": "", "backup-use-iam": "false"}),
	)))

	bodies, err := ServiceBackupTask{}.ExportGlobal(ctx)
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 backup task, got %d", len(bodies))
	}
	want := ServiceBackupTask{
		Service:  "postgres",
		Name:     "my-db",
		Schedule: "0 3 * * *",
		Bucket:   "my-bucket",
		UseIam:   true,
		State:    StatePresent,
	}
	// The secrets are reported only as fingerprints, and the region cannot be
	// declared without them, so none of it is exported.
	if got := bodies[0].(ServiceBackupTask); !reflect.DeepEqual(got, want) {
		t.Errorf("backup task = %#v, want %#v", got, want)
	}
}

func TestExportServicePropertyReadsSetProperties(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(mergeFixtures(
		map[string]string{"--quiet plugin:trigger service-list": "postgres:my-db"},
		infoFixture("postgres", "my-db", map[string]string{
			"restart-policy":   "always",
			"expose-mode":      "direct",
			"backup-timestamp": "false",
			"backup-mailto":    "ops@example.com",
			"volume-targets":   "/var/lib/postgresql/data",
			// Defaults stand in for an unset value and are not exported.
			"wait-timeout": "",
			"log-driver":   "",
			// Keys other tasks own are never exported here.
			"shm-size":                         "128m",
			"memory":                           "512",
			"backup-bucket":                    "my-bucket",
			"backup-schedule":                  "0 3 * * *",
			"backup-access-key-id-fingerprint": backupFingerprint("AKIAEXAMPLE"),
		}),
	)))

	bodies, err := ServicePropertyTask{}.ExportGlobal(ctx)
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	got := map[string]string{}
	for _, b := range bodies {
		p := b.(ServicePropertyTask)
		if p.Service != "postgres" || p.Name != "my-db" || p.State != StatePresent {
			t.Errorf("unexpected property task: %#v", p)
		}
		got[p.Property] = p.Value
	}
	want := map[string]string{
		"backup-timestamp": "false",
		"backup-mailto":    "ops@example.com",
		"restart-policy":   "always",
		"expose-mode":      "direct",
		"volume-targets":   "/var/lib/postgresql/data",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("exported properties = %v, want %v", got, want)
	}
}

func TestExportServicePropertySkipsDefaults(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(mergeFixtures(
		map[string]string{"--quiet plugin:trigger service-list": "redis:cache"},
		infoFixture("redis", "cache", map[string]string{
			"expose-mode":      "ambassador",
			"backup-timestamp": "true",
		}),
	)))
	bodies, err := ServicePropertyTask{}.ExportGlobal(ctx)
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	if len(bodies) != 0 {
		t.Errorf("expected no property tasks for default values, got %v", bodies)
	}
}

// noBackupInfo is shaped like the info payload of a datastore that cannot be
// backed up and takes no extra arguments or moved volumes: info still reports
// every key, empty or at its default.
func noBackupInfo() map[string]string {
	return map[string]string{
		"version":                              "example/nobackup:1.0",
		"backup-schedule":                      "",
		"backup-bucket":                        "",
		"backup-use-iam":                       "false",
		"backup-timestamp":                     "true",
		"backup-access-key-id-fingerprint":     "",
		"backup-secret-access-key-fingerprint": "",
		"backup-default-region":                "",
		"backup-signature-version":             "",
		"backup-endpoint-url":                  "",
		"backup-encryption-fingerprint":        "",
		"backup-public-key-id":                 "",
		"backup-keyserver":                     "",
		"backup-mailto":                        "",
		"backup-storage-class":                 "",
		"backup-object-name":                   "",
		"export-args":                          "",
		"import-args":                          "",
		"volume-targets":                       "",
		"expose-mode":                          "",
	}
}

// TestDatastoreWithoutBackupsConverges pins that a datastore refusing the
// backup settings needs no special case in docket: info reports nothing
// configured, so the exports emit nothing and the absent states are in sync.
func TestDatastoreWithoutBackupsConverges(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(mergeFixtures(
		map[string]string{"--quiet plugin:trigger service-list": "nobackup:svc"},
		infoFixture("nobackup", "svc", noBackupInfo()),
	)))

	if bodies, err := (ServiceBackupTask{}).ExportGlobal(ctx); err != nil || len(bodies) != 0 {
		t.Errorf("backup export = %v, %v; want nothing", bodies, err)
	}
	if bodies, err := (ServicePropertyTask{}).ExportGlobal(ctx); err != nil || len(bodies) != 0 {
		t.Errorf("property export = %v, %v; want nothing", bodies, err)
	}

	backup := ServiceBackupTask{Service: "nobackup", Name: "svc", State: StateAbsent}
	if plan := backup.Plan(ctx); plan.Error != nil || !plan.InSync {
		t.Errorf("backup absent: InSync=%v err=%v, want in sync", plan.InSync, plan.Error)
	}
	for _, property := range []string{"backup-bucket", "backup-use-iam", "backup-timestamp", "export-args", "volume-targets"} {
		task := ServicePropertyTask{Service: "nobackup", Name: "svc", Property: property, State: StateAbsent}
		if plan := task.Plan(ctx); plan.Error != nil || !plan.InSync {
			t.Errorf("property %s absent: InSync=%v err=%v, want in sync", property, plan.InSync, plan.Error)
		}
	}
}

func TestExportServiceLinkEnumeratesForApp(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet plugin:trigger service-list": "postgres:my-db\nredis:cache",
		"--quiet postgres:links my-db":        "web\nworker",
		"--quiet redis:links cache":           "worker",
	}))

	bodies, err := ServiceLinkTask{}.ExportApp(ctx, "web")
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

	bodies, err = ServiceLinkTask{}.ExportApp(ctx, "worker")
	if err != nil {
		t.Fatalf("ExportApp worker: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 links for worker, got %d", len(bodies))
	}
}

func TestExportAclServiceReadsUsers(t *testing.T) {
	t.Parallel()
	// acl:list-service emits one username per line on stdout in dokku-acl
	// 2.0.0+, and on stderr in 1.5.1 and earlier.
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			t.Parallel()
			stdout := map[string]string{
				"--quiet plugin:trigger service-list": "postgres:my-db\nredis:cache",
			}
			stderr := map[string]string{}
			acl := stdout
			if stream == "stderr" {
				acl = stderr
			}
			acl["--quiet acl:list-service postgres my-db"] = "bob\nalice"
			acl["--quiet acl:list-service redis cache"] = ""
			ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
				key := strings.Join(in.Args, " ")
				return subprocess.ExecCommandResponse{Stdout: stdout[key], Stderr: stderr[key]}, nil
			})

			bodies, err := AclServiceTask{}.ExportGlobal(ctx)
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
			// state:set replaces the whole list, so re-applying an export
			// converges a service carrying an extra user rather than adding to it.
			if a.State != StateSet {
				t.Errorf("acl State = %q, want %q", a.State, StateSet)
			}
			if err := a.Validate(); err != nil {
				t.Errorf("exported task must be valid, got: %v", err)
			}
			if plan := a.Plan(ctx); !plan.InSync {
				t.Errorf("re-planning the exported task should report no drift, got status %v reason %q", plan.Status, plan.Reason)
			}
		})
	}
}

func TestExportConfigExcludesLinkedServiceDSNs(t *testing.T) {
	t.Parallel()
	dsn := "postgres://postgres:pw@dokku-postgres-my-db:5432/my_db"
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list": "web",
		// DATABASE_URL is the plain link; ALT_DB_URL is the same link made with
		// a scheme override and a querystring, which an exact match misses.
		"--quiet config:export --format json web": `{"DATABASE_URL":"` + dsn + `","ALT_DB_URL":"postgresql://postgres:pw@dokku-postgres-my-db:5432/my_db?sslmode=disable","SECRET_KEY":"s3cr3t","NO_VHOST":"1","COMMIT_SHA":"abc123"}`,
		"git:report web --git-rev-env-var":        "COMMIT_SHA",
		"--quiet plugin:trigger service-list":     "postgres:my-db",
		"--quiet postgres:links my-db":            "web",
		"--quiet postgres:info my-db --dsn":       dsn,
	}))

	bodies, err := ConfigTask{}.ExportApp(ctx, "web")
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 config task, got %d", len(bodies))
	}
	c := bodies[0].(ConfigTask)
	if !reflect.DeepEqual(c.Config, map[string]string{"SECRET_KEY": "s3cr3t"}) {
		t.Errorf("config = %+v, want only SECRET_KEY: link keys, NO_VHOST and the rev var are not the recipe's", c.Config)
	}
	if c.State != StateSet {
		t.Errorf("state = %q, want %q so the export declares the app's entire config", c.State, StateSet)
	}
}

func TestIsLinkedServiceValue(t *testing.T) {
	t.Parallel()
	dsn := "postgres://postgres:pw@dokku-postgres-my-db:5432/my_db"
	cases := []struct {
		name  string
		value string
		dsns  map[string]bool
		want  bool
	}{
		{"exact dsn", dsn, map[string]bool{dsn: true}, true},
		{"scheme override", "postgresql://postgres:pw@dokku-postgres-my-db:5432/my_db", map[string]bool{dsn: true}, true},
		{"querystring", dsn + "?sslmode=disable", map[string]bool{dsn: true}, true},
		{"unrelated value", "info", map[string]bool{dsn: true}, false},
		{"external database", "postgres://postgres:other@mydb.abc123.us-east-1.rds.amazonaws.com:5432/my_db", map[string]bool{dsn: true}, false},
		{"another service", "postgres://postgres:pw@dokku-postgres-other:5432/other", map[string]bool{dsn: true}, false},
		{"no linked services", dsn, map[string]bool{}, false},
		{"empty dsn never matches", "anything", map[string]bool{"": true}, false},
		{"scheme-only dsn never matches", "anything", map[string]bool{"postgres://": true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isLinkedServiceValue(tc.value, tc.dsns); got != tc.want {
				t.Errorf("isLinkedServiceValue(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestExportRecipeIncludesServiceTasks(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list":                           "web",
		"--quiet config:export --format json web":     `{}`,
		"domains:report web --domains-app-vhosts":     "",
		"--quiet plugin:trigger service-list":         "postgres:my-db",
		"--quiet postgres:info my-db --exposed-ports": "5432->5432",
		"--quiet postgres:links my-db":                "web",
		"--quiet postgres:info my-db --dsn":           "postgres://postgres:pw@dokku-postgres-my-db:5432/my_db",
		infoKey("postgres", "my-db"):                  `{"version":"postgres:17.2","restart-policy":"always"}`,
	}))

	res, err := ExportRecipe(ctx, ExportOptions{})
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
		"dokku_service_property",
		"property: restart-policy",
		"dokku_service_expose",
		"dokku_service_link",
		// The exported create carries the image the service is running.
		"image: postgres",
		"image_version: \"17.2\"",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
	// Drift policy is a recipe-authoring choice, not something a server has,
	// so export must not put words in the operator's mouth about it. Both
	// fields rely on `omitempty` and their zero values to stay out.
	for _, unwanted := range []string{"image_drift", "restart_apps"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("recipe should not export %q:\n%s", unwanted, out)
		}
	}
}
