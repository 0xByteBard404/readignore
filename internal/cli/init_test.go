package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// init 在空目录生成 .readignore 模板，内容含常见敏感文件示例。
func TestInit_CreatesTemplate(t *testing.T) {
	dir := chdirTemp(t)

	out, err := runCmd(t, []string{"init"})
	require.NoError(t, err)
	assert.Contains(t, out, "已生成")
	assert.Contains(t, out, ".readignore")

	content, statErr := os.ReadFile(filepath.Join(dir, ".readignore"))
	require.NoError(t, statErr, "模板文件应已写入")
	body := string(content)
	// 模板含常见敏感文件类别示例。
	assert.Contains(t, body, ".env")
	assert.Contains(t, body, "*.pem")
	assert.Contains(t, body, "**/id_rsa")
	assert.Contains(t, body, ".aws/")
	assert.Contains(t, body, "#") // 含注释
	// 默认放行脱敏示例文件（! 取反启用），与 README 语法示例一致。
	assert.Contains(t, body, "!.env.example")
	// 误判保险：确保不是以注释形式出现（旧模板曾注释掉）。
	assert.NotContains(t, body, "# !.env.example")
}

// .readignore 已存在时拒绝覆盖，提示 --force。
func TestInit_RefusesOverwrite(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", "# my custom\n.env\n")

	_, err := runCmd(t, []string{"init"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "已存在")
	assert.Contains(t, err.Error(), "--force")

	// 文件未被改写。
	got, errR := os.ReadFile(".readignore")
	require.NoError(t, errR)
	assert.Equal(t, "# my custom\n.env\n", string(got))
}

// --force 覆盖既有 .readignore，内容变为模板。
func TestInit_ForceOverwrites(t *testing.T) {
	chdirTemp(t)
	writeFile(t, ".", ".readignore", "# old\n")

	out, err := runCmd(t, []string{"init", "--force"})
	require.NoError(t, err)
	assert.Contains(t, out, "覆盖")

	got, errR := os.ReadFile(".readignore")
	require.NoError(t, errR)
	assert.Contains(t, string(got), "*.pem") // 模板内容，非旧内容
	assert.NotContains(t, string(got), "# old")
}

// init 打印「下一步」引导。
func TestInit_PrintsNextSteps(t *testing.T) {
	chdirTemp(t)
	out, err := runCmd(t, []string{"init"})
	require.NoError(t, err)
	assert.Contains(t, out, "readignore adapters")
	assert.Contains(t, out, "readignore generate")
	assert.Contains(t, out, "readignore install")
}
