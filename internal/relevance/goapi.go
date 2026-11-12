package relevance

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"reflect"
	"strconv"
)

// goAnalyzer detects changes of the exported API of Go source files using
// go/parser. It builds a canonical description of each side's exported API and
// reports a change when the two differ.
//
// API surface: exported functions; exported methods whose receiver base type is
// exported; exported type declarations (structs keep only exported fields and
// embedded exported types, with their tags; interfaces keep exported methods
// and embedded types); exported constants (type and value) and variables (type;
// the value only when no type is declared). Parameter and receiver names, bodies,
// comments, imports, unexported declarations, formatting and declaration order
// never count. A side that does not parse counts as changed.
type goAnalyzer struct{}

func (goAnalyzer) Extensions() []string { return []string{".go"} }

func (goAnalyzer) APIChanged(oldSrc, newSrc []byte) (bool, error) {
	oldAPI, oldOK := goExportedAPI(oldSrc)
	newAPI, newOK := goExportedAPI(newSrc)
	if !oldOK || !newOK {
		return true, nil
	}
	if len(oldAPI) != len(newAPI) {
		return true, nil
	}
	for key, text := range oldAPI {
		if other, ok := newAPI[key]; !ok || other != text {
			return true, nil
		}
	}
	return false, nil
}

// goExportedAPI maps a symbol key to the canonical text of its declaration. An
// empty source is an absent file with an empty API; ok is false on parse errors.
func goExportedAPI(src []byte) (api map[string]string, ok bool) {
	api = map[string]string{}
	if len(src) == 0 {
		return api, true
	}
	// Comments are not parsed, so they can never influence the result.
	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		return nil, false
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			addGoFunc(api, d)
		case *ast.GenDecl:
			addGoGenDecl(api, d)
		}
	}
	return api, true
}

func addGoFunc(api map[string]string, d *ast.FuncDecl) {
	if !d.Name.IsExported() {
		return
	}
	key := "func " + d.Name.Name
	if d.Recv != nil && len(d.Recv.List) == 1 {
		base := receiverBaseName(d.Recv.List[0].Type)
		if !ast.IsExported(base) {
			return
		}
		key = "method " + base + "." + d.Name.Name
	}
	d.Body = nil
	d.Doc = nil
	canonicalize(d)
	api[key] = printNode(d)
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

func addGoGenDecl(api map[string]string, d *ast.GenDecl) {
	var lastType ast.Expr
	var lastValues []ast.Expr
	for index, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if !s.Name.IsExported() {
				continue
			}
			s.Doc, s.Comment = nil, nil
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
			addGoValueSpec(api, d.Tok, s, index)
		}
	}
}

func addGoValueSpec(api map[string]string, tok token.Token, s *ast.ValueSpec, index int) {
	for i, name := range s.Names {
		if !name.IsExported() {
			continue
		}
		text := ""
		if s.Type != nil {
			text = printNode(canonicalize(s.Type))
		}
		if tok == token.CONST || s.Type == nil {
			value := s.Values
			if len(s.Values) == len(s.Names) {
				value = s.Values[i : i+1]
			}
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
}

// pruneUnexported removes everything from struct and interface types that is
// not part of the public API, recursively.
func pruneUnexported(node ast.Node) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.StructType:
			t.Fields.List = exportedFields(t.Fields.List, false)
		case *ast.InterfaceType:
			t.Methods.List = exportedFields(t.Methods.List, true)
		}
		return true
	})
}

// exportedFields keeps the exported fields. Embedded fields (no names) are kept
// when their type name is exported; interface embeds are always kept because
// they can contribute exported methods.
func exportedFields(list []*ast.Field, keepAllEmbedded bool) []*ast.Field {
	var kept []*ast.Field
	for _, f := range list {
		if len(f.Names) == 0 {
			if keepAllEmbedded || ast.IsExported(embeddedName(f.Type)) {
				kept = append(kept, f)
			}
			continue
		}
		var names []*ast.Ident
		for _, n := range f.Names {
			if n.IsExported() {
				names = append(names, n)
			}
		}
		if len(names) > 0 {
			f.Names = names
			kept = append(kept, f)
		}
	}
	return kept
}

func embeddedName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return embeddedName(e.X)
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.IndexExpr:
		return embeddedName(e.X)
	case *ast.IndexListExpr:
		return embeddedName(e.X)
	case *ast.Ident:
		return e.Name
	default:
		return ""
	}
}

var tokenPosType = reflect.TypeOf(token.NoPos)

// canonicalize makes the printed form independent of the source layout: it
// drops parameter names (they are not part of a signature's identity) and
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
	for _, f := range list.List {
		f.Names = nil
	}
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
