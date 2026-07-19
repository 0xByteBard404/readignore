package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xByteBard404/readignore/internal/adapter"
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

// uninstall 不依赖 .readignore 存在；且对「非纯产物」的共享配置文件（含用户配置）
// 必须保留并提示，不得整删（这正是 surgical removal 要修复的误删 bug）。
func TestUninstall_NonPureProduct_Preserved(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", "opencode.json", `{"existing": true}`) // 用户自己的配置，非 readignore 纯产物

	out, err := runCmd(t, []string{"uninstall", "opencode"})
	require.NoError(t, err)
	// 关键：文件保留（旧行为会误删），输出含「跳过」提示。
	assert.FileExists(t, filepath.Join(dir, "opencode.json"))
	assert.Contains(t, out, "跳过")
}

// uninstall claude-code：settings.json 混了 permissions + readignore hook ->
// sh 整删、settings.json 摘 readignore 段保留 permissions。
func TestUninstall_ClaudeCode_SurgicalKeepsPermissions(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	_, err := runCmd(t, []string{"install", "claude-code"})
	require.NoError(t, err)

	// 往 settings.json 注入用户 permissions（模拟手动合并）。
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	mixed := `{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {"PreToolUse": [{"matcher": "Read|Grep|Glob|Bash|Edit|Write|NotebookEdit", "hooks": [{"type": "command", "command": "bash .claude/hooks/readignore.sh", "shell": "bash", "timeout": 5}]}]}
}`
	require.NoError(t, os.WriteFile(settingsPath, []byte(mixed), 0o644))

	out, err := runCmd(t, []string{"uninstall", "claude-code"})
	require.NoError(t, err)
	assert.Contains(t, out, "摘除")

	// sh 整删；settings.json 保留且 readignore hook 已摘（只剩 permissions）。
	assert.NoFileExists(t, filepath.Join(dir, ".claude/hooks/readignore.sh"))
	raw, err := os.ReadFile(settingsPath)
	require.NoError(t, err)
	assert.JSONEq(t, `{"permissions": {"allow": ["Bash(ls:*)"]}}`, string(raw))
}

// dry-run：settings.json 含 permissions + readignore -> 输出 per-file「将摘除」且文件不变。
//
// 必须注入用户 permissions（mirror TestUninstall_ClaudeCode_SurgicalKeepsPermissions）：
// install 写出的 settings.json 只含 readignore hook，摘段后 root 为空 -> 走「将删除」
// 整删分支，per-file 不会打印「将摘除」。那样 assert.Contains(out,"将摘除") 只能命中
// 摘要行的列标签（"将摘除 0"），即便 removeGeneratedFiles 顶部加了 `if dryRun { continue }`
// 跳过整个 surgical 分支也会通过——正是 §3.1 退化的回归。注入 permissions 后摘段 root
// 非空 -> 走写回分支打印 per-file「将摘除 .claude/settings.json 的 readignore 段」，
// 该子串只在 surgical 分支真跑时出现，从而真正守卫 dry-run 不再 early-return。
func TestUninstall_DryRun_SurgicalPreview(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	_, err := runCmd(t, []string{"install", "claude-code"})
	require.NoError(t, err)

	// 注入用户 permissions（与 SurgicalKeepsPermissions 同形）：摘 readignore hook 后
	// root 仍非空 -> 走写回分支，打印 per-file「将摘除」而非「将删除（摘除后为空）」。
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	mixed := `{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {"PreToolUse": [{"matcher": "Read|Grep|Glob|Bash|Edit|Write|NotebookEdit", "hooks": [{"type": "command", "command": "bash .claude/hooks/readignore.sh", "shell": "bash", "timeout": 5}]}]}
}`
	require.NoError(t, os.WriteFile(settingsPath, []byte(mixed), 0o644))

	out, err := runCmd(t, []string{"uninstall", "claude-code", "--dry-run"})
	require.NoError(t, err)
	// per-file 消息（surgical 分支真跑才会打印）；不要 assert 摘要行的「将摘除」列标签。
	assert.Contains(t, out, "将摘除 .claude/settings.json")
	// dry-run 不改盘：settings.json 仍是我们注入的原样。
	assert.FileExists(t, filepath.Join(dir, ".claude/settings.json"))
}

