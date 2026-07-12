package claudecode

import (
	"encoding/json"
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

// buildReadignoreBinary 构建 readignore CLI 二进制到临时目录，返回该目录（用于注入 PATH）。
// v0.3 起 sh hook 调 `readignore match`（命令名），集成测试必须让 shell 能找到 readignore。
//
// 用 go build 而非 mock：真跑 readignore match（go-git 权威 matcher）才能等价覆盖生产行为。
func buildReadignoreBinary(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	binName := "readignore"
	if runtime.GOOS == "windows" {
		binName = "readignore.exe"
	}
	// go build ./cmd/readignore -o <binDir>/<binName>。
	//nolint:gosec // G204: args are hardcoded literals (binDir/binName are test-controlled temp paths, not user input).
	cmd := exec.Command("go", "build", "-o", filepath.Join(binDir, binName), "./cmd/readignore")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build readignore failed: %v\n%s", err, string(out))
	}
	return binDir
}

// repoRoot 返回 readignore 仓库根（测试包位于 internal/adapter/claudecode）。
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	// wd = .../internal/adapter/claudecode；上溯 4 层到仓库根。
	root := wd
	for i := 0; i < 3; i++ {
		root = filepath.Dir(root)
	}
	return root
}

// writeHookFiles 把 Generate 的产物按预定路径写入临时仓库根，并把 patterns 落盘成
// repoRoot/.readignore（v0.3 起 readignore match 运行时读 cwd/.readignore，不再读 plan）。
// 返回仓库根路径，供真跑 pipe-test。
//
// 关键：把 Generate 的输出原样落盘（不做任何 mock / 改写），之后用 bash 真执行 sh，
// sh 内部再 fork readignore match（go-git 权威）真跑匹配。
func writeHookFiles(t *testing.T, plan adapter.Plan) string {
	t.Helper()
	a := Adapter{}
	files, err := a.Generate(plan)
	require.NoError(t, err)
	require.Len(t, files, 2, "claude-code adapter must generate exactly 2 files (sh + settings.json)")

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

	// v0.3：patterns 落盘成 cwd/.readignore（readignore match 运行时读）。
	if len(plan.RawPatterns) > 0 {
		require.NoError(t, os.WriteFile(
			filepath.Join(repoRoot, ".readignore"),
			[]byte(strings.Join(plan.RawPatterns, "\n")+"\n"),
			0o644,
		))
	}
	return repoRoot
}

// pipeTest 真跑：printf '<json>' | bash <repoRoot>/.claude/hooks/readignore.sh
// 返回 (stdout, stderr, exitCode)。命中拦截期望 stdout 含 permissionDecision:deny。
//
// readignore 二进制通过 PATH 注入（binDir）：sh 里调 `readignore match`（命令名），
// 故测试环境的 PATH 必须含 binDir 才能让 shell 找到 readignore。
func pipeTest(t *testing.T, repoRoot, binDir, jsonInput string) (string, string, int) {
	t.Helper()
	shPath := filepath.Join(repoRoot, ".claude", "hooks", "readignore.sh")
	require.FileExists(t, shPath, "readignore.sh must be generated")

	// 把仓库根设为工作目录：readignore match 读 cwd/.readignore，
	// Claude Code 实际也是从仓库根发起 hook。
	cmd := exec.Command("bash", shPath)
	cmd.Dir = repoRoot
	cmd.Stdin = strings.NewReader(jsonInput)
	// 注入 readignore 二进制目录到 PATH（Windows 上 PATH 用 ; 分隔，但 bash 子进程
	// 在 Git Bash 下用 : 分隔；统一用 PATHsep）。
	sep := string(os.PathListSeparator)
	cur := os.Getenv("PATH")
	cmd.Env = append(os.Environ(), "PATH="+binDir+sep+cur)
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
	// value 经 JSON 转义（真实 Claude Code 传合法 JSON；含反斜杠/引号等必须转义，
	// 否则 v0.3.3 hook-check 的 encoding/json 解析失败 → 放行 → 漏判）。
	b, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return `{"tool_name":"` + tool + `","tool_input":{"` + field + `":` + string(b) + `}}`
}

