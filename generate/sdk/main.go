// Command sdk writes the public sdk package's generated files from the engine:
// sdk/sdk_gen.go, the functions, constants and variables an embedding caller
// uses, and sdk/types_gen.go, a type alias for every engine type that caller
// has to be able to name.
//
// The engine lives under internal/, so a caller outside this module cannot
// import it. The sdk package re-exports the embedding surface. Which
// declarations that is, is marked where they are declared: an `//sdk:export`
// line in a declaration's doc comment exports it, and `//sdk:export Name`
// exports it under another name. On a const or var block the directive
// exports every name in the block; on one spec inside a block it exports only
// that spec. The doc comment, without the directive, becomes the sdk's doc.
//
// Functions become one-line wrappers rather than variables, so the sdk's
// documentation lists them as functions, and generic ones keep their type
// parameters. Types are aliases rather than copies, so a value built through
// sdk is the engine's own value. Beyond the exported types, every registered
// task and every named engine type reachable from an exported type, function
// signature or constant through exported fields is aliased too: writing that
// list by hand would miss the next task someone adds, so it is derived here,
// and TestGeneratedSDKIsCurrent fails when a committed file falls behind.
//
// Because the surface is generated, a rebase that conflicts in either file is
// settled by regenerating it.
//
// It is run by `go generate` from the sdk directory, which is where the output
// is written.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"log"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/dokku/docket/internal/tasks"

	"golang.org/x/tools/go/packages"
)

// Output file names, relative to the sdk directory.
const (
	funcsFile = "sdk_gen.go"
	typesFile = "types_gen.go"
)

// directive marks a declaration as part of the sdk.
const directive = "//sdk:export"

// enginePackages maps each engine package the sdk re-exports to the name the
// generated files import it under.
var enginePackages = map[string]string{
	"github.com/dokku/docket/internal/tasks":      "tasks",
	"github.com/dokku/docket/internal/subprocess": "subprocess",
}

// tasksPackage is the package the registered task types live in.
const tasksPackage = "github.com/dokku/docket/internal/tasks"

// exportKind is the kind of declaration an export re-exports.
type exportKind int

const (
	kindFunc exportKind = iota
	kindConst
	kindVar
	kindType
)

// export is one exported name.
type export struct {
	kind exportKind
	// name is the sdk name; engine is the name in the engine package.
	name   string
	engine string
	pkg    string
	doc    string
	obj    types.Object
}

// group is the unit the generated file is written in: a function, a type, or
// a const or var block holding one or more names under one doc comment.
type group struct {
	kind    exportKind
	doc     string
	members []export
}

// surface is everything the generator writes.
type surface struct {
	groups []group
	// aliased is every engine type the sdk aliases, by its declaring object.
	aliased map[*types.TypeName]bool
	// pkgs is the loaded engine packages, by import path.
	pkgs map[string]*packages.Package
}

var (
	loadOnce   sync.Once
	loadedPkgs map[string]*packages.Package
	loadErr    error
)

// loadEngine type-checks the engine packages once per process.
func loadEngine() (map[string]*packages.Package, error) {
	loadOnce.Do(func() {
		paths := make([]string, 0, len(enginePackages))
		for path := range enginePackages {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo}
		pkgs, err := packages.Load(cfg, paths...)
		if err != nil {
			loadErr = err
			return
		}
		loadedPkgs = map[string]*packages.Package{}
		for _, pkg := range pkgs {
			if len(pkg.Errors) > 0 {
				loadErr = fmt.Errorf("load %s: %v", pkg.PkgPath, pkg.Errors[0])
				return
			}
			loadedPkgs[pkg.PkgPath] = pkg
		}
	})
	return loadedPkgs, loadErr
}

