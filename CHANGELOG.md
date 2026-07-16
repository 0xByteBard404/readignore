# Changelog

All notable changes to this project will be documented in this file.
本项目所有重要变更均记录于此文件。

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
格式基于 [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)，并遵循
[语义化版本](https://semver.org/spec/v2.0.0.html)。

## [Unreleased]

### Added / 新增

<!-- 下次发版在此添加 -->

## [0.6.0] - 2026-07-16

Two major features: **sectioned file permissions** (`[read]`/`[edit]`/`[delete]`) and **npm single-package with bundled binaries** (drop postinstall, allow-scripts-safe).

两大功能：**分段式文件权限**（`[read]`/`[edit]`/`[delete]`）+ **npm 单包含全平台 binary**（移除 postinstall，allow-scripts 无关）。

### Added / 新增

- **Sectioned file permissions** (`[read]`/`[edit]`/`[delete]`): one `.readignore` now segments
  into three permission sections — unreadable / read-only / no-delete. Claude Code & codex
  intercept `Edit`/`Write`/`NotebookEdit` tools (via `notebook_path`) + Bash `rm`
  (`parseDeletePaths`). opencode/kilo get `permission.edit`; pi honestly labeled unsupported.
  Bare patterns default to `[read]` (zero-breaking read path). `[delete]` is best-effort.
  — **分段式文件权限**：一份 `.readignore` 分三段——不可读 / 只读不可改 / 不可删。Claude Code
    与 codex 拦截 `Edit`/`Write`/`NotebookEdit`（取 `notebook_path`）+ Bash `rm`
    （`parseDeletePaths`）。opencode/kilo 获得 `permission.edit`；pi 诚实标注 unsupported。
    裸 pattern 默认归 `[read]`（读路径零 breaking）。`[delete]` 为 best-effort。
- **npm single-package bundles all platform binaries** (`npm/bin.js` + `npm/bin/<platform>/`):
  the npm package now ships all 5 platform prebuilt binaries (~7MB) inside the package.
  No `postinstall` download, no `allow-scripts` dependency — 100% reliable on default npm config.
  — **npm 单包含全平台 binary**：npm 包内含 5 平台 prebuilt binary（~7MB）。无 postinstall 下载、
    无 `allow-scripts` 依赖——默认 npm 配置下 100% 可靠。

### Changed / 变更

- **Bare patterns also block write-class Bash commands that read the source**
  (`internal/cli/hookcheck.go`): a `.readignore` with no section headers puts every
  pattern into `[read]`. Write-class Bash verbs (`cp`/`mv`/`tee`/`sed -i`/`>` redirect)
  are classified as `OpEdit`, but they **read the source file** before writing
  (e.g. `cp .env /tmp` reads `.env`). Previously the `OpEdit` branch only checked the
  `[edit]` section, which is empty for bare patterns — so `cp .env` was silently
  allowed (source leak, a regression from the v0.5.0 single-section behavior). The
  `OpEdit` branch now also checks the `[read]` section to preserve leak protection.
  `rm`/delete is unaffected: deletion does not read the source, and protecting a file
  from deletion still requires an explicit `[delete]` section (section-independent design).
  — **裸 pattern 同时拦「读源的写类 Bash 命令」**（`internal/cli/hookcheck.go`）：无段头的
    `.readignore` 把所有 pattern 归入 `[read]`。写类 Bash 动词（`cp`/`mv`/`tee`/`sed -i`/`>`
    重定向）归 `OpEdit`，但它们在写目的端前会**读源文件**（如 `cp .env /tmp` 读 `.env`）。
    此前 `OpEdit` 分支只查 `[edit]` 段——裸 pattern 的 `[edit]` 段为空，`cp .env` 被静默放行
    （源泄露，相比 v0.5.0 单段行为属回归）。`OpEdit` 分支现补查 `[read]` 段以守泄露。
    `rm`/delete 不受影响：删除不读源；护删仍需显式 `[delete]` 段（段独立设计）。

## [0.5.0] - 2026-07-15

New feature: **update-check** — readignore now checks GitHub for a newer version
when you run a non-hot-path command, and prints a green bilingual notice to stderr
if yours is outdated, pointing at real upgrade channels (brew/npm/install.sh).
24h cache, 1s timeout, never blocks. See README "Update checks" for phone-home
details and opt-out.

新功能：**新版本检测提示**——readignore 现在会在你跑非热路径命令时检测 GitHub 最新版，
落后则向 stderr 打印绿色双语提示，指明真实升级渠道（brew/npm/install.sh）。24 小时缓存、
1 秒超时、绝不阻断主命令。phone-home 透明度与 opt-out 见 README "更新检查"节。

### Added / 新增

- **update-check** (`internal/cli/updatecheck.go`, mounted via root `PersistentPreRunE`):
  detects the latest GitHub release when you run a non-hot-path command and prints a
  green bilingual notice to stderr if yours is outdated. Real upgrade channels only
  (`brew upgrade readignore` / `npm i -g readignore` / re-run `install.sh`) — never
  `readignore update` (which only refreshes adapter artifacts, not the binary).
  24h cache at `<cache-dir>/readignore/version-check.json`, 1s HTTP timeout, hand-written
  semver (no `x/mod` dep). Guards (all silent, never block): `dev` build, hot-path
  commands (`match`/`hook-check`) + `update`, `READIGNORE_NO_UPDATE_CHECK=1` env, non-TTY.
  — **新版本检测提示**（`internal/cli/updatecheck.go`，经 root `PersistentPreRunE` 挂载）：
    跑非热路径命令时检测 GitHub 最新版，落后则向 stderr 打印绿色双语提示。只指真实
    升级渠道（`brew upgrade readignore` / `npm i -g readignore` / 重跑 `install.sh`）——
    绝不指 `readignore update`（它只刷新适配器产物，不升级二进制）。24h 缓存于
    `<缓存目录>/readignore/version-check.json`，1s HTTP 超时，手写 semver（不引入 `x/mod`）。
    护栏（全部静默、绝不阻断）：`dev` 构建、热路径命令（`match`/`hook-check`）+ `update`、
    `READIGNORE_NO_UPDATE_CHECK=1` 环境变量、non-TTY。

## [0.4.0] - 2026-07-14

New adapter: **kilocode** (kilo.ai). Plus two build/install fixes — a module-path
typo that broke `make tidy`/lint in CI, and a wrong npm package name in the
installer's Windows hint.

新适配器：**kilocode**（kilo.ai）。外加两个构建/安装修复——一处模块路径拼写错误
（曾在 CI 破坏 `make tidy`/lint），一处安装脚本 Windows 提示里的 npm 包名错误。

### Added / 新增

- **kilocode adapter** (`internal/adapter/kilocode`, strength: **config**): generates
  `kilo.json` with `permission.read` deny/allow globs for [kilo code](https://kilo.ai)
  (open-source AI coding agent, OpenCode fork). Detects `.kilo/`, `kilo.json`, `.kilocode/`.
  Handles `**/` prefix stripping (kilocode wildcard has no `**` support). Honestly labeled
  `config` due to known deny bypass bugs (#8293, #11637).
  — **kilocode 适配器**（`internal/adapter/kilocode`，强度 **config**）：为
    [kilo code](https://kilo.ai)（开源 AI 编程智能体，OpenCode fork）生成 `kilo.json`，
    含 `permission.read` deny/allow glob。检测 `.kilo/`、`kilo.json`、`.kilocode/`。
    处理 `**/` 前缀剥离（kilocode wildcard 不支持 `**`）。因已知 deny 绕过 bug（#8293、
    #11637），诚实标为 `config`。

### Fixed / 修复

- **Wrong npm package name in installer's Windows hint** (`install.sh`): the
  Windows/MINGW error pointed at `@caixuetang/readignore`, a scope that does not
  exist on npm. Corrected to the real package name `readignore`.
  — **安装脚本 Windows 提示里的 npm 包名错误**（`install.sh`）：Windows/MINGW 错误
    提示指向 `@caixuetang/readignore`——npm 上不存在的 scope。已改为真实包名 `readignore`。
- **Module-path typo broke `make tidy` and CI lint** (`internal/cli/root.go`): the
  blank import for `internal/adapter/opencode` was spelled
  `github.com/0ByteBard404/...` (missing the `x` in `0xByteBard404`), so Go treated
  the project's own internal package as external and tried to fetch it from GitHub,
  breaking `make tidy` and lint. Restored the `x` so the import resolves locally.
  — **模块路径拼写错误导致 `make tidy` 与 CI lint 失败**（`internal/cli/root.go`）：
    `internal/adapter/opencode` 的 blank import 拼成
    `github.com/0ByteBard404/...`（`0xByteBard404` 缺了 `x`），Go 于是把项目自身的
    internal 包当成外部 module 去 GitHub 拉，导致 `make tidy` 与 lint 失败。已补回 `x`，
    import 本地解析正常。

## [0.3.3] - 2026-07-12

Hook moved to Go: the Claude Code/codex hook now forwards to a new
`readignore hook-check` subcommand. JSON parsing and matching are done in Go
(`encoding/json`), fixing the multi-line-command / escaped-quote bypass of the
old bash `grep` extraction.

钩子迁移到 Go：Claude Code/codex 钩子转发到新增的 `readignore hook-check` 子命令。
JSON 解析与匹配全在 Go（encoding/json），修复旧 bash grep 抽取的多行命令 / 转义引号绕过。

### Fixed / 修复

- **Hook bypass via multi-line Bash commands** (`hookengine` + new `cli/hook-check`):
  the old bash hook extracted the `command` field with `grep -oE` (line mode), so
  multi-line commands escaped extraction entirely — `echo x\ncat .env` was not
  blocked. The hook now forwards to `readignore hook-check` (Go), which parses
  tool_input with `encoding/json` (multi-line, escaped quotes, all JSON edges) and
  pre-normalizes real newlines to spaces. Also covers `\"` truncation and `~` paths.
  — **多行 Bash 命令绕过钩子**（`hookengine` + 新 `cli/hook-check`）：旧 bash 钩子用
    `grep -oE`（行模式）抽 `command`，多行命令整个逃过抽取——`echo x\ncat .env` 拦不住。
    现在转发到 `readignore hook-check`（Go），用 `encoding/json` 解析（多行、转义引号、
    所有 JSON 边界）并预处理换行→空格。同时覆盖 `\"` 截断与 `~` 路径。

### Security / 安全说明

Bash 拦截现覆盖所有**字面路径**（多行/转义/`~`/扩展名/dotfile）。变量展开（`cat $F`）
与间接路径（`ln -s .env x; cat x`）仍是静态分析固有天花板——Read/Grep/Glob 工具仍
100% 硬拦截（路径直接、精确）。

## [0.3.2] - 2026-07-12

`readignore update` now defaults to `--all` when called with no arguments — one
command refreshes every detected adapter.

`readignore update` 无参时默认 `--all`，一条命令刷新所有检测到的适配器。

### Changed / 变更

- **`update` defaults to `--all` with no args** (`internal/cli`): `readignore update`
  with no arguments now refreshes all detected adapters (previously errored asking
  for an ID or `--all`). `install` is unchanged — it still requires an explicit
  target, since installing is a write that should name its subject.
  — **`update` 无参默认 `--all`**（`internal/cli`）：`readignore update` 无参时刷新
    所有检测到的适配器（原先报错要 ID 或 `--all`）。`install` 不变——install 是写入
    操作，仍需明确目标。

## [0.3.1] - 2026-07-12

Quick fix release: a new `uninstall` command (install's inverse) and a fix for
the Claude Code/codex hook falsely blocking legitimate Bash commands.

快速修复版：新增 `uninstall` 命令（install 的逆操作），并修复 Claude Code/codex
钩子误拦合法 Bash 命令的问题。

### Added / 新增

- **`readignore uninstall` subcommand** (`internal/cli`): removes an adapter's
  generated files (`readignore uninstall <id|--all>`), the inverse of `install`.
  Supports `--dry-run` to preview. Reuses `Generate` for the file list (no
  manifest needed). Leaves `.readignore` intact. Closes the
  install-without-uninstall asymmetry surfaced during dogfooding.
  — **`readignore uninstall` 子命令**（`internal/cli`）：移除适配器生成的文件
    （`readignore uninstall <id|--all>`），即 install 的逆操作。支持 `--dry-run`
    预览。复用 `Generate` 取产物清单（无需 manifest）。不删 `.readignore`。补上
    dogfood 时发现的「能 install 不能 uninstall」不对称缺口。

- **`readignore update` subcommand** (`internal/cli`): refreshes an adapter's
  generated files to the current readignore version — equivalent to
  `install --force`. Use it after upgrading readignore to pick up hook/adapter
  improvements (e.g. the false-positive fix in this release: `readignore update
  claude-code` refreshes `.claude/hooks/readignore.sh`).
  — **`readignore update` 子命令**（`internal/cli`）：把适配器产物刷新到当前
    readignore 版本——等价于 `install --force`。readignore 升级后用它拾取钩子/适配器
    改进（如本次误报修复：`readignore update claude-code` 刷新 `.claude/hooks/readignore.sh`）。

### Fixed / 修复

- **Hook no longer false-positives on Bash commands** (`internal/adapter/shared/hookengine`):
  the Claude Code/codex hook used to split the Bash command into tokens and
  `readignore match` every one, which blocked legit commands like
  `git config --global user.email x@y` when a token happened to match a
  `.readignore` rule. A `looks_like_path` guard now skips command names, flags,
  and values — only path-like tokens (bearing `/`, bearing `.`, or present on
  disk) are matched.
  — **钩子不再对 Bash 命令误报**（`internal/adapter/shared/hookengine`）：Claude
    Code/codex 钩子原先把 Bash 命令切成 token 逐个 match，导致
    `git config --global user.email x@y` 这类合法命令因某 token 撞上 `.readignore`
    规则被误拦。现加 `looks_like_path` 守卫——跳过命令名/选项/值，只对路径式 token
    （含 `/`、含 `.`、或磁盘存在的）做匹配。

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

[Unreleased]: https://github.com/0xByteBard404/readignore/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/0xByteBard404/readignore/releases/tag/v0.6.0
[0.5.0]: https://github.com/0xByteBard404/readignore/releases/tag/v0.5.0
[0.4.0]: https://github.com/0xByteBard404/readignore/releases/tag/v0.4.0
[0.3.3]: https://github.com/0xByteBard404/readignore/releases/tag/v0.3.3
[0.3.2]: https://github.com/0xByteBard404/readignore/releases/tag/v0.3.2
[0.3.1]: https://github.com/0xByteBard404/readignore/releases/tag/v0.3.1
[0.3.0]: https://github.com/0xByteBard404/readignore/releases/tag/v0.3.0
[0.2.0]: https://github.com/0xByteBard404/readignore/releases/tag/v0.2.0
[0.1.0]: https://github.com/0xByteBard404/readignore/releases/tag/v0.1.0
