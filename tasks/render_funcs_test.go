package tasks

import (
	"testing"

	_ "github.com/gliderlabs/sigil/builtin"
)

func TestYAMLScalar(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"billing", "billing"},
		// A quote or apostrophe mid-value is a valid plain scalar, so it
		// stays unquoted; yaml.Marshal quotes only when it must.
		{`ab"cd`, `ab"cd`},
		{"has'apostrophe", "has'apostrophe"},
		{"foo: bar", "'foo: bar'"},
		{"@web", "'@web'"},
		{"true", `"true"`},
		{"", `""`},
	}
	for _, tc := range cases {
		got, err := YAMLScalar(tc.in)
		if err != nil {
			t.Fatalf("YAMLScalar(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("YAMLScalar(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestJSONScalar(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"billing", `"billing"`},
		{`say "hi"`, `"say \"hi\""`},
		{"line1\nline2", `"line1\nline2"`},
	}
	for _, tc := range cases {
		got, err := JSONScalar(tc.in)
		if err != nil {
			t.Fatalf("JSONScalar(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("JSONScalar(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDoubleQuoteEscape(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"plain", "plain"},
		{`ab"cd`, `ab\"cd`},
		{`say "hi"`, `say \"hi\"`},
		{"line1\nline2", `line1\nline2`},
		{`back\slash`, `back\\slash`},
		{"a<b>&c", "a<b>&c"},
	}
	for _, tc := range cases {
		got, err := DoubleQuoteEscape(tc.in)
		if err != nil {
			t.Fatalf("DoubleQuoteEscape(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("DoubleQuoteEscape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// A missing key arrives as nil and must render as empty, not error, so the
	// filter survives the empty-context render used for input discovery.
	if got, err := DoubleQuoteEscape(nil); err != nil || got != "" {
		t.Errorf("DoubleQuoteEscape(nil) = %q, %v; want \"\", nil", got, err)
	}
	// The apply/plan context stores input values as *string, and sigil passes
	// that pointer to the filter verbatim; it must be dereferenced, not
	// formatted as an address.
	s := `say "hi"`
	if got, err := DoubleQuoteEscape(&s); err != nil || got != `say \"hi\"` {
		t.Errorf("DoubleQuoteEscape(*string) = %q, %v; want %q", got, err, `say \"hi\"`)
	}
	var nilPtr *string
	if got, err := DoubleQuoteEscape(nilPtr); err != nil || got != "" {
		t.Errorf("DoubleQuoteEscape((*string)(nil)) = %q, %v; want \"\", nil", got, err)
	}
}

// TestGetTasksDoubleQuoteEscapePointerValue guards the #371 apply path: the
// real apply/plan context holds *string values (pflag hands back pointers), and
// the dq filter must dereference them rather than render the pointer address.
func TestGetTasksDoubleQuoteEscapePointerValue(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: configure
      dokku_config:
        app: test-app
        config:
          MOTD: "{{ .motd | dq }}"
`)
	motd := `say "hi"`
	context := map[string]interface{}{"motd": &motd}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks with *string context failed: %v", err)
	}
	task := tasks.Get("configure")
	if task == nil {
		t.Fatal("task 'configure' not found")
	}
	configTask, ok := task.(*ConfigTask)
	if !ok {
		ct, ok2 := task.(ConfigTask)
		if !ok2 {
			t.Fatalf("task is not a ConfigTask (type is %T)", task)
		}
		configTask = &ct
	}
	if got := configTask.Config["MOTD"]; got != motd {
		t.Errorf("Config[MOTD] = %q, want %q", got, motd)
	}
}

// TestGetTasksDoubleQuoteEscapeSpecialValue is the #371 regression: an input
// value containing characters that would break the surrounding double-quoted
// scalar must render into a task body correctly when piped through `dq`. A
// config value is used because it (unlike an app name) legitimately holds
// arbitrary characters.
func TestGetTasksDoubleQuoteEscapeSpecialValue(t *testing.T) {
	for _, value := range []string{`say "hi"`, `he said "don't"`, "line1\nline2"} {
		t.Run(value, func(t *testing.T) {
			data := []byte(`---
- tasks:
    - name: configure
      dokku_config:
        app: test-app
        config:
          MOTD: "{{ .motd | dq }}"
`)
			context := map[string]interface{}{"motd": value}

			tasks, err := GetTasks(data, context)
			if err != nil {
				t.Fatalf("GetTasks with dq filter failed for %q: %v", value, err)
			}

			task := tasks.Get("configure")
			if task == nil {
				t.Fatal("task 'configure' not found")
			}
			configTask, ok := task.(*ConfigTask)
			if !ok {
				ct, ok2 := task.(ConfigTask)
				if !ok2 {
					t.Fatalf("task is not a ConfigTask (type is %T)", task)
				}
				configTask = &ct
			}
			if got := configTask.Config["MOTD"]; got != value {
				t.Errorf("Config[MOTD] = %q, want %q", got, value)
			}
		})
	}
}
