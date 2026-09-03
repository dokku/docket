package tasks

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// fakeDokku returns an exec runner that answers dokku invocations from a map
// keyed by the space-joined args, so export tests never spawn a process or
// contact a server. Unlisted commands return empty stdout.
func fakeDokku(responses map[string]string) func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	return func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{Stdout: responses[strings.Join(in.Args, " ")]}, nil
	}
}

func exportFixture() map[string]string {
	return map[string]string{
		"--quiet apps:list":                           "app-one\napp-two",
		"--quiet config:export --format json app-one": `{"SECRET_KEY":"s3cr3t","LOG_LEVEL":"info"}`,
		"domains:report app-one --domains-app-vhosts": "app-one.example.com www.example.com",
		"--quiet config:export --format json app-two": `{}`,
		"domains:report app-two --domains-app-vhosts": "",
	}
}

func TestAppExportOrderIsValid(t *testing.T) {
	t.Parallel()
	for _, key := range appExportOrder {
		proto, ok := RegisteredTasks[key]
		if !ok {
			t.Errorf("appExportOrder has unknown task key %q", key)
			continue
		}
		if _, ok := proto.(AppExporter); !ok {
			t.Errorf("task %q in appExportOrder does not implement AppExporter", key)
		}
	}
}

func TestGlobalExportOrderIsValid(t *testing.T) {
	t.Parallel()
	for _, key := range globalExportOrder {
		proto, ok := RegisteredTasks[key]
		if !ok {
			t.Errorf("globalExportOrder has unknown task key %q", key)
			continue
		}
		if _, ok := proto.(GlobalExporter); !ok {
			t.Errorf("task %q in globalExportOrder does not implement GlobalExporter", key)
		}
	}
}

