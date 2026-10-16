package tasks

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// readbackFixture drives one exporter against a fake dokku whose answers carry
// a value for every field the server does report, so an exported body that
// fills a write_only or apply_only field could only have done so by reading
// it - which is the claim the tag denies.
type readbackFixture struct {
	responses map[string]string
	export    func(ctx context.Context) ([]interface{}, error)
}

// readbackFixtures holds one fixture per exporter that carries a write_only or
// apply_only field. TestExportReadbackFixturesCoverEveryTaggedExporter fails
// when a tagged exporter is missing here.
func readbackFixtures() map[string]readbackFixture {
	app := func(task AppExporter) func(ctx context.Context) ([]interface{}, error) {
		return func(ctx context.Context) ([]interface{}, error) { return task.ExportApp(ctx, "web") }
	}
	global := func(task GlobalExporter) func(ctx context.Context) ([]interface{}, error) {
		return task.ExportGlobal
	}
	certPEM := "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----"
	keyPEM := "-----BEGIN PRIVATE KEY-----\nMIIE\n-----END PRIVATE KEY-----"

	return map[string]readbackFixture{
		"dokku_http_auth_user": {
			responses: httpAuthExportFixture("admin", "admin:"+exportHttpAuthHash+"\n"),
			export:    app(HttpAuthUserTask{}),
		},
		"dokku_http_auth": {
			responses: httpAuthExportFixture("admin", "admin:"+exportHttpAuthHash+"\n"),
			export:    app(HttpAuthTask{}),
		},
		"dokku_service_create": {
			responses: mergeFixtures(
				map[string]string{"--quiet plugin:trigger service-list": "postgres:my-db"},
				infoFixture("postgres", "my-db", map[string]string{"version": "postgres:17.2", "memory": "512"}),
			),
			export: global(ServiceCreateTask{}),
		},
		"dokku_service_backup": {
			responses: mergeFixtures(
				map[string]string{"--quiet plugin:trigger service-list": "postgres:my-db"},
				infoFixture("postgres", "my-db", map[string]string{
					"backup-schedule":                      "0 3 * * *",
					"backup-bucket":                        "my-bucket",
					"backup-use-iam":                       "false",
					"backup-access-key-id-fingerprint":     backupFingerprint("AKIAEXAMPLE"),
					"backup-secret-access-key-fingerprint": backupFingerprint("secret"),
					"backup-encryption-fingerprint":        backupFingerprint("passphrase"),
				}),
			),
			export: global(ServiceBackupTask{}),
		},
		"dokku_registry_auth": {
			responses: map[string]string{
				"--quiet registry:report web --format json":      `{"auth-servers":"ghcr.io"}`,
				"--quiet registry:report --global --format json": `{"global-auth-servers":"docker.io"}`,
			},
			export: app(RegistryAuthTask{}),
		},
		"dokku_config": {
			responses: map[string]string{
				"--quiet config:export --format json web": `{"SECRET_KEY":"s3cr3t"}`,
			},
			export: app(ConfigTask{}),
		},
		"dokku_git_sync": {
			responses: map[string]string{
				"apps:report web --format json": `{"app-deploy-source":"git-sync","app-deploy-source-metadata":"https://github.com/org/repo.git#abc123def"}`,
				"git:report web --format json":  `{"deploy-branch":"main"}`,
			},
			export: app(GitSyncTask{}),
		},
		"dokku_git_from_archive": {
			responses: map[string]string{
				"apps:report web --format json": `{"app-deploy-source":"tar","app-deploy-source-metadata":"https://example.com/app.tar"}`,
			},
			export: app(GitFromArchiveTask{}),
		},
		"dokku_git_from_image": {
			responses: map[string]string{
				"apps:report web --format json": `{"app-deploy-source":"docker-image","app-deploy-source-metadata":"nginx:1.27"}`,
			},
			export: app(GitFromImageTask{}),
		},
		"dokku_storage_entry": {
			responses: storageEntriesFixture(),
			export:    global(StorageEntryTask{}),
		},
		"dokku_ps_scale": {
			responses: map[string]string{"--quiet ps:scale web": "web: 2"},
			export:    app(PsScaleTask{}),
		},
		"dokku_resource_limit": {
			responses: map[string]string{"--quiet resource:report web --format json": `{"_default_.limit.memory":"512m"}`},
			export:    app(ResourceLimitTask{}),
		},
		"dokku_resource_reserve": {
			responses: map[string]string{"--quiet resource:report web --format json": `{"_default_.reserve.memory":"256m"}`},
			export:    app(ResourceReserveTask{}),
		},
		"dokku_certs": {
			responses: mergeFixtures(certsAppFixture("web", certPEM), map[string]string{
				"--quiet certs:show web key": keyPEM,
			}),
			export: app(CertsTask{}),
		},
	}
}

