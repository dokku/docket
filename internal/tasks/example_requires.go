package tasks

import (
	"fmt"
	"reflect"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// Every documented example is a single task naming its app or service by a
// literal, as if the object already existed. An embedder that turns the
// examples into runnable configurations, such as a Terraform provider, used
// to work out from objects[].keys and creates which task makes each object,
// and could not tell when an example also needed a plugin, a deployed app or
// some other resource (#652). Each example now publishes the tasks it needs
// applied first, and the example integration tests set up from that same
// list.
//
// Most of the list is derived: TaskRequires adds a dokku_plugin for every
// plugin the task's plugin requirements name, and the creating task for every
// object the task names but does not create, each preceded by what it needs
// in turn. An example that needs more declares it with a Requires field on
// its example struct, and the derived requirements of those declared tasks are
// added before them.

// ExampleRequirementSchema is one task an example needs applied before it, in
// the same form as the example: the task type and its YAML snippet.
type ExampleRequirementSchema struct {
	// Type is the task type, the YAML snippet's only top-level key.
	Type string `json:"type"`

	// YAML is the task, ready to paste into a recipe's task list ahead of the
	// example.
	YAML string `json:"yaml"`
}

// ExampleRequirer is implemented by an example struct that declares tasks its
// example needs beyond the ones TaskRequires derives, such as a deployed app
// or a resource the example names by a field that identifies no object.
type ExampleRequirer interface {
	GetRequires() []Task
}

// objectCreators names the task that creates each object kind from nothing
// but the keys naming it. dokku_app_clone also creates an app, but needs a
// source app too, so it is not the one a requirement uses.
// TestObjectCreatorsCreateTheirKind pins each entry to the task's own
// ObjectEffects.
var objectCreators = map[ObjectKind]string{
	ObjectApp:     "dokku_app",
	ObjectService: "dokku_service_create",
}

// pluginRepositories are the git URLs a dokku_plugin requirement installs each
// fixed-name plugin from. A datastore plugin, whose name comes from a task's
// service type, installs from datastorePluginRepository instead.
// TestPluginRepositoriesCoverRequirements fails when a task names a plugin
// missing here, and TestPluginRepositoriesMatchCIPins holds them to the
// repositories the setup-dokku action installs.
var pluginRepositories = map[string]string{
	"acl":         "https://github.com/dokku-community/dokku-acl.git",
	"global-cert": "https://github.com/dokku-community/dokku-global-cert.git",
	"http-auth":   "https://github.com/dokku/dokku-http-auth.git",
	"letsencrypt": "https://github.com/dokku/dokku-letsencrypt.git",
	"maintenance": "https://github.com/dokku/dokku-maintenance.git",
}

// exampleDeployImage is the image an example's deployed-app requirement
// deploys: dokku's own smoke-test app, which needs no build.
const exampleDeployImage = "dokku/smoke-test-app:dockerfile"

// deployedExampleApp is the requirement of an example that acts on a running
// app, such as a service link or a letsencrypt certificate: app deployed from
// exampleDeployImage. TaskRequires adds the dokku_app creating it.
func deployedExampleApp(app string) Task {
	return GitFromImageTask{App: app, Image: exampleDeployImage}
}

// datastorePluginRepository is the git URL of the official datastore plugin
// for a service type, such as dokku-redis for redis.
func datastorePluginRepository(service string) string {
	return fmt.Sprintf("https://github.com/dokku/dokku-%s.git", service)
}

// TaskRequires returns the tasks t needs applied before it, in the order to
// apply them, the same derivation the catalog publishes as each example's
// `requires`: a dokku_plugin installing each non-core plugin t's plugin
// requirements name, then the task creating each object t names but does not
// create itself - dokku_app for an app, dokku_service_create for a service -
// each preceded by the tasks it needs in turn. A task needing nothing returns
// nil.
//
// The list covers what docket can derive from the task's declarations alone.
// An example that needs more, such as a deployed app or a storage entry it
// names, declares it, and the catalog publishes both.
//
//sdk:export
func TaskRequires(t Task) []Task {
	var list requirementList
	list.addDependencies(t)
	return list.tasks
}

// requirementList collects required tasks in apply order, each once.
type requirementList struct {
	tasks []Task
	seen  map[string]bool
}

// add appends t after the tasks it needs, unless an identical requirement is
// already listed. Two requirements are the same when they share a type and
// an identity address.
func (l *requirementList) add(t Task) {
	l.addDependencies(t)
	key := IdentityAddress(typeKeyOf(t), t)
	if l.seen[key] {
		return
	}
	if l.seen == nil {
		l.seen = map[string]bool{}
	}
	l.seen[key] = true
	l.tasks = append(l.tasks, t)
}

// addDependencies adds the requirements TaskRequires derives for t, but not
// t itself.
func (l *requirementList) addDependencies(t Task) {
	if support, ok := TaskMinimumVersions(t); ok {
		for _, plugin := range support.Plugins {
			if !pluginApplies(t, plugin) {
				continue
			}
			name, ok := pluginListName(t, plugin)
			if !ok {
				continue
			}
			url := pluginRepositories[name]
			if plugin.NameField != "" {
				url = datastorePluginRepository(name)
			}
			l.add(&PluginTask{Name: name, URL: url})
		}
	}
	for _, object := range TaskObjects(t) {
		if !object.Set || object.Creates {
			continue
		}
		if creator := objectCreator(object); creator != nil {
			l.add(creator)
		}
	}
}

// objectCreator builds the task that creates object, with the creator's own
// keys for the object's roles set to the object's values. Nil when the kind
// has no creator.
func objectCreator(object TaskObject) Task {
	typeKey, ok := objectCreators[object.Kind]
	if !ok {
		return nil
	}
	v, err := allocateTask(typeKey)
	if err != nil {
		return nil
	}
	task, ok := v.Interface().(Task)
	if !ok {
		return nil
	}
	for _, created := range TaskObjects(task) {
		if created.Kind != object.Kind {
			continue
		}
		for i, key := range created.Keys {
			field, ok := fieldByRecipeKey(v.Elem(), key)
			if ok && field.Kind() == reflect.String && i < len(object.Values) {
				field.SetString(object.Values[i])
			}
		}
	}
	return task
}

// exampleRequires is the requirement list of one example: what TaskRequires
// derives for the example's own task, then each task the example declares,
// each after what it needs in turn.
func exampleRequires(example Task, declared []Task) []Task {
	var list requirementList
	list.addDependencies(example)
	for _, task := range declared {
		list.add(task)
	}
	return list.tasks
}

// buildExampleRequires renders an example's requirement list for the
// catalog. The example is decoded from its published YAML, defaults applied,
// so the derivation sees what a recipe holding the snippet would run.
func buildExampleRequires(example Doc) ([]ExampleRequirementSchema, error) {
	task, err := decodeExampleCodeblock(example.Codeblock)
	if err != nil {
		return nil, fmt.Errorf("example %q: %w", example.Name, err)
	}
	var out []ExampleRequirementSchema
	for _, required := range exampleRequires(task, example.Requires) {
		schema, err := buildRequirementSchema(required)
		if err != nil {
			return nil, fmt.Errorf("example %q: %w", example.Name, err)
		}
		out = append(out, schema)
	}
	return out, nil
}

// buildRequirementSchema renders one required task as the catalog publishes
// it: its type, and its snippet keyed by that type the way an example is.
func buildRequirementSchema(required Task) (ExampleRequirementSchema, error) {
	typeKey := typeKeyOf(required)
	if typeKey == "" {
		return ExampleRequirementSchema{}, fmt.Errorf("required task %T is not a registered task type", required)
	}
	b, err := yaml.Marshal(map[string]Task{typeKey: required})
	if err != nil {
		return ExampleRequirementSchema{}, fmt.Errorf("required %s: %w", typeKey, err)
	}
	return ExampleRequirementSchema{
		Type: typeKey,
		YAML: strings.TrimSpace(string(b)) + "\n",
	}, nil
}

// decodeExampleCodeblock decodes a single-task snippet, keyed by its task
// type, the way the loader decodes one task of a recipe.
func decodeExampleCodeblock(codeblock string) (Task, error) {
	var body map[string]yaml.Node
	if err := yaml.Unmarshal([]byte(codeblock), &body); err != nil {
		return nil, err
	}
	if len(body) != 1 {
		return nil, fmt.Errorf("expected exactly one task type key, got %d", len(body))
	}
	for typeKey, node := range body {
		raw, err := yaml.Marshal(&node)
		if err != nil {
			return nil, err
		}
		return decodeTaskBytes(typeKey, raw)
	}
	return nil, nil
}
