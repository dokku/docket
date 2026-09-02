package subprocess

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestMaskStringWithNothingRegistered(t *testing.T) {
	t.Parallel()
	m := NewMasker()

	if got := m.String("hello world"); got != "hello world" {
		t.Errorf("MaskString with no global set = %q, want input unchanged", got)
	}
}

func TestMaskStringReplacesAllOccurrences(t *testing.T) {
	t.Parallel()
	m := NewMasker("secret")

	got := m.String("a secret and another secret")
	want := "a *** and another ***"
	if got != want {
		t.Errorf("MaskString = %q, want %q", got, want)
	}
}

func TestMaskStringEmptyEntriesSkipped(t *testing.T) {
	t.Parallel()
	m := NewMasker("", "tok")

	got := m.String("xtoky")
	if got != "x***y" {
		t.Errorf("MaskString = %q, want %q", got, "x***y")
	}
	// Verify the empty entry didn't cause every character to mask.
	if strings.Contains(m.String("abc"), "***") && !strings.Contains("abc", "tok") {
		t.Errorf("empty entry caused unintended masking")
	}
}

func TestMaskStringLongerBeforeShorter(t *testing.T) {
	t.Parallel()
	// "ab" is a substring of "abcdef"; the longer one must be masked first
	// so we don't see "***cdef" instead of a single "***".
	m := NewMasker("ab", "abcdef")

	got := m.String("xabcdefy")
	if got != "x***y" {
		t.Errorf("MaskString = %q, want %q (longer match first)", got, "x***y")
	}
}

func TestMaskerSetDeduplicates(t *testing.T) {
	t.Parallel()
	m := NewMasker("a", "a", "b")

	values := m.Values()
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

// TestMaskersAreIndependent is the property that replaced clearing. The old
// registry was cleared on the way out of a run, and that teardown is what made
// a second run in the same process lose its secrets. A Masker is never cleared;
// two of them simply do not see each other.
func TestMaskersAreIndependent(t *testing.T) {
	t.Parallel()

	first := NewMasker("first-secret")
	second := NewMasker("second-secret")

	if got := first.String("first-secret second-secret"); got != "*** second-secret" {
		t.Errorf("first masker = %q, want it to mask only its own value", got)
	}
	if got := second.String("first-secret second-secret"); got != "first-secret ***" {
		t.Errorf("second masker = %q, want it to mask only its own value", got)
	}
}

func TestMaskerAddAppendsKeepingExisting(t *testing.T) {
	t.Parallel()
	m := NewMasker("first")

	m.Add("second")

	got := m.String("first and second")
	if got != "*** and ***" {
		t.Errorf("MaskString = %q, want both values masked", got)
	}
}

func TestMaskerAddDeduplicatesAgainstExisting(t *testing.T) {
	t.Parallel()
	m := NewMasker("tok")

	m.Add("tok", "", "tok")

	count := 0
	for _, v := range m.Values() {
		if v == "tok" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Add introduced duplicates: %v", m.Values())
	}
}

func TestMaskerAddKeepsLengthDescOrder(t *testing.T) {
	t.Parallel()
	// "ab" registered first; adding the longer "abcdef" must still mask the
	// longer match first so a substring secret does not leak its remainder.
	m := NewMasker("ab")

	m.Add("abcdef")

	if got := m.String("xabcdefy"); got != "x***y" {
		t.Errorf("MaskString = %q, want %q (longer match first)", got, "x***y")
	}
}

func TestMaskerAddOnEmptyRegistry(t *testing.T) {
	t.Parallel()
	m := NewMasker()

	m.Add("late")

	if got := m.String("a late value"); got != "a *** value" {
		t.Errorf("MaskString = %q, want the added value masked", got)
	}
}

func TestMaskerAddNoValuesIsNoop(t *testing.T) {
	t.Parallel()
	m := NewMasker("keep")

	m.Add()

	if got := m.String("keep"); got != "***" {
		t.Errorf("MaskString = %q, want existing registry untouched", got)
	}
}

// TestMaskerSetRegistersTrimmedSpelling covers #473: a secret whose
// value carries leading or trailing whitespace is rendered trimmed in places
// docket builds text - the `(item=<value>)` loop suffix on a task name - and
// masking is literal substring replacement, so both spellings must register.
func TestMaskerSetRegistersTrimmedSpelling(t *testing.T) {
	t.Parallel()
	m := NewMasker("  padded  ")

	if got := m.String("deploy (item=padded)"); got != "deploy (item=***)" {
		t.Errorf("MaskString = %q, want the trimmed spelling masked", got)
	}
	if got := m.String("raw:  padded  ."); got != "raw:***." {
		t.Errorf("MaskString = %q, want the literal spelling masked", got)
	}
}

// TestMaskerAddRegistersTrimmedSpelling pins the same rule on the
// late-registration path, which is how a task-declared secret joins the
// registry - after the recipe has parsed and already named its expansions.
func TestMaskerAddRegistersTrimmedSpelling(t *testing.T) {
	t.Parallel()
	m := NewMasker()

	m.Add("\tlate\n")

	if got := m.String("configure (item=late)"); got != "configure (item=***)" {
		t.Errorf("MaskString = %q, want the trimmed spelling masked", got)
	}
}

// TestSensitiveTrimmedSpellingSortsAfterLiteral pins the ordering the
// length-descending sort gives the two spellings: the padded literal is
// replaced first, so text holding the full value masks to a single `***`
// rather than leaving the padding behind around an inner match.
func TestSensitiveTrimmedSpellingSortsAfterLiteral(t *testing.T) {
	t.Parallel()
	m := NewMasker(" tok ")

	values := m.Values()
	want := []string{" tok ", "tok"}
	if len(values) != len(want) {
		t.Fatalf("Values() = %v, want %v", values, want)
	}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("Values() = %v, want %v", values, want)
		}
	}
}

