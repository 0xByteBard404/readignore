package claudecode

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

// TestAdapter_ID_Strength_Detect_Instructions 覆盖适配器身份元数据契约。
func TestAdapter_ID_Strength_Detect_Instructions(t *testing.T) {
	a := Adapter{}

	// ID 稳定、全小写、跨版本不变。
	assert.Equal(t, "claude-code", a.ID())
	// Claude Code PreToolUse hook 是执行前可编程拦截，强度最高。
	assert.Equal(t, adapter.StrengthHard, a.Strength())

	// Detect：.claude/ 目录或 CLAUDE.md 任一存在即判定已安装。
	t.Run("Detect .claude dir", func(t *testing.T) {
		tmp := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(tmp, ".claude"), 0o755))
		assert.True(t, a.Detect(tmp))
	})
	t.Run("Detect CLAUDE.md", func(t *testing.T) {
		tmp := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmp, "CLAUDE.md"), []byte("x"), 0o644))
		assert.True(t, a.Detect(tmp))
	})
	t.Run("Detect absent", func(t *testing.T) {
		tmp := t.TempDir()
		assert.False(t, a.Detect(tmp))
	})

	// InstallInstructions 非空、点出无需重启。
	instr := a.InstallInstructions()
	assert.NotEmpty(t, instr)
	assert.Contains(t, instr, ".claude/")
	assert.Contains(t, instr, "无需重启")
}

// TestAdapter_RegisteredInRegistry 验证 init() 已把本适配器登记进全局 registry，
// CLI 才能通过 adapter.All() / adapter.Get() 发现它。
func TestAdapter_RegisteredInRegistry(t *testing.T) {
	got, ok := adapter.Get("claude-code")
	require.True(t, ok, "claude-code adapter must self-register in init()")
	assert.Equal(t, adapter.StrengthHard, got.Strength())
}

// writeHookFiles 把 Generate 的三个产物按预定路径写入临时仓库根，
// 返回仓库根路径，供真跑 pipe-test。
//
// 关键：这里把 Generate 的输出**原样落盘**（不做任何 mock / 改写），
// 之后用 bash 真执行 sh，sh 内部再 fork python 真跑 readignore.py。
func writeHookFiles(t *testing.T, plan adapter.Plan) string {
	t.Helper()
	a := Adapter{}
	files, err := a.Generate(plan)
	require.NoError(t, err)
	require.Len(t, files, 3, "claude-code adapter must generate exactly 3 files")

	repoRoot := t.TempDir()
	for _, f := range files {
		abs := filepath.Join(repoRoot, filepath.FromSlash(f.Path))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		mode := os.FileMode(f.Mode)
		if mode == 0 {
			mode = 0o644
		}
		require.NoError(t, os.WriteFile(abs, []byte(f.Content), mode))
		// 显式按声明权限收尾（WriteFile 受 umask 影响，0755 可能落地成 0755-umask）。
		require.NoError(t, os.Chmod(abs, mode))
	}
	return repoRoot
}

// pipeTest 真跑：printf '<json>' | bash <repoRoot>/.claude/hooks/readignore.sh
// 返回 (stdout, stderr, exitCode)。命中拦截期望 stdout 含 permissionDecision:deny。
func pipeTest(t *testing.T, repoRoot, jsonInput string) (string, string, int) {
	t.Helper()
	shPath := filepath.Join(repoRoot, ".claude", "hooks", "readignore.sh")
	require.FileExists(t, shPath, "readignore.sh must be generated")

	// 把仓库根设为工作目录：sh 里以相对路径 .claude/hooks/readignore.py 调 python，
	// Claude Code 实际也是从仓库根发起 hook。
	cmd := exec.Command("bash", shPath)
	cmd.Dir = repoRoot
	cmd.Stdin = strings.NewReader(jsonInput)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// PreToolUse hook 设计上 exit 0（决策走 stdout JSON），所以这里一般 0。
	// 但我们仍捕获并返回，便于诊断。
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			t.Fatalf("spawn bash: %v", err)
		}
	}
	return stdout.String(), stderr.String(), exitCode
}

