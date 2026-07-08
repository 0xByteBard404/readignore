# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-07-08

First public release. Claude Code (hard) + opencode (config) MVP.

### Added

- **`.readignore` parser** (`internal/readignore`): full gitignore-syntax parser
  supporting `*`, `**`, `?`, `[abc]` character classes, `!` negation
  (last-match-wins), trailing `/` directory anchoring, `#` comments, and
  basename vs. root-anchored modes. Semantics aligned with go-git.
- **Adapter abstraction** (`internal/adapter`): `Adapter` interface,
  `Strength` (hard/config/soft), `GeneratedFile` / `Plan` value objects, and a
  self-registering global registry (`Register` / `All` / `Get`).
- **Claude Code adapter** (`internal/adapter/claudecode`, strength: **hard**):
  generates a `PreToolUse` hook (shell + Python engine + `settings.json`) that
  blocks `Read | Grep | Glob | Bash` calls whose target matches `.readignore`
  **before** the tool runs. Zero third-party deps (Python stdlib only). The only
  adapter with true runtime interception today.
- **opencode adapter** (`internal/adapter/opencode`, strength: **config**):
  generates `opencode.json` with `permission.read` deny/allow globs. Negation
  (`!`) is approximated via glob specificity. Honestly labeled `config` strength
  since opencode's `permission.ask` hook is a runtime no-op
  ([opencode #7006](https://github.com/anomalyco/opencode/issues/7006)).
- **CLI** (`internal/cli`, cobra): five subcommands —
  `init`, `adapters`, `generate <id>`, `install <id|--all>`, `check`. Idempotent
  installs (skip existing files unless `--force`), friendly errors, version flag
  (`-v`/`--version`) with `ldflags` injection.
- **CI** (`.github/workflows/ci.yml`): build & test matrix
  (ubuntu/macos/windows × Go 1.25) + golangci-lint job.
- **goreleaser config** (`.goreleaser.yml`): cross-platform binaries
  (linux/darwin/windows × amd64/arm64), SHA256 `checksums.txt`, Homebrew tap,
  Conventional-Commits-grouped changelog, draft GitHub Releases.
- **golangci-lint config** (`.golangci.yml`): govet, staticcheck, errcheck,
  gosec, gofmt, gofumpt, ineffassign, unused.
- Project scaffolding: `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`,
  MIT `LICENSE`, `Makefile` targets, issue templates.

[Unreleased]: https://github.com/0xByteBard404/readignore/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/0xByteBard404/readignore/releases/tag/v0.1.0
