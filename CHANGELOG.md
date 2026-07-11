# Changelog

All notable changes to this project will be documented in this file.
本项目所有重要变更均记录于此文件。

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
格式基于 [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)，并遵循
[语义化版本](https://semver.org/spec/v2.0.0.html)。

## [Unreleased]

## [0.3.0] - 2026-07-11

The match authority is unified: every hook now calls `readignore match` (go-git
gitignore engine), so **editing `.readignore` takes effect immediately** — no
re-install needed. The Python match engine and the pi hand-written TS matcher
are gone (-486 lines net). Install gets easier too: `curl | sh` and Homebrew.

匹配权威统一：所有钩子现调 `readignore match`（go-git gitignore 引擎），因此
**改 `.readignore` 立即生效**——无需重新 install。Python 匹配引擎与 pi 手写 TS
matcher 已移除（净减 486 行）。安装也更方便：`curl | sh` 与 Homebrew。

### Added / 新增

- **`readignore match` subcommand** (`internal/cli`): authoritative gitignore
  match via go-git's `format/gitignore`. `readignore match <path>` exits `0` if
  the path is allowed (not matched) and `1` if denied (matched against
  `cwd/.readignore`). Reads the file live on every call — the single source of
  truth for all hooks.
  — **`readignore match` 子命令**（`internal/cli`）：基于 go-git
    `format/gitignore` 的权威 gitignore 匹配。`readignore match <path>` 路径放行
    （未命中）退出 `0`，命中（匹配 `cwd/.readignore`）退出 `1`。每次调用都实时读盘
    ——所有钩子的唯一匹配权威。
- **Dynamic `.readignore` (no re-install)**: hooks re-read `cwd/.readignore` via
  `readignore match` on every tool call. Editing rules takes effect immediately,
  matching the `.gitignore` edit-and-go experience. This is the core v0.3
  value: previously you had to re-run `install` after editing `.readignore` to
  re-bake patterns into the generated hook files.
  — **动态 `.readignore`（无需 re-install）**：钩子每次工具调用都通过
    `readignore match` 重读 `cwd/.readignore`。编辑规则立即生效，与 `.gitignore`
    一样改完即用。这是 v0.3 的核心价值：之前改 `.readignore` 后必须重跑 `install`
    把模式重新冻结进生成的钩子文件。
- **`curl | sh` one-liner installer** (`install.sh`): detects OS/arch, fetches
  the matching binary + `checksums.txt` from the latest release, verifies the
  SHA256, and installs `readignore` to `/usr/local/bin` (falling back to
  `~/.local/bin`). Linux/macOS, no Go or npm required.
  — **`curl | sh` 一键安装脚本**（`install.sh`）：检测 OS/架构，从最新 release
    下载对应二进制 + `checksums.txt`，校验 SHA256 后安装到 `/usr/local/bin`
    （回退到 `~/.local/bin`）。Linux/macOS，无需 Go 或 npm。
- **Homebrew tap**: `brews` stanza enabled in `.goreleaser.yml`
  (`0xByteBard404/homebrew-tap`). After the tap repo is created and the first
  v0.3 release publishes: `brew tap 0xByteBard404/tap && brew install readignore`.
  — **Homebrew tap**：`.goreleaser.yml` 启用 `brews` 段
    （`0xByteBard404/homebrew-tap`）。tap 仓库创建、首个 v0.3 release 发布后：
    `brew tap 0xByteBard404/tap && brew install readignore`。

### Changed / 变更

- **Hooks call `readignore match` — Python engine removed**: the Claude Code and
  codex hooks (`.claude/hooks/readignore.sh`, `.codex/hooks/readignore.sh`) now
  shell out to `readignore match <path>` instead of embedding a Python match
  engine. Adapter output drops from **3 files (sh + py + json) → 2 files
  (sh + json)**. If `readignore` is not on `PATH`, the hook falls back to
  **allow** with a stderr warning (never breaks the agent). Net **-486 lines**.
  — **钩子改调 `readignore match`——移除 Python 引擎**：Claude Code 与 codex 钩子
    （`.claude/hooks/readignore.sh`、`.codex/hooks/readignore.sh`）改为 shell 调
    `readignore match <path>`，不再内嵌 Python 匹配引擎。适配器产物从
    **3 文件（sh + py + json）→ 2 文件（sh + json）**。若 `readignore` 不在
    `PATH`，钩子回退**放行**并打 stderr 警告（绝不搞死 agent）。净减 **486 行**。
- **pi TS matcher removed**: `.pi/extensions/readignore.ts` now calls
  `readignore match` via `child_process.execFileSync` instead of a hand-written
  gitignore matcher. Spawn failure falls back to **allow** + stderr warning.
  The matcher is no longer frozen into the extension (different `.readignore`
  contents produce identical output).
  — **pi TS matcher 移除**：`.pi/extensions/readignore.ts` 改为通过
    `child_process.execFileSync` 调 `readignore match`，不再用手写 gitignore
    matcher。spawn 失败回退**放行** + stderr 警告。matcher 不再冻结进扩展
    （不同 `.readignore` 内容产出完全一致）。

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

[Unreleased]: https://github.com/0xByteBard404/readignore/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/0xByteBard404/readignore/releases/tag/v0.3.0
[0.2.0]: https://github.com/0xByteBard404/readignore/releases/tag/v0.2.0
[0.1.0]: https://github.com/0xByteBard404/readignore/releases/tag/v0.1.0