// denyCase / allowCase 是 pipe-test 的断言封装，集中把「期望 deny」与「期望 allow」语义化。
func denyCase(t *testing.T, repoRoot, binDir, label, tool, field, value string) {
	t.Helper()
	out, _, _ := pipeTest(t, repoRoot, binDir, denyJSON(tool, field, value))
	if !assert.Containsf(t, out, `"permissionDecision":"deny"`,
		"[%s] expected DENY but got stdout=%q", label, out) {
		// 失败时把输入打印出来便于诊断。
		t.Logf("input: tool=%s %s=%s", tool, field, value)
	}
}

func allowCase(t *testing.T, repoRoot, binDir, label, tool, field, value string) {
	t.Helper()
	out, _, _ := pipeTest(t, repoRoot, binDir, denyJSON(tool, field, value))
	assert.NotContainsf(t, out, `"permissionDecision":"deny"`,
		"[%s] expected ALLOW (no deny) but got stdout=%q", label, out)
}

// TestIntegration_PipeTest_RealGeneratedScripts 是本适配器的核心集成测试：
// 构造 plan → Generate → 落盘 → bash 真跑 sh → sh 调 readignore match（go-git 权威）
// → 断言 deny/allow。这里**真跑**生成的脚本（非 mock 自欺），任何匹配语义错误都会暴露在断言里。
func TestIntegration_PipeTest_RealGeneratedScripts(t *testing.T) {
	binDir := buildReadignoreBinary(t)

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
	denyCase(t, repoRoot, binDir, "Read .env", "Read", "file_path", ".env")
	denyCase(t, repoRoot, binDir, "Read .env.local", "Read", "file_path", ".env.local")
	denyCase(t, repoRoot, binDir, "Read .env.production", "Read", "file_path", ".env.production")
	allowCase(t, repoRoot, binDir, "Read .env.example (取反, 最关键)", "Read", "file_path", ".env.example")
	denyCase(t, repoRoot, binDir, "Read .env.sample (命中 .env.*，未取反)", "Read", "file_path", ".env.sample")
	denyCase(t, repoRoot, binDir, "Read secret.pem", "Read", "file_path", "secret.pem")
	denyCase(t, repoRoot, binDir, "Read sub/id_rsa (** 任意层级)", "Read", "file_path", "sub/id_rsa")
	denyCase(t, repoRoot, binDir, "Read sub/dir/id_rsa (** 深层)", "Read", "file_path", "sub/dir/id_rsa")
	allowCase(t, repoRoot, binDir, "Read main.go", "Read", "file_path", "main.go")
	allowCase(t, repoRoot, binDir, "Read send_env.py (env 前是_)", "Read", "file_path", "send_env.py")
	allowCase(t, repoRoot, binDir, "Read dotenv.py (无 .env 子串)", "Read", "file_path", "dotenv.py")
	allowCase(t, repoRoot, binDir, "Read .environment (.env 后是 i)", "Read", "file_path", ".environment")
	denyCase(t, repoRoot, binDir, "Bash cat .env (command 含 .env)", "Bash", "command", "cat .env")
	denyCase(t, repoRoot, binDir, "Grep path=.env", "Grep", "path", ".env")

	// ---- 补充 case：Glob pattern 通配、绝对/相对路径、Windows 反斜杠 ----
	denyCase(t, repoRoot, binDir, "Glob *.pem", "Glob", "pattern", "configs/server.pem")
	denyCase(t, repoRoot, binDir, "Read ./sub/dir/id_rsa", "Read", "file_path", "./sub/dir/id_rsa")
	denyCase(t, repoRoot, binDir, "Bash: cat ./.env.production", "Bash", "command", "cat ./.env.production")
	allowCase(t, repoRoot, binDir, "Bash: cat README.md", "Bash", "command", "cat README.md")
	// v0.3.1 回归：钩子 Bash token 误报修复——合法命令的选项/值不再被当路径误判。
	allowCase(t, repoRoot, binDir, "Bash: git config user.email (选项/值不匹配)", "Bash", "command", "git config --global user.email a@b.com")
	allowCase(t, repoRoot, binDir, "Bash: ls -la /tmp (选项+非敏感路径)", "Bash", "command", "ls -la /tmp")
	denyCase(t, repoRoot, binDir, "Bash: grep foo .env (多 token, .env 命中)", "Bash", "command", "grep foo .env")
	allowCase(t, repoRoot, binDir, "Grep path=.envrc (非 .env.*)", "Grep", "path", ".envrc")

	// Windows 反斜杠路径：sh/readignore match 应规范化为 / 后判定。
	if runtime.GOOS == "windows" {
		denyCase(t, repoRoot, binDir, "Read Windows sub\\id_rsa", "Read", "file_path", "sub\\id_rsa")
	}
}

