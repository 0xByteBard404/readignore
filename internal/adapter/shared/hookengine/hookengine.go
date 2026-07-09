// Package hookengine 提供 Claude-style PreToolUse hook 的公共生成引擎。
//
// 这是 v0.2 阶段0（DRY 重构）的产物：v0.1 的 claudecode 适配器把 sh（hookShellScript）
// 与 py（hookPythonEngine）两套生成逻辑实现得已经很稳（含根锚定 I-1/I-2、字符类 M-1、
// 控制字符转义 M-2、注入安全、!!foo 取反等修复，21 case 集成测试 + 89.5% 覆盖守行为）。
//
// 由于 codex 的 hook 协议与 Claude Code 高度近似（同为 PreToolUse + permissionDecision:deny，
// 源码确认），v0.2 把这两套生成逻辑抽到本 shared 包，供 claudecode + codex 共用。
// 本包**只**负责 sh 与 py 内容生成；适配器专属的配置包装（如 Claude Code 的
// .claude/settings.json 片段、codex 的 ~/.codex 配置）仍留在各自适配器包。
//
// 契约（PreToolUse，sh ↔ py 进程协议）：
//   - sh 从 tool_input JSON 抽取目标路径/命令（grep，无 jq 依赖），交 py 判定；
//   - py 命中即 stdout 输出 "DENY"；sh 见 DENY 则输出 PreToolUse deny JSON；
//   - 不命中 → 静默 exit 0。
//
// 已知限制（与 claudecode v0.1 一致，不在搬迁中改）：py 引擎不区分目录与文件——
// `foo/` 这类「仅目录」模式会命中非目录的 `foo`（hook 拿到的候选路径不带尾斜杠，
// 引擎也无 stat 调用）。这是安全侧偏置（多拦而非少拦）。
package hookengine

import (
	"fmt"
	"strings"
)

// BuildShScript 返回 readignore.sh 全文（Claude-style PreToolUse hook 的 shell 脚本），
// 其中 python 引擎路径固定为 .claude/hooks/readignore.py（claudecode 适配器专用便捷函数）。
//
// rawPatterns 当前不被脚本内容引用（sh 不内嵌 patterns，匹配判定全在 readignore.py 里），
// 但保留在签名里以便：(1) 调用方语义清晰（与 BuildPyEngine 对称）；
// (2) 未来若 sh 需要按 patterns 做静态优化（如 patterns 全为空时直接放行）有扩展点。
//
// 落点不同的适配器（如 codex 写 .codex/hooks/）应改调 [BuildShScriptAt] 显式传路径。
func BuildShScript(rawPatterns []string) string {
	return BuildShScriptAt(rawPatterns, ".claude/hooks/readignore.py")
}

