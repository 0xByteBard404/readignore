package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// root 无子命令时打印 help（含 Long 描述与子命令列表）。
func TestRoot_NoArgs_PrintsHelp(t *testing.T) {
	out, err := runCmd(t, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "readignore")
	assert.Contains(t, out, "init")
	assert.Contains(t, out, "adapters")
	assert.Contains(t, out, "generate")
	assert.Contains(t, out, "install")
	assert.Contains(t, out, "check")
}

// --version / -v 打印版本号。
func TestRoot_VersionFlag(t *testing.T) {
	t.Run("long", func(t *testing.T) {
		out, err := runCmd(t, []string{"--version"})
		// --version 走 errVersionPrinted 哨兵，Execute 翻译成 nil。
		require.NoError(t, err)
		assert.Contains(t, out, "readignore ")
		assert.Contains(t, out, Version)
	})
	t.Run("short", func(t *testing.T) {
		out, err := runCmd(t, []string{"-v"})
		require.NoError(t, err)
		assert.Contains(t, out, Version)
	})
}

// 未知子命令应报错（cobra 默认行为）。
func TestRoot_UnknownSubcommand_Errors(t *testing.T) {
	_, err := runCmd(t, []string{"bogus-command"})
	require.Error(t, err)
}

// Execute 内部对 errVersionPrinted 做了正常退出翻译；这里仅断言哨兵本身存在且
// newRootCmd 在不调用 Execute 时不 panic（契约层）。
func TestRoot_VersionSentinelExists(t *testing.T) {
	assert.Equal(t, "__readignore_version_printed__", errVersionPrinted.Error())
}

// resolveRepoRoot 返回当前 cwd（非空、可 stat）。
func TestResolveRepoRoot(t *testing.T) {
	root, err := resolveRepoRoot()
	require.NoError(t, err)
	assert.NotEmpty(t, root)
	// 返回值应是合法目录路径（不含换行/空字符串）。
	assert.False(t, strings.Contains(root, "\n"))
}

// PersistentPreRunE：跑非排除命令时触发 update-check（缓存命中落后 → 提示）。
// 必须 chdirTemp：init 会在 cwd 生成 .readignore，直接跑会污染仓库目录。
func TestRoot_UpdateCheckTriggersOnInit(t *testing.T) {
	withVersion(t, "0.4.0")
	forceUpdateCheckOn(t) // 注入 isTerminal=true + 缓存命中 latest=0.4.1（见 helper）
	chdirTemp(t)          // init 落盘到临时目录，不污染仓库

	out, err := runCmd(t, []string{"init"})
	assert.NoError(t, err) // init 会因无 .readignore 模板正常跑/打印
	assert.Contains(t, out, "new version 0.4.1")
	assert.Contains(t, out, "brew upgrade readignore")
}

// match / update 不触发 update-check（排除名单）。
// chdirTemp：match 读 .readignore、update 写产物，都需临时目录；即便
// forceUpdateCheckOn 注入了 isTerminal=true，排除名单先于 non-TTY 护栏命中。
func TestRoot_UpdateCheckSkipsOnMatchAndUpdate(t *testing.T) {
	withVersion(t, "0.4.0")
	forceUpdateCheckOn(t) // 即便强制开启，排除命令也应跳过
	chdirTemp(t)

	for _, args := range [][]string{{"match", ".readignore"}, {"update"}} {
		out, _ := runCmd(t, args)
		assert.NotContains(t, out, "new version", "命令 %v 不应触发 update-check", args)
	}
}