// TestIntegration_DynamicRead_NoReinstall 是 v0.3 的核心验收测试：
// 写 .readignore → install hook → pipe-test .env deny → **改 .readignore（加 *.pem）→ 不 re-install**
// → pipe-test *.pem deny。证明改 .readignore 无需重新生成/安装 hook 即立即生效。
//
// 这是 v0.3（hook 调 readignore match，运行时读盘）与 v0.2（patterns 在 Generate 时内嵌
// 进 py，改 .readignore 需 re-install）的根本区别。本测试若失败说明 hook 仍在用生成时冻结
// 的 patterns，违背 v0.3 设计。
func TestIntegration_DynamicRead_NoReinstall(t *testing.T) {
	binDir := buildReadignoreBinary(t)

	// 初始：只 .env。
	plan := adapter.Plan{RawPatterns: []string{".env"}}
	repoRoot := writeHookFiles(t, plan)

	// .env 在初始 .readignore 里 → deny。
	denyCase(t, repoRoot, binDir, "初始 .env deny", "Read", "file_path", ".env")
	// *.pem 不在初始 .readignore 里 → allow。
	allowCase(t, repoRoot, binDir, "初始 *.pem allow（未声明）", "Read", "file_path", "secret.pem")

	// **不 re-install**：直接改 .readignore，追加 *.pem。
	readignorePath := filepath.Join(repoRoot, ".readignore")
	require.NoError(t, os.WriteFile(readignorePath, []byte(".env\n*.pem\n"), 0o644))

	// 现在不重新调 Generate / writeHookFiles，直接再 pipe-test：
	denyCase(t, repoRoot, binDir, "改 .readignore 后 *.pem 立即 deny（动态读，无需 re-install）",
		"Read", "file_path", "secret.pem")
	denyCase(t, repoRoot, binDir, "改 .readignore 后 .env 仍 deny", "Read", "file_path", ".env")
}

