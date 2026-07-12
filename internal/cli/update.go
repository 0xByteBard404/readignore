package cli

import (
	"github.com/spf13/cobra"
)

// newUpdateCmd 构造 `readignore update` 子命令：把已安装适配器的产物刷新到当前
// readignore 版本（等价于 install --force）。
//
// 典型场景：readignore 升级后（如钩子脚本模板有改进），老用户仓库里的
// .claude/hooks/readignore.sh 等还是旧版——跑 update 覆盖刷新到新版。
//
// 实现上直接复用 runInstall(force=true)：Generate + writeGeneratedFiles 覆盖写。
// update 与 install --force 功能等价，仅语义不同（"更新已装的" vs "重装"）。
//
// 与 install 的差异：update 无参时默认 --all（刷新所有检测到的产物）——update 语义
// 是"刷新已装的"，无参作用于全部更顺手；install 无参仍报错（install 是写入操作，
// 需明确目标）。
func newUpdateCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "update [adapter-id]",
		Short: "更新已装适配器产物（覆盖刷新到当前版本；默认 --all）",
		Long: `更新（覆盖刷新）已安装适配器的产物到当前 readignore 版本。

用法：
  readignore update                 更新所有检测到的适配器产物（默认，等同 --all）
  readignore update <adapter-id>    只更新单个适配器产物

等价于 install --force：覆盖已有产物文件。readignore 升级后用它刷新钩子脚本等
产物（例如钩子模板改进后，更新本地的 .claude/hooks/readignore.sh）。

注意：覆盖写会替换你对产物文件的手改（如 .claude/settings.json）。`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 无参默认 --all（update 语义是"刷新已装的"，默认作用于全部更顺手）。
			if !all && len(args) == 0 {
				all = true
			}
			return runInstall(cmd.OutOrStdout(), args, all, true) // update = install --force
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "更新所有在当前目录检测到的适配器产物（无参时的默认）")
	return cmd
}
