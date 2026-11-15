# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/). The release date is filled in when the version tag is created.

## [Unreleased]

## [0.2.0] - 2026-11-15

### Changed

- `check` and `explain` apply `mode.default` when `--base` is not given: `history` runs history mode, `diff` is a usage error asking for `--base`. An unset key behaves as before (history mode). `--base` always selects diff mode.
- `init` lists sections that link more than 10 files as skipped, with the reason and a count in the summary line, instead of omitting them silently. Nothing is written for them.
- `ack` writes `doc-drift.lock` through the same atomic write path as `init`. A new lockfile still gets mode `0644`; an existing one keeps its permission bits.

### Fixed

- The text output of `check` and `links` escapes control characters in repository-provided values (paths, headings, references) and caps their length, as `explain`, SARIF and GitHub output already did.

## [0.1.0] - 2026-11-15

First release.

### Added

- `links` command: shows which code files each documentation section describes, with the origin of every link (annotation, config, implicit), unresolved references and unmapped sections; `--json` for machine-readable output.
- Section-to-code links from `<!-- drift: glob, glob -->` annotations, `mappings` in `.docdrift.yaml`, and implicit file-path and relative-link references.
- `check` command in two modes: diff mode (`--base <ref>`, compares `base...HEAD`) and history mode (no `--base`, compares commit dates, `--ignore-revs-file` for formatting commits).
- Output formats `text`, `json`, `sarif` (SARIF 2.1.0) and `github` (workflow annotations); exit codes `0` no drift, `1` drift, `2` error; `--warn-only`.
- Severity `low` and `high` with broken-reference detection for flags, environment variables and identifiers; `--min-severity` and `severity.fail-on`.
- Relevance filters: ignore globs with sensible defaults, whitespace-only changes, and exported-API analysis for Go sources.
- `ack` command and the `doc-drift.lock` lockfile to confirm that a section is still correct.
- `explain` command that shows why a section is, or is not, reported.
- `init` command that writes a starter `.docdrift.yaml` and suggests (`--write`: inserts) annotations.
- Configuration file `.docdrift.yaml` with `docs`, `ignore`, `mappings`, `mode.default` (reserved) and `severity.fail-on`.
- GitHub Action (`action.yml`) and a pre-commit hook (`.pre-commit-hooks.yaml`); CI guide in `docs/ci-integration.md`.
- Release binaries for macOS and Linux (amd64 and arm64) with checksums, built by GoReleaser when a `v*` tag is pushed.
- MIT license.
