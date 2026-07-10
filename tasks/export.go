package tasks

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
	yaml "gopkg.in/yaml.v3"
)

// AppExporter is implemented by a task type that can reconstruct its recipe
// representation for a single app from live server state. ExportApp returns
// zero or more task bodies (each the task's own struct, populated with real
// values); the engine wraps each under the task's type-key and applies
// vars-extraction/redaction uniformly afterwards.
type AppExporter interface {
	ExportApp(app string) ([]interface{}, error)
}

// GlobalExporter is the not-app-scoped counterpart of AppExporter (global
// certs, networks, ssh keys, ...). It is defined here so global resources can
// be added the same way; the engine runs these into a leading global play.
type GlobalExporter interface {
	ExportGlobal() ([]interface{}, error)
}

// globalExportOrder is the fixed order in which not-app-scoped task types are
// emitted into the leading global play. Adding a global resource means
// implementing GlobalExporter on its task and adding its type-key here.
var globalExportOrder = []string{
	// plugins first: installing a third-party plugin is a prerequisite for the
	// resources that follow.
	"dokku_plugin",
	// networks next: a foundational resource that app network attachments
	// (dokku_network_property, emitted in the app plays) bind to.
	"dokku_network",
	"dokku_ssh_key",
	// global SSL certificate: requires the dokku-global-cert plugin, installed
	// by the dokku_plugin tasks emitted first. dokku_certs also appears in
	// appExportOrder, where it emits the per-app scope.
	"dokku_certs",
	"dokku_storage_entry",
	"dokku_scheduler_k3s_profile",
	"dokku_scheduler_k3s_chart",
	// scheduler-k3s annotations/labels/trigger-auth can be set globally as well
	// as per-app; the global scope is emitted here and the per-app scope in
	// appExportOrder.
	"dokku_scheduler_k3s_annotations",
	"dokku_scheduler_k3s_labels",
	"dokku_scheduler_k3s_autoscaling_auth",
	// datastore services: create must precede expose/backup/acl, which all
	// operate on an existing service instance. The datastore plugins they rely
	// on are installed by the dokku_plugin tasks emitted first.
	"dokku_service_create",
	"dokku_service_expose",
	"dokku_service_backup",
	"dokku_acl_service",
}

// appExportOrder is the fixed order in which app-scoped task types are emitted
// into each app's play. dokku_app comes first; deploy sources are emitted last.
// Adding a task to export means implementing AppExporter on it and adding its
// type-key here.
var appExportOrder = []string{
	"dokku_app",
	"dokku_app_lock",
	"dokku_config",
	"dokku_domains",
	"dokku_ports",
	"dokku_docker_options",
	"dokku_buildpacks",
	"dokku_storage_mount",
	"dokku_ps_scale",
	"dokku_resource_limit",
	"dokku_resource_reserve",
	"dokku_checks_toggle",
	"dokku_proxy_toggle",
	"dokku_domains_toggle",
	"dokku_maintenance",
	"dokku_maintenance_custom_page",
	"dokku_http_auth_user",
	"dokku_http_auth_allowed_ip",
	"dokku_http_auth_domain",
	"dokku_acl_app",
	"dokku_certs",
	"dokku_letsencrypt",
	// property-plugin tasks (reconstructed from <plugin>:report by exportProperties)
	"dokku_app_json_property",
	"dokku_apps_property",
	"dokku_builder_property",
	"dokku_builder_dockerfile_property",
	"dokku_builder_herokuish_property",
	"dokku_builder_lambda_property",
	"dokku_builder_nixpacks_property",
	"dokku_builder_pack_property",
	"dokku_builder_railpack_property",
	"dokku_buildpacks_property",
	"dokku_builds_property",
	"dokku_caddy_property",
	"dokku_checks_property",
	"dokku_cron_property",
	"dokku_git_property",
	"dokku_haproxy_property",
	"dokku_letsencrypt_property",
	"dokku_logs_property",
	"dokku_network_property",
	"dokku_nginx_property",
	"dokku_openresty_property",
	"dokku_proxy_property",
	"dokku_ps_property",
	"dokku_registry_property",
	"dokku_scheduler_property",
	"dokku_scheduler_docker_local_property",
	"dokku_scheduler_k3s_property",
	// scheduler-k3s annotations/labels/trigger-auth also emit a global-scope
	// task in globalExportOrder; here they emit the per-app scope.
	"dokku_scheduler_k3s_annotations",
	"dokku_scheduler_k3s_labels",
	"dokku_scheduler_k3s_autoscaling_auth",
	"dokku_traefik_property",
	// service links bind an already-created datastore service (from the leading
	// global play) to the app.
	"dokku_service_link",
	// deploy source last: only one of these emits per app
	"dokku_git_sync",
	"dokku_git_from_image",
	"dokku_git_from_archive",
}

