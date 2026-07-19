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

// newUninstallCmd 构造 `readignore uninstall` 子命令：移除适配器产物（install 的逆操作）。
//
// 两种用法：
//   - readignore uninstall <adapter-id>   移除单个适配器的产物；
//   - readignore uninstall --all          移除所有 Detect()=true 的适配器产物。
//
// 删除策略（按产物的 Removal 分派，避免误删用户配置）：
//   - RemovalDefault（独占文件，如 .claude/hooks/readignore.sh）：整删；
//   - RemovalSurgical（共享 JSON，如 .claude/settings.json）：按指纹精确摘除
//     readignore 注入的段，保留用户其余配置；
//   - RemovalPureProduct（共享 JSON，如 opencode.json）：仅当整文件都是
//     readignore 原样产物时整删，否则跳过并提示；
//   - --dry-run：只打印将做什么，不真改盘；
//   - 不存在的文件跳过（计 missing，不报错）；
//   - 仅当文件真被整删（action==removed）且非 dry-run 时，才尝试清空空的父目录
//     （如 .claude/hooks/），非空静默忽略——只清 readignore 产生的空目录；
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

删除策略（按产物 Removal 分派，保留你的其他配置）：
  独占文件（readignore.sh 等）整删；共享 JSON（settings.json 等）按指纹
  摘除 readignore 段，保留你写的其他配置。加 --dry-run 只预览不真改。
  不存在的文件跳过；只在整删文件后清空产物所在空目录。
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

	// 构造 plan：优先用 .readignore 的规则（供 PureProduct 字节比对 expectedContent）；
	// .readignore 不可读则空 plan（uninstall 不依赖规则文件存在，仅退化为结构判定）。
	// sections 在循环外算一次，复用给 expected 准备，避免每个适配器重复读盘。
	plan := adapter.Plan{RepoRoot: repoRoot}
	var sections *readignore.Sections
	if s, err := loadSections(repoRoot); err == nil && s != nil {
		sections = s
		plan.Rules = adapter.ClassifiedPatterns{
			Read:   patternStrings(sections.Read),
			Edit:   patternStrings(sections.Edit),
			Delete: patternStrings(sections.Delete),
		}
	}

	if dryRun {
		writeOut(out, "预览（dry-run，不实际改动）：\n\n")
	} else {
		writeOut(out, "⚠️  正在移除适配器产物（仅摘除 readignore 部分，保留你的其他配置）。\n\n")
	}

	var aggErr error
	for _, a := range targets {
		files, genErr := a.Generate(plan)
		if genErr != nil {
			return fmt.Errorf("适配器 %s 生成失败: %w", a.ID(), genErr)
		}
		// PureProduct 文件：用 readignore-plan Generate 的 Content 作 expectedContent（字节比对）。
		// .readignore 不可读时 sections==nil -> 不放入 expected，removePureProduct 退化为
		// 仅用结构判定（§6.1 边界，宁可少删不误删）。
		expected := map[string]string{}
		if sections != nil {
			for _, f := range files {
				if f.Removal == adapter.RemovalPureProduct {
					expected[f.Path] = f.Content
				}
			}
		}
		res := removeGeneratedFiles(out, repoRoot, a.ID(), files, dryRun, expected)
		verb := "已"
		if dryRun {
			verb = "将"
		}
		fmt.Fprintf(out, "适配器 %s：%s删除 %d / %s摘除 %d / 跳过 %d（不存在 %d）。\n",
			a.ID(), verb, res.removed, verb, res.modified, res.skipped, res.missing)
		if res.failed > 0 {
			thisErr := fmt.Errorf("适配器 %s 部分文件处理失败（%d 个）", a.ID(), res.failed)
			if aggErr == nil {
				aggErr = thisErr
			} else {
				aggErr = fmt.Errorf("%w；%s", aggErr, thisErr.Error())
			}
		}
	}
	return aggErr
}

