package relevance

import "testing"

func TestGoAnalyzerFileHeader(t *testing.T) {
	const licence = "// Copyright Example.\n// Licensed under X.\n\n"
	tests := []struct {
		name     string
		old, new string
		want     bool
	}{
		// package clause and build constraints count
		{"package renamed", "package p\nfunc F() {}\n", "package q\nfunc F() {}\n", true},
		{"go:build changed", "//go:build linux\n\npackage p\n", "//go:build windows\n\npackage p\n", true},
		{"go:build added", "package p\n", "//go:build linux\n\npackage p\n", true},
		{"go:build removed", "//go:build linux\n\npackage p\n", "package p\n", true},
		{"legacy build changed", "// +build linux\n\npackage p\n", "// +build windows\n\npackage p\n", true},
		{"legacy build added", "package p\n", "// +build linux\n\npackage p\n", true},
		{"both forms, one changed", "//go:build linux\n// +build linux\n\npackage p\n", "//go:build linux\n// +build darwin\n\npackage p\n", true},
		{"build expression changed", "//go:build linux && amd64\n\npackage p\n", "//go:build linux || amd64\n\npackage p\n", true},

		// everything else around the header does not
		{"go:build reformatted", "//go:build linux&&amd64\n\npackage p\n", "//go:build linux && amd64\n\npackage p\n", false},
		{"go:build unchanged", "//go:build linux\n\npackage p\n", "//go:build linux\n\npackage p\n", false},
		{"licence header unchanged above build line", licence + "//go:build linux\n\npackage p\n", licence + "//go:build linux\n\npackage p\n", false},
		{"licence header text changed", licence + "//go:build linux\n\npackage p\n", "// Copyright Other.\n\n//go:build linux\n\npackage p\n", false},
		{"licence header added", "//go:build linux\n\npackage p\n", licence + "//go:build linux\n\npackage p\n", false},
		{"package doc comment changed", "// Package p does X.\npackage p\n", "// Package p does Y.\npackage p\n", false},
		{"block comment above package", "/* old */\npackage p\n", "/* new */\npackage p\n", false},
		{"go:build mention after package clause", "package p\n\n// see //go:build linux\nfunc F() {}\n", "package p\n\n// see //go:build windows\nfunc F() {}\n", false},
		{"go:build mention in function body", "package p\n\nfunc F() {\n\t//go:build linux\n}\n", "package p\n\nfunc F() {\n\t//go:build windows\n}\n", false},
		{"go:build line added after package clause", "package p\n\nfunc F() {}\n", "package p\n\n//go:build linux\n\nfunc F() {}\n", false},
		{"build comment on a declaration", "package p\n\n// +build linux\nfunc F() {}\n", "package p\n\n// +build darwin\nfunc F() {}\n", false},

		// added and deleted files: header facts alone never make them relevant
		{"added file with build line only", "", "//go:build linux\n\npackage p\n", false},
		{"added file with build line and API", "", "//go:build linux\n\npackage p\nfunc F() {}\n", true},
		{"deleted file with build line only", "//go:build linux\n\npackage p\n", "", false},

		// Pinned limits of the exported-API design.
		// An unexported method in an exported interface breaks external
		// implementers but is not part of the exported API we compare.
		{"unexported interface method added", "package p\ntype I interface{ A() }\n", "package p\ntype I interface {\n\tA()\n\tb()\n}\n", false},
		{"unexported interface method removed", "package p\ntype I interface {\n\tA()\n\tb()\n}\n", "package p\ntype I interface{ A() }\n", false},
		// Other directives do not alter the declared exported symbols or types.
		{"go:generate added", "package p\nfunc F() {}\n", "package p\n\n//go:generate stringer -type=X\nfunc F() {}\n", false},
		{"go:linkname added", "package p\nfunc F() {}\n", "package p\n\n//go:linkname F runtime.f\nfunc F() {}\n", false},
		{"go:embed added", "package p\nvar V string\n", "package p\n\n//go:embed v.txt\nvar V string\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := goAnalyzer{}.APIChanged([]byte(tt.old), []byte(tt.new))
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("APIChanged = %v, want %v", got, tt.want)
			}
		})
	}
}
