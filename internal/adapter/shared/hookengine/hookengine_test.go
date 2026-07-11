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

// TestBuildShScript_Structure 验证 sh 脚本骨架：调 readignore match + 输出 PreToolUse
// deny 结构（permissionDecision）+ readignore 不在 PATH 的 fallback。
// 这是 claudecode + codex 共用 PreToolUse 协议的最低契约。
func TestBuildShScript_Structure(t *testing.T) {
	got := BuildShScript()
	require.NotEmpty(t, got, "BuildShScript must return non-empty script")

	// 必须调 readignore match（go-git 权威 matcher，v0.3 核心改造）。
	assert.Contains(t, got, "readignore match", "sh must invoke `readignore match`")

	// 不应再引用 v0.2 的 py 引擎（已废弃）。
	assert.NotContains(t, got, "readignore.py", "sh must not reference deprecated readignore.py")
	assert.NotContains(t, got, "python3", "sh must not probe python (py engine dropped)")
	assert.NotContains(t, got, "PATTERNS", "sh must not embed patterns (runtime read via match)")

	// 必须含 PreToolUse deny 结构（permissionDecision），这是 Claude-style 协议的拦截信号。
	assert.Contains(t, got, "permissionDecision", "sh must emit permissionDecision deny JSON")
	assert.Contains(t, got, `"deny"`, "sh must contain deny literal")

	// readignore 不在 PATH 的 fallback：放行 + stderr 警告（不搞死）。
	assert.Contains(t, got, "command -v readignore")
	assert.Contains(t, got, "hook disabled", "sh must warn when readignore missing from PATH")

	// 覆盖 Read|Grep|Glob|Bash 四个工具的字段抽取。
	assert.Contains(t, got, "file_path")
	assert.Contains(t, got, "path")
	assert.Contains(t, got, "pattern")
	assert.Contains(t, got, "command")
}

// TestBuildShScript_Idempotent 多次调用返回同一份常量脚本（无参数、无随机）。
func TestBuildShScript_Idempotent(t *testing.T) {
	a := BuildShScript()
	b := BuildShScript()
	assert.Equal(t, a, b, "BuildShScript must be deterministic (constant script)")
}