// ExportOptions controls an export run.
type ExportOptions struct {
	// Apps restricts the export to these app names; empty means every app.
	Apps []string

	// Redact replaces sensitive values with placeholders instead of the real
	// values (in the vars-file for file mode, in place for stdout mode).
	Redact bool

	// Inline keeps sensitive values in the task bodies instead of lifting them
	// into a vars map. Used for stdout output, which has no companion file.
	Inline bool
}

// ExportReport carries non-fatal diagnostics from an export run.
type ExportReport struct {
	Warnings []string
}

// ExportResult is the outcome of ExportRecipe: the assembled recipe (as a list
// of plays), the companion vars map (empty in inline mode), and any warnings.
type ExportResult struct {
	plays  []map[string]interface{}
	Vars   map[string]string
	Report ExportReport

	usedVarNames map[string]bool
}

// ExportRecipe reads the live Dokku server (via the current subprocess host)
// and assembles a recipe describing it. It enumerates apps, runs every
// registered AppExporter for each, lifts sensitive values into a vars map
// (unless opts.Inline), and returns the result for the caller to marshal.
func ExportRecipe(opts ExportOptions) (*ExportResult, error) {
	res := &ExportResult{
		Vars:         map[string]string{},
		usedVarNames: map[string]bool{},
	}

	// Global resources come first, in a leading "global" play. Skipped when
	// the export is narrowed to specific apps with --app.
	if len(opts.Apps) == 0 {
		if global := res.exportGlobalPlay(opts); global != nil {
			res.plays = append(res.plays, global)
		}
	}

	apps, err := listApps()
	if err != nil {
		return nil, err
	}
	apps = filterApps(apps, opts.Apps)
	sort.Strings(apps)

	for _, app := range apps {
		play := res.exportAppPlay(app, opts)
		if play != nil {
			res.plays = append(res.plays, play)
		}
	}

	return res, nil
}

// exportGlobalPlay builds the leading global play by running every registered
// GlobalExporter. Returns nil when there are no global resources.
func (res *ExportResult) exportGlobalPlay(opts ExportOptions) map[string]interface{} {
	var taskList []map[string]interface{}
	var inputs []map[string]interface{}

	for _, typeKey := range globalExportOrder {
		proto, ok := RegisteredTasks[typeKey]
		if !ok {
			continue
		}
		exporter, ok := proto.(GlobalExporter)
		if !ok {
			continue
		}
		bodies, err := exporter.ExportGlobal()
		if err != nil {
			res.Report.Warnings = append(res.Report.Warnings,
				fmt.Sprintf("global: %s: %v", typeKey, err))
			continue
		}
		for _, body := range bodies {
			body, ins := res.processBody("global", body, opts)
			taskList = append(taskList, map[string]interface{}{typeKey: body})
			inputs = append(inputs, ins...)
		}
	}

	if len(taskList) == 0 {
		return nil
	}

	play := map[string]interface{}{"name": "global"}
	if len(inputs) > 0 {
		play["inputs"] = inputs
	}
	play["tasks"] = taskList
	return play
}

// exportAppPlay builds one play for a single app by running each app-scoped
// exporter in appExportOrder. Returns nil when the app yields no tasks.
func (res *ExportResult) exportAppPlay(app string, opts ExportOptions) map[string]interface{} {
	var taskList []map[string]interface{}
	var inputs []map[string]interface{}

	for _, typeKey := range appExportOrder {
		proto, ok := RegisteredTasks[typeKey]
		if !ok {
			continue
		}
		exporter, ok := proto.(AppExporter)
		if !ok {
			continue
		}
		bodies, err := exporter.ExportApp(app)
		if err != nil {
			res.Report.Warnings = append(res.Report.Warnings,
				fmt.Sprintf("%s: %s: %v", app, typeKey, err))
			continue
		}
		for _, body := range bodies {
			body, ins := res.processBody(app, body, opts)
			taskList = append(taskList, map[string]interface{}{typeKey: body})
			inputs = append(inputs, ins...)
		}
	}

	if len(taskList) == 0 {
		return nil
	}

	play := map[string]interface{}{"name": app}
	if len(inputs) > 0 {
		play["inputs"] = inputs
	}
	play["tasks"] = taskList
	return play
}

