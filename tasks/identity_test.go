package tasks

import (
	"reflect"
	"strings"
	"testing"
)

// TestIdentityAddressRendersDeclaredKeys covers the rendering rules the whole
// feature rests on: keys appear in declaration order, a key holding its zero
// value is omitted (which is what lets the mutually exclusive app / global
// pair render honestly), and a task with no key value at all falls back to the
// bare type key.
func TestIdentityAddressRendersDeclaredKeys(t *testing.T) {
	for _, tt := range []struct {
		name    string
		typeKey string
		task    Task
		want    string
	}{
		{
			name:    "single key",
			typeKey: "dokku_app",
			task:    AppTask{App: "api"},
			want:    "dokku_app[app=api]",
		},
		{
			name:    "several keys in declaration order",
			typeKey: "dokku_service_property",
			task:    ServicePropertyTask{Service: "postgres", Name: "db", Property: "max-connections", Value: "100"},
			want:    "dokku_service_property[service=postgres,name=db,property=max-connections]",
		},
		{
			name:    "app scope omits the unset global flag",
			typeKey: "dokku_apps_property",
			task:    AppsPropertyTask{App: "api", Property: "deploy-source"},
			want:    "dokku_apps_property[app=api,property=deploy-source]",
		},
		{
			name:    "global scope omits the empty app",
			typeKey: "dokku_apps_property",
			task:    AppsPropertyTask{Global: true, Property: "disable-autocreation"},
			want:    "dokku_apps_property[global=true,property=disable-autocreation]",
		},
		{
			name:    "no key value renders the bare type key",
			typeKey: "dokku_domains",
			task:    DomainsTask{Domains: []string{"example.com"}},
			want:    "dokku_domains",
		},
		{
			name:    "an int key renders as a number",
			typeKey: "dokku_ports",
			task:    PortsTask{App: "api"},
			want:    "dokku_ports[app=api]",
		},
		{
			name:    "the desired state is not part of the address",
			typeKey: "dokku_config",
			task:    ConfigTask{App: "api", State: StateAbsent},
			want:    "dokku_config[app=api]",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := IdentityAddress(tt.typeKey, tt.task); got != tt.want {
				t.Errorf("IdentityAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestIdentityAddressQuotesOnlyWhatItMust pins the quoting rule. The address
// is a parseable surface, so a value that would break the parse is quoted -
// and nothing else is. A docker option is the motivating case: it is full of
// `=`, spaces, and colons, none of which need quoting, and quoting them all
// would make every dokku_docker_options task name unreadable.
func TestIdentityAddressQuotesOnlyWhatItMust(t *testing.T) {
	for _, tt := range []struct {
		name string
		task DockerOptionsTask
		want string
	}{
		{
			name: "equals and spaces stay bare",
			task: DockerOptionsTask{App: "api", Phase: "deploy", Option: "-v /var/run/docker.sock:/var/run/docker.sock"},
			want: "dokku_docker_options[app=api,phase=deploy,option=-v /var/run/docker.sock:/var/run/docker.sock]",
		},
		{
			name: "a comma is quoted because it separates keys",
			task: DockerOptionsTask{App: "api", Phase: "run", Option: "-e FOO=bar,BAZ=qux"},
			want: `dokku_docker_options[app=api,phase=run,option="-e FOO=bar,BAZ=qux"]`,
		},
		{
			name: "a closing bracket is quoted because it ends the address",
			task: DockerOptionsTask{App: "api", Phase: "build", Option: "--label a]b"},
			want: `dokku_docker_options[app=api,phase=build,option="--label a]b"]`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := IdentityAddress("dokku_docker_options", tt.task)
			if got != tt.want {
				t.Fatalf("IdentityAddress() = %q, want %q", got, tt.want)
			}
			// Whatever the quoting, the address has to survive a round trip:
			// it is what --start-at-task and export --resource are handed.
			typeKey, keys, err := ParseIdentityAddress(got)
			if err != nil {
				t.Fatalf("ParseIdentityAddress(%q) errored: %v", got, err)
			}
			if typeKey != "dokku_docker_options" {
				t.Errorf("type key = %q, want dokku_docker_options", typeKey)
			}
			if keys["option"] != tt.task.Option {
				t.Errorf("option = %q, want %q", keys["option"], tt.task.Option)
			}
		})
	}
}

// TestParseIdentityAddress covers the accepted forms and the rejected ones. A
// bare type key is legal and means "every resource of this type"; a task name
// carrying a collision ordinal or a loop suffix is not an address and must be
// rejected rather than silently truncated to one.
func TestParseIdentityAddress(t *testing.T) {
	for _, tt := range []struct {
		name     string
		address  string
		wantType string
		wantKeys map[string]string
		wantErr  string
	}{
		{
			name:     "bare type key",
			address:  "dokku_config",
			wantType: "dokku_config",
			wantKeys: map[string]string{},
		},
		{
			name:     "one key",
			address:  "dokku_config[app=api]",
			wantType: "dokku_config",
			wantKeys: map[string]string{"app": "api"},
		},
		{
			name:     "several keys",
			address:  "dokku_service_property[service=postgres,name=db,property=x]",
			wantType: "dokku_service_property",
			wantKeys: map[string]string{"service": "postgres", "name": "db", "property": "x"},
		},
		{
			name:     "quoted value holding a comma",
			address:  `dokku_docker_options[app=api,option="-e A=1,B=2"]`,
			wantType: "dokku_docker_options",
			wantKeys: map[string]string{"app": "api", "option": "-e A=1,B=2"},
		},
		{
			name:     "quoted empty value",
			address:  `dokku_domains[app=""]`,
			wantType: "dokku_domains",
			wantKeys: map[string]string{"app": ""},
		},
		{
			name:    "collision ordinal is a task name, not an address",
			address: "dokku_config[app=api] #2",
			wantErr: "missing closing",
		},
		{
			name:    "loop suffix is a task name, not an address",
			address: "dokku_app (item=api)",
			wantErr: "expected <task_type>",
		},
		{
			name:    "empty",
			address: "   ",
			wantErr: "is empty",
		},
		{
			name:    "no keys between the brackets",
			address: "dokku_config[]",
			wantErr: "holds no keys",
		},
		{
			name:    "missing type",
			address: "[app=api]",
			wantErr: "missing task type",
		},
		{
			name:    "not key=value",
			address: "dokku_config[app]",
			wantErr: "is not key=value",
		},
		{
			name:    "repeated key",
			address: "dokku_config[app=a,app=b]",
			wantErr: "appears twice",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			typeKey, keys, err := ParseIdentityAddress(tt.address)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got none", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if typeKey != tt.wantType {
				t.Errorf("type key = %q, want %q", typeKey, tt.wantType)
			}
			if !reflect.DeepEqual(keys, tt.wantKeys) {
				t.Errorf("keys = %#v, want %#v", keys, tt.wantKeys)
			}
		})
	}
}

// TestIdentityMatches covers the predicate `export --resource` filters emitted
// bodies with: an empty request matches everything, a partial request matches
// on the keys it named, and a key the task does not declare never matches.
func TestIdentityMatches(t *testing.T) {
	task := AppsPropertyTask{App: "api", Property: "deploy-source", Value: "git"}

	for _, tt := range []struct {
		name string
		want map[string]string
		ok   bool
	}{
		{name: "no keys matches everything", want: nil, ok: true},
		{name: "matching subset", want: map[string]string{"app": "api"}, ok: true},
		{name: "all keys", want: map[string]string{"app": "api", "property": "deploy-source"}, ok: true},
		{name: "wrong value", want: map[string]string{"app": "web"}, ok: false},
		{name: "unset key requested", want: map[string]string{"global": "true"}, ok: false},
		{name: "undeclared key", want: map[string]string{"nope": "x"}, ok: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := IdentityMatches(task, tt.want); got != tt.ok {
				t.Errorf("IdentityMatches(%#v) = %v, want %v", tt.want, got, tt.ok)
			}
		})
	}
}

// TestTaskIdentityCollections covers the three item-identity shapes: a scalar
// slice identified by value, a map identified by its key, and a struct slice
// whose element declares its own keys.
func TestTaskIdentityCollections(t *testing.T) {
	for _, tt := range []struct {
		name     string
		task     Task
		field    string
		item     ItemIdentity
		itemKeys []string
	}{
		{name: "value-identified set", task: DomainsTask{}, field: "domains", item: ItemIdentityValue},
		{name: "map-identified pairs", task: ConfigTask{}, field: "config", item: ItemIdentityMapKey},
		{name: "struct items", task: PortsTask{}, field: "port_mappings", item: ItemIdentityFields, itemKeys: []string{"scheme", "host"}},
		{name: "struct items with one key", task: HttpAuthUserTask{}, field: "users", item: ItemIdentityFields, itemKeys: []string{"username"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			collections := TaskIdentityCollections(tt.task)
			if len(collections) != 1 {
				t.Fatalf("got %d collections, want 1", len(collections))
			}
			got := collections[0]
			if got.YAMLName != tt.field {
				t.Errorf("field = %q, want %q", got.YAMLName, tt.field)
			}
			if got.Item != tt.item {
				t.Errorf("item identity = %q, want %q", got.Item, tt.item)
			}
			if tt.itemKeys != nil && !reflect.DeepEqual(got.ItemKeys, tt.itemKeys) {
				t.Errorf("item keys = %v, want %v", got.ItemKeys, tt.itemKeys)
			}
		})
	}
}

// TestTaskIdentityReportsUnsetKeys asserts that a prototype read from the
// registry still reports its declared keys, with Present false. The docs
// generator relies on this: it renders the Identity section from prototypes,
// which hold no values.
func TestTaskIdentityReportsUnsetKeys(t *testing.T) {
	keys := TaskIdentity(RegisteredTasks["dokku_apps_property"])
	if len(keys) != 3 {
		t.Fatalf("got %d keys, want 3", len(keys))
	}
	for _, key := range keys {
		if key.Present {
			t.Errorf("key %q reports Present on a zero-valued prototype", key.YAMLName)
		}
	}
	if got := IdentityKeyNames(RegisteredTasks["dokku_apps_property"]); !reflect.DeepEqual(got, []string{"app", "global", "property"}) {
		t.Errorf("IdentityKeyNames() = %v, want [app global property]", got)
	}
}
