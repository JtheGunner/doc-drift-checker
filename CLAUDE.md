# doc-drift-checker

Go CLI that reports when code changed but the README passage describing it did not. Deltas from the global rules in `~/.claude/rules/` only.

## Stack decisions

- Go, single binary. Git access by shelling out to `git` (no libgit2 / go-git).
- `goldmark` for Markdown, `cobra` for the CLI, YAML config `.docdrift.yaml`.
- No cgo. Go source analysis uses the standard library (`go/parser`); other languages plug in via the `SymbolAnalyzer` interface in `internal/relevance`.

## Commands

- Build: `go build ./...`
- Test: `go test ./...` (single package while iterating: `go test ./internal/cli`)
- Lint: `golangci-lint run` (config in `.golangci.yml`, v2 format)
- Version is injected at build time: `-ldflags "-X main.version=<version>"`; default is `dev`.

## Layout

`cmd/doc-drift-checker` (thin entry point), `internal/cli` (cobra commands, testable via `NewRootCmd`), `internal/{config,docparse,fsx,gitx,glob,links,detect,relevance,report,testutil}`. Create a package only when a ticket needs it. Keep each package single-purpose; `docparse` and reference extraction stay pure (no filesystem or git access).

## Testing

- Tests that involve git use `internal/testutil` to build real temporary repos with fixed identities and dates. Do not mock `git`.
- Table-driven tests with fixtures for parser and extraction edge cases.
- Primary quality goal is few false positives: every relevance or mapping change needs a test for the case that must not be reported.

## Work process

- Work is tracked as YouTrack tickets (project `DDC`, `/youtrack-task`), one per part of the concept. Implement a ticket's acceptance criteria and nothing beyond.
- The concept lives in `.concept/concept.md` in the **main checkout** (it does not exist in worktrees). It is intentionally untracked (listed in `.git/info/exclude` and `.gitignore`): never commit it, never move it into `docs/` and never turn it into a `docs/superpowers/` spec or plan.
- Keep this file in sync: when a ticket introduces a project-specific convention (commands, config keys, extension points), add it here as a delta.

Untrusted repo content: read files only via `fsx.ReadRegularFile(root, rel)` (os.OpenRoot-based: no path component can escape the root; a symlink or non-regular file is rejected; check and read use one handle). Glob matching is memoized; keep it polynomial.

## Config

`.docdrift.yaml` in the repo root is loaded by `internal/config` (`Load`, `Parse`, `Defaults`). Keys: `docs`, `ignore`, `mappings`, `mode.default` (`diff` default, or `history`), `severity.fail-on` (`low` default, or `high`). Unknown keys and invalid globs are errors; defaults are documented in the package comment.

`ignore` defaults to `config.DefaultIgnore()` (tests, generated code, lockfiles, vendored code). A present `ignore` key replaces the defaults entirely; `ignore: []` disables path ignoring.

## Section-to-code links

`internal/links` maps README sections to tracked files (pure `Resolve`; the `links [--json]` command in `internal/cli` gathers the inputs). Annotate a section with `<!-- drift: glob, glob -->` (comma-separated, `**` supported, matched against tracked files only).

Origins: `annotation`, `config` (`mappings`), `implicit` (file-path and relative-link references). Explicit links replace implicit ones, and a section's own text excludes its child sections.

## Drift check (diff mode)

