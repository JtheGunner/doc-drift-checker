// Package config loads and validates the optional .docdrift.yaml file.
//
// Defaults (applied when the file is missing, empty, or omits a key):
//
//	docs:             ["README.md"]
//	ignore:           []
//	mappings:         {}
//	mode.default:     "branch"
//	severity.default: "warning"
//	severity.fail-on: "error"
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the name of the config file in the repository root.
const FileName = ".docdrift.yaml"

// ModeName selects which changes are compared against the documentation.
type ModeName string

// Supported modes.
const (
	ModeBranch   ModeName = "branch"   // commits on the current branch
	ModeStaged   ModeName = "staged"   // staged changes only
	ModeWorktree ModeName = "worktree" // uncommitted working tree changes
)

// Level is the severity of a finding.
type Level string

// Supported severity levels, from least to most severe.
const (
	LevelInfo    Level = "info"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
)

// Mode holds the mode settings.
type Mode struct {
	// Default is the mode used when no mode is requested explicitly.
	Default ModeName `yaml:"default"`
}

// Severity holds the severity settings.
type Severity struct {
	// Default is the level assigned to findings.
	Default Level `yaml:"default"`
	// FailOn is the lowest level that makes the run fail.
	FailOn Level `yaml:"fail-on"`
}

// Config is the validated configuration.
type Config struct {
	// Docs are globs of documentation files to check.
	Docs []string `yaml:"docs"`
	// Ignore are globs of paths excluded from checking.
	Ignore []string `yaml:"ignore"`
	// Mappings maps a heading or anchor (e.g. "Installation", "#usage")
	// to the code globs that the section describes.
	Mappings map[string][]string `yaml:"mappings"`
	Mode     Mode                `yaml:"mode"`
	Severity Severity            `yaml:"severity"`
}

// Defaults returns the configuration used when no file is present.
func Defaults() Config {
	return Config{
		Docs:     []string{"README.md"},
		Ignore:   nil,
		Mappings: nil,
		Mode:     Mode{Default: ModeBranch},
		Severity: Severity{Default: LevelWarning, FailOn: LevelError},
	}
}

// Load reads FileName from root. A missing file yields Defaults.
func Load(root string) (Config, error) {
	file := filepath.Join(root, FileName)
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", file, err)
	}
	return Parse(data, file)
}

// Parse decodes and validates config data. name is used in error messages.
func Parse(data []byte, name string) (Config, error) {
	cfg := Defaults()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("%s: %w", name, err)
	}
	if err := validate(cfg); err != nil {
		return Config{}, fmt.Errorf("%s: %w", name, err)
	}
	return cfg, nil
}

func validate(cfg Config) error {
	if err := validateGlobs("docs", cfg.Docs); err != nil {
		return err
	}
	if err := validateGlobs("ignore", cfg.Ignore); err != nil {
		return err
	}
	headings := make([]string, 0, len(cfg.Mappings))
	for heading := range cfg.Mappings {
		headings = append(headings, heading)
	}
	sort.Strings(headings)
	for _, heading := range headings {
		if strings.TrimSpace(heading) == "" {
			return errors.New("key mappings: heading or anchor must not be empty")
		}
		if err := validateGlobs("mappings."+heading, cfg.Mappings[heading]); err != nil {
			return err
		}
	}
	switch cfg.Mode.Default {
	case ModeBranch, ModeStaged, ModeWorktree:
	default:
		return fmt.Errorf("key mode.default: unknown mode %q (want branch, staged or worktree)", cfg.Mode.Default)
	}
	if err := validateLevel("severity.default", cfg.Severity.Default); err != nil {
		return err
	}
	return validateLevel("severity.fail-on", cfg.Severity.FailOn)
}

func validateLevel(key string, level Level) error {
	switch level {
	case LevelInfo, LevelWarning, LevelError:
		return nil
	}
	return fmt.Errorf("key %s: unknown level %q (want info, warning or error)", key, level)
}

func validateGlobs(key string, globs []string) error {
	for i, glob := range globs {
		if err := validateGlob(glob); err != nil {
			return fmt.Errorf("key %s[%d]: invalid glob %q: %w", key, i, glob, err)
		}
	}
	return nil
}

// validateGlob checks path.Match syntax; "**" is treated like "*" because
// it is only valid syntax-wise as a double star.
func validateGlob(glob string) error {
	if strings.TrimSpace(glob) == "" {
		return errors.New("empty pattern")
	}
	_, err := path.Match(strings.ReplaceAll(glob, "**", "*"), "")
	return err
}
