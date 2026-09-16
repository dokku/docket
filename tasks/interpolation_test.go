package tasks

import (
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// TestRiskyInterpolations pins which actions a rewrite has to protect.
//
// The split is between an action that substitutes text from outside the
// recipe unescaped - where the surrounding quotes decide what the value may
// contain - and one that either emits only literal recipe text or has
// already been escaped with dq, where re-quoting is harmless or a fix.
func TestRiskyInterpolations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  []string
	}{
		{"plain reference", "{{ .app }}", []string{"{{ .app }}"}},
		{"reference mid-string", "web-{{ .app }}.example.com", []string{"{{ .app }}"}},
		{"filtered but not escaped", "{{ .app | upper }}", []string{"{{ .app | upper }}"}},
		{"two references", "{{ .a }}-{{ .b }}", []string{"{{ .a }}", "{{ .b }}"}},
		{"value beside a conditional", "{{ .a }}{{ if .b }}x{{ end }}", []string{"{{ .a }}"}},
		{"trim markers", "{{- .app -}}", []string{"{{- .app -}}"}},
		{"no spaces", "{{.app}}", []string{"{{.app}}"}},
		{"pipe inside a string literal", `{{ printf "a|b" .x }}`, []string{`{{ printf "a|b" .x }}`}},

		{"no interpolation", "web", nil},
		{"dq filtered", "{{ .app | dq }}", nil},
		{"dq as a function call", "{{ dq .app }}", nil},
		{"dq at the end of a pipeline", "{{ .app | upper | dq }}", nil},
		{"dq with no spaces", "{{.app|dq}}", nil},
		{"dq with trim markers", "{{- .app | dq -}}", nil},
		{"conditional only", "web{{ if .debug }}-verbose{{ end }}", nil},
		{"range only", "{{ range .xs }}x{{ end }}", nil},
		{"template comment", "{{/* a note */}}", nil},
		{"unterminated action", "{{ .app", nil},
		{"dq named inside a string literal", `{{ printf "| dq" .x | dq }}`, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := riskyInterpolations(tc.value)
			if len(got) != len(tc.want) {
				t.Fatalf("riskyInterpolations(%q) = %q, want %q", tc.value, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("action %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestYAMLQuotingSitesNamesTheStyle covers the four scalar styles that do
// not survive being rewritten double-quoted, and the one that does.
func TestYAMLQuotingSitesNamesTheStyle(t *testing.T) {
	t.Parallel()

	const recipe = `---
- tasks:
    - dokku_config:
        SINGLE: '{{ .a }}'
        PLAIN: web{{ .b }}
        LITERAL: |
          {{ .c }}
        FOLDED: >
          {{ .d }}
        DOUBLE: "{{ .e }}"
`

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(recipe), &root); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	sites := yamlQuotingSites(documentBody(&root))

	want := []quotingSite{
		{Line: 4, Action: "{{ .a }}", Style: "single-quoted"},
		{Line: 5, Action: "{{ .b }}", Style: "unquoted"},
		{Line: 6, Action: "{{ .c }}", Style: "in a literal block scalar"},
		{Line: 8, Action: "{{ .d }}", Style: "in a folded block scalar"},
	}
	if len(sites) != len(want) {
		t.Fatalf("sites = %+v, want %d of them", sites, len(want))
	}
	for i, w := range want {
		if sites[i] != w {
			t.Errorf("site %d = %+v, want %+v", i, sites[i], w)
		}
	}
}

// TestYAMLQuotingSitesReadsMappingKeys covers a key carrying an
// interpolation, which is substituted exactly the way a value is.
func TestYAMLQuotingSitesReadsMappingKeys(t *testing.T) {
	t.Parallel()

	var root yaml.Node
	if err := yaml.Unmarshal([]byte("---\n- tasks:\n    - dokku_config:\n        '{{ .key }}': web\n"), &root); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	sites := yamlQuotingSites(documentBody(&root))
	if len(sites) != 1 || sites[0].Action != "{{ .key }}" {
		t.Fatalf("sites = %+v, want the key reported once", sites)
	}
}

// TestJSON5QuotingSites is the JSON5 half: only the single-quoted string
// qualifies, since the double-quoted one is already what every rewrite
// produces and an unquoted key cannot hold an interpolation.
func TestJSON5QuotingSites(t *testing.T) {
	t.Parallel()

	const recipe = `[
  {
    tasks: [
      {
        dokku_config: {
          SINGLE: '{{ .a }}',
          DOUBLE: "{{ .b }}",
          ESCAPED: '{{ .c | dq }}',
          QUOTE_IN_VALUE: "it's '{{ .d }}' here",
        },
      },
    ],
  },
]
`

	sites, err := json5QuotingSites([]byte(recipe))
	if err != nil {
		t.Fatalf("json5QuotingSites: %v", err)
	}
	want := []quotingSite{{Line: 6, Action: "{{ .a }}", Style: "single-quoted"}}
	if len(sites) != len(want) {
		t.Fatalf("sites = %+v, want %+v", sites, want)
	}
	if sites[0] != want[0] {
		t.Errorf("site = %+v, want %+v", sites[0], want[0])
	}
}

// TestUnportableQuotingErrorWording covers the three shapes the message
// takes: one site, several, and an action that has no double-quoted
// spelling at all.
func TestUnportableQuotingErrorWording(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		sites  []quotingSite
		want   []string
		refute []string
	}{
		{
			name:  "one site carries the rewritten spelling",
			sites: []quotingSite{{Line: 7, Action: "{{ .app }}", Style: "single-quoted"}},
			want:  []string{"line 7", "`{{ .app }}`", "single-quoted", `"{{ .app | dq }}"`},
		},
		{
			name: "a filtered action keeps its filters in the suggestion",
			sites: []quotingSite{
				{Line: 3, Action: "{{ .app | upper }}", Style: "unquoted"},
			},
			want: []string{`"{{ .app | upper | dq }}"`},
		},
		{
			name: "trim markers stay against the delimiters",
			sites: []quotingSite{
				{Line: 3, Action: "{{- .app -}}", Style: "unquoted"},
			},
			want: []string{`"{{- .app | dq -}}"`},
		},
		{
			name: "several sites are counted and listed",
			sites: []quotingSite{
				{Line: 4, Action: "{{ .a }}", Style: "single-quoted"},
				{Line: 9, Action: "{{ .b }}", Style: "in a literal block scalar"},
			},
			want: []string{"2 interpolations", "line 4 `{{ .a }}` (single-quoted)", "line 9 `{{ .b }}` (in a literal block scalar)", `"{{ .a | dq }}"`},
		},
		{
			name: "an action holding a double quote has no suggestion",
			sites: []quotingSite{
				{Line: 3, Action: `{{ .app | default "" }}`, Style: "unquoted"},
			},
			want:   []string{"cannot be written in a double-quoted scalar", "backquoted raw string"},
			refute: []string{"write it as"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := unportableQuotingError(tc.sites).Error()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("error = %q, want it to mention %q", got, want)
				}
			}
			for _, refute := range tc.refute {
				if strings.Contains(got, refute) {
					t.Errorf("error = %q, want it not to mention %q", got, refute)
				}
			}
		})
	}
}

// TestUnportableQuotingErrorCapsTheList keeps one error readable on a
// recipe that has many sites, while still counting all of them.
func TestUnportableQuotingErrorCapsTheList(t *testing.T) {
	t.Parallel()

	var sites []quotingSite
	for i := 1; i <= maxQuotingSitesNamed+3; i++ {
		sites = append(sites, quotingSite{Line: i, Action: "{{ .app }}", Style: "single-quoted"})
	}
	got := unportableQuotingError(sites).Error()
	if !strings.Contains(got, "8 interpolations") {
		t.Errorf("error = %q, want the full count", got)
	}
	if !strings.Contains(got, "and 3 more") {
		t.Errorf("error = %q, want the capped remainder", got)
	}
	if strings.Contains(got, "line 6 ") {
		t.Errorf("error = %q, want the list to stop at %d entries", got, maxQuotingSitesNamed)
	}
}
