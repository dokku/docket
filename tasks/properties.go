package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// PropertyKeys carries the JSON keys that dokku <plugin>:report --format json
// emits for one property on dokku 0.38.8+. An empty string means the property
// has no form in that scope (so probing it in that scope is rejected at plan
// time, matching dokku's own CLI rejection).
//
// Values point at the canonical bare-key shape (no <plugin>- prefix). The
// legacy <plugin>-prefixed keys remain emitted during the 0.38.x deprecation
// window but are ignored by the lookup.
//
// Sensitive marks a property whose value is a secret (e.g. a password or a
// cluster token). When set, planProperty registers both the desired value and
// the server-probed current value with the masker, so the `(was %q)` drift
// reason and the command echo are masked. It is a per-property flag because a
// plugin usually has a mix of secret and benign properties.
type PropertyKeys struct {
	PerApp    string
	Global    string
	Sensitive bool
}

// RejectedPropertyFamily is a family of property names, matched by prefix, that
// a task deliberately refuses because another task owns them.
//
// It is not the same as a name simply being absent from Keys. An absent name is
// a typo, and the right answer is the list of names that are supported; a
// rejected family is a name the user meant, written against the wrong task, and
// the only useful answer is which task to write it against instead. Offering
// the supported list there sends the user looking for something that is not on
// it (#458).
//
// Declaring it on the table rather than guarding inside Plan() is what keeps
// plan, apply and validate on the same message: validatePropertyInput is the
// one function both paths reach, and it is where the family is checked.
type RejectedPropertyFamily struct {
	// Prefix is what a member's name starts with, for example "chart.".
	Prefix string

	// Replacement is the task type that manages the family instead, for
	// example "dokku_scheduler_k3s_chart".
	Replacement string

	// Reason says why this task does not manage it, as a clause that reads
	// after a semicolon: "the scheduler-k3s:set path for chart values is
	// deprecated in dokku".
	Reason string
}

// err returns the error a member of the family is rejected with. All three
// fields are required; TestRejectedPropertyFamiliesAreWellFormed fails the
// build for a family that leaves one empty, so there is nothing to branch on
// here.
func (f RejectedPropertyFamily) err() error {
	return fmt.Errorf("%s* properties are managed by %s; %s", f.Prefix, f.Replacement, f.Reason)
}

// PropertyTable is the property schema one *_property task manages: the dokku
// subcommand a set/unset runs, and the report keys for every property name the
// task supports.
//
// It is the single source of truth for that task. Plan(), Validate(), the two
// exporters and the machine-readable catalog all read the same value, because
// the shared helpers below take the task through PropertyTableDocer rather than
// taking a subcommand and a map as separate arguments. A task therefore cannot
// declare one table to the catalog and run against another.
type PropertyTable struct {
	// Subcommand is the dokku subcommand a set or unset runs, for example
	// "apps:set" or "buildpacks:set-property".
	Subcommand string

	// Keys maps each supported property name to the JSON keys
	// `dokku <plugin>:report --format json` emits for it per scope.
	Keys map[string]PropertyKeys

	// Rejected are the name families this task refuses outright because
	// another task manages them. Checked before Keys, so a member is
	// answered with the task that owns it rather than with the list of
	// names this one supports.
	Rejected []RejectedPropertyFamily
}

// rejectedFamilyFor returns the family that refuses a property, if any.
func (p PropertyTable) rejectedFamilyFor(property string) (RejectedPropertyFamily, bool) {
	for _, family := range p.Rejected {
		if strings.HasPrefix(property, family.Prefix) {
			return family, true
		}
	}
	return RejectedPropertyFamily{}, false
}

// Plugin returns the dokku plugin the table belongs to, derived from the
// subcommand: "apps:set" -> "apps".
func (p PropertyTable) Plugin() string {
	return pluginFromSubcommand(p.Subcommand)
}

// PropertyTableDocer is implemented by every task that manages a dokku property
// table.
//
// Unlike its siblings (ExportDocer, ProbeDocer) this one is not read only by
// the docs generator: planProperty, validatePropertyInput and the property
// exporters take it as their first argument, so a task cannot reach the shared
// property machinery without declaring the table the catalog publishes.
//
// A task whose property names are validated by dokku rather than by docket -
// dokku_service_property, whose names come from whichever datastore plugin
// backs the service - deliberately does not implement it. See
// TestEveryPropertyTaskDeclaresPropertyTable.
type PropertyTableDocer interface {
	PropertyTable() PropertyTable
}

