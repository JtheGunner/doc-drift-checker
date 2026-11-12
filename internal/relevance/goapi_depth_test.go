package relevance

import (
	"strings"
	"testing"
	"time"
)

// deepLevels exceeds maxASTDepth while the node count stays far below the node
// budget, so these tests exercise the depth cap and not the size budget.
const deepLevels = 1500

// deepSources builds, per shape, a source whose exported symbol nests deepLevels levels.
func deepSources(levels int) map[string]string {
	rep := strings.Repeat
	return map[string]string{
		"left-nested binary chain in const": "package p\nconst X = 1" + rep("+1", levels) + "\n",
		"left-nested binary chain in var":   "package p\nvar V = 1" + rep("+1", levels) + "\n",
		"nested parentheses":                "package p\nconst X = " + rep("(", levels) + "1" + rep(")", levels) + "\n",
		"nested call chain":                 "package p\nvar V = " + rep("f(", levels) + "1" + rep(")", levels) + "\n",
		"selector chain":                    "package p\nvar V = a" + rep(".b", levels) + "\n",
		"unary chain":                       "package p\nconst X = " + rep("!", levels) + "true\n",
		"nested composite literals":         "package p\nvar V = " + rep("T{", levels) + rep("}", levels) + "\n",
		"nested func types":                 "package p\ntype F " + rep("func(", levels) + rep(")", levels) + "\n",
		"nested slice and pointer types":    "package p\ntype X " + rep("[]*", levels) + "int\n",
		"nested func signature":             "package p\nfunc F(a " + rep("func(", levels) + rep(")", levels) + ") {}\n",
	}
}

func TestGoAnalyzerFailsClosedOnDeepNesting(t *testing.T) {
	for name, src := range deepSources(deepLevels) {
		t.Run(name, func(t *testing.T) {
			done := make(chan bool, 1)
			go func() {
				changed, err := goAnalyzer{}.APIChanged([]byte(src), []byte(src))
				done <- err == nil && changed
			}()
			select {
			case ok := <-done:
				if !ok {
					t.Fatal("APIChanged must return true, nil above the depth cap")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("analysis did not finish in time")
			}
		})
	}
}

// Realistic nesting stays precise: the cap must not cost accuracy.
func TestGoAnalyzerRealisticNestingStaysPrecise(t *testing.T) {
	for name, src := range deepSources(100) {
		t.Run(name+" identical", func(t *testing.T) {
			if changed, err := (goAnalyzer{}).APIChanged([]byte(src), []byte(src)); err != nil || changed {
				t.Fatalf("APIChanged = %v, %v; want false, nil", changed, err)
			}
		})
	}
	chain := "package p\nconst X = 1" + strings.Repeat("+1", 100) + "\n"
	tests := []struct {
		name, new string
		want      bool
	}{
		{"reformatted", strings.ReplaceAll(chain, "+1", " + 1"), false},
		{"last term changed", strings.TrimSuffix(chain, "+1\n") + "+2\n", true},
		{"term added", strings.TrimSuffix(chain, "\n") + "+1\n", true},
	}
	for _, tt := range tests {
		t.Run("chain "+tt.name, func(t *testing.T) {
			got, err := goAnalyzer{}.APIChanged([]byte(chain), []byte(tt.new))
			if err != nil || got != tt.want {
				t.Fatalf("APIChanged = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

// A chain far beyond what a safe recursion could handle: it must fail closed
// without crashing the process (a Go stack overflow is fatal, not recoverable).
func TestGoAnalyzerSurvivesVeryDeepNesting(t *testing.T) {
	src := "package p\nconst X = 1" + strings.Repeat("+1", 1_000_000) + "\n"
	changed, err := goAnalyzer{}.APIChanged([]byte(src), []byte(src))
	if err != nil || !changed {
		t.Fatalf("APIChanged = %v, %v; want true, nil", changed, err)
	}
}

func TestGoAnalyzerFailsClosedOnOversizedSource(t *testing.T) {
	src := "package p\n// " + strings.Repeat("x", maxSourceBytes) + "\nfunc F() {}\n"
	changed, err := goAnalyzer{}.APIChanged([]byte(src), []byte(src))
	if err != nil || !changed {
		t.Fatalf("APIChanged = %v, %v; want true, nil", changed, err)
	}
}
