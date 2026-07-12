// Package hookengine 提供 Claude-style PreToolUse hook 的公共生成引擎。
//
// v0.3 起，匹配权威统一收敛到 `readignore match` 子命令（go-git format/gitignore）。
// 本包生成的 sh 脚本不再内嵌 patterns、不再 fork python 引擎——它从 tool_input JSON
// 抽取目标路径/命令，逐个调 `readignore match <path>`（exit 1 = deny），命中即输出
// PreToolUse deny 决策。`.readignore` 在运行时由 `readignore match` 直接读盘，故
// 改 .readignore 不必 re-install 即立即生效（v0.3 核心价值）。
//
// 本包只负责 sh 内容生成；适配器专属的配置包装（Claude Code 的 .claude/settings.json、
// codex 的 ~/.codex/hooks.json）仍留在各自适配器包。
//
// 契约（PreToolUse，sh ↔ readignore match 进程协议）：
//   - sh 从 tool_input JSON 抽取目标路径/命令（grep，无 jq 依赖）；
//   - 调 `readignore match <path>`：exit 1（deny）即命中 .readignore；
//   - 命中 → stdout 输出 PreToolUse deny JSON；不命中 → 静默 exit 0；
//   - readignore 不在 PATH → 放行（fallback，不搞死）+ stderr 警告。
//
// 已知限制：`readignore match` 读 cwd/.readignore，故 hook 必须从仓库根发起
// （Claude Code / codex 实际均如此）。绝对路径会被 go-git matcher 按相对路径处理，
// 与 v0.2 py 引擎行为一致（候选路径规范化在 readignore match 侧）。
package hookengine

// denyJSON 是 PreToolUse deny 决策的固定输出。命中 .readignore 时 sh 直接 printf 它。
//
// hookSpecificOutput.permissionDecision:"deny" 是 Claude-style 协议的拦截信号
// （claudecode + codex 共用此 schema，源码确认）。
const denyJSON = `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"readignore: path declared in .readignore"}}` + "\n"

// shScript 是 readignore.sh 全文（Claude-style PreToolUse hook 的 shell 脚本）。
//
// 设计要点：
//   - 无 jq 依赖：用 grep -oE 从原始 JSON 文本抽取字段值（Read→file_path、Grep→path、
//     Glob→pattern、Bash→command）；
//   - readignore 不在 PATH → fallback 放行 + stderr 警告（不搞死开发环境）；
//   - 逐个调 `readignore match <path>`：exit 1 = deny → 输出 PreToolUse deny JSON；
//   - command 字段（Bash）整体当 shell 命令处理：按元字符切 token，逐个 match；
//   - 命中 → stdout 输出 deny JSON；不命中 → 静默 exit 0；
//   - 工作目录假设为仓库根（readignore match 读 cwd/.readignore）。
//
// 与 v0.2 py 引擎的差异：匹配判定不再在 hook 进程内做，而是委托给 `readignore match`
// （go-git 权威），故 .readignore 改动无需 re-install 即立即生效（动态读核心价值）。
const shScript = `#!/usr/bin/env bash
# readignore PreToolUse hook（由 readignore CLI 生成，请勿手改）。
# 从 Claude Code 传入的 tool_input JSON 中抽取目标路径/命令，调 ` + "`readignore match`" + `
# 判定是否命中 cwd/.readignore；命中即输出 PreToolUse deny 决策。
#
# 覆盖工具：Read | Grep | Glob | Bash（由 settings/hooks.json 的 matcher 指定）。
# 匹配权威：` + "`readignore match`" + `（go-git format/gitignore，与 git 一致）。
# 无 jq 依赖：用 grep -oE 抽取字段值；readignore 不在 PATH 则放行（不搞死）。
set -uo pipefail

input=$(cat)

# readignore 不在 PATH：fallback 放行 + stderr 警告（避免把开发环境搞死）。
command -v readignore >/dev/null 2>&1 || {
  echo "readignore: readignore not in PATH; hook disabled (allowing)." >&2
  exit 0
}

# tool_input 字段抽取。Claude Code 实际 JSON: {"tool_name":"...","tool_input":{"<field>":"<value>"}}。
# 抓 "field":"value" 形式；value 内不允许未转义双引号（Claude Code 路径均经 JSON 转义）。
extract_field() {
  printf '%s' "$input" | grep -oE "\"$1\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -n1 | sed -E 's/.*:[[:space:]]*"(.*)"$/\1/'
}

deny() {
  printf '%s\n' '` + denyJSON + `'
  exit 0
}

# 依次尝试各路径字段（file_path/path/pattern）；任一被 readignore match 判定 deny
# （exit 1）即 deny。
for field in file_path path pattern; do
  val=$(extract_field "$field")
  [ -z "$val" ] && continue
  if ! readignore match "$val" >/dev/null 2>&1; then
    deny
  fi
done

# command 字段（Bash 工具）整体当 shell 命令处理：按空白/shell 元字符切 token，
# 只对「像路径」的 token 调 readignore match——跳过命令名/选项/字符串值，避免把
# git config --global user.email x@y 这类合法命令误判命中（false positive）。
# 覆盖：cat .env / grep foo secret.pem / scp sub/id_rsa host:/ 等。
looks_like_path() {
  case "$1" in
    -*) return 1 ;;     # --global / -rf / -n  等选项
    */*) return 0 ;;    # sub/id_rsa, ./.env.production, host:/path
    *.*) return 0 ;;    # 含点：dotfile（.env .aws）或带扩展名（secret.pem, config.json）
  esac
  [ -e "$1" ]           # 磁盘存在兜底（无点无 / 的真实文件名，如 README）
}
val=$(extract_field "command")
if [ -n "$val" ]; then
  # 用一组常见分隔符切 token（保守：不破坏文件名里的 . _ - +）。
  for tok in $(printf '%s' "$val" | tr ' \t|;<>&"'\''(){}' '\n'); do
    [ -z "$tok" ] && continue
    looks_like_path "$tok" || continue
    if ! readignore match "$tok" >/dev/null 2>&1; then
      deny
    fi
  done
fi

# 无命中：静默放行。
exit 0
`

// BuildShScript 返回 readignore.sh 全文（Claude-style PreToolUse hook 的 shell 脚本）。
//
// v0.3 起 sh 是通用的：不内嵌 patterns、不引用 readignore.py，而是调 `readignore match`
// （go-git 权威）。故本函数无参数——claudecode 与 codex 适配器共用同一份 sh。
//
// 匹配判定全在 `readignore match` 侧（读 cwd/.readignore），故改 .readignore 不必
// re-install 即立即生效（动态读核心价值）。
func BuildShScript() string {
	return shScript
}