func TestExportPluginsBecomeTasks(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet plugin:list --format json": `[
			{"name":"00_dokku-standard","core":true,"source_url":"","committish":"","branch":""},
			{"name":"redis","core":false,"source_url":"https://github.com/dokku/dokku-redis.git","committish":"c0ffee1234","branch":"master"},
			{"name":"acl","core":false,"source_url":"https://github.com/dokku/dokku-acl.git","committish":"def4567890abcdef","branch":""},
			{"name":"tarball-plugin","core":false,"source_url":"","committish":"","branch":""}
		]`,
	}))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	// Plugin URLs are readable, so nothing is lifted into the vars map.
	if res.HasVars() {
		t.Errorf("expected no lifted vars, got %v", res.Vars)
	}

	recipe, err := res.MarshalRecipe("yaml")
	if err != nil {
		t.Fatalf("MarshalRecipe: %v", err)
	}
	out := string(recipe)

	// The two git-installed third-party plugins are reconstructed, with the URL
	// inline and the committish following the branch (redis) or the exact commit
	// for a detached checkout (acl).
	for _, want := range []string{
		"name: global",
		"dokku_plugin",
		"name: redis",
		"https://github.com/dokku/dokku-redis.git",
		"committish: master",
		"name: acl",
		"https://github.com/dokku/dokku-acl.git",
		"committish: def4567890abcdef",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}

	// Core plugins and non-git (tarball/local) installs are skipped.
	for _, unwanted := range []string{"00_dokku-standard", "tarball-plugin"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("recipe should not contain %q:\n%s", unwanted, out)
		}
	}

	// The URL is emitted inline, never lifted into a (sensitive) input.
	if strings.Contains(out, "{{") {
		t.Errorf("recipe should not template the plugin url:\n%s", out)
	}
	if strings.Contains(out, "sensitive: true") {
		t.Errorf("plugin url should not be marked sensitive:\n%s", out)
	}
}

func TestExportRecipeFileMode(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	// Two apps -> two plays.
	if len(res.plays) != 2 {
		t.Fatalf("expected 2 plays, got %d", len(res.plays))
	}

	// Config values are lifted into the vars map, not left in the recipe.
	if got := res.Vars["app_one_SECRET_KEY"]; got != "s3cr3t" {
		t.Errorf("vars[app_one_SECRET_KEY] = %q, want s3cr3t", got)
	}
	if got := res.Vars["app_one_LOG_LEVEL"]; got != "info" {
		t.Errorf("vars[app_one_LOG_LEVEL] = %q, want info", got)
	}

	recipe, err := res.MarshalRecipe("yaml")
	if err != nil {
		t.Fatalf("MarshalRecipe: %v", err)
	}
	out := string(recipe)

	// The recipe references the lifted values via inputs, and never contains
	// the raw secret.
	if strings.Contains(out, "s3cr3t") {
		t.Errorf("recipe leaked a secret value:\n%s", out)
	}
	for _, want := range []string{
		"{{ .app_one_SECRET_KEY }}",
		"name: app-one",
		"dokku_app",
		"dokku_config",
		"dokku_domains",
		"app-one.example.com",
		"name: app_one_SECRET_KEY",
		"name: app-two",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}

	// Both apps get a dokku_app task (the ":" avoids matching dokku_app_lock).
	if strings.Count(out, "dokku_app:") != 2 {
		t.Errorf("expected dokku_app in both plays:\n%s", out)
	}
}

// exportHttpAuthHash is the stored htpasswd entry the export tests read back
// through http-auth:export-users.
const exportHttpAuthHash = "$6$Xm3kx1s9$Zq8mQ0zH1p"

// httpAuthExportFixture is a one-app server with http-auth serving for the
// named users. Both the report and export-users have to answer: the report
// drives dokku_http_auth, export-users drives dokku_http_auth_user.
func httpAuthExportFixture(users, entries string) map[string]string {
	return map[string]string{
		"--quiet apps:list":                  "web",
		"http-auth:report web --format json": `{"enabled":"true","users":"` + users + `","allowed-ips":"","domains":""}`,
		"--quiet http-auth:export-users web": entries,
	}
}

// TestExportHttpAuthUserHashesBecomeVars is the payoff of #443: the users come
// back carrying their htpasswd hashes, so the recipe reproduces them with no
// operator-supplied password. The hash is still credential material, so it
// lands in the vars-file behind a sensitive input rather than in the recipe.
func TestExportHttpAuthUserHashesBecomeVars(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthExportFixture("admin", "admin:"+exportHttpAuthHash+"\n")))

	bodies, err := HttpAuthUserTask{}.ExportApp(ctx, "web")
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 exported task, got %d", len(bodies))
	}
	users := bodies[0].(HttpAuthUserTask)
	if len(users.Users) != 1 || users.Users[0].Hash != exportHttpAuthHash {
		t.Fatalf("exported users = %+v, want admin carrying the stored hash", users.Users)
	}
	if users.Users[0].Password != "" {
		t.Errorf("exported user must carry no cleartext password, got %q", users.Users[0].Password)
	}
	// State is explicit so the body is plannable straight out of the exporter.
	if users.State != StatePresent {
		t.Errorf("State = %q, want %q", users.State, StatePresent)
	}
	if err := users.Validate(); err != nil {
		t.Errorf("exported task must be valid, got: %v", err)
	}
	if plan := users.Plan(ctx); !plan.InSync {
		t.Errorf("re-planning the exported task should report no drift, got status %v reason %q", plan.Status, plan.Reason)
	}

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if got := res.Vars["web_http_auth_hash_admin"]; got != exportHttpAuthHash {
		t.Errorf("vars[web_http_auth_hash_admin] = %q, want the real hash", got)
	}
	if len(res.Report.Warnings) != 0 {
		for _, w := range res.Report.Warnings {
			if strings.Contains(w, "http-auth") {
				t.Errorf("unexpected http-auth export warning: %q", w)
			}
		}
	}

	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	if strings.Contains(out, exportHttpAuthHash) {
		t.Errorf("recipe leaked the hash instead of lifting it:\n%s", out)
	}
	for _, want := range []string{
		"username: admin",
		"{{ .web_http_auth_hash_admin }}",
		"name: web_http_auth_hash_admin",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
}

// TestExportHttpAuthUserRedactBlanksTheVar keeps the redacted pair usable: the
// recipe still references the input, only the vars-file value is withheld.
func TestExportHttpAuthUserRedactBlanksTheVar(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthExportFixture("admin", "admin:"+exportHttpAuthHash+"\n")))

	res, err := ExportRecipe(ctx, ExportOptions{Redact: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	v, ok := res.Vars["web_http_auth_hash_admin"]
	if !ok || v != "" {
		t.Errorf("expected an empty placeholder under --redact, got %q (ok=%v)", v, ok)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	if !strings.Contains(string(recipe), "{{ .web_http_auth_hash_admin }}") {
		t.Errorf("redacted recipe must still reference the input:\n%s", recipe)
	}
}

// TestExportHttpAuthEnabledBecomesTask covers the first row of the export state
// table (#428): an app serving http-auth emits dokku_http_auth with state
// present, after the rest of the family so it is authoritative.
func TestExportHttpAuthEnabledBecomesTask(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthExportFixture("admin", "admin:"+exportHttpAuthHash+"\n")))

	bodies, err := HttpAuthTask{}.ExportApp(ctx, "web")
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 exported task, got %d", len(bodies))
	}
	auth := bodies[0].(HttpAuthTask)
	if auth.State != StatePresent {
		t.Errorf("State = %q, want %q", auth.State, StatePresent)
	}
	// The credentials are asserted on the struct, not by grepping the rendered
	// recipe: the sibling dokku_http_auth_user task legitimately emits a
	// username for each user, so a whole-recipe check would match that instead.
	if auth.Username != "" || auth.Password != "" {
		t.Errorf("exported enable task should carry no credentials, got username=%q password=%q", auth.Username, auth.Password)
	}
	if err := auth.Validate(); err != nil {
		t.Errorf("exported http-auth task must be valid, got: %v", err)
	}

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	for _, want := range []string{"dokku_http_auth:", "state: present"} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
	// http-auth:add-user writes enabled=true as a side effect, so the task that
	// owns the flag has to come last for the stated state to stick.
	enable := strings.Index(out, "dokku_http_auth:")
	users := strings.Index(out, "dokku_http_auth_user:")
	if users < 0 {
		t.Fatalf("recipe missing the user task:\n%s", out)
	}
	if enable < users {
		t.Errorf("dokku_http_auth must be emitted after dokku_http_auth_user:\n%s", out)
	}
}

// TestExportHttpAuthEnabledWithoutUsersBecomesTask covers the second row: a bare
// `http-auth:enable <app>` leaves no users, allowed IPs or domains behind, so
// nothing else's enable side effect would restore it and this task is the only
// thing that carries the state.
func TestExportHttpAuthEnabledWithoutUsersBecomesTask(t *testing.T) {
	t.Parallel()
	// export-users is answered explicitly with nothing, so the "no user task"
	// assertion below tests the empty htpasswd rather than a missing fixture.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthExportFixture("", "")))

	bodies, err := HttpAuthTask{}.ExportApp(ctx, "web")
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 exported task, got %d", len(bodies))
	}
	if got := bodies[0].(HttpAuthTask).State; got != StatePresent {
		t.Errorf("State = %q, want %q", got, StatePresent)
	}
	if err := bodies[0].(HttpAuthTask).Validate(); err != nil {
		t.Errorf("exported http-auth task must be valid, got: %v", err)
	}

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	if !strings.Contains(out, "dokku_http_auth:") {
		t.Errorf("recipe missing the enable task:\n%s", out)
	}
	if strings.Contains(out, "dokku_http_auth_user:") {
		t.Errorf("no user task expected when the app has no http-auth users:\n%s", out)
	}
}

// TestExportHttpAuthDisabledWithConfigBecomesAbsentTask covers the third row:
// http-auth:disable leaves the users, allowed IPs and auth domains in place, and
// re-applying those turns auth back on, so the export has to state absent
// explicitly to undo it.
func TestExportHttpAuthDisabledWithConfigBecomesAbsentTask(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		report string
	}{
		{"users remain", `{"enabled":"false","users":"admin","allowed-ips":"","domains":""}`},
		{"allowed ips remain", `{"enabled":"false","users":"","allowed-ips":"192.0.2.1","domains":""}`},
		{"auth domains remain", `{"enabled":"false","users":"","allowed-ips":"","domains":"web.example.com"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
				"--quiet apps:list":                  "web",
				"http-auth:report web --format json": tc.report,
			}))

			bodies, err := HttpAuthTask{}.ExportApp(ctx, "web")
			if err != nil {
				t.Fatalf("ExportApp: %v", err)
			}
			if len(bodies) != 1 {
				t.Fatalf("expected 1 exported task, got %d", len(bodies))
			}
			auth := bodies[0].(HttpAuthTask)
			if auth.State != StateAbsent {
				t.Errorf("State = %q, want %q", auth.State, StateAbsent)
			}
			if err := auth.Validate(); err != nil {
				t.Errorf("exported http-auth task must be valid, got: %v", err)
			}

			res, err := ExportRecipe(ctx, ExportOptions{})
			if err != nil {
				t.Fatalf("ExportRecipe: %v", err)
			}
			recipe, _ := res.MarshalRecipe("yaml")
			out := string(recipe)
			if !strings.Contains(out, "dokku_http_auth:") {
				t.Errorf("recipe missing the disable task:\n%s", out)
			}
			if !strings.Contains(out, "state: absent") {
				t.Errorf("recipe missing the absent state:\n%s", out)
			}
		})
	}
}

