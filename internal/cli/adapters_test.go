package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adapters 列出全部已注册适配器：至少含 claude-code(hard) 与 opencode(config)。
// 验证 blank import 触发自注册生效。
func TestAdapters_ListsRegistered(t *testing.T) {
	chdirTemp(t) // 取 cwd 为仓库根，避免 Detect 命中真实仓库痕迹。

	out, err := runCmd(t, []string{"adapters"})
	require.NoError(t, err)

	// 表头。
	assert.Contains(t, out, "ID")
	assert.Contains(t, out, "STRENGTH")
	assert.Contains(t, out, "DETECTED")

	// claude-code + opencode 必须出现（blank import 生效）。
	assert.Contains(t, out, "claude-code")
	assert.Contains(t, out, "hard")
	assert.Contains(t, out, "opencode")
	assert.Contains(t, out, "config")
}

// adapters 在检测到工具痕迹的目录里，DETECTED 列显示 yes。
func TestAdapters_DetectsClaudeCode(t *testing.T) {
	chdirTemp(t)
	// 制造 .claude/ 目录，触发 claude-code Detect=true。
	writeFile(t, ".", ".claude/.gitkeep", "")

	out, err := runCmd(t, []string{"adapters"})
	require.NoError(t, err)
	assert.Contains(t, out, "claude-code")
	// claude-code 行应含 yes（检测到 .claude/）。
	assert.Regexp(t, `claude-code\s+hard\s+yes`, out)
}

// adapters 不依赖 .readignore：纯空目录也能跑。
func TestAdapters_NoReadignoreRequired(t *testing.T) {
	chdirTemp(t)
	_, err := runCmd(t, []string{"adapters"})
	require.NoError(t, err)
}
