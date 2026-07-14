package tasks

import "testing"

func TestDetectJSON5DuplicateKeys(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		dup     bool
		wantKey string
	}{
		{
			name:  "no duplicates",
			input: `{a: 1, b: 2, c: {a: 3}}`,
			dup:   false,
		},
		{
			name:    "simple unquoted duplicate",
			input:   `{a: 1, a: 2}`,
			dup:     true,
			wantKey: "a",
		},
		{
			name:    "quoted and unquoted spelling collide",
			input:   `{"dokku_app": {}, dokku_app: {}}`,
			dup:     true,
			wantKey: "dokku_app",
		},
		{
			name:    "single quoted duplicate",
			input:   `{'x': 1, x: 2}`,
			dup:     true,
			wantKey: "x",
		},
		{
			name:  "same key in different objects is fine",
			input: `[{a: 1}, {a: 2}]`,
			dup:   false,
		},
		{
			name:  "nested object with distinct keys",
			input: `{outer: {a: 1, b: 2}, other: {a: 1}}`,
			dup:   false,
		},
		{
			name:    "duplicate in a nested object",
			input:   `{outer: {a: 1, a: 2}}`,
			dup:     true,
			wantKey: "a",
		},
		{
			name:  "braces and colons inside a string value do not confuse",
			input: `{a: "b: {not a key}", b: "c: d"}`,
			dup:   false,
		},
		{
			name:    "duplicate after a string with braces",
			input:   `{a: "x: {y}", a: 2}`,
			dup:     true,
			wantKey: "a",
		},
		{
			name:  "line and block comments skipped",
			input: "{\n  a: 1, // a comment: not a key\n  /* b: block */ b: 2\n}",
			dup:   false,
		},
		{
			name:    "duplicate across comments",
			input:   "{\n  a: 1,\n  // filler\n  a: 2\n}",
			dup:     true,
			wantKey: "a",
		},
		{
			name:  "trailing comma is fine",
			input: `{a: 1, b: 2,}`,
			dup:   false,
		},
		{
			name:  "array of numbers and literals",
			input: `{list: [1, 2, true, null, Infinity], done: false}`,
			dup:   false,
		},
		{
			name:    "escaped quote inside a value does not end the string early",
			input:   `{a: "he said \"hi\"", a: 2}`,
			dup:     true,
			wantKey: "a",
		},
		{
			name:  "key that is a substring of another is not a duplicate",
			input: `{app: 1, application: 2}`,
			dup:   false,
		},
		{
			name:    "recipe-shaped duplicate task-type key",
			input:   `[{tasks: [{dokku_app: {app: "a"}, dokku_app: {app: "b"}}]}]`,
			dup:     true,
			wantKey: "dokku_app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectJSON5DuplicateKeys([]byte(tt.input))
			if tt.dup {
				if got == nil {
					t.Fatalf("expected duplicate key %q, got none", tt.wantKey)
				}
				if got.Key != tt.wantKey {
					t.Errorf("duplicate key = %q, want %q", got.Key, tt.wantKey)
				}
				if got.Line == 0 {
					t.Errorf("expected a non-zero line for the duplicate")
				}
			} else if got != nil {
				t.Fatalf("expected no duplicate, got %q at line %d", got.Key, got.Line)
			}
		})
	}
}
