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

		// === bad case 扩充：路径变体 / 不同读法 / 多敏感 token ===
		// DENY：路径变体（绕过尝试 —— 前缀、嵌套、glob 后缀、深层 **）
		{"Bash cat ./.env (./ 前缀)", `{"tool_name":"Bash","tool_input":{"command":"cat ./.env"}}`, true},
		{"Bash cat .env.production (.env.*)", `{"tool_name":"Bash","tool_input":{"command":"cat .env.production"}}`, true},
		{"Bash cat secrets/.env (嵌套)", `{"tool_name":"Bash","tool_input":{"command":"cat secrets/.env"}}`, true},
		{"Bash cat sub/dir/id_rsa (深层 **)", `{"tool_name":"Bash","tool_input":{"command":"cat sub/dir/id_rsa"}}`, true},
		// DENY：不同读法（不止 cat —— head/cp/tar 等都该拦 .env token）
		{"Bash head .env", `{"tool_name":"Bash","tool_input":{"command":"head .env"}}`, true},
		{"Bash cp .env /tmp/x (复制)", `{"tool_name":"Bash","tool_input":{"command":"cp .env /tmp/x"}}`, true},
		{"Bash tar czf x.tgz .env (打包)", `{"tool_name":"Bash","tool_input":{"command":"tar czf x.tgz .env"}}`, true},
		{"Bash cat .env secret.pem (多敏感 token)", `{"tool_name":"Bash","tool_input":{"command":"cat .env secret.pem"}}`, true},

		// ALLOW：防误报（命令名 / 选项 / 无规则路径 / 管道 / 赋值）
		{"Bash git status (命令名)", `{"tool_name":"Bash","tool_input":{"command":"git status"}}`, false},
		{"Bash npm install (命令名)", `{"tool_name":"Bash","tool_input":{"command":"npm install"}}`, false},
		{"Bash go build ./... (选项+路径)", `{"tool_name":"Bash","tool_input":{"command":"go build ./..."}}`, false},
		{"Bash cat main.go (无规则路径)", `{"tool_name":"Bash","tool_input":{"command":"cat main.go"}}`, false},
		{"Bash ls | grep foo (管道)", `{"tool_name":"Bash","tool_input":{"command":"ls | grep foo"}}`, false},
		{"Bash export FOO=bar (赋值)", `{"tool_name":"Bash","tool_input":{"command":"export FOO=bar"}}`, false},

		// 固有 limitation（静态分析天花板，钉住「已知不拦」——防未来误以为该拦而改坏）
		{"Bash cat $SECRET (变量展开, 固有不拦)", `{"tool_name":"Bash","tool_input":{"command":"cat $SECRET"}}`, false},
		{"Bash cat linked (间接路径, 固有不拦)", `{"tool_name":"Bash","tool_input":{"command":"cat linked"}}`, false},
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
