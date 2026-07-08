// Package claudecode 实现 Claude Code 适配器：把 .readignore 翻译成 Claude Code
// 的 PreToolUse hook，实现「执行前可编程硬拦」—— 五个目标工具里唯一能在工具真正
// 执行前用脚本判定并阻断的，故本包是 readignore 的参考实现。
//
// 产物三件套（Generate 返回，由调用方/安装层写入磁盘）：
//   - .claude/hooks/readignore.sh  (0755)  从 tool_input JSON 抽取目标路径/命令，
//     交 readignore.py 判定，命中即输出 PreToolUse deny JSON；
//   - .claude/hooks/readignore.py  (0644)  匹配引擎：用标准库实现 gitignore 语义
//     (*、**、!、目录尾斜杠)，零第三方依赖；规则在 Generate 时内嵌；
//   - .claude/settings.json        (0)     PreToolUse 注册片段（与既有 settings.json
//     的合并留给阶段5 CLI install 层，本适配器只 Generate 片段）。
//
// init() 调 adapter.Register 自登记，CLI 通过 adapter.Get("claude-code") 发现本适配器。
package claudecode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

// Adapter 实现 [adapter.Adapter]，把 .readignore 翻译成 Claude Code PreToolUse hook。
//
// 零字段、无状态：所有产物在 Generate 时根据 plan 即时生成，便于测试与并发安全。
type Adapter struct{}

// 编译期保证 Adapter 满足接口契约；缺失方法在编译时即报错，而非运行时。
var _ adapter.Adapter = Adapter{}

// init 把本适配器登记进全局 registry，使 adapter.All()/Get() 可发现。
// 放在包 init（而非显式调用）符合「具体适配器自登记」的设计约定。
func init() {
	adapter.Register(Adapter{})
}

// ID 返回稳定短标识 "claude-code"，用作 CLI 参数、配置键与 registry 索引。
// 全小写、无空格、跨版本不变。
func (Adapter) ID() string { return "claude-code" }

// Strength 返回 [adapter.StrengthHard]：Claude Code PreToolUse hook 在工具真正
// 执行前由 bash/python 判定并阻断，是当前支持的最强拦截强度。
func (Adapter) Strength() adapter.Strength { return adapter.StrengthHard }

// Detect 探测 repoRoot 下是否已存在 Claude Code 痕迹：.claude/ 目录或 CLAUDE.md。
// 命中仅影响 CLI 是否默认启用本适配器；Generate 即便未检测到也能产出可手动安装的文件。
func (Adapter) Detect(repoRoot string) bool {
	if repoRoot == "" {
		return false
	}
	if fi, err := os.Stat(filepath.Join(repoRoot, ".claude")); err == nil && fi.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "CLAUDE.md")); err == nil {
		return true
	}
	return false
}

// InstallInstructions 给出「如何让 Claude Code 读取所生成文件」的人类可读说明。
// Claude Code 的 settings watcher 实时加载 .claude/ 下变更，故无需重启。
func (Adapter) InstallInstructions() string {
	return "已写入 .claude/。Claude Code settings watcher 实时加载，无需重启。"
}