// BuildShScriptAt 与 [BuildShScript] 相同，但允许调用方指定 readignore.py 的相对路径
// （相对仓库根，sh 内部以该路径调 python）。codex 等落点不同的适配器用本函数。
//
// pyPath 应为 POSIX 风格（用 `/` 分隔），典型值：
//   - claudecode：".claude/hooks/readignore.py"（[BuildShScript] 即此默认）；
//   - codex：    ".codex/hooks/readignore.py"。
//
// 安全约束（API 契约）：pyPath 被直接插入未引用的 shell 位置（生成脚本里
// `"$PY" <pyPath> "$val"`，pyPath 不在引号内），故 pyPath 必须是不含空格与 shell
// 元字符（; | & $ ` " ' \ ( ) < > * ? [ ] # ! 等）的 POSIX 路径，否则会破坏生成的
// sh 语法或引入命令注入面。本函数不做转义/校验——这是调用方的责任。当前的合法调用方
// （claudecode/codex）均传字面常量路径，安全；若未来调用方接收外部输入，必须先做白名单校验。
//
// 设计要点（与 v0.1 claudecode 零差异，纯搬迁）：
//   - 无 jq 依赖：用 grep -oE 从原始 JSON 文本抽取字段值（Read→file_path、Grep→path、
//     Glob→pattern、Bash→command）；
//   - 跨平台 python：逐个试 python3/python 的 --version，第一个真正可执行的胜出
//     （Windows 上 python3 常是商店占位符，command -v 不够）；
//   - command 字段（Bash）整体当 shell 命令处理：py 按元字符切 token 扫描，
//     而非把 "cat .env" 当一个文件名；其它字段按路径判；
//   - 命中 → stdout 输出 PreToolUse deny JSON；不命中 → 静默 exit 0；
//   - 工作目录假设为仓库根（Claude Code 实际也是从仓库根发起 hook），
//     故用相对路径调 py。
//
// 实现说明：sh 内容分为固定 head 与一段引用 pyPath 的 loop，二者用普通字符串拼接
// （head + loop），避免 raw string literal 内无法插值 pyPath 的问题。
func BuildShScriptAt(rawPatterns []string, pyPath string) string {
	_ = rawPatterns // 当前 sh 不内嵌 patterns；保留参数供未来扩展（见 godoc）。
	const head = `#!/usr/bin/env bash
# readignore PreToolUse hook（由 readignore CLI 生成，请勿手改）。
# 从 Claude Code 传入的 tool_input JSON 中抽取目标路径/命令，喂给 readignore.py
# 判定是否命中 .readignore；命中即输出 PreToolUse deny 决策。
#
# 覆盖工具：Read | Grep | Glob | Bash（由 settings/hooks.json 的 matcher 指定）。
# 无 jq 依赖：用 grep -oE 抽取字段值；匹配判定全在 readignore.py 里（标准库）。
set -u

input=$(cat)

# 选真正可执行的 python：command -v 不足以判断（Windows 上 python3 常是商店占位符，
# 存在但执行返回非零）。逐个试 --version，第一个真正能跑的胜出。
pick_python() {
  local candidate
  for candidate in python3 python; do
    if command -v "$candidate" >/dev/null 2>&1 && "$candidate" --version >/dev/null 2>&1; then
      printf '%s' "$candidate"
      return 0
    fi
  done
  return 1
}
PY=$(pick_python)
if [ -z "$PY" ]; then
  # python 不可用：放行（避免把开发环境搞死），但写一条 stderr 警告便于排查。
  echo "readignore: python not found on PATH; hook disabled (allowing)." >&2
  exit 0
fi

# tool_input 字段抽取。Claude Code 实际 JSON: {"tool_name":"...","tool_input":{"<field>":"<value>"}}。
# 抽取策略：对每个支持字段尝试 grep -oE，命中任一即拿去判定（多命中也只判第一个）。
#
# 已知限制（M-4，记录不改）：grep -oE 用 [^"]* 截取 value，路径里含未转义双引号
# （罕见但理论可能）会被截断。Claude Code 的 tool_input 路径均经 JSON 转义（" → \"），
# 故正常输入不受影响；此处仅备注边界。
extract_field() {
  local input="$1"
  local field="$2"
  # 抓 "field":"value" 形式；value 内不允许未转义双引号。
  # 双引号外层包整个 regex，内层用 \" 表字面双引号，$field 直接插值。
  printf '%s' "$input" | grep -oE "\"$field\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -n1 | sed -E 's/.*:[[:space:]]*"(.*)"$/\1/'
}

deny() {
  printf '%s\n' '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"readignore: 该路径在 .readignore 中声明为敏感文件，已拦截"}}'
  exit 0
}

# 依次尝试各工具的目标字段；任一被 py 判定命中（stdout 输出 DENY）即 deny。
# 注意：py 的 allow/deny 信号走 stdout，exit code 恒为 0（PreToolUse hook 不能非零退）。
# command 字段（Bash 工具）整体当 shell 命令处理：py 按元字符切 token 扫描，
# 而非把 "cat .env" 当一整个文件名。其它字段（file_path/path/pattern）按路径判。
`
	loop := `for field in file_path path pattern; do
  val=$(extract_field "$input" "$field")
  if [ -n "$val" ]; then
    result=$("$PY" ` + pyPath + ` "$val" 2>/dev/null)
    if [ "$result" = "DENY" ]; then
      deny
    fi
  fi
done
val=$(extract_field "$input" "command")
if [ -n "$val" ]; then
  result=$("$PY" ` + pyPath + ` --command "$val" 2>/dev/null)
  if [ "$result" = "DENY" ]; then
    deny
  fi
fi

# 无命中：静默放行。
exit 0
`
	return head + loop
}

