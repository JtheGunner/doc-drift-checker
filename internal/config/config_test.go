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
  default: staged
severity:
  default: error
  fail-on: warning
`,
			want: Config{
				Docs:   []string{"README.md", "docs/*.md"},
				Ignore: []string{"vendor/**"},
				Mappings: map[string][]string{
					"Installation": {"cmd/**", "go.mod"},
					"#usage":       {"internal/cli/*.go"},
				},
				Mode:     Mode{Default: ModeStaged},
				Severity: Severity{Default: LevelError, FailOn: LevelWarning},
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
			input:   "severity:\n  fail-on: fatal\n",
			wantErr: []string{".docdrift.yaml", "severity.fail-on", "fatal"},
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
	if d.Mode.Default != ModeBranch || d.Severity.Default != LevelWarning || d.Severity.FailOn != LevelError {
		t.Errorf("unexpected defaults: %+v", d)
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