// parseDirective reports whether doc carries the directive, and the name it
// renames to, if any.
func parseDirective(doc *ast.CommentGroup) (bool, string, error) {
	if doc == nil {
		return false, "", nil
	}
	found, rename := false, ""
	for _, c := range doc.List {
		if !strings.HasPrefix(c.Text, directive) {
			continue
		}
		rest := strings.TrimPrefix(c.Text, directive)
		if rest != "" && !strings.HasPrefix(rest, " ") {
			return false, "", fmt.Errorf("malformed directive %q", c.Text)
		}
		if found {
			return false, "", fmt.Errorf("directive repeated in one doc comment")
		}
		found, rename = true, strings.TrimSpace(rest)
	}
	return found, rename, nil
}

// docText returns doc's text with the directive removed, and with a leading
// mention of the engine name rewritten to the sdk name.
func docText(doc *ast.CommentGroup, engine, name string) string {
	if doc == nil {
		return ""
	}
	text := strings.TrimRight(doc.Text(), "\n")
	if engine != name && strings.HasPrefix(text, engine+" ") {
		text = name + strings.TrimPrefix(text, engine)
	}
	return text
}

// collectExports scans the engine packages for directives.
func collectExports(pkgs map[string]*packages.Package) ([]group, error) {
	var groups []group
	paths := make([]string, 0, len(pkgs))
	for path := range pkgs {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		pkg := pkgs[path]
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				found, err := declExports(pkg, decl)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", pkg.Fset.Position(decl.Pos()), err)
				}
				groups = append(groups, found...)
			}
		}
	}
	renameMentions(groups)
	return groups, nil
}

// renameMentions rewrites every doc's mention of a renamed export to the name
// the sdk gives it, so a doc never names a declaration the sdk does not have.
func renameMentions(groups []group) {
	var pairs []string
	for _, g := range groups {
		for _, m := range g.members {
			if m.name != m.engine {
				pairs = append(pairs, m.engine, m.name)
			}
		}
	}
	if len(pairs) == 0 {
		return
	}
	rewrite := func(text string) string {
		for i := 0; i < len(pairs); i += 2 {
			text = regexp.MustCompile(`\b`+regexp.QuoteMeta(pairs[i])+`\b`).ReplaceAllString(text, pairs[i+1])
		}
		return text
	}
	for i := range groups {
		groups[i].doc = rewrite(groups[i].doc)
		for j := range groups[i].members {
			groups[i].members[j].doc = rewrite(groups[i].members[j].doc)
		}
	}
}

// declExports returns the groups decl contributes.
func declExports(pkg *packages.Package, decl ast.Decl) ([]group, error) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		ok, rename, err := parseDirective(d.Doc)
		if err != nil || !ok {
			return nil, err
		}
		if d.Recv != nil {
			return nil, fmt.Errorf("directive on method %s: only package-level declarations can be exported", d.Name.Name)
		}
		e, err := newExport(pkg, kindFunc, d.Name, rename, d.Doc)
		if err != nil {
			return nil, err
		}
		return []group{{kind: kindFunc, doc: e.doc, members: []export{e}}}, nil

	case *ast.GenDecl:
		kind, ok := map[token.Token]exportKind{token.CONST: kindConst, token.VAR: kindVar, token.TYPE: kindType}[d.Tok]
		if !ok {
			return nil, nil
		}
		declOK, declRename, err := parseDirective(d.Doc)
		if err != nil {
			return nil, err
		}
		single := d.Lparen == token.NoPos

		var members []export
		for _, spec := range d.Specs {
			names, specDoc := specNames(spec)
			specOK, specRename, err := parseDirective(specDoc)
			if err != nil {
				return nil, err
			}
			if !declOK && !specOK {
				continue
			}
			rename := specRename
			if declOK {
				rename = declRename
			}
			if rename != "" && (len(names) != 1 || (declOK && !single)) {
				return nil, fmt.Errorf("directive renames %s, but it covers more than one name", names[0].Name)
			}
			doc := specDoc
			if single {
				doc = d.Doc
			}
			for _, ident := range names {
				e, err := newExport(pkg, kind, ident, rename, doc)
				if err != nil {
					return nil, err
				}
				members = append(members, e)
			}
		}
		if len(members) == 0 {
			return nil, nil
		}
		if kind == kindType {
			var out []group
			for _, m := range members {
				out = append(out, group{kind: kindType, doc: m.doc, members: []export{m}})
			}
			return out, nil
		}
		g := group{kind: kind, members: members}
		if single {
			g.doc = members[0].doc
			members[0].doc = ""
		} else {
			g.doc = strings.TrimRight(d.Doc.Text(), "\n")
		}
		return []group{g}, nil
	}
	return nil, nil
}