// TestSensitiveWhitespaceOnlyValueIsDropped keeps the trimmed spelling from
// reintroducing the empty entry the masker drops: an all-whitespace
// value trims to "", which would otherwise match every position in a string.
func TestSensitiveWhitespaceOnlyValueIsDropped(t *testing.T) {
	t.Parallel()
	m := NewMasker("   ")

	if got := m.Values(); len(got) != 1 || got[0] != "   " {
		t.Fatalf("Values() = %v, want only the literal whitespace value", got)
	}
	if got := m.String("plain"); got != "plain" {
		t.Errorf("MaskString = %q, want %q; an empty entry masked everything", got, "plain")
	}
}

// TestSensitiveUnpaddedValueRegistersOnce keeps the common case free of a
// duplicate entry: a value that is already trimmed contributes one spelling.
func TestSensitiveUnpaddedValueRegistersOnce(t *testing.T) {
	t.Parallel()
	m := NewMasker("plain")

	if got := m.Values(); len(got) != 1 || got[0] != "plain" {
		t.Errorf("Values() = %v, want a single entry", got)
	}
}

// TestMaskerSetRegistersEscapedSpelling covers #475: a generated
// resource address wraps a key value in strconv.Quote when a bare form would
// not parse back, which escapes the double quote the value carries, so the
// registered literal no longer matches inside the address it produced.
func TestMaskerSetRegistersEscapedSpelling(t *testing.T) {
	t.Parallel()
	m := NewMasker(`quo"ted`)

	if got := m.String(`dokku_stub[key="quo\"ted"]`); got != `dokku_stub[key="***"]` {
		t.Errorf("MaskString = %q, want the escaped spelling masked", got)
	}
	if got := m.String(`raw:quo"ted.`); got != "raw:***." {
		t.Errorf("MaskString = %q, want the literal spelling masked", got)
	}
}