// removeGeneratedFiles 按 Removal 分派：Surgical 摘段、PureProduct 检测、Default 整删。
func TestRemoveGeneratedFiles_Dispatch(t *testing.T) {
	dir := t.TempDir()
	files := []adapter.GeneratedFile{
		{Path: "sh.sh", Content: "# sh", Mode: 0o755}, // Default 整删
		{
			Path:    ".claude/settings.json",
			Content: `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"bash .claude/hooks/readignore.sh"}]}]},"permissions":{"allow":["Bash(ls:*)"]}}`,
			Removal: adapter.RemovalSurgical,
			Surgical: &adapter.SurgicalSpec{
				HookPath:    "hooks.PreToolUse",
				Fingerprint: "readignore.sh",
			},
		},
		{
			Path:    "opencode.json",
			Content: `{"$schema":"https://opencode.ai/config.json","permission":{"read":{},"edit":{}}}`,
			Removal: adapter.RemovalPureProduct,
		},
	}
	// 落盘。
	for _, f := range files {
		writeFile(t, dir, f.Path, f.Content)
		if f.Mode != 0 {
			_ = os.Chmod(filepath.Join(dir, filepath.FromSlash(f.Path)), os.FileMode(f.Mode))
		}
	}
	expected := map[string]string{"opencode.json": files[2].Content}

	buf := &bytes.Buffer{}
	// adapterID 仅 PureProduct 用（isPureProduct 按适配器键集合判定）；Surgical 走 spec、
	// Default 走 removeWhole，均不依赖 adapterID。这里三件产物混合，传 "opencode"
	// 让 opencode.json 走 PureProduct 整删（与 sh 整删、settings.json 摘段并存）。
	res := removeGeneratedFiles(buf, dir, "opencode", files, false, expected)

	// sh 整删 + settings.json 摘段（保留 permissions）+ opencode.json 纯产物整删。
	assert.Equal(t, 2, res.removed, "sh 与 opencode.json 整删") // sh + opencode.json
	assert.Equal(t, 1, res.modified, "settings.json 摘段写回")
	assert.Equal(t, 0, res.failed)

	assert.NoFileExists(t, filepath.Join(dir, "sh.sh"))
	assert.NoFileExists(t, filepath.Join(dir, "opencode.json"))
	raw, err := os.ReadFile(filepath.Join(dir, ".claude/settings.json"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"permissions":{"allow":["Bash(ls:*)"]}}`, string(raw))
}

// 目录清理：action==removed 才清空父目录；modified 不清。
func TestRemoveGeneratedFiles_DirPruning(t *testing.T) {
	dir := t.TempDir()
	// sh 在 .codex/hooks/ 下。triedDirs 仅对每个产物尝试 filepath.Dir(absPath)——
	// 单文件 .codex/hooks/readignore.sh 的直接父目录就是 .codex/hooks/，故只清它；
	// .codex/ 不会被尝试（没有任何产物以它为直接父目录），故意保留以防误删用户目录。
	files := []adapter.GeneratedFile{
		{Path: ".codex/hooks/readignore.sh", Content: "# sh", Mode: 0o755},
	}
	writeFile(t, dir, ".codex/hooks/readignore.sh", "# sh")

	buf := &bytes.Buffer{}
	res := removeGeneratedFiles(buf, dir, "codex", files, false, nil)
	require.Equal(t, 1, res.removed)
	assert.NoFileExists(t, filepath.Join(dir, ".codex/hooks/readignore.sh"))
	assert.Contains(t, buf.String(), "已清空目录 .codex/hooks") // 仅 .codex/hooks/ 被清
}