// processBody applies vars-extraction (file mode) or redaction (inline mode) to
// a single task body and returns the possibly-rewritten body plus any input
// declarations the recipe should carry for lifted values.
func (res *ExportResult) processBody(app string, body interface{}, opts ExportOptions) (interface{}, []map[string]interface{}) {
	switch b := body.(type) {
	case ConfigTask:
		return res.processConfig(app, b, opts)
	case HttpAuthUserTask:
		return res.processHttpAuthUser(app, b, opts)
	case MaintenanceCustomPageTask:
		return res.processMaintenanceCustomPage(app, b, opts)
	case SchedulerK3sAutoscalingAuthTask:
		return res.processSchedulerK3sAutoscalingAuth(app, b, opts)
	default:
		return res.processSensitiveScalars(app, body, opts)
	}
}

// processSensitiveScalars lifts every non-empty string field tagged
// `sensitive:"true"` into a required, sensitive input (e.g. a git image or
// archive URL, which can embed credentials). Map/slice sensitive fields are
// handled by their own cases above.
func (res *ExportResult) processSensitiveScalars(app string, body interface{}, opts ExportOptions) (interface{}, []map[string]interface{}) {
	rv := reflect.ValueOf(body)
	if rv.Kind() != reflect.Struct {
		return body, nil
	}
	rt := rv.Type()
	out := reflect.New(rt).Elem()
	out.Set(rv)

	var inputs []map[string]interface{}
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.Tag.Get("sensitive") != "true" || field.Type.Kind() != reflect.String {
			continue
		}
		fv := out.Field(i)
		value := fv.String()
		if value == "" {
			continue
		}
		if opts.Inline {
			if opts.Redact {
				fv.SetString("")
			}
			continue
		}
		name := res.uniqueVarName(app, yamlFieldName(field))
		fv.SetString("{{ ." + name + " }}")
		if opts.Redact {
			res.Vars[name] = ""
		} else {
			res.Vars[name] = value
		}
		inputs = append(inputs, map[string]interface{}{
			"name":      name,
			"required":  true,
			"sensitive": true,
		})
	}
	if len(inputs) == 0 {
		return body, nil
	}
	return out.Interface(), inputs
}

// yamlFieldName returns the recipe key for a struct field from its yaml tag.
func yamlFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("yaml")
	if comma := strings.IndexByte(tag, ','); comma >= 0 {
		tag = tag[:comma]
	}
	if tag == "" || tag == "-" {
		return strings.ToLower(field.Name)
	}
	return tag
}

// processHttpAuthUser lifts each user's password into a required input, since
// http-auth:report never exposes password material. The recipe therefore always
// needs the passwords supplied in the vars-file before apply.
func (res *ExportResult) processHttpAuthUser(app string, b HttpAuthUserTask, opts ExportOptions) (interface{}, []map[string]interface{}) {
	if opts.Inline || len(b.Users) == 0 {
		return b, nil
	}
	var inputs []map[string]interface{}
	users := make([]HttpAuthUser, len(b.Users))
	for i, u := range b.Users {
		name := res.uniqueVarName(app, "http_auth_password_"+u.Username)
		u.Password = "{{ ." + name + " }}"
		users[i] = u
		res.Vars[name] = "" // password is not readable; the user fills this in
		inputs = append(inputs, map[string]interface{}{
			"name":      name,
			"required":  true,
			"sensitive": true,
		})
	}
	b.Users = users
	return b, inputs
}

// processMaintenanceCustomPage lifts the custom page HTML into a required input,
// since maintenance:report exposes only a checksum, never the page content. The
// recipe therefore always needs the HTML supplied in the vars-file before apply,
// so the value is blanked unconditionally (like http-auth passwords). Unlike a
// secret, the page is public HTML, so the input is not marked sensitive.
func (res *ExportResult) processMaintenanceCustomPage(app string, b MaintenanceCustomPageTask, opts ExportOptions) (interface{}, []map[string]interface{}) {
	if opts.Inline {
		return b, nil
	}
	name := res.uniqueVarName(app, "maintenance_custom_page")
	b.Content = "{{ ." + name + " }}"
	res.Vars[name] = "" // page HTML is not readable; the user fills this in
	inputs := []map[string]interface{}{{
		"name":     name,
		"required": true,
	}}
	return b, inputs
}

