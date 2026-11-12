package relevance

import (
	"bytes"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/printer"
	"go/token"
	"reflect"
	"strconv"
	"strings"
)

// goAnalyzer detects changes of the exported API of Go source files using
// go/parser. It builds a canonical description of each side's exported API and
// reports a change when the two differ.
//
// API surface: exported functions; exported methods whose receiver base type is
// exported; exported type declarations (structs keep exported fields, with their
// tags, and all embedded fields; interfaces keep exported methods
// and embedded types); exported constants (type and value) and variables (type;
// the value only when no type is declared). Parameter and receiver names, bodies,
// comments, imports, unexported declarations, formatting and declaration order
// never count. The package name and the build constraint lines (//go:build and
// legacy // +build before the package clause, normalized) are compared too,
// since they decide which package exports the API; no other comment is read.
// A side that does not parse counts as changed. An added or deleted file is
// changed iff the present side has exported API.
//
// Expanding grouped names ("a, b int" is two parameters) can blow up on hostile
// sources with nested groups, so before any expansion the analyzer bounds the
// expanded size of each symbol (maxSymbolNodes) and of the whole file
// (maxFileNodes); a source above either limit counts as changed (fail closed).
//
// Known limits: there is no type-checking. Exported consts, vars and types that
// refer to unexported package-level identifiers are compared textually, so a
// change of the referenced declaration (for example "const Max = max" with max
// changing, "type Foo bar" with bar changing, or the body of an unexported
// embedded type) is not seen; only the embed line itself counts. Adding or
// removing an unexported spec inside a const group shifts the group index of
// later exported consts and is reported as changed (conservative). An unexported
// method added to or removed from an exported interface breaks external
// implementers but is not compared (only the exported API is). Other directives
// (//go:generate, //go:linkname, //go:embed) do not change the declared
// exported symbols or types and are ignored with all other comments.
type goAnalyzer struct{}

// Size limits, in AST nodes after expansion of grouped names. Real code stays
// orders of magnitude below them.
const (
	maxSymbolNodes = 1 << 14
	maxFileNodes   = 1 << 17
)

func (goAnalyzer) Extensions() []string { return []string{".go"} }

func (goAnalyzer) APIChanged(oldSrc, newSrc []byte) (bool, error) {
	oldAPI, oldOK := goExportedAPI(oldSrc)
	newAPI, newOK := goExportedAPI(newSrc)
	if !oldOK || !newOK {
		return true, nil
	}
	// An added or deleted file is changed iff the present side has exported API;
	// its package name and build constraints alone are not API.
	if len(oldSrc) == 0 {
		return len(newAPI.symbols) > 0, nil
	}
	if len(newSrc) == 0 {
		return len(oldAPI.symbols) > 0, nil
	}
	if oldAPI.header != newAPI.header || len(oldAPI.symbols) != len(newAPI.symbols) {
		return true, nil
	}
	for key, text := range oldAPI.symbols {
		if other, ok := newAPI.symbols[key]; !ok || other != text {
			return true, nil
		}
	}
	return false, nil
}

// goSurface is the canonical comparable form of one source file.
type goSurface struct {
	// header holds the package name and the build constraint lines.
	header string
	// symbols maps a symbol key to the canonical text of its declaration.
	symbols map[string]string
}

// goExportedAPI builds the surface of one file. An empty source is an absent
// file with an empty surface; ok is false on parse errors and when the file
// exceeds the size limits.
func goExportedAPI(src []byte) (surface goSurface, ok bool) {
	api := map[string]string{}
	surface.symbols = api
	if len(src) == 0 {
		return surface, true
	}
	// Declarations are parsed without comments, so comments can never influence them.
	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		return goSurface{}, false
	}
	surface.header = goFileHeader(src, file.Name.Name)
	budget := &nodeBudget{}
	for _, decl := range file.Decls {
		within := true
		switch d := decl.(type) {
		case *ast.FuncDecl:
			within = addGoFunc(api, budget, d)
		case *ast.GenDecl:
			within = addGoGenDecl(api, budget, d)
		}
		if !within {
			return goSurface{}, false
		}
	}
	return surface, true
}

// goFileHeader returns the package name plus the normalized build constraint
// lines (//go:build and legacy // +build) found before the package clause. The
// source is parsed again up to the package clause only, with comments, so no
// other comment is ever looked at.
func goFileHeader(src []byte, packageName string) string {
	var b strings.Builder
	b.WriteString("package " + packageName)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments|parser.PackageClauseOnly)
	if err != nil {
		return b.String()
	}
	for _, group := range file.Comments {
		if group.End() > file.Package {
			break
		}
		for _, c := range group.List {
			if line, ok := buildConstraintLine(c.Text); ok {
				b.WriteString("\n" + line)
			}
		}
	}
	return b.String()
}

