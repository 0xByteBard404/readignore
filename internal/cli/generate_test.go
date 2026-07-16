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
	// v0.3.3：sh 转发到 readignore hook-check（JSON 解析+匹配在 Go）。
	assert.Contains(t, out, "readignore hook-check")
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

// generate opencode with [edit] section: stdout 含 permission.read + permission.edit 的 deny/allow 配置，
// edit 段规则进 permission.edit、不泄漏进 permission.read。这是 Task5 的分段 E2E 校验。
func TestGenerate_Opencode_EditSegment(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n[edit]\nsecrets/*.key\n!public/sample.key\n")

	out, err := runCmd(t, []string{"generate", "opencode"})
	require.NoError(t, err)
	assert.Contains(t, out, "opencode.json")
	// read 段：.env deny。
	assert.Contains(t, out, `".env": "deny"`)
	// edit 段：secrets/*.key deny、取反 public/sample.key allow。
	assert.Contains(t, out, `"secrets/*.key": "deny"`)
	assert.Contains(t, out, `"public/sample.key": "allow"`)
	// edit 段规则不应泄漏成 read 段 deny（.env 是唯一 read deny）。
	assert.Contains(t, out, `"edit"`)
	assert.Contains(t, out, `"read"`)
}

// generate kilocode with [edit] section: 同 opencode，edit 段进 permission.edit（含 ** 降级）。
func TestGenerate_Kilocode_EditSegment(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n[edit]\n**/id_rsa\nsecrets/*.key\n")

	out, err := runCmd(t, []string{"generate", "kilocode"})
	require.NoError(t, err)
	assert.Contains(t, out, "kilo.json")
	// read 段。
	assert.Contains(t, out, `".env": "deny"`)
	// edit 段：**/id_rsa 降级为 basename id_rsa、secrets/*.key deny。
	assert.Contains(t, out, `"id_rsa": "deny"`)
	assert.Contains(t, out, `"secrets/*.key": "deny"`)
	assert.NotContains(t, out, `**`)
	assert.Contains(t, out, `"edit"`)
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