// TestExportLeavesUnreadableFieldsEmpty is the runtime half of the catalog's
// write_only and apply_only flags (#604): a consumer refreshing state from an
// export trusts them to say which fields carry no server value, so an exporter
// that starts filling one in has to drop the tag too. A write_only field must
// come back empty or as a placeholder whose vars entry is empty - a placeholder
// backed by a real value would mean the value was read after all. An
// apply_only field must hold its zero value or its declared default.
func TestExportLeavesUnreadableFieldsEmpty(t *testing.T) {
	t.Parallel()
	for typeKey, fixture := range readbackFixtures() {
		t.Run(typeKey, func(t *testing.T) {
			t.Parallel()
			ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(fixture.responses))
			bodies, err := fixture.export(ctx)
			if err != nil {
				t.Fatalf("export: %v", err)
			}
			if len(bodies) == 0 {
				t.Fatal("the fixture exported nothing, so the fields went unchecked")
			}

			res := &ExportResult{Vars: map[string]string{}, usedVarNames: map[string]bool{}}
			checked := 0
			for _, body := range bodies {
				if got := reflect.TypeOf(body); got != registeredTypeOrNil(typeKey) {
					t.Fatalf("exported %v, want the type registered as %s", got, typeKey)
				}
				out, _ := res.processBody("web", body, ExportOptions{})
				n, problems := checkReadbackFields(typeKey, reflect.ValueOf(out), res.Vars)
				checked += n
				for _, problem := range problems {
					t.Error(problem)
				}
			}
			if checked == 0 {
				t.Error("no tagged field was reached, so the fields went unchecked")
			}
		})
	}
}

// TestExportReadbackFixturesCoverEveryTaggedExporter keeps
// TestExportLeavesUnreadableFieldsEmpty from going stale: tagging a field on a
// task that exports, without a fixture to prove the exporter leaves it alone,
// fails here.
func TestExportReadbackFixturesCoverEveryTaggedExporter(t *testing.T) {
	t.Parallel()
	fixtures := readbackFixtures()
	for name, task := range allRegisteredTasks() {
		_, isApp := task.(AppExporter)
		_, isGlobal := task.(GlobalExporter)
		if !isApp && !isGlobal {
			continue
		}
		if !hasReadbackTag(taskStructOf(task)) {
			continue
		}
		if _, ok := fixtures[name]; !ok {
			t.Errorf("task %q exports and has a write_only or apply_only field but no readback fixture", name)
		}
	}
	for name := range fixtures {
		task, ok := Lookup(name)
		if !ok {
			t.Errorf("readback fixture %q names no registered task", name)
			continue
		}
		if !hasReadbackTag(taskStructOf(task)) {
			t.Errorf("readback fixture %q covers a task with no write_only or apply_only field", name)
		}
	}
}

// registeredTypeOrNil returns the value type registered under typeKey.
func registeredTypeOrNil(typeKey string) reflect.Type {
	rt, _ := registeredType(typeKey)
	return rt
}

// hasReadbackTag reports whether rt, or the struct element of one of its
// collection fields, carries a write_only or apply_only tag.
func hasReadbackTag(rt reflect.Type) bool {
	if rt == nil || rt.Kind() != reflect.Struct {
		return false
	}
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.Tag.Get("write_only") == "true" || field.Tag.Get("apply_only") == "true" {
			return true
		}
		if elem := readbackElem(field.Type); elem != nil && hasReadbackTag(elem) {
			return true
		}
	}
	return false
}

// readbackElem returns the struct element of a slice or map field, or nil.
func readbackElem(t reflect.Type) reflect.Type {
	if t.Kind() != reflect.Slice && t.Kind() != reflect.Array && t.Kind() != reflect.Map {
		return nil
	}
	return structElem(t.Elem())
}