// BuildPyEngine 返回 readignore.py 全文，把 patterns 以合法 Python 字面量内嵌。
//
// rawPatterns 在内嵌前会先经 sanitizePatterns 清洗（去空白/注释行，保留取反行），
// 与 v0.1 claudecode.Generate 的行为一致——调用方传入 RawPatterns 即可，无需自行清洗。
//
// 匹配引擎方案（零第三方依赖，仅标准库 re）：
//   - 每条 pattern 编译成一条 (negated, regex) 规则；
//   - glob→regex 转换遵循 gitignore 语义：
//   - `**/`  → 任意层级目录前缀（含零层）；
//   - `/**`  → 任意层级后缀；
//   - `/**/` → 跨任意层级（含零层）的目录分隔；
//   - `*`    → 匹配单层内任意字符（不跨 /）；
//   - `?`    → 单个非 / 字符；
//   - 尾 `/` → 仅匹配目录（判定时把候选当作含尾斜杠传入即识别）；
//   - 无 `/` → basename 匹配（gitignore：无斜杠的模式匹配任意层级 basename）。
//   - 取反 (`!`)：按文件顺序求值，最后一条命中者决定结果（与 go-git 一致）。
//
// 进程契约：argv 任一路径命中（应拦截）→ exit 0 且 stdout 输出 "DENY"；
// 不命中 → exit 0 无输出。sh 据此「py 成功且有 stdout」判定 deny。
func BuildPyEngine(rawPatterns []string) string {
	patterns := sanitizePatterns(rawPatterns)
	var b strings.Builder
	b.WriteString(pythonHead())
	b.WriteString("# --- 内嵌的 .readignore 规则（Generate 时冻结）。")
	b.WriteString("PATTERNS 必须先于 RULES 定义，RULES 在 import 时立即编译各 pattern。---\n")
	fmt.Fprintf(&b, "PATTERNS = %s\n", pythonListLiteral(patterns))
	b.WriteString("RULES = [Rule(p) for p in PATTERNS]\n\n")
	b.WriteString(pythonTail())
	return b.String()
}