// TaskPropertyTable returns t's property table and whether t declared one.
// Centralised so the catalog, the docs generator and the coverage test share
// one read site, mirroring TaskExportSupport and TaskProbeSupport.
func TaskPropertyTable(t Task) (PropertyTable, bool) {
	if d, ok := t.(PropertyTableDocer); ok {
		return d.PropertyTable(), true
	}
	return PropertyTable{}, false
}

// pluginFromSubcommand returns the plugin component of a colon-separated
// subcommand. For example, "nginx:set" -> "nginx", "buildpacks:set-property" ->
// "buildpacks", "app-json:set" -> "app-json".
func pluginFromSubcommand(subcommand string) string {
	return strings.SplitN(subcommand, ":", 2)[0]
}

// PropertyFields is the recipe shape every app-or-global property task shares:
// the scope (`app` or `global`), the property being managed, its value, and
// whether it should be present or absent. A property task declares
// `type XPropertyTask PropertyFields` rather than restating the five fields, so
// a cross-cutting field change - the identity tags of #427, say - lands in one
// place instead of twenty-seven.
//
// A defined type rather than an embedded struct because the field set stays
// flat to reflection: the catalog, the required-field walk behind
// missing_required_field, the identity walk that names an unnamed task, and the
// sensitive-value walks all read a task's fields at the top level, and an
// embedded field set would empty every one of them out.
//
// The descriptions are deliberately plugin-agnostic. A task's page already
// names its plugin in the title, the synopsis and the Properties table, so
// repeating it in every parameter row bought nothing and cost twenty-seven
// copies of the field set. See TestPropertyTasksDeclareTheSharedFields for the
// tasks whose shape genuinely differs.
type PropertyFields struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" identity:"key" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the property should be applied globally
	Global bool `required:"false" identity:"key" yaml:"global,omitempty" description:"Flag indicating if the property should be applied globally"`

	// Property is the name of the property to set
	Property string `required:"true" identity:"key" yaml:"property" description:"Name of the property to set"`

	// Value is the value to set for the property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the property"`

	// State is the desired state of the property
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the property"`
}

// SensitivePropertyFields is PropertyFields with a masked Value, for a plugin
// whose property values can carry credentials. Every value is masked, benign
// ones included, which is preferable to leaking a secret because the per-property
// judgement was wrong.
//
// The tag drives output masking only - export decides what to lift into a vars
// file from the property's PropertyKeys entry, so a benign value stays readable
// in the exported recipe (#451). It is a separate type rather than a tag
// override because a defined type carries the tags of exactly one struct;
// TestSensitivePropertyFieldsMatchesPropertyFields fails if the two drift apart
// in anything but that one tag.
type SensitivePropertyFields struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" identity:"key" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the property should be applied globally
	Global bool `required:"false" identity:"key" yaml:"global,omitempty" description:"Flag indicating if the property should be applied globally"`

	// Property is the name of the property to set
	Property string `required:"true" identity:"key" yaml:"property" description:"Name of the property to set"`

	// Value is the value to set for the property
	Value string `required:"false" sensitive:"true" yaml:"value,omitempty" description:"Value to set for the property"`

	// State is the desired state of the property
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the property"`
}

// errUnknownProperty is returned by getProperty when the JSON :report payload
// has no entry for the key the task asked us to look up.
type errUnknownProperty struct {
	plugin    string
	property  string
	lookedFor string
	validKeys []string
}

func (e *errUnknownProperty) Error() string {
	return fmt.Sprintf("dokku %s:report has no key %q for property %q", e.plugin, e.lookedFor, e.property)
}

// getProperty reads the current value of a property via
// `dokku <plugin>:report [<app>|--global] --format json`. The JSON payload is
// parsed and the value is read from the JSON key the task specifies via its
// PropertyKeys map.
//
// Returns:
//   - (value, nil) when the looked-up key exists in the JSON payload
//   - ("", *errUnknownProperty) when the JSON parsed but the key was absent
//   - ("", err) when the exec or JSON parse failed
func getProperty(ctx context.Context, subcommand, app string, global bool, property string, keys map[string]PropertyKeys) (string, error) {
	plugin := pluginFromSubcommand(subcommand)
	args := getPropertyArgs(plugin, app, global)

	response, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    args,
	})
	if err != nil {
		return "", err
	}

	payload := map[string]string{}
	if err := json.Unmarshal(response.StdoutBytes(), &payload); err != nil {
		return "", fmt.Errorf("parse %s:report json: %w", plugin, err)
	}

	entry := keys[property]
	lookup := entry.PerApp
	if global {
		lookup = entry.Global
	}

	value, ok := payload[lookup]
	if !ok {
		// A probeable dynamic property only gets a report row once it holds a
		// value, so an absent row means the property is unset rather than that
		// the key map is stale. The entry is compared against the synthesized
		// one so a caller that passed a map without it - where lookup would be
		// the empty string - still takes the error path (#449).
		if dyn, dynamic := dynamicPropertyKeys(plugin, property); dynamic && entry == dyn {
			return "", nil
		}
		keyList := make([]string, 0, len(payload))
		for k := range payload {
			keyList = append(keyList, k)
		}
		sort.Strings(keyList)
		return "", &errUnknownProperty{
			plugin:    plugin,
			property:  property,
			lookedFor: lookup,
			validKeys: keyList,
		}
	}
	return value, nil
}

