package codex

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

// TestAdapter_RegisteredInRegistry 验证 init() 已把 codex 适配器登记进全局 registry，
// CLI 才能通过 adapter.Get("codex") 发现它；并确认强度为 Hard（PreToolUse 执行前拦截）。
func TestAdapter_RegisteredInRegistry(t *testing.T) {
	got, ok := adapter.Get("codex")
	require.True(t, ok, "codex adapter must self-register in init()")
	assert.Equal(t, adapter.StrengthHard, got.Strength())
}

// TestAdapter_ID_Detect_Instructions 覆盖身份元数据与 Detect 契约。
func TestAdapter_ID_Detect_Instructions(t *testing.T) {
	a := Adapter{}

	assert.Equal(t, "codex", a.ID())

	// Detect：.codex/ 目录或 AGENTS.md 任一存在即判定已安装。
	t.Run("Detect .codex dir", func(t *testing.T) {
		tmp := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(tmp, ".codex"), 0o755))
		assert.True(t, a.Detect(tmp))
	})
	t.Run("Detect AGENTS.md", func(t *testing.T) {
		tmp := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte("x"), 0o644))
		assert.True(t, a.Detect(tmp))
	})
	t.Run("Detect absent", func(t *testing.T) {
		tmp := t.TempDir()
		assert.False(t, a.Detect(tmp))
	})
	t.Run("Detect empty repoRoot", func(t *testing.T) {
		assert.False(t, a.Detect(""))
	})

	instr := a.InstallInstructions()
	assert.NotEmpty(t, instr)
	assert.Contains(t, instr, ".codex/")
	// codex 首次需信任 hook 或绕过信任检查——提示里要点出来。
	assert.True(t,
		strings.Contains(instr, "信任") || strings.Contains(instr, "trust") ||
			strings.Contains(instr, "bypass"),
		"InstallInstructions should mention hook trust; got: %s", instr)
}

// buildReadignoreBinary 构建 readignore CLI 二进制到临时目录，返回该目录（用于注入 PATH）。
// v0.3 起 sh hook 调 `readignore match`（命令名），集成测试必须让 shell 能找到 readignore。
func buildReadignoreBinary(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	binName := "readignore"
	if runtime.GOOS == "windows" {
		binName = "readignore.exe"
	}
	//nolint:gosec // G204: args are hardcoded literals (binDir/binName are test-controlled temp paths, not user input).
	cmd := exec.Command("go", "build", "-o", filepath.Join(binDir, binName), "./cmd/readignore")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build readignore failed: %v\n%s", err, string(out))
	}
	return binDir
}

// repoRoot 返回 readignore 仓库根（测试包位于 internal/adapter/codex）。
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	root := wd
	for i := 0; i < 3; i++ {
		root = filepath.Dir(root)
	}
	return root
}

