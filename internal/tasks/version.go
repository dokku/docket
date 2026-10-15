package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/dokku/docket/internal/subprocess"
)

// VersionSupport is the oldest Dokku release a task can run on, plus the
// non-core plugins it needs. MinimumVersions returns the declaration for the
// task type, including plugins that apply only for some field values. The
// gate, not the method, decides which of those apply to a particular task.
type VersionSupport struct {
	// Dokku is the oldest Dokku release, as major.minor.patch.
	Dokku string

	// Plugins are the non-core plugins the task needs, in docs order.
	Plugins []PluginRequirement
}

// PluginRequirement is one non-core plugin a task needs.
//
// Name is the name `plugin:list` prints (`letsencrypt`), which is not always
// the name the docs use (`dokku-letsencrypt`). Text is that docs line and is
// required so the two cannot be derived from each other.
//
// NameField, when set, is the Go field whose string value is the plugin:list
// name. Datastore tasks set it to Service. WhenField, when set, is the Go
// bool field that must be true before the requirement applies. Certs sets it
// to Global. The catalog publishes the recipe keys for those fields, not the
// Go names.
type PluginRequirement struct {
	Name      string
	Minimum   string
	NameField string
	WhenField string
	Text      string
}

// VersionDocer is the interface a task implements to declare the oldest
// Dokku, and any plugins, it supports. Every registered task is expected to
// implement it. A coverage test enforces that, but it is modelled as an
// optional interface so a test-only task can stand in for a real one without
// a floor. Plan, apply, and export read it; a task that does not implement
// it is not checked.
type VersionDocer interface {
	MinimumVersions() VersionSupport
}

// TaskMinimumVersions returns the version declaration for t, and whether t
// declared one.
func TaskMinimumVersions(t Task) (VersionSupport, bool) {
	if d, ok := t.(VersionDocer); ok {
		return d.MinimumVersions(), true
	}
	return VersionSupport{}, false
}

// parsedVersion is a major.minor.patch triple. Dokku and the plugins docket
// tasks depend on publish plain triples, so this does not accept prerelease
// or build metadata.
type parsedVersion struct {
	major int
	minor int
	patch int
}

func (v parsedVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func (v parsedVersion) olderThan(min parsedVersion) bool {
	if v.major != min.major {
		return v.major < min.major
	}
	if v.minor != min.minor {
		return v.minor < min.minor
	}
	return v.patch < min.patch
}

// parseVersion reads a major.minor.patch triple. A leading "v" is accepted.
// A phrase such as "dokku version 0.38.31" uses its last field. Anything else,
// including a commit sha or a prerelease suffix, is an error.
func parseVersion(raw string) (parsedVersion, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return parsedVersion{}, fmt.Errorf("empty version")
	}
	if fields := strings.Fields(s); len(fields) > 1 {
		s = fields[len(fields)-1]
	}
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return parsedVersion{}, fmt.Errorf("version %q is not major.minor.patch", raw)
	}
	var nums [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || (len(part) > 1 && part[0] == '0') {
			return parsedVersion{}, fmt.Errorf("version %q is not major.minor.patch", raw)
		}
		nums[i] = n
	}
	return parsedVersion{major: nums[0], minor: nums[1], patch: nums[2]}, nil
}

// versionCacheKey is the context key for the per-run version cache. The cache
// is a pointer, so a play context derived with ContextWithTarget still sees
// the map the run installed, and two hosts do not share a version.
type versionCacheKey struct{}

// versionCache holds one Dokku version and one plugin list per target. Tasks
// in a run are sequential, and the mutex is what keeps a test that checks two
// hosts, or an invalidate during apply, from racing the read.
type versionCache struct {
	mu       sync.Mutex
	byTarget map[string]*targetVersions
}

// targetVersions is what one server (one host, one sudo bit) has reported.
type targetVersions struct {
	dokkuLoaded bool
	dokkuRaw    string
	dokkuErr    error

	pluginsLoaded bool
	plugins       map[string]string
	pluginsErr    error
}

