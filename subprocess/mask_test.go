package subprocess

import (
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestMaskStringNoGlobalSet(t *testing.T) {
	SetGlobalSensitive(nil)
	defer SetGlobalSensitive(nil)

	if got := MaskString("hello world"); got != "hello world" {
		t.Errorf("MaskString with no global set = %q, want input unchanged", got)
	}
}

func TestMaskStringReplacesAllOccurrences(t *testing.T) {
	SetGlobalSensitive([]string{"secret"})
	defer SetGlobalSensitive(nil)

	got := MaskString("a secret and another secret")
	want := "a *** and another ***"
	if got != want {
		t.Errorf("MaskString = %q, want %q", got, want)
	}
}

func TestMaskStringEmptyEntriesSkipped(t *testing.T) {
	SetGlobalSensitive([]string{"", "tok"})
	defer SetGlobalSensitive(nil)

	got := MaskString("xtoky")
	if got != "x***y" {
		t.Errorf("MaskString = %q, want %q", got, "x***y")
	}
	// Verify the empty entry didn't cause every character to mask.
	if strings.Contains(MaskString("abc"), "***") && !strings.Contains("abc", "tok") {
		t.Errorf("empty entry caused unintended masking")
	}
}

func TestMaskStringLongerBeforeShorter(t *testing.T) {
	// "ab" is a substring of "abcdef"; the longer one must be masked first
	// so we don't see "***cdef" instead of a single "***".
	SetGlobalSensitive([]string{"ab", "abcdef"})
	defer SetGlobalSensitive(nil)

	got := MaskString("xabcdefy")
	if got != "x***y" {
		t.Errorf("MaskString = %q, want %q (longer match first)", got, "x***y")
	}
}

func TestSetGlobalSensitiveDeduplicates(t *testing.T) {
	SetGlobalSensitive([]string{"a", "a", "b"})
	defer SetGlobalSensitive(nil)

	values := GlobalSensitive()
	count := 0
	for _, v := range values {
		if v == "a" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("duplicates not removed: got %d copies of 'a' in %v", count, values)
	}
}

func TestSetGlobalSensitiveClear(t *testing.T) {
	SetGlobalSensitive([]string{"x"})
	SetGlobalSensitive(nil)
	if got := MaskString("xyz"); got != "xyz" {
		t.Errorf("MaskString after clear = %q, want %q", got, "xyz")
	}
}

func TestAddGlobalSensitiveAppendsKeepingExisting(t *testing.T) {
	SetGlobalSensitive([]string{"first"})
	defer SetGlobalSensitive(nil)

	AddGlobalSensitive("second")

	got := MaskString("first and second")
	if got != "*** and ***" {
		t.Errorf("MaskString = %q, want both values masked", got)
	}
}

func TestAddGlobalSensitiveDeduplicatesAgainstExisting(t *testing.T) {
	SetGlobalSensitive([]string{"tok"})
	defer SetGlobalSensitive(nil)

	AddGlobalSensitive("tok", "", "tok")

	count := 0
	for _, v := range GlobalSensitive() {
		if v == "tok" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("AddGlobalSensitive introduced duplicates: %v", GlobalSensitive())
	}
}

func TestAddGlobalSensitiveKeepsLengthDescOrder(t *testing.T) {
	// "ab" registered first; adding the longer "abcdef" must still mask the
	// longer match first so a substring secret does not leak its remainder.
	SetGlobalSensitive([]string{"ab"})
	defer SetGlobalSensitive(nil)

	AddGlobalSensitive("abcdef")

	if got := MaskString("xabcdefy"); got != "x***y" {
		t.Errorf("MaskString = %q, want %q (longer match first)", got, "x***y")
	}
}

func TestAddGlobalSensitiveOnEmptyRegistry(t *testing.T) {
	SetGlobalSensitive(nil)
	defer SetGlobalSensitive(nil)

	AddGlobalSensitive("late")

	if got := MaskString("a late value"); got != "a *** value" {
		t.Errorf("MaskString = %q, want the added value masked", got)
	}
}

func TestAddGlobalSensitiveNoValuesIsNoop(t *testing.T) {
	SetGlobalSensitive([]string{"keep"})
	defer SetGlobalSensitive(nil)

	AddGlobalSensitive()

	if got := MaskString("keep"); got != "***" {
		t.Errorf("MaskString = %q, want existing registry untouched", got)
	}
}

// TestSetGlobalSensitiveRegistersTrimmedSpelling covers #473: a secret whose
// value carries leading or trailing whitespace is rendered trimmed in places
// docket builds text - the `(item=<value>)` loop suffix on a task name - and
// masking is literal substring replacement, so both spellings must register.
func TestSetGlobalSensitiveRegistersTrimmedSpelling(t *testing.T) {
	SetGlobalSensitive([]string{"  padded  "})
	defer SetGlobalSensitive(nil)

	if got := MaskString("deploy (item=padded)"); got != "deploy (item=***)" {
		t.Errorf("MaskString = %q, want the trimmed spelling masked", got)
	}
	if got := MaskString("raw:  padded  ."); got != "raw:***." {
		t.Errorf("MaskString = %q, want the literal spelling masked", got)
	}
}

// TestAddGlobalSensitiveRegistersTrimmedSpelling pins the same rule on the
// late-registration path, which is how a task-declared secret joins the
// registry - after the recipe has parsed and already named its expansions.
func TestAddGlobalSensitiveRegistersTrimmedSpelling(t *testing.T) {
	SetGlobalSensitive(nil)
	defer SetGlobalSensitive(nil)

	AddGlobalSensitive("\tlate\n")

	if got := MaskString("configure (item=late)"); got != "configure (item=***)" {
		t.Errorf("MaskString = %q, want the trimmed spelling masked", got)
	}
}

// TestSensitiveTrimmedSpellingSortsAfterLiteral pins the ordering the
// length-descending sort gives the two spellings: the padded literal is
// replaced first, so text holding the full value masks to a single `***`
// rather than leaving the padding behind around an inner match.
func TestSensitiveTrimmedSpellingSortsAfterLiteral(t *testing.T) {
	SetGlobalSensitive([]string{" tok "})
	defer SetGlobalSensitive(nil)

	values := GlobalSensitive()
	want := []string{" tok ", "tok"}
	if len(values) != len(want) {
		t.Fatalf("GlobalSensitive = %v, want %v", values, want)
	}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("GlobalSensitive = %v, want %v", values, want)
		}
	}
}

