# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2026-07-09

Three hard adapters — Claude Code, **codex CLI**, and **pi** — plus one config
adapter (opencode). The "防 AI 读敏感文件" hard-block coverage expands from one
tool to three, all honestly labeled.

### Added

- **codex adapter** (`internal/adapter/codex`, strength: **hard**): generates a
  Claude-style `PreToolUse` hook (`.codex/hooks.json` + shared bash/python
  engine under `.codex/hooks/`). codex's hook protocol mirrors Claude Code's
  (`permissionDecision: "deny"` blocks the call before it runs), so the same
  match engine is reused. First project-level hook run is gated behind codex's
  hook trust prompt (`--dangerously-bypass-hook-trust` skips it).
- **pi adapter** (`internal/adapter/pi`, strength: **hard**): generates a single
  `.pi/extensions/readignore.ts` that **overrides** pi's built-in `read` tool —
  matched paths return `Access denied` before the file is read. pi auto-loads
  `.pi/extensions/*.ts` at startup, so no extra config is needed. The matcher is
  a hand-written gitignore engine with zero npm deps (no pi type imports, so the
  file type-checks in isolation).
- **shared hook engine** (`internal/adapter/shared/hookengine`): the bash+python
  match engine that v0.1 had inside the Claude Code adapter is extracted into a
  shared package. `claudecode` and `codex` now both consume
  `BuildShScriptAt` / `BuildPyEngine` — DRY refactor with **zero behavior
  change** (v0.1's 21-case claudecode integration suite stays green).
- **`.gitattributes`**: enforces LF line endings repo-wide, fixing CRLF drift
  on Windows checkouts that was breaking the generated bash hook scripts.

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

[Unreleased]: https://github.com/0xByteBard404/readignore/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/0xByteBard404/readignore/releases/tag/v0.2.0
[0.1.0]: https://github.com/0xByteBard404/readignore/releases/tag/v0.1.0