// TestMaskerAddRegistersEscapedSpelling pins the same rule on the
// late-registration path, which is how a task-declared secret joins the
// registry - after the recipe has parsed and already named its tasks.
func TestMaskerAddRegistersEscapedSpelling(t *testing.T) {
	t.Parallel()
	m := NewMasker()

	m.Add(`la"te`)

	if got := m.String(`dokku_stub[key="la\"te"]`); got != `dokku_stub[key="***"]` {
		t.Errorf("MaskString = %q, want the escaped spelling masked", got)
	}
}

// TestSensitiveEscapedSpellingSortsBeforeLiteral pins the ordering the
// length-descending sort gives the two spellings. The escaped form is the
// longer one, so it is replaced first; the reverse order would leave the
// escaping backslash stranded next to a `***`.
func TestSensitiveEscapedSpellingSortsBeforeLiteral(t *testing.T) {
	t.Parallel()
	m := NewMasker(`a"b`)

	values := m.Values()
	want := []string{`a\"b`, `a"b`}
	if len(values) != len(want) {
		t.Fatalf("Values() = %q, want %q", values, want)
	}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("Values() = %q, want %q", values, want)
		}
	}
}

// TestSensitiveBackslashValueRegistersEscapedSpelling covers the character
// that only ever leaks in combination: a backslash needs no quoting of its
// own, so it reaches an address escaped only when the value also carries a
// comma, a bracket, or a quote. Both spellings register either way.
func TestSensitiveBackslashValueRegistersEscapedSpelling(t *testing.T) {
	t.Parallel()
	m := NewMasker(`a,b\c`)

	if got := m.String(`dokku_stub[key="a,b\\c"]`); got != `dokku_stub[key="***"]` {
		t.Errorf("MaskString = %q, want the escaped spelling masked", got)
	}
	if got := m.String(`raw:a,b\c.`); got != "raw:***." {
		t.Errorf("MaskString = %q, want the literal spelling masked", got)
	}
}

// TestSensitiveCommaValueRegistersOnce pins the boundary of the escaped
// spelling: a comma forces an address to quote the value but needs no
// escaping inside those quotes, so the literal still matches there and the
// registry stays at one entry.
func TestSensitiveCommaValueRegistersOnce(t *testing.T) {
	t.Parallel()
	m := NewMasker("a,b")

	if got := m.Values(); len(got) != 1 || got[0] != "a,b" {
		t.Errorf("Values() = %q, want a single entry", got)
	}
	if got := m.String(`dokku_stub[key="a,b"]`); got != `dokku_stub[key="***"]` {
		t.Errorf("MaskString = %q, want the quoted value masked", got)
	}
}

// TestSensitivePaddedEscapedValueRegistersEverySpelling pins how the two
// derivations compose. A value that is both padded and escape-bearing is
// printed four ways, and every one of them masks.
func TestSensitivePaddedEscapedValueRegistersEverySpelling(t *testing.T) {
	t.Parallel()
	m := NewMasker(` p"q `)

	values := m.Values()
	want := []string{` p\"q `, ` p"q `, `p\"q`, `p"q`}
	if len(values) != len(want) {
		t.Fatalf("Values() = %q, want %q", values, want)
	}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("Values() = %q, want %q", values, want)
		}
	}
}

// TestSensitiveEscapedSpellingLeavesLookalikesAlone pins the boundary:
// registering the escaped spelling must not widen masking to a value that
// merely shares a prefix with a secret.
func TestSensitiveEscapedSpellingLeavesLookalikesAlone(t *testing.T) {
	t.Parallel()
	m := NewMasker(`a"b`)

	if got := m.String(`dokku_stub[key=keepzzz]`); got != `dokku_stub[key=keepzzz]` {
		t.Errorf("MaskString = %q, want the non-secret value untouched", got)
	}
}