// checkReadbackFields checks the write_only and apply_only contract on every
// tagged field of v, descending into slices of structs. It returns how many
// tagged fields it checked and a problem per field that breaks the contract.
func checkReadbackFields(where string, v reflect.Value, vars map[string]string) (int, []string) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return 0, nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, nil
	}

	checked := 0
	var problems []string
	rt := v.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		fv := v.Field(i)
		name := where + "." + yamlFieldName(field)

		if field.Tag.Get("write_only") == "true" {
			checked++
			if value := fv.String(); value != "" {
				varName, ok := placeholderVar(value)
				switch {
				case !ok:
					problems = append(problems, fmt.Sprintf("%s is write_only but exported %q", name, value))
				case vars[varName] != "":
					problems = append(problems, fmt.Sprintf("%s is write_only but its input %s carries a value read from the server", name, varName))
				}
			}
		}

		if field.Tag.Get("apply_only") == "true" {
			checked++
			if !isZeroOrDefault(fv, field.Tag.Get("default")) {
				problems = append(problems, fmt.Sprintf("%s is apply_only but exported %v, which is neither its zero value nor its default", name, fv.Interface()))
			}
		}

		if fv.Kind() == reflect.Slice || fv.Kind() == reflect.Array {
			for j := 0; j < fv.Len(); j++ {
				n, nested := checkReadbackFields(name+"[]", fv.Index(j), vars)
				checked += n
				problems = append(problems, nested...)
			}
		}
	}
	return checked, problems
}

// TestCheckReadbackFieldsCatchesALeak proves the checker is not vacuous: a
// body that carries a server value in each kind of tagged field - a nested
// write_only password, a placeholder backed by a real vars entry, an apply_only
// knob off its default - is reported once per field, and the clean form of
// each passes.
func TestCheckReadbackFieldsCatchesALeak(t *testing.T) {
	t.Parallel()
	update := true
	cases := []struct {
		name string
		body interface{}
		vars map[string]string
		want int
	}{
		{"nested write_only value", HttpAuthUserTask{App: "web", Users: []HttpAuthUser{{Username: "admin", Password: "hunter2"}}}, nil, 1},
		{"placeholder backed by a value", RegistryAuthTask{Server: "ghcr.io", Username: "{{ .user }}", Password: "{{ .pass }}"}, map[string]string{"user": "", "pass": "read-back"}, 1},
		{"apply_only off its default", HttpAuthUserTask{App: "web", UpdatePassword: &update}, nil, 1},
		{"apply_only at a pointer default", ConfigTask{App: "web", Restart: &update}, nil, 0},
		{"empty placeholders", RegistryAuthTask{Server: "ghcr.io", Username: "{{ .user }}", Password: "{{ .pass }}"}, map[string]string{"user": "", "pass": ""}, 0},
	}
	for _, tc := range cases {
		checked, problems := checkReadbackFields("task", reflect.ValueOf(tc.body), tc.vars)
		if checked == 0 {
			t.Errorf("%s: no tagged field was checked", tc.name)
		}
		if len(problems) != tc.want {
			t.Errorf("%s: problems = %q; want %d", tc.name, problems, tc.want)
		}
	}
}

// placeholderVar returns the input name inside a `{{ .name }}` placeholder.
func placeholderVar(value string) (string, bool) {
	inner, ok := strings.CutPrefix(value, "{{ .")
	if !ok {
		return "", false
	}
	inner, ok = strings.CutSuffix(inner, " }}")
	return inner, ok
}

// isZeroOrDefault reports whether fv holds its zero value or the value its
// `default` tag declares. A pointer counts when it is nil or points at either.
func isZeroOrDefault(fv reflect.Value, def string) bool {
	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			return true
		}
		fv = fv.Elem()
	}
	if fv.IsZero() {
		return true
	}
	if def == "" {
		return false
	}
	switch fv.Kind() {
	case reflect.String:
		return fv.String() == def
	case reflect.Bool:
		want, err := strconv.ParseBool(def)
		return err == nil && fv.Bool() == want
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		want, err := strconv.ParseInt(def, 10, 64)
		return err == nil && fv.Int() == want
	}
	return false
}
