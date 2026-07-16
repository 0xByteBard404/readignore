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
		// cp 归 OpEdit，但写类动词读源文件（cp .env 读 .env 源）→ OpEdit 额外查 Read 段
		// 守泄露。本夹具是裸 pattern（无段头）→ .env 进 Read 段 → 拦（不再静默放行泄露）。
		{"Bash cp .env /tmp/x (复制读源, OpEdit 查 Read 守泄露)", `{"tool_name":"Bash","tool_input":{"command":"cp .env /tmp/x"}}`, true},
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

// === Task 3：分段式路由（tool_name → read/edit/delete 段）===

// [edit] package-lock.json → Edit 工具改它应被拦。
func TestRunHookCheck_EditSection(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, dir, ".readignore", "[edit]\npackage-lock.json\n")
	in := strings.NewReader(`{"tool_name":"Edit","tool_input":{"file_path":"package-lock.json"}}`)
	var out bytes.Buffer
	require.NoError(t, runHookCheck(in, &out))
	assert.Contains(t, out.String(), "permissionDecision") // deny
}

// package-lock 在 [edit] 不在 [read] → Read 工具读它应放行（段独立）。
func TestRunHookCheck_EditSection_NotInRead(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, dir, ".readignore", "[edit]\npackage-lock.json\n")
	in := strings.NewReader(`{"tool_name":"Read","tool_input":{"file_path":"package-lock.json"}}`)
	var out bytes.Buffer
	require.NoError(t, runHookCheck(in, &out))
	assert.Empty(t, out.String()) // 放行
}

// C2：NotebookEdit 用 notebook_path（非 file_path）→ OpEdit + notebook_path 命中 edit 段。
func TestRunHookCheck_NotebookEdit_NotebookPath(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, dir, ".readignore", "[edit]\nnb.ipynb\n")
	in := strings.NewReader(`{"tool_name":"NotebookEdit","tool_input":{"notebook_path":"nb.ipynb"}}`)
	var out bytes.Buffer
	require.NoError(t, runHookCheck(in, &out))
	assert.Contains(t, out.String(), "permissionDecision") // deny（notebook_path 命中 edit 段）

	// 反向钉死：file_path 字段对 NotebookEdit 无效（NotebookEdit 只取 notebook_path）。
	// 把 .ipynb 规则放进 [edit]，但工具传 file_path=nb.ipynb（notebook_path 缺失）→ 放行。
	var out2 bytes.Buffer
	in2 := strings.NewReader(`{"tool_name":"NotebookEdit","tool_input":{"file_path":"nb.ipynb"}}`)
	require.NoError(t, runHookCheck(in2, &out2))
	assert.Empty(t, out2.String()) // 放行（NotebookEdit 不读 file_path）
}

// Bash rm → OpDelete；[delete] src/ 模式（dirOnly）应命中裸 token "src"（matchAny 尾斜杠兜底）。
func TestRunHookCheck_BashRm_DeleteSection(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, dir, ".readignore", "[delete]\nsrc/\n")
	in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"rm -rf src"}}`)
	var out bytes.Buffer
	require.NoError(t, runHookCheck(in, &out))
	assert.Contains(t, out.String(), "permissionDecision") // deny
}

// Bash cat → OpRead；[read] .env 命中。
func TestRunHookCheck_BashCat_ReadSection(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, dir, ".readignore", "[read]\n.env\n")
	in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"cat .env"}}`)
	var out bytes.Buffer
	require.NoError(t, runHookCheck(in, &out))
	assert.Contains(t, out.String(), "permissionDecision") // deny
}

// parseDeletePaths：跳 - 选项；-- 后皆文件。
func TestParseDeletePaths(t *testing.T) {
	assert.Equal(t, []string{"src"}, parseDeletePaths("rm -rf src"))
	assert.Equal(t, []string{"a", "b", "c"}, parseDeletePaths("rm a b c"))
	assert.Equal(t, []string{"-weird"}, parseDeletePaths("rm -- -weird"))
}

// I1 修复：裸 pattern（无 [edit] 段）+ cp .env /tmp → 应 DENY。
// cp 是写类动词但读源文件（cp .env 读 .env 源），OpEdit 额外查 Read 段守泄露——
// 否则裸 .readignore（只进 Read 段、Edit 段空）下 cp .env 会静默放行（源泄露）。
func TestRunHookCheck_BashCp_ReadSectionDeny(t *testing.T) {
	dir := chdirTemp(t)
	writeFile(t, dir, ".readignore", ".env\n") // 裸 pattern 归 [read]，无 [edit] 段
	in := strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"cp .env /tmp/x"}}`)
	var out bytes.Buffer
	require.NoError(t, runHookCheck(in, &out))
	assert.Contains(t, out.String(), "permissionDecision") // DENY（cp 读源 .env 在 Read 段）
}
