// Package lock reads, writes and compares the doc-drift.lock file, which records
// that a documentation section was acknowledged as still correct.
//
// The package is pure: bytes in, bytes out. It never touches the filesystem or
// git, so the format rules can be tested in isolation. The lockfile is untrusted
// repository content: Parse validates it strictly, and no path it contains is
// ever used to access the filesystem (paths are only compared).
//
// # Format
//
// JSON, 2-space indent, LF line endings, one trailing newline, map keys sorted,
// no timestamps, so equal content always yields equal bytes and diffs stay small:
//
//	{
//	  "version": 1,
//	  "sections": {
//	    "README.md#installation": {
//	      "files": {
//	        "src/main.go": "sha256:<64 lowercase hex digits>"
//	      }
//	    }
//	  }
//	}
//
// A section key is "<doc path>#<anchor>". File paths are repository-relative,
// slash-separated and clean. Parse rejects unknown fields, duplicate keys,
// trailing data, a wrong version, empty entries, unsafe paths (absolute,
// "..", backslashes, control characters) and malformed hashes.
package lock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
)

const (
	// FileName is the lockfile name in the repository root.
	FileName = "doc-drift.lock"
	// MaxBytes is the largest lockfile that is read.
	MaxBytes = 4 << 20

	version      = 1
	hashPrefix   = "sha256:"
	maxKeyBytes  = 1024
	maxPathBytes = 4096
)

var (
	hashPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	drivePattern = regexp.MustCompile(`^[A-Za-z]:`)
)

// Lock maps a section key to the hashes of its mapped code files at the time
// of the acknowledgement: key -> file path -> hash.
type Lock struct {
	Sections map[string]map[string]string
}

// Hash returns the lockfile hash of file content.
func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return hashPrefix + hex.EncodeToString(sum[:])
}

// With returns a copy of the lock in which the entry of key is replaced by
// files. Every other entry is unchanged. The receiver is not modified.
func (l Lock) With(key string, files map[string]string) Lock {
	sections := make(map[string]map[string]string, len(l.Sections)+1)
	for k, v := range l.Sections {
		sections[k] = v
	}
	copied := make(map[string]string, len(files))
	for p, h := range files {
		copied[p] = h
	}
	sections[key] = copied
	return Lock{Sections: sections}
}

// Diff returns the paths whose state differs between the locked and the
// current files (different hash, added, or removed), sorted and never nil.
func Diff(locked, current map[string]string) []string {
	changed := []string{}
	for p, h := range locked {
		if now, ok := current[p]; !ok || now != h {
			changed = append(changed, p)
		}
	}
	for p := range current {
		if _, ok := locked[p]; !ok {
			changed = append(changed, p)
		}
	}
	sort.Strings(changed)
	return changed
}

type document struct {
	Version  int                    `json:"version"`
	Sections map[string]sectionJSON `json:"sections"`
}

type sectionJSON struct {
	Files map[string]string `json:"files"`
}

// Marshal serializes the lock deterministically. It refuses a lock that Parse
// would reject, so a written lockfile can always be read back.
func (l Lock) Marshal() ([]byte, error) {
	doc := document{Version: version, Sections: make(map[string]sectionJSON, len(l.Sections))}
	for key, files := range l.Sections {
		if err := validateEntry(key, files); err != nil {
			return nil, err
		}
		doc.Sections[key] = sectionJSON{Files: files}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode lockfile: %w", err)
	}
	return buf.Bytes(), nil
}

