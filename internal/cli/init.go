package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// templateReadignore 是 `readignore init` 生成的 .readignore 模板。
//
// 设计目标：覆盖常见敏感文件类别（环境变量、密钥、凭据、IDE/系统杂物），
// 每类带简短注释解释为何忽略；用户可直接增删。模板用 gitignore 语法，
// 保证 readignore.Parse 与各适配器都能正确处理。
const templateReadignore = `# readignore —— AI coding agent 防护规则（gitignore 语法）
# 列在此处的文件/目录将被适配器翻译成各 AI agent（Claude Code、opencode 等）
# 的防护配置，阻止 agent 读取或泄露这些敏感路径。
#
# 语法与 .gitignore 一致：# 注释；! 取反（放行）；** 跨任意层级；尾 / 锚定目录。

# --- 环境变量 / 密钥文件 ---
.env
.env.*
*.pem
*.key
*.p12
*.pfx

# --- SSH / 云凭据 ---
**/id_rsa
**/id_rsa.*
**/id_ed25519
**/id_ed25519.*
.aws/
.gcp/
.azure/

# --- 通用敏感目录与文件 ---
secrets/
**/secrets/
*.keystore
credentials.json
service-account*.json

# --- 私有配置 / 令牌 ---
.npmrc
.pypirc
.netrc
*.token

# --- IDE / 系统杂物（非敏感但通常不应进 agent 上下文） ---
.DS_Store
Thumbs.db
.idea/
.vscode/

# --- 示例：取反放行（脱敏的示例文件允许 agent 读取） ---
!.env.example
# !credentials.example.json
`

// newInitCmd 构造 `readignore init` 子命令：在仓库根生成 .readignore 模板。
//
// 行为：
//   - 默认在 repoRoot 下创建 .readignore（若已存在则拒绝，提示 --force）；
//   - --force 覆盖既有文件；
//   - 成功后打印生成路径与下一步建议（adapters / generate / install）。
func newInitCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "在当前目录生成 .readignore 模板",
		Long: `生成一份含常见敏感文件示例（.env / *.pem / id_rsa / .aws/ 等）的 .readignore 模板。

若 .readignore 已存在，默认拒绝覆盖（提示使用 --force）。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd.OutOrStdout(), force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "覆盖已存在的 .readignore")
	return cmd
}

// runInit 是 init 命令的核心实现，独立于 cobra 便于测试。
// out 接收用户可见输出（生产用 cmd.OutOrStdout()，测试可传 *bytes.Buffer）。
func runInit(out io.Writer, force bool) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}

	path := filepath.Join(repoRoot, readignoreFileName)
	if _, statErr := os.Stat(path); statErr == nil {
		// 文件已存在。
		if !force {
			return fmt.Errorf("%s 已存在；如需覆盖请加 --force", path)
		}
		// force 覆盖：提示用户这是覆写。
		writeOut(out, fmt.Sprintf("警告：覆盖既有 %s\n", path))
	} else if !os.IsNotExist(statErr) {
		// 其它 stat 错误（权限等）如实返回。
		return fmt.Errorf("检查 %s 失败: %w", path, statErr)
	}

	if err := os.WriteFile(path, []byte(templateReadignore), 0o644); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", path, err)
	}

	writeOut(out, fmt.Sprintf("已生成 %s\n", path))
	writeOut(out, "下一步：\n")
	writeOut(out, "  readignore adapters             查看支持的适配器\n")
	writeOut(out, "  readignore generate claude-code 预览生成产物（dry-run）\n")
	writeOut(out, "  readignore install claude-code  把产物写到磁盘\n")
	return nil
}