// TestSensitiveWhitespaceOnlyValueIsDropped keeps the trimmed spelling from
// reintroducing the empty entry SetGlobalSensitive drops: an all-whitespace
// value trims to "", which would otherwise match every position in a string.
func TestSensitiveWhitespaceOnlyValueIsDropped(t *testing.T) {
	SetGlobalSensitive([]string{"   "})
	defer SetGlobalSensitive(nil)

	if got := GlobalSensitive(); len(got) != 1 || got[0] != "   " {
		t.Fatalf("GlobalSensitive = %v, want only the literal whitespace value", got)
	}
	if got := MaskString("plain"); got != "plain" {
		t.Errorf("MaskString = %q, want %q; an empty entry masked everything", got, "plain")
	}
}

// TestSensitiveUnpaddedValueRegistersOnce keeps the common case free of a
// duplicate entry: a value that is already trimmed contributes one spelling.
func TestSensitiveUnpaddedValueRegistersOnce(t *testing.T) {
	SetGlobalSensitive([]string{"plain"})
	defer SetGlobalSensitive(nil)

	if got := GlobalSensitive(); len(got) != 1 || got[0] != "plain" {
		t.Errorf("GlobalSensitive = %v, want a single entry", got)
	}
}

// TestSetGlobalSensitiveRegistersEscapedSpelling covers #475: a generated
// resource address wraps a key value in strconv.Quote when a bare form would
// not parse back, which escapes the double quote the value carries, so the
// registered literal no longer matches inside the address it produced.
func TestSetGlobalSensitiveRegistersEscapedSpelling(t *testing.T) {
	SetGlobalSensitive([]string{`quo"ted`})
	defer SetGlobalSensitive(nil)

	if got := MaskString(`dokku_stub[key="quo\"ted"]`); got != `dokku_stub[key="***"]` {
		t.Errorf("MaskString = %q, want the escaped spelling masked", got)
	}
	if got := MaskString(`raw:quo"ted.`); got != "raw:***." {
		t.Errorf("MaskString = %q, want the literal spelling masked", got)
	}
}

