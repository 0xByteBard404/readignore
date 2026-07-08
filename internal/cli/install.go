package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

// newInstallCmd 构造 `readignore install` 子命令：把适配器产物写到磁盘。
//
// 两种用法：
//   - readignore install <adapter-id>   安装单个适配器；
//   - readignore install --all          安装所有 Detect()=true 的适配器。
//
// 写盘策略（MVP，配置深度合并留作 TODO）：
//   - 若目标文件已存在且未传 --force，跳过该文件并提示用户手动合并
//     （避免覆盖既有配置，如 .claude/settings.json / opencode.json）；
//   - --force 时覆写整个文件（明确告知用户这是覆写）；
//   - Mode≠0 的文件写盘后 chmod 到目标权限（如 hook 的 0755）。
func newInstallCmd() *cobra.Command {
	var all bool
	var force bool
	cmd := &cobra.Command{
		Use:   "install <adapter-id>",
		Short: "把适配器产物写到磁盘",
		Long: `解析 .readignore，调用适配器 Generate，把产物写入仓库（相对仓库根）。

用法：
  readignore install <adapter-id>   安装单个适配器
  readignore install --all          安装所有在当前目录检测到的适配器

写盘策略（重要限制）：
  若目标文件已存在，默认跳过并提示手动合并（避免覆盖既有配置）。
  加 --force 覆写整个文件。

TODO（未来增强）：与既有配置文件（如 .claude/settings.json、opencode.json）
的深度合并当前未实现，MVP 采用「文件已存在则跳过/提示」策略。`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(cmd.OutOrStdout(), args, all, force)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "安装所有在当前目录检测到的适配器")
	cmd.Flags().BoolVar(&force, "force", false, "覆写已存在的目标文件")
	return cmd
}

// runInstall 是 install 命令的核心实现，独立于 cobra 便于测试。
func runInstall(out io.Writer, args []string, all, force bool) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}

	// --all 与显式 ID 互斥（避免歧义）。
	if all && len(args) > 0 {
		return fmt.Errorf("--all 与显式适配器 ID 不能同时使用")
	}
	if !all && len(args) == 0 {
		return fmt.Errorf("请指定适配器 ID，或用 --all 安装所有检测到的适配器")
	}

	// 选出要安装的适配器列表。
	//
	// 校验顺序说明：--all 分支在 loadPatterns 之前先用 Detect() 过滤出目标适配器，
	// 故「未检测到任何工具」会先于「.readignore 不存在」报出。这是有意的——
	// --all 的语义是「为已安装的工具装防护」，连工具都没有时谈规则文件无意义；
	// 而显式 ID 分支则相反，先校验 ID 再 loadPatterns，让缺 .readignore 的错误
	// 比缺适配器更早暴露（用户明确知道要装谁）。
	var targets []adapter.Adapter
	if all {
		every := adapter.All()
		for _, a := range every {
			if a.Detect(repoRoot) {
				targets = append(targets, a)
			}
		}
		if len(targets) == 0 {
			writeOut(out, "未检测到任何已安装工具；--all 没有目标。可显式指定 ID（如 `readignore install claude-code`）。\n")
			return nil
		}
	} else {
		a, ok := adapter.Get(args[0])
		if !ok {
			return fmt.Errorf("未知适配器 ID %q；用 `readignore adapters` 查看支持的适配器", args[0])
		}
		targets = []adapter.Adapter{a}
	}

	rawPatterns, err := loadPatterns(repoRoot)
	if err != nil {
		return err
	}
	plan := adapter.Plan{
		RepoRoot:    repoRoot,
		RawPatterns: rawPatterns,
	}

	var aggErr error
	for _, a := range targets {
		files, genErr := a.Generate(plan)
		if genErr != nil {
			return fmt.Errorf("适配器 %s 生成失败: %w", a.ID(), genErr)
		}
		installed, skipped, failed, total := writeGeneratedFiles(out, repoRoot, a.ID(), files, force)
		fmt.Fprintf(out, "适配器 %s：%d 个文件写入，%d 个已跳过（已存在）。\n", a.ID(), installed, skipped)
		// 仅当真正写入 ≥1 个文件时才打印 InstallInstructions（含「已写入...无需重启」
		// 等关键文案）——旧实现无脑打印，在「0 个文件写入」时会自相矛盾。
		if installed > 0 {
			writeOut(out, a.InstallInstructions())
			writeOut(out, "\n")
		} else if failed > 0 {
			// 有失败但无成功：写盘结果为空，不误导用户「已写入」。失败明细/汇总错误
			// 已在 writeGeneratedFiles 逐条打印、并由下方 aggErr 上报 stderr。
			writeOut(out, "无文件成功写入（详见上方失败明细）。\n\n")
		} else {
			// 全部已存在被跳过、且未传 --force：提示用 --force 覆写。
			writeOut(out, "未变更（全部已存在；如需覆写加 --force）。\n\n")
		}
		if failed > 0 {
			// 部分文件写失败 → 汇总成 error，最终让 CLI exit 非 0（CI 可感知）。
			thisErr := fmt.Errorf("适配器 %s 部分文件写入失败（%d/%d 失败）", a.ID(), failed, total)
			if aggErr == nil {
				aggErr = thisErr
			} else {
				aggErr = fmt.Errorf("%w；%s", aggErr, thisErr.Error())
			}
		}
	}
	return aggErr
}