// removalResult 汇总一次 uninstall 处理的计数。
type removalResult struct {
	removed  int // 整删（含摘空、纯产物、独占文件）
	modified int // 摘段写回（文件保留）
	skipped  int // 跳过（无 readignore hook / 非纯产物）
	missing  int // 文件不存在
	failed   int // 处理失败
}

// removeGeneratedFiles 按每个产物的 Removal 策略分派处理：
//   - RemovalDefault：整删（removeWhole）；
//   - RemovalSurgical：摘除 readignore hook 段（removeSurgicalJSON）；
//   - RemovalPureProduct：纯产物检测整删（removePureProduct）。
//
// expectedContents 仅供 PureProduct 字节比对（key = 产物 Path）。
//
// dry-run 处理下沉到每个分支内部（removeWhole / removeSurgicalJSON / removePureProduct
// 各自判断 dryRun 并打印「将…」），**不在本函数顶部统一 early-return**——否则
// Surgical/PureProduct 分支会在 dry-run 时被完全跳过，无法预览摘除/纯产物判定。
//
// 目录清理（dir pruning）：仅 action==actionRemoved 且非 dry-run 时，对该文件父目录
// 尝试 os.Remove（只删空目录，非空自动失败——正好「只清 readignore 产生的空目录」）。
// modified/unchanged/missing/failed 均不触发清理。
func removeGeneratedFiles(out io.Writer, repoRoot, adapterID string, files []adapter.GeneratedFile, dryRun bool, expectedContents map[string]string) removalResult {
	res := removalResult{}
	triedDirs := map[string]bool{}

	for _, f := range files {
		absPath := filepath.Join(repoRoot, filepath.FromSlash(f.Path))

		if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
			fmt.Fprintf(out, "  跳过 %s（不存在）\n", f.Path)
			res.missing++
			continue
		} else if statErr != nil {
			fmt.Fprintf(out, "  失败 %s：%v\n", f.Path, statErr)
			res.failed++
			continue
		}

		var action removalAction
		var err error
		switch f.Removal {
		case adapter.RemovalSurgical:
			if f.Surgical == nil {
				// 声明了 Surgical 但未给参数：保守退化为整删（独占文件语义）。
				action, err = removeWhole(out, absPath, f.Path, dryRun)
			} else {
				action, err = removeSurgicalJSON(out, absPath, f.Path, *f.Surgical, dryRun)
			}
		case adapter.RemovalPureProduct:
			action, err = removePureProduct(out, absPath, f.Path, adapterID, expectedContents[f.Path], dryRun)
		default:
			action, err = removeWhole(out, absPath, f.Path, dryRun)
		}

		if err != nil {
			res.failed++
			continue
		}

		switch action {
		case actionRemoved:
			res.removed++
			// 目录清理：仅真实删除时清空空父目录（dry-run 不清，预览不改盘）。
			if !dryRun {
				if dir := filepath.Dir(absPath); dir != "" && dir != "." && !triedDirs[dir] {
					triedDirs[dir] = true
					if rmErr := os.Remove(dir); rmErr == nil {
						if rel, relErr := filepath.Rel(repoRoot, dir); relErr == nil {
							fmt.Fprintf(out, "  已清空目录 %s\n", filepath.ToSlash(rel))
						}
					}
				}
			}
		case actionModified:
			res.modified++
		case actionUnchanged:
			res.skipped++
		}
	}
	return res
}

// removeWhole 整删文件（RemovalDefault 策略）。dryRun 时只打印「将删除」不真删，
// 但返回 actionRemoved 以便外层统计与（非 dry-run 时）目录清理。
func removeWhole(out io.Writer, absPath, displayPath string, dryRun bool) (removalAction, error) {
	if dryRun {
		fmt.Fprintf(out, "  将删除 %s\n", displayPath)
		return actionRemoved, nil
	}
	if err := os.Remove(absPath); err != nil {
		fmt.Fprintf(out, "  失败 %s：%v\n", displayPath, err)
		return actionUnchanged, err
	}
	fmt.Fprintf(out, "  已删除 %s\n", displayPath)
	return actionRemoved, nil
}
