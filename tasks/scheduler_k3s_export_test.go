package tasks

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestParseSchedulerK3sAutoscalingAuthReport(t *testing.T) {
	t.Run("dot format grouped by trigger", func(t *testing.T) {
		raw := []byte(`{"datadog.apiKey":"secret-1","datadog.appKey":"secret-2"}`)
		md, err := parseSchedulerK3sAutoscalingAuthReport(raw, "datadog")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if md["apiKey"] != "secret-1" || md["appKey"] != "secret-2" {
			t.Errorf("metadata mismatch: %+v", md)
		}
		if len(md) != 2 {
			t.Errorf("expected 2 keys, got %d: %+v", len(md), md)
		}
	})

	t.Run("only the requested trigger is returned", func(t *testing.T) {
		raw := []byte(`{"datadog.apiKey":"a","aws-secret-manager.awsRegion":"us-east-1"}`)
		md, err := parseSchedulerK3sAutoscalingAuthReport(raw, "datadog")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(md) != 1 || md["apiKey"] != "a" {
			t.Errorf("expected only the datadog apiKey, got %+v", md)
		}
	})

	t.Run("metadata key may contain dots", func(t *testing.T) {
		raw := []byte(`{"datadog.some.dotted.key":"v"}`)
		md, err := parseSchedulerK3sAutoscalingAuthReport(raw, "datadog")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if md["some.dotted.key"] != "v" {
			t.Errorf("expected dotted key preserved, got %+v", md)
		}
	})

	t.Run("empty payload yields empty metadata", func(t *testing.T) {
		md, err := parseSchedulerK3sAutoscalingAuthReport([]byte(`{}`), "datadog")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(md) != 0 {
			t.Errorf("expected no metadata, got %+v", md)
		}
	})

	t.Run("malformed payload errors", func(t *testing.T) {
		_, err := parseSchedulerK3sAutoscalingAuthReport([]byte("not json"), "datadog")
		if err == nil {
			t.Fatal("expected parse error")
		}
		if !strings.Contains(err.Error(), "parse scheduler-k3s:autoscaling-auth:report json") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func annotationScope(bodies []interface{}, processType, resourceType string) *SchedulerK3sAnnotationsTask {
	for i := range bodies {
		if a, ok := bodies[i].(SchedulerK3sAnnotationsTask); ok && a.ProcessType == processType && a.ResourceType == resourceType {
			return &a
		}
	}
	return nil
}

func TestSchedulerK3sAnnotationsExportApp(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet scheduler-k3s:annotations:report myapp --format json": `{
			"global.deployment.prometheus.io/scrape":"true",
			"global.deployment.prometheus.io/port":"9090",
			"web.ingress.nginx.ingress.kubernetes.io/rewrite-target":"/"
		}`,
	}))()

	bodies, err := SchedulerK3sAnnotationsTask{}.ExportApp("myapp")
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 scope bodies, got %d: %+v", len(bodies), bodies)
	}

	// Deterministic order: sorted by resourceType then processType, so the
	// deployment scope precedes the ingress scope.
	first, ok := bodies[0].(SchedulerK3sAnnotationsTask)
	if !ok || first.ResourceType != "deployment" {
		t.Fatalf("expected deployment scope first, got %+v", bodies[0])
	}

	// global process scope maps back to an empty ProcessType, and dotted/slashed
	// annotation keys survive the split.
	dep := annotationScope(bodies, "", "deployment")
	if dep == nil {
		t.Fatalf("missing global/deployment scope: %+v", bodies)
	}
	if dep.App != "myapp" || dep.Global {
		t.Errorf("expected app scope, got App=%q Global=%v", dep.App, dep.Global)
	}
	if dep.Annotations["prometheus.io/scrape"] != "true" || dep.Annotations["prometheus.io/port"] != "9090" {
		t.Errorf("deployment annotations mismatch: %+v", dep.Annotations)
	}

	ing := annotationScope(bodies, "web", "ingress")
	if ing == nil {
		t.Fatalf("missing web/ingress scope: %+v", bodies)
	}
	if ing.Annotations["nginx.ingress.kubernetes.io/rewrite-target"] != "/" {
		t.Errorf("ingress annotations mismatch: %+v", ing.Annotations)
	}
}

func TestSchedulerK3sAnnotationsExportGlobal(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet scheduler-k3s:annotations:report --global --format json": `{"global.deployment.managed-by":"docket"}`,
	}))()

	bodies, err := SchedulerK3sAnnotationsTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 body, got %d: %+v", len(bodies), bodies)
	}
	got := bodies[0].(SchedulerK3sAnnotationsTask)
	if !got.Global || got.App != "" || got.ProcessType != "" || got.ResourceType != "deployment" {
		t.Errorf("unexpected global body: %+v", got)
	}
	if got.Annotations["managed-by"] != "docket" {
		t.Errorf("annotations mismatch: %+v", got.Annotations)
	}
}

func TestSchedulerK3sLabelsExportApp(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet scheduler-k3s:labels:report myapp --format json": `{"web.deployment.tier":"edge","web.deployment.app.kubernetes.io/component":"api"}`,
	}))()

	bodies, err := SchedulerK3sLabelsTask{}.ExportApp("myapp")
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 scope body, got %d: %+v", len(bodies), bodies)
	}
	got := bodies[0].(SchedulerK3sLabelsTask)
	if got.App != "myapp" || got.ProcessType != "web" || got.ResourceType != "deployment" {
		t.Errorf("unexpected labels body: %+v", got)
	}
	if got.Labels["tier"] != "edge" || got.Labels["app.kubernetes.io/component"] != "api" {
		t.Errorf("labels mismatch: %+v", got.Labels)
	}
}

func TestSchedulerK3sAutoscalingAuthExportApp(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet scheduler-k3s:autoscaling-auth:report myapp --format json": `{
			"datadog.apiKey":"secret-1",
			"aws-secret-manager.awsRegion":"us-east-1",
			"aws-secret-manager.secretName":"my-secret"
		}`,
	}))()

	bodies, err := SchedulerK3sAutoscalingAuthTask{}.ExportApp("myapp")
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 trigger bodies, got %d: %+v", len(bodies), bodies)
	}

	// Triggers are emitted in sorted order.
	first := bodies[0].(SchedulerK3sAutoscalingAuthTask)
	if first.Trigger != "aws-secret-manager" {
		t.Errorf("expected aws-secret-manager first, got %q", first.Trigger)
	}
	if first.App != "myapp" || first.Global {
		t.Errorf("expected app scope, got App=%q Global=%v", first.App, first.Global)
	}
	if first.Metadata["awsRegion"] != "us-east-1" || first.Metadata["secretName"] != "my-secret" {
		t.Errorf("aws metadata mismatch: %+v", first.Metadata)
	}

	second := bodies[1].(SchedulerK3sAutoscalingAuthTask)
	if second.Trigger != "datadog" || second.Metadata["apiKey"] != "secret-1" {
		t.Errorf("datadog body mismatch: %+v", second)
	}
}

func TestSchedulerK3sAutoscalingAuthExportGlobal(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet scheduler-k3s:autoscaling-auth:report --global --format json": `{"datadog.apiKey":"global-secret"}`,
	}))()

	bodies, err := SchedulerK3sAutoscalingAuthTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 body, got %d: %+v", len(bodies), bodies)
	}
	got := bodies[0].(SchedulerK3sAutoscalingAuthTask)
	if !got.Global || got.App != "" || got.Trigger != "datadog" {
		t.Errorf("unexpected global body: %+v", got)
	}
	if got.Metadata["apiKey"] != "global-secret" {
		t.Errorf("metadata mismatch: %+v", got.Metadata)
	}
}

func TestSchedulerK3sScopedPairsExportEmpty(t *testing.T) {
	// An app with no annotations returns "{}"; the exporter yields no bodies.
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet scheduler-k3s:annotations:report myapp --format json": `{}`,
	}))()

	bodies, err := SchedulerK3sAnnotationsTask{}.ExportApp("myapp")
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 0 {
		t.Errorf("expected no bodies, got %+v", bodies)
	}
}

// TestExportSchedulerK3sScopedAndAuthRecipe drives the full engine: annotations
// and labels are emitted inline, while trigger-auth metadata (a secret) is
// lifted into the vars map and never leaks into the recipe body.
func TestExportSchedulerK3sScopedAndAuthRecipe(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet apps:list": "web",
		"--quiet scheduler-k3s:annotations:report web --format json":      `{"global.deployment.prometheus.io/scrape":"true"}`,
		"--quiet scheduler-k3s:labels:report web --format json":           `{"web.deployment.tier":"edge"}`,
		"--quiet scheduler-k3s:autoscaling-auth:report web --format json": `{"datadog.apiKey":"s3cr3t-key"}`,
	}))()

	res, err := ExportRecipe(ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	// The trigger-auth secret is lifted into the vars map, keyed by trigger.
	if got := res.Vars["web_datadog_apiKey"]; got != "s3cr3t-key" {
		t.Errorf("vars[web_datadog_apiKey] = %q, want s3cr3t-key", got)
	}

	recipe, err := res.MarshalRecipe("yaml")
	if err != nil {
		t.Fatalf("MarshalRecipe: %v", err)
	}
	out := string(recipe)

	if strings.Contains(out, "s3cr3t-key") {
		t.Errorf("recipe leaked the trigger-auth secret:\n%s", out)
	}
	for _, want := range []string{
		"dokku_scheduler_k3s_annotations",
		"prometheus.io/scrape",
		"dokku_scheduler_k3s_labels",
		"tier: edge",
		"dokku_scheduler_k3s_autoscaling_auth",
		"trigger: datadog",
		"{{ .web_datadog_apiKey }}",
		"name: web_datadog_apiKey",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
}

// globalExportReporter is satisfied by SchedulerK3sProfileTask so the export
// engine prefers ExportGlobalReport and routes the left-out-profile diagnostic
// into ExportReport.Warnings rather than dropping it silently. #483.
var _ globalExportReporter = SchedulerK3sProfileTask{}

// schedulerK3sProfileExportFixture is a profiles:list payload holding one
// profile docket can express and three it cannot, one per rule the exported
// body has to satisfy: a name helm could not turn into a release because of its
// case, one it could not because of its length, and a profile dokku reports
// with no role at all. All three would come back as a recipe `docket validate`
// refuses, which fails the whole file rather than just the task.
func schedulerK3sProfileExportFixture() map[string]string {
	overLong := strings.Repeat("a", schedulerK3sProfileNameHelmMaxLength+1)
	return map[string]string{
		"--quiet apps:list": "",
		"--quiet scheduler-k3s:profiles:list --format json": `[
			{"name":"edge-pool","role":"worker","kubelet_args":["max-pods=64"],"taint_scheduling":true},
			{"name":"EdgePool","role":"worker"},
			{"name":"` + overLong + `","role":"worker"},
			{"name":"no-role","role":""}
		]`,
	}
}

// TestSchedulerK3sProfileExportReportLeavesOutUnappliableProfiles pins #483: a
// profile the server reports but the task would refuse is warned about and left
// out, and the profiles that survive carry an explicit state so the body is
// plannable straight out of the exporter.
func TestSchedulerK3sProfileExportReportLeavesOutUnappliableProfiles(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(schedulerK3sProfileExportFixture()))()

	var warnings []string
	bodies, err := SchedulerK3sProfileTask{}.ExportGlobalReport(func(msg string) {
		warnings = append(warnings, msg)
	})
	if err != nil {
		t.Fatalf("ExportGlobalReport: %v", err)
	}

	if len(bodies) != 1 {
		t.Fatalf("expected 1 body, got %d: %+v", len(bodies), bodies)
	}
	body, ok := bodies[0].(SchedulerK3sProfileTask)
	if !ok {
		t.Fatalf("body type = %T, want SchedulerK3sProfileTask", bodies[0])
	}
	if body.Name != "edge-pool" {
		t.Errorf("exported profile = %q, want edge-pool", body.Name)
	}
	if body.State != StatePresent {
		t.Errorf("exported state = %q, want %q", body.State, StatePresent)
	}
	// The emitted body is exactly what the loader would hand to plan, so it has
	// to satisfy the same check `docket validate` runs.
	if err := body.Validate(); err != nil {
		t.Errorf("exported body does not validate: %v", err)
	}

	if len(warnings) != 3 {
		t.Fatalf("expected 3 warnings, got %d (%v)", len(warnings), warnings)
	}
	// Every warning names its profile: the engine prefix says only which task
	// type spoke, and Validate()'s role message never mentions the name.
	for _, want := range []string{"EdgePool", "must be lowercase", "state 'absent'"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("uppercase warning = %q, want it to mention %q", warnings[0], want)
		}
	}
	if !strings.Contains(warnings[1], "at most 26 characters") {
		t.Errorf("over-long warning = %q, want the length rule", warnings[1])
	}
	if !strings.Contains(warnings[2], "no-role") || !strings.Contains(warnings[2], "role is required") {
		t.Errorf("missing-role warning = %q, want the profile name and the role rule", warnings[2])
	}
}

// TestSchedulerK3sProfileExportGlobalDropsWithoutDiagnostics pins that the
// plain GlobalExporter form leaves out the same profiles; it only discards the
// explanation, which is why the engine prefers ExportGlobalReport.
func TestSchedulerK3sProfileExportGlobalDropsWithoutDiagnostics(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(schedulerK3sProfileExportFixture()))()

	bodies, err := SchedulerK3sProfileTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 body, got %d: %+v", len(bodies), bodies)
	}
	if got := bodies[0].(SchedulerK3sProfileTask).Name; got != "edge-pool" {
		t.Errorf("exported profile = %q, want edge-pool", got)
	}
}

// TestExportSchedulerK3sProfileRecipeOmitsUnappliableProfiles drives the full
// engine: the warnings reach ExportReport.Warnings under the global prefix, and
// the recipe that comes back carries only the profile that can be applied.
func TestExportSchedulerK3sProfileRecipeOmitsUnappliableProfiles(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(schedulerK3sProfileExportFixture()))()

	res, err := ExportRecipe(ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	// The other global exporters answer the empty fixture with their own
	// unparseable-report warnings, so count only the ones this task produced -
	// which is also what pins the engine prefix onto them.
	var profileWarnings []string
	for _, w := range res.Report.Warnings {
		if strings.HasPrefix(w, "global: dokku_scheduler_k3s_profile: ") {
			profileWarnings = append(profileWarnings, w)
		}
	}
	if len(profileWarnings) != 3 {
		t.Fatalf("expected 3 profile warnings, got %d (%v)", len(profileWarnings), profileWarnings)
	}

	recipe, err := res.MarshalRecipe("yaml")
	if err != nil {
		t.Fatalf("MarshalRecipe: %v", err)
	}
	out := string(recipe)
	for _, want := range []string{"dokku_scheduler_k3s_profile", "name: edge-pool", "state: present"} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"EdgePool", "no-role"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("recipe carries the unappliable profile %q:\n%s", unwanted, out)
		}
	}
}

// TestExportSchedulerK3sProfileResourceAddressReportsAMissingProfile pins the
// consequence of leaving a profile out: an address pinning one matches nothing,
// so export reports it as missing and the command exits non-zero. The warning
// printed above it is what says the profile is on the server but unexportable.
func TestExportSchedulerK3sProfileResourceAddressReportsAMissingProfile(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(schedulerK3sProfileExportFixture()))()

	selectors, err := ParseResourceSelectors([]string{"dokku_scheduler_k3s_profile[name=EdgePool]"})
	if err != nil {
		t.Fatalf("ParseResourceSelectors: %v", err)
	}
	res, err := ExportRecipe(ExportOptions{Resources: selectors})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	if res.PlayCount() != 0 {
		t.Errorf("expected no plays, got %d", res.PlayCount())
	}
	want := []string{"dokku_scheduler_k3s_profile[name=EdgePool]"}
	if !reflect.DeepEqual(res.Report.MissingResources, want) {
		t.Errorf("MissingResources = %v, want %v", res.Report.MissingResources, want)
	}
	if len(res.Report.Warnings) == 0 {
		t.Fatal("expected the left-out warning to survive the empty play")
	}
	if !strings.Contains(res.Report.Warnings[0], "EdgePool") {
		t.Errorf("warning = %q, want it to name EdgePool", res.Report.Warnings[0])
	}
}