// specNames returns the names a spec declares and its own doc comment.
func specNames(spec ast.Spec) ([]*ast.Ident, *ast.CommentGroup) {
	switch s := spec.(type) {
	case *ast.ValueSpec:
		return s.Names, s.Doc
	case *ast.TypeSpec:
		return []*ast.Ident{s.Name}, s.Doc
	}
	return nil, nil
}

// newExport builds the export for ident.
func newExport(pkg *packages.Package, kind exportKind, ident *ast.Ident, rename string, doc *ast.CommentGroup) (export, error) {
	if !ident.IsExported() {
		return export{}, fmt.Errorf("directive on unexported %s", ident.Name)
	}
	name := ident.Name
	if rename != "" {
		if !token.IsExported(rename) || !token.IsIdentifier(rename) {
			return export{}, fmt.Errorf("directive renames %s to %q, which is not an exported identifier", ident.Name, rename)
		}
		name = rename
	}
	obj := pkg.TypesInfo.Defs[ident]
	if obj == nil {
		return export{}, fmt.Errorf("no type information for %s", ident.Name)
	}
	return export{kind: kind, name: name, engine: ident.Name, pkg: pkg.PkgPath, doc: docText(doc, ident.Name, name), obj: obj}, nil
}

// collect records every exported named engine type t is, or reaches through
// exported struct fields and pointer, slice, array and map elements. A named
// type is recorded before its underlying type is walked, so a named slice
// such as `type X []Y` gets an alias for X as well as Y.
func collect(t types.Type, seen map[*types.TypeName]bool) {
	t = types.Unalias(t)
	if named, ok := t.(*types.Named); ok {
		obj := named.Obj()
		if obj.Pkg() == nil || !obj.Exported() {
			return
		}
		if _, engine := enginePackages[obj.Pkg().Path()]; !engine {
			return
		}
		if seen[obj] {
			return
		}
		seen[obj] = true
		t = named.Underlying()
	}

	switch u := t.(type) {
	case *types.Pointer:
		collect(u.Elem(), seen)
	case *types.Slice:
		collect(u.Elem(), seen)
	case *types.Array:
		collect(u.Elem(), seen)
	case *types.Map:
		collect(u.Key(), seen)
		collect(u.Elem(), seen)
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if field := u.Field(i); field.Exported() {
				collect(field.Type(), seen)
			}
		}
	}
}

// collectSignature walks sig's parameter, result and type parameter
// constraint types.
func collectSignature(sig *types.Signature, seen map[*types.TypeName]bool) {
	for i := 0; i < sig.Params().Len(); i++ {
		collect(sig.Params().At(i).Type(), seen)
	}
	for i := 0; i < sig.Results().Len(); i++ {
		collect(sig.Results().At(i).Type(), seen)
	}
	for i := 0; i < sig.TypeParams().Len(); i++ {
		collect(sig.TypeParams().At(i).Constraint(), seen)
	}
}