// Generate 依据 plan 产出三个文件（sh / py / settings.json）。
//
// 关键设计：
//   - patterns 在此刻以合法 Python 字面量内嵌进 readignore.py（generate 时即冻结），
//     运行时不再读盘，避免 .readignore 缺失/漂移导致 hook 行为不确定；
//   - sh 仅做 JSON 字段抽取（grep，无 jq 依赖），匹配判定全在 py 里，便于跨平台
//     （sh 里只调 python，不在 bash 里重写匹配逻辑）；
//   - settings.json 只 Generate PreToolUse 片段，与既有 settings 的合并由 CLI 完成。
func (Adapter) Generate(plan adapter.Plan) ([]adapter.GeneratedFile, error) {
	patterns := sanitizePatterns(plan.RawPatterns)
	return []adapter.GeneratedFile{
		{
			Path:    ".claude/hooks/readignore.sh",
			Mode:    0o755,
			Content: hookShellScript(),
		},
		{
			Path:    ".claude/hooks/readignore.py",
			Mode:    0o644,
			Content: hookPythonEngine(patterns),
		},
		{
			Path:    ".claude/settings.json",
			Mode:    0,
			Content: settingsJSON(),
		},
	}, nil
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

// hookShellScript 返回 readignore.sh 全文。
// 设计要点（借鉴已验证的 block_dotenv.sh，但泛化为「把抽取的路径喂给 py」）：
//   - 无 jq 依赖：用 grep -oE 从原始 JSON 文本抽取字段值（Read→file_path、
//     Grep→path、Glob→pattern、Bash→command）；
//   - 跨平台 python：逐个试 python3/python 的 --version，第一个真正可执行的胜出
//     （Windows 上 python3 常为商店占位符，command -v 不够）；
//   - command 字段（Bash）整体当 shell 命令处理：py 按元字符切 token 扫描，
//     而非把 "cat .env" 当一个文件名；其它字段按路径判；
//   - 命中 → stdout 输出 PreToolUse deny JSON；不命中 → 静默 exit 0；
//   - 工作目录假设为仓库根（Claude Code 实际也是从仓库根发起 hook），故用相对路径调 py。
func hookShellScript() string {
	const tmpl = `#!/usr/bin/env bash
# readignore PreToolUse hook（由 readignore CLI 生成，请勿手改）。
# 从 Claude Code 传入的 tool_input JSON 中抽取目标路径/命令，喂给 readignore.py
# 判定是否命中 .readignore；命中即输出 PreToolUse deny 决策。
#
# 覆盖工具：Read | Grep | Glob | Bash（由 .claude/settings.json 的 matcher 指定）。
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
# command 字段（Bash 工具）整体当 shell 命令处理：py 按 shell 元字符切 token 扫描，
# 而非把 "cat .env" 当一整个文件名。其它字段（file_path/path/pattern）按路径判。
for field in file_path path pattern; do
  val=$(extract_field "$input" "$field")
  if [ -n "$val" ]; then
    result=$("$PY" .claude/hooks/readignore.py "$val" 2>/dev/null)
    if [ "$result" = "DENY" ]; then
      deny
    fi
  fi
done
val=$(extract_field "$input" "command")
if [ -n "$val" ]; then
  result=$("$PY" .claude/hooks/readignore.py --command "$val" 2>/dev/null)
  if [ "$result" = "DENY" ]; then
    deny
  fi
fi

# 无命中：静默放行。
exit 0
`
	return tmpl
}

// settingsJSON 返回 .claude/settings.json 片段：仅 PreToolUse 注册项。
// 与既有 settings.json 的深度合并由阶段5 CLI install 层负责，本适配器只产片段。
func settingsJSON() string {
	return `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Read|Grep|Glob|Bash",
        "hooks": [
          {
            "type": "command",
            "command": "bash .claude/hooks/readignore.sh",
            "shell": "bash",
            "timeout": 5
          }
        ]
      }
    ]
  }
}
`
}

// hookPythonEngine 返回 readignore.py 全文，把 patterns 以合法 Python 字面量内嵌。
//
// 匹配引擎方案（零第三方依赖，仅标准库 re）：
//   - 每条 pattern 编译成一条 (negated, regex) 规则；
//   - glob→regex 转换遵循 gitignore 语义：
//     * `**/`  → 任意层级目录前缀（含零层）；
//     * `/**`  → 任意层级后缀；
//     * `/**/` → 跨任意层级（含零层）的目录分隔；
//     * `*`    → 匹配单层内任意字符（不跨 /）；
//     * `?`    → 单个非 / 字符；
//     * 尾 `/` → 仅匹配目录（判定时把候选当作含尾斜杠传入即识别）；
//     * 无 `/` → basename 匹配（gitignore：无斜杠的模式匹配任意层级 basename）。
//   - 取反 (`!`)：按文件顺序求值，最后一条命中者决定结果（与 go-git 一致）。
//
// 进程契约：argv 任一路径命中（应拦截）→ exit 0 且 stdout 输出 "DENY"；
// 不命中 → exit 0 无输出。sh 据此「py 成功且有 stdout」判定 deny。
// （exit 0 兼容 PreToolUse hook 不能非零退出的预期；deny 信号走 stdout。）
func hookPythonEngine(patterns []string) string {
	var b strings.Builder
	b.WriteString(pythonHead())
	b.WriteString("# --- 内嵌的 .readignore 规则（Generate 时冻结）。")
	b.WriteString("PATTERNS 必须先于 RULES 定义，RULES 在 import 时立即编译各 pattern。---\n")
	fmt.Fprintf(&b, "PATTERNS = %s\n", pythonListLiteral(patterns))
	b.WriteString("RULES = [Rule(p) for p in PATTERNS]\n\n")
	b.WriteString(pythonTail())
	return b.String()
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
    """把单条 gitignore glob（已剥离前导 !）编译成 re。

    规则（与 go-git/gitignore 对齐）：
      * /  : 目录锚定；多段时按路径分隔逐段匹配；
      * ** : 跨任意层级（含零层）；
      * *  : 单层内任意字符（不跨 /）；
      * ?  : 单个非 / 字符；
      * 无 /: basename 模式（匹配任意层级下的同名条目）；
      * 尾 /: 仅匹配目录（调用方在候选末尾加 / 即可识别）。
    """
    # basename 锚定的模式（无 / 分隔）等价于 **/<glob>。
    has_slash = "/" in (glob[:-1] if glob.endswith("/") else glob)
    if not has_slash and not glob.startswith("/"):
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
        # 其它字符按 re 字面转义。
        out.append(re.escape(c))
        i += 1
    pattern = "".join(out)
    return re.compile(r"(?:^|/)" + pattern + r"(?:/|$)")


class Rule:
    """一条解析后的规则：是否取反 + 编译后的正则。"""

    __slots__ = ("raw", "negated", "regex")

    def __init__(self, raw):
        self.raw = raw
        self.negated = raw.startswith("!")
        pat = raw[1:] if self.negated else raw
        # 仅目录模式：去尾 / 后，候选路径补尾 / 仍能命中（regex 末尾 (?:/|$) 已兼容）。
        self.regex = _glob_to_regex(pat.rstrip("/") if pat.endswith("/") else pat)


`
}

// pythonTail 是 readignore.py 的后半段：matches / main / __main__ 入口。
// 由 hookPythonEngine 在 PATTERNS/RULES 注入之后拼接。引用模块级 RULES。
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

    Bash 命令里文件名以空白/shell 元字符分隔，故按 [:\\s|;<>&"'` + "`" + `] 切片，
    对每个非空 token 调用 matches。覆盖：cat .env / cat ./.env.production /
    grep foo secret.pem / scp sub/id_rsa host:/ 等。
    """
    if command is None:
        return False
    # 用一组常见分隔符切（保守：不破坏文件名里的 . _ - +）。Windows 路径 \\ 留给 matches 规范化。
    import re as _re
    tokens = _re.split(r"[\s|;<>&\"'` + "`" + `()]+", command)
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

// pythonRepr 渲染单个字符串为 Python 双引号字面量，转义反斜杠、双引号、换行等，
// 保证含特殊字符的 pattern 不会破坏生成的 python 语法。
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
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