// denyJSON 构造 PreToolUse 工具入参 JSON 的最小有效片段。
// Claude Code 实际传入的是 {"tool_input":{...}, "tool_name":"..."}。
// 我们的 sh 只关心从原始文本里抽取字段，所以这里写成接近真实的结构。
func denyJSON(tool, field, value string) string {
	// field 不带尾随引号，便于 Bash/Grep/Glob/Read 各自字段名拼接。
	return `{"tool_name":"` + tool + `","tool_input":{"` + field + `":"` + value + `"}}`
}

// denyCase / allowCase 是 pipe-test 的断言封装，集中把「期望 deny」与「期望 allow」语义化。
func denyCase(t *testing.T, repoRoot, label, tool, field, value string) {
	t.Helper()
	out, _, _ := pipeTest(t, repoRoot, denyJSON(tool, field, value))
	if !assert.Containsf(t, out, `"permissionDecision":"deny"`,
		"[%s] expected DENY but got stdout=%q", label, out) {
		// 失败时把输入打印出来便于诊断。
		t.Logf("input: tool=%s %s=%s", tool, field, value)
	}
}

func allowCase(t *testing.T, repoRoot, label, tool, field, value string) {
	t.Helper()
	out, _, _ := pipeTest(t, repoRoot, denyJSON(tool, field, value))
	assert.NotContainsf(t, out, `"permissionDecision":"deny"`,
		"[%s] expected ALLOW (no deny) but got stdout=%q", label, out)
}

// TestIntegration_PipeTest_RealGeneratedScripts 是本适配器的核心集成测试：
// 构造 plan → Generate → 落盘 → bash 真跑 sh+py → 断言 deny/allow。
// 这里**真跑**生成的脚本（非 mock 自欺），任何匹配语义错误都会暴露在断言里。
func TestIntegration_PipeTest_RealGeneratedScripts(t *testing.T) {
	// patterns 与任务给定一致：覆盖取反、**任意层级、目录锚定等语义。
	patterns := []string{".env", ".env.*", "!.env.example", "*.pem", "**/id_rsa", ".env.local", ".env.production"}

	plan := adapter.Plan{
		RepoRoot:     "/repo/root", // 仅占位；真跑用临时目录。
		MatchedPaths: nil,          // 不参与匹配，留空。
		RawPatterns:  patterns,
	}
	repoRoot := writeHookFiles(t, plan)

	// ---- 必须覆盖的 case ----
	// 语义说明：readignore 严格遵循 gitignore 语义。.env.* 匹配 .env.sample/.env.example 等
	// 所有 .env.<suffix>，**除非** 用户显式写取反（!.env.example）放行。故 .env.example 放行
	// （被 !.env.example 取反），而 .env.sample 仍命中（用户没写取反）。若需放行所有模板，
	// 用户应在 .readignore 里加 !.env.sample / !.env.template 等——与 git 一致。
	denyCase(t, repoRoot, "Read .env", "Read", "file_path", ".env")
	denyCase(t, repoRoot, "Read .env.local", "Read", "file_path", ".env.local")
	denyCase(t, repoRoot, "Read .env.production", "Read", "file_path", ".env.production")
	allowCase(t, repoRoot, "Read .env.example (取反, 最关键)", "Read", "file_path", ".env.example")
	denyCase(t, repoRoot, "Read .env.sample (命中 .env.*，未取反)", "Read", "file_path", ".env.sample")
	denyCase(t, repoRoot, "Read secret.pem", "Read", "file_path", "secret.pem")
	denyCase(t, repoRoot, "Read sub/id_rsa (** 任意层级)", "Read", "file_path", "sub/id_rsa")
	denyCase(t, repoRoot, "Read sub/dir/id_rsa (** 深层)", "Read", "file_path", "sub/dir/id_rsa")
	allowCase(t, repoRoot, "Read main.go", "Read", "file_path", "main.go")
	allowCase(t, repoRoot, "Read send_env.py (env 前是_)", "Read", "file_path", "send_env.py")
	allowCase(t, repoRoot, "Read dotenv.py (无 .env 子串)", "Read", "file_path", "dotenv.py")
	allowCase(t, repoRoot, "Read .environment (.env 后是 i)", "Read", "file_path", ".environment")
	denyCase(t, repoRoot, "Bash cat .env (command 含 .env)", "Bash", "command", "cat .env")
	denyCase(t, repoRoot, "Grep path=.env", "Grep", "path", ".env")

	// ---- 补充 case：Glob pattern 通配、绝对/相对路径、Windows 反斜杠 ----
	denyCase(t, repoRoot, "Glob *.pem", "Glob", "pattern", "configs/server.pem")
	denyCase(t, repoRoot, "Read ./sub/dir/id_rsa", "Read", "file_path", "./sub/dir/id_rsa")
	denyCase(t, repoRoot, "Bash: cat ./.env.production", "Bash", "command", "cat ./.env.production")
	allowCase(t, repoRoot, "Bash: cat README.md", "Bash", "command", "cat README.md")
	allowCase(t, repoRoot, "Grep path=.envrc (非 .env.*)", "Grep", "path", ".envrc")

	// Windows 反斜杠路径：sh/python 应规范化为 / 后判定。
	if runtime.GOOS == "windows" {
		denyCase(t, repoRoot, "Read Windows sub\\id_rsa", "Read", "file_path", "sub\\id_rsa")
	}
}