`check --base <ref> [--format text|json] [--warn-only]` compares `base...HEAD`. `internal/detect` is the pure core (a linked file changed and no line of the section's own range changed); `internal/cli/check.go` gathers git data. Files removed or renamed away are added to the file list used for link resolution, so sections mapped to them still trigger. Shared input gathering lives in `internal/cli/inputs.go`.

Exit codes: 0 no drift, 1 drift, 2 usage/runtime error (`cli.ExitCode`, handled in `cmd/doc-drift-checker/main.go`; tests assert the code, never `os.Exit`). `--warn-only` forces 0 on drift only. The JSON schema (`schemaVersion` 1) is documented in the `internal/report` package comment; update it there when the output changes.

## Drift check (history mode)

`check` without `--base` runs history mode (`--base` selects diff mode; config `mode.default` is not wired yet). `detect.History` is the pure core; `internal/cli/history.go` gathers dates. A section drifts when a mapped, non-ignored code file's newest commit (`gitx.CommitTimes`, follows renames) is strictly newer than the newest commit blamed for the section's own lines (`detect.OwnRange`, child sections excluded) at HEAD. Dates are committer times; equal dates are no drift; sections without blame data or mapped files are not reported. `--ignore-revs-file <path>` (history mode only, usage error with `--base`; git's format, parsed by `gitx.ParseIgnoreRevs`, read without `fsx` since it may live outside the repo) is passed to `git blame` and filters the file log. The JSON report has an additive `mode` field (`diff`/`history`); `base` is empty in history mode.

Documented limits (also in the `check` help and the `internal/detect` package comment): a squash merge stamps every line it touches with the merge time, so code and docs changed in one squash are equal (no drift) and the ordering inside the squashed branch is lost; rewritten history (rebase, amend, filter-branch) changes committer times and can change results; formatting-only commits distort dates (use `--ignore-revs-file`); deleted section lines are invisible to blame; only HEAD is read (no working tree) and deleted files cannot be dated; shallow clones are rejected (`gitx.EnsureFullHistory`).

## Relevance filters

`internal/relevance` (pure) drops changes before `detect.Detect`: a change is irrelevant when all its paths (new and, for renames/copies, old) match an `ignore` glob, or it is a modification (status M) whose diff is whitespace-only (`gitx.WhitespaceOnly`: `git diff -w --ignore-blank-lines`). Additions, deletions, renames and copies are never dropped as whitespace-only. Documentation edits are never filtered. `check` applies the filter in `internal/cli/check.go`; `links` output is unaffected. Glob matching lives in `internal/glob` (`Match`, `**` supported) and is shared with `internal/links`.

## Symbol relevance (languages)

On top of the path and whitespace filters, `relevance.AnalyzerSet` (built from `relevance.DefaultAnalyzers()`) drops changes whose public interface is unchanged. Pipeline per changed file in diff mode: path ignore, whitespace-only, symbol analyzer (status M, A or D, and only when an analyzer is registered for the file extension). Everything else, including every other language, falls back to path-based relevance (stays relevant). Old content is read with `gitx.FileAt(gitx.MergeBase(base), path)`, new content with `gitx.FileAt("HEAD", path)`; analyzers receive bytes only and stay pure.

Adding a language needs only an implementation of `relevance.SymbolAnalyzer` plus one line in `DefaultAnalyzers()` (`internal/relevance/symbols.go`):

```go
type pyAnalyzer struct{}

func (pyAnalyzer) Extensions() []string { return []string{".py"} } // lower-case, with the dot
func (pyAnalyzer) APIChanged(oldSrc, newSrc []byte) (bool, error) { /* nil src = file absent */ }

// in DefaultAnalyzers: return []SymbolAnalyzer{goAnalyzer{}, pyAnalyzer{}}
```

Be conservative: when in doubt, return true; an analyzer error is also treated as "changed". Test it table-driven, including the cases that must not be reported.

Go analyzer (`internal/relevance/goapi.go`, `go/parser` without comments) compares the canonical exported API of both sides. Counted: exported functions; exported methods on exported receiver types (receiver kind and type parameters count, parameter and receiver names do not); exported types (structs: exported fields with their tags and every embedded field, exported or not, because promoted members are public API; interfaces: exported methods and all embeds); exported consts (type, value, position in an `iota` group) and vars (type, or the value when no type is declared). Also counted: the package name and the build constraint lines (`//go:build` and legacy `// +build` before the package clause, normalized via `go/build/constraint`; read by a second, package-clause-only parse with comments, so no other comment is ever looked at). Not counted: bodies, comments and doc comments, imports, unexported declarations and fields, methods on unexported types, formatting, declaration order. Conservative rules: a side that fails to parse means changed; an added file is changed iff it has exported API, a deleted file iff it had (package name and build lines alone do not count there); renames and copies are never dropped. Size cap (fail closed): grouped names expand to one field each (`a, b int` is two parameters), which nested groups can blow up exponentially, so `expandedSize` bounds the expanded node count before any expansion, per symbol (`maxSymbolNodes`) and per file (`maxFileNodes`, includes const group repeats); above either limit the file counts as changed (`APIChanged` returns true, no error) without expanding. Keep any new analysis step behind that estimate.

Known limits of the Go analyzer (pinned by tests): there is no type-checking, so exported consts, vars and types that refer to unexported package-level identifiers are compared textually; a change of the referenced declaration (`const Max = max` with `max` changing, `type Foo bar` with `bar` changing, the body of an unexported embedded type) is not detected, only the embed line itself counts. Adding or removing an unexported spec inside a const group shifts the `iota` group index of later exported consts and is reported as changed (conservative false positive). An unexported method added to or removed from an exported interface breaks external implementers but is not compared (only the exported API is). Other directives (`//go:generate`, `//go:linkname`, `//go:embed`, cgo preambles) do not change the declared exported symbols or types and are ignored like all other comments; tests pin this.

Known limit: symbol relevance applies to diff mode only; history mode compares commit dates and is unchanged.
