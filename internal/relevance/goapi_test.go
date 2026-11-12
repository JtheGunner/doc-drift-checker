package relevance

import (
	"strings"
	"testing"
)

const goBase = "package p\n" +
	"\n" +
	"import \"fmt\"\n" +
	"\n" +
	"// Doc for F.\n" +
	"func F(a int) string { return fmt.Sprint(a) }\n" +
	"\n" +
	"type T struct {\n" +
	"\tName string `json:\"name\"`\n" +
	"\tage  int\n" +
	"}\n" +
	"\n" +
	"func (t *T) M() {}\n" +
	"\n" +
	"type I interface {\n" +
	"\tDo() error\n" +
	"}\n" +
	"\n" +
	"const C = 1\n" +
	"\n" +
	"var V int\n" +
	"\n" +
	"func helper() {}\n"

const reordered = "package p\n" +
	"\n" +
	"var V int\n" +
	"\n" +
	"const C = 1\n" +
	"\n" +
	"func helper() {}\n" +
	"\n" +
	"type I interface {\n" +
	"\tDo() error\n" +
	"}\n" +
	"\n" +
	"func (t *T) M() {}\n" +
	"\n" +
	"type T struct {\n" +
	"\tage  int\n" +
	"\tName string `json:\"name\"`\n" +
	"}\n" +
	"\n" +
	"// Reworded.\n" +
	"func F(a int) string { return \"\" }\n"

func replace(t *testing.T, s, from, to string) string {
	t.Helper()
	if !strings.Contains(s, from) {
		t.Fatalf("fixture does not contain %q", from)
	}
	return strings.Replace(s, from, to, 1)
}

