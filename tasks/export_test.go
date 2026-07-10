package tasks

import (
	"context"
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
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet plugin:list --format json": `[
			{"name":"00_dokku-standard","core":true,"source_url":"","committish":"","branch":""},
			{"name":"redis","core":false,"source_url":"https://github.com/dokku/dokku-redis.git","committish":"c0ffee1234","branch":"master"},
			{"name":"acl","core":false,"source_url":"https://github.com/dokku/dokku-acl.git","committish":"def4567890abcdef","branch":""},
			{"name":"tarball-plugin","core":false,"source_url":"","committish":"","branch":""}
		]`,
	}))()

	res, err := ExportRecipe(ExportOptions{})
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
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	res, err := ExportRecipe(ExportOptions{})
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

func TestExportHttpAuthUserPasswordsBecomeInputs(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet apps:list":                  "web",
		"http-auth:report web --format json": `{"enabled":"true","users":"admin","allowed-ips":"","domains":""}`,
	}))()

	res, err := ExportRecipe(ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	// The password is not readable, so it is lifted to a required input with an
	// empty placeholder in the vars map.
	v, ok := res.Vars["web_http_auth_password_admin"]
	if !ok || v != "" {
		t.Errorf("expected empty placeholder for http-auth password, got %q (ok=%v)", v, ok)
	}

	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	for _, want := range []string{
		"username: admin",
		"{{ .web_http_auth_password_admin }}",
		"name: web_http_auth_password_admin",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
}

func TestExportMaintenanceCustomPageBecomesInput(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet apps:list":                    "web",
		"maintenance:report web --format json": `{"enabled":"false","custom-page-sha256":"7b645f273842a941c68302a4022ed03e219bd8db318ef32a92dddb148a72ef05"}`,
	}))()

	res, err := ExportRecipe(ExportOptions{})
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
	for _, tc := range []struct {
		name   string
		report string
	}{
		{"set but empty", `{"enabled":"false","custom-page-sha256":""}`},
		{"key absent (old plugin)", `{"enabled":"false"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer subprocess.SetExecRunner(fakeDokku(map[string]string{
				"--quiet apps:list":                    "web",
				"maintenance:report web --format json": tc.report,
			}))()

			res, err := ExportRecipe(ExportOptions{})
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
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	res, err := ExportRecipe(ExportOptions{Redact: true})
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
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	res, err := ExportRecipe(ExportOptions{Inline: true})
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
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	res, err := ExportRecipe(ExportOptions{Apps: []string{"app-two"}})
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
	certPEM := "-----BEGIN CERTIFICATE-----\nMIIFAKECERT\n-----END CERTIFICATE-----"
	keyPEM := "-----BEGIN PRIVATE KEY-----\nMIIFAKEKEY\n-----END PRIVATE KEY-----"
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet apps:list": "",
		"--quiet global-cert:report --global --global-cert-enabled": "true",
		"--quiet global-cert:show crt":                              certPEM,
		"--quiet global-cert:show key":                              keyPEM,
	}))()

	res, err := ExportRecipe(ExportOptions{})
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

func TestExportGlobalCertDisabledEmitsNoTask(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet apps:list": "",
		"--quiet global-cert:report --global --global-cert-enabled": "false",
	}))()

	res, err := ExportRecipe(ExportOptions{})
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
