package tasks

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	yaml "gopkg.in/yaml.v3"
)

// FormatNameHCL is the canonical name of the HCL codec.
const FormatNameHCL = "hcl"

// hclCodec reads and writes recipes as HCL, the block-and-attribute syntax
// Terraform and Packer are configured in (#407).
//
// It is the first format whose syntax is not a nesting of collections, so
// unlike YAML and JSON5 it needs a stated mapping between blocks and the recipe
// tree; document_hcl.go holds it, and docs/hcl.md documents it.
type hclCodec struct{}

func (hclCodec) Name() string { return FormatNameHCL }

// Aliases has nothing to add. "hcl2" would be the only candidate, and HCL1 is
// a different language docket does not read.
func (hclCodec) Aliases() []string { return nil }

func (hclCodec) Extensions() []string { return []string{"hcl"} }

// Sniff claims bytes that open the way an HCL body does. See hclSniff for what
// that means and why it is narrow enough to be safe.
func (hclCodec) Sniff(data []byte) bool { return hclSniff(data) }

// ToYAML walks the HCL into the interchange tree and re-emits it as YAML, so
// the rest of the pipeline - the sigil render, the yaml.Node walk, the
// line/column reporting - keeps a single implementation.
//
// It shares that walk with DecodeDocument rather than running a second one of
// its own, which is the difference from the JSON5 codec. JSON5's two paths can
// afford to differ because both are mechanical translations of the same
// nesting; HCL's is a mapping with decisions in it, and two copies of those
// decisions would be two things to keep in step.
func (hclCodec) ToYAML(data []byte) ([]byte, *Problem) {
	doc, err := hclDocumentToYAML(data, hclSourceName, false)
	if err != nil {
		return nil, hclProblem(err)
	}
	if doc == nil {
		// An empty or comment-only file holds no recipe. Handing back no
		// bytes lets parseRecipe say so in its own words, the same words an
		// empty YAML recipe gets.
		return []byte{}, nil
	}
	out, marshalErr := yaml.Marshal(doc)
	if marshalErr != nil {
		return nil, &Problem{Code: "hcl_parse", Message: marshalErr.Error()}
	}
	return out, nil
}

// Lint has nothing to add. A repeated attribute - HCL's duplicate key - is a
// parse error in hclsyntax rather than something a later scan has to find, and
// ToYAML reports it with the duplicate_key code the other formats use.
func (hclCodec) Lint([]byte) []Problem { return nil }

func (hclCodec) Format(data []byte) ([]byte, error) { return FormatHCL(data) }

// DecodeDocument parses the recipe into the comment-carrying interchange tree.
//
// The quoting refusal runs first, for the reason the JSON5 codec runs its own
// there: canonical HCL folds a heredoc holding an interpolation into a quoted
// string, and the two escape a substituted value differently.
func (hclCodec) DecodeDocument(data []byte) (*yaml.Node, error) {
	if err := refuseHCLUnportableQuoting(data); err != nil {
		return nil, err
	}
	return hclDocumentToYAML(data, hclSourceName, true)
}

func (hclCodec) EncodeDocument(doc *yaml.Node) ([]byte, error) {
	return yamlDocumentToHCL(doc)
}

// Marshal renders v as a canonically formatted HCL recipe.
//
// It round-trips through YAML first so the struct yaml tags drive the key
// names - the same tags Plays() and the other two codecs use - and only then
// walks the tree into HCL.
func (hclCodec) Marshal(v interface{}) ([]byte, error) {
	raw, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	doc, err := decodeSingleYAMLDocument(raw)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, fmt.Errorf("nothing to write as an hcl recipe")
	}
	return yamlDocumentToHCL(doc)
}

