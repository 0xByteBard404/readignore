package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// update 覆盖刷新已装产物（= install --force）。手改会被覆盖。
func TestUpdate_OverwritesExisting(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	_, err := runCmd(t, []string{"install", "claude-code"})
	require.NoError(t, err)
	// 手改 settings.json（模拟用户改动）。
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude/settings.json"), []byte(`{"user":"edit"}`), 0o644))

	out, err := runCmd(t, []string{"update", "claude-code"})
	require.NoError(t, err)
	assert.Contains(t, out, "写入")
	// 被覆盖回 readignore 产物（含 PreToolUse 钩子注册）。
	got, errR := os.ReadFile(filepath.Join(dir, ".claude/settings.json"))
	require.NoError(t, errR)
	assert.Contains(t, string(got), "PreToolUse")
}

// update --all 覆盖刷新所有检测到的适配器。
func TestUpdate_All(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	// opencode.json 存在 → Detect(opencode)=true。
	writeFile(t, ".", "opencode.json", `{"existing": true}`)

	_, err := runCmd(t, []string{"update", "--all"})
	require.NoError(t, err)
	got, errR := os.ReadFile(filepath.Join(dir, "opencode.json"))
	require.NoError(t, errR)
	assert.Contains(t, string(got), "permission")
}

// update 无参默认 --all（刷新所有检测到的）。
func TestUpdate_DefaultAll(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	writeFile(t, ".", "opencode.json", `{"existing": true}`) // 触发 Detect(opencode)

	_, err := runCmd(t, []string{"update"}) // 无参 → 默认 --all
	require.NoError(t, err)
	got, errR := os.ReadFile(filepath.Join(dir, "opencode.json"))
	require.NoError(t, errR)
	assert.Contains(t, string(got), "permission")
}