// processConfig lifts config values into the vars map (file mode) or blanks
// them (inline + redact), since every config value is treated as an opaque
// secret.
func (res *ExportResult) processConfig(app string, b ConfigTask, opts ExportOptions) (interface{}, []map[string]interface{}) {
	if len(b.Config) == 0 {
		return b, nil
	}

	keys := make([]string, 0, len(b.Config))
	for k := range b.Config {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	newConfig := make(map[string]string, len(b.Config))
	var inputs []map[string]interface{}

	for _, k := range keys {
		value := b.Config[k]
		if opts.Inline {
			if opts.Redact {
				value = ""
			}
			newConfig[k] = value
			continue
		}
		name := res.uniqueVarName(app, k)
		newConfig[k] = "{{ ." + name + " }}"
		if opts.Redact {
			res.Vars[name] = ""
		} else {
			res.Vars[name] = value
		}
		inputs = append(inputs, map[string]interface{}{
			"name":      name,
			"required":  true,
			"sensitive": true,
		})
	}

	b.Config = newConfig
	return b, inputs
}

// processSchedulerK3sAutoscalingAuth lifts each KEDA trigger authentication
// metadata value into the vars map (file mode) or blanks it (inline + redact),
// since trigger-auth metadata are credentials. This mirrors processConfig; the
// values are keyed by trigger and metadata key so they stay unique across
// triggers and scopes.
func (res *ExportResult) processSchedulerK3sAutoscalingAuth(app string, b SchedulerK3sAutoscalingAuthTask, opts ExportOptions) (interface{}, []map[string]interface{}) {
	if len(b.Metadata) == 0 {
		return b, nil
	}

	keys := make([]string, 0, len(b.Metadata))
	for k := range b.Metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	newMetadata := make(map[string]string, len(b.Metadata))
	var inputs []map[string]interface{}

	for _, k := range keys {
		value := b.Metadata[k]
		if opts.Inline {
			if opts.Redact {
				value = ""
			}
			newMetadata[k] = value
			continue
		}
		name := res.uniqueVarName(app, b.Trigger+"_"+k)
		newMetadata[k] = "{{ ." + name + " }}"
		if opts.Redact {
			res.Vars[name] = ""
		} else {
			res.Vars[name] = value
		}
		inputs = append(inputs, map[string]interface{}{
			"name":      name,
			"required":  true,
			"sensitive": true,
		})
	}

	b.Metadata = newMetadata
	return b, inputs
}

// uniqueVarName builds a globally-unique, identifier-safe input name for a
// lifted (app, key) value, since the companion vars-file is one flat mapping
// shared across every play.
func (res *ExportResult) uniqueVarName(app, key string) string {
	base := sanitizeIdent(app) + "_" + sanitizeIdent(key)
	name := base
	for i := 2; res.usedVarNames[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	res.usedVarNames[name] = true
	return name
}

// sortedSetKeys returns the keys of a set (map[string]bool) in sorted order, a
// common shape for the list-returning readers the exporters reuse.
func sortedSetKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sanitizeIdent replaces every character that is not a letter, digit, or
// underscore with an underscore so the result is a valid sigil identifier.
func sanitizeIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// MarshalRecipe renders the assembled recipe as canonical YAML (or JSON5 when
// format is a JSON5 alias), using the same formatter as `docket fmt`.
func (res *ExportResult) MarshalRecipe(format string) ([]byte, error) {
	raw, err := yaml.Marshal(res.plays)
	if err != nil {
		return nil, err
	}
	if IsJSON5Format(format) {
		// Round-trip through YAML so the struct yaml tags drive the keys, then
		// re-encode as JSON for the JSON5 formatter.
		var generic interface{}
		if err := yaml.Unmarshal(raw, &generic); err != nil {
			return nil, err
		}
		jsonRaw, err := json.Marshal(generic)
		if err != nil {
			return nil, err
		}
		return FormatJSON5(jsonRaw)
	}
	return Format(raw)
}

// MarshalVars renders the companion vars-file (a flat mapping of input name to
// value) in YAML or JSON to match the vars-output extension.
func (res *ExportResult) MarshalVars(format string) ([]byte, error) {
	if IsJSON5Format(format) {
		return json.MarshalIndent(res.Vars, "", "  ")
	}
	return yaml.Marshal(res.Vars)
}

// HasVars reports whether the export lifted any values into the vars map.
func (res *ExportResult) HasVars() bool {
	return len(res.Vars) > 0
}

// PlayCount returns the number of plays (one per exported app) in the result.
func (res *ExportResult) PlayCount() int {
	return len(res.plays)
}

// listApps returns every app on the server via `dokku apps:list`.
func listApps() ([]string, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "apps:list"},
	})
	if err != nil {
		return nil, err
	}
	var apps []string
	for _, line := range strings.Split(result.StdoutContents(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			apps = append(apps, line)
		}
	}
	return apps, nil
}

// filterApps keeps only the apps named in want, or all apps when want is empty.
func filterApps(apps, want []string) []string {
	if len(want) == 0 {
		return apps
	}
	keep := map[string]bool{}
	for _, a := range want {
		keep[a] = true
	}
	var out []string
	for _, a := range apps {
		if keep[a] {
			out = append(out, a)
		}
	}
	return out
}
