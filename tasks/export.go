package tasks

import (
	"encoding/json"
	"fmt"
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

// appExportOrder is the fixed order in which app-scoped task types are emitted
// into each app's play. dokku_app comes first; deploy sources are emitted last.
// Adding a task to export means implementing AppExporter on it and adding its
// type-key here.
var appExportOrder = []string{
	"dokku_app",
	"dokku_config",
	"dokku_domains",
	"dokku_ports",
	"dokku_buildpacks",
	"dokku_storage_mount",
	"dokku_ps_scale",
	"dokku_checks_toggle",
	"dokku_proxy_toggle",
	"dokku_domains_toggle",
	"dokku_maintenance",
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
	"dokku_traefik_property",
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
	default:
		return body, nil
	}
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