// Parse validates and decodes a lockfile.
func Parse(data []byte) (Lock, error) {
	if len(data) > MaxBytes {
		return Lock{}, fmt.Errorf("lockfile is larger than %d bytes", MaxBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var sectionsRaw json.RawMessage
	haveVersion := false
	err := walkObject(dec, func(key string, raw json.RawMessage) error {
		switch key {
		case "version":
			if string(bytes.TrimSpace(raw)) != "1" {
				return fmt.Errorf("unsupported version %s (want %d)", bytes.TrimSpace(raw), version)
			}
			haveVersion = true
		case "sections":
			sectionsRaw = raw
		default:
			return fmt.Errorf("unknown field %q", key)
		}
		return nil
	})
	if err != nil {
		return Lock{}, fmt.Errorf("invalid lockfile: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Lock{}, errors.New("invalid lockfile: unexpected data after the top-level object")
	}
	if !haveVersion {
		return Lock{}, errors.New("invalid lockfile: missing version")
	}
	if sectionsRaw == nil {
		return Lock{}, errors.New("invalid lockfile: missing sections")
	}
	sections, err := parseSections(sectionsRaw)
	if err != nil {
		return Lock{}, fmt.Errorf("invalid lockfile: %w", err)
	}
	return Lock{Sections: sections}, nil
}

func parseSections(raw json.RawMessage) (map[string]map[string]string, error) {
	sections := map[string]map[string]string{}
	err := walkObject(json.NewDecoder(bytes.NewReader(raw)), func(key string, entryRaw json.RawMessage) error {
		files, err := parseEntry(entryRaw)
		if err != nil {
			return fmt.Errorf("section %q: %w", key, err)
		}
		if err := validateEntry(key, files); err != nil {
			return err
		}
		sections[key] = files
		return nil
	})
	return sections, err
}

func parseEntry(raw json.RawMessage) (map[string]string, error) {
	var filesRaw json.RawMessage
	err := walkObject(json.NewDecoder(bytes.NewReader(raw)), func(key string, value json.RawMessage) error {
		if key != "files" {
			return fmt.Errorf("unknown field %q", key)
		}
		filesRaw = value
		return nil
	})
	if err != nil {
		return nil, err
	}
	if filesRaw == nil {
		return nil, errors.New("missing files")
	}
	files := map[string]string{}
	err = walkObject(json.NewDecoder(bytes.NewReader(filesRaw)), func(p string, value json.RawMessage) error {
		var hash string
		if err := json.Unmarshal(value, &hash); err != nil {
			return fmt.Errorf("hash of %q is not a string", p)
		}
		files[p] = hash
		return nil
	})
	return files, err
}

// walkObject reads one JSON object from dec and calls fn for every member. A
// duplicate key is an error, which encoding/json would silently accept.
func walkObject(dec *json.Decoder, fn func(key string, value json.RawMessage) error) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("read JSON: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return errors.New("expected a JSON object")
	}
	seen := map[string]bool{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("read JSON: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return errors.New("expected a string key")
		}
		if seen[key] {
			return fmt.Errorf("duplicate key %q", key)
		}
		seen[key] = true
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return fmt.Errorf("read JSON: %w", err)
		}
		if err := fn(key, value); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("read JSON: %w", err)
	}
	return nil
}

func validateEntry(key string, files map[string]string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("section %q has no files", key)
	}
	for p, h := range files {
		if err := validatePath(p); err != nil {
			return fmt.Errorf("section %q: %w", key, err)
		}
		if !hashPattern.MatchString(h) {
			return fmt.Errorf("section %q: invalid hash for %q", key, p)
		}
	}
	return nil
}

// validateKey requires "<doc path>#<anchor>" with a safe doc path. The anchor
// is everything after the last "#".
func validateKey(key string) error {
	if key == "" || len(key) > maxKeyBytes || hasControl(key) {
		return fmt.Errorf("invalid section key %q", key)
	}
	i := strings.LastIndex(key, "#")
	if i <= 0 || i == len(key)-1 {
		return fmt.Errorf("invalid section key %q: want <doc path>#<anchor>", key)
	}
	if err := validatePath(key[:i]); err != nil {
		return fmt.Errorf("invalid section key %q: %w", key, err)
	}
	return nil
}

// validatePath accepts only clean, relative, slash-separated paths without
// parent segments, backslashes, drive letters or control characters.
func validatePath(p string) error {
	switch {
	case p == "" || len(p) > maxPathBytes:
		return fmt.Errorf("invalid path %q", p)
	case hasControl(p):
		return fmt.Errorf("path %q contains a control character", p)
	case strings.Contains(p, `\`):
		return fmt.Errorf("path %q contains a backslash", p)
	case strings.HasPrefix(p, "/") || drivePattern.MatchString(p):
		return fmt.Errorf("path %q is absolute", p)
	case p == "." || path.Clean(p) != p:
		return fmt.Errorf("path %q is not clean", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("path %q contains a parent segment", p)
		}
	}
	return nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