// exportProperties reconstructs the explicitly-set properties of a property
// plugin for an app. It reads `<plugin>:report --format json` once and, for
// each property in keys, emits a task body (built by factory) when the raw
// per-app key is present and non-empty. dokku only includes the raw key in the
// report when the property has been set (unset properties appear only under a
// `computed-` key), so this naturally captures the non-default settings without
// a defaults table. Read-only/computed keys are skipped because they are not in
// keys.
//
// A probeable dynamic family cannot be enumerated in keys, so the properties the
// payload happens to carry are synthesized into it first; without that a set
// letsencrypt `dns-provider-<KEY>` credential is dropped from the export (#449).
func exportProperties(ctx context.Context, task PropertyTableDocer, app string, factory func(app, property, value string) interface{}) ([]interface{}, error) {
	table := task.PropertyTable()
	keys := table.Keys
	plugin := table.Plugin()
	payload, err := readPropertyReport(ctx, plugin, app, false)
	if err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, nil
	}
	keys = withDynamicProperties(plugin, keys, dynamicPropertiesFromReport(plugin, payload, false))

	props := make([]string, 0, len(keys))
	for prop := range keys {
		props = append(props, prop)
	}
	sort.Strings(props)

	var out []interface{}
	for _, prop := range props {
		key := keys[prop].PerApp
		if key == "" {
			continue
		}
		value, ok := payload[key]
		if !ok || value == "" {
			continue
		}
		out = append(out, factory(app, prop, value))
	}
	return out, nil
}

// exportGlobalProperties reconstructs the explicitly-set global properties of a
// property plugin, mirroring exportProperties for the --global scope: it reads
// `<plugin>:report --global --format json` once and, for each property whose
// Global key is present and non-empty, emits a task body (built by factory). A
// property with no global form (an empty Global key) is skipped, so
// globally-scoped state (for example scheduler-k3s bootstrap keys) is captured
// instead of silently dropped (#327). Probeable dynamic properties are
// synthesized into keys the same way exportProperties does it.
func exportGlobalProperties(ctx context.Context, task PropertyTableDocer, factory func(property, value string) interface{}) ([]interface{}, error) {
	table := task.PropertyTable()
	keys := table.Keys
	plugin := table.Plugin()
	payload, err := readPropertyReport(ctx, plugin, "", true)
	if err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, nil
	}
	keys = withDynamicProperties(plugin, keys, dynamicPropertiesFromReport(plugin, payload, true))

	props := make([]string, 0, len(keys))
	for prop := range keys {
		props = append(props, prop)
	}
	sort.Strings(props)

	var out []interface{}
	for _, prop := range props {
		key := keys[prop].Global
		if key == "" {
			continue
		}
		value, ok := payload[key]
		if !ok || value == "" {
			continue
		}
		out = append(out, factory(prop, value))
	}
	return out, nil
}

// readPropertyReport runs `dokku <plugin>:report [<app>|--global] --format json`
// and returns the decoded payload, distinguishing a plugin that is not installed
// (returns nil, nil - a quiet skip) from one that is installed but whose report
// cannot be read (returns an error the export surfaces as a warning). An SSH
// transport failure always propagates. Any other exec failure is a quiet skip
// only when the plugin is not installed; when it is installed, the failure is an
// error. A JSON parse failure is always an error, since the exec succeeded so the
// plugin responded with something unparseable (for example a deprecation line
// printed before the JSON payload) rather than being absent (#329).
func readPropertyReport(ctx context.Context, plugin, app string, global bool) (map[string]string, error) {
	response, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    getPropertyArgs(plugin, app, global),
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		if installed, ierr := pluginInstalled(ctx, plugin); ierr != nil || !installed {
			return nil, nil
		}
		return nil, fmt.Errorf("dokku %s:report failed: %w", plugin, err)
	}

	payload := map[string]string{}
	if err := json.Unmarshal(response.StdoutBytes(), &payload); err != nil {
		return nil, fmt.Errorf("parse %s:report json: %w", plugin, err)
	}
	return payload, nil
}

