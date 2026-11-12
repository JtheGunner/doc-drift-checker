// Package ciconfig pins the structure of the CI integration files at the
// repository root (action.yml, .pre-commit-hooks.yaml). It has no
// non-test code and needs no network access.
package ciconfig

import (
	"os"
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

var fullShaOrVersionTag = regexp.MustCompile(`@([0-9a-f]{40}|v\d+\.\d+\.\d+)( |$)`)

func TestActionThirdPartyActionsArePinned(t *testing.T) {
	for _, step := range loadAction(t).Runs.Steps {
		if step.Uses != "" && !fullShaOrVersionTag.MatchString(step.Uses+" ") {
			t.Errorf("action %q is not pinned to a version tag or commit", step.Uses)
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
