package docparse

import (
	"reflect"
	"testing"
)

// extractFirst extracts the references of the first section of src.
func extractFirst(t *testing.T, src string) []Reference {
	t.Helper()
	sections := ParseSections([]byte(src))
	if len(sections) == 0 {
		t.Fatalf("no sections in %q", src)
	}
	return ExtractReferences(sections[0])
}

func TestExtractReferences(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []Reference
	}{
		{
			name: "no references in plain prose",
			src:  "# T\nThis is plain text with nothing to see.\n",
			want: nil,
		},
		{
			name: "file path with directory and extension",
			src:  "# T\nSee `internal/config/config.go` for details.\n",
			want: []Reference{{Kind: KindFilePath, Value: "internal/config/config.go", Line: 2}},
		},
		{
			name: "bare file name with known extension",
			src:  "# T\nEdit `.docdrift.yaml` and `go.mod`.\n",
			want: []Reference{
				{Kind: KindFilePath, Value: ".docdrift.yaml", Line: 2},
				{Kind: KindFilePath, Value: "go.mod", Line: 2},
			},
		},
		{
			name: "directory path without extension",
			src:  "# T\nCode lives in `internal/docparse` and `cmd/tool/`.\n",
			want: []Reference{
				{Kind: KindFilePath, Value: "internal/docparse", Line: 2},
				{Kind: KindFilePath, Value: "cmd/tool/", Line: 2},
			},
		},
		{
			name: "path with line suffix drops the suffix",
			src:  "# T\nSee `main.go:42`.\n",
			want: []Reference{{Kind: KindFilePath, Value: "main.go", Line: 2}},
		},
		{
			name: "well-known file without extension",
			src:  "# T\nRun via `Makefile`.\n",
			want: []Reference{{Kind: KindFilePath, Value: "Makefile", Line: 2}},
		},
		{
			name: "path-like prose is not a reference",
			src:  "# T\nUse and/or input/output, TCP/IP at 24/7 or read/write; visit example.com.\n",
			want: nil,
		},
		{
			name: "path-like words in code spans that are not paths",
			src:  "# T\nChoose `and/or`, `24/7`, `yes/no` or `https://example.com/a/b.go` or `example.com`.\n",
			want: nil,
		},
		{
			name: "relative link",
			src:  "# T\nRead the [guide](docs/guide.md#install) and [more](./other.md).\n",
			want: []Reference{
				{Kind: KindRelativeLink, Value: "docs/guide.md", Line: 2},
				{Kind: KindRelativeLink, Value: "./other.md", Line: 2},
			},
		},
		{
			name: "external and in-page links are not relative links",
			src:  "# T\n[a](https://example.com) [b](mailto:x@y.z) [c](#top) [d](//cdn.example.com/x.js) [e]()\n",
			want: nil,
		},
		{
			name: "inline code identifiers",
			src:  "# T\nCall `ParseSections`, `config.Load()`, `parse_file` and `fooBar`.\n",
			want: []Reference{
				{Kind: KindIdentifier, Value: "ParseSections", Line: 2},
				{Kind: KindIdentifier, Value: "config.Load()", Line: 2},
				{Kind: KindIdentifier, Value: "parse_file", Line: 2},
				{Kind: KindIdentifier, Value: "fooBar", Line: 2},
			},
		},
		{
			name: "ambiguous inline code is not an identifier",
			src:  "# T\nSet `true`, `foo`, `JSON`, `v1.2`, `1.2.3`, `e.g.` and `a + b`.\n",
			want: nil,
		},
		{
			name: "cli flags in inline code",
			src:  "# T\nUse `--verbose` or `--out=dir`.\n",
			want: []Reference{
				{Kind: KindFlag, Value: "--verbose", Line: 2},
				{Kind: KindFlag, Value: "--out", Line: 2},
			},
		},
		{
			name: "flags in plain prose are not references",
			src:  "# T\nPass --force too; a -- b is a dash and word--word is not a flag.\n",
			want: nil,
		},
		{
			name: "framework names are not file paths",
			src:  "# T\nBuilt with `Node.js`, `Next.js`, `Vue.js`, `Chart.js`, `Three.js`, `Express.js` and `Socket.io`.\n",
			want: nil,
		},
		{
			name: "real bare file names stay file paths",
			src:  "# T\n`README.md` `CHANGELOG.md` `LICENSE` `Makefile` `Dockerfile` `go.mod` `main.go` `config.yaml` `server.js` `.golangci.yml`\n",
			want: []Reference{
				{Kind: KindFilePath, Value: "README.md", Line: 2},
				{Kind: KindFilePath, Value: "CHANGELOG.md", Line: 2},
				{Kind: KindFilePath, Value: "LICENSE", Line: 2},
				{Kind: KindFilePath, Value: "Makefile", Line: 2},
				{Kind: KindFilePath, Value: "Dockerfile", Line: 2},
				{Kind: KindFilePath, Value: "go.mod", Line: 2},
				{Kind: KindFilePath, Value: "main.go", Line: 2},
				{Kind: KindFilePath, Value: "config.yaml", Line: 2},
				{Kind: KindFilePath, Value: "server.js", Line: 2},
				{Kind: KindFilePath, Value: ".golangci.yml", Line: 2},
			},
		},
		{
			name: "capitalized file inside a directory stays a file path",
			src:  "# T\n`web/Button.tsx`\n",
			want: []Reference{{Kind: KindFilePath, Value: "web/Button.tsx", Line: 2}},
		},
		{
			name: "flag inside multi-word inline command",
			src:  "# T\nRun `go test --race ./...` now.\n",
			want: []Reference{{Kind: KindFlag, Value: "--race", Line: 2}},
		},
		{
			name: "environment variables",
			src:  "# T\nSet `DOCDRIFT_MODE`, `$HOME`, `${XDG_CONFIG_HOME}` and `TOKEN=abc`.\n",
			want: []Reference{
				{Kind: KindEnvVar, Value: "DOCDRIFT_MODE", Line: 2},
				{Kind: KindEnvVar, Value: "HOME", Line: 2},
				{Kind: KindEnvVar, Value: "XDG_CONFIG_HOME", Line: 2},
				{Kind: KindEnvVar, Value: "TOKEN", Line: 2},
			},
		},
		{
			name: "shell block commands",
			src:  "# T\n```sh\n# install\n\n$ go build ./...\ngo test \\\n  ./internal/...\n```\n",
			want: []Reference{
				{Kind: KindCommand, Value: "go build ./...", Line: 5},
				{Kind: KindCommand, Value: "go test ./internal/...", Line: 6},
			},
		},
		{
			name: "console block only treats prompt lines as commands",
			src:  "# T\n```console\n$ ls -la\ntotal 0\n```\n",
			want: []Reference{{Kind: KindCommand, Value: "ls -la", Line: 3}},
		},
		{
			name: "non-shell code blocks are not scanned",
			src:  "# T\n```go\nfmt.Println(`internal/x.go`)\n```\n\n```\nnpm install\n```\n",
			want: nil,
		},
		{
			name: "duplicates on the same line are reported once",
			src:  "# T\n`foo.go` and `foo.go`\n",
			want: []Reference{{Kind: KindFilePath, Value: "foo.go", Line: 2}},
		},
		{
			name: "same value on different lines is reported per line",
			src:  "# T\n`foo.go`\n\n`foo.go`\n",
			want: []Reference{
				{Kind: KindFilePath, Value: "foo.go", Line: 2},
				{Kind: KindFilePath, Value: "foo.go", Line: 4},
			},
		},
		{
			name: "references in headings and lists",
			src:  "# Using `cmd/tool`\n\n- item\n  - see `ParseSections`\n",
			want: []Reference{
				{Kind: KindFilePath, Value: "cmd/tool", Line: 1},
				{Kind: KindIdentifier, Value: "ParseSections", Line: 4},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFirst(t, tt.src)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractReferences() =\n  %+v\nwant\n  %+v", got, tt.want)
			}
		})
	}
}

func TestExtractReferencesLinesAreDocumentRelative(t *testing.T) {
	src := "# A\nintro\n\n## B\n\ntext\nsee `internal/x.go`\n\n```bash\nmake test\n```\n"
	sections := ParseSections([]byte(src))
	got := ExtractReferences(sections[1])
	want := []Reference{
		{Kind: KindFilePath, Value: "internal/x.go", Line: 7},
		{Kind: KindCommand, Value: "make test", Line: 10},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractReferences() = %+v, want %+v", got, want)
	}
}

func TestExtractReferencesPreambleLines(t *testing.T) {
	src := "\n\nSee `a/b.go`.\n\n# T\n"
	sections := ParseSections([]byte(src))
	got := ExtractReferences(sections[0])
	want := []Reference{{Kind: KindFilePath, Value: "a/b.go", Line: 3}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractReferences() = %+v, want %+v", got, want)
	}
}