// TestGenerate_ProducesCodexHookFiles 验证 Generate 产出 2 个文件：
// 路径、权限位正确（sh 0755 / hooks.json 0），hooks.json 合法且含
// PreToolUse + matcher + command 指向 .codex/hooks/readignore.sh。
func TestGenerate_ProducesCodexHookFiles(t *testing.T) {
	a := Adapter{}
	files, err := a.Generate(adapter.Plan{
		RawPatterns: []string{".env", "!.env.example"},
	})
	require.NoError(t, err)
	require.Len(t, files, 2, "codex adapter must generate exactly 2 files (sh + hooks.json)")

	byPath := map[string]adapter.GeneratedFile{}
	for _, f := range files {
		byPath[f.Path] = f
	}

	t.Run("readignore.sh executable 0755", func(t *testing.T) {
		f, ok := byPath[".codex/hooks/readignore.sh"]
		require.True(t, ok, "must produce .codex/hooks/readignore.sh")
		assert.Equal(t, uint32(0o755), f.Mode, "hook must be executable")
		// v0.3.3：sh 转发到 readignore hook-check（JSON 解析+匹配在 Go）。
		assert.Contains(t, f.Content, "readignore hook-check")
		assert.NotContains(t, f.Content, "readignore.py", "sh must not reference dropped py engine")
	})

	// v0.3：不再生成 readignore.py（py 引擎废弃）。
	t.Run("no readignore.py generated", func(t *testing.T) {
		_, ok := byPath[".codex/hooks/readignore.py"]
		assert.False(t, ok, "codex must NOT generate readignore.py in v0.3")
	})

	t.Run("hooks.json valid + PreToolUse + matcher + command", func(t *testing.T) {
		f, ok := byPath[".codex/hooks.json"]
		require.True(t, ok, "must produce .codex/hooks.json")
		assert.Equal(t, uint32(0), f.Mode, "hooks.json: caller uses default")

		// 必须是合法 JSON。
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(f.Content), &parsed), "hooks.json must be valid JSON")

		hooks, ok := parsed["hooks"].(map[string]any)
		require.True(t, ok, "hooks.json top-level must have \"hooks\" object")

		pre, ok := hooks["PreToolUse"].([]any)
		require.True(t, ok, "must contain PreToolUse matcher group array")
		require.Len(t, pre, 1, "PreToolUse should have exactly one matcher group")

		group, ok := pre[0].(map[string]any)
		require.True(t, ok)

		// matcher：codex 用 exact pipe 语法（Read|Grep|Glob|Bash），须含 Bash。
		matcher, _ := group["matcher"].(string)
		assert.Contains(t, matcher, "Bash", "matcher must cover Bash tool")
		assert.Contains(t, matcher, "Read", "matcher should cover Read")

		hs, ok := group["hooks"].([]any)
		require.True(t, ok, "matcher group must have hooks array")
		require.Len(t, hs, 1)
		h, ok := hs[0].(map[string]any)
		require.True(t, ok)

		// type=command、command 指向 .codex/hooks/readignore.sh。
		assert.Equal(t, "command", h["type"], "handler type must be command")
		cmd, _ := h["command"].(string)
		assert.Contains(t, cmd, ".codex/hooks/readignore.sh",
			"command must invoke .codex/hooks/readignore.sh; got %q", cmd)

		// timeout 字段：codex schema 用 "timeout"（序列化名，对应 Rust timeout_sec 字段）。
		// 不应出现 codex 不识别的 "shell" 字段（codex HookHandlerConfig 无此字段）。
		_, hasShell := h["shell"]
		assert.False(t, hasShell, "codex hooks.json must NOT include unsupported \"shell\" field")
	})
}