func TestGoAnalyzerAPIChanged(t *testing.T) {
	r := func(from, to string) string { return replace(t, goBase, from, to) }
	tests := []struct {
		name     string
		old, new string
		want     bool
	}{
		// not API changes
		{"identical", goBase, goBase, false},
		{"comment only", goBase, "// New header.\n" + goBase + "\n// trailing\n", false},
		{"doc comment edit", goBase, r("// Doc for F.", "// Completely different doc."), false},
		{"body only", goBase, r("return fmt.Sprint(a)", "return fmt.Sprint(a + 1)"), false},
		{"method body only", goBase, r("func (t *T) M() {}", "func (t *T) M() { _ = t.age }"), false},
		{"import only", goBase, r("import \"fmt\"", "import (\n\t\"fmt\"\n\t\"os\"\n)\n\nvar _ = os.Args"), false},
		{"formatting only", goBase, r("func F(a int) string {", "func   F( a   int )  string  {\n"), false},
		{"unexported func added", goBase, goBase + "\nfunc other() {}\n", false},
		{"unexported func changed", goBase, r("func helper() {}", "func helper(x int) int { return x }"), false},
		{"unexported func removed", goBase, r("func helper() {}", ""), false},
		{"unexported field added", goBase, r("age  int", "age  int\n\tother string"), false},
		{"unexported field removed", goBase, r("age  int", ""), false},
		{"unexported field type changed", goBase, r("age  int", "age  string"), false},
		{"unexported const and var", goBase, goBase + "\nconst k = 1\nvar v = 2\n", false},
		{"unexported type added", goBase, goBase + "\ntype u struct{ X int }\n", false},
		{"method on unexported type added", goBase, goBase + "\ntype u struct{}\n\nfunc (u) Exported() {}\n", false},
		{
			"method on unexported type changed",
			goBase + "\ntype u struct{}\n\nfunc (u) Exported() {}\n",
			goBase + "\ntype u struct{}\n\nfunc (u) Exported(x int) {}\n",
			false,
		},
		{"unexported method on exported type", goBase, goBase + "\nfunc (t *T) private() {}\n", false},
		{"one-line struct vs multi-line", "package p\ntype T struct{ A int }\n", "package p\ntype T struct {\n\tA int\n}\n", false},
		{"one-line interface vs multi-line", "package p\ntype I interface{ M() }\n", "package p\ntype I interface {\n\tM()\n}\n", false},
		{"multi-line params", "package p\nfunc F(a int, b string) {}\n", "package p\nfunc F(\n\ta int,\n\tb string,\n) {}\n", false},
		{"declaration order", goBase, reordered, false},
		{"unexported interface method", goBase, r("Do() error", "Do() error\n\tdone()"), false},
		{"parameter name only", goBase, r("func F(a int)", "func F(b int)"), false},

		// API changes
		{"exported func added", goBase, goBase + "\nfunc G() {}\n", true},
		{"exported func removed", goBase, r("func F(a int) string { return fmt.Sprint(a) }", ""), true},
		{"param type changed", goBase, r("func F(a int)", "func F(a int64)"), true},
		{"param added", goBase, r("func F(a int)", "func F(a int, b int)"), true},
		{"result changed", goBase, r("string { return", "(string, error) { return"), true},
		{"receiver kind changed", goBase, r("func (t *T) M()", "func (t T) M()"), true},
		{"exported method added", goBase, goBase + "\nfunc (t *T) N() {}\n", true},
		{"exported method removed", goBase, r("func (t *T) M() {}", ""), true},
		{"exported type added", goBase, goBase + "\ntype U int\n", true},
		{"exported type removed", goBase, r("type I interface {\n\tDo() error\n}", ""), true},
		{"type kind changed", "package p\ntype U int\n", "package p\ntype U string\n", true},
		{"alias changed", "package p\ntype U = int\n", "package p\ntype U = string\n", true},
		{"struct field added", goBase, r("age  int", "age  int\n\tExtra bool"), true},
		{"struct field removed", goBase, r("Name string `json:\"name\"`", ""), true},
		{"struct field type changed", goBase, r("Name string", "Name []byte"), true},
		{"struct tag changed", goBase, r("json:\"name\"", "json:\"title\""), true},
		{"embedded exported type added", goBase, r("age  int", "age  int\n\tfmt.Stringer"), true},
		{"interface method added", goBase, r("Do() error", "Do() error\n\tMore()"), true},
		{"interface method removed", goBase, r("Do() error", ""), true},
		{"interface embed added", goBase, r("Do() error", "Do() error\n\tfmt.Stringer"), true},
		{"const value changed", goBase, r("const C = 1", "const C = 2"), true},
		{"const added", goBase, goBase + "\nconst D = 3\n", true},
		{"const removed", goBase, r("const C = 1", ""), true},
		{"const type changed", "package p\nconst C int = 1\n", "package p\nconst C int64 = 1\n", true},
		{"const in group added", "package p\nconst (\n\tA = iota\n\tB\n)\n", "package p\nconst (\n\tA = iota\n\tB\n\tC\n)\n", true},
		{"var type changed", goBase, r("var V int", "var V string"), true},
		{"var added", goBase, goBase + "\nvar W int\n", true},
		{"type parameter changed", "package p\ntype L[T any] struct{ V T }\n", "package p\ntype L[T comparable] struct{ V T }\n", true},
		{"generic func constraint changed", "package p\nfunc G[T any](t T) {}\n", "package p\nfunc G[T ~int](t T) {}\n", true},
		{"nested struct field type changed", "package p\ntype T struct{ In struct{ A int } }\n", "package p\ntype T struct{ In struct{ A string } }\n", true},

		// parse failures are conservative
		{"new does not parse", goBase, "package p\nfunc (", true},
		{"old does not parse", "func (", goBase, true},
		{"both do not parse", "func (", "func ((", true},

		// added and deleted files
		{"added file with exported", "", "package p\nfunc F() {}\n", true},
		{"added file unexported only", "", "package p\nfunc f() {}\ntype t struct{ X int }\n", false},
		{"added file method on unexported type only", "", "package p\ntype t struct{}\nfunc (t) M() {}\n", false},
		{"added empty package", "", "package p\n", false},
		{"deleted file with exported", "package p\nfunc F() {}\n", "", true},
		{"deleted file unexported only", "package p\nfunc f() {}\n", "", false},
		{"added file that does not parse", "", "func (", true},
		{"deleted file that does not parse", "func (", "", true},
	}
	a := goAnalyzer{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := a.APIChanged([]byte(tt.old), []byte(tt.new))
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("APIChanged = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGoAnalyzerExtensions(t *testing.T) {
	exts := goAnalyzer{}.Extensions()
	if len(exts) != 1 || exts[0] != ".go" {
		t.Fatalf("Extensions() = %v, want [.go]", exts)
	}
}

func TestDefaultAnalyzersCoverGo(t *testing.T) {
	set := NewAnalyzerSet(DefaultAnalyzers()...)
	if set.For("a/b.go") == nil {
		t.Fatal("the default set must analyze .go files")
	}
	if set.For("a/b.py") != nil {
		t.Fatal("python must fall back to path-based relevance")
	}
}
