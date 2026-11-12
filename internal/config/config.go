// Package config loads and validates the optional .docdrift.yaml file.
//
// Defaults (applied when the file is missing, empty, or omits a key):
//
//	docs:             ["README.md"]
//	ignore:           see "Default ignore globs" below
//	mappings:         {}
//	mode.default:     "diff"
//	severity.fail-on: "low"
//
// # Default ignore globs
//
// Changes that only touch ignored paths never cause drift. The defaults cover
// four groups (see DefaultIgnore for the exact list):
//
//	tests:      **/*_test.go  **/*.test.*  **/*.spec.*  **/test/**  **/tests/**
//	            **/__tests__/**  **/testdata/**
//	generated:  **/*.pb.go  **/*_gen.go  **/*.generated.*  **/generated/**  **/*.min.js
//	lockfiles:  **/go.sum  **/package-lock.json  **/yarn.lock  **/pnpm-lock.yaml
//	            **/composer.lock  **/Cargo.lock  **/Gemfile.lock  **/poetry.lock
//	            **/uv.lock
//	vendored:   **/vendor/**  **/node_modules/**  **/third_party/**
//
// A "**/" prefix also matches at the repository root. When the ignore key is
// present in the file it replaces the defaults entirely: "ignore: []" disables
// path ignoring, and a custom list must repeat any default it still wants.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/JtheGunner/doc-drift-checker/internal/fsx"
)

// FileName is the name of the config file in the repository root.
const FileName = ".docdrift.yaml"

// ModeName selects how drift is detected.
type ModeName string

// Supported modes.
const (
	ModeDiff    ModeName = "diff"    // compares base...HEAD
	ModeHistory ModeName = "history" // uses git blame dates
)

// SeverityLevel is the severity of a drift finding.
type SeverityLevel string

// Supported severities, from least to most severe.
const (
	SeverityLow  SeverityLevel = "low"
	SeverityHigh SeverityLevel = "high"
)

// Mode holds the mode settings.
type Mode struct {
	// Default is the mode used when no mode is requested explicitly.
	Default ModeName `yaml:"default"`
}

// Severity holds the severity settings.
type Severity struct {
	// FailOn is the lowest drift severity that makes the check fail.
	FailOn SeverityLevel `yaml:"fail-on"`
}

// Config is the validated configuration.
type Config struct {
	// Docs are globs of documentation files to check.
	Docs []string `yaml:"docs"`
	// Ignore are globs of paths whose changes never cause drift. Defaults to
	// DefaultIgnore; a key present in the file replaces the defaults.
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
		Ignore:   DefaultIgnore(),
		Mappings: nil,
		Mode:     Mode{Default: ModeDiff},
		Severity: Severity{FailOn: SeverityLow},
	}
}

// DefaultIgnore returns a fresh copy of the built-in ignore globs: test files,
// generated code, lockfiles and vendored code. A configured ignore key replaces it.
func DefaultIgnore() []string {
	return []string{
		// tests
		"**/*_test.go", "**/*.test.*", "**/*.spec.*",
		"**/test/**", "**/tests/**", "**/__tests__/**", "**/testdata/**",
		// generated code
		"**/*.pb.go", "**/*_gen.go", "**/*.generated.*", "**/generated/**", "**/*.min.js",
		// lockfiles
		"**/go.sum", "**/package-lock.json", "**/yarn.lock", "**/pnpm-lock.yaml",
		"**/composer.lock", "**/Cargo.lock", "**/Gemfile.lock", "**/poetry.lock", "**/uv.lock",
		// vendored code
		"**/vendor/**", "**/node_modules/**", "**/third_party/**",
	}
}

// Load reads FileName from root. A missing file yields Defaults.
func Load(root string) (Config, error) {
	file := filepath.Join(root, FileName)
	data, err := fsx.ReadRegularFile(root, FileName)
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
	case ModeDiff, ModeHistory:
	default:
		return fmt.Errorf("key mode.default: unknown mode %q (want diff or history)", cfg.Mode.Default)
	}
	switch cfg.Severity.FailOn {
	case SeverityLow, SeverityHigh:
	default:
		return fmt.Errorf("key severity.fail-on: unknown severity %q (want low or high)", cfg.Severity.FailOn)
	}
	return nil
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