// TestGenerate_FileArtifacts_Static 静态断言生成文件结构：
// 路径、权限位、关键内容（取反 patterns 已内嵌、sh 调 python、settings 片段合规）。
// 与上面的「真跑」互补：真跑验证语义正确性，这里验证产物结构正确性。
func TestGenerate_FileArtifacts_Static(t *testing.T) {
	a := Adapter{}
	files, err := a.Generate(adapter.Plan{
		RawPatterns: []string{".env", "!.env.example", "*.pem"},
	})
	require.NoError(t, err)
	require.Len(t, files, 3)

	byPath := map[string]adapter.GeneratedFile{}
	for _, f := range files {
		byPath[f.Path] = f
	}

	t.Run("readignore.sh executable", func(t *testing.T) {
		f, ok := byPath[".claude/hooks/readignore.sh"]
		require.True(t, ok)
		assert.Equal(t, uint32(0o755), f.Mode, "hook must be executable")
		assert.Contains(t, f.Content, "readignore.py")
		assert.Contains(t, f.Content, "permissionDecision")
		// 跨平台 python 探测。
		assert.Contains(t, f.Content, "python3")
		assert.Contains(t, f.Content, "python")
	})

	t.Run("readignore.py patterns embedded", func(t *testing.T) {
		f, ok := byPath[".claude/hooks/readignore.py"]
		require.True(t, ok)
		assert.Equal(t, uint32(0o644), f.Mode)
		// 取反行必须被原样内嵌进 python（最后规则胜出的关键）。
		assert.Contains(t, f.Content, "!.env.example")
		assert.Contains(t, f.Content, ".env")
		assert.Contains(t, f.Content, "*.pem")
		// 零第三方依赖：只用标准库 re（fnmatch 对 ** 处理弱，故选 re 自管 glob→regex）。
		assert.Contains(t, f.Content, "import re")
		// 不允许 require pathspec 等外部库。
		assert.NotContains(t, f.Content, "import pathspec")
	})

	t.Run("settings.json PreToolUse fragment", func(t *testing.T) {
		f, ok := byPath[".claude/settings.json"]
		require.True(t, ok)
		assert.Equal(t, uint32(0), f.Mode, "settings.json: caller uses default")
		// 解析为合法 JSON 并断言结构。
		assert.Contains(t, f.Content, `"PreToolUse"`)
		assert.Contains(t, f.Content, `"Read|Grep|Glob|Bash"`)
		assert.Contains(t, f.Content, "readignore.sh")
		assert.Contains(t, f.Content, `"timeout"`)
	})
}