// sanitizePatterns 规整待内嵌的 patterns：去空白/注释行，保留取反行（顺序敏感）。
// Generate 内嵌前做一遍清洗，避免把空行/注释写进 python 字面量。
func sanitizePatterns(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// pythonHead 是 readignore.py 的前半段：模块 docstring、import、_glob_to_regex、Rule 类。
// 它定义 Rule 类但不构造 RULES（因为 RULES 引用 PATTERNS，而 PATTERNS 由 Generate 注入
// 在 head 与 tail 之间）。head 必须能独立通过 python 语法检查（被 import 时执行到 RULES 之前）。
func pythonHead() string {
	return `#!/usr/bin/env python3
"""readignore 匹配引擎（由 readignore CLI 生成，请勿手改）。

实现 gitignore 语义的子集：* / ** / ? / ! 取反 / 目录尾斜杠 / basename 锚定。
只用标准库（re），零第三方依赖。判定契约：
  - argv 任一路径命中（应拦截）→ exit 0 且 stdout 输出 "DENY"；
  - 不命中 → exit 0 无输出（sh 据此放行）。

PATTERNS 在文件生成时已内嵌（见下方），运行时不读盘。
"""
import re
import sys


def _glob_to_regex(glob):
    """把单条 gitignore glob（已剥离前导 ! 与尾 /）编译成 (regex, anchored_to_root)。

    返回值：
      - regex             : 已编译的 re.Pattern。
      - anchored_to_root  : 是否锚定到仓库根（含内部 / 或前导 / 时为 True）。

    锚定规则（与 go-git/gitignore 对齐，I-1/I-2 修复）：
      * 含「内部斜杠」（首字符之外某处的 /）→ anchored_to_root=True，
        正则只以 ^ 开头，不允许匹配路径中间（foo/bar 不应匹配 sub/foo/bar）。
      * 前导 /  → 去掉前导 /，标记 anchored_to_root=True，正则以 ^ 开头
        （/leading 应匹配根 leading，不匹配 sub/leading）。
      * **/ 前缀 → 任意层级目录前缀（含零层），不算根锚定（仍可任意层级匹配）。
      * 无 /    → basename 模式，等价 **/<glob>，匹配任意层级下的同名条目。

    其它通配：
      * ** : 跨任意层级（含零层）；*  : 单层内任意非 / 字符；?  : 单个非 / 字符；
      * [...]  : 字符类（M-1），透传为正则字符类；不闭合的 [ 当字面。
    """
    # 先判定根锚定：前导 / 或去掉前导 / 后仍含内部 /。
    anchored_to_root = False
    if glob.startswith("/"):
        anchored_to_root = True
        glob = glob[1:]
    elif "/" in glob:
        # 含内部斜杠即根锚定（basename 模式由下方 **/ 分支处理，不会到这里）。
        anchored_to_root = True

    # basename 锚定的模式（去掉前导 / 后若仍无 /）等价于 **/<glob>。
    # 这一步必须在根锚定判定之后：**/ 前缀不算根锚定（仍允许任意层级匹配）。
    if not anchored_to_root and not glob.startswith("**/"):
        glob = "**/" + glob

    i = 0
    n = len(glob)
    out = []
    while i < n:
        c = glob[i]
        if c == "*":
            # 先看是否 **（含 **/  /  /** 形式）。
            if i + 1 < n and glob[i + 1] == "*":
                # **/  → 任意层级目录前缀（含零层）。
                if i + 2 < n and glob[i + 2] == "/":
                    out.append(r"(?:.*/)?")
                    i += 3
                    continue
                # /**（结尾）或 /**/ 形式由前后 / 控制；这里已处理 **/，剩余 ** 作跨层。
                out.append(r".*")
                i += 2
                continue
            # 单 *：单层内任意非 / 字符。
            out.append(r"[^/]*")
            i += 1
            continue
        if c == "?":
            out.append(r"[^/]")
            i += 1
            continue
        if c == "[":
            # M-1：字符类 [...]。寻找配对的 ]（[ 后可有可选 ^ 与首字符 ]）。
            j = i + 1
            negate = False
            if j < n and glob[j] == "!":
                # gitignore 用 [!abc] 取反（与正则 [^abc] 一致）。
                negate = True
                j += 1
            elif j < n and glob[j] == "^":
                # 容错：少数用户写 [^abc]，按正则习惯当取反。
                negate = True
                j += 1
            # ] 紧跟在 [ / [^ 之后视为字面 ]（POSIX 规则）。
            if j < n and glob[j] == "]":
                j += 1
            close = glob.find("]", j)
            if close == -1:
                # 未闭合的 [ 当字面 [ 处理，避免破坏正则。
                out.append(re.escape("["))
                i += 1
                continue
            body = glob[i + 1:close]
            # gitignore 字符类语法简单：列字符即可，区间用 -，[!abc]/[^abc] 取反。
            # 直接把体内文本透传为正则字符类（与 go-git 行为一致）。
            cls = ("^" if negate else "") + body
            out.append("[" + cls + "]")
            i = close + 1
            continue
        # 其它字符按 re 字面转义。
        out.append(re.escape(c))
        i += 1
    pattern = "".join(out)
    if anchored_to_root:
        # 根锚定：只允许 ^ 开头（不允许中间 / 匹配）。
        full = r"^" + pattern + r"(?:/|$)"
    else:
        # basename / **/ 前缀：允许在任意层级匹配（含路径中间）。
        full = r"(?:^|/)" + pattern + r"(?:/|$)"
    return re.compile(full), anchored_to_root


class Rule:
    """一条解析后的规则：是否取反 + 编译后的正则。"""

    __slots__ = ("raw", "negated", "regex")

    def __init__(self, raw):
        self.raw = raw
        self.negated = raw.startswith("!")
        pat = raw[1:] if self.negated else raw
        # 仅目录模式：去尾 / 后，候选路径补尾 / 仍能命中（regex 末尾 (?:/|$) 已兼容）。
        body = pat.rstrip("/") if pat.endswith("/") else pat
        regex, _ = _glob_to_regex(body)
        self.regex = regex


`
}

// pythonTail 是 readignore.py 的后半段：matches / main / __main__ 入口。
// 由 BuildPyEngine 在 PATTERNS/RULES 注入之后拼接。引用模块级 RULES。
func pythonTail() string {
	return `
def matches(path):
    """判定单条相对路径是否应被拦截。

    路径规范化：Windows 反斜杠 → /，去掉前导 ./。无 / 分隔时按 basename 判定。
    取反语义：按文件顺序扫描，最后一条命中规则决定结果（与 go-git 一致）。
    """
    if path is None:
        return False
    p = path.replace("\\", "/").lstrip("/")
    # 去掉前导 ./（用户/工具常写 ./foo）。
    while p.startswith("./"):
        p = p[2:]
    if not p:
        return False
    excluded = False
    for rule in RULES:
        if rule.regex.search(p):
            excluded = not rule.negated
    return excluded


def matches_command(command):
    """判定一条 shell 命令是否引用了 .readignore 命中的路径。

    Bash 命令里文件名以空白/shell 元字符分隔，故按 [:\s|;<>&"'` + "`" + `] 切片，
    对每个非空 token 调用 matches。覆盖：cat .env / cat ./.env.production /
    grep foo secret.pem / scp sub/id_rsa host:/ 等。
    """
    if command is None:
        return False
    # 用一组常见分隔符切（保守：不破坏文件名里的 . _ - +）。Windows 路径 \ 留给 matches 规范化。
    # 直接用模块顶已 import 的 re，无需局部再 import（M-3 清理）。
    tokens = re.split(r"[\s|;<>&\"'` + "`" + `()]+", command)
    for tok in tokens:
        if not tok:
            continue
        # 不主动跳过命令名/flag：matches 只在 token 真匹配某条 pattern 时才返回 True，
        # 故 cat / grep / -f 这类无关 token 自然落空，不会误伤。
        if matches(tok):
            return True
    return False


def main(argv):
    if len(argv) < 2:
        return 0
    # 默认每个 argv 视为路径；--command 标志把后续整体当作 shell 命令做 token 扫描。
    if argv[1] == "--command" and len(argv) >= 3:
        if matches_command(argv[2]):
            sys.stdout.write("DENY")
            sys.stdout.flush()
            return 0
        return 0
    for candidate in argv[1:]:
        if matches(candidate):
            sys.stdout.write("DENY")
            sys.stdout.flush()
            return 0
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
`
}

// pythonListLiteral 把字符串切片渲染成合法、安全的 Python 字面量
// （用 repr 风格双引号，逐字符转义；确保 patterns 含引号/反斜杠时不破坏 python 语法）。
func pythonListLiteral(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(pythonRepr(s))
	}
	b.WriteByte(']')
	return b.String()
}

// pythonRepr 渲染单个字符串为 Python 双引号字面量，转义反斜杠、双引号、换行、
// 及所有控制字符（NUL/VT/FF/DEL 等），保证含特殊字符的 pattern 不会破坏生成的
// python 语法（M-2：控制字符直出会让 .py 文件 SyntaxError）。
func pythonRepr(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			// 控制字符（C0 控制码 0x00-0x1F 与 DEL 0x7F）必须以 \xNN 转义，
			// 否则生成的 .py 文件含裸控制字符会触发 SyntaxError。
			// 0x7f (DEL) 是控制字符但不算 printable，单独转义。
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
