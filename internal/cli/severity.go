package cli

import (
	"errors"
	"fmt"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
)

const (
	// maxMappedFiles bounds how many distinct mapped files are read for severity.
	// Above it the remaining files are unknown, so nothing is claimed high.
	maxMappedFiles = 2000
	// maxMappedBytes bounds the total content read for severity (both sides).
	maxMappedBytes = 64 << 20
)

// parseSeverity validates a --min-severity value.
func parseSeverity(value string) (config.SeverityLevel, error) {
	switch level := config.SeverityLevel(value); level {
	case config.SeverityLow, config.SeverityHigh:
		return level, nil
	}
	return "", fmt.Errorf("invalid --min-severity %q: use %s or %s", value, config.SeverityLow, config.SeverityHigh)
}

// failsAt reports whether at least one drift has a severity of at least threshold.
func failsAt(drifts []detect.Drift, threshold config.SeverityLevel) bool {
	for _, d := range drifts {
		if threshold == config.SeverityLow || d.Severity == config.SeverityHigh {
			return true
		}
	}
	return false
}

// classifyEvaluations sets the severity of the diff-mode drifts: it reads the
// mapped files of the drifted sections at the merge base and at HEAD and hands
// them to the pure detect.Classify (through Evaluations.WithSeverity).
func classifyEvaluations(git *gitx.Client, base string, docs []links.DocLinks, sections map[string][]docparse.Section, changes []gitx.Change, evals detect.Evaluations) (detect.Evaluations, error) {
	drifts := evals.Drifts()
	if len(drifts) == 0 {
		return evals, nil
	}
	mergeBase, err := git.MergeBase(base)
	if err != nil {
		return nil, err
	}
	reader := versionReader{git: git, mergeBase: mergeBase, changes: changes}
	versions := map[string]detect.FileVersion{}
	for _, file := range detect.LinkedFiles(docs, drifts) {
		version, exists, err := reader.read(file)
		if err != nil {
			return nil, err
		}
		if exists {
			versions[file] = version
		}
	}
	return evals.WithSeverity(docs, references(sections, drifts), versions), nil
}

// references extracts the references of the drifted sections.
func references(sections map[string][]docparse.Section, drifts []detect.Drift) map[detect.SectionKey][]docparse.Reference {
	refs := map[detect.SectionKey][]docparse.Reference{}
	for _, d := range drifts {
		for _, s := range sections[d.Doc] {
			if s.StartLine == d.Line {
				refs[detect.SectionKey{Doc: d.Doc, Line: d.Line}] = docparse.ExtractReferences(s)
				break
			}
		}
	}
	return refs
}

// versionReader reads a mapped file at the merge base and at HEAD, following
// renames, within fixed budgets.
type versionReader struct {
	git       *gitx.Client
	mergeBase string
	changes   []gitx.Change

	files int
	bytes int64
}

// read returns the versions of file; exists is false when it is on neither side.
// A file that cannot be read in full (too large, over budget, not a regular
// blob) is returned as unknown.
func (v *versionReader) read(file string) (version detect.FileVersion, exists bool, err error) {
	oldPath, newPath, changed := file, file, false
	for _, c := range v.changes {
		switch {
		case c.Status == gitx.StatusRenamed && c.Path == file:
			oldPath, changed = c.OldPath, true
		case c.Status == gitx.StatusRenamed && c.OldPath == file:
			newPath, changed = c.Path, true
		case c.Path == file:
			changed = true
		}
	}
	if v.files++; v.files > maxMappedFiles {
		return detect.FileVersion{Unknown: true}, true, nil
	}
	newSrc, newOK, err := v.readSide("HEAD", newPath)
	if err != nil {
		return detect.FileVersion{}, false, err
	}
	oldSrc, oldOK := newSrc, newOK
	if changed {
		if oldSrc, oldOK, err = v.readSide(v.mergeBase, oldPath); err != nil {
			return detect.FileVersion{}, false, err
		}
	}
	if !oldOK && !newOK {
		return detect.FileVersion{}, false, nil
	}
	if (oldOK && oldSrc == nil) || (newOK && newSrc == nil) {
		return detect.FileVersion{Unknown: true}, true, nil
	}
	return detect.FileVersion{Old: oldSrc, New: newSrc}, true, nil
}

// readSide reads one side. ok is false when the file does not exist there. A
// nil content with ok true means the content is unknown.
func (v *versionReader) readSide(rev, path string) (src []byte, ok bool, err error) {
	size, err := v.git.FileSizeAt(rev, path)
	if errors.Is(err, gitx.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, nil
	}
	if size > detect.MaxFileBytes || v.bytes+size > maxMappedBytes {
		return nil, true, nil
	}
	v.bytes += size
	src, err = v.git.FileAt(rev, path)
	if errors.Is(err, gitx.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, nil
	}
	if src == nil {
		src = []byte{}
	}
	return src, true, nil
}
