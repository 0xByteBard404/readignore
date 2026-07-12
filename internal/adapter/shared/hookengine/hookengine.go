// Package hookengine 提供 Claude-style PreToolUse hook 的公共生成引擎。
//
// v0.3.3 起，sh 脚本简化为一行：调 `readignore hook-check`（Go 子命令）。
// JSON 解析（encoding/json，健壮处理多行 command / 转义双引号 / 所有 JSON 边界）
// 与匹配（go-git format/gitignore）全收敛到 Go，消除旧 bash grep 抽取的绕过面。
//
// 旧版（v0.3.0–v0.3.2）sh 用 grep -oE 抽 tool_input，对多行 command / \" 转义脆弱
// （可绕过）。v0.3.3 改为转发到 hook-check，根治 JSON 层。
//
// 本包只负责 sh 内容生成；适配器专属配置包装（Claude Code 的 .claude/settings.json、
// codex 的 ~/.codex/hooks.json）仍留在各自适配器包。
package hookengine

// shScript 是 readignore.sh 全文（Claude-style PreToolUse hook 的 shell 脚本）。
//
// v0.3.3 起简化为：检测 readignore 在 PATH → 调 `readignore hook-check`。
// 所有 JSON 解析与匹配在 Go（hook-check）完成，sh 只转发 stdin/stdout。
//
// 设计要点：
//   - readignore 不在 PATH → fallback 放行 + stderr 警告（不搞死开发环境）；
//   - hook-check 从 stdin 读 tool_input，命中 cwd/.readignore → stdout 输出 deny JSON；
//   - 工作目录假设为仓库根（hook-check 读 cwd/.readignore）。
const shScript = `#!/usr/bin/env bash
# readignore PreToolUse hook（由 readignore CLI 生成，请勿手改）。
# 转发到 readignore hook-check：JSON 解析与匹配全在 Go（encoding/json 健壮解析，
# 正确处理多行 command、转义双引号等所有 JSON 边界，消除旧 bash grep 的绕过面）。
# readignore 不在 PATH → 放行 + stderr 警告（不搞死开发环境）。
command -v readignore >/dev/null 2>&1 || {
  echo "readignore: readignore not in PATH; hook disabled (allowing)." >&2
  exit 0
}
readignore hook-check
`

// BuildShScript 返回 readignore.sh 全文（Claude-style PreToolUse hook 的 shell 脚本）。
//
// v0.3.3 起 sh 是一行转发（调 readignore hook-check）。本函数无参数——claudecode 与
// codex 适配器共用同一份 sh。
func BuildShScript() string {
	return shScript
}