// buildConstraintLine normalizes a build constraint comment; ok is false for
// every other comment.
func buildConstraintLine(comment string) (line string, ok bool) {
	text := strings.TrimSpace(comment)
	switch {
	case constraint.IsGoBuild(text):
		line = "//go:build "
	case constraint.IsPlusBuild(text):
		line = "// +build "
	default:
		return "", false
	}
	expr, err := constraint.Parse(text)
	if err != nil {
		// Unparsable constraint: compare the raw text, which is conservative.
		return text, true
	}
	return line + expr.String(), true
}

func addGoFunc(api map[string]string, budget *nodeBudget, d *ast.FuncDecl) bool {
	if !d.Name.IsExported() {
		return true
	}
	key := "func " + d.Name.Name
	if d.Recv != nil && len(d.Recv.List) == 1 {
		base := receiverBaseName(d.Recv.List[0].Type)
		if !ast.IsExported(base) {
			return true
		}
		key = "method " + base + "." + d.Name.Name
	}
	d.Body = nil
	d.Doc = nil
	if !budget.allow(expandedSize(d)) {
		return false
	}
	canonicalize(d)
	api[key] = printNode(d)
	return true
}

// receiverBaseName returns the receiver's type name without pointer and type arguments.
func receiverBaseName(expr ast.Expr) string {
	for {
		switch e := expr.(type) {
		case *ast.StarExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.IndexListExpr:
			expr = e.X
		case *ast.Ident:
			return e.Name
		default:
			return ""
		}
	}
}

func addGoGenDecl(api map[string]string, budget *nodeBudget, d *ast.GenDecl) bool {
	var lastType ast.Expr
	var lastValues []ast.Expr
	for index, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if !s.Name.IsExported() {
				continue
			}
			s.Doc, s.Comment = nil, nil
			if !budget.allow(expandedSize(s)) {
				return false
			}
			pruneUnexported(s.Type)
			canonicalize(s)
			api["type "+s.Name.Name] = printNode(s)
		case *ast.ValueSpec:
			if d.Tok == token.CONST {
				// A const spec without values repeats the previous expression list.
				if len(s.Values) == 0 && s.Type == nil {
					s = &ast.ValueSpec{Names: s.Names, Type: lastType, Values: lastValues}
				} else {
					lastType, lastValues = s.Type, s.Values
				}
			}
			if !addGoValueSpec(api, budget, d.Tok, s, index) {
				return false
			}
		}
	}
	return true
}

func addGoValueSpec(api map[string]string, budget *nodeBudget, tok token.Token, s *ast.ValueSpec, index int) bool {
	for i, name := range s.Names {
		if !name.IsExported() {
			continue
		}
		value := s.Values
		if len(s.Values) == len(s.Names) {
			value = s.Values[i : i+1]
		}
		withValue := tok == token.CONST || s.Type == nil
		cost := 1
		if s.Type != nil {
			cost += expandedSize(s.Type)
		}
		if withValue {
			for _, v := range value {
				cost += expandedSize(v)
			}
		}
		if !budget.allow(cost) {
			return false
		}
		text := ""
		if s.Type != nil {
			text = printNode(canonicalize(s.Type))
		}
		if withValue {
			for _, v := range value {
				text += " = " + printNode(canonicalize(v))
			}
		}
		if tok == token.CONST {
			// iota-dependent values change with the position inside the group.
			text += " #" + strconv.Itoa(index)
		}
		api[tok.String()+" "+name.Name] = text
	}
	return true
}

// nodeBudget accumulates the expanded size of everything canonicalized in one file.
type nodeBudget struct {
	total int
}

// allow charges size against the budget and reports whether both the per-symbol
// and the per-file limit still hold.
func (b *nodeBudget) allow(size int) bool {
	if size > maxSymbolNodes {
		return false
	}
	b.total += size
	return b.total <= maxFileNodes
}

// expandedSize is an upper bound of the node count of node after grouped names
// are expanded into one field each. It reads the tree before any expansion and
// stops counting once the symbol limit is exceeded, so it is cheap for any
// input and never materializes the expansion. The result is at most
// maxSymbolNodes+1.
func expandedSize(node ast.Node) int {
	return estimateNodes(reflect.ValueOf(node), maxSymbolNodes)
}

var fieldListType = reflect.TypeOf(ast.FieldList{})

