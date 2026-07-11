package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

// install claude-code：写两个文件（v0.3：sh + settings.json），sh 可执行（0755）。
func TestInstall_ClaudeCode_WritesFiles(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n*.pem\n")

	out, err := runCmd(t, []string{"install", "claude-code"})
	require.NoError(t, err)
	assert.Contains(t, out, "写入")

	// v0.3：两个产物均存在（不再生成 readignore.py）。
	for _, rel := range []string{
		".claude/hooks/readignore.sh",
		".claude/settings.json",
	} {
		assert.FileExistsf(t, filepath.Join(dir, filepath.FromSlash(rel)), "应写入 %s", rel)
	}
	// readignore.py 不应再生成（py 引擎废弃）。
	assert.NoFileExistsf(t, filepath.Join(dir, ".claude/hooks/readignore.py"),
		"v0.3 不应生成 readignore.py")

	// sh 应可执行（0755）。Windows 无可执行位概念，跳过权限断言。
	if runtime.GOOS != "windows" {
		fi, statErr := os.Stat(filepath.Join(dir, ".claude/hooks/readignore.sh"))
		require.NoError(t, statErr)
		// 任何用户可执行位（u+x / g+x / o+x 任一）即判定可执行。
		assert.NotZero(t, fi.Mode().Perm()&0o111, "readignore.sh 应可执行，实际 %o", fi.Mode().Perm())
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

// I-1：全部产物已存在且未传 --force 时，install 跳过所有文件、installed==0，
// 此时应打印「未变更」类提示，而【不】打印 InstallInstructions（旧实现会
// 无脑打「已写入...无需重启」，与「0 个文件写入」自相矛盾）。
func TestInstall_AllSkipped_NoInstructions(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	// 预置 opencode.json → install opencode 时全部（单文件）已存在被跳过。
	writeFile(t, ".", "opencode.json", `{"existing": true}`)

	out, err := runCmd(t, []string{"install", "opencode"})
	require.NoError(t, err) // 全跳过不算失败
	assert.Contains(t, out, "0 个文件写入")
	assert.Contains(t, out, "未变更")
	// 关键：不应出现 InstallInstructions 文案（opencode 含「已生成 opencode.json」）。
	assert.NotContains(t, out, "已生成 opencode.json")
	// claude-code 风格的 InstallInstructions 关键文案也不应出现（防误植）。
	assert.NotContains(t, out, "无需重启")
}

// I-2：install 部分文件写失败时 runInstall 应返回 error（CLI exit 非 0，CI 可感知）。
//
// 跨平台触发写失败：在 cwd 下预置一个名为 .claude 的【普通文件】。
// install claude-code 尝试 MkdirAll(.claude/hooks) 时，因 .claude 是文件而非目录
// 失败（MkdirAll 要求路径段是目录）→ 产物写盘失败 → runInstall 返回 error。
// 此法不依赖只读目录权限（Windows 不强制），Windows/POSIX 均稳定复现。
func TestInstall_PartialWriteFailure_ReturnsError(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	// 预置 .claude 为普通文件（阻断 MkdirAll(.claude/hooks)）。
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude"), []byte("block"), 0o644))

	out, err := runCmd(t, []string{"install", "claude-code"})
	// 产物写失败 → runInstall 返回 error → CLI exit 非 0。
	require.Error(t, err, "部分文件写失败应返回 error")
	assert.Contains(t, err.Error(), "部分文件写入失败")
	// 失败明细打到 stdout。
	assert.Contains(t, out, "失败")
}

// I-2 详测：writeGeneratedFiles 在 Mkdir/WriteFile 失败时返回 failed 计数正确。
// 直接调用内部函数，断言 (installed, skipped, failed, total) 四元组。
//
// 触发失败的跨平台方式：把 repoRoot 指向一个【本身是普通文件而非目录】的路径，
// 使 MkdirAll(repoRoot/.claude/hooks) 失败（MkdirAll 要求路径段是目录）。
// 这比依赖只读目录权限更可靠（Windows 不强制只读目录的写禁令）。
func TestWriteGeneratedFiles_FailedCounted(t *testing.T) {
	dir := chdirTemp(t)
	// 制造一个「是文件不是目录」的 repoRoot。
	notADir := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(notADir, []byte("I am a file"), 0o644))

	a := mustGetAdapter(t, "claude-code")
	files, err := a.Generate(adapter.Plan{RepoRoot: dir, RawPatterns: []string{".env"}})
	require.NoError(t, err)
	require.Len(t, files, 2) // v0.3：sh + settings.json

	buf := &bytes.Buffer{}
	// repoRoot=普通文件 → 产物路径 join 后的父目录 MkdirAll 必失败。
	installed, skipped, failed, total := writeGeneratedFiles(buf, notADir, a.ID(), files, false)
	assert.Equal(t, 2, total)
	assert.Equal(t, 0, installed)
	assert.Equal(t, 0, skipped)
	assert.Equal(t, 2, failed, "两个产物都因 MkdirAll 失败应全部计入 failed")
	assert.Contains(t, buf.String(), "失败")
}
