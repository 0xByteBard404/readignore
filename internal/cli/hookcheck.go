package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/0xByteBard404/readignore/internal/readignore"
)

// hookCheckDenyJSON 是 PreToolUse deny 决策的固定输出（与 hookengine denyJSON 一致）。
const hookCheckDenyJSON = `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"readignore: path declared in .readignore"}}` + "\n"

// newHookCheckCmd 构造 `readignore hook-check`（内部命令，由 PreToolUse 钩子调用）。
//
// 从 stdin 读 Claude-style PreToolUse 的 tool_input JSON，用 encoding/json 健壮解析
// （正确处理多行 command、转义双引号等所有 JSON 边界），抽取目标路径/命令，对每个
// 路径式 token 调 readignore 匹配；命中即输出 deny JSON。
//
// 相比旧 bash 钩子（grep 抽取，对多行/转义脆弱），本命令把 JSON 解析与匹配全收敛到 Go，
// 消除 bash grep 的绕过面（多行 command、\" 转义截断等）。
func newHookCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "hook-check",
		Short:  "internal: PreToolUse hook（stdin 读 tool_input，命中 .readignore 输出 deny）",
		Hidden: true, // 内部命令，不在公开 help 列表
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHookCheck(cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}

// toolInputSchema 是 Claude-style PreToolUse 传入的 JSON 结构。
// ToolInput 用 map[string]string：file_path/path/pattern/command 都是字符串字段。
type toolInputSchema struct {
	ToolName  string            `json:"tool_name"`
	ToolInput map[string]string `json:"tool_input"`
}

// runHookCheck 是 hook-check 的核心实现，独立于 cobra 便于测试。
//
// 流程：
//  1. 读 stdin（tool_input JSON）；预处理实际换行→空格（兜底 Claude Code 非规范 JSON）；
//  2. encoding/json 解析（健壮，多行/转义/所有 JSON 边界）；
//  3. 读 cwd/.readignore + Parse；不存在/失败 → 放行（fallback，不搞死）；
//  4. file_path/path/pattern 任一 Matches → deny；
//  5. command 切 token，路径式 token Matches → deny；
//  6. 无命中 → 静默 exit 0（放行）。
func runHookCheck(in io.Reader, out io.Writer) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return nil // 读失败 → 放行
	}

	// 预处理：实际换行（\n \r）→ 空格。Claude Code 传多行 Bash command 时，
	// JSON 可能含实际换行（非规范）或 \n 转义。统一把实际换行→空格——不影响
	// JSON 结构（空白不敏感），字符串内换行→空格也不影响后续 token 切分（空格
	// 和换行都是命令分隔符）。这让 json.Unmarshal 能健壮解析两种格式。
	normalized := strings.ReplaceAll(string(data), "\n", " ")
	normalized = strings.ReplaceAll(normalized, "\r", " ")

	var input toolInputSchema
	if err := json.Unmarshal([]byte(normalized), &input); err != nil {
		return nil // JSON 解析失败 → 放行（不搞死）
	}

	// 读 cwd/.readignore + Parse（一次）。
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(cwd, readignoreFileName))
	if err != nil {
		return nil // 无 .readignore → 放行
	}
	sections, err := readignore.ParseSections(string(raw))
	if err != nil {
		fmt.Fprintf(os.Stderr, "readignore hook-check: %v\n", err)
		return nil // Parse 失败 → 放行
	}

	// 按 tool_name 路由到对应段（read/edit/delete），各段独立匹配。
	// Matches 内部已规范化 Windows 反斜杠 → 正斜杠（parser.go），直接传路径即可。
	op, paths := routeToolInput(input.ToolName, input.ToolInput)
	switch op {
	case readignore.OpRead:
		if matchAny(sections.Read, paths) {
			writeOut(out, hookCheckDenyJSON)
			return nil
		}
	case readignore.OpEdit:
		if matchAny(sections.Edit, paths) {
			writeOut(out, hookCheckDenyJSON)
			return nil
		}
	case readignore.OpDelete:
		if matchAny(sections.Delete, paths) {
			writeOut(out, hookCheckDenyJSON)
			return nil
		}
	case "unknown":
		// 未知工具/命令：保守查 Read ∪ Delete（最致命两类）。
		if matchAny(sections.Read, paths) || matchAny(sections.Delete, paths) {
			writeOut(out, hookCheckDenyJSON)
			return nil
		}
	}

	return nil // 放行
}

// routeToolInput 按 tool_name 把请求映射到 (op, paths)。
//   - Read/Grep/Glob → OpRead，取 file_path/path/pattern
//   - Edit/Write → OpEdit，取 file_path
//   - NotebookEdit → OpEdit，取 notebook_path（非 file_path —— Claude API 字段名）
//   - Bash → classifyCommand(command) 按 verb 分类
//   - 其余 → "unknown" + nil（保守放行，由调用方对未知工具查 Read∪Delete）
func routeToolInput(toolName string, ti map[string]string) (readignore.Op, []string) {
	switch toolName {
	case "Read", "Grep", "Glob":
		return readignore.OpRead, fields(ti, "file_path", "path", "pattern")
	case "Edit", "Write":
		return readignore.OpEdit, fields(ti, "file_path")
	case "NotebookEdit":
		return readignore.OpEdit, fields(ti, "notebook_path") // C2: notebook_path 非 file_path
	case "Bash":
		return classifyCommand(ti["command"])
	}
	return "unknown", nil
}

