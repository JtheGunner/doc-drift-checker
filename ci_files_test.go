// Package ciconfig pins the structure of the CI integration files at the
// repository root (action.yml, .pre-commit-hooks.yaml) and of the release
// automation (.goreleaser.yaml, release workflow). It has no
// non-test code and needs no network access.
package ciconfig

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type actionInput struct {
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Default     string `yaml:"default"`
}

type actionStep struct {
	Name  string            `yaml:"name"`
	Uses  string            `yaml:"uses"`
	Run   string            `yaml:"run"`
	Shell string            `yaml:"shell"`
	Env   map[string]string `yaml:"env"`
	With  map[string]string `yaml:"with"`
}

type action struct {
	Description string                 `yaml:"description"`
	Author      string                 `yaml:"author"`
	Name        string                 `yaml:"name"`
	Inputs      map[string]actionInput `yaml:"inputs"`
	Runs        struct {
		Using string       `yaml:"using"`
		Steps []actionStep `yaml:"steps"`
	} `yaml:"runs"`
}

func loadAction(t *testing.T) action {
	t.Helper()
	data, err := os.ReadFile("action.yml")
	if err != nil {
		t.Fatal(err)
	}
	var a action
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&a); err != nil {
		t.Fatalf("action.yml: %v", err)
	}
	return a
}

func TestActionIsComposite(t *testing.T) {
	a := loadAction(t)
	if a.Name == "" || a.Runs.Using != "composite" || len(a.Runs.Steps) == 0 {
		t.Fatalf("not a composite action: %+v", a)
	}
}

func TestActionInputsAndDefaults(t *testing.T) {
	a := loadAction(t)
	want := map[string]string{
		"base":              "${{ github.base_ref || github.event.repository.default_branch }}",
		"format":            "github",
		"min-severity":      "",
		"version":           "latest",
		"working-directory": ".",
	}
	if len(a.Inputs) != len(want) {
		t.Errorf("inputs = %v, want exactly %d", a.Inputs, len(want))
	}
	for name, def := range want {
		in, ok := a.Inputs[name]
		if !ok {
			t.Errorf("missing input %q", name)
			continue
		}
		if in.Default != def {
			t.Errorf("input %q default = %q, want %q", name, in.Default, def)
		}
		if in.Description == "" || in.Required {
			t.Errorf("input %q needs a description and must be optional", name)
		}
	}
}

func TestActionRunBlocksTakeNoExpressions(t *testing.T) {
	for i, step := range loadAction(t).Runs.Steps {
		if step.Run == "" {
			continue
		}
		if strings.Contains(step.Run, "${{") {
			t.Errorf("step %d: expression inside run block (script injection risk):\n%s", i, step.Run)
		}
		if step.Shell != "bash" {
			t.Errorf("step %d: composite run steps need an explicit shell, got %q", i, step.Shell)
		}
	}
}

func TestActionPassesInputsThroughEnv(t *testing.T) {
	a := loadAction(t)
	var runSteps int
	envValues := map[string]string{}
	for _, step := range a.Runs.Steps {
		if step.Run == "" {
			continue
		}
		runSteps++
		for k, v := range step.Env {
			envValues[k] = v
		}
	}
	if runSteps == 0 {
		t.Fatal("no run step")
	}
	for _, input := range []string{"base", "format", "min-severity", "version", "working-directory"} {
		found := false
		for _, v := range envValues {
			if strings.Contains(v, "inputs."+input) {
				found = true
			}
		}
		if !found {
			t.Errorf("input %q is never passed through env", input)
		}
	}
}

// pinnedUses matches a remote action reference pinned to a full commit SHA
// and followed by a version comment such as "# v4.4.0".
var pinnedUses = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40} # v\d+\.\d+\.\d+$`)

// usesLine finds a `uses:` key in a workflow or action file, with or without
// the leading list dash.
var usesLine = regexp.MustCompile(`^\s*(?:-\s+)?uses:\s*(.+?)\s*$`)

