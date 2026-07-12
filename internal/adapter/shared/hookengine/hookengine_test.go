// Package hookengine 的测试覆盖公共 hook 引擎的 sh 脚本生成。
//
// 本测试只断言 sh 脚本产物结构（含关键片段：调 readignore match、输出 deny JSON、
// PATH 检测 fallback），不重复 claudecode 包里的 21 case 真跑集成测试——真跑覆盖
// 由 claudecode 包承担（claudecode.Generate 调本包 BuildShScript，集成测试等价真跑）。
package hookengine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildShScript_Structure 验证 sh 脚本骨架：转发到 readignore hook-check
// + readignore 不在 PATH 的 fallback。这是 claudecode + codex 共用 PreToolUse
// 协议的最低契约。
//
// v0.3.3 起 sh 不再做 JSON 抽取/匹配（收敛到 Go 的 hook-check），故不再断言
// file_path/deny 等字面——那些由 hook-check 输出，sh 只转发。
func TestBuildShScript_Structure(t *testing.T) {
	got := BuildShScript()
	require.NotEmpty(t, got, "BuildShScript must return non-empty script")

	// v0.3.3：sh 转发到 readignore hook-check（Go 子命令，JSON 解析与匹配在 Go）。
	assert.Contains(t, got, "readignore hook-check", "sh must invoke `readignore hook-check`")

	// 不应再引用 v0.2 py 引擎或旧 bash grep 抽取（已废弃/迁移到 hook-check）。
	assert.NotContains(t, got, "readignore.py", "sh must not reference deprecated readignore.py")
	assert.NotContains(t, got, "python3", "sh must not probe python (py engine dropped)")
	assert.NotContains(t, got, "PATTERNS", "sh must not embed patterns")
	assert.NotContains(t, got, "extract_field", "sh must not do bash grep extraction (moved to hook-check)")

	// readignore 不在 PATH 的 fallback：放行 + stderr 警告（不搞死）。
	assert.Contains(t, got, "command -v readignore")
	assert.Contains(t, got, "hook disabled", "sh must warn when readignore missing from PATH")
}

// TestBuildShScript_Idempotent 多次调用返回同一份常量脚本（无参数、无随机）。
func TestBuildShScript_Idempotent(t *testing.T) {
	a := BuildShScript()
	b := BuildShScript()
	assert.Equal(t, a, b, "BuildShScript must be deterministic (constant script)")
}
