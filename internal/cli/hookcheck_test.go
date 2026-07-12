package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunHookCheck 覆盖 hook-check 的核心场景，重点验证旧 bash 钩子的绕过面已修复：
// 多行 command、转义双引号、~ 路径，以及基本的 deny/allow/取反。
func TestRunHookCheck(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, dir, ".readignore", ".env\n.env.*\n!.env.example\n*.pem\n**/id_rsa\n")

	cases := []struct {
		name string
		json string
		deny bool
	}{
		// Read 路径字段（100% 精确，无绕过）
		{"Read .env", `{"tool_name":"Read","tool_input":{"file_path":".env"}}`, true},
		{"Read .env.example (取反放行)", `{"tool_name":"Read","tool_input":{"file_path":".env.example"}}`, false},
		{"Read main.go (无规则)", `{"tool_name":"Read","tool_input":{"file_path":"main.go"}}`, false},
		{"Read sub/id_rsa (** 任意层级)", `{"tool_name":"Read","tool_input":{"file_path":"sub/id_rsa"}}`, true},
		{"Read sub\\id_rsa (Windows 反斜杠)", `{"tool_name":"Read","tool_input":{"file_path":"sub\\id_rsa"}}`, true},

		// Bash command —— 字面路径
		{"Bash cat .env", `{"tool_name":"Bash","tool_input":{"command":"cat .env"}}`, true},
		{"Bash git config (合法命令放行)", `{"tool_name":"Bash","tool_input":{"command":"git config --global user.email x@y.com"}}`, false},
		{"Bash grep foo .env (多 token)", `{"tool_name":"Bash","tool_input":{"command":"grep foo .env"}}`, true},
		{"Bash ls /tmp (合法)", `{"tool_name":"Bash","tool_input":{"command":"ls -la /tmp"}}`, false},

		// 旧 bash 钩子的绕过面（hook-check 必须拦住）
		// 多行 command：JSON 里 \n 转义 → Unmarshal 成换行 → tokenize 切 → .env 命中
		{"Bash 多行 command (JSON \\n 转义)", `{"tool_name":"Bash","tool_input":{"command":"echo hi\ncat .env"}}`, true},
		// 多行 command：JSON 含实际换行（非规范）→ 预处理换行→空格 → 解析 → .env 命中
		{"Bash 多行 command (实际换行)", "{ \"tool_name\":\"Bash\", \"tool_input\":{ \"command\":\"echo hi\ncat .env\" }}", true},
		// 转义双引号：command 含 \"（旧 grep [^\"] 截断）→ Go json 正确解析 → .env 命中
		{"Bash 转义双引号 + .env", `{"tool_name":"Bash","tool_input":{"command":"echo \"hi\"; cat .env"}}`, true},
		// ~ 路径（旧 looks_like_path 漏 ~）→ 现在 ~ 当路径 → *.pem 命中
		{"Bash ~/ssh/secret.pem (~ 路径)", `{"tool_name":"Bash","tool_input":{"command":"cat ~/ssh/secret.pem"}}`, true},

		// Grep path 字段
		{"Grep path .env", `{"tool_name":"Grep","tool_input":{"path":".env"}}`, true},
		{"Grep path main.go (放行)", `{"tool_name":"Grep","tool_input":{"path":"main.go"}}`, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			require.NoError(t, runHookCheck(strings.NewReader(c.json), buf))
			if c.deny {
				assert.Contains(t, buf.String(), `"permissionDecision":"deny"`)
			} else {
				assert.NotContains(t, buf.String(), "deny")
			}
		})
	}
}

// 无 .readignore → 放行（fallback，不搞死）。
func TestRunHookCheck_NoReadignore_Allows(t *testing.T) {
	chdirTemp(t) // 无 .readignore
	buf := &bytes.Buffer{}
	require.NoError(t, runHookCheck(strings.NewReader(`{"tool_name":"Read","tool_input":{"file_path":".env"}}`), buf))
	assert.NotContains(t, buf.String(), "deny")
}

// 非法 JSON → 放行（不搞死）。
func TestRunHookCheck_BadJSON_Allows(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, dir, ".readignore", ".env\n")
	buf := &bytes.Buffer{}
	require.NoError(t, runHookCheck(strings.NewReader(`not json`), buf))
	assert.NotContains(t, buf.String(), "deny")
}