// buildSurface returns everything the generator writes.
func buildSurface() (*surface, error) {
	pkgs, err := loadEngine()
	if err != nil {
		return nil, err
	}
	groups, err := collectExports(pkgs)
	if err != nil {
		return nil, err
	}

	seen := map[*types.TypeName]bool{}
	for _, g := range groups {
		for _, m := range g.members {
			switch m.kind {
			case kindType:
				collect(m.obj.Type(), seen)
			case kindFunc:
				collectSignature(m.obj.Type().(*types.Signature), seen)
			default:
				collect(m.obj.Type(), seen)
			}
		}
	}

	scope := pkgs[tasksPackage].Types.Scope()
	for _, typeKey := range tasks.TaskTypes() {
		task, ok := tasks.Lookup(typeKey)
		if !ok {
			return nil, fmt.Errorf("task type %q is listed but not registered", typeKey)
		}
		name := reflect.TypeOf(task).Elem().Name()
		obj, ok := scope.Lookup(name).(*types.TypeName)
		if !ok {
			return nil, fmt.Errorf("task type %q is %s, which the tasks package does not declare", typeKey, name)
		}
		collect(obj.Type(), seen)
	}

	return &surface{groups: groups, aliased: seen, pkgs: pkgs}, nil
}

// typesByName indexes the aliased types by name, refusing two that share one,
// since the sdk can alias only one of them. Types are walked by package path
// then name, so the refusal names the pair in the same order every run.
func typesByName(seen map[*types.TypeName]bool) (map[string]*types.TypeName, error) {
	ordered := make([]*types.TypeName, 0, len(seen))
	for t := range seen {
		ordered = append(ordered, t)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Pkg().Path() != ordered[j].Pkg().Path() {
			return ordered[i].Pkg().Path() < ordered[j].Pkg().Path()
		}
		return ordered[i].Name() < ordered[j].Name()
	})
	names := map[string]*types.TypeName{}
	for _, t := range ordered {
		if other, ok := names[t.Name()]; ok {
			return nil, fmt.Errorf("type name %s is declared in both %s and %s", t.Name(), other.Pkg().Path(), t.Pkg().Path())
		}
		names[t.Name()] = t
	}
	return names, nil
}

// checkNames refuses an sdk name exported twice, whether by two directives or
// by a directive and an aliased type.
func checkNames(s *surface, aliases map[string]*types.TypeName) error {
	owner := map[string]string{}
	for name, t := range aliases {
		owner[name] = "type " + t.Pkg().Path() + "." + t.Name()
	}
	for _, g := range s.groups {
		for _, m := range g.members {
			if m.kind == kindType {
				if m.name != m.engine {
					return fmt.Errorf("type %s.%s is renamed to %s, but types are aliased under their own name", m.pkg, m.engine, m.name)
				}
				continue
			}
			this := m.pkg + "." + m.engine
			if other, ok := owner[m.name]; ok {
				return fmt.Errorf("sdk name %s is exported by both %s and %s", m.name, other, this)
			}
			owner[m.name] = this
		}
	}
	return nil
}

// render returns the formatted contents of every generated file, by name.
func render() (map[string][]byte, error) {
	s, err := buildSurface()
	if err != nil {
		return nil, err
	}
	aliases, err := typesByName(s.aliased)
	if err != nil {
		return nil, err
	}
	if err := checkNames(s, aliases); err != nil {
		return nil, err
	}

	funcs, err := renderFuncs(s)
	if err != nil {
		return nil, err
	}
	typesSrc, err := renderTypes(aliases)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{funcsFile: funcs, typesFile: typesSrc}, nil
}

// header writes the generated-file header and the import block.
func header(buf *bytes.Buffer, imports map[string]string) {
	buf.WriteString("// Code generated by generate/sdk; DO NOT EDIT.\n\n")
	buf.WriteString("package sdk\n\n")
	// The standard library first, then the module's own packages, as goimports
	// groups them.
	var std, module []string
	for path := range imports {
		if strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
			module = append(module, path)
		} else {
			std = append(std, path)
		}
	}
	sort.Strings(std)
	sort.Strings(module)
	buf.WriteString("import (\n")
	for _, path := range std {
		fmt.Fprintf(buf, "\t%q\n", path)
	}
	if len(std) > 0 && len(module) > 0 {
		buf.WriteString("\n")
	}
	for _, path := range module {
		fmt.Fprintf(buf, "\t%q\n", path)
	}
	buf.WriteString(")\n\n")
}