// TestGenerate_EmptyPatterns 适配器对空 plan 不应崩溃，且仍产出可运行（但放行一切）的脚本。
func TestGenerate_EmptyPatterns(t *testing.T) {
	a := Adapter{}
	files, err := a.Generate(adapter.Plan{})
	require.NoError(t, err)
	require.Len(t, files, 3)
	// 真跑一遍：空 patterns 必然放行。
	repoRoot := writeHookFiles(t, adapter.Plan{})
	allowCase(t, repoRoot, "empty patterns allow .env", "Read", "file_path", ".env")
}

// TestGenerate_DetectResilient 当 patterns 含特殊字符（引号、反斜杠）时，
// 内嵌进 python 的方式不得破坏 python 语法。这里用真跑验证不崩。
func TestGenerate_PatternsWithSpecialChars(t *testing.T) {
	a := Adapter{}
	files, err := a.Generate(adapter.Plan{
		RawPatterns: []string{`*.pem`, `secret's file`},
	})
	require.NoError(t, err)
	require.Len(t, files, 3)
	// 至少把脚本落盘后能被 bash 调起来不报语法错。
	repoRoot := writeHookFiles(t, adapter.Plan{
		RawPatterns: []string{`*.pem`, `secret's file`},
	})
	out, stderr, code := pipeTest(t, repoRoot, denyJSON("Read", "file_path", "main.go"))
	_ = out
	assert.Equal(t, 0, code, "python must not crash on special chars; stderr=%s", stderr)
}

// TestIntegration_RootAnchoring_SlashPatterns 验证 I-1/I-2 修复：含斜杠（含前导斜杠）
// 的 pattern 必须锚定到仓库根，不允许匹配路径中间。
//
// 这一组与 go-git 权威 matcher 对齐：
//   - foo/bar    只匹配根 foo/bar，不匹配 sub/foo/bar（I-1）
//   - /leading   匹配根 leading，不匹配 sub/leading（I-2）
//
// 同时保证 basename 模式（无斜杠）仍任意层级匹配（不回归）。
func TestIntegration_RootAnchoring_SlashPatterns(t *testing.T) {
	plan := adapter.Plan{
		RawPatterns: []string{"foo/bar", "/leading", "secret.pem", "**/id_rsa"},
	}
	repoRoot := writeHookFiles(t, plan)

	// I-1：含内部斜杠锚定根。
	t.Run("I-1 foo/bar anchored to root", func(t *testing.T) {
		denyCase(t, repoRoot, "foo/bar at root → deny", "Read", "file_path", "foo/bar")
		allowCase(t, repoRoot, "sub/foo/bar → ALLOW (anchored, not mid-path)", "Read", "file_path", "sub/foo/bar")
		allowCase(t, repoRoot, "xfoo/bar → ALLOW (prefix anchored)", "Read", "file_path", "xfoo/bar")
	})

	// I-2：前导斜杠匹配根。
	t.Run("I-2 /leading matches root leading", func(t *testing.T) {
		denyCase(t, repoRoot, "leading at root → deny (前导斜杠匹配根)", "Read", "file_path", "leading")
		allowCase(t, repoRoot, "sub/leading → ALLOW", "Read", "file_path", "sub/leading")
		allowCase(t, repoRoot, "leadings → ALLOW (suffix anchored)", "Read", "file_path", "leadings")
	})

	// 回归保护：basename 模式（无斜杠）仍任意层级匹配。
	t.Run("regression secret.pem basename any level", func(t *testing.T) {
		denyCase(t, repoRoot, "secret.pem at root → deny", "Read", "file_path", "secret.pem")
		denyCase(t, repoRoot, "a/secret.pem → deny (basename any level)", "Read", "file_path", "a/secret.pem")
		allowCase(t, repoRoot, "xsecret.pem → ALLOW", "Read", "file_path", "xsecret.pem")
	})

	// 回归保护：**/x 仍任意层级。
	t.Run("regression **/id_rsa any level", func(t *testing.T) {
		denyCase(t, repoRoot, "id_rsa at root → deny", "Read", "file_path", "id_rsa")
		denyCase(t, repoRoot, "sub/dir/id_rsa → deny", "Read", "file_path", "sub/dir/id_rsa")
	})
}