// writeHookFiles 把 Generate 的产物原样落盘到临时仓库根，并把 patterns 落盘成
// repoRoot/.readignore。返回 repoRoot，供真跑 pipe-test。
func writeHookFiles(t *testing.T, plan adapter.Plan) string {
	t.Helper()
	a := Adapter{}
	files, err := a.Generate(plan)
	require.NoError(t, err)
	require.Len(t, files, 2)

	repoRoot := t.TempDir()
	for _, f := range files {
		abs := filepath.Join(repoRoot, filepath.FromSlash(f.Path))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		mode := os.FileMode(f.Mode)
		if mode == 0 {
			mode = 0o644
		}
		require.NoError(t, os.WriteFile(abs, []byte(f.Content), mode))
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

// pipeTest 真跑：printf '<json>' | bash <repoRoot>/.codex/hooks/readignore.sh
// readignore 二进制通过 PATH 注入（binDir）。
func pipeTest(t *testing.T, repoRoot, binDir, jsonInput string) (string, string, int) {
	t.Helper()
	shPath := filepath.Join(repoRoot, ".codex", "hooks", "readignore.sh")
	require.FileExists(t, shPath)

	cmd := exec.Command("bash", shPath)
	cmd.Dir = repoRoot
	cmd.Stdin = strings.NewReader(jsonInput)
	sep := string(os.PathListSeparator)
	cur := os.Getenv("PATH")
	cmd.Env = append(os.Environ(), "PATH="+binDir+sep+cur)
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
	return stdout.String(), stderr.String(), exitCode
}

func denyJSON(tool, field, value string) string {
	// value 经 JSON 转义（与 claudecode_test 一致；含反斜杠/引号等必须转义，
	// 否则 hook-check 的 encoding/json 解析失败 → 放行 → 漏判）。
	b, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return `{"tool_name":"` + tool + `","tool_input":{"` + field + `":` + string(b) + `}}`
}

func denyCase(t *testing.T, repoRoot, binDir, label, tool, field, value string) {
	t.Helper()
	out, _, _ := pipeTest(t, repoRoot, binDir, denyJSON(tool, field, value))
	if !assert.Containsf(t, out, `"permissionDecision":"deny"`,
		"[%s] expected DENY but got stdout=%q", label, out) {
		t.Logf("input: tool=%s %s=%s", tool, field, value)
	}
}

func allowCase(t *testing.T, repoRoot, binDir, label, tool, field, value string) {
	t.Helper()
	out, _, _ := pipeTest(t, repoRoot, binDir, denyJSON(tool, field, value))
	assert.NotContainsf(t, out, `"permissionDecision":"deny"`,
		"[%s] expected ALLOW (no deny) but got stdout=%q", label, out)
}

// TestIntegration_PipeTest_RealGeneratedScripts 是核心集成测试：
// 构造 plan → Generate → 落盘 → bash 真跑 sh → sh 调 readignore match → 断言 deny/allow。
//
// codex 的 PreToolUse tool_input 字段名经源码确认：
//   - Bash 工具的 tool_input 为 {"command": "..."}（与 Claude Code 一致）；
//   - codex 同时把 tool_name 暴露为 "Bash"（matcher 用 exact pipe 语法）。
func TestIntegration_PipeTest_RealGeneratedScripts(t *testing.T) {
	binDir := buildReadignoreBinary(t)
	patterns := []string{".env", ".env.*", "!.env.example", "*.pem", "**/id_rsa", ".env.local", ".env.production"}

	plan := adapter.Plan{
		RepoRoot:     "/repo/root",
		RawPatterns:  patterns,
		MatchedPaths: nil,
	}
	repoRoot := writeHookFiles(t, plan)

	// codex 实际 hook 走 Bash 工具 + command 字段（源码 core/tests/suite/hooks.rs:3242 确认）。
	denyCase(t, repoRoot, binDir, "Bash cat .env (command 含 .env)", "Bash", "command", "cat .env")
	denyCase(t, repoRoot, binDir, "Bash cat .env.production", "Bash", "command", "cat .env.production")
	allowCase(t, repoRoot, binDir, "Bash cat .env.example (取反放行)", "Bash", "command", "cat .env.example")
	denyCase(t, repoRoot, binDir, "Bash cat .env.sample (命中 .env.* 未取反)", "Bash", "command", "cat .env.sample")
	allowCase(t, repoRoot, binDir, "Bash cat README.md", "Bash", "command", "cat README.md")

	// ** 任意层级：sub/dir/id_rsa 必须命中。
	denyCase(t, repoRoot, binDir, "Bash cat sub/dir/id_rsa (** 任意层级)", "Bash", "command", "cat sub/dir/id_rsa")

	// shared sh 也抽 file_path/path/pattern（Claude-style 工具名），
	// codex 若未来暴露 Read/Grep/Glob 同名工具，这些字段同样覆盖。
	denyCase(t, repoRoot, binDir, "Read .env (file_path)", "Read", "file_path", ".env")
	allowCase(t, repoRoot, binDir, "Read .env.example (取反)", "Read", "file_path", ".env.example")
	denyCase(t, repoRoot, binDir, "Read sub/id_rsa (** 任意层级)", "Read", "file_path", "sub/id_rsa")
	denyCase(t, repoRoot, binDir, "Grep path=.env", "Grep", "path", ".env")
	denyCase(t, repoRoot, binDir, "Glob pattern=configs/server.pem", "Glob", "pattern", "configs/server.pem")
}

// TestGenerate_EmptyPatterns 空 plan 不应崩溃，真跑必放行。
func TestGenerate_EmptyPatterns(t *testing.T) {
	binDir := buildReadignoreBinary(t)
	a := Adapter{}
	files, err := a.Generate(adapter.Plan{})
	require.NoError(t, err)
	require.Len(t, files, 2)
	repoRoot := writeHookFiles(t, adapter.Plan{})
	allowCase(t, repoRoot, binDir, "empty patterns allow .env", "Bash", "command", "cat .env")
}

// TestIntegration_DynamicRead_NoReinstall 是 v0.3 的核心验收测试（codex 版）：
// 改 .readignore 不 re-install → 立即生效。
func TestIntegration_DynamicRead_NoReinstall(t *testing.T) {
	binDir := buildReadignoreBinary(t)

	plan := adapter.Plan{RawPatterns: []string{".env"}}
	repoRoot := writeHookFiles(t, plan)

	denyCase(t, repoRoot, binDir, "初始 .env deny", "Bash", "command", "cat .env")
	allowCase(t, repoRoot, binDir, "初始 *.pem allow", "Bash", "command", "cat secret.pem")

	// 不 re-install，直接改 .readignore。
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, ".readignore"), []byte(".env\n*.pem\n"), 0o644))

	denyCase(t, repoRoot, binDir, "改 .readignore 后 *.pem 立即 deny", "Bash", "command", "cat secret.pem")
	denyCase(t, repoRoot, binDir, "改 .readignore 后 .env 仍 deny", "Bash", "command", "cat .env")
}
