package tasks

import (
	"encoding/base64"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// registeredSensitive reports whether the export collected want. It asserts on
// ExportResult.SensitiveValues() rather than on the process-wide mask registry
// because export does not populate that registry itself: commands/export.go
// does, once, from this set (#488). Keeping the collection out of the global
// also keeps every other ExportRecipe test in this package free of leftovers.
func registeredSensitive(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func assertRegistered(t *testing.T, res *ExportResult, want string) {
	t.Helper()
	if !registeredSensitive(res.SensitiveValues(), want) {
		t.Errorf("expected %q to be collected for masking, got %v", want, res.SensitiveValues())
	}
}

func assertNotRegistered(t *testing.T, res *ExportResult, unwanted string) {
	t.Helper()
	if registeredSensitive(res.SensitiveValues(), unwanted) {
		t.Errorf("%q is not a secret and must not be collected for masking, got %v", unwanted, res.SensitiveValues())
	}
}

// TestExportRegistersConfigValues is the base case: every config value is an
// opaque secret, so an export that reads one has something to mask by the time
// it prints a warning. The base64 spelling comes along because that is how
// dokku_config declares its own sensitive set, and it is the spelling
// `config:set --encoded` puts on argv.
func TestExportRegistersConfigValues(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	res, err := ExportRecipe(ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	assertRegistered(t, res, "s3cr3t")
	assertRegistered(t, res, base64.StdEncoding.EncodeToString([]byte("s3cr3t")))
}

// TestExportRedactStillRegistersTheRealValue is the case that makes reading
// res.Vars after the fact wrong: --redact writes a placeholder there, but the
// value was still read off the server and can still reach a warning.
func TestExportRedactStillRegistersTheRealValue(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	res, err := ExportRecipe(ExportOptions{Redact: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if got, ok := res.Vars["app_one_SECRET_KEY"]; !ok || got != "" {
		t.Fatalf("vars[app_one_SECRET_KEY] = %q (present %v), want a blanked placeholder", got, ok)
	}
	assertRegistered(t, res, "s3cr3t")
}

// TestExportInlineRegistersValuesItDoesNotLift is the other half: inline mode
// lifts nothing into a vars map at all, yet its warnings print to the same
// stream as everything else.
func TestExportInlineRegistersValuesItDoesNotLift(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	res, err := ExportRecipe(ExportOptions{Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if res.HasVars() {
		t.Fatalf("inline mode must lift nothing, got %v", res.Vars)
	}
	assertRegistered(t, res, "s3cr3t")
}

// TestExportRegistersSensitivePropertyValue covers the secret no struct tag
// expresses: scheduler-k3s is built from PropertyFields, so only the property
// family's Sensitive flag says the cluster token is credential material. Its
// benign sibling in the same report is the boundary.
func TestExportRegistersSensitivePropertyValue(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet apps:list": "",
		"--quiet scheduler-k3s:report --global --format json": `{"global-token":"s3cr3ttoken","global-ingress-class":"nginx-ingress"}`,
	}))()

	res, err := ExportRecipe(ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	assertRegistered(t, res, "s3cr3ttoken")
	assertNotRegistered(t, res, "nginx-ingress")
}

// TestExportRegistersHttpAuthHashes covers the secrets the tag walker cannot
// reach on its own - they live in a slice of structs, so HttpAuthUserTask
// declares them through SensitiveValues(). Inline mode is the interesting one:
// the hash stays in the streamed body and is lifted nowhere.
func TestExportRegistersHttpAuthHashes(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(httpAuthExportFixture("admin", "admin:"+exportHttpAuthHash+"\n")))()

	res, err := ExportRecipe(ExportOptions{Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	assertRegistered(t, res, exportHttpAuthHash)
}

// TestExportRegistersEveryLetsencryptPropertyValue pins deliberate
// over-registration. letsencrypt is the one property task built from
// SensitivePropertyFields, so its `value` carries `sensitive:"true"` and a
// benign property registers alongside the credential - even though export
// leaves that one inline rather than lifting it
// (TestExportLetsencryptBenignPropertyNotLifted). That is exactly what apply
// masks for the same task, and over-masking is the safe direction.
func TestExportRegistersEveryLetsencryptPropertyValue(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet apps:list":                            "web",
		"--quiet letsencrypt:report web --format json": `{"email":"admin@example.com","dns-provider-CLOUDFLARE_API_TOKEN":"tok3n"}`,
	}))()

	res, err := ExportRecipe(ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	assertRegistered(t, res, "tok3n")
	assertRegistered(t, res, "admin@example.com")
}

// TestExportRegistersNothingForBenignProperties is the boundary the rest of the
// file leans on: a property plugin built from PropertyFields whose family is
// not Sensitive contributes nothing, so an export of a server holding no
// secrets masks nothing and its diagnostics read exactly as they did before.
func TestExportRegistersNothingForBenignProperties(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet apps:list":                         "",
		"--quiet git:report --global --format json": `{"global-deploy-branch":"main","global-archive-max-files":"100","global-keep-git-dir":""}`,
	}))()

	res, err := ExportRecipe(ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if got := res.SensitiveValues(); len(got) != 0 {
		t.Errorf("a benign export must collect nothing, got %v", got)
	}
}
