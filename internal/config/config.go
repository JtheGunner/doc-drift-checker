// Package config loads and validates the optional .docdrift.yaml file.
//
// Defaults (applied when the file is missing, empty, or omits a key):
//
//	docs:             ["README.md"]
//	ignore:           []
//	mappings:         {}
//	mode.default:     "diff"
//	severity.fail-on: "low"
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
		Mode:     Mode{Default: ModeDiff},
		Severity: Severity{FailOn: SeverityLow},
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
