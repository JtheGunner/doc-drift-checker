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

`cmd/doc-drift-checker` (thin entry point), `internal/cli` (cobra commands, testable via `NewRootCmd`), `internal/{config,docparse,gitx,links,detect,relevance,report,testutil}`. Create a package only when a ticket needs it. Keep each package single-purpose; `docparse` and reference extraction stay pure (no filesystem or git access).

## Testing

- Tests that involve git use `internal/testutil` to build real temporary repos with fixed identities and dates. Do not mock `git`.
- Table-driven tests with fixtures for parser and extraction edge cases.
- Primary quality goal is few false positives: every relevance or mapping change needs a test for the case that must not be reported.

## Work process

- Work is tracked as YouTrack tickets (project `DDC`, `/youtrack-task`), one per part of the concept. Implement a ticket's acceptance criteria and nothing beyond.
- The concept lives in `.concept/concept.md` in the **main checkout** (it does not exist in worktrees). It is intentionally untracked (listed in `.git/info/exclude` and `.gitignore`): never commit it, never move it into `docs/` and never turn it into a `docs/superpowers/` spec or plan.
- Keep this file in sync: when a ticket introduces a project-specific convention (commands, config keys, extension points), add it here as a delta.
