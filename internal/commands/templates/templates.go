// Package templates ships the embedded scaffolds used by `docket init`.
//
// The init command reads these files from the embedded FS, renders them
// through text/template (with custom delimiters so sigil syntax in the
// body is preserved), and writes the result to disk.
package templates

import "embed"

// Names follow default.<codec>.tmpl / minimal.<codec>.tmpl, where <codec>
// is a canonical recipe format name, so init picks a template without
// branching on the format. The glob is deliberately open-ended to let a
// new format add its pair without touching this file;
// TestEveryCodecHasInitTemplates is what turns a missing pair into a test
// failure rather than a runtime error at `docket init --format <new>`.
//
//go:embed *.tmpl
var FS embed.FS