// ContextWithVersionGate returns a context on which plan, apply, and export
// refuse a task whose declared floor is newer than the server. A context that
// already carries a gate is returned as-is, so installing it twice does not
// split the cache. Without a gate, CheckMinimumVersions does nothing: export
// unit tests call ExportRecipe directly and their fake dokku answers an empty
// version, which would otherwise skip every exporter.
func ContextWithVersionGate(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if versionCacheFrom(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, versionCacheKey{}, &versionCache{
		byTarget: map[string]*targetVersions{},
	})
}

func versionCacheFrom(ctx context.Context) *versionCache {
	if ctx == nil {
		return nil
	}
	cache, _ := ctx.Value(versionCacheKey{}).(*versionCache)
	return cache
}

// targetCacheKey distinguishes two servers in one run. Host is the remote
// (empty locally). Sudo is part of the key because the plan and apply
// contexts carry it on the target, and a sudo and a non-sudo view of the
// same host are different invocations even when they happen to see the same
// Dokku.
func targetCacheKey(ctx context.Context) string {
	target := subprocess.TargetFromContext(ctx)
	sudo := "0"
	if target.Sudo {
		sudo = "1"
	}
	return target.Host + "\x00" + sudo
}

func (c *versionCache) entry(ctx context.Context) *targetVersions {
	key := targetCacheKey(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.byTarget[key]
	if !ok {
		entry = &targetVersions{}
		c.byTarget[key] = entry
	}
	return entry
}

// InvalidatePluginVersions drops the cached plugin list for the target ctx
// names. dokku_plugin calls it after a successful install or uninstall so a
// later task on the same host sees the plugin it just changed. The Dokku
// version is left alone: installing a plugin does not change it. A context
// with no gate, which is every unit test that calls the task directly, is a
// no-op.
func InvalidatePluginVersions(ctx context.Context) {
	cache := versionCacheFrom(ctx)
	if cache == nil {
		return
	}
	entry := cache.entry(ctx)
	cache.mu.Lock()
	entry.pluginsLoaded = false
	entry.plugins = nil
	entry.pluginsErr = nil
	cache.mu.Unlock()
}

// withoutVersionGate returns a context on which CheckMinimumVersions does
// nothing, even when ctx carries a gate. Export uses it for IgnoreVersion.
func withoutVersionGate(ctx context.Context) context.Context {
	if versionCacheFrom(ctx) == nil {
		return ctx
	}
	return context.WithValue(ctx, versionCacheKey{}, (*versionCache)(nil))
}

// VersionError is the error CheckMinimumVersions returns when the server, or
// an installed plugin a task needs, is older than the task's floor, or when
// that version could not be read or parsed. Its message names no flag: the
// CLI adds the --ignore-version hint when it renders one.
type VersionError struct {
	// TaskType is the task type that was checked, such as "dokku_app".
	TaskType string

	// Plugin is the plugin:list name of the plugin that was checked, or ""
	// when the floor is Dokku's own. When the plugin list could not be read
	// it is the first plugin the task needed.
	Plugin string

	// Minimum is the floor the task declares.
	Minimum string

	// Found is the version the server reported, or "" when it reported
	// nothing or could not be asked.
	Found string

	// Err is why the version could not be read or parsed. It is nil when the
	// server is simply older than Minimum.
	Err error

	// readFailed is set when asking the server failed, as opposed to its
	// answer not parsing.
	readFailed bool
}

func (e *VersionError) component() string {
	if e.Plugin == "" {
		return "Dokku"
	}
	return e.Plugin
}

func (e *VersionError) Error() string {
	switch {
	case e.readFailed && e.Plugin == "":
		return fmt.Sprintf("%s: checking Dokku version: %v", e.TaskType, e.Err)
	case e.readFailed:
		return fmt.Sprintf("%s: checking plugin versions: %v", e.TaskType, e.Err)
	case e.Err != nil && e.Plugin == "":
		return fmt.Sprintf("%s requires Dokku >= %s, but the server version %q could not be parsed", e.TaskType, e.Minimum, strings.TrimSpace(e.Found))
	case e.Err != nil:
		return fmt.Sprintf("%s requires %s >= %s, but the installed version %q could not be parsed", e.TaskType, e.Plugin, e.Minimum, strings.TrimSpace(e.Found))
	default:
		return fmt.Sprintf("%s requires %s >= %s, server has %s", e.TaskType, e.component(), e.Minimum, e.Found)
	}
}

func (e *VersionError) Unwrap() error {
	return e.Err
}

// CheckMinimumVersions reports whether the server ctx targets is older than
// the floor task declares. A context with no version gate returns nil, as
// does a task that declares no floor. An uninstalled plugin is not a failure:
// the task's own probe reports that, and a recipe can install the plugin
// earlier in the same run. An installed plugin that is older, or a version
// that does not parse, fails the task with a *VersionError.
func CheckMinimumVersions(ctx context.Context, typeKey string, task Task) error {
	cache := versionCacheFrom(ctx)
	if cache == nil || task == nil {
		return nil
	}
	support, ok := TaskMinimumVersions(task)
	if !ok {
		return nil
	}
	if typeKey == "" {
		typeKey = "task"
	}
	need, err := parseVersion(support.Dokku)
	if err != nil {
		return fmt.Errorf("%s declares Dokku version %q, which is not major.minor.patch", typeKey, support.Dokku)
	}

	entry := cache.entry(ctx)
	raw, probeErr := entry.dokku(ctx, cache)
	if probeErr != nil {
		return &VersionError{TaskType: typeKey, Minimum: need.String(), Err: probeErr, readFailed: true}
	}
	have, err := parseVersion(raw)
	if err != nil {
		return &VersionError{TaskType: typeKey, Minimum: need.String(), Found: raw, Err: err}
	}
	if have.olderThan(need) {
		return &VersionError{TaskType: typeKey, Minimum: need.String(), Found: have.String()}
	}

	var applicable []PluginRequirement
	for _, plugin := range support.Plugins {
		if !pluginApplies(task, plugin) {
			continue
		}
		name, ok := pluginListName(task, plugin)
		if !ok {
			continue
		}
		applicable = append(applicable, pluginWithName(plugin, name))
	}
	if len(applicable) == 0 {
		return nil
	}

	plugins, pluginErr := entry.pluginList(ctx, cache)
	if pluginErr != nil {
		return &VersionError{TaskType: typeKey, Plugin: applicable[0].Name, Minimum: applicable[0].Minimum, Err: pluginErr, readFailed: true}
	}
	for _, plugin := range applicable {
		reported, installed := plugins[plugin.Name]
		if !installed {
			continue
		}
		needPlugin, err := parseVersion(plugin.Minimum)
		if err != nil {
			return fmt.Errorf("%s declares %s version %q, which is not major.minor.patch", typeKey, plugin.Name, plugin.Minimum)
		}
		havePlugin, err := parseVersion(reported)
		if err != nil {
			return &VersionError{TaskType: typeKey, Plugin: plugin.Name, Minimum: needPlugin.String(), Found: reported, Err: err}
		}
		if havePlugin.olderThan(needPlugin) {
			return &VersionError{TaskType: typeKey, Plugin: plugin.Name, Minimum: needPlugin.String(), Found: havePlugin.String()}
		}
	}
	return nil
}

func pluginWithName(plugin PluginRequirement, name string) PluginRequirement {
	plugin.Name = name
	return plugin
}

func (e *targetVersions) dokku(ctx context.Context, cache *versionCache) (string, error) {
	cache.mu.Lock()
	if e.dokkuLoaded {
		raw, err := e.dokkuRaw, e.dokkuErr
		cache.mu.Unlock()
		return raw, err
	}
	cache.mu.Unlock()

	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"version"},
	})
	raw := ""
	if err == nil {
		raw = result.StdoutContents()
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !e.dokkuLoaded {
		e.dokkuLoaded = true
		e.dokkuRaw = raw
		e.dokkuErr = err
	}
	return e.dokkuRaw, e.dokkuErr
}

