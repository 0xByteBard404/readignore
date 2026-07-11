package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generate claude-code：解析 .readignore → stdout 含 sh/settings 产物（v0.3 两件套）。
func TestGenerate_ClaudeCode(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n*.pem\n!/.env.example\n")

	out, err := runCmd(t, []string{"generate", "claude-code"})
	require.NoError(t, err)
	// dry-run 提示。
	assert.Contains(t, out, "dry-run")
	// v0.3：两个产物路径（sh + settings.json；不再生成 readignore.py）。
	assert.Contains(t, out, ".claude/hooks/readignore.sh")
	assert.Contains(t, out, ".claude/settings.json")
	assert.NotContains(t, out, "readignore.py", "v0.3 must not generate readignore.py")
	// sh 内容标记可执行（mode 0755 在头里）。
	assert.Contains(t, out, "mode 755")
	// v0.3：sh 调 readignore match（go-git 权威），不内嵌 patterns。
	assert.Contains(t, out, "readignore match")
}

// generate opencode：stdout 含 permission.read 的 deny 配置。
func TestGenerate_Opencode(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n.env.*\n!.env.example\n")

	out, err := runCmd(t, []string{"generate", "opencode"})
	require.NoError(t, err)
	assert.Contains(t, out, "opencode.json")
	// deny 配置（.env / .env.*）。
	assert.Contains(t, out, `"deny"`)
	// 取反 → allow。
	assert.Contains(t, out, `"allow"`)
	assert.Contains(t, out, ".env.example")
}

// .readignore 不存在时 generate 报错（提示先 init）。
func TestGenerate_NoReadignore_Errors(t *testing.T) {
	chdirTemp(t)
	_, err := runCmd(t, []string{"generate", "claude-code"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".readignore")
	assert.Contains(t, err.Error(), "init")
}

// 未知适配器 ID 报错。
func TestGenerate_UnknownAdapter(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	_, err := runCmd(t, []string{"generate", "no-such-adapter"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未知适配器")
}

// generate 缺少 adapter-id 参数报错（cobra.ExactArgs(1)）。
func TestGenerate_MissingAdapterArg(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")
	_, err := runCmd(t, []string{"generate"})
	require.Error(t, err)
}

// dry-run 不写盘：generate 后仓库里没有 .claude/。
func TestGenerate_DoesNotWrite(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")

	_, err := runCmd(t, []string{"generate", "opencode"})
	require.NoError(t, err)
	assert.NoFileExistsf(t, dir+"/opencode.json", "dry-run 不应写盘")
	assert.NoFileExistsf(t, dir+"/.claude", "dry-run 不应建 .claude/")
}