func TestMaskStringConcurrent(t *testing.T) {
	t.Parallel()
	m := NewMasker("secret")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			m.Add("token")
		}()
		go func() {
			defer wg.Done()
			_ = m.String("a secret token here")
		}()
	}
	wg.Wait()

	// A value added under contention is still registered afterwards.
	if got := m.String("a secret token here"); got != "a *** *** here" {
		t.Errorf("after concurrent use = %q, want both values masked", got)
	}
}

func TestMaskerValuesReturnsCopy(t *testing.T) {
	t.Parallel()
	m := NewMasker("a")

	values := m.Values()
	values[0] = "mutated"
	again := m.Values()
	if again[0] != "a" {
		t.Errorf("Values returned a shared slice; mutation leaked: %v", again)
	}
}

// TestMaskValue covers the walker that masks a value arriving as interface{}.
// The `loop_item` field of `--list-tasks --json` is such a value: a recipe's
// `loop:` can resolve to a scalar, a list, or a mapping, so a secret can sit
// at any depth and on either side of a map entry.
func TestMaskValue(t *testing.T) {
	t.Parallel()
	m := NewMasker("sekret")

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
			if got := m.Value(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("m.Value(%#v) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// TestMaskValueWithNothingRegistered pins that a value survives the walk unchanged
// when nothing is registered, so the listing renders identically for a recipe
// that declares no secrets.
func TestMaskValueWithNothingRegistered(t *testing.T) {
	t.Parallel()
	m := NewMasker()

	in := map[string]interface{}{"user": "alice", "ports": []interface{}{80, "443"}}
	if got := m.Value(in); !reflect.DeepEqual(got, in) {
		t.Errorf("MaskValue with an empty registry = %#v, want input unchanged", got)
	}
}

// TestMaskerSurvivesAnotherRunFinishing is the fail-open case #501 exists to
// close. The registry this replaced was process-wide and the API that filled
// it *replaced* the set, so a second run's teardown - a deferred clear - blanked
// the first run's secrets while it was still writing output. A Masker is owned
// by its run and never cleared, so a run that finishes cannot affect one that
// has not.
func TestMaskerSurvivesAnotherRunFinishing(t *testing.T) {
	t.Parallel()

	first := NewMasker("first-secret")

	// A second run starts, does its work, and ends. Under the old registry
	// this is the point the first run's secrets were lost.
	func() {
		second := NewMasker("second-secret")
		if got := second.String("second-secret"); got != MaskPlaceholder {
			t.Errorf("second run masked %q, want %q", got, MaskPlaceholder)
		}
	}()

	if got := first.String("first-secret"); got != MaskPlaceholder {
		t.Errorf("first run masked %q after the second finished, want %q", got, MaskPlaceholder)
	}
}

// TestContextMaskerRoundTrip pins the carrier the layers with no other reason
// to know about a run use to reach it.
func TestContextMaskerRoundTrip(t *testing.T) {
	t.Parallel()

	m := NewMasker("secret")
	if got := MaskerFromContext(ContextWithMasker(context.Background(), m)); got != m {
		t.Errorf("MaskerFromContext returned %p, want %p", got, m)
	}
}

// TestNilMaskerMasksNothing pins the property that lets every masking site skip
// a nil check: a caller that registered no secrets has nothing to hide.
func TestNilMaskerMasksNothing(t *testing.T) {
	t.Parallel()

	var m *Masker
	if got := m.String("secret"); got != "secret" {
		t.Errorf("nil masker changed %q to %q", "secret", got)
	}
	if got := m.Value([]string{"secret"}); !reflect.DeepEqual(got, []string{"secret"}) {
		t.Errorf("nil masker changed %v", got)
	}
	if got := m.Values(); got != nil {
		t.Errorf("nil masker has values: %v", got)
	}
	m.Add("ignored") // must not panic
	// A context carrying nothing yields the same nil masker.
	if got := MaskerFromContext(context.Background()); got != nil {
		t.Errorf("empty context yielded a masker: %v", got)
	}
}
