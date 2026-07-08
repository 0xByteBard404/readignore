package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

// newGenerateCmd 构造 `readignore generate <adapter-id>` 子命令：dry-run 预览产物。
//
// 流程：解析 .readignore → 构造 adapter.Plan → 调目标适配器 Generate →
// 把产出的 []GeneratedFile 打印到 stdout（含路径、权限、内容分隔），不写盘。
// .readignore 不存在则报错（提示先 init）。
func newGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate <adapter-id>",
		Short: "解析 .readignore 并预览适配器产物（dry-run，打印到 stdout）",
		Long: `解析仓库根的 .readignore，调用指定适配器的 Generate，把产物打印到 stdout（不写盘）。

参数：
  <adapter-id>  目标适配器 ID（如 claude-code、opencode）；用 ` + "`readignore adapters`" + ` 查看全部。

输出包含每个生成文件的：相对路径、文件模式（八进制）、分隔线与完整内容。
.readignore 不存在时报错（请先运行 readignore init）。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerate(cmd.OutOrStdout(), args[0])
		},
	}
	return cmd
}

// runGenerate 是 generate 命令的核心实现，独立于 cobra 便于测试。
func runGenerate(out io.Writer, adapterID string) error {
	a, ok := adapter.Get(adapterID)
	if !ok {
		return fmt.Errorf("未知适配器 ID %q；用 `readignore adapters` 查看支持的适配器", adapterID)
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}

	rawPatterns, err := loadPatterns(repoRoot)
	if err != nil {
		return err
	}

	plan := adapter.Plan{
		RepoRoot:    repoRoot,
		RawPatterns: rawPatterns,
	}
	files, err := a.Generate(plan)
	if err != nil {
		return fmt.Errorf("适配器 %s 生成失败: %w", adapterID, err)
	}

	printGeneratedFiles(out, adapterID, files)
	return nil
}

// printGeneratedFiles 把一组 GeneratedFile 渲染为人类可读的 dry-run 输出。
//
// 格式：每个文件以 `=== <adapter>/<path> (mode <octal>) ===` 头 + 内容 + 空行分隔。
// 抽成独立函数便于 generate/install 共享渲染逻辑（install 也会先打印将要写入的文件）。
func printGeneratedFiles(out io.Writer, adapterID string, files []adapter.GeneratedFile) {
	fmt.Fprintf(out, "适配器 %s 将生成 %d 个文件（dry-run，未写盘）：\n\n", adapterID, len(files))
	for _, f := range files {
		header := fmt.Sprintf("=== %s :: %s (mode %o) ===", adapterID, f.Path, f.Mode)
		fmt.Fprintln(out, header)
		// 内容若本身不以换行结尾，补一个换行保证分隔整洁。
		body := f.Content
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		fmt.Fprint(out, body)
		fmt.Fprintln(out)
	}
}
