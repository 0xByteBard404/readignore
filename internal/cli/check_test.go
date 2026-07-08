package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// check：合法 .readignore + 适配器状态报告（全部 not installed）。
func TestCheck_ValidSyntax_StatusReport(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n*.pem\n!/.env.example\n")

	out, err := runCmd(t, []string{"check"})
	require.NoError(t, err)
	assert.Contains(t, out, "语法合法")
	// 两个适配器的状态行。
	assert.Contains(t, out, "claude-code")
	assert.Contains(t, out, "opencode")
	assert.Contains(t, out, "not installed")
}

// .readignore 不存在时 check 不报错，报告未找到（友好提示先 init）。
func TestCheck_NoReadignore_FriendlyMessage(t *testing.T) {
	chdirTemp(t)
	out, err := runCmd(t, []string{"check"})
	require.NoError(t, err)
	assert.Contains(t, out, "未找到")
	assert.Contains(t, out, "init")
}

// check 检测到产物文件存在时报告 installed / partial。
func TestCheck_ReportsInstalled(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")

	// 预先 install opencode（写 opencode.json）。
	_, err := runCmd(t, []string{"install", "opencode"})
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "opencode.json"))

	out, err := runCmd(t, []string{"check"})
	require.NoError(t, err)
	// opencode.json 单文件已存在 → installed。
	assert.Contains(t, out, "opencode")
	assert.Contains(t, out, "installed")
	// claude-code 三件套都不在 → not installed。
	assert.Contains(t, out, "not installed")

	// 制造 claude-code 的部分产物（只放 sh）：应报 partial。
	writeFile(t, ".", ".claude/hooks/readignore.sh", "#!/bin/sh\n")
	out, err = runCmd(t, []string{"check"})
	require.NoError(t, err)
	assert.Contains(t, out, "partial")
}

// check 是只读命令：不创建任何文件。
func TestCheck_DoesNotWrite(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")

	before, _ := filepath.Glob(filepath.Join(dir, "*"))
	_, err := runCmd(t, []string{"check"})
	require.NoError(t, err)
	after, _ := filepath.Glob(filepath.Join(dir, "*"))
	// 顶层文件集合不变（check 不写盘；.readignore 是夹具预置的）。
	assert.Equal(t, before, after)

	// 确保没生成 .claude/ 或 opencode.json。
	_, statErr := os.Stat(filepath.Join(dir, ".claude"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

// adapterInstallStatus：Generate 失败时返回 error:（用注入式适配器难，跳过；
// 此处验证三种文件存在性分支已由 TestCheck_ReportsInstalled 覆盖）。
func TestAdapterInstallStatus_AllAbsent(t *testing.T) {
	dir := chdirTemp(t)
	// 用真实 claude-code 适配器（三件套都不在）。
	a := mustGetAdapter(t, "claude-code")
	got := adapterInstallStatus(dir, a)
	assert.Equal(t, "not installed", got)
}