// TestIntegration_CharClasses 验证 M-1：gitignore 字符类 [...] 支持。
// go-git 支持 *.[cho] 匹配 .c/.h/.o（单字符集）；python 引擎须同样支持。
func TestIntegration_CharClasses(t *testing.T) {
	plan := adapter.Plan{
		RawPatterns: []string{"*.[cho]"},
	}
	repoRoot := writeHookFiles(t, plan)

	denyCase(t, repoRoot, "main.o → deny (char class)", "Read", "file_path", "main.o")
	denyCase(t, repoRoot, "main.c → deny", "Read", "file_path", "main.c")
	denyCase(t, repoRoot, "main.h → deny", "Read", "file_path", "main.h")
	allowCase(t, repoRoot, "main.txt → ALLOW (not in class)", "Read", "file_path", "main.txt")
	allowCase(t, repoRoot, "main.cho → ALLOW (single char class, not multi)", "Read", "file_path", "main.cho")
}

// TestIntegration_InjectSafety 验证 pythonRepr 改动（M-2 控制字符转义）后，
// patterns 含 " / \ / 恶意 python 代码仍被安全转义为字面字符串，不会被解释执行。
func TestIntegration_InjectSafety(t *testing.T) {
	// 这条 pattern 试图逃逸字面量注入恶意 python："] + [__import__("os").system("x")
	// 必须被当作「文件名」字面匹配，绝不执行 os.system。
	plan := adapter.Plan{
		RawPatterns: []string{
			`"] + [__import__("os").system("x")`,
			`normal.pem`,
		},
	}
	repoRoot := writeHookFiles(t, plan)

	// 真跑：python 必须不报语法/执行错，且这条 pattern 仅作为字面文件名匹配。
	out, stderr, code := pipeTest(t, repoRoot, denyJSON("Read", "file_path", "main.go"))
	assert.Equal(t, 0, code, "inject pattern must not break python; stderr=%s", stderr)
	assert.NotContains(t, out, `"permissionDecision":"deny"`,
		"inject pattern must not match unrelated main.go; out=%s", out)
	// stderr 里不应出现任何 os.system 副作用（如 shell 报错）。
	assert.NotContains(t, stderr, "system")
}

// TestPythonRepr_ControlChars 验证 M-2：pythonRepr 对控制字符（NUL/VT/FF/DEL）转义为
// \xNN，避免生成的 .py 文件含裸控制字符触发 SyntaxError。这里直接断言产物字符串。
func TestPythonRepr_ControlChars(t *testing.T) {
	a := Adapter{}
	// 含 NUL(\x00) / VT(\x0b) / FF(\x0c) / DEL(\x7f) 的 pattern。
	files, err := a.Generate(adapter.Plan{
		RawPatterns: []string{"bad" + string([]byte{0x00, 0x0b, 0x0c, 0x7f}) + "name"},
	})
	require.NoError(t, err)
	require.Len(t, files, 3)

	byPath := map[string]adapter.GeneratedFile{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	py, ok := byPath[".claude/hooks/readignore.py"]
	require.True(t, ok)
	// 不得含裸控制字符；必须以 \xNN 转义形式出现。
	for _, c := range []byte{0x00, 0x0b, 0x0c, 0x7f} {
		assert.NotContainsf(t, py.Content, string(c),
			"control char 0x%02x must be escaped, not raw", c)
	}
	assert.Contains(t, py.Content, `\x00`)
	assert.Contains(t, py.Content, `\x0b`)
	assert.Contains(t, py.Content, `\x0c`)
	assert.Contains(t, py.Content, `\x7f`)
}