func (e *targetVersions) pluginList(ctx context.Context, cache *versionCache) (map[string]string, error) {
	cache.mu.Lock()
	if e.pluginsLoaded {
		plugins, err := e.plugins, e.pluginsErr
		cache.mu.Unlock()
		return plugins, err
	}
	cache.mu.Unlock()

	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "plugin:list", "--format", "json"},
	})
	var plugins map[string]string
	if err == nil {
		plugins = parsePluginList(result.StdoutContents())
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !e.pluginsLoaded {
		e.pluginsLoaded = true
		e.plugins = plugins
		e.pluginsErr = err
	}
	return e.plugins, e.pluginsErr
}

// parsePluginList reads `plugin:list --format json` when the body is a JSON
// array, and the whitespace table otherwise. Older Dokku accepts the flag,
// exits 0, and prints the table. The version column is the second field;
// a row whose second field is not a triple (the status word, on a plugin
// that prints none) is recorded with an empty version so a later compare
// fails closed instead of treating the plugin as missing.
func parsePluginList(body string) map[string]string {
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "[") {
		var entries []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if err := json.Unmarshal([]byte(trimmed), &entries); err == nil {
			out := make(map[string]string, len(entries))
			for _, entry := range entries {
				if entry.Name == "" {
					continue
				}
				out[entry.Name] = entry.Version
			}
			return out
		}
	}

	out := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		version := ""
		if len(fields) >= 2 {
			if _, err := parseVersion(fields[1]); err == nil {
				version = fields[1]
			}
		}
		out[fields[0]] = version
	}
	return out
}

