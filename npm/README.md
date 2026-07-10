# readignore

**`.gitignore` for AI coding agents — declare files your AI agent must not read.**

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/0xByteBard404/readignore/blob/main/LICENSE)

This is the **npm wrapper package** for [readignore](https://github.com/0xByteBard404/readignore) — a Go CLI that adapts a single `.readignore` (gitignore syntax) into each AI coding agent's strongest native defense mechanism.

> The `postinstall` hook downloads the right prebuilt Go binary for your platform from [GitHub Releases](https://github.com/0xByteBard404/readignore/releases). No Go toolchain required.

---

## Why

AI coding agents (Claude Code, Cursor, Codex, opencode, kilo code, …) can read any file in your repo at runtime — **including secrets** like `.env`, `*.pem`, `id_rsa`, `credentials.json`.

- `.gitignore` only stops **git** from committing; the agent still reads the file.
- Claude Code's `permissions.deny: Read(.env)` only blocks the **Read** tool — agents bypass it with `Grep`, `Glob`, or `Bash` (`grep . .env` works).

**readignore** closes that gap with one `.readignore` that gets adapted into each agent's native defense mechanism — at the strongest level that agent actually supports.

---

## Install

```bash
npm i -g readignore        # global install
# or run once without installing:
npx readignore --version
```

Requires Node 18+ and one of: linux (x64/arm64), macOS (x64/arm64), Windows (x64).

---

## Quickstart

```bash
cd your-repo
readignore init                      # generates .readignore with common secret patterns
readignore install claude-code       # single agent
readignore install --all             # every adapter detected in this repo
readignore check                     # validate .readignore + report install status
```

`init` refuses to overwrite an existing `.readignore` unless you pass `--force`.

---

## Commands

```bash
readignore init [--force]                  # generate a .readignore template
readignore adapters                        # list adapters + strength + detection status
readignore generate <adapter>              # dry-run: print what an adapter would generate
readignore install <adapter> [--force]     # write an adapter's output to disk
readignore install --all                   # all adapters detected here
readignore check                           # validate syntax + report install status
```

If a target file already exists, `install` **skips it** (and tells you to merge manually) unless you pass `--force`.

---

## Supported agents

readignore adapts `.readignore` into each agent's strongest *real* mechanism. Strength tiers are **honest**, not marketing:

| Agent | Strength | Mechanism | Status |
|---|---|---|---|
| **Claude Code** | hard | `PreToolUse` hook — blocks the tool call **before** it runs (Read, Grep, Glob, Bash). | ✅ shipped |
| **codex CLI** | hard | `.codex/hooks.json` Claude-style `PreToolUse` hook (bash+python). | ✅ shipped |
| **pi** | hard | `.pi/extensions/readignore.ts` TS extension that **overrides** the built-in `read` tool. | ✅ shipped |
| **opencode** | config | `permission.read` deny/allow globs in `opencode.json`. | ✅ shipped |
| **Cursor** | soft | `.cursor/rules` natural-language advisory (model may comply). | 🗺 roadmap |
| **kilo code** | — | mechanism TBD. | 🗺 roadmap |

---

## `.readignore` syntax

100% gitignore-compatible. Zero learning curve:

```gitignore
# readignore — files this repo's AI agent must not read

# Secrets & keys
.env
.env.*
!.env.example            # ! un-ignores (negation): allow the template through
*.pem
*.key

# SSH / cloud credentials
**/id_rsa
.aws/
.gcp/

# Sensitive directories
secrets/
credentials.json

# Trailing / anchors to directories only
build/
```

Supported: `*`, `**`, `?`, `[abc]` character classes, `!` negation (last-match-wins), trailing `/` for directory anchoring, `#` comments.

---

## Full documentation

For the capability matrix, per-adapter details, negation caveats, and design notes, see the **[full README on GitHub](https://github.com/0xByteBard404/readignore#readme)**.

---

## Wrapper package notes

- This npm package is a thin wrapper. The actual CLI is a Go binary downloaded by `postinstall`.
- `postinstall` verifies the download with SHA256 (from `checksums.txt`) when available.
- To install without npm, see [alternative install methods](https://github.com/0xByteBard404/readignore#installation) (`go install`, direct binary download).
- Offline / air-gapped install: pre-place the Go binary at `node_modules/readignore/bin/readignore[.exe]` and run `npm install readignore --ignore-scripts` to skip the download.

---

## License

[MIT](https://github.com/0xByteBard404/readignore/blob/main/LICENSE) © 2026 0xByteBard404
