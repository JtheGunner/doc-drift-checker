package cli

import (
	"errors"
	"fmt"
	"sort"

	"github.com/JtheGunner/doc-drift-checker/internal/detect"
	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
	"github.com/JtheGunner/doc-drift-checker/internal/lock"
	"github.com/JtheGunner/doc-drift-checker/internal/relevance"
)

// sectionKey is the lockfile key of a section: "<doc path>#<anchor>".
func sectionKey(doc, anchor string) string {
	return doc + "#" + anchor
}

// mappedState is the state of a section's mapped code at HEAD: the hash of
// every hashable file, and the files that could not be hashed.
type mappedState struct {
	hashes     map[string]string
	unhashable []string
}

// readMappedState hashes the section's mapped files at HEAD. Files matching an
// ignore glob, files absent at HEAD (deleted, renamed away, staged only) and
// the lockfile itself are left out. A file above detect.MaxFileBytes or one
// that cannot be read as a blob is listed as unhashable.
func readMappedState(git *gitx.Client, filter relevance.Filter, section links.SectionLinks) (mappedState, error) {
	state := mappedState{hashes: map[string]string{}}
	var total int64
	seen := map[string]bool{}
	for _, l := range section.Links {
		file := l.File
		if seen[file] || file == lock.FileName || filter.Ignored(file) {
			continue
		}
		seen[file] = true
		size, err := git.FileSizeAt("HEAD", file)
		if errors.Is(err, gitx.ErrNotExist) {
			continue
		}
		if err != nil || size > detect.MaxFileBytes {
			state.unhashable = append(state.unhashable, file)
			continue
		}
		if len(seen) > maxMappedFiles {
			return mappedState{}, fmt.Errorf("section maps more than %d files", maxMappedFiles)
		}
		if total += size; total > maxMappedBytes {
			return mappedState{}, fmt.Errorf("mapped files of a section exceed %d bytes", maxMappedBytes)
		}
		content, err := git.FileAt("HEAD", file)
		if errors.Is(err, gitx.ErrNotExist) {
			continue
		}
		if err != nil {
			state.unhashable = append(state.unhashable, file)
			continue
		}
		state.hashes[file] = lock.Hash(content)
	}
	sort.Strings(state.unhashable)
	return state, nil
}

// lockVerdicts compares the current mapped code of every section that has a
// lockfile entry with its locked state. The value of a section is the sorted
// list of differing files (changed, added, removed or unhashable); an empty
// list means the locked state still matches. Files matching an ignore glob are
// excluded on both sides, so a change to an ignored file never reopens a
// section.
func lockVerdicts(git *gitx.Client, filter relevance.Filter, docs []links.DocLinks, locked lock.Lock) (map[detect.SectionKey][]string, error) {
	verdicts := map[detect.SectionKey][]string{}
	if len(locked.Sections) == 0 {
		return verdicts, nil
	}
	for _, doc := range docs {
		for _, section := range doc.Sections {
			entry, ok := locked.Sections[sectionKey(doc.Doc, section.Anchor)]
			if !ok || section.Anchor == "" {
				continue
			}
			state, err := readMappedState(git, filter, section)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", sectionKey(doc.Doc, section.Anchor), err)
			}
			verdicts[detect.SectionKey{Doc: doc.Doc, Line: section.StartLine}] = changedFiles(filter, entry, state)
		}
	}
	return verdicts, nil
}

func changedFiles(filter relevance.Filter, entry map[string]string, state mappedState) []string {
	lockedFiles := make(map[string]string, len(entry))
	for file, hash := range entry {
		if file != lock.FileName && !filter.Ignored(file) {
			lockedFiles[file] = hash
		}
	}
	changed := lock.Diff(lockedFiles, state.hashes)
	changed = append(changed, state.unhashable...)
	sort.Strings(changed)
	out := changed[:0]
	for i, file := range changed {
		if i == 0 || file != changed[i-1] {
			out = append(out, file)
		}
	}
	return out
}
