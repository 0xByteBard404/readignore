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
	m, err := readignore.Parse(string(raw))
	if err != nil {
		fmt.Fprintf(os.Stderr, "readignore hook-check: %v\n", err)
		return nil // Parse 失败 → 放行
	}

	// 路径字段（Read/Grep/Glob）：直接 match，精确无绕过。
	for _, field := range []string{"file_path", "path", "pattern"} {
		if v := input.ToolInput[field]; v != "" {
			// filepath.ToSlash 规范化 Windows 反斜杠 → 正斜杠（跨平台匹配，
			// go-git matcher 按 / 分段；否则 sub\id_rsa 被当单段，漏判 **/id_rsa）。
			if m.Matches(filepath.ToSlash(v)) {
				writeOut(out, hookCheckDenyJSON)
				return nil
			}
		}
	}

	// command（Bash）：切 token，只对路径式 token match。
	// 注意：Bash 静态分析有固有天花板（变量展开 $F、间接路径 ln -s），无法 100%；
	// 这里拦的是所有「字面路径式 token」，覆盖 cat .env / grep foo secret.pem 等。
	if cmd := input.ToolInput["command"]; cmd != "" {
		for _, tok := range tokenizeCommand(cmd) {
			if looksLikePath(tok) && m.Matches(tok) {
				writeOut(out, hookCheckDenyJSON)
				return nil
			}
		}
	}

	return nil // 放行
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