// writeGeneratedFiles 把一组 GeneratedFile 写到 repoRoot 下，返回
// (写入数, 跳过数, 失败数, 总数)。
//
// 写盘规则（详见 newInstallCmd 的 Long 说明）：
//   - 目标已存在且非 force：跳过 + 提示手动合并；
//   - 目标不存在（或 force）：先写父目录（如需），再写文件；
//   - Mode≠0：写完后 chmod 到目标权限（用八进制）。
//
// 失败计数（Mkdir/WriteFile/chmod）向上层暴露：Mkdir/WriteFile 失败计 failed++，
// 让 runInstall 在「部分文件写失败」时返回 error → CLI exit 非 0（CI 可感知）；
// chmod 失败仅警告不计 failed（权限降级不阻断写入，文件已落盘）。
//
// 抽成独立函数便于测试断言「写了几个、跳过几个、权限是否正确」。
func writeGeneratedFiles(out io.Writer, repoRoot, adapterID string, files []adapter.GeneratedFile, force bool) (installed, skipped, failed, total int) {
	total = len(files)
	for _, f := range files {
		// GeneratedFile.Path 是 POSIX 风格（/ 分隔），跨平台用 ToSlash 再 Join。
		absPath := filepath.Join(repoRoot, filepath.FromSlash(f.Path))

		if _, statErr := os.Stat(absPath); statErr == nil && !force {
			fmt.Fprintf(out, "  跳过 %s（已存在，请手动合并；如需覆写加 --force）\n", f.Path)
			skipped++
			continue
		}

		// 确保父目录存在（如 .claude/hooks/）。
		if dir := filepath.Dir(absPath); dir != "" && dir != "." {
			if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
				fmt.Fprintf(out, "  失败 %s：创建目录失败: %v\n", f.Path, mkErr)
				failed++
				continue
			}
		}

		mode := os.FileMode(0o644) // 默认权限（含 Mode==0）。
		if f.Mode != 0 {
			mode = os.FileMode(f.Mode)
		}
		if writeErr := os.WriteFile(absPath, []byte(f.Content), mode); writeErr != nil {
			fmt.Fprintf(out, "  失败 %s：%v\n", f.Path, writeErr)
			failed++
			continue
		}
		// WriteFile 的 mode 受 umask 影响，写完显式 chmod 保证最终权限符合预期
		// （如 hook 的 0755 不能因 umask 降级成 0744）。
		if f.Mode != 0 {
			if chmodErr := os.Chmod(absPath, os.FileMode(f.Mode)); chmodErr != nil {
				fmt.Fprintf(out, "  警告 %s：设置权限失败: %v\n", f.Path, chmodErr)
			}
		}
		// M-1：Mode==0（语义=调用方默认 0644）打印时显式标 default，
		// 避免用户看到「mode 0」误以为权限是 000（不可读不可执行）。
		modeLabel := fmt.Sprintf("%o", f.Mode)
		if f.Mode == 0 {
			modeLabel = "0644 (default)"
		}
		fmt.Fprintf(out, "  写入 %s (mode %s)\n", f.Path, modeLabel)
		installed++
	}
	return installed, skipped, failed, total
}