// fields 从 tool_input 取非空字段值（保留传入 key 顺序）。
func fields(ti map[string]string, keys ...string) []string {
	var out []string
	for _, k := range keys {
		if v := ti[k]; v != "" {
			out = append(out, v)
		}
	}
	return out
}

// classifyCommand 把 Bash command 按 verb 分类到 (op, paths)。
//   - rm/rmdir/unlink → OpDelete + parseDeletePaths（精确跳选项）
//   - cat/head/tail/less/more/grep/rg/awk/cut → OpRead + 路径式 token
//   - sed → -i 改文件 = OpEdit，否则 OpRead
//   - tee/dd/cp/mv/truncate → OpEdit
//   - 含 '>' 重定向 → OpEdit
//   - 其余 → "unknown" + 路径式 token（保守查 Read∪Delete）
//
// 注意：Bash 静态分析有固有天花板（变量展开 $F、间接路径 ln -s），无法 100%；
// 这里拦的是所有「字面路径式 token」，覆盖 cat .env / grep foo secret.pem 等。
func classifyCommand(command string) (readignore.Op, []string) {
	toks := tokenizeCommand(command)
	if len(toks) == 0 {
		return "unknown", nil
	}
	verb := toks[0]
	// 路径式 token（沿用 looksLikePath）。
	var pathToks []string
	for _, t := range toks[1:] {
		if looksLikePath(t) {
			pathToks = append(pathToks, t)
		}
	}
	switch verb {
	case "rm", "rmdir", "unlink":
		return readignore.OpDelete, parseDeletePaths(command)
	case "cat", "head", "tail", "less", "more", "grep", "rg", "awk", "cut":
		return readignore.OpRead, pathToks
	case "sed":
		// sed -i 改文件 → edit；无 -i 只输出 → read。
		if strings.Contains(command, " -i") {
			return readignore.OpEdit, pathToks
		}
		return readignore.OpRead, pathToks
	case "tee", "dd", "cp", "mv", "truncate":
		return readignore.OpEdit, pathToks
	}
	// 含重定向 > / >> → edit。
	if strings.Contains(command, ">") {
		return readignore.OpEdit, pathToks
	}
	return "unknown", pathToks
}

// parseDeletePaths 从 rm/rmdir/unlink 命令提取路径参数。
// 跳过 - 开头的选项；-- 之后皆文件（即便形如 -weird）。不展开变量/$()（静态分析天花板）。
func parseDeletePaths(command string) []string {
	toks := tokenizeCommand(command)
	var paths []string
	afterDoubleDash := false
	for _, t := range toks[1:] { // 跳过 verb
		if afterDoubleDash {
			paths = append(paths, t)
			continue
		}
		if t == "--" {
			afterDoubleDash = true
			continue
		}
		if strings.HasPrefix(t, "-") {
			continue // 选项
		}
		paths = append(paths, t)
	}
	return paths
}

// matchAny 判断 paths 中是否有任一被 m 命中（命中即 deny）。
//
// 尾斜杠兜底：gitignore 的目录模式（如 "src/"）是 dirOnly，不命中无尾斜杠的裸 token
// （rm -rf src 的 "src"）。补一次 p+"/" 形式，让目录模式对裸 token 也生效。
// Matches 对 nil/空 matcher 返回 false（parser.go），空段安全。
func matchAny(m *readignore.Matcher, paths []string) bool {
	for _, p := range paths {
		if m.Matches(p) {
			return true
		}
		if !strings.HasSuffix(p, "/") && m.Matches(p+"/") {
			return true
		}
	}
	return false
}

// tokenizeCommand 把 shell 命令切成 token。按空白 + 常见 shell 元符切（保守，
// 不破坏文件名里的 . _ - +），对应旧 bash 钩子的 tr ' \t|;<>&"...' '\n'。
func tokenizeCommand(cmd string) []string {
	const sep = " \t\n\r|;<>&\"'(){}"
	return strings.FieldsFunc(cmd, func(r rune) bool {
		return strings.ContainsRune(sep, r)
	})
}

// looksLikePath 判断 token 是否"像文件路径"（对应旧 bash 钩子的 looks_like_path）。
// 跳过命令名/选项/字符串值，只对路径式 token match，避免 git config 等合法命令误报。
func looksLikePath(tok string) bool {
	switch {
	case strings.HasPrefix(tok, "-"): // 选项（--global / -rf）
		return false
	case strings.HasPrefix(tok, "~"): // ~ 或 ~/... home 路径
		return true
	case strings.Contains(tok, "/"): // 含路径分隔
		return true
	case strings.Contains(tok, "."): // 含点（dotfile / 扩展名）
		return true
	}
	// 磁盘存在兜底（无点无 / 的真实文件名，如 README）。
	if _, err := os.Stat(tok); err == nil {
		return true
	}
	return false
}