// TestAddGlobalSensitiveRegistersEscapedSpelling pins the same rule on the
// late-registration path, which is how a task-declared secret joins the
// registry - after the recipe has parsed and already named its tasks.
func TestAddGlobalSensitiveRegistersEscapedSpelling(t *testing.T) {
	SetGlobalSensitive(nil)
	defer SetGlobalSensitive(nil)

	AddGlobalSensitive(`la"te`)

	if got := MaskString(`dokku_stub[key="la\"te"]`); got != `dokku_stub[key="***"]` {
		t.Errorf("MaskString = %q, want the escaped spelling masked", got)
	}
}

// TestSensitiveEscapedSpellingSortsBeforeLiteral pins the ordering the
// length-descending sort gives the two spellings. The escaped form is the
// longer one, so it is replaced first; the reverse order would leave the
// escaping backslash stranded next to a `***`.
func TestSensitiveEscapedSpellingSortsBeforeLiteral(t *testing.T) {
	SetGlobalSensitive([]string{`a"b`})
	defer SetGlobalSensitive(nil)

	values := GlobalSensitive()
	want := []string{`a\"b`, `a"b`}
	if len(values) != len(want) {
		t.Fatalf("GlobalSensitive = %q, want %q", values, want)
	}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("GlobalSensitive = %q, want %q", values, want)
		}
	}
}

// TestSensitiveBackslashValueRegistersEscapedSpelling covers the character
// that only ever leaks in combination: a backslash needs no quoting of its
// own, so it reaches an address escaped only when the value also carries a
// comma, a bracket, or a quote. Both spellings register either way.
func TestSensitiveBackslashValueRegistersEscapedSpelling(t *testing.T) {
	SetGlobalSensitive([]string{`a,b\c`})
	defer SetGlobalSensitive(nil)

	if got := MaskString(`dokku_stub[key="a,b\\c"]`); got != `dokku_stub[key="***"]` {
		t.Errorf("MaskString = %q, want the escaped spelling masked", got)
	}
	if got := MaskString(`raw:a,b\c.`); got != "raw:***." {
		t.Errorf("MaskString = %q, want the literal spelling masked", got)
	}
}

// TestSensitiveCommaValueRegistersOnce pins the boundary of the escaped
// spelling: a comma forces an address to quote the value but needs no
// escaping inside those quotes, so the literal still matches there and the
// registry stays at one entry.
func TestSensitiveCommaValueRegistersOnce(t *testing.T) {
	SetGlobalSensitive([]string{"a,b"})
	defer SetGlobalSensitive(nil)

	if got := GlobalSensitive(); len(got) != 1 || got[0] != "a,b" {
		t.Errorf("GlobalSensitive = %q, want a single entry", got)
	}
	if got := MaskString(`dokku_stub[key="a,b"]`); got != `dokku_stub[key="***"]` {
		t.Errorf("MaskString = %q, want the quoted value masked", got)
	}
}

// TestSensitivePaddedEscapedValueRegistersEverySpelling pins how the two
// derivations compose. A value that is both padded and escape-bearing is
// printed four ways, and every one of them masks.
func TestSensitivePaddedEscapedValueRegistersEverySpelling(t *testing.T) {
	SetGlobalSensitive([]string{` p"q `})
	defer SetGlobalSensitive(nil)

	values := GlobalSensitive()
	want := []string{` p\"q `, ` p"q `, `p\"q`, `p"q`}
	if len(values) != len(want) {
		t.Fatalf("GlobalSensitive = %q, want %q", values, want)
	}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("GlobalSensitive = %q, want %q", values, want)
		}
	}
}