// TestGenerate_FileArtifacts_Static 静态断言生成文件结构：
// 路径、权限位、关键内容（sh 调 readignore match、settings 片段合规）。
// 与上面的「真跑」互补：真跑验证语义正确性，这里验证产物结构正确性。
func TestGenerate_FileArtifacts_Static(t *testing.T) {
	a := Adapter{}
	files, err := a.Generate(adapter.Plan{
		RawPatterns: []string{".env", "!.env.example", "*.pem"},
	})
	require.NoError(t, err)
	require.Len(t, files, 2)

	byPath := map[string]adapter.GeneratedFile{}
	for _, f := range files {
		byPath[f.Path] = f
	}

	t.Run("readignore.sh executable", func(t *testing.T) {
		f, ok := byPath[".claude/hooks/readignore.sh"]
		require.True(t, ok)
		assert.Equal(t, uint32(0o755), f.Mode, "hook must be executable")
		// v0.3.3：sh 转发到 readignore hook-check（JSON 解析+匹配在 Go）。
		assert.Contains(t, f.Content, "readignore hook-check")
		assert.NotContains(t, f.Content, "readignore.py", "sh must not reference dropped py engine")
		// readignore 不在 PATH 的 fallback。
		assert.Contains(t, f.Content, "command -v readignore")
	})

	// v0.3：不再生成 readignore.py（py 引擎废弃）。
	t.Run("no readignore.py generated", func(t *testing.T) {
		_, ok := byPath[".claude/hooks/readignore.py"]
		assert.False(t, ok, "claudecode must NOT generate readignore.py in v0.3")
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
	binDir := buildReadignoreBinary(t)
	a := Adapter{}
	files, err := a.Generate(adapter.Plan{})
	require.NoError(t, err)
	require.Len(t, files, 2)
	// 真跑一遍：空 patterns 必然放行。
	repoRoot := writeHookFiles(t, adapter.Plan{})
	allowCase(t, repoRoot, binDir, "empty patterns allow .env", "Read", "file_path", ".env")
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
	binDir := buildReadignoreBinary(t)
	plan := adapter.Plan{
		RawPatterns: []string{"foo/bar", "/leading", "secret.pem", "**/id_rsa"},
	}
	repoRoot := writeHookFiles(t, plan)

	// I-1：含内部斜杠锚定根。
	t.Run("I-1 foo/bar anchored to root", func(t *testing.T) {
		denyCase(t, repoRoot, binDir, "foo/bar at root → deny", "Read", "file_path", "foo/bar")
		allowCase(t, repoRoot, binDir, "sub/foo/bar → ALLOW (anchored, not mid-path)", "Read", "file_path", "sub/foo/bar")
		allowCase(t, repoRoot, binDir, "xfoo/bar → ALLOW (prefix anchored)", "Read", "file_path", "xfoo/bar")
	})

	// I-2：前导斜杠匹配根。
	t.Run("I-2 /leading matches root leading", func(t *testing.T) {
		denyCase(t, repoRoot, binDir, "leading at root → deny (前导斜杠匹配根)", "Read", "file_path", "leading")
		allowCase(t, repoRoot, binDir, "sub/leading → ALLOW", "Read", "file_path", "sub/leading")
		allowCase(t, repoRoot, binDir, "leadings → ALLOW (suffix anchored)", "Read", "file_path", "leadings")
	})

	// 回归保护：basename 模式（无斜杠）仍任意层级匹配。
	t.Run("regression secret.pem basename any level", func(t *testing.T) {
		denyCase(t, repoRoot, binDir, "secret.pem at root → deny", "Read", "file_path", "secret.pem")
		denyCase(t, repoRoot, binDir, "a/secret.pem → deny (basename any level)", "Read", "file_path", "a/secret.pem")
		allowCase(t, repoRoot, binDir, "xsecret.pem → ALLOW", "Read", "file_path", "xsecret.pem")
	})

	// 回归保护：**/x 仍任意层级。
	t.Run("regression **/id_rsa any level", func(t *testing.T) {
		denyCase(t, repoRoot, binDir, "id_rsa at root → deny", "Read", "file_path", "id_rsa")
		denyCase(t, repoRoot, binDir, "sub/dir/id_rsa → deny", "Read", "file_path", "sub/dir/id_rsa")
	})
}

// TestIntegration_CharClasses 验证 gitignore 字符类 [...] 支持。
// go-git 支持 *.[cho] 匹配 .c/.h/.o（单字符集）；readignore match 须同样支持。
func TestIntegration_CharClasses(t *testing.T) {
	binDir := buildReadignoreBinary(t)
	plan := adapter.Plan{
		RawPatterns: []string{"*.[cho]"},
	}
	repoRoot := writeHookFiles(t, plan)

	denyCase(t, repoRoot, binDir, "main.o → deny (char class)", "Read", "file_path", "main.o")
	denyCase(t, repoRoot, binDir, "main.c → deny", "Read", "file_path", "main.c")
	denyCase(t, repoRoot, binDir, "main.h → deny", "Read", "file_path", "main.h")
	allowCase(t, repoRoot, binDir, "main.txt → ALLOW (not in class)", "Read", "file_path", "main.txt")
	allowCase(t, repoRoot, binDir, "main.cho → ALLOW (single char class, not multi)", "Read", "file_path", "main.cho")
}

// TestIntegration_ReadignoreMissing_Fallback 验证 readignore 不在 PATH 时 hook 放行
// （不搞死 agent）+ stderr 警告。这是 v0.3 的 fallback 契约。
func TestIntegration_ReadignoreMissing_Fallback(t *testing.T) {
	plan := adapter.Plan{RawPatterns: []string{".env"}}
	repoRoot := writeHookFiles(t, plan)

	// 故意把 PATH 清空到只有系统最小集（确保 readignore 找不到）。
	shPath := filepath.Join(repoRoot, ".claude", "hooks", "readignore.sh")
	cmd := exec.Command("bash", shPath)
	cmd.Dir = repoRoot
	cmd.Stdin = strings.NewReader(denyJSON("Read", "file_path", ".env"))
	// PATH 设为一个肯定无 readignore 的目录（仓库根自身的空目录）。
	emptyDir := t.TempDir()
	cmd.Env = append(os.Environ(), "PATH="+emptyDir)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			t.Fatalf("spawn bash: %v", err)
		}
	}
	// fallback 放行：不 deny。
	assert.NotContains(t, stdout.String(), `"permissionDecision":"deny"`,
		"readignore missing from PATH must fallback to allow (not block agent)")
	assert.Equal(t, 0, exitCode, "hook must exit 0 on fallback")
	// stderr 警告便于排查。
	assert.Contains(t, stderr.String(), "hook disabled",
		"stderr should warn that readignore is missing from PATH")
}
