package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

// chdirTemp 创建一个临时目录并在测试期间 chdir 进去，测试结束自动恢复原 cwd。
//
// 用途：CLI 大量逻辑依赖「仓库根 = 当前工作目录」。直接 os.Chdir 到 t.TempDir()
// 可让 init/generate/install 真正落盘到临时目录而不污染仓库。
//
// 必须恢复原 cwd：否则后续测试的相对路径解析会错乱。t.Cleanup 保证这一点。
func chdirTemp(t *testing.T) string {
	t.Helper()
	prev, err := os.Getwd()
	require.NoError(t, err, "读取原 cwd 失败")
	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir), "chdir 到临时目录失败")
	t.Cleanup(func() {
		_ = os.Chdir(prev)
	})
	return dir
}

// writeFile 写文件到 dir 下相对路径 rel，自动建父目录。返回绝对路径。
// 测试夹具用，统一处理目录创建避免每个测试重复 mkdir 逻辑。
func writeFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if dir2 := filepath.Dir(abs); dir2 != "" {
		require.NoError(t, os.MkdirAll(dir2, 0o755))
	}
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
	return abs
}

// runCmd 把 newRootCmd 的输出捕获到 *bytes.Buffer 并返回（out, err）。
// 测试统一入口：SetArgs 设参数、SetOut/SetErr 重定向、Execute 取结果。
//
// 与生产入口 Execute() 保持一致：把 --version 的哨兵 errVersionPrinted
// 翻译成 nil（版本已正常打印），测试无需感知哨兵存在。
func runCmd(t *testing.T, args []string) (string, error) {
	t.Helper()
	buf := &bytes.Buffer{}
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(buf)
	root.SetErr(buf)
	err := root.Execute()
	if err == errVersionPrinted {
		return buf.String(), nil
	}
	return buf.String(), err
}

// mustGetAdapter 从 registry 取适配器，找不到则 t.Fatal。测试夹具用。
func mustGetAdapter(t *testing.T, id string) adapter.Adapter {
	t.Helper()
	a, ok := adapter.Get(id)
	if !ok {
		t.Fatalf("适配器 %q 未注册（blank import 是否生效？）", id)
	}
	return a
}