// MarshalVars emits the vars mapping as a flat body of attributes.
//
// Unlike a recipe it gets no canonical-key pass - a vars-file is input name to
// value and the play ordering has nothing to say about it - but it does go
// through hclwrite.Format, so the `=` signs line up the way they do everywhere
// else.
//
// The JSON5 codec can emit plain JSON here and have it load even when the file
// ends up named `.yml`, because JSON is also YAML. HCL is not, so an HCL
// vars-file has to be named `.hcl`; recipeOutputFormatMismatch says so.
func (hclCodec) MarshalVars(v interface{}) ([]byte, error) {
	raw, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return nil, err
	}
	body := documentBody(&node)
	if body == nil || isEmptyDocument(body) {
		return []byte{}, nil
	}
	if body.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("a vars-file must be a mapping of input names to values")
	}

	e := &hclEmitter{}
	for _, pair := range mappingPairs(body) {
		if err := e.writeAttribute(pair, 0); err != nil {
			return nil, err
		}
	}
	return hclwrite.Format(e.buf.Bytes()), nil
}

// UnmarshalVars reads a vars-file back, the other half of MarshalVars.
//
// Decoding goes through the interchange node rather than straight to Go values
// so an HCL number lands in a string field the way a YAML one does - the same
// reason UnmarshalRecipe normalises to YAML rather than decoding the surface
// syntax into the structs.
func (hclCodec) UnmarshalVars(data []byte) (map[string]interface{}, error) {
	file, diags := hclsyntax.ParseConfig(data, hclSourceName, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, hclDiagnosticError(diags)
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("hcl parse error: unexpected body type %T", file.Body)
	}
	if len(body.Blocks) > 0 {
		return nil, fmt.Errorf("%s: a vars-file is a flat mapping of input names to values, not %q blocks",
			hclPos(body.Blocks[0].TypeRange), body.Blocks[0].Type)
	}

	dec := &hclDecoder{src: data}
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if _, err := dec.walkBody(body, len(data), func(item hclBodyItem) error {
		return dec.appendAttribute(mapping, item)
	}); err != nil {
		return nil, err
	}

	out := map[string]interface{}{}
	if len(mapping.Content) == 0 {
		return out, nil
	}
	if err := mapping.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// EscapeDoubleQuoted is `dq` for HCL: the shared JSON escaping, plus HCL's own
// doubling of `${` and `%{`.
func (hclCodec) EscapeDoubleQuoted(v interface{}) (string, error) {
	return HCLDoubleQuoteEscape(v)
}

// DoubleQuotesOnly is true. Canonical HCL writes every string holding an
// interpolation as a quoted scalar, and a heredoc - the only other spelling -
// escapes nothing at all.
func (hclCodec) DoubleQuotesOnly() bool { return true }

// hclAttributeRedefinedSummary is the diagnostic hclsyntax raises for a
// repeated attribute.
//
// Matching on the wording is keying off a detail of the library, which is why
// TestHCLDuplicateAttributeIsADuplicateKey pins it: should a future hcl/v2
// reword the summary, the test fails rather than the code quietly degrading to
// the generic hcl_parse.
const hclAttributeRedefinedSummary = "Attribute redefined"

// hclRedefinedName pulls the attribute name out of hclsyntax's detail text,
// which reads `The argument "app" was already set at …`.
var hclRedefinedName = regexp.MustCompile(`^The argument "([^"]*)" `)

// hclProblem renders a decode failure as the Problem the validator reports.
//
// A repeated attribute becomes duplicate_key rather than hcl_parse so the code
// docs/json-output.md documents means the same thing in all three formats, and
// a tool reading docket's JSON does not have to special-case HCL.
func hclProblem(err error) *Problem {
	var detail *hclError
	if !errors.As(err, &detail) {
		return &Problem{Code: "hcl_parse", Message: err.Error()}
	}
	if detail.Summary == hclAttributeRedefinedSummary {
		message := detail.Message
		if match := hclRedefinedName.FindStringSubmatch(detail.Detail); match != nil {
			message = fmt.Sprintf("duplicate key %q", match[1])
		}
		return &Problem{
			Code:    "duplicate_key",
			Line:    detail.Line,
			Column:  detail.Column,
			Message: message,
		}
	}
	return &Problem{
		Code:    "hcl_parse",
		Line:    detail.Line,
		Column:  detail.Column,
		Message: detail.Message,
	}
}