// getPropertyArgs builds the `dokku <plugin>:report ... --format json` arg list.
func getPropertyArgs(plugin, app string, global bool) []string {
	args := []string{"--quiet", plugin + ":report"}
	if global {
		args = append(args, "--global")
	} else {
		args = append(args, app)
	}
	return append(args, "--format", "json")
}

// unknownPropertyWarning returns a diagnostic (and true) when the probe
// identifies the property as unknown, either because the JSON payload had no
// matching key (likely a stale map or a dokku version mismatch) or because the
// :report invocation rejected `--format json` itself (older plugin versions).
// It returns ok=false for every other error, since callers already propagate
// those through PlanResult.Reason. The returned message is raw; planProperty
// attaches it to PlanResult.Warnings and the emitter masks it at output time.
func unknownPropertyWarning(plugin, property string, err error) (PlanWarning, bool) {
	if err == nil {
		return PlanWarning{}, false
	}

	var unknown *errUnknownProperty
	if errors.As(err, &unknown) {
		// Skip the warning for known dynamic-property families, where
		// missing-from-report is the normal pre-set state, not a typo.
		// A probeable family no longer reaches this from planProperty -
		// getProperty reads its absent row as unset - so this now only
		// guards a family docket cannot probe at all.
		if isDynamicProperty(plugin, property) {
			return PlanWarning{}, false
		}
		return PlanWarning{
			Reason: WarnReasonUnknownProperty,
			Message: fmt.Sprintf("dokku %s:report has no key %q for property %q (available keys: %s)",
				plugin, unknown.lookedFor, property, strings.Join(unknown.validKeys, ", ")),
		}, true
	}

	var execErr *subprocess.ExecError
	if !errors.As(err, &execErr) {
		return PlanWarning{}, false
	}
	stderr := strings.TrimSpace(execErr.Response.Stderr)
	if !strings.Contains(stderr, "Invalid flag passed, valid flags:") {
		return PlanWarning{}, false
	}
	return PlanWarning{
		Reason:  WarnReasonProbeRejected,
		Message: fmt.Sprintf("dokku %s:report rejected probe for property %q: %s", plugin, property, stderr),
	}, true
}

// dynamicPropertyFamily is a family of property names a plugin validates
// through `:set` rather than through its report schema, so the names cannot be
// enumerated in a PropertyTable.
type dynamicPropertyFamily struct {
	// Prefix is what a member's name starts with.
	Prefix string

	// Probeable is true when the plugin emits a report row per set member, so
	// the lookup keys can be synthesized from the property name and the member
	// probes like any mapped property. False means the member is applied
	// unconditionally and the task never converges for it.
	Probeable bool

	// Sensitive is true when docket treats members as secrets. It is
	// independent of Probeable: whether a value is a credential and whether
	// docket can read it back are separate questions (#457).
	Sensitive bool

	// Scopes is the non-empty subset of ["app", "global"] the plugin accepts a
	// member in, the same vocabulary PropertyEntrySchema.Scopes publishes for
	// an enumerable property. A mapped property says this by leaving one of its
	// PropertyKeys halves empty; a dynamic one has no map entry to say it with,
	// so the family carries it and keysFor, validateProperty and the catalog all
	// read this one field. traefik's `traefik:set` refuses a `dns-provider-*`
	// key outside `--global`, so without it an app-scoped member would probe a
	// row that cannot exist: `state: present` would plan create forever and
	// `state: absent` would report in sync while never unsetting the credential.
	Scopes []string
}

