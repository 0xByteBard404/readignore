package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

// newUninstallCmd 构造 `readignore uninstall` 子命令：移除适配器产物（install 的逆操作）。
//
// 两种用法：
//   - readignore uninstall <adapter-id>   移除单个适配器的产物；
//   - readignore uninstall --all          移除所有 Detect()=true 的适配器产物。
//
// 删除策略（与 install 的「默认写」对称）：
//   - 默认真删：逐个删除 Generate() 声明的产物文件，打印清单；
//   - --dry-run：只打印将删除什么，不真删；
//   - 不存在的文件跳过（不报错）；
//   - 删后尝试清空产物所在空目录（如 .claude/hooks/），非空则静默忽略；
//   - 不删 .readignore（用户规则源，只清适配器产物）。
func newUninstallCmd() *cobra.Command {
	var all bool
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "uninstall <adapter-id>",
		Short: "移除适配器产物（install 的逆操作）",
		Long: `移除指定适配器在仓库里生成的产物文件（install 的逆操作）。

用法：
  readignore uninstall <adapter-id>   移除单个适配器产物
  readignore uninstall --all          移除所有在当前目录检测到的适配器产物

删除策略：
  默认真删（不可逆）。加 --dry-run 只预览不真删。
  不存在的文件跳过；删后顺手清空产物所在空目录。
  不删 .readignore（那是你的规则源文件）。`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(cmd.OutOrStdout(), args, all, dryRun)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "移除所有在当前目录检测到的适配器产物")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "只打印将删除什么，不真删")
	return cmd
}

// runUninstall 是 uninstall 命令的核心实现，独立于 cobra 便于测试。
func runUninstall(out io.Writer, args []string, all, dryRun bool) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}

	// --all 与显式 ID 互斥（与 install 一致）。
	if all && len(args) > 0 {
		return fmt.Errorf("--all 与显式适配器 ID 不能同时使用")
	}
	if !all && len(args) == 0 {
		return fmt.Errorf("请指定适配器 ID，或用 --all 移除所有检测到的适配器产物")
	}

	// 选目标适配器（与 install 同样的选取逻辑）。注意：uninstall 不要求工具仍 InstalledFor——
	// 用户可能已卸载 Claude Code 但想清理它留下的 .claude/，故 --all 仍用 Detect() 过滤
	// （Detect 探测的是「工具存在过」的痕迹，如 .claude/ 目录），显式 ID 则无条件尊重。
	var targets []adapter.Adapter
	if all {
		every := adapter.All()
		for _, a := range every {
			if a.Detect(repoRoot) {
				targets = append(targets, a)
			}
		}
		if len(targets) == 0 {
			writeOut(out, "未检测到任何已安装工具；--all 没有目标。可显式指定 ID（如 `readignore uninstall claude-code`）。\n")
			return nil
		}
	} else {
		a, ok := adapter.Get(args[0])
		if !ok {
			return fmt.Errorf("未知适配器 ID %q；用 `readignore adapters` 查看支持的适配器", args[0])
		}
		targets = []adapter.Adapter{a}
	}

	// uninstall 不依赖 .readignore（卸载不该要求规则文件还在）：用空 RawPatterns 调 Generate
	// 拿产物路径——路径与规则内容无关（claudecode/codex/pi 的 sh/extension 是通用模板；
	// opencode 的 opencode.json 路径也恒定）。这与 check.go 探测安装状态的做法一致。
	plan := adapter.Plan{RepoRoot: repoRoot}

	if !dryRun {
		writeOut(out, "⚠️  正在删除适配器产物（不可逆）。加 --dry-run 可先预览。\n\n")
	}

	var aggErr error
	for _, a := range targets {
		files, genErr := a.Generate(plan)
		if genErr != nil {
			return fmt.Errorf("适配器 %s 生成失败: %w", a.ID(), genErr)
		}
		removed, missing, failed := removeGeneratedFiles(out, repoRoot, a.ID(), files, dryRun)
		verb := "已删除"
		if dryRun {
			verb = "将删除"
		}
		fmt.Fprintf(out, "适配器 %s：%s %d 个文件（%d 个不存在已跳过）。\n", a.ID(), verb, removed, missing)
		if failed > 0 {
			thisErr := fmt.Errorf("适配器 %s 部分文件删除失败（%d 个）", a.ID(), failed)
			if aggErr == nil {
				aggErr = thisErr
			} else {
				aggErr = fmt.Errorf("%w；%s", aggErr, thisErr.Error())
			}
		}
	}
	return aggErr
}

// removeGeneratedFiles 删除一组 GeneratedFile，返回 (已删/将删数, 不存在跳过数, 失败数)。
//
// 删除规则（详见 newUninstallCmd 的 Long 说明）：
//   - dryRun=true：只打印「将删除 <path>」，不真删；
//   - 文件不存在：跳过（计 missing）；
//   - 删除成功后，尝试 os.Remove 其父目录（清空 .claude/hooks/ 等空目录），
//     非空自动失败——正好「只清 readignore 产生的空目录，不碰用户其他文件」。
//
// 抽成独立函数便于测试断言「删了几个、跳过几个」。
func removeGeneratedFiles(out io.Writer, repoRoot, adapterID string, files []adapter.GeneratedFile, dryRun bool) (removed, missing, failed int) {
	_ = adapterID // 预留：未来按适配器差异化日志
	// 记录已尝试清空的目录，避免同一目录被多个文件重复尝试。
	triedDirs := map[string]bool{}
	for _, f := range files {
		absPath := filepath.Join(repoRoot, filepath.FromSlash(f.Path))

		if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
			fmt.Fprintf(out, "  跳过 %s（不存在）\n", f.Path)
			missing++
			continue
		} else if statErr != nil {
			fmt.Fprintf(out, "  失败 %s：%v\n", f.Path, statErr)
			failed++
			continue
		}

		if dryRun {
			fmt.Fprintf(out, "  将删除 %s\n", f.Path)
			removed++
			continue
		}

		if rmErr := os.Remove(absPath); rmErr != nil {
			fmt.Fprintf(out, "  失败 %s：%v\n", f.Path, rmErr)
			failed++
			continue
		}
		fmt.Fprintf(out, "  已删除 %s\n", f.Path)
		removed++

		// 尝试清空产物所在目录（.claude/hooks/、.codex/ 等）。os.Remove 只删空目录，
		// 非空自动失败——符合「只清 readignore 产生的空目录」语义。
		if dir := filepath.Dir(absPath); dir != "" && dir != "." && !triedDirs[dir] {
			triedDirs[dir] = true
			if rmErr := os.Remove(dir); rmErr == nil {
				if rel, relErr := filepath.Rel(repoRoot, dir); relErr == nil {
					fmt.Fprintf(out, "  已清空目录 %s\n", filepath.ToSlash(rel))
				}
			}
		}
	}
	return removed, missing, failed
}
