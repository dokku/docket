package tasks

import (
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// Codec is everything docket needs to know about one recipe surface
// syntax. A recipe's format is a string on the wire - the --tasks-format /
// --format flags, recipeSource.Format, ValidateOptions.Format - and every
// place that used to branch on that string now resolves it to a Codec and
// calls a method.
//
// Adding a format means writing one implementation and adding it to the
// codecs slice below. Nothing else in the tree should compare a format
// string against a literal; TestCodecConformance enforces what a new
// implementation has to satisfy.
type Codec interface {
	// Name is the canonical identifier for the format: what
	// --tasks-format / --format resolve to, and what every format string
	// downstream holds.
	Name() string

	// Aliases are the other spellings the flags accept - "yml" for YAML,
	// "json" for JSON5. They resolve to Name but are deliberately left
	// out of shell completion so it suggests one spelling per format.
	Aliases() []string

	// Extensions are the file extensions, without the leading dot, that
	// select this codec. Order matters: it is the order shell completion
	// offers files in, so the canonical spelling comes first.
	Extensions() []string

	// Sniff reports whether data is written in this format. It is the
	// last resort for a source with no name to key off, which in practice
	// means stdin. The default codec is never asked - it is what
	// SniffCodec falls back to when no other codec claims the bytes.
	Sniff(data []byte) bool

	// ToYAML converts data from this surface syntax to the YAML bytes
	// every downstream stage reads, so the loader, the validator and
	// flag registration all agree on how a scalar lands in a Go field.
	//
	// A failure comes back as this format's parse diagnostic rather than
	// a bare error because the validator reports it to the user by Code
	// (docs/json-output.md documents those codes as a stable machine
	// key). Note the "yaml_parse" code is not one of these: it belongs to
	// the yaml.v3 parse that runs after normalisation, for every format.
	ToYAML(data []byte) ([]byte, *Problem)

	// Lint reports findings only this format can raise, reading the
	// ORIGINAL bytes rather than ToYAML's output - a duplicated JSON5 key
	// has already been deduped by the time the conversion is done.
	//
	// Kept separate from ToYAML on purpose: normalizeRecipeBytes runs
	// both, UnmarshalRecipe runs only the conversion. Folding Lint into
	// ToYAML would make parseInputDocument and countTasks start rejecting
	// duplicate-keyed JSON5 that they accept today.
	Lint(data []byte) []Problem

	// Format returns the canonical rendering of data - what `docket fmt`
	// writes back.
	Format(data []byte) ([]byte, error)

	// DecodeDocument parses data into a comment-carrying yaml.Node
	// document. That tree is the neutral interchange every cross-format
	// conversion passes through, which is what keeps conversion O(codecs)
	// rather than O(codec pairs): a new format implements this pair and
	// gets conversion to and from every other format for free.
	//
	// yaml.Node is the interchange rather than a tree of docket's own
	// because it is the richest model in play - anchors, tags, styles and
	// head/line/foot comments - so information is only ever lost on the
	// way OUT of it, in EncodeDocument, where the codec that cannot
	// represent something is the one deciding what to do about it. It also
	// makes the YAML codec's own pair free.
	//
	// A nil node with a nil error means data holds no document at all, for
	// a format that has an empty form: YAML's empty and comment-only
	// files, which Format already returns untouched. A format with no
	// empty representation returns an error instead, exactly as its Format
	// does today - parseJSON5 rejects empty input, so `docket fmt
	// empty.json` fails now and keeps failing.
	DecodeDocument(data []byte) (*yaml.Node, error)

	// EncodeDocument renders that document as a canonically formatted
	// recipe in this surface syntax - the same bytes Format would produce
	// for it. A nil document is an error; Convert screens for one first.
	//
	// This is where a format states what it cannot carry. JSON5 has no
	// anchors, so Convert flattens them before calling; it has no date
	// type, so a !!timestamp widens to a string; it has no complex keys,
	// so a non-scalar mapping key is refused outright rather than
	// stringified into something that means less.
	EncodeDocument(doc *yaml.Node) ([]byte, error)

	// Marshal renders v as a canonically formatted recipe document, the
	// same bytes Format would produce for it.
	Marshal(v interface{}) ([]byte, error)

	// MarshalVars renders v as a companion vars-file: a flat mapping of
	// input name to value, not a recipe, so it gets no canonical-form
	// pass.
	MarshalVars(v interface{}) ([]byte, error)

	// UnmarshalVars reads a vars-file back, the other half of MarshalVars.
	UnmarshalVars(data []byte) (map[string]interface{}, error)
}

// codecs is every recipe format docket understands, in priority order.
//
// The order is load-bearing three times over: codecs[0] is the default
// codec (what an empty or unrecognised format string means), SniffCodec
// walks the rest in order, and CodecNames drives both shell completion
// and the "must be one of ..." rejection an invalid --tasks-format gets.
//
// This is an ordered literal rather than a RegisterCodec function on
// purpose. Nothing outside this package adds a codec, so registration
// would buy nothing and cost determinism: any init() in the package - a
// _test.go file included - could move what DefaultCodec returns out from
// under a package that leans on t.Parallel() throughout, and there would
// be no way to put it back. tasks/export.go's globalExportOrder is the
// same shape for the same reason.
var codecs = []Codec{yamlCodec{}, json5Codec{}}

// Codecs returns every registered codec in priority order.
func Codecs() []Codec {
	out := make([]Codec, len(codecs))
	copy(out, codecs)
	return out
}

// DefaultCodec is the codec a recipe is read with when nothing says
// otherwise: an empty format string, an unrecognised one, an unknown file
// extension, or bytes no other codec claims.
//
// This is the explicit default that replaced IsJSON5Format(""), which
// decided the same question by returning false.
func DefaultCodec() Codec {
	return codecs[0]
}

// LookupCodec resolves a user-typed format spelling - a canonical name or
// an alias, case-insensitive and space-tolerant - to its codec. The second
// return is false for anything unrecognised, including the empty string;
// callers that treat "" as "not set" must check for it first.
func LookupCodec(spelling string) (Codec, bool) {
	want := strings.ToLower(strings.TrimSpace(spelling))
	if want == "" {
		return nil, false
	}
	for _, codec := range codecs {
		if codec.Name() == want {
			return codec, true
		}
		for _, alias := range codec.Aliases() {
			if alias == want {
				return codec, true
			}
		}
	}
	return nil, false
}

// CodecFor resolves a format string to its codec, falling back to
// DefaultCodec for the empty string and for anything unrecognised. It is
// the lenient lookup every dispatch site uses; the CLI has already
// rejected a bad spelling via LookupCodec by the time a format reaches
// here, and an embedder passing no format keeps getting YAML.
func CodecFor(format string) Codec {
	if codec, ok := LookupCodec(format); ok {
		return codec
	}
	return DefaultCodec()
}

// CodecForExtension resolves a file extension - without the leading dot,
// case-insensitive - to its codec. The second return is false when no
// codec claims it, which the caller turns into DefaultCodec so an
// explicit path like `--tasks recipe.txt` still reads as YAML.
func CodecForExtension(ext string) (Codec, bool) {
	want := strings.ToLower(strings.TrimPrefix(ext, "."))
	if want == "" {
		return nil, false
	}
	for _, codec := range codecs {
		for _, candidate := range codec.Extensions() {
			if candidate == want {
				return codec, true
			}
		}
	}
	return nil, false
}

// SniffCodec picks a codec from the recipe bytes themselves, for a source
// with no name to key off. The default codec is skipped rather than asked:
// it is the answer when nobody else claims the bytes, so letting it match
// first would stop any other codec from ever being reached.
func SniffCodec(data []byte) Codec {
	fallback := DefaultCodec()
	for _, codec := range codecs {
		if codec.Name() == fallback.Name() {
			continue
		}
		if codec.Sniff(data) {
			return codec
		}
	}
	return fallback
}

// CodecNames returns the canonical format names in priority order. It
// backs shell completion for --tasks-format / --format and the list an
// invalid value is rejected against, so those two can never disagree.
func CodecNames() []string {
	out := make([]string, 0, len(codecs))
	for _, codec := range codecs {
		out = append(out, codec.Name())
	}
	return out
}

// CodecExtensions returns every recognised recipe file extension, without
// leading dots, in codec order and then in each codec's own order. It
// backs file-completion and the positional-recipe-path check.
func CodecExtensions() []string {
	var out []string
	for _, codec := range codecs {
		out = append(out, codec.Extensions()...)
	}
	return out
}
