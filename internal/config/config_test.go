package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Config
		wantErr []string
	}{
		{
			name:  "empty file yields defaults",
			input: "",
			want:  Defaults(),
		},
		{
			name:  "comments only yields defaults",
			input: "# nothing here\n",
			want:  Defaults(),
		},
		{
			name:  "minimal config keeps other defaults",
			input: "docs:\n  - docs/**/*.md\n",
			want: func() Config {
				c := Defaults()
				c.Docs = []string{"docs/**/*.md"}
				return c
			}(),
		},
		{
			name: "full config",
			input: `docs: [README.md, "docs/*.md"]
ignore: ["vendor/**"]
mappings:
  "Installation": ["cmd/**", "go.mod"]
  "#usage": ["internal/cli/*.go"]
mode:
  default: history
severity:
  fail-on: high
`,
			want: Config{
				Docs:   []string{"README.md", "docs/*.md"},
				Ignore: []string{"vendor/**"},
				Mappings: map[string][]string{
					"Installation": {"cmd/**", "go.mod"},
					"#usage":       {"internal/cli/*.go"},
				},
				Mode:     Mode{Default: ModeHistory},
				Severity: Severity{FailOn: SeverityHigh},
			},
		},
		{
			name:    "unknown top-level key",
			input:   "docz: [a.md]\n",
			wantErr: []string{".docdrift.yaml", "docz"},
		},
		{
			name:    "unknown nested key",
			input:   "severity:\n  loud: error\n",
			wantErr: []string{".docdrift.yaml", "loud"},
		},
		{
			name:    "invalid docs glob",
			input:   "docs: [\"[\"]\n",
			wantErr: []string{".docdrift.yaml", "docs[0]", "invalid glob"},
		},
		{
			name:    "invalid ignore glob",
			input:   "ignore: [ok, \"a[\"]\n",
			wantErr: []string{".docdrift.yaml", "ignore[1]", "invalid glob"},
		},
		{
			name:    "invalid mapping glob",
			input:   "mappings:\n  Usage: [\"[\"]\n",
			wantErr: []string{".docdrift.yaml", "mappings.Usage[0]", "invalid glob"},
		},
		{
			name:    "empty mapping heading",
			input:   "mappings:\n  \"\": [a]\n",
			wantErr: []string{".docdrift.yaml", "mappings", "empty"},
		},
		{
			name:    "invalid mode",
			input:   "mode:\n  default: bogus\n",
			wantErr: []string{".docdrift.yaml", "mode.default", "bogus"},
		},
		{
			name:    "invalid severity",
			input:   "severity:\n  fail-on: critical\n",
			wantErr: []string{".docdrift.yaml", "severity.fail-on", "critical"},
		},
		{
			name:    "wrong type",
			input:   "docs: README.md\n",
			wantErr: []string{".docdrift.yaml"},
		},
		{
			name:    "malformed yaml",
			input:   "docs: [\n",
			wantErr: []string{".docdrift.yaml"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse([]byte(tt.input), ".docdrift.yaml")
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("expected error, got config %+v", got)
				}
				for _, part := range tt.wantErr {
					if !strings.Contains(err.Error(), part) {
						t.Errorf("error %q does not contain %q", err, part)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	d := Defaults()
	if !reflect.DeepEqual(d.Docs, []string{"README.md"}) {
		t.Errorf("Docs default = %v", d.Docs)
	}
	if d.Mode.Default != ModeDiff || d.Severity.FailOn != SeverityLow {
		t.Errorf("unexpected defaults: %+v", d)
	}
}

func TestDefaultIgnore(t *testing.T) {
	d := Defaults()
	if len(d.Ignore) == 0 {
		t.Fatal("Ignore default must not be empty")
	}
	for _, want := range []string{"**/*_test.go", "**/*.pb.go", "**/go.sum", "**/vendor/**"} {
		found := false
		for _, glob := range d.Ignore {
			found = found || glob == want
		}
		if !found {
			t.Errorf("default ignore list lacks %q", want)
		}
	}
	if err := validateGlobs("ignore", d.Ignore); err != nil {
		t.Errorf("default ignore globs must be valid: %v", err)
	}
	d.Ignore[0] = "mutated"
	if Defaults().Ignore[0] == "mutated" {
		t.Error("Defaults must return a fresh slice")
	}
}

func TestIgnoreOverride(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"absent key keeps defaults", "docs: [README.md]\n", DefaultIgnore()},
		{"explicit list replaces defaults", "ignore: [\"docs/**\"]\n", []string{"docs/**"}},
		{"explicit empty list disables ignoring", "ignore: []\n", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse([]byte(tt.input), ".docdrift.yaml")
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Ignore) != len(tt.want) || (len(tt.want) > 0 && !reflect.DeepEqual(got.Ignore, tt.want)) {
				t.Errorf("Ignore = %v, want %v", got.Ignore, tt.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	t.Run("missing file yields defaults", func(t *testing.T) {
		got, err := Load(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, Defaults()) {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("reads file from root", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte("ignore: [tmp/**]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Ignore, []string{"tmp/**"}) {
			t.Errorf("Ignore = %v", got.Ignore)
		}
	})
	t.Run("error names the file path", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, FileName)
		if err := os.WriteFile(path, []byte("nope: 1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("error %v should contain %q", err, path)
		}
	})
}

func TestLoadRejectsSymlink(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("ignore: [tmp/**]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inside := t.TempDir()
	if err := os.WriteFile(filepath.Join(inside, "real.yaml"), []byte("ignore: [tmp/**]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"outside": outside, "inside": filepath.Join(inside, "real.yaml")} {
		t.Run(name, func(t *testing.T) {
			dir := inside
			if name == "outside" {
				dir = t.TempDir()
			}
			link := filepath.Join(dir, FileName)
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unsupported: %v", err)
			}
			_, err := Load(dir)
			if err == nil || !strings.Contains(err.Error(), link) || !strings.Contains(err.Error(), "regular file") {
				t.Errorf("error %v should name %q and mention a regular file", err, link)
			}
		})
	}
}
