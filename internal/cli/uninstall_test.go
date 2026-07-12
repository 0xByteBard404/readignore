package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// install 后 uninstall：产物文件消失，.readignore 保留。
func TestUninstall_RemovesGeneratedFiles(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	_, err := runCmd(t, []string{"install", "claude-code"})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(dir, ".claude/hooks/readignore.sh"))

	out, err := runCmd(t, []string{"uninstall", "claude-code"})
	require.NoError(t, err)
	assert.Contains(t, out, "已删除")

	// 产物消失。
	assert.NoFileExists(t, filepath.Join(dir, ".claude/hooks/readignore.sh"))
	assert.NoFileExists(t, filepath.Join(dir, ".claude/settings.json"))
	// .readignore 必须保留（只清适配器产物）。
	assert.FileExists(t, filepath.Join(dir, ".readignore"))
}

// --dry-run 只预览，不真删。
func TestUninstall_DryRun_NoDelete(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	_, err := runCmd(t, []string{"install", "claude-code"})
	require.NoError(t, err)

	out, err := runCmd(t, []string{"uninstall", "claude-code", "--dry-run"})
	require.NoError(t, err)
	assert.Contains(t, out, "将删除")
	// 文件还在（dry-run 没真删）。
	assert.FileExists(t, filepath.Join(dir, ".claude/hooks/readignore.sh"))
}

// 不存在的产物：跳过不报错（没装就卸）。
func TestUninstall_MissingFiles_Skipped(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")

	out, err := runCmd(t, []string{"uninstall", "claude-code"})
	require.NoError(t, err) // 缺文件不算失败
	assert.Contains(t, out, "不存在")
}

// --all 卸载所有检测到的适配器。
func TestUninstall_All(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	_, err := runCmd(t, []string{"install", "opencode"})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(dir, "opencode.json"))

	out, err := runCmd(t, []string{"uninstall", "--all"})
	require.NoError(t, err)
	// opencode.json 存在 → Detect(opencode)=true → --all 卸它。
	assert.NoFileExists(t, filepath.Join(dir, "opencode.json"))
	assert.Contains(t, out, "已删除")
}

// --all 与显式 ID 互斥。
func TestUninstall_AllAndID_MutuallyExclusive(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	_, err := runCmd(t, []string{"uninstall", "claude-code", "--all"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不能同时")
}

// 既无 ID 也无 --all 报错。
func TestUninstall_NoTarget_Errors(t *testing.T) {
	chdirTemp(t)
	_, err := runCmd(t, []string{"uninstall"})
	require.Error(t, err)
}

// 未知适配器 ID 报错。
func TestUninstall_UnknownAdapter(t *testing.T) {
	chdirTemp(t)
	_, err := runCmd(t, []string{"uninstall", "ghost"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未知适配器")
}

// uninstall 不依赖 .readignore 存在（卸载不该要求规则文件还在）。
// 手动造一个 opencode.json（模拟装过），无 .readignore 也能卸。
func TestUninstall_NoReadignore_StillWorks(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", "opencode.json", `{"existing": true}`)

	_, err := runCmd(t, []string{"uninstall", "opencode"})
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "opencode.json"))
}