// scoped reports whether the family may be used in a scope.
func (f dynamicPropertyFamily) scoped(scope string) bool {
	for _, s := range f.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// keysFor returns the PropertyKeys one member of the family gets. The report
// keys exist only when the plugin reports the family, so an unprobeable member
// carries no lookup keys - but it still carries the family's Sensitive mark, so
// its value is masked on the way out even though it can never be read back. A
// scope the family does not list gets no key either, which is how a global-only
// family reaches validateProperty's existing "no per-app form" rejection.
func (f dynamicPropertyFamily) keysFor(property string) PropertyKeys {
	entry := PropertyKeys{Sensitive: f.Sensitive}
	if !f.Probeable {
		return entry
	}
	if f.scoped(PropertyScopeApp) {
		entry.PerApp = property
	}
	if f.scoped(PropertyScopeGlobal) {
		entry.Global = "global-" + property
	}
	return entry
}

// dynamicPropertyFamilies is the whole set of dynamic families docket knows
// about, keyed by plugin. It is a table rather than a switch so the catalog can
// publish it: a consumer validating a recipe offline has to accept a name that
// cannot be enumerated, and the only way to know which names those are is to be
// told the prefix.
//
// scheduler-k3s `chart.*.*` used to live here but moved to the dedicated
// dokku_scheduler_k3s_chart task; it is now a RejectedPropertyFamily on
// schedulerK3sPropertyTable, so validatePropertyInput turns it away before
// reaching this helper.
var dynamicPropertyFamilies = map[string][]dynamicPropertyFamily{
	// dokku-letsencrypt 0.25.0+ emits a `dns-provider-<KEY>` row for the app
	// scope and a `global-dns-provider-<KEY>` row for the global one per
	// property that is set (#449). The values are DNS provider API
	// credentials, hence Sensitive: planProperty registers both the desired
	// value and, because this family is probed, the value read back before it
	// can reach a `(was %q)` drift reason.
	"letsencrypt": {{
		Prefix:    "dns-provider-",
		Probeable: true,
		Sensitive: true,
		Scopes:    []string{PropertyScopeApp, PropertyScopeGlobal},
	}},

	// traefik's family holds the same credentials and reports them the same
	// way, as of dokku 0.38.27 (dokku/dokku#8928, #450). It is global-only:
	// `traefik:set` refuses a `dns-provider-*` key outside `--global`, so there
	// is a `global-dns-provider-<KEY>` row and no per-app one. The version is
	// not gated because docket's dokku floor is already 0.38.27; below it the
	// rows are genuinely absent and a `state: absent` task would read an unset
	// property as already gone.
	"traefik": {{
		Prefix:    "dns-provider-",
		Probeable: true,
		Sensitive: true,
		Scopes:    []string{PropertyScopeGlobal},
	}},
}

// isDynamicProperty reports whether a (plugin, property) pair belongs to a
// dynamic property family. This is what lets validateProperty accept a name
// that cannot be enumerated in the key map - the scope it accepts it in still
// comes from the family's Scopes. It says nothing about whether the property
// can be probed - that is the family's Probeable flag, read by
// dynamicPropertyKeys.
func isDynamicProperty(plugin, property string) bool {
	_, ok := dynamicPropertyFamilyFor(plugin, property)
	return ok
}

// DynamicPropertyFamilies returns the dynamic name families a plugin declares,
// sorted by prefix, in the form the task catalog publishes. Exported so a
// consumer validating a recipe offline knows which unenumerable names to
// accept.
func DynamicPropertyFamilies(plugin string) []DynamicPropertySchema {
	families := dynamicPropertyFamilies[plugin]
	if len(families) == 0 {
		return nil
	}
	out := make([]DynamicPropertySchema, 0, len(families))
	for _, family := range families {
		scopes := make([]string, 0, len(family.Scopes))
		for _, scope := range []string{PropertyScopeApp, PropertyScopeGlobal} {
			if family.scoped(scope) {
				scopes = append(scopes, scope)
			}
		}
		out = append(out, DynamicPropertySchema{
			Prefix:    family.Prefix,
			Probeable: family.Probeable,
			Sensitive: family.Sensitive,
			Scopes:    scopes,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out
}

// RejectedPropertyFamilies returns the name families a table refuses, sorted by
// prefix, in the form the task catalog publishes. Exported so a consumer
// validating a recipe offline can answer a rejected name the way docket does -
// with the task that manages it - instead of reporting it as unknown.
func RejectedPropertyFamilies(table PropertyTable) []RejectedPropertySchema {
	if len(table.Rejected) == 0 {
		return nil
	}
	out := make([]RejectedPropertySchema, 0, len(table.Rejected))
	for _, family := range table.Rejected {
		out = append(out, RejectedPropertySchema{
			Prefix:      family.Prefix,
			Replacement: family.Replacement,
			Reason:      family.Reason,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out
}

// dynamicPropertyFamilyFor returns the family a property belongs to, if any.
func dynamicPropertyFamilyFor(plugin, property string) (dynamicPropertyFamily, bool) {
	for _, family := range dynamicPropertyFamilies[plugin] {
		if strings.HasPrefix(property, family.Prefix) {
			return family, true
		}
	}
	return dynamicPropertyFamily{}, false
}

// dynamicPropertyKeys returns the report keys for a dynamic property whose
// plugin surfaces the family in its `:report` payload, synthesized from the
// property name and the scopes the family declares. A family the plugin does
// not report has no keys to synthesize and falls through to the unprobed path.
func dynamicPropertyKeys(plugin, property string) (PropertyKeys, bool) {
	family, ok := dynamicPropertyFamilyFor(plugin, property)
	if !ok || !family.Probeable {
		return PropertyKeys{}, false
	}
	return family.keysFor(property), true
}

// propertyEntry returns the PropertyKeys a plugin uses for one property,
// falling back to the family's entry when the property belongs to a dynamic
// family the map cannot enumerate. Reading the map directly instead would
// report a `dns-provider-<KEY>` credential as non-Sensitive and export it in
// cleartext (#451). The fallback goes through the family rather than through
// dynamicPropertyKeys so a family docket cannot probe still answers the
// sensitivity question (#457).
func propertyEntry(plugin, property string, keys map[string]PropertyKeys) PropertyKeys {
	if entry, ok := keys[property]; ok {
		return entry
	}
	family, ok := dynamicPropertyFamilyFor(plugin, property)
	if !ok {
		return PropertyKeys{}
	}
	return family.keysFor(property)
}

// taskPropertyEntry is propertyEntry for a task that carries its own table, so
// the export path does not have to repeat the plugin name and the map beside
// the task type it already matched on.
func taskPropertyEntry(task PropertyTableDocer, property string) PropertyKeys {
	table := task.PropertyTable()
	return propertyEntry(table.Plugin(), property, table.Keys)
}

// withDynamicProperties returns keys plus a synthesized entry for every probeable
// dynamic property in props. The map is copied only when there is something to
// add, since the caller's map is a package-level singleton shared by every task
// of that plugin.
func withDynamicProperties(plugin string, keys map[string]PropertyKeys, props []string) map[string]PropertyKeys {
	var merged map[string]PropertyKeys
	for _, property := range props {
		entry, ok := dynamicPropertyKeys(plugin, property)
		if !ok {
			continue
		}
		if merged == nil {
			merged = make(map[string]PropertyKeys, len(keys)+len(props))
			for k, v := range keys {
				merged[k] = v
			}
		}
		merged[property] = entry
	}
	if merged == nil {
		return keys
	}
	return merged
}

// dynamicPropertiesFromReport returns the probeable dynamic property names a
// report payload carries for the given scope, sorted. In the app scope the
// payload also carries the `global-` and `computed-` variants of a key; only the
// bare row is the app's own value, so a key is kept only when the scope's
// synthesized lookup round-trips back to it.
func dynamicPropertiesFromReport(plugin string, payload map[string]string, global bool) []string {
	var props []string
	for key := range payload {
		property := key
		if global {
			property = strings.TrimPrefix(key, "global-")
			if property == key {
				continue
			}
		}
		entry, ok := dynamicPropertyKeys(plugin, property)
		if !ok {
			continue
		}
		lookup := entry.PerApp
		if global {
			lookup = entry.Global
		}
		if lookup != key {
			continue
		}
		props = append(props, property)
	}
	sort.Strings(props)
	return props
}

// planProperty is the shared Plan() implementation for property tasks. It
// probes the current value via getProperty using the task's PropertyKeys
// map, returns InSync when current matches desired, and otherwise embeds an
// apply closure that runs the underlying `dokku <subcommand>` call.
// ExecutePlan is the only invoker.
//
// When the probe errors (other than SSH transport failures), the apply
// closure runs the set/unset unconditionally. Diagnostic warnings are
// attached to PlanResult.Warnings via unknownPropertyWarning for typos and
// unsupported plugin versions (the run loop routes them through the emitter);
// other probe failures are recorded in PlanResult.Reason and the apply still
// runs, matching pre-probe behavior.
// validatePropertyInput checks a property task's inputs without probing the
// server: that the property is not from a family the table rejects, app/global
// scoping, that the property is supported for the target scope, and that a
// value is supplied only in the state that allows it. Both planProperty and
// each property task's Validate() call it so plan and validate report the same
// errors.
func validatePropertyInput(task PropertyTableDocer, state State, app string, global bool, property, value string) error {
	table := task.PropertyTable()
	// A rejected family is checked before anything else, including scoping:
	// the user wrote a name this task will never manage, so naming the task
	// that does is more use than telling them the app field is missing too.
	if family, ok := table.rejectedFamilyFor(property); ok {
		return family.err()
	}
	if !global && app == "" {
		return errors.New("app is required when global is false")
	}
	if global && app != "" {
		return fmt.Errorf("'app' must not be set when 'global' is set to true")
	}
	if err := validateProperty(table.Plugin(), property, global, table.Keys); err != nil {
		return err
	}
	if state == StatePresent && value == "" {
		return fmt.Errorf("setting a state of 'present' is invalid without a value for 'value'")
	}
	if state == StateAbsent && value != "" {
		return fmt.Errorf("setting a state of 'absent' is invalid with a value for 'value'")
	}
	return nil
}

func planProperty(ctx context.Context, task PropertyTableDocer, state State, app string, global bool, property, value string) PlanResult {
	if err := validatePropertyInput(task, state, app, global, property, value); err != nil {
		return planErr(err)
	}

	table := task.PropertyTable()
	keys := table.Keys
	subcommand := table.Subcommand
	plugin := table.Plugin()
	target := app
	if global {
		target = "--global"
	}

	// A dynamic property has no static map entry. When its plugin reports the
	// family, synthesize the entry so the probe path below runs against it; the
	// families that stay unreported keep falling through to the unprobed path.
	keys = withDynamicProperties(plugin, keys, []string{property})

	// A property flagged Sensitive carries a secret value. Register the desired
	// value now so the command echo is masked; empties are dropped. The
	// server-probed current value is registered after each probe below, since
	// it is not known from the recipe (a hand-written recipe never tags it, and
	// the `(was %q)` drift reason would otherwise leak the live server secret).
	//
	// propertyEntry answers this rather than the merged map, because the map
	// only ever gains an entry for a family docket can probe - reading it
	// directly would leave an unprobeable credential unmasked (#457).
	sensitive := propertyEntry(plugin, property, keys).Sensitive
	if sensitive {
		subprocess.MaskerFromContext(ctx).Add(value)
	}

	return DispatchPlan(state, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			// Dynamic property families have no probe key; treat as drift
			// and run the mutation unconditionally.
			if _, mapped := keys[property]; !mapped && isDynamicProperty(plugin, property) {
				return runUnprobedSet(ctx, subcommand, target, property, value)
			}

			// Probe; treat dokku-level failure as "drift, must mutate"
			// (matches pre-probe behavior for unsupported plugins) but
			// surface SSH transport failures so the user sees `! ssh:`.
			current, probeErr := getProperty(ctx, subcommand, app, global, property, keys)
			if sensitive {
				subprocess.MaskerFromContext(ctx).Add(current)
			}
			var warnings []PlanWarning
			if probeErr != nil {
				var sshErr *subprocess.SSHError
				if errors.As(probeErr, &sshErr) {
					return PlanResult{Status: PlanStatusError, Error: probeErr}
				}
				if w, ok := unknownPropertyWarning(plugin, property, probeErr); ok {
					warnings = append(warnings, w)
				}
			}
			if probeErr == nil && current == value {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}

			status := PlanStatusModify
			reason := fmt.Sprintf("would set %s on %s", property, target)
			if probeErr != nil {
				reason = fmt.Sprintf("would set %s on %s (probe failed: %v)", property, target, probeErr)
			} else if current == "" {
				status = PlanStatusCreate
				reason = fmt.Sprintf("%s missing on %s", property, target)
			} else {
				reason = fmt.Sprintf("%s drift on %s (was %q)", property, target, current)
			}

			inputs := propertySetInputs(subcommand, target, property, value)
			return PlanResult{
				InSync:    false,
				Status:    status,
				Reason:    reason,
				Mutations: []string{fmt.Sprintf("set %s=%s", property, value)},
				Commands:  resolveCommands(ctx, inputs),
				Warnings:  warnings,
				apply:     applyPropertySet(subcommand, target, property, value),
			}
		},
		StateAbsent: func() PlanResult {
			if _, mapped := keys[property]; !mapped && isDynamicProperty(plugin, property) {
				return runUnprobedUnset(ctx, subcommand, target, property)
			}

			current, probeErr := getProperty(ctx, subcommand, app, global, property, keys)
			if sensitive {
				subprocess.MaskerFromContext(ctx).Add(current)
			}
			var warnings []PlanWarning
			if probeErr != nil {
				var sshErr *subprocess.SSHError
				if errors.As(probeErr, &sshErr) {
					return PlanResult{Status: PlanStatusError, Error: probeErr}
				}
				if w, ok := unknownPropertyWarning(plugin, property, probeErr); ok {
					warnings = append(warnings, w)
				}
			}
			if probeErr == nil && current == "" {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}

			reason := fmt.Sprintf("would unset %s on %s", property, target)
			if probeErr != nil {
				reason = fmt.Sprintf("would unset %s on %s (probe failed: %v)", property, target, probeErr)
			} else {
				reason = fmt.Sprintf("would unset %s on %s (was %q)", property, target, current)
			}

			inputs := propertyUnsetInputs(subcommand, target, property)
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    reason,
				Mutations: []string{fmt.Sprintf("unset %s", property)},
				Commands:  resolveCommands(ctx, inputs),
				Warnings:  warnings,
				apply:     applyPropertyUnset(subcommand, target, property),
			}
		},
	})
}

// validateProperty rejects unsupported properties or scope mismatches before
// any subprocess call. A dynamic property family bypasses the name check, since
// its members can't be enumerated in the map, but not the scope check: the
// family declares its scopes and a member is held to them the same way a mapped
// property is, so an app-scoped traefik `dns-provider-*` is refused here rather
// than planning forever and failing on `traefik:set`'s own rejection.
func validateProperty(plugin, property string, global bool, keys map[string]PropertyKeys) error {
	entry, ok := keys[property]
	if !ok {
		family, dynamic := dynamicPropertyFamilyFor(plugin, property)
		if dynamic {
			return validatePropertyScope(plugin, property, global,
				family.scoped(PropertyScopeApp), family.scoped(PropertyScopeGlobal))
		}
		supported := make([]string, 0, len(keys))
		for k := range keys {
			supported = append(supported, k)
		}
		sort.Strings(supported)
		return fmt.Errorf("dokku %s: unsupported property %q (supported: %s)", plugin, property, strings.Join(supported, ", "))
	}
	return validatePropertyScope(plugin, property, global, entry.PerApp != "", entry.Global != "")
}

// validatePropertyScope rejects a property used in a scope it has no form in,
// matching dokku's own CLI rejection. Shared by the mapped and the dynamic arms
// of validateProperty so both report the same sentence: a mapped property says
// which scopes it has by leaving a PropertyKeys half empty, a dynamic family
// says it in Scopes, and the user sees no difference.
func validatePropertyScope(plugin, property string, global, hasApp, hasGlobal bool) error {
	if global && !hasGlobal {
		return fmt.Errorf("property %q on plugin %s has no global form", property, plugin)
	}
	if !global && !hasApp {
		return fmt.Errorf("property %q on plugin %s has no per-app form", property, plugin)
	}
	return nil
}

// runUnprobedSet returns a PlanResult that runs `:set` unconditionally for
// dynamic properties that have no probe key. Every family docket knows about is
// probeable today, so nothing reaches this; it is the other half of the
// Probeable contract, kept for the next plugin that takes a family it does not
// report. See dynamicPropertyFamilies.
func runUnprobedSet(ctx context.Context, subcommand, target, property, value string) PlanResult {
	inputs := propertySetInputs(subcommand, target, property, value)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusModify,
		Reason:    fmt.Sprintf("would set %s on %s (no probe key)", property, target),
		Mutations: []string{fmt.Sprintf("set %s=%s", property, value)},
		Commands:  resolveCommands(ctx, inputs),
		apply:     applyPropertySet(subcommand, target, property, value),
	}
}

// runUnprobedUnset returns a PlanResult that runs `:set` (no value, the unset
// form) unconditionally for dynamic properties with no probe key. Unreached for
// the same reason runUnprobedSet is.
func runUnprobedUnset(ctx context.Context, subcommand, target, property string) PlanResult {
	inputs := propertyUnsetInputs(subcommand, target, property)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("would unset %s on %s (no probe key)", property, target),
		Mutations: []string{fmt.Sprintf("unset %s", property)},
		Commands:  resolveCommands(ctx, inputs),
		apply:     applyPropertyUnset(subcommand, target, property),
	}
}

// propertySetInputs returns the subprocess inputs that set a property.
func propertySetInputs(subcommand, target, property, value string) []subprocess.ExecCommandInput {
	return []subprocess.ExecCommandInput{
		{Command: "dokku", Args: []string{"--quiet", subcommand, target, property, value}},
	}
}

// propertyUnsetInputs returns the subprocess inputs that unset a property.
func propertyUnsetInputs(subcommand, target, property string) []subprocess.ExecCommandInput {
	return []subprocess.ExecCommandInput{
		{Command: "dokku", Args: []string{"--quiet", subcommand, target, property}},
	}
}

// applyPropertySet returns a closure that runs `dokku <subcommand> <target>
// <property> <value>` and converts the result into a TaskOutputState.
func applyPropertySet(subcommand, target, property, value string) func(ctx context.Context) TaskOutputState {
	inputs := propertySetInputs(subcommand, target, property, value)
	return func(ctx context.Context) TaskOutputState {
		return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
	}
}

// applyPropertyUnset returns a closure that runs `dokku <subcommand> <target>
// <property>` (no value, which dokku interprets as unset) and converts the
// result into a TaskOutputState.
func applyPropertyUnset(subcommand, target, property string) func(ctx context.Context) TaskOutputState {
	inputs := propertyUnsetInputs(subcommand, target, property)
	return func(ctx context.Context) TaskOutputState {
		return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
	}
}
