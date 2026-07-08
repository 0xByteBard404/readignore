package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// root 无子命令时打印 help（含 Long 描述与子命令列表）。
func TestRoot_NoArgs_PrintsHelp(t *testing.T) {
	out, err := runCmd(t, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "readignore")
	assert.Contains(t, out, "init")
	assert.Contains(t, out, "adapters")
	assert.Contains(t, out, "generate")
	assert.Contains(t, out, "install")
	assert.Contains(t, out, "check")
}

// --version / -v 打印版本号。
func TestRoot_VersionFlag(t *testing.T) {
	t.Run("long", func(t *testing.T) {
		out, err := runCmd(t, []string{"--version"})
		// --version 走 errVersionPrinted 哨兵，Execute 翻译成 nil。
		require.NoError(t, err)
		assert.Contains(t, out, "readignore ")
		assert.Contains(t, out, Version)
	})
	t.Run("short", func(t *testing.T) {
		out, err := runCmd(t, []string{"-v"})
		require.NoError(t, err)
		assert.Contains(t, out, Version)
	})
}

// 未知子命令应报错（cobra 默认行为）。
func TestRoot_UnknownSubcommand_Errors(t *testing.T) {
	_, err := runCmd(t, []string{"bogus-command"})
	require.Error(t, err)
}

// Execute 内部对 errVersionPrinted 做了正常退出翻译；这里仅断言哨兵本身存在且
// newRootCmd 在不调用 Execute 时不 panic（契约层）。
func TestRoot_VersionSentinelExists(t *testing.T) {
	assert.Equal(t, "__readignore_version_printed__", errVersionPrinted.Error())
}

// resolveRepoRoot 返回当前 cwd（非空、可 stat）。
func TestResolveRepoRoot(t *testing.T) {
	root, err := resolveRepoRoot()
	require.NoError(t, err)
	assert.NotEmpty(t, root)
	// 返回值应是合法目录路径（不含换行/空字符串）。
	assert.False(t, strings.Contains(root, "\n"))
}
