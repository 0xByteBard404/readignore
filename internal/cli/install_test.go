package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// install claude-code：写三个文件，sh 可执行（0755）。
func TestInstall_ClaudeCode_WritesFiles(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n*.pem\n")

	out, err := runCmd(t, []string{"install", "claude-code"})
	require.NoError(t, err)
	assert.Contains(t, out, "写入")

	// 三个产物均存在。
	for _, rel := range []string{
		".claude/hooks/readignore.sh",
		".claude/hooks/readignore.py",
		".claude/settings.json",
	} {
		assert.FileExistsf(t, filepath.Join(dir, filepath.FromSlash(rel)), "应写入 %s", rel)
	}

	// sh 应可执行（0755）。Windows 无可执行位概念，跳过权限断言。
	if runtime.GOOS != "windows" {
		fi, statErr := os.Stat(filepath.Join(dir, ".claude/hooks/readignore.sh"))
		require.NoError(t, statErr)
		// 任何用户可执行位（u+x / g+x / o+x 任一）即判定可执行。
		assert.NotZero(t, fi.Mode().Perm()&0o111, "readignore.sh 应可执行，实际 %o", fi.Mode().Perm())
	}
	// py 不需要可执行（0644）。
	fi, err := os.Stat(filepath.Join(dir, ".claude/hooks/readignore.py"))
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Zero(t, fi.Mode().Perm()&0o111, "readignore.py 不应可执行")
	}
}

// install opencode：写 opencode.json。
func TestInstall_Opencode(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")

	out, err := runCmd(t, []string{"install", "opencode"})
	require.NoError(t, err)
	assert.FileExistsf(t, filepath.Join(dir, "opencode.json"), "应写 opencode.json")
	assert.Contains(t, out, "permission.read")
}

// install 末尾打印该适配器的 InstallInstructions。
func TestInstall_PrintsInstructions(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")

	out, err := runCmd(t, []string{"install", "claude-code"})
	require.NoError(t, err)
	// claude-code 的 InstallInstructions 含「无需重启」。
	assert.Contains(t, out, "无需重启")
}

// 已存在文件默认跳过，提示手动合并（不覆盖既有配置）。
func TestInstall_SkipsExistingWithoutForce(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	// 预置一个既有 opencode.json（用户手工配置）。
	writeFile(t, ".", "opencode.json", `{"existing": true}`)

	out, err := runCmd(t, []string{"install", "opencode"})
	require.NoError(t, err)
	assert.Contains(t, out, "跳过")
	assert.Contains(t, out, "--force")

	// 内容未被覆盖。
	got, errR := os.ReadFile(filepath.Join(dir, "opencode.json"))
	require.NoError(t, errR)
	assert.Contains(t, string(got), `"existing": true`)
	assert.NotContains(t, string(got), "permission")
}

// --force 覆盖既有文件。
func TestInstall_ForceOverwrites(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	writeFile(t, ".", "opencode.json", `{"existing": true}`)

	out, err := runCmd(t, []string{"install", "opencode", "--force"})
	require.NoError(t, err)
	// 被覆盖成 readignore 产物。
	got, errR := os.ReadFile(filepath.Join(dir, "opencode.json"))
	require.NoError(t, errR)
	assert.Contains(t, string(got), "permission")
	assert.Contains(t, out, "写入")
}

// --all 安装所有检测到的适配器。
func TestInstall_All_DetectedOnly(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	// 只制造 opencode 痕迹（.claude/ 不存在）→ --all 应只装 opencode。
	writeFile(t, ".", "opencode.json", `{"existing": true}`) // 触发 Detect

	_, err := runCmd(t, []string{"install", "--all", "--force"})
	require.NoError(t, err)
	// opencode.json 被覆盖（force）。
	got, errR := os.ReadFile("opencode.json")
	require.NoError(t, errR)
	assert.Contains(t, string(got), "permission")
}

// --all 与显式 ID 互斥。
func TestInstall_AllAndID_MutuallyExclusive(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	_, err := runCmd(t, []string{"install", "claude-code", "--all"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不能同时")
}

// 既无 ID 也无 --all 报错。
func TestInstall_NoTarget_Errors(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	_, err := runCmd(t, []string{"install"})
	require.Error(t, err)
}

// 未知适配器 ID 报错。
func TestInstall_UnknownAdapter(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	_, err := runCmd(t, []string{"install", "ghost"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未知适配器")
}

// .readignore 不存在时 install 报错。
func TestInstall_NoReadignore_Errors(t *testing.T) {
	chdirTemp(t)
	_, err := runCmd(t, []string{"install", "claude-code"})
	require.Error(t, err)
}
