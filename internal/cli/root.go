// Package cli 实现 readignore 的命令行界面（基于 spf13/cobra）。
//
// 本包是阶段5 的交付入口，把前四阶段的能力串联成用户可执行的命令：
//
//   - readignore init              在当前目录生成 .readignore 模板；
//   - readignore adapters          列出已注册适配器及其强度、检测状态；
//   - readignore generate <id>     解析 .readignore 并打印适配器产物（dry-run）；
//   - readignore install <id>      把适配器产物写到磁盘；
//   - readignore check             校验 .readignore 语法并报告各适配器安装状态。
//   - readignore update <id>       更新已装适配器产物（覆盖刷新到当前版本）。
//   - readignore uninstall <id>    移除适配器产物（install 的逆操作）。
//
// 适配器（claudecode / opencode / ……）通过自身 init() 自登记进 adapter registry，
// 本包用 blank import 触发这些 init()，使 adapter.All() 能发现全部适配器：
//
//	import (
//	    _ "github.com/0xByteBard404/readignore/internal/adapter/claudecode"
//	    _ "github.com/0xByteBard404/readignore/internal/adapter/codex"
//	    _ "github.com/0xByteBard404/readignore/internal/adapter/opencode"
//	)
//
// 设计原则：
//   - 本包不含业务逻辑，只做参数解析、文件 IO 与用户提示；
//   - 跨平台路径一律用 filepath，规则文本透传给 readignore.Parse / 适配器；
//   - 错误信息面向用户（友好 + 可操作），不暴露内部 panic 栈。
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	// Blank import 触发各适配器 init() 自登记进 adapter registry，
	// 使 adapter.All() / adapter.Get() 能发现它们。顺序不影响 registry 行为
	// （registry 按注册先后返回，但具体顺序对 CLI 列表展示无功能影响）。
	_ "github.com/0xByteBard404/readignore/internal/adapter/claudecode"
	_ "github.com/0xByteBard404/readignore/internal/adapter/codex"
	_ "github.com/0xByteBard404/readignore/internal/adapter/kilocode"
	_ "github.com/0xByteBard404/readignore/internal/adapter/opencode"
	_ "github.com/0xByteBard404/readignore/internal/adapter/pi"
)

// Version 是 readignore 的版本字符串，构建时可通过
//
//	go build -ldflags "-X github.com/0xByteBard404/readignore/internal/cli.Version=1.0.0"
//
// 注入；未注入时为 "dev"，便于从源码直接 go run / go build 使用。
var Version = "dev"

// readignoreFileName 是 readignore 在仓库根读取的规则文件名。
// 集中成常量，便于 init/check/generate/install 共享且不被拼写漂移。
const readignoreFileName = ".readignore"

// errVersionPrinted 是 --version 已打印版本后的哨兵错误。
// RunE 返回它时，Execute 把它翻译成正常退出（exit 0），而非错误退出。
// 用哨兵而非 os.Exit 是为了保持可测试性（测试驱动 newRootCmd 不会真杀进程）。
var errVersionPrinted = fmt.Errorf("__readignore_version_printed__")