// writeDoc writes text as a doc comment at indent.
func writeDoc(buf *bytes.Buffer, indent, text string) {
	if text == "" {
		return
	}
	for _, line := range strings.Split(text, "\n") {
		switch {
		case line == "":
			buf.WriteString(indent + "//\n")
		case strings.HasPrefix(line, "\t"):
			buf.WriteString(indent + "//" + line + "\n")
		default:
			buf.WriteString(indent + "// " + line + "\n")
		}
	}
}

// renderTypes returns sdk/types_gen.go.
func renderTypes(aliases map[string]*types.TypeName) ([]byte, error) {
	names := make([]string, 0, len(aliases))
	imports := map[string]string{}
	for name, t := range aliases {
		names = append(names, name)
		imports[t.Pkg().Path()] = enginePackages[t.Pkg().Path()]
	}
	sort.Strings(names)

	var buf bytes.Buffer
	header(&buf, imports)
	for _, name := range names {
		qualified := enginePackages[aliases[name].Pkg().Path()] + "." + name
		fmt.Fprintf(&buf, "// %s is %s.\n", name, qualified)
		fmt.Fprintf(&buf, "type %s = %s\n\n", name, qualified)
	}
	return format.Source(buf.Bytes())
}

// groupKey sorts groups by their first sdk name.
func groupKey(g group) string { return g.members[0].name }

// renderFuncs returns sdk/sdk_gen.go.
func renderFuncs(s *surface) ([]byte, error) {
	var groups []group
	for _, g := range s.groups {
		if g.kind != kindType {
			groups = append(groups, g)
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groupKey(groups[i]) < groupKey(groups[j]) })

	imports := map[string]string{}
	var body bytes.Buffer
	for _, g := range groups {
		switch g.kind {
		case kindFunc:
			if err := writeFunc(&body, s, g.members[0], imports); err != nil {
				return nil, err
			}
		case kindConst, kindVar:
			writeValues(&body, g, imports)
		}
		body.WriteString("\n")
	}

	var buf bytes.Buffer
	header(&buf, imports)
	buf.Write(body.Bytes())
	return format.Source(buf.Bytes())
}

// writeValues writes a const or var group.
func writeValues(buf *bytes.Buffer, g group, imports map[string]string) {
	keyword := "const"
	if g.kind == kindVar {
		keyword = "var"
	}
	writeDoc(buf, "", g.doc)
	if len(g.members) == 1 && g.members[0].doc == "" {
		m := g.members[0]
		imports[m.pkg] = enginePackages[m.pkg]
		fmt.Fprintf(buf, "%s %s = %s.%s\n", keyword, m.name, enginePackages[m.pkg], m.engine)
		return
	}
	fmt.Fprintf(buf, "%s (\n", keyword)
	for i, m := range g.members {
		imports[m.pkg] = enginePackages[m.pkg]
		if i > 0 && m.doc != "" {
			buf.WriteString("\n")
		}
		writeDoc(buf, "\t", m.doc)
		fmt.Fprintf(buf, "\t%s = %s.%s\n", m.name, enginePackages[m.pkg], m.engine)
	}
	buf.WriteString(")\n")
}

