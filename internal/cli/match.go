package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xByteBard404/readignore/internal/readignore"
)

// errDenied 是 match 命中 .readignore 时返回的哨兵错误。
//
// RunE 返回它 → Execute() 把它翻译成 exit 1（deny）。
// 用哨兵而非直接 os.Exit(1) 是为了保持可测试性：测试通过 runCmd 拿到 error
// 即可断言 deny，无需 fork 子进程或劫持 os.Exit。
//
// 文案固定为 "denied by .readignore"，stderr 输出对用户/agent 可读。
var errDenied = errors.New("denied by .readignore")

// newMatchCmd 构造 `readignore match <path>` 子命令：判断 path 是否被
// 当前工作目录下的 .readignore 命中。
//
// 退出码语义（供 hook 等外部脚本消费）：
//   - exit 0 = allow：未命中，或 .readignore 不存在 / Parse 失败（fallback 放行，不拦）；
//   - exit 1 = deny：命中某条 .readignore 规则（取反规则可放行，由 go-git matcher 保证）。
//
// 设计选择：fallback 放行而非拦截。理由是 readignore 是「加强防护」而非「必要依赖」——
// 没配 .readignore 或规则解析失败时，不应因此阻断 AI agent 的正常读取，宁可放行
// 也不可因自身故障把用户锁死。
//
// 匹配权威：internal/readignore（go-git format/gitignore），不重新实现匹配，
// 保证与 git 行为一致（取反、** 任意层级、目录锚定）。
func newMatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "match <path>",
		Short: "判断 path 是否被当前目录的 .readignore 命中（exit 0=allow/1=deny）",
		Long: `判断给定的相对路径是否被当前工作目录下的 .readignore 命中。

匹配语义遵循 gitignore 规则（委托 internal/readignore，go-git 权威 matcher）：
  - 命中 → exit 1（deny）；
  - 未命中 / 取反放行 → exit 0（allow）；
  - .readignore 不存在或解析失败 → exit 0（fallback 放行，不拦）。

典型用于 AI coding agent 的 read hook：在读取文件前问一句是否被 readignore 拦截。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMatch(args[0])
		},
	}
}

// runMatch 是 match 命令的核心实现，独立于 cobra 便于测试。
//
// 流程：
//  1. 读 cwd/.readignore；不存在或读失败 → 直接返回 nil（fallback allow）；
//  2. readignore.Parse；失败 → stderr 警告 + 返回 nil（容错 allow，不拦）；
//  3. Matcher.Matches(path)；命中 → 返回 errDenied（→ exit 1），否则 nil（exit 0）。
func runMatch(path string) error {
	cwd, err := os.Getwd()
	if err != nil {
		// 连 cwd 都拿不到属于环境异常，按 fallback 放行（不拦），
		// 与 .readignore 缺失一致策略。
		return nil
	}
	data, err := os.ReadFile(filepath.Join(cwd, readignoreFileName))
	if err != nil {
		// 无 .readignore 或读失败 → fallback 放行（不拦）。
		return nil
	}
	m, err := readignore.Parse(string(data))
	if err != nil {
		// 容错：Parse 失败不拦，stderr 警告便于用户排查。
		fmt.Fprintf(os.Stderr, "readignore match: %v\n", err)
		return nil
	}
	if m.Matches(path) {
		return errDenied // → Execute() exit 1
	}
	return nil // exit 0（allow）
}
