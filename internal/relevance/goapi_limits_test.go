package relevance

import (
	"strings"
	"testing"
	"time"
)

// nestDepth is deep enough that expanding grouped names (two per level) would
// need 2^nestDepth nodes; the analyzer must refuse instead of expanding.
var nestDepth = 40

func nestedGroupedParams(depth int) string {
	typ := "int"
	for i := 0; i < depth; i++ {
		typ = "func(a, b " + typ + ")"
	}
	return "package p\nfunc F(a, b " + typ + ") {}\n"
}

func nestedGroupedFields(depth int) string {
	typ := "int"
	for i := 0; i < depth; i++ {
		typ = "struct{ A, B " + typ + " }"
	}
	return "package p\ntype T " + typ + "\n"
}

func TestGoAnalyzerFailsClosedOnAmplifyingSources(t *testing.T) {
	tests := []struct{ name, src string }{
		{"nested grouped params", nestedGroupedParams(nestDepth)},
		{"nested grouped struct fields", nestedGroupedFields(nestDepth)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			type result struct {
				changed bool
				err     error
			}
			done := make(chan result, 1)
			go func() {
				changed, err := goAnalyzer{}.APIChanged([]byte(tt.src), []byte(tt.src))
				done <- result{changed, err}
			}()
			select {
			case res := <-done:
				if res.err != nil || !res.changed {
					t.Fatalf("APIChanged = %v, %v; want true, nil (fail closed)", res.changed, res.err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("analysis did not finish in time")
			}
		})
	}
}

func TestGoAnalyzerModerateNestingStaysPrecise(t *testing.T) {
	params := nestedGroupedParams(5)
	fields := nestedGroupedFields(5)
	tests := []struct {
		name, old, new string
		want           bool
	}{
		{"params identical", params, params, false},
		{"params renamed", params, strings.ReplaceAll(params, "a, b", "x, y"), false},
		{"outer param removed", params, strings.Replace(params, "F(a, b ", "F(a ", 1), true},
		{"inner param removed", params, strings.Replace(params, "func(a, b int)", "func(a int)", 1), true},
		{"fields identical", fields, fields, false},
		{"inner field removed", fields, strings.Replace(fields, "A, B int", "A int", 1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := goAnalyzer{}.APIChanged([]byte(tt.old), []byte(tt.new))
			if err != nil || got != tt.want {
				t.Fatalf("APIChanged = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestGoAnalyzerFailsClosedOnLargeConstGroups(t *testing.T) {
	// Every implicit const repeats the previous (large) expression.
	expr := strings.Repeat("1 + ", 1500) + "1"
	src := "package p\nconst (\n\tA = " + expr + "\n" + strings.Repeat("\tB\n", 3000) + ")\n"
	got, err := goAnalyzer{}.APIChanged([]byte(src), []byte(src))
	if err != nil || !got {
		t.Fatalf("APIChanged = %v, %v; want true, nil (fail closed)", got, err)
	}
}