// estimateNodes counts the nodes below v, multiplying the type of every field
// that declares several names, and returns at most limit+1.
func estimateNodes(v reflect.Value, limit int) int {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return 0
		}
		return estimateNodes(v.Elem(), limit)
	case reflect.Slice:
		total := 0
		for i := 0; i < v.Len() && total <= limit; i++ {
			total += estimateNodes(v.Index(i), limit-total)
		}
		return min(total, limit+1)
	case reflect.Struct:
		if v.Type() == fieldListType {
			return estimateFieldList(v.FieldByName("List"), limit)
		}
		total := 1
		for i := 0; i < v.NumField() && total <= limit; i++ {
			total += estimateNodes(v.Field(i), limit-total)
		}
		return min(total, limit+1)
	default:
		return 0
	}
}

func estimateFieldList(list reflect.Value, limit int) int {
	total := 1
	for i := 0; i < list.Len() && total <= limit; i++ {
		field := list.Index(i).Interface().(*ast.Field)
		copies := max(len(field.Names), 1)
		inner := estimateNodes(reflect.ValueOf(field.Type), limit)
		if copies*(inner+1) > limit {
			return limit + 1
		}
		total += copies * (inner + 1)
	}
	return min(total, limit+1)
}

// pruneUnexported removes everything from struct and interface types that is
// not part of the public API, recursively.
func pruneUnexported(node ast.Node) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.StructType:
			t.Fields.List = exportedFields(t.Fields.List)
		case *ast.InterfaceType:
			t.Methods.List = exportedFields(t.Methods.List)
		}
		return true
	})
}

// exportedFields keeps the exported named fields and every embedded field.
// Embedded fields are kept even when their type is unexported: the exported
// fields and methods promoted through them are part of the public API, so
// adding, removing or retyping the embed itself is an API change.
func exportedFields(list []*ast.Field) []*ast.Field {
	var kept []*ast.Field
	for _, f := range list {
		if len(f.Names) == 0 {
			kept = append(kept, f)
			continue
		}
		// One field per name, so "X, Y int" equals two separate fields.
		for _, n := range f.Names {
			if n.IsExported() {
				kept = append(kept, &ast.Field{Names: []*ast.Ident{n}, Type: f.Type, Tag: f.Tag})
			}
		}
	}
	return kept
}

var tokenPosType = reflect.TypeOf(token.NoPos)

// canonicalize makes the printed form independent of the source layout: it
// drops parameter names while keeping the parameter count (names are not part of
// a signature's identity, grouped names like "a, b int" still are two parameters) and
// clears every position, which the printer would otherwise use to choose
// between one-line and multi-line output.
func canonicalize[N ast.Node](node N) N {
	ast.Inspect(node, func(n ast.Node) bool {
		if ft, ok := n.(*ast.FuncType); ok {
			dropNames(ft.Params)
			dropNames(ft.Results)
		}
		return true
	})
	if d, ok := ast.Node(node).(*ast.FuncDecl); ok && d.Recv != nil {
		dropNames(d.Recv)
	}
	clearPositions(reflect.ValueOf(node))
	return node
}

func dropNames(list *ast.FieldList) {
	if list == nil {
		return
	}
	// A field with N names stands for N parameters: keep the arity by expanding it
	// into N unnamed fields that share the type.
	var expanded []*ast.Field
	for _, f := range list.List {
		count := max(len(f.Names), 1)
		for range count {
			expanded = append(expanded, &ast.Field{Type: f.Type})
		}
	}
	list.List = expanded
}

func clearPositions(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			clearPositions(v.Elem())
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			clearPositions(v.Index(i))
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.Type() == tokenPosType && field.CanSet() {
				field.SetInt(canonicalPos(v.Type().Field(i).Name, token.Pos(field.Int())))
				continue
			}
			if field.CanSet() {
				clearPositions(field)
			}
		}
	}
}

// canonicalPos returns the position to store in place of pos. Positions are
// cleared, except for the fields whose mere validity changes the printed text
// (go/printer checks IsValid on them): TypeSpec.Assign prints "=" for an alias
// and CallExpr.Ellipsis prints "..." for a spread argument. Those keep a fixed
// valid position so the syntax survives while the source layout does not.
func canonicalPos(fieldName string, pos token.Pos) int64 {
	if (fieldName == "Assign" || fieldName == "Ellipsis") && pos.IsValid() {
		return 1
	}
	return 0
}

func printNode(node ast.Node) string {
	var buf bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces, Tabwidth: 8}
	if err := cfg.Fprint(&buf, token.NewFileSet(), node); err != nil {
		// Printing an AST that just parsed cannot fail; stay conservative if it does.
		return "unprintable:" + strconv.Itoa(buf.Len())
	}
	return buf.String()
}