// writeFunc writes a wrapper for one function.
func writeFunc(buf *bytes.Buffer, s *surface, m export, imports map[string]string) error {
	sig := m.obj.Type().(*types.Signature)
	var qualErr error
	qualifier := func(p *types.Package) string {
		if _, engine := enginePackages[p.Path()]; engine {
			return ""
		}
		imports[p.Path()] = p.Name()
		return p.Name()
	}
	typeString := func(t types.Type) string {
		if err := checkAliased(s, t); err != nil && qualErr == nil {
			qualErr = fmt.Errorf("%s.%s: %w", m.pkg, m.engine, err)
		}
		return types.TypeString(t, qualifier)
	}

	var typeParams, typeArgs []string
	for i := 0; i < sig.TypeParams().Len(); i++ {
		tp := sig.TypeParams().At(i)
		typeParams = append(typeParams, tp.Obj().Name()+" "+typeString(tp.Constraint()))
		typeArgs = append(typeArgs, tp.Obj().Name())
	}

	var params, args []string
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		name := p.Name()
		if name == "" || name == "_" {
			name = fmt.Sprintf("p%d", i)
		}
		t := typeString(p.Type())
		arg := name
		if sig.Variadic() && i == sig.Params().Len()-1 {
			t = "..." + typeString(p.Type().(*types.Slice).Elem())
			arg += "..."
		}
		params = append(params, name+" "+t)
		args = append(args, arg)
	}

	var results []string
	for i := 0; i < sig.Results().Len(); i++ {
		results = append(results, typeString(sig.Results().At(i).Type()))
	}
	if qualErr != nil {
		return qualErr
	}

	imports[m.pkg] = enginePackages[m.pkg]
	writeDoc(buf, "", m.doc)
	fmt.Fprintf(buf, "func %s", m.name)
	if len(typeParams) > 0 {
		fmt.Fprintf(buf, "[%s]", strings.Join(typeParams, ", "))
	}
	fmt.Fprintf(buf, "(%s)", strings.Join(params, ", "))
	switch len(results) {
	case 0:
	case 1:
		buf.WriteString(" " + results[0])
	default:
		fmt.Fprintf(buf, " (%s)", strings.Join(results, ", "))
	}
	call := enginePackages[m.pkg] + "." + m.engine
	if len(typeArgs) > 0 {
		call += "[" + strings.Join(typeArgs, ", ") + "]"
	}
	call += "(" + strings.Join(args, ", ") + ")"
	if len(results) > 0 {
		call = "return " + call
	}
	fmt.Fprintf(buf, " {\n\t%s\n}\n", call)
	return nil
}

// checkAliased refuses an engine type in t that the sdk does not alias, which
// a wrapper could not name.
func checkAliased(s *surface, t types.Type) error {
	for _, leaked := range unaliased(t, s.aliased) {
		return fmt.Errorf("uses %s, which the sdk does not alias", leaked)
	}
	return nil
}

// unaliased returns the engine types t names that are not in aliased: t
// itself when it is a named type, else the types it is built from.
func unaliased(t types.Type, aliased map[*types.TypeName]bool) []*types.TypeName {
	var out []*types.TypeName
	var visit func(types.Type)
	visit = func(t types.Type) {
		t = types.Unalias(t)
		switch u := t.(type) {
		case *types.Named:
			// A named type is either aliased or not; its underlying type is the
			// sdk's concern only through collect.
			obj := u.Obj()
			if obj.Pkg() != nil {
				if _, engine := enginePackages[obj.Pkg().Path()]; engine && !aliased[obj] {
					out = append(out, obj)
				}
			}
		case *types.Pointer:
			visit(u.Elem())
		case *types.Slice:
			visit(u.Elem())
		case *types.Array:
			visit(u.Elem())
		case *types.Chan:
			visit(u.Elem())
		case *types.Map:
			visit(u.Key())
			visit(u.Elem())
		case *types.Signature:
			for i := 0; i < u.Params().Len(); i++ {
				visit(u.Params().At(i).Type())
			}
			for i := 0; i < u.Results().Len(); i++ {
				visit(u.Results().At(i).Type())
			}
		}
	}
	visit(t)
	return out
}

func main() {
	files, err := render()
	if err != nil {
		log.Fatalf("failed to render the sdk: %v", err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.WriteFile(name, files[name], 0644); err != nil {
			log.Fatalf("failed to write %s: %v", name, err)
		}
	}
}
