// Package hookengine 的测试覆盖公共 hook 引擎的两个导出构造器。
//
// 本测试**只**断言字符串产物结构（非空、含关键片段），不重复 claudecode 包里的
// 21 case 真跑集成测试——真跑覆盖由 claudecode 包承担（claudecode.Generate 现已改调
// 本包的 BuildShScript/BuildPyEngine，故 claudecode 的集成测试等价于在真跑本引擎）。
package hookengine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildShScript_Structure 验证 sh 脚本骨架：调 readignore.py + 输出 PreToolUse
// deny 结构（permissionDecision）。这是 claudecode + codex 共用 PreToolUse 协议的最低契约。
func TestBuildShScript_Structure(t *testing.T) {
	got := BuildShScript([]string{".env", "!.env.example"})
	require.NotEmpty(t, got, "BuildShScript must return non-empty script")

	// 必须调用 readignore.py（py 是匹配引擎，sh 只做 JSON 抽取）。
	assert.Contains(t, got, "readignore.py", "sh must invoke readignore.py")

	// 必须含 PreToolUse deny 结构（permissionDecision），这是 Claude-style 协议的拦截信号。
	assert.Contains(t, got, "permissionDecision", "sh must emit permissionDecision deny JSON")
	assert.Contains(t, got, `"deny"`, "sh must contain deny literal")

	// 跨平台 python 探测（Windows python3 常为商店占位符）。
	assert.Contains(t, got, "python3")
	assert.Contains(t, got, "python")

	// 覆盖 Read|Grep|Glob|Bash 四个工具的字段抽取。
	assert.Contains(t, got, "file_path")
	assert.Contains(t, got, "path")
	assert.Contains(t, got, "pattern")
	assert.Contains(t, got, "command")
}

// TestBuildPyEngine_Structure 验证 py 引擎骨架：内嵌 patterns + matches 函数 + 取反语义。
func TestBuildPyEngine_Structure(t *testing.T) {
	raw := []string{".env", "!.env.example"}
	got := BuildPyEngine(raw)
	require.NotEmpty(t, got, "BuildPyEngine must return non-empty engine source")

	// patterns 必须原样内嵌（取反行保留，最后规则胜出的关键）。
	assert.Contains(t, got, ".env")
	assert.Contains(t, got, "!.env.example", "negation pattern must be embedded verbatim")

	// 必须含 matches 函数（claudecode + codex sh 都靠它判路径）。
	assert.Contains(t, got, "def matches(")

	// 取反语义：Rule.negated 字段 + matches 里 excluded = not rule.negated 的求值。
	assert.Contains(t, got, "negated", "engine must implement negation semantics")
	assert.Contains(t, got, "excluded")

	// 零第三方依赖：仅标准库 re。
	assert.Contains(t, got, "import re")
	assert.NotContains(t, got, "import pathspec")

	// DENY 信号契约：argv 任一命中 → stdout 输出 "DENY"。
	assert.Contains(t, got, `"DENY"`)
}

// TestBuildPyEngine_EmptyPatterns 空 patterns 不应崩溃，且渲染成合法 python 字面量。
func TestBuildPyEngine_EmptyPatterns(t *testing.T) {
	got := BuildPyEngine(nil)
	require.NotEmpty(t, got)
	assert.Contains(t, got, "PATTERNS = []")
}

// TestBuildPyEngine_SpecialChars patterns 含引号/反斜杠/控制字符时，pythonRepr 必须
// 安全转义（M-2 修复），不破坏 python 语法。这里只断言产物含转义形式，
// 真跑不崩由 claudecode 包的 TestGenerate_PatternsWithSpecialChars 验证。
func TestBuildPyEngine_SpecialChars(t *testing.T) {
	got := BuildPyEngine([]string{`secret's "file"`, `back\slash`})
	// 反斜杠必须被转义成 \\（避免被 python 当成转义序列起点）。
	assert.Contains(t, got, `\\`)
	// 双引号必须被转义成 \"（字面量边界安全）。
	assert.True(t, strings.Contains(got, `\"`), "double quote must be escaped")
	// 单引号在双引号字面量里可原样出现（无需转义），但不应让产物崩溃。
	assert.Contains(t, got, "secret")
}
