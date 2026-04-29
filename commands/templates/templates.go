// Package templates ships the embedded scaffolds used by `docket init`.
//
// The init command reads these files from the embedded FS, renders them
// through text/template (with custom delimiters so sigil syntax in the
// body is preserved), and writes the result to disk.
package templates

import "embed"

//go:embed *.yml.tmpl *.json5.tmpl
var FS embed.FS