// newRootCmd 构造 readignore 根命令及其全部子命令。
//
// 拆成工厂函数（而非包级 var）便于测试：测试可拿到一个全新 *cobra.Command，
// 用 SetArgs/SetOut 驱动而不污染全局状态。生产入口 Execute() 内部调用本函数。
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "readignore",
		Short: "把 .readignore 适配成各 AI coding agent 的防护配置",
		Long: `readignore 把仓库根的 .readignore（gitignore 语法）翻译成各 AI coding agent
（Claude Code、opencode 等）的原生防护配置。

典型流程：
  readignore init                 生成 .readignore 模板
  readignore adapters             查看支持的适配器与检测状态
  readignore generate claude-code 预览生成产物（dry-run，打印到 stdout）
  readignore install claude-code  把产物写到磁盘
  readignore check                校验 .readignore 并报告各适配器安装状态

仓库根默认取当前工作目录。规则文件名固定为 .readignore。`,
		// 子命令出错时不打印 usage（错误信息已足够清晰）；同时 SilenceErrors
		// 让 cobra 不自动打印 error（--version 走哨兵错误，由 cli.Execute 统一处理；
		// 真实命令错误也由 cli.Execute 打印，保持输出格式一致）。
		SilenceUsage:  true,
		SilenceErrors: true,
		// PersistentPreRunE：每个子命令（及根）执行前触发 update-check。
		// 与下面"--version 走 RunE 而非 PersistentPreRun"不冲突：
		// --version 避开 PersistentPreRun 是因为"打印版本后立即退出"语义错位；
		// update-check 走 PersistentPreRun 语义正确（每命令前查，且按 cmd.Name()
		// 排除 match/hook-check/update）。两条注释各管各的用途。
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// 用 cmd.ErrOrStderr() 而非裸 os.Stderr：生产等价（未 SetErr 时返回
			// os.Stderr），但尊重 cobra 输出重定向——测试里 runCmd 的 SetErr(buf)
			// 才能捕获提示。裸 os.Stderr 会绕过 buf 导致集成测试断言失败。
			Check(cmd, cmd.ErrOrStderr())
			return nil // Check 内部静默，绝不返回错误阻断主命令
		},
		// 版本 flag：--version / -v 直接打印版本退出。放在 root.RunE 而非
		// PersistentPreRun，是因为后者在子命令存在时也会触发，语义错位；
		// 而 root 自带 RunE 后，`readignore --version`（无子命令）才会走到这里。
		// （注：PersistentPreRunE 仍用于 update-check，见上方——那是不同用途。）
		RunE: func(cmd *cobra.Command, args []string) error {
			if v, _ := cmd.Flags().GetBool("version"); v {
				fmt.Fprintf(cmd.OutOrStdout(), "readignore %s\n", Version)
				return errVersionPrinted
			}
			// 无子命令、无 --version：打印 help（cobra 默认行为是 help，但显式更稳）。
			return cmd.Help()
		},
	}

	// 版本 flag（短 -v）。必须在 RunE 之外注册，否则 help/补全看不到。
	root.Flags().BoolP("version", "v", false, "打印 readignore 版本并退出")

	root.AddCommand(newInitCmd())
	root.AddCommand(newAdaptersCmd())
	root.AddCommand(newGenerateCmd())
	root.AddCommand(newInstallCmd())
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newHookCheckCmd())
	root.AddCommand(newCheckCmd())
	root.AddCommand(newMatchCmd())
	root.AddCommand(newUninstallCmd())

	return root
}

// Execute 是 readignore CLI 的生产入口：构造根命令、执行、处理错误。
// 供 cmd/readignore/main.go 直接调用。
//
// 行为：
//   - --version 打印版本后 exit 0；
//   - 正常命令失败 exit 1 且打印友好错误（不打印 usage）；
//   - 无子命令时打印 help（cobra 默认行为）。
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		// --version 哨兵：版本已打印，正常退出。
		if err == errVersionPrinted {
			return
		}
		fmt.Fprintf(os.Stderr, "readignore: %v\n", err)
		os.Exit(1)
	}
}

// resolveRepoRoot 返回仓库根路径。当前实现取当前工作目录。
//
// 抽成独立函数便于：未来若需支持「向上查找含 .git 的目录」可只改此处；
// 测试可注入不同 cwd 而不耦合子命令实现。
//
// 失败时返回 error（os.Getwd 极少失败，但仍如实传递）。
func resolveRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("无法确定当前工作目录: %w", err)
	}
	return cwd, nil
}

// writeOut 把 s 写入 w，是对 cmd.OutOrStdout() 的小封装，便于 mock 测试。
// 不做错误处理：stdout 写失败通常意味着管道关闭，无恢复余地。
func writeOut(w io.Writer, s string) {
	_, _ = io.WriteString(w, s)
}
