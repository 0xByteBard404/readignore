# Changelog

All notable changes to this project will be documented in this file.
本项目所有重要变更均记录于此文件。

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
格式基于 [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)，并遵循
[语义化版本](https://semver.org/spec/v2.0.0.html)。

## [Unreleased]

## [0.2.0] - 2026-07-09

Three hard adapters — Claude Code, **codex CLI**, and **pi** — plus one config
adapter (opencode). The hard-block coverage against AI agents reading sensitive
files expands from one tool to three, all honestly labeled.

三个 hard 适配器——Claude Code、**codex CLI**、**pi**——外加一个 config 适配器
（opencode）。“防 AI 读敏感文件”的硬拦覆盖面从一个工具扩展到三个，并诚实标注每一项。

### Added / 新增

- **codex adapter** (`internal/adapter/codex`, strength: **hard**): generates a
  Claude-style `PreToolUse` hook (`.codex/hooks.json` + shared bash/python
  engine under `.codex/hooks/`). codex's hook protocol mirrors Claude Code's
  (`permissionDecision: "deny"` blocks the call before it runs), so the same
  match engine is reused. First project-level hook run is gated behind codex's
  hook trust prompt (`--dangerously-bypass-hook-trust` skips it).
  — **codex 适配器**（`internal/adapter/codex`，强度 **hard**）：生成 Claude-style 的
    `PreToolUse` 钩子（`.codex/hooks.json` + `.codex/hooks/` 下的共享 bash/python
    引擎）。codex 的钩子协议与 Claude Code 一致（`permissionDecision: "deny"` 在执行前
    阻断），因此复用同一套匹配引擎。项目级钩子首次运行受 codex 钩子信任提示管控
    （`--dangerously-bypass-hook-trust` 可跳过）。
- **pi adapter** (`internal/adapter/pi`, strength: **hard**): generates a single
  `.pi/extensions/readignore.ts` that **overrides** pi's built-in `read` tool —
  matched paths return `Access denied` before the file is read. pi auto-loads
  `.pi/extensions/*.ts` at startup, so no extra config is needed. The matcher is
  a hand-written gitignore engine with zero npm deps (no pi type imports, so the
  file type-checks in isolation).
  — **pi 适配器**（`internal/adapter/pi`，强度 **hard**）：生成单个
    `.pi/extensions/readignore.ts`，**覆写** pi 内置 `read` 工具——匹配路径在文件被读取前
    返回 `Access denied`。pi 启动时自动加载 `.pi/extensions/*.ts`，无需额外配置。匹配器为
    手写 gitignore 引擎，零 npm 依赖（不引入 pi 类型，文件可独立通过类型检查）。
- **shared hook engine** (`internal/adapter/shared/hookengine`): the bash+python
  match engine that v0.1 had inside the Claude Code adapter is extracted into a
  shared package. `claudecode` and `codex` now both consume
  `BuildShScriptAt` / `BuildPyEngine` — DRY refactor with **zero behavior
  change** (v0.1's 21-case claudecode integration suite stays green).
  — **共享钩子引擎**（`internal/adapter/shared/hookengine`）：v0.1 内嵌于 Claude Code
    适配器的 bash+python 匹配引擎被抽到共享包。`claudecode` 与 `codex` 现在都消费
    `BuildShScriptAt` / `BuildPyEngine`——DRY 重构，**行为零变化**（v0.1 的 21 例
    claudecode 集成测试全绿）。
- **`.gitattributes`**: enforces LF line endings repo-wide, fixing CRLF drift
  on Windows checkouts that was breaking the generated bash hook scripts.
  — **`.gitattributes`**：全仓库强制 LF 行尾，修复 Windows 检出时的 CRLF 漂移问题
    （该问题曾破坏生成的 bash 钩子脚本）。

## [0.1.0] - 2026-07-08

First public release. Claude Code (hard) + opencode (config) MVP.
首次公开发布。Claude Code（hard）+ opencode（config）MVP。

### Added / 新增

- **`.readignore` parser** (`internal/readignore`): full gitignore-syntax parser
  supporting `*`, `**`, `?`, `[abc]` character classes, `!` negation
  (last-match-wins), trailing `/` directory anchoring, `#` comments, and
  basename vs. root-anchored modes. Semantics aligned with go-git.
  — **`.readignore` 解析器**（`internal/readignore`）：完整 gitignore 语法解析器，支持
    `*`、`**`、`?`、`[abc]` 字符类、`!` 取反（最后匹配优先）、末尾 `/` 目录锚定、`#`
    注释，以及 basename 与 root-anchored 两种模式。语义与 go-git 对齐。
- **Adapter abstraction** (`internal/adapter`): `Adapter` interface,
  `Strength` (hard/config/soft), `GeneratedFile` / `Plan` value objects, and a
  self-registering global registry (`Register` / `All` / `Get`).
  — **适配器抽象**（`internal/adapter`）：`Adapter` 接口、`Strength`
    （hard/config/soft）、`GeneratedFile` / `Plan` 值对象，以及自注册的全局注册表
    （`Register` / `All` / `Get`）。
- **Claude Code adapter** (`internal/adapter/claudecode`, strength: **hard**):
  generates a `PreToolUse` hook (shell + Python engine + `settings.json`) that
  blocks `Read | Grep | Glob | Bash` calls whose target matches `.readignore`
  **before** the tool runs. Zero third-party deps (Python stdlib only). The only
  adapter with true runtime interception today.
  — **Claude Code 适配器**（`internal/adapter/claudecode`，强度 **hard**）：生成
    `PreToolUse` 钩子（shell + Python 引擎 + `settings.json`），在工具运行**前**阻断目标
    匹配 `.readignore` 的 `Read | Grep | Glob | Bash` 调用。零第三方依赖（仅 Python
    标准库）。当前唯一具备真正运行时拦截的适配器。
- **opencode adapter** (`internal/adapter/opencode`, strength: **config**):
  generates `opencode.json` with `permission.read` deny/allow globs. Negation
  (`!`) is approximated via glob specificity. Honestly labeled `config` strength
  since opencode's `permission.ask` hook is a runtime no-op
  ([opencode #7006](https://github.com/anomalyco/opencode/issues/7006)).
  — **opencode 适配器**（`internal/adapter/opencode`，强度 **config**）：生成
    `opencode.json`，含 `permission.read` 的 deny/allow glob。取反（`!`）通过 glob
    特异性近似。诚实标注为 `config` 强度，因 opencode 的 `permission.ask` 钩子在运行时
    为空操作（[opencode #7006](https://github.com/anomalyco/opencode/issues/7006)）。
- **CLI** (`internal/cli`, cobra): five subcommands —
  `init`, `adapters`, `generate <id>`, `install <id|--all>`, `check`. Idempotent
  installs (skip existing files unless `--force`), friendly errors, version flag
  (`-v`/`--version`) with `ldflags` injection.
  — **CLI**（`internal/cli`，cobra）：五个子命令——`init`、`adapters`、
    `generate <id>`、`install <id|--all>`、`check`。幂等安装（除非 `--force`，否则跳过
    已有文件），友好的错误提示，版本标志（`-v`/`--version`）通过 `ldflags` 注入。
- **CI** (`.github/workflows/ci.yml`): build & test matrix
  (ubuntu/macos/windows × Go 1.25) + golangci-lint job.
  — **CI**（`.github/workflows/ci.yml`）：构建与测试矩阵
    （ubuntu/macos/windows × Go 1.25）+ golangci-lint 任务。
- **goreleaser config** (`.goreleaser.yml`): cross-platform binaries
  (linux/darwin/windows × amd64/arm64), SHA256 `checksums.txt`, Homebrew tap,
  Conventional-Commits-grouped changelog, draft GitHub Releases.
  — **goreleaser 配置**（`.goreleaser.yml`）：跨平台二进制
    （linux/darwin/windows × amd64/arm64）、SHA256 `checksums.txt`、Homebrew tap、按
    Conventional-Commits 分组的 changelog、GitHub Release 草稿。
- **golangci-lint config** (`.golangci.yml`): govet, staticcheck, errcheck,
  gosec, gofmt, gofumpt, ineffassign, unused.
  — **golangci-lint 配置**（`.golangci.yml`）：govet、staticcheck、errcheck、gosec、
    gofmt、gofumpt、ineffassign、unused。
- Project scaffolding: `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`,
  MIT `LICENSE`, `Makefile` targets, issue templates.
  — 项目脚手架：`CONTRIBUTING.md`、`CODE_OF_CONDUCT.md`、`SECURITY.md`、MIT
    `LICENSE`、`Makefile` 目标、issue 模板。

[Unreleased]: https://github.com/0xByteBard404/readignore/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/0xByteBard404/readignore/releases/tag/v0.2.0
[0.1.0]: https://github.com/0xByteBard404/readignore/releases/tag/v0.1.0
