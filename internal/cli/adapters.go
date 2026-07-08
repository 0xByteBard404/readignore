package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

// newAdaptersCmd 构造 `readignore adapters` 子命令：列出全部已注册适配器。
//
// 输出表格列：ID | 强度（hard/config/soft）| 当前仓库检测状态（yes/no）。
// 不依赖 .readignore：哪怕仓库里没有规则文件，也能查看支持哪些适配器。
func newAdaptersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adapters",
		Short: "列出已注册的适配器及其强度、检测状态",
		Long: `列出全部已注册适配器（claude-code、opencode 等）的 ID、拦截强度与当前目录的检测状态。

强度含义：
  hard   执行前可编程拦截（最强，运行时阻断）
  config 生成原生 deny 配置（中，工具加载时生效）
  soft   仅自然语言规则（最弱，依赖模型自觉）

本命令不依赖 .readignore，任何目录都能查看支持的适配器。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 把取列表抽成实参注入，使「registry 为空 = 构建错误」分支可单测
			// （全局 registry 在包级 blank import 后恒非空，无法用真实 registry 触发空分支）。
			return runAdapters(cmd.OutOrStdout(), adapter.All())
		},
	}
	return cmd
}

// runAdapters 是 adapters 命令的核心实现，独立于 cobra 便于测试。
// all 为当前 registry 快照，由调用方传入（生产代码传 adapter.All()）。
func runAdapters(out io.Writer, all []adapter.Adapter) error {
	if len(all) == 0 {
		// registry 为空 = 构建问题（blank import 未触发各适配器 init() 自登记）。
		// 这不是正常状态，应让 CLI exit 非 0，而不是伪装成「无适配器也算成功」。
		return fmt.Errorf("未发现任何已注册适配器——这通常是构建问题（blank import 未生效）")
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		// 取不到 cwd 不致命：检测状态列统一标 "n/a"。
		repoRoot = ""
	}

	// tabwriter 对齐表格列；minwidth=0、tabwidth=2、padding=2 兼顾紧凑与可读。
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTRENGTH\tDETECTED")
	for _, a := range all {
		detected := "no"
		if repoRoot != "" && a.Detect(repoRoot) {
			detected = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", a.ID(), a.Strength(), detected)
	}
	return tw.Flush()
}