// TestRemoteActionsArePinnedToCommits makes sure no workflow or composite
// action runs a mutable tag: a moved tag would execute foreign code with the
// job's token. Local (./) references are exempt.
func TestRemoteActionsArePinnedToCommits(t *testing.T) {
	files := []string{"action.yml"}
	workflows, err := filepath.Glob(".github/workflows/*.y*ml")
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) == 0 {
		t.Fatal("no workflow files found")
	}
	files = append(files, workflows...)

	var seen int
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			m := usesLine.FindStringSubmatch(line)
			if m == nil || strings.HasPrefix(m[1], "./") {
				continue
			}
			seen++
			if !pinnedUses.MatchString(m[1]) {
				t.Errorf("%s:%d: %q is not pinned to a 40-character commit SHA with a version comment", path, i+1, m[1])
			}
		}
	}
	if seen == 0 {
		t.Fatal("no remote action references found; the scan is broken")
	}
}

func TestPinnedUsesPattern(t *testing.T) {
	sha := strings.Repeat("a", 40)
	cases := map[string]bool{
		"actions/checkout@" + sha + " # v4.4.0":                  true,
		"github/codeql-action/upload-sarif@" + sha + " # v3.1.0": true,
		"actions/checkout@v4":                                    false,
		"actions/checkout@v4.4.0":                                false,
		"actions/checkout@" + sha:                                false,
		"actions/checkout@" + sha[:39] + " # v4.4.0":             false,
		"actions/checkout@" + strings.ToUpper(sha) + " # v4.4.0": false,
		"actions/checkout@main # v4.4.0":                         false,
	}
	for in, want := range cases {
		if got := pinnedUses.MatchString(in); got != want {
			t.Errorf("pinnedUses(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestActionChecksAgainstBaseAndFailsOnDrift(t *testing.T) {
	var script string
	for _, step := range loadAction(t).Runs.Steps {
		script += step.Run + "\n"
	}
	for _, want := range []string{"go install", "(check --base", `"origin/${`, "--format", "--min-severity", "fetch"} {
		if !strings.Contains(script, want) {
			t.Errorf("scripts never contain %q", want)
		}
	}
	if strings.Contains(script, "|| true") || strings.Contains(script, "--warn-only") {
		t.Error("the action must fail the job on drift")
	}
}

type hookDefinition struct {
	ID            string   `yaml:"id"`
	Name          string   `yaml:"name"`
	Description   string   `yaml:"description"`
	Entry         string   `yaml:"entry"`
	Language      string   `yaml:"language"`
	PassFilenames bool     `yaml:"pass_filenames"`
	AlwaysRun     bool     `yaml:"always_run"`
	Stages        []string `yaml:"stages"`
}

func TestPreCommitHookDefinition(t *testing.T) {
	data, err := os.ReadFile(".pre-commit-hooks.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var hooks []hookDefinition
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&hooks); err != nil {
		t.Fatalf(".pre-commit-hooks.yaml: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("hooks = %d, want 1", len(hooks))
	}
	h := hooks[0]
	if h.ID != "doc-drift-checker" || h.Entry != "doc-drift-checker check" || h.Language != "golang" {
		t.Errorf("unexpected hook: %+v", h)
	}
	if h.PassFilenames || !h.AlwaysRun {
		t.Errorf("pass_filenames must be false and always_run true: %+v", h)
	}
	if len(h.Stages) != 1 || h.Stages[0] != "pre-push" {
		t.Errorf("stages = %v, want [pre-push]", h.Stages)
	}
	if h.Name == "" || h.Description == "" || len(h.Description) > 120 {
		t.Errorf("name and a short description are required: %+v", h)
	}
}

type releaseWorkflow struct {
	Name string `yaml:"name"`
	On   struct {
		Push struct {
			Tags     []string `yaml:"tags"`
			Branches []string `yaml:"branches"`
		} `yaml:"push"`
	} `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]struct {
		RunsOn      string            `yaml:"runs-on"`
		Permissions map[string]string `yaml:"permissions"`
		Steps       []actionStep      `yaml:"steps"`
	} `yaml:"jobs"`
}

func readStrict(t *testing.T, path string, out any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestReleaseWorkflow(t *testing.T) {
	var w releaseWorkflow
	readStrict(t, ".github/workflows/release.yml", &w)

	if len(w.On.Push.Tags) != 1 || w.On.Push.Tags[0] != "v*" || len(w.On.Push.Branches) != 0 {
		t.Errorf("trigger must be exactly push of v* tags: %+v", w.On.Push)
	}
	if len(w.Permissions) != 1 || w.Permissions["contents"] != "read" {
		t.Errorf("top-level permissions = %v, want contents: read only", w.Permissions)
	}
	if len(w.Jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(w.Jobs))
	}
	for name, job := range w.Jobs {
		if len(job.Permissions) != 1 || job.Permissions["contents"] != "write" {
			t.Errorf("job %s permissions = %v, want contents: write only", name, job.Permissions)
		}
		var sawFullHistory, sawGoreleaser bool
		for _, step := range job.Steps {
			if strings.Contains(step.Run, "${{") {
				t.Errorf("expression inside run block:\n%s", step.Run)
			}
			if strings.HasPrefix(step.Uses, "actions/checkout@") && step.With["fetch-depth"] == "0" {
				sawFullHistory = true
			}
			if strings.HasPrefix(step.Uses, "goreleaser/goreleaser-action@") {
				sawGoreleaser = true
				if step.With["args"] != "release --clean" {
					t.Errorf("goreleaser args = %q", step.With["args"])
				}
				if len(step.Env) != 1 || step.Env["GITHUB_TOKEN"] != "${{ secrets.GITHUB_TOKEN }}" {
					t.Errorf("goreleaser env = %v, want only GITHUB_TOKEN from secrets.GITHUB_TOKEN", step.Env)
				}
			}
		}
		if !sawFullHistory || !sawGoreleaser {
			t.Errorf("job %s needs a full-history checkout and a GoReleaser step", name)
		}
	}
}

type goreleaserConfig struct {
	Version     int    `yaml:"version"`
	ProjectName string `yaml:"project_name"`
	Builds      []struct {
		ID      string   `yaml:"id"`
		Main    string   `yaml:"main"`
		Binary  string   `yaml:"binary"`
		Env     []string `yaml:"env"`
		Goos    []string `yaml:"goos"`
		Goarch  []string `yaml:"goarch"`
		Flags   []string `yaml:"flags"`
		Ldflags []string `yaml:"ldflags"`
	} `yaml:"builds"`
	Archives []struct {
		ID           string   `yaml:"id"`
		Formats      []string `yaml:"formats"`
		NameTemplate string   `yaml:"name_template"`
		Files        []string `yaml:"files"`
	} `yaml:"archives"`
	Checksum  map[string]string `yaml:"checksum"`
	Changelog map[string]any    `yaml:"changelog"`
	Release   map[string]string `yaml:"release"`
}

func TestGoReleaserConfig(t *testing.T) {
	var c goreleaserConfig
	readStrict(t, ".goreleaser.yaml", &c)

	if c.Version != 2 {
		t.Errorf("version = %d, want 2", c.Version)
	}
	if len(c.Builds) != 1 {
		t.Fatalf("builds = %d, want 1", len(c.Builds))
	}
	b := c.Builds[0]
	if b.Main != "./cmd/doc-drift-checker" {
		t.Errorf("main = %q", b.Main)
	}
	if strings.Join(b.Env, ",") != "CGO_ENABLED=0" {
		t.Errorf("env = %v, want CGO_ENABLED=0", b.Env)
	}
	if strings.Join(b.Goos, ",") != "darwin,linux" || strings.Join(b.Goarch, ",") != "amd64,arm64" {
		t.Errorf("targets = %v x %v, want darwin,linux x amd64,arm64", b.Goos, b.Goarch)
	}
	if !strings.Contains(strings.Join(b.Ldflags, " "), "-X main.version={{.Version}}") {
		t.Errorf("ldflags %v do not inject main.version", b.Ldflags)
	}
	if len(c.Archives) != 1 || strings.Join(c.Archives[0].Formats, ",") != "tar.gz" {
		t.Fatalf("archives = %+v, want one tar.gz archive", c.Archives)
	}
	files := strings.Join(c.Archives[0].Files, ",")
	for _, want := range []string{"LICENSE", "README.md", "CHANGELOG.md"} {
		if !strings.Contains(files, want) {
			t.Errorf("archive does not include %s", want)
		}
	}
	if c.Checksum["name_template"] == "" {
		t.Error("checksums are not configured")
	}
}