// TestExportHttpAuthDisabledEmitsNoTask covers the fourth row: an app with auth
// off and nothing configured is the default for every app on a server, so it
// must not litter every play with a no-op disable task.
func TestExportHttpAuthDisabledEmitsNoTask(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list":                  "web",
		"http-auth:report web --format json": `{"enabled":"false","users":"","allowed-ips":"","domains":""}`,
	}))

	bodies, err := HttpAuthTask{}.ExportApp(ctx, "web")
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 0 {
		t.Errorf("expected no exported task for an unconfigured app, got %v", bodies)
	}

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	if strings.Contains(string(recipe), "dokku_http_auth:") {
		t.Errorf("expected no dokku_http_auth task:\n%s", recipe)
	}
}

func TestExportMaintenanceCustomPageBecomesInput(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list":                    "web",
		"maintenance:report web --format json": `{"enabled":"false","custom-page-sha256":"7b645f273842a941c68302a4022ed03e219bd8db318ef32a92dddb148a72ef05"}`,
	}))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	// The HTML is not readable, so it is lifted to a required input with an
	// empty placeholder in the vars map.
	v, ok := res.Vars["web_maintenance_custom_page"]
	if !ok || v != "" {
		t.Errorf("expected empty placeholder for custom page, got %q (ok=%v)", v, ok)
	}

	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	for _, want := range []string{
		"dokku_maintenance_custom_page",
		"{{ .web_maintenance_custom_page }}",
		"name: web_maintenance_custom_page",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
	// The page is public HTML, not a secret: the input is not marked sensitive,
	// and the checksum never leaks into the recipe body.
	if strings.Contains(out, "sensitive: true") {
		t.Errorf("custom page input should not be sensitive:\n%s", out)
	}
	if strings.Contains(out, "7b645f273842a941") {
		t.Errorf("recipe leaked the custom-page checksum:\n%s", out)
	}
}