// TestSensitiveEscapedSpellingLeavesLookalikesAlone pins the boundary:
// registering the escaped spelling must not widen masking to a value that
// merely shares a prefix with a secret.
func TestSensitiveEscapedSpellingLeavesLookalikesAlone(t *testing.T) {
	SetGlobalSensitive([]string{`a"b`})
	defer SetGlobalSensitive(nil)

	if got := MaskString(`dokku_stub[key=keepzzz]`); got != `dokku_stub[key=keepzzz]` {
		t.Errorf("MaskString = %q, want the non-secret value untouched", got)
	}
}

func TestMaskStringConcurrent(t *testing.T) {
	SetGlobalSensitive([]string{"secret"})
	defer SetGlobalSensitive(nil)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			SetGlobalSensitive([]string{"secret", "token"})
		}()
		go func() {
			defer wg.Done()
			_ = MaskString("a secret token here")
		}()
	}
	wg.Wait()
}

func TestGlobalSensitiveReturnsCopy(t *testing.T) {
	SetGlobalSensitive([]string{"a"})
	defer SetGlobalSensitive(nil)

	values := GlobalSensitive()
	values[0] = "mutated"
	again := GlobalSensitive()
	if again[0] != "a" {
		t.Errorf("GlobalSensitive returned shared slice; mutation leaked: %v", again)
	}
}

// TestMaskValue covers the walker that masks a value arriving as interface{}.
// The `loop_item` field of `--list-tasks --json` is such a value: a recipe's
// `loop:` can resolve to a scalar, a list, or a mapping, so a secret can sit
// at any depth and on either side of a map entry.
func TestMaskValue(t *testing.T) {
	SetGlobalSensitive([]string{"sekret"})
	defer SetGlobalSensitive(nil)

	cases := []struct {
		name string
		in   interface{}
		want interface{}
	}{
		{"nil", nil, nil},
		{"string", "a-sekret-b", "a-***-b"},
		{"int passes through", 42, 42},
		{"float passes through", 1.5, 1.5},
		{"bool passes through", true, true},
		{"string slice", []string{"sekret", "plain"}, []string{"***", "plain"}},
		{
			"nested list",
			[]interface{}{"sekret", []interface{}{"sekret", 1}},
			[]interface{}{"***", []interface{}{"***", 1}},
		},
		{
			"map masks keys and values",
			map[string]interface{}{"sekret": "sekret", "k": 1},
			map[string]interface{}{"***": "***", "k": 1},
		},
		{
			"string map",
			map[string]string{"sekret": "sekret"},
			map[string]string{"***": "***"},
		},
		{
			"map nested in a list",
			[]interface{}{map[string]interface{}{"token": "sekret"}},
			[]interface{}{map[string]interface{}{"token": "***"}},
		},
		{
			"typed slice reaches the reflect fallback",
			[]map[string]string{{"token": "sekret"}},
			[]interface{}{map[string]string{"token": "***"}},
		},
		{
			"non-string map key reaches the reflect fallback",
			map[interface{}]interface{}{"sekret": "sekret"},
			map[string]interface{}{"***": "***"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskValue(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("MaskValue(%#v) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// TestMaskValueEmptyRegistry pins that a value survives the walk unchanged
// when nothing is registered, so the listing renders identically for a recipe
// that declares no secrets.
func TestMaskValueEmptyRegistry(t *testing.T) {
	SetGlobalSensitive(nil)
	defer SetGlobalSensitive(nil)

	in := map[string]interface{}{"user": "alice", "ports": []interface{}{80, "443"}}
	if got := MaskValue(in); !reflect.DeepEqual(got, in) {
		t.Errorf("MaskValue with an empty registry = %#v, want input unchanged", got)
	}
}
