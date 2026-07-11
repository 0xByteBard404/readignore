package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// match 命中 deny：.readignore 含 `.env`，问 `.env` → 返回 error（exit 1）。
func TestMatch_Deny(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")

	_, err := runCmd(t, []string{"match", ".env"})
	require.Error(t, err, "命中 .readignore 的路径应 deny（返回 error → exit 1）")
}

// match 放行：.readignore 含 `.env`，问 `main.go` → 无 error（exit 0）。
func TestMatch_Allow(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n")

	_, err := runCmd(t, []string{"match", "main.go"})
	assert.NoError(t, err, "未命中 .readignore 的路径应 allow（无 error → exit 0）")
}

// match 取反：`.env` + `!.env.example`，问 `.env.example` → 无 error（取反放行）。
// go-git matcher 从最后一条规则向前扫描，最先命中者胜出（Include 胜出 → 放行）。
func TestMatch_Negation(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", ".env\n!.env.example\n")

	_, err := runCmd(t, []string{"match", ".env.example"})
	assert.NoError(t, err, "取反规则 !.env.example 应放行（无 error → exit 0）")

	// 对照：被取反前的 `.env` 仍应 deny。
	_, err = runCmd(t, []string{"match", ".env"})
	require.Error(t, err, ".env 未被取反，仍应 deny")
}

// 无 .readignore → fallback 放行（不拦）：返回 nil（exit 0）。
func TestMatch_NoReadignore(t *testing.T) {
	chdirTemp(t)
	// 故意不写 .readignore。

	_, err := runCmd(t, []string{"match", "anything"})
	assert.NoError(t, err, "无 .readignore 时应 fallback 放行（无 error → exit 0）")
}

// 任意层级通配：`**/id_rsa` 应命中任意深度子路径下的 id_rsa。
func TestMatch_AnyLevel(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", "**/id_rsa\n")

	_, err := runCmd(t, []string{"match", "sub/dir/id_rsa"})
	require.Error(t, err, "**/id_rsa 应命中 sub/dir/id_rsa（deny）")
}
