package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"github.com/JtheGunner/doc-drift-checker/internal/fsx"
	"github.com/JtheGunner/doc-drift-checker/internal/gitx"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
)

// repoInputs are the inputs shared by commands that resolve documentation links:
// the repository root, its config and the tracked files.
type repoInputs struct {
	git     *gitx.Client
	root    string
	cfg     config.Config
	tracked []string
}

// loadRepoInputs gathers the repository root, config and tracked files of the
// repository containing the working directory.
func loadRepoInputs() (repoInputs, error) {
	wd, err := os.Getwd()
	if err != nil {
		return repoInputs{}, fmt.Errorf("get working directory: %w", err)
	}
	root, err := gitx.New(wd).Root()
	if err != nil {
		return repoInputs{}, fmt.Errorf("find repository root: %w", err)
	}
	// Every later git call runs from the root: paths are root-relative, and a
	// pathspec resolves against the working directory.
	git := gitx.New(root)
	cfg, err := config.Load(root)
	if err != nil {
		return repoInputs{}, err
	}
	tracked, err := git.TrackedFiles()
	if err != nil {
		return repoInputs{}, err
	}
	return repoInputs{git: git, root: root, cfg: cfg, tracked: tracked}, nil
}

// resolveDocs reads every configured documentation file and resolves its
// sections against the given file list.
func (in repoInputs) resolveDocs(files []string) ([]links.DocLinks, error) {
	docs, _, err := in.resolveDocsWithSections(files)
	return docs, err
}

// resolveDocsWithSections is resolveDocs that also returns the parsed sections
// per document, for callers that need the section text.
func (in repoInputs) resolveDocsWithSections(files []string) ([]links.DocLinks, map[string][]docparse.Section, error) {
	docs := []links.DocLinks{}
	parsed := map[string][]docparse.Section{}
	for _, doc := range links.MatchDocs(in.cfg.Docs, in.tracked) {
		src, err := fsx.ReadRegularFile(in.root, filepath.FromSlash(doc))
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", doc, err)
		}
		sections := docparse.ParseSections(src)
		parsed[doc] = sections
		docs = append(docs, links.Resolve(doc, sections, in.cfg, files))
	}
	return docs, parsed, nil
}
