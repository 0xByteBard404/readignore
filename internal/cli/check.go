package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xByteBard404/readignore/internal/adapter"
	"github.com/0xByteBard404/readignore/internal/readignore"
)

// newCheckCmd 构造 `readignore check` 子命令：校验 .readignore 并报告安装状态。
//
// 两个职责：
//  1. 语法校验：读 .readignore 交 readignore.Parse，不报错即合法；
//  2. 适配器状态：对每个已注册适配器，检测其产物文件是否已存在于仓库根，
//     给出 yes/partial/no 三档（partial = 部分产物存在）。
//
// check 是只读命令，不写盘。
func newCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "校验 .readignore 语法并报告各适配器安装状态",
		Long: `校验仓库根的 .readignore 语法（readignore.Parse 不报错即合法），
并报告每个已注册适配器的安装状态（产物文件是否已存在）。

只读命令，不修改任何文件。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(cmd.OutOrStdout())
		},
	}
	return cmd
}

// runCheck 是 check 命令的核心实现，独立于 cobra 便于测试。
func runCheck(out io.Writer) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}
	path := filepath.Join(repoRoot, readignoreFileName)

	// 1. .readignore 存在性 + 语法。
	content, readErr := os.ReadFile(path)
	switch {
	case readErr != nil && os.IsNotExist(readErr):
		writeOut(out, fmt.Sprintf("✗ 未找到 %s（请先运行 `readignore init`）\n", path))
		writeOut(out, "跳过适配器状态报告。\n")
		// 不是 error：check 命令成功执行了，只是报告状态为「未配置」。
		return nil
	case readErr != nil:
		return fmt.Errorf("读取 %s 失败: %w", path, readErr)
	}

	if _, parseErr := readignore.Parse(string(content)); parseErr != nil {
		writeOut(out, fmt.Sprintf("✗ %s 语法错误: %v\n", path, parseErr))
		return parseErr
	}
	writeOut(out, fmt.Sprintf("✓ %s 语法合法（%d 字节）\n", path, len(content)))

	// 2. 各适配器安装状态。
	writeOut(out, "\n适配器安装状态：\n")
	for _, a := range adapter.All() {
		status := adapterInstallStatus(repoRoot, a)
		fmt.Fprintf(out, "  %-14s %s\n", a.ID(), status)
	}
	return nil
}

// adapterInstallStatus 检测某适配器的产物文件在 repoRoot 下的存在情况。
//
// 策略：用一份空规则 Plan 触发 Generate（不影响产物路径），检查产物路径是否存在。
// 返回值：
//   - "installed"  全部产物文件均存在；
//   - "partial"    部分存在；
//   - "not installed" 全部缺失；
//   - "error: ..." Generate 失败（理论不该发生，但如实标注）。
//
// 注意：用空 RawPatterns 调 Generate 仅用于探明产物路径集合；产物内容会因此
// 变化（如 opencode.json 的 permission.read 为空），但 check 只关心「文件在不在」，
// 不校验内容与当前 .readignore 是否一致——后者是 TODO（内容差异比对）。
func adapterInstallStatus(repoRoot string, a adapter.Adapter) string {
	files, err := a.Generate(adapter.Plan{RepoRoot: repoRoot})
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	exist := 0
	for _, f := range files {
		abs := filepath.Join(repoRoot, filepath.FromSlash(f.Path))
		if _, statErr := os.Stat(abs); statErr == nil {
			exist++
		}
	}
	switch exist {
	case 0:
		return "not installed"
	case len(files):
		return "installed"
	default:
		return fmt.Sprintf("partial (%d/%d)", exist, len(files))
	}
}
