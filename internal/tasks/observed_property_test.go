package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

func init() {
	registerObservedFixtures(observedPropertyFixtures)
}

// observedPropertyFixtures covers every task that plans through planProperty,
// generated from each task's own property table, and dokku_service_property,
// which reads its properties from `<service>:info` instead.
func observedPropertyFixtures() map[string]observedFixture {
	out := map[string]observedFixture{}
	for name, task := range allRegisteredTasks() {
		docer, ok := task.(PropertyTableDocer)
		if !ok {
			continue
		}
		out[name] = propertyTaskFixture(name, docer)
	}
	out["dokku_service_property"] = servicePropertyObservedFixture()
	return out
}

// propertyTaskFixture builds the fixture for one property task from its table.
// App "web" has one property set and global has another, so the export emits
// one body per scope. App "gone" reports the app property with no value, which
// is how dokku reports one that was never set; a task with no per-app form
// gets its absent case from a second global property reported empty instead.
func propertyTaskFixture(typeKey string, docer PropertyTableDocer) observedFixture {
	table := docer.propertyTable()
	plugin := table.Plugin()
	appProps, globalProps := propertiesByScopeForTest(table)

	report := func(target string, payload map[string]string) (string, string) {
		body, err := json.Marshal(payload)
		if err != nil {
			panic(err)
		}
		return fmt.Sprintf("--quiet %s:report %s --format json", plugin, target), string(body)
	}

	rt := registeredTypeOrNil(typeKey)
	build := func(f PropertyFields) Task {
		return reflect.ValueOf(f).Convert(rt).Interface().(Task)
	}

	appPayload := map[string]string{}
	gonePayload := map[string]string{}
	globalPayload := map[string]string{}
	var cases []observedCase
	if len(appProps) > 0 {
		prop := appProps[0]
		appPayload[table.Keys[prop].PerApp] = "app-value"
		gonePayload[table.Keys[prop].PerApp] = ""
		cases = append(cases,
			observedCase{
				name: "absent",
				task: build(PropertyFields{App: "gone", Property: prop, State: StatePresent, Value: "wanted"}),
				want: build(PropertyFields{App: "gone", Property: prop, State: StateAbsent}),
			},
			observedCase{
				name:   "drifted, planned absent",
				task:   build(PropertyFields{App: "web", Property: prop, State: StateAbsent}),
				want:   build(PropertyFields{App: "web", Property: prop, Value: "app-value", State: StatePresent}),
				exists: true,
			},
		)
	}
	if len(globalProps) > 0 {
		prop := globalProps[0]
		globalPayload[table.Keys[prop].Global] = "global-value"
		cases = append(cases, observedCase{
			name:   "global",
			task:   build(PropertyFields{Global: true, Property: prop, Value: "other", State: StatePresent}),
			want:   build(PropertyFields{Global: true, Property: prop, Value: "global-value", State: StatePresent}),
			exists: true,
		})
		if len(appProps) == 0 {
			if len(globalProps) < 2 {
				panic(fmt.Sprintf("%s: a global-only table needs two properties for an absent case", typeKey))
			}
			unset := globalProps[1]
			globalPayload[table.Keys[unset].Global] = ""
			cases = append(cases, observedCase{
				name: "absent",
				task: build(PropertyFields{Global: true, Property: unset, Value: "wanted", State: StatePresent}),
				want: build(PropertyFields{Global: true, Property: unset, State: StateAbsent}),
			})
		}
	}

	responses := map[string]string{}
	for target, payload := range map[string]map[string]string{"web": appPayload, "gone": gonePayload, "--global": globalPayload} {
		k, v := report(target, payload)
		responses[k] = v
	}

	appExporter := docer.(AppExporter)
	globalExporter := docer.(GlobalExporter)
	return observedFixture{
		responses: responses,
		export: func(ctx context.Context) ([]interface{}, error) {
			bodies, err := appExporter.ExportApp(ctx, "web")
			if err != nil {
				return nil, err
			}
			global, err := globalExporter.ExportGlobal(ctx)
			if err != nil {
				return nil, err
			}
			return append(bodies, global...), nil
		},
		cases: cases,
	}
}

// propertiesByScopeForTest returns the table's property names, sorted, that
// have a per-app form and those that have a global form.
func propertiesByScopeForTest(table PropertyTable) (appProps, globalProps []string) {
	names := make([]string, 0, len(table.Keys))
	for name := range table.Keys {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := table.Keys[name]
		if entry.PerApp != "" {
			appProps = append(appProps, name)
		}
		if entry.Global != "" {
			globalProps = append(globalProps, name)
		}
	}
	return appProps, globalProps
}

// servicePropertyObservedFixture is one postgres service with a restart
// policy set, expose-mode at its unset default, and a backup credential that
// info reports only by fingerprint.
func servicePropertyObservedFixture() observedFixture {
	return observedFixture{
		responses: mergeFixtures(
			map[string]string{
				"--quiet plugin:trigger service-list": "postgres:db",
				"--quiet postgres:exists db":          "",
			},
			infoFixture("postgres", "db", map[string]string{
				"restart-policy":                   "always",
				"expose-mode":                      "ambassador",
				"log-driver":                       "",
				"backup-access-key-id-fingerprint": backupFingerprint("AKIA"),
			}),
		),
		export: exportGlobalFrom(ServicePropertyTask{}),
		cases: []observedCase{
			{
				name: "absent",
				task: ServicePropertyTask{Service: "postgres", Name: "db", Property: "log-driver", Value: "json-file", State: StatePresent},
				want: ServicePropertyTask{Service: "postgres", Name: "db", Property: "log-driver", State: StateAbsent},
			},
			{
				name: "unset default reads as absent",
				task: ServicePropertyTask{Service: "postgres", Name: "db", Property: "expose-mode", State: StateAbsent},
				want: ServicePropertyTask{Service: "postgres", Name: "db", Property: "expose-mode", State: StateAbsent},
			},
			{
				name:   "drifted, planned absent",
				task:   ServicePropertyTask{Service: "postgres", Name: "db", Property: "restart-policy", State: StateAbsent},
				want:   ServicePropertyTask{Service: "postgres", Name: "db", Property: "restart-policy", Value: "always", State: StatePresent},
				exists: true,
			},
			{
				name:   "fingerprint matches the declared credential",
				task:   ServicePropertyTask{Service: "postgres", Name: "db", Property: "backup-access-key-id", Value: "AKIA", State: StatePresent},
				want:   ServicePropertyTask{Service: "postgres", Name: "db", Property: "backup-access-key-id", Value: "AKIA", State: StatePresent},
				exists: true,
			},
			{
				name:   "fingerprint of another credential",
				task:   ServicePropertyTask{Service: "postgres", Name: "db", Property: "backup-access-key-id", Value: "OTHER", State: StatePresent},
				want:   ServicePropertyTask{Service: "postgres", Name: "db", Property: "backup-access-key-id", State: StatePresent},
				exists: true,
			},
		},
	}
}