func TestExportMaintenanceCustomPageAbsentEmitsNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		report string
	}{
		{"set but empty", `{"enabled":"false","custom-page-sha256":""}`},
		{"key absent (old plugin)", `{"enabled":"false"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
				"--quiet apps:list":                    "web",
				"maintenance:report web --format json": tc.report,
			}))

			res, err := ExportRecipe(ctx, ExportOptions{})
			if err != nil {
				t.Fatalf("ExportRecipe: %v", err)
			}
			if _, ok := res.Vars["web_maintenance_custom_page"]; ok {
				t.Errorf("expected no lifted var when no custom page is set")
			}
			recipe, _ := res.MarshalRecipe("yaml")
			if strings.Contains(string(recipe), "dokku_maintenance_custom_page") {
				t.Errorf("expected no dokku_maintenance_custom_page task:\n%s", recipe)
			}
		})
	}
}

func TestExportRecipeRedactBlanksVars(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	res, err := ExportRecipe(ctx, ExportOptions{Redact: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if v, ok := res.Vars["app_one_SECRET_KEY"]; !ok || v != "" {
		t.Errorf("redacted vars[app_one_SECRET_KEY] = %q (ok=%v), want empty and present", v, ok)
	}

	recipe, _ := res.MarshalRecipe("yaml")
	if !strings.Contains(string(recipe), "{{ .app_one_SECRET_KEY }}") {
		t.Errorf("redacted recipe should still reference the input:\n%s", recipe)
	}
}

func TestExportRecipeInlineKeepsValues(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	res, err := ExportRecipe(ctx, ExportOptions{Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if res.HasVars() {
		t.Errorf("inline mode should not lift any vars, got %v", res.Vars)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	if !strings.Contains(out, "s3cr3t") {
		t.Errorf("inline recipe should contain the real value:\n%s", out)
	}
	if strings.Contains(out, "{{ .") {
		t.Errorf("inline recipe should not use input templates:\n%s", out)
	}
}

func TestExportRecipeAppFilter(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	res, err := ExportRecipe(ctx, ExportOptions{Apps: []string{"app-two"}})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if len(res.plays) != 1 {
		t.Fatalf("expected 1 play with --app filter, got %d", len(res.plays))
	}
	recipe, _ := res.MarshalRecipe("yaml")
	if strings.Contains(string(recipe), "app-one") {
		t.Errorf("filtered export should not include app-one:\n%s", recipe)
	}
}

func TestExportGlobalCertBecomesGlobalTask(t *testing.T) {
	t.Parallel()
	certPEM := "-----BEGIN CERTIFICATE-----\nMIIFAKECERT\n-----END CERTIFICATE-----"
	keyPEM := "-----BEGIN PRIVATE KEY-----\nMIIFAKEKEY\n-----END PRIVATE KEY-----"
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list": "",
		"--quiet global-cert:report --global --global-cert-enabled": "true",
		"--quiet global-cert:show crt":                              certPEM,
		"--quiet global-cert:show key":                              keyPEM,
	}))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	// global-cert:show streams the real PEM, so it is lifted into the vars map
	// under the global scope (not blanked like an unreadable secret).
	if got := res.Vars["global_cert_content"]; got != certPEM {
		t.Errorf("vars[global_cert_content] = %q, want the cert PEM", got)
	}
	if got := res.Vars["global_key_content"]; got != keyPEM {
		t.Errorf("vars[global_key_content] = %q, want the key PEM", got)
	}

	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	for _, want := range []string{
		"name: global",
		"dokku_certs",
		"global: true",
		"{{ .global_cert_content }}",
		"{{ .global_key_content }}",
		"name: global_cert_content",
		"name: global_key_content",
		"sensitive: true",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
	// The PEM is lifted into the vars-file, never left inline in the recipe body.
	if strings.Contains(out, "MIIFAKECERT") || strings.Contains(out, "MIIFAKEKEY") {
		t.Errorf("recipe leaked the certificate PEM:\n%s", out)
	}
}

func TestExportInlineRedactBlanksSensitiveScalar(t *testing.T) {
	t.Parallel()
	// Regression for #311: inline + redact must blank sensitive scalar fields
	// (cert/key PEM here) in place, not fall back to the original unredacted body.
	certPEM := "-----BEGIN CERTIFICATE-----\nMIIFAKECERT\n-----END CERTIFICATE-----"
	keyPEM := "-----BEGIN PRIVATE KEY-----\nMIIFAKEKEY\n-----END PRIVATE KEY-----"
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list": "",
		"--quiet global-cert:report --global --global-cert-enabled": "true",
		"--quiet global-cert:show crt":                              certPEM,
		"--quiet global-cert:show key":                              keyPEM,
	}))

	res, err := ExportRecipe(ctx, ExportOptions{Inline: true, Redact: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	if strings.Contains(out, "MIIFAKECERT") || strings.Contains(out, "MIIFAKEKEY") {
		t.Errorf("inline+redact leaked the certificate PEM:\n%s", out)
	}
	// The task is still present, just with blanked content.
	if !strings.Contains(out, "dokku_certs") {
		t.Errorf("expected the certs task to remain after redaction:\n%s", out)
	}
}

func TestExportAppCertSkippedWhenLetsencryptActive(t *testing.T) {
	t.Parallel()
	// Regression for #337: a letsencrypt-managed app reports ssl-enabled, but its
	// cert is ephemeral and re-issued by dokku_letsencrypt, so it must not be
	// pinned as a static dokku_certs task.
	certPEM := "-----BEGIN CERTIFICATE-----\nMIILEACERT\n-----END CERTIFICATE-----"
	keyPEM := "-----BEGIN PRIVATE KEY-----\nMIILEAKEY\n-----END PRIVATE KEY-----"
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list":                      "web",
		"--quiet certs:report web --ssl-enabled": "true",
		"--quiet letsencrypt:active web":         "true",
		"--quiet certs:show web crt":             certPEM,
		"--quiet certs:show web key":             keyPEM,
	}))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	if strings.Contains(out, "dokku_certs") {
		t.Errorf("letsencrypt-managed cert must not be exported as dokku_certs:\n%s", out)
	}
	if strings.Contains(out, "MIILEACERT") || strings.Contains(out, "MIILEAKEY") {
		t.Errorf("recipe leaked the ephemeral letsencrypt PEM:\n%s", out)
	}
	if !strings.Contains(out, "dokku_letsencrypt") {
		t.Errorf("expected the letsencrypt task to be exported:\n%s", out)
	}
}

func TestExportAppCertExportedWhenLetsencryptInactive(t *testing.T) {
	t.Parallel()
	// A manual (non-letsencrypt) cert is still exported as dokku_certs.
	certPEM := "-----BEGIN CERTIFICATE-----\nMIIMANUAL\n-----END CERTIFICATE-----"
	keyPEM := "-----BEGIN PRIVATE KEY-----\nMIIMANUALKEY\n-----END PRIVATE KEY-----"
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list":                      "web",
		"--quiet certs:report web --ssl-enabled": "true",
		"--quiet letsencrypt:active web":         "false",
		"--quiet certs:show web crt":             certPEM,
		"--quiet certs:show web key":             keyPEM,
	}))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	if !strings.Contains(out, "dokku_certs") {
		t.Errorf("a manual cert should be exported as dokku_certs:\n%s", out)
	}
	if strings.Contains(out, "dokku_letsencrypt") {
		t.Errorf("no letsencrypt task expected when inactive:\n%s", out)
	}
}

func TestExportMaintenanceCustomPageInlinesContent(t *testing.T) {
	t.Parallel()
	// #334: with maintenance:custom-page-export available, the page HTML is read
	// back and inlined as content instead of lifted into an input, producing a
	// valid, self-contained task in both file and stdout modes.
	html := "<html><body><h1>Down for maintenance</h1></body></html>\n"
	tarBytes, err := buildMaintenancePageTarball(html)
	if err != nil {
		t.Fatalf("buildMaintenancePageTarball: %v", err)
	}
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list":                          "web",
		"maintenance:report web --format json":       `{"enabled":"false","custom-page-sha256":"abc123"}`,
		"--quiet maintenance:custom-page-export web": string(tarBytes),
	}))

	// The task ExportApp yields a valid task carrying the real content.
	bodies, err := MaintenanceCustomPageTask{}.ExportApp(ctx, "web")
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 exported task, got %d", len(bodies))
	}
	page := bodies[0].(MaintenanceCustomPageTask)
	if page.Content != html {
		t.Errorf("Content = %q, want the exported HTML", page.Content)
	}
	if err := page.Validate(); err != nil {
		t.Errorf("exported maintenance page task must be valid, got: %v", err)
	}

	for _, inline := range []bool{false, true} {
		res, err := ExportRecipe(ctx, ExportOptions{Inline: inline})
		if err != nil {
			t.Fatalf("ExportRecipe(ctx, inline=%v): %v", inline, err)
		}
		if _, ok := res.Vars["web_maintenance_custom_page"]; ok {
			t.Errorf("inline=%v: content should be inlined, not lifted into a var", inline)
		}
		recipe, _ := res.MarshalRecipe("yaml")
		out := string(recipe)
		if !strings.Contains(out, "Down for maintenance") {
			t.Errorf("inline=%v: recipe should contain the real page content:\n%s", inline, out)
		}
		if strings.Contains(out, "{{ .web_maintenance_custom_page }}") {
			t.Errorf("inline=%v: content should not be an input template:\n%s", inline, out)
		}
	}
}

// TestExportHttpAuthUserInlineKeepsTheHash covers #410's pipe: a streamed
// recipe has no companion vars-file, and now that the hash is readable it can
// simply ride along, so `export --output - | apply` reproduces the users with
// nothing supplied by hand.
func TestExportHttpAuthUserInlineKeepsTheHash(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthExportFixture("admin", "admin:"+exportHttpAuthHash+"\n")))

	res, err := ExportRecipe(ctx, ExportOptions{Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if len(res.Vars) != 0 {
		t.Errorf("inline mode has no vars-file to write to, got %v", res.Vars)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	if !strings.Contains(out, exportHttpAuthHash) {
		t.Errorf("streamed recipe should carry the hash inline:\n%s", out)
	}
	if strings.Contains(out, "{{ .web_http_auth_hash_admin }}") {
		t.Errorf("streamed recipe should not lift the hash into an input:\n%s", out)
	}
	for _, w := range res.Report.Warnings {
		if strings.Contains(w, "http-auth") {
			t.Errorf("nothing needs supplying by hand any more, got warning %q", w)
		}
	}
}

// TestExportHttpAuthUserInlineRedactLiftsAndWarns is the one combination with
// nowhere to put the value: stdout has no vars-file, and blanking the hash
// would emit a task that fails its own Validate. It is lifted into a required
// input instead, and the caller is told the values still have to be supplied
// (#334).
func TestExportHttpAuthUserInlineRedactLiftsAndWarns(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthExportFixture("admin", "admin:"+exportHttpAuthHash+"\n")))

	res, err := ExportRecipe(ctx, ExportOptions{Inline: true, Redact: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if len(res.Vars) != 0 {
		t.Errorf("inline mode must not populate the vars map, got %v", res.Vars)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	if strings.Contains(out, exportHttpAuthHash) {
		t.Errorf("redacted recipe leaked the hash:\n%s", out)
	}
	for _, want := range []string{
		"{{ .web_http_auth_hash_admin }}",
		"name: web_http_auth_hash_admin",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("redacted stdout recipe missing %q:\n%s", want, out)
		}
	}
	found := false
	for _, w := range res.Report.Warnings {
		if strings.Contains(w, "http-auth") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a warning that the hashes must be supplied, got %v", res.Report.Warnings)
	}
}

func TestAppCountExcludesGlobalPlay(t *testing.T) {
	t.Parallel()
	// #345: the leading global play is not an app, so AppCount excludes it.
	responses := exportFixture()
	responses["--quiet plugin:list --format json"] = `[{"name":"redis","core":false,"source_url":"https://github.com/dokku/dokku-redis.git","committish":"c0ffee","branch":"master"}]`
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(responses))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if got := res.PlayCount(); got != 3 {
		t.Errorf("PlayCount = %d, want 3 (global + 2 apps)", got)
	}
	if got := res.AppCount(); got != 2 {
		t.Errorf("AppCount = %d, want 2 (excludes the global play)", got)
	}
}

func TestExportMissingAppsRecorded(t *testing.T) {
	t.Parallel()
	// #346: a nonexistent --app is recorded, not silently dropped, and the
	// existing app still exports.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	res, err := ExportRecipe(ctx, ExportOptions{Apps: []string{"app-one", "nope"}})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if len(res.Report.MissingApps) != 1 || res.Report.MissingApps[0] != "nope" {
		t.Errorf("MissingApps = %v, want [nope]", res.Report.MissingApps)
	}
	if res.AppCount() != 1 {
		t.Errorf("AppCount = %d, want 1 (app-one still exported)", res.AppCount())
	}
}

func TestExportGlobalPropertiesReadsGlobalScope(t *testing.T) {
	t.Parallel()
	// #327: exportGlobalProperties reads the --global report and emits the global
	// form of each set property (both global-only and dual-scope keys), skipping
	// empty values and per-app-only keys.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet git:report --global --format json": `{"global-deploy-branch":"main","global-archive-max-files":"100","global-keep-git-dir":""}`,
	}))

	bodies, err := exportGlobalProperties(ctx, GitPropertyTask{}, func(property, value string) interface{} {
		return GitPropertyTask{Global: true, Property: property, Value: value}
	})
	if err != nil {
		t.Fatalf("exportGlobalProperties: %v", err)
	}
	got := map[string]string{}
	for _, b := range bodies {
		p := b.(GitPropertyTask)
		if !p.Global {
			t.Errorf("expected Global:true for %q", p.Property)
		}
		got[p.Property] = p.Value
	}
	if got["deploy-branch"] != "main" {
		t.Errorf("deploy-branch = %q, want main", got["deploy-branch"])
	}
	if got["archive-max-files"] != "100" {
		t.Errorf("archive-max-files = %q, want 100", got["archive-max-files"])
	}
	if _, ok := got["keep-git-dir"]; ok {
		t.Errorf("keep-git-dir has an empty value and must be skipped")
	}
}

// TestExportLetsencryptDynamicPropertiesFromAppReport covers the export half of
// #449: dns-provider-* rows cannot be enumerated in the key map, so they are
// lifted straight out of the report. The app scope also carries the global and
// computed variants of a key, and only the bare row is the app's own value.
func TestExportLetsencryptDynamicPropertiesFromAppReport(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet letsencrypt:report web --format json": `{"email":"admin@example.com","dns-provider":"namecheap","dns-provider-NAMECHEAP_API_USER":"deploy-bot","global-dns-provider-NAMECHEAP_API_KEY":"globalkey","computed-dns-provider-NAMECHEAP_API_USER":"deploy-bot"}`,
	}))

	bodies, err := exportProperties(ctx, LetsencryptPropertyTask{}, "web", func(app, property, value string) interface{} {
		return LetsencryptPropertyTask{App: app, Property: property, Value: value}
	})
	if err != nil {
		t.Fatalf("exportProperties: %v", err)
	}
	got := map[string]string{}
	for _, b := range bodies {
		p := b.(LetsencryptPropertyTask)
		got[p.Property] = p.Value
	}
	if got["dns-provider-NAMECHEAP_API_USER"] != "deploy-bot" {
		t.Errorf("dns-provider-NAMECHEAP_API_USER = %q, want deploy-bot", got["dns-provider-NAMECHEAP_API_USER"])
	}
	if got["email"] != "admin@example.com" || got["dns-provider"] != "namecheap" {
		t.Errorf("mapped properties should still export, got %v", got)
	}
	for _, unwanted := range []string{"global-dns-provider-NAMECHEAP_API_KEY", "dns-provider-NAMECHEAP_API_KEY", "computed-dns-provider-NAMECHEAP_API_USER"} {
		if _, ok := got[unwanted]; ok {
			t.Errorf("%q is not the app's own value and must not export", unwanted)
		}
	}
}

func TestExportGlobalLetsencryptDynamicProperties(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet letsencrypt:report --global --format json": `{"global-email":"","global-dns-provider":"namecheap","global-dns-provider-NAMECHEAP_API_KEY":"globalkey"}`,
	}))

	bodies, err := exportGlobalProperties(ctx, LetsencryptPropertyTask{}, func(property, value string) interface{} {
		return LetsencryptPropertyTask{Global: true, Property: property, Value: value}
	})
	if err != nil {
		t.Fatalf("exportGlobalProperties: %v", err)
	}
	got := map[string]string{}
	for _, b := range bodies {
		p := b.(LetsencryptPropertyTask)
		if !p.Global {
			t.Errorf("expected Global:true for %q", p.Property)
		}
		got[p.Property] = p.Value
	}
	if got["dns-provider-NAMECHEAP_API_KEY"] != "globalkey" {
		t.Errorf("dns-provider-NAMECHEAP_API_KEY = %q, want globalkey", got["dns-provider-NAMECHEAP_API_KEY"])
	}
	if _, ok := got["email"]; ok {
		t.Error("an empty global property must be skipped")
	}
}

// TestExportGlobalLetsencryptCredentialLiftedAsSensitiveInput proves the newly
// exported credential never lands in the recipe in cleartext, and that the input
// it is lifted into is named after the property rather than the `value` field
// (#451). The benign properties alongside it are not secrets and stay inline, so
// an exported recipe still describes the letsencrypt configuration and only asks
// the operator for the credential.
func TestExportGlobalLetsencryptCredentialLiftedAsSensitiveInput(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list": "",
		"--quiet letsencrypt:report --global --format json": `{"global-email":"admin@example.com","global-dns-provider":"namecheap","global-dns-provider-NAMECHEAP_API_KEY":"s3cr3tkey"}`,
	}))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if got := res.Vars["global_dns_provider_NAMECHEAP_API_KEY"]; got != "s3cr3tkey" {
		t.Errorf("vars[global_dns_provider_NAMECHEAP_API_KEY] = %q, want the credential lifted (vars: %v)", got, res.Vars)
	}
	if len(res.Vars) != 1 {
		t.Errorf("only the credential should be lifted, got %v", res.Vars)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	if strings.Contains(out, "s3cr3tkey") {
		t.Errorf("recipe leaked the dns provider credential:\n%s", out)
	}
	for _, want := range []string{
		"dokku_letsencrypt_property",
		"dns-provider-NAMECHEAP_API_KEY",
		"{{ .global_dns_provider_NAMECHEAP_API_KEY }}",
		"sensitive: true",
		"admin@example.com", // a benign property stays inline
		"namecheap",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
}

// TestExportLetsencryptBenignPropertyNotLifted keeps the app scope honest: only
// the credential family is a secret, so a plain property must not turn into a
// required input the operator has to re-supply on every apply (#451).
func TestExportLetsencryptBenignPropertyNotLifted(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list":                            "web",
		"--quiet letsencrypt:report web --format json": `{"email":"admin@example.com","dns-provider-CLOUDFLARE_API_TOKEN":"tok3n"}`,
	}))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if got := res.Vars["web_dns_provider_CLOUDFLARE_API_TOKEN"]; got != "tok3n" {
		t.Errorf("vars[web_dns_provider_CLOUDFLARE_API_TOKEN] = %q, want the credential lifted (vars: %v)", got, res.Vars)
	}
	for name, value := range res.Vars {
		if value == "admin@example.com" {
			t.Errorf("the email is not a secret and must not be lifted into input %q", name)
		}
	}
	recipe, _ := res.MarshalRecipe("yaml")
	if out := string(recipe); !strings.Contains(out, "admin@example.com") {
		t.Errorf("expected the email inline in the recipe:\n%s", out)
	}
}

func TestExportGlobalK3sTokenLiftedAsSensitiveInput(t *testing.T) {
	t.Parallel()
	// #327: the scheduler-k3s global token is core bootstrap state and must be
	// exported - but as a secret, lifted into a sensitive input, never inline.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list": "",
		"--quiet scheduler-k3s:report --global --format json": `{"global-token":"s3cr3ttoken","global-ingress-class":"nginx-ingress"}`,
	}))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if got := res.Vars["global_token"]; got != "s3cr3ttoken" {
		t.Errorf("vars[global_token] = %q, want the token lifted", got)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	if strings.Contains(out, "s3cr3ttoken") {
		t.Errorf("recipe leaked the k3s cluster token:\n%s", out)
	}
	for _, want := range []string{
		"dokku_scheduler_k3s_property",
		"global: true",
		"{{ .global_token }}",
		"name: global_token",
		"sensitive: true",
		"nginx-ingress", // a non-sensitive global stays inline
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
}

func TestExportGlobalTraefikPasswordLiftedAsSensitiveInput(t *testing.T) {
	t.Parallel()
	// #327: the traefik global basic-auth password is also a secret and must be
	// lifted rather than emitted in cleartext.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list": "",
		"--quiet traefik:report --global --format json": `{"global-basic-auth-password":"hunter2"}`,
	}))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if got := res.Vars["global_basic_auth_password"]; got != "hunter2" {
		t.Errorf("vars[global_basic_auth_password] = %q, want the password lifted", got)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	if strings.Contains(out, "hunter2") {
		t.Errorf("recipe leaked the traefik basic-auth password:\n%s", out)
	}
	for _, want := range []string{
		"dokku_traefik_property",
		"{{ .global_basic_auth_password }}",
		"sensitive: true",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
}

// TestExportGlobalTraefikDynamicProperties covers the export half of #450:
// dokku 0.38.27 reports every set `dns-provider-<KEY>` credential as a `global-`
// row, so the exporter lifts them straight out of the payload instead of
// dropping the whole family.
func TestExportGlobalTraefikDynamicProperties(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet traefik:report --global --format json": `{"global-log-level":"","global-dns-provider":"cloudflare","global-dns-provider-CLOUDFLARE_API_TOKEN":"globaltoken"}`,
	}))

	bodies, err := exportGlobalProperties(ctx, TraefikPropertyTask{}, func(property, value string) interface{} {
		return TraefikPropertyTask{Global: true, Property: property, Value: value}
	})
	if err != nil {
		t.Fatalf("exportGlobalProperties: %v", err)
	}
	got := map[string]string{}
	for _, b := range bodies {
		p := b.(TraefikPropertyTask)
		if !p.Global {
			t.Errorf("expected Global:true for %q", p.Property)
		}
		got[p.Property] = p.Value
	}
	if got["dns-provider-CLOUDFLARE_API_TOKEN"] != "globaltoken" {
		t.Errorf("dns-provider-CLOUDFLARE_API_TOKEN = %q, want globaltoken", got["dns-provider-CLOUDFLARE_API_TOKEN"])
	}
	if got["dns-provider"] != "cloudflare" {
		t.Errorf("mapped properties should still export, got %v", got)
	}
	if _, ok := got["log-level"]; ok {
		t.Error("an empty global property must be skipped")
	}
}

// TestExportTraefikDynamicPropertiesSkipAppScope pins the global-only half. A
// traefik report is global state whichever scope it is asked for, so the app
// payload carries the same `global-dns-provider-*` rows; none of them are the
// app's own value, and the family synthesizes no per-app key to lift them with.
func TestExportTraefikDynamicPropertiesSkipAppScope(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet traefik:report web --format json": `{"global-dns-provider-CLOUDFLARE_API_TOKEN":"globaltoken","computed-dns-provider":"cloudflare"}`,
	}))

	bodies, err := exportProperties(ctx, TraefikPropertyTask{}, "web", func(app, property, value string) interface{} {
		return TraefikPropertyTask{App: app, Property: property, Value: value}
	})
	if err != nil {
		t.Fatalf("exportProperties: %v", err)
	}
	if len(bodies) != 0 {
		t.Errorf("a global-only family must export nothing per app, got %+v", bodies)
	}
}

// TestExportGlobalTraefikCredentialLiftedAsSensitiveInput proves the newly
// exported credential never lands in the recipe in cleartext. Unlike the
// letsencrypt property task, this one is not built from
// SensitivePropertyFields, so nothing but the family's Sensitive mark says the
// value is a secret - which is exactly the coupling #457 broke.
func TestExportGlobalTraefikCredentialLiftedAsSensitiveInput(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list": "",
		"--quiet traefik:report --global --format json": `{"global-dns-provider-CLOUDFLARE_API_TOKEN":"cf-s3cr3t"}`,
	}))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if got := res.Vars["global_dns_provider_CLOUDFLARE_API_TOKEN"]; got != "cf-s3cr3t" {
		t.Errorf("vars[global_dns_provider_CLOUDFLARE_API_TOKEN] = %q, want the credential lifted (%v)", got, res.Vars)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	if strings.Contains(out, "cf-s3cr3t") {
		t.Errorf("recipe leaked the traefik dns provider credential:\n%s", out)
	}
	for _, want := range []string{
		"dokku_traefik_property",
		"dns-provider-CLOUDFLARE_API_TOKEN",
		"{{ .global_dns_provider_CLOUDFLARE_API_TOKEN }}",
		"sensitive: true",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
}

// TestExportGlobalDynamicCredentialsCollideDeterministically covers a case only
// reachable now that both families export: the same provider env var name set
// under letsencrypt and under traefik wants the same input name. Export order
// is fixed, so the second one is suffixed rather than overwriting the first, and
// both values survive.
func TestExportGlobalDynamicCredentialsCollideDeterministically(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list": "",
		"--quiet letsencrypt:report --global --format json": `{"global-dns-provider-CLOUDFLARE_API_TOKEN":"le-token"}`,
		"--quiet traefik:report --global --format json":     `{"global-dns-provider-CLOUDFLARE_API_TOKEN":"traefik-token"}`,
	}))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	values := map[string]bool{}
	for _, value := range res.Vars {
		values[value] = true
	}
	for _, want := range []string{"le-token", "traefik-token"} {
		if !values[want] {
			t.Errorf("expected %q lifted into its own input, got %v", want, res.Vars)
		}
	}
	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	for _, unwanted := range []string{"le-token", "traefik-token"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("recipe leaked %q:\n%s", unwanted, out)
		}
	}
}

func TestExportGlobalCertDisabledEmitsNoTask(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list": "",
		"--quiet global-cert:report --global --global-cert-enabled": "false",
	}))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	if res.HasVars() {
		t.Errorf("expected no lifted vars when no global cert is set, got %v", res.Vars)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	if strings.Contains(string(recipe), "dokku_certs") {
		t.Errorf("recipe should not contain a certs task when the global cert is disabled:\n%s", recipe)
	}
}

func TestExportPortsUsesStateSet(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet ports:report web --ports-map-json": `[{"container_port":5000,"host_port":443,"scheme":"https"},{"container_port":5000,"host_port":80,"scheme":"http"}]`,
	}))

	// state:set replaces the whole mapping list, so re-applying an export
	// converges an app that has extra mappings rather than adding to them.
	bodies, err := PortsTask{}.ExportApp(ctx, "web")
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 exported task, got %d", len(bodies))
	}
	ports := bodies[0].(PortsTask)
	if ports.State != StateSet {
		t.Errorf("State = %q, want %q", ports.State, StateSet)
	}
	// The probe hands back a map, so the export sorts for a stable recipe.
	want := []string{"http:80:5000", "https:443:5000"}
	got := []string{}
	for _, pm := range ports.PortMappings {
		got = append(got, pm.String())
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PortMappings = %v, want %v", got, want)
	}
	if err := ports.Validate(); err != nil {
		t.Errorf("exported ports task must be valid, got: %v", err)
	}
}

func TestExportPortsEmitsNoTaskWithoutMappings(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet ports:report web --ports-map-json": "null",
	}))

	bodies, err := PortsTask{}.ExportApp(ctx, "web")
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 0 {
		t.Errorf("expected no exported task when the app has no mappings, got %v", bodies)
	}
}

// TestMarshalRecipeJSON5 covers the format argument that every other
// MarshalRecipe test passes as the literal "yaml": the JSON5 arm was only
// ever reached end-to-end through `docket export`, so a codec-level
// regression had nowhere to surface in this package.
//
// The assertions are about shape rather than an exact document: a
// top-level array, unquoted identifier keys and a task type in the body,
// none of which YAML output would satisfy.
func TestMarshalRecipeJSON5(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	recipe, err := res.MarshalRecipe(FormatNameJSON5)
	if err != nil {
		t.Fatalf("MarshalRecipe(json5): %v", err)
	}
	out := string(recipe)

	if !strings.HasPrefix(out, "[") {
		t.Errorf("JSON5 recipe should open with a top-level array, got:\n%s", out)
	}
	if strings.HasPrefix(out, "---") {
		t.Error("JSON5 recipe must not carry the YAML document marker")
	}
	if !strings.Contains(out, "dokku_app") {
		t.Errorf("JSON5 recipe should name the app task type, got:\n%s", out)
	}

	// What Marshal emits is already canonical, and it reads back as a
	// recipe - the two properties `docket export | docket apply` needs.
	formatted, err := FormatJSON5(recipe)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	if string(formatted) != out {
		t.Errorf("MarshalRecipe(json5) output is not canonical:\ngot:\n%s\nformatted:\n%s", out, formatted)
	}
	if _, err := UnmarshalRecipe(recipe, FormatNameJSON5); err != nil {
		t.Fatalf("UnmarshalRecipe of the JSON5 export: %v", err)
	}

	// The YAML and JSON5 renderings must describe the same recipe.
	yamlRecipe, err := res.MarshalRecipe(FormatYAML)
	if err != nil {
		t.Fatalf("MarshalRecipe(yaml): %v", err)
	}
	fromYAML, err := UnmarshalRecipe(yamlRecipe, FormatYAML)
	if err != nil {
		t.Fatalf("UnmarshalRecipe(yaml): %v", err)
	}
	fromJSON5, err := UnmarshalRecipe(recipe, FormatNameJSON5)
	if err != nil {
		t.Fatalf("UnmarshalRecipe(json5): %v", err)
	}
	if len(fromYAML) != len(fromJSON5) {
		t.Errorf("YAML export = %d plays, JSON5 export = %d; the two renderings must agree", len(fromYAML), len(fromJSON5))
	}
}

// TestMarshalVarsMatchesTheCodec pins the vars-file side of the same
// dispatch. YAML gets a plain mapping; JSON5 gets indented JSON, which is
// valid YAML too - which is why a .yml vars-file holding JSON still loads
// and why recipeOutputFormatMismatch deliberately says nothing about it.
func TestMarshalVarsMatchesTheCodec(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	res, err := ExportRecipe(ctx, ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if !res.HasVars() {
		t.Fatal("export fixture lifted no vars; nothing to marshal")
	}

	asYAML, err := res.MarshalVars(FormatYAML)
	if err != nil {
		t.Fatalf("MarshalVars(yaml): %v", err)
	}
	if strings.HasPrefix(strings.TrimSpace(string(asYAML)), "{") {
		t.Errorf("YAML vars-file should be a plain mapping, got:\n%s", asYAML)
	}

	asJSON5, err := res.MarshalVars(FormatNameJSON5)
	if err != nil {
		t.Fatalf("MarshalVars(json5): %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(asJSON5)), "{") {
		t.Errorf("JSON5 vars-file should be a JSON object, got:\n%s", asJSON5)
	}

	// Both spellings must read back to the same values.
	fromYAML, err := CodecFor(FormatYAML).UnmarshalVars(asYAML)
	if err != nil {
		t.Fatalf("UnmarshalVars(yaml): %v", err)
	}
	fromJSON5, err := CodecFor(FormatNameJSON5).UnmarshalVars(asJSON5)
	if err != nil {
		t.Fatalf("UnmarshalVars(json5): %v", err)
	}
	if !reflect.DeepEqual(fromYAML, fromJSON5) {
		t.Errorf("vars round-trip differs by codec:\nyaml:  %#v\njson5: %#v", fromYAML, fromJSON5)
	}
}