// pluginApplies reports whether plugin's WhenField is unset or true on task.
// A WhenField the task does not have is treated as applying, so a typo fails
// closed instead of silently skipping the check. The coverage test rejects
// that typo before it ships.
func pluginApplies(task Task, plugin PluginRequirement) bool {
	if plugin.WhenField == "" {
		return true
	}
	field, ok := structField(task, plugin.WhenField)
	if !ok || field.Kind() != reflect.Bool {
		return true
	}
	return field.Bool()
}

// pluginListName is the plugin:list name for plugin on task. A NameField that
// is empty (the zero prototype export checks, or a task whose service type
// was not set) returns false so the check is skipped rather than looking up
// a plugin named "".
func pluginListName(task Task, plugin PluginRequirement) (string, bool) {
	if plugin.NameField == "" {
		return plugin.Name, plugin.Name != ""
	}
	field, ok := structField(task, plugin.NameField)
	if !ok || field.Kind() != reflect.String {
		return "", false
	}
	name := field.String()
	if name == "" {
		return "", false
	}
	return name, true
}

// structField returns the named field of task, descending into embedded
// structs. The name is the Go field name.
func structField(task Task, name string) (reflect.Value, bool) {
	value := reflect.ValueOf(task)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}, false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	return walkStructField(value, name)
}

func walkStructField(value reflect.Value, name string) (reflect.Value, bool) {
	structType := value.Type()
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		if field.Name == name {
			return value.Field(i), true
		}
		if field.Anonymous && value.Field(i).Kind() == reflect.Struct {
			if found, ok := walkStructField(value.Field(i), name); ok {
				return found, true
			}
		}
	}
	return reflect.Value{}, false
}

// recipeKeyFor returns the recipe key of the Go field name on task. The
// catalog publishes this rather than the Go identifier.
func recipeKeyFor(task Task, goName string) string {
	structType := reflect.TypeOf(task)
	for structType != nil && structType.Kind() == reflect.Pointer {
		structType = structType.Elem()
	}
	if structType == nil || structType.Kind() != reflect.Struct {
		return ""
	}
	field, ok := walkStructType(structType, goName)
	if !ok {
		return ""
	}
	return catalogYAMLName(field.Tag, field.Name)
}

func walkStructType(structType reflect.Type, name string) (reflect.StructField, bool) {
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		if field.Name == name {
			return field, true
		}
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			if found, ok := walkStructType(field.Type, name); ok {
				return found, true
			}
		}
	}
	return reflect.StructField{}, false
}
