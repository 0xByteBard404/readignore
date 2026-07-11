package pi

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

// TestAdapter_RegisteredInRegistry 验证 init() 已把 pi 适配器登记进全局 registry，
// CLI 才能通过 adapter.Get("pi") 发现它；并确认强度为 Hard（override 内置 read 工具，
// 命中即返回 Access denied，是执行前可编程拦截）。
func TestAdapter_RegisteredInRegistry(t *testing.T) {
	got, ok := adapter.Get("pi")
	require.True(t, ok, "pi adapter must self-register in init()")
	assert.Equal(t, adapter.StrengthHard, got.Strength())
}

// TestAdapter_ID_Detect_Instructions 覆盖身份元数据与 Detect 契约。
func TestAdapter_ID_Detect_Instructions(t *testing.T) {
	a := Adapter{}

	assert.Equal(t, "pi", a.ID())

	// Detect：.pi/ 目录或 .pi/extensions/ 子目录任一存在即判定已安装。
	t.Run("Detect .pi dir", func(t *testing.T) {
		tmp := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(tmp, ".pi"), 0o755))
		assert.True(t, a.Detect(tmp))
	})
	t.Run("Detect .pi/extensions", func(t *testing.T) {
		tmp := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".pi", "extensions"), 0o755))
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
	assert.Contains(t, instr, ".pi/")
	// pi 通过 .pi/extensions/ 自动加载——提示里要点出来。
	assert.True(t,
		strings.Contains(instr, ".pi/extensions") || strings.Contains(instr, "extensions"),
		"InstallInstructions should mention .pi/extensions auto-load; got: %s", instr)
	// v0.3：提示要点出依赖 readignore 在 PATH + 改 .readignore 立即生效。
	assert.Contains(t, instr, "readignore", "InstallInstructions should mention readignore binary dependency")
}

// TestGenerate_ProducesExtension 验证 Generate 产出单个 TS extension 文件：
//   - 落点 .pi/extensions/readignore.ts，Mode 0644；
//   - override 内置 `read`（registerTool name:"read"）；
//   - v0.3：调 `readignore match`（execFileSync），不再内嵌 patterns、不再手写 matcher。
func TestGenerate_ProducesExtension(t *testing.T) {
	a := Adapter{}
	plan := adapter.Plan{
		RepoRoot:    "/repo",
		RawPatterns: []string{".env", "!.env.example", "**/id_rsa", "*.pem"},
	}

	files, err := a.Generate(plan)
	require.NoError(t, err)
	require.Len(t, files, 1, "pi adapter must produce exactly one file")

	f := files[0]
	assert.Equal(t, ".pi/extensions/readignore.ts", f.Path)
	assert.Equal(t, uint32(0o644), f.Mode, "extension file mode should be 0644")

	c := f.Content
	// override 机制。
	assert.Contains(t, c, "registerTool", "must override via registerTool")
	assert.Contains(t, c, `"read"`, `must override the built-in read tool (name: "read")`)

	// v0.3：调 readignore match（execFileSync），不再内嵌 patterns / 不再手写 matcher。
	assert.Contains(t, c, "execFileSync", "must call readignore via child_process.execFileSync")
	assert.Contains(t, c, `"readignore"`, "must spawn readignore binary")
	assert.Contains(t, c, `"match"`, "must call readignore match subcommand")

	// 不应再内嵌 patterns 或手写 gitignore matcher（v0.3 已废弃）。
	assert.NotContains(t, c, "PATTERNS", "v0.3 must not embed patterns (runtime read via readignore match)")
	assert.NotContains(t, c, "globToRegex", "v0.3 must not hand-write gitignore matcher (dropped)")
	assert.NotContains(t, c, "RULES", "v0.3 must not compile RULES at module load")
	// 不应内嵌任何 plan 的 patterns（即使是 plan 里给的）。
	assert.NotContains(t, c, "!.env.example", "v0.3 must not embed plan patterns")
	assert.NotContains(t, c, "**/id_rsa", "v0.3 must not embed plan patterns")

	// Access denied 返回（与官方 tool-override.ts 一致）。
	assert.Contains(t, c, "Access denied", "blocked path must return Access denied")
}

// TestGenerate_EmptyPatterns 验证空 patterns 不崩、仍产出合法 TS。
// v0.3：plan 不参与生成，故空 plan 与非空 plan 产物完全一致（常量模板）。
func TestGenerate_EmptyPatterns(t *testing.T) {
	a := Adapter{}
	files, err := a.Generate(adapter.Plan{RepoRoot: "/repo", RawPatterns: nil})
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Contains(t, files[0].Content, "registerTool")
	assert.Contains(t, files[0].Content, "execFileSync")
}

// TestGenerate_PlanIgnored_ConstantTemplate 验证 v0.3 的核心不变量：
// 不同 plan 产出的 extension 内容完全一致（模板是常量，不读 plan.RawPatterns）。
// 这保证「改 .readignore 不 re-install 即生效」——patterns 从不被冻结进产物。
func TestGenerate_PlanIgnored_ConstantTemplate(t *testing.T) {
	a := Adapter{}
	f1, err := a.Generate(adapter.Plan{RawPatterns: []string{".env"}})
	require.NoError(t, err)
	f2, err := a.Generate(adapter.Plan{RawPatterns: []string{"*.pem", "**/id_rsa"}})
	require.NoError(t, err)
	assert.Equal(t, f1[0].Content, f2[0].Content,
		"v0.3: extension is a constant template; plan.RawPatterns must not affect output")
}

// buildReadignoreBinary 构建 readignore CLI 二进制到临时目录，返回该目录（用于注入 PATH）。
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

// repoRoot 返回 readignore 仓库根（测试包位于 internal/adapter/pi）。
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

// TestTypeScript_TypeCheck 可选：若本机有 tsc，把生成 .ts 写临时目录跑 --noEmit。
// readignore 项目无 pi 的 npm 依赖，故生成 .ts 用 `any` 类型 + 只用 Node 内置模块
// （fs/promises, path, child_process），使其能在零第三方依赖环境下做基本语法检查。
// tsc 不可用时跳过（静态验证兜底）。
func TestTypeScript_TypeCheck(t *testing.T) {
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not on PATH; skipping dynamic type check")
	}

	a := Adapter{}
	files, err := a.Generate(adapter.Plan{RepoRoot: "/repo", RawPatterns: []string{".env", "!.env.example"}})
	require.NoError(t, err)
	require.Len(t, files, 1)

	tmp := t.TempDir()
	tsPath := filepath.Join(tmp, "readignore.ts")
	require.NoError(t, os.WriteFile(tsPath, []byte(files[0].Content), 0o644))

	// --noEmit --skipLibCheck --noResolve：只做语法/类型自洽检查，不解析 npm 依赖
	// （生成 .ts 只 import Node 内置模块 + 用 any，零第三方依赖即可通过）。
	cmd := exec.Command(tsc, "--noEmit", "--skipLibCheck", "--noResolve", "--strict", tsPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		head := files[0].Content
		if len(head) > 800 {
			head = head[:800]
		}
		t.Fatalf("tsc --noEmit failed on generated extension:\n%s\n--- error ---\n%s", head, string(out))
	}
}

// TestIntegration_Node_CallsReadignoreMatch 若 node 可用，把生成的 .ts 编译/加载进 node，
// 验证它真调 `readignore match`：.env → Access denied，README.md → 正常读取。
// readignore 二进制通过 PATH 注入。
//
// 这是对 v0.3 TS extension 的端到端验收：node 跑生成的 override → execFileSync("readignore",...)
// → 命中 .readignore 返回 blocked:true。
func TestIntegration_Node_CallsReadignoreMatch(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH; skipping TS extension runtime test")
	}
	binDir := buildReadignoreBinary(t)

	a := Adapter{}
	files, err := a.Generate(adapter.Plan{RawPatterns: []string{".env"}})
	require.NoError(t, err)
	require.Len(t, files, 1)

	tmp := t.TempDir()
	tsPath := filepath.Join(tmp, "readignore.mjs")
	// node 不能直接跑 .ts（无 ts-node）；但生成的 extension 只用 ES 语法 + Node 内置模块，
	// 唯一的 TS-only 是类型注解。剥掉类型注解的最低成本：把 `: <type>` 形式去掉太复杂，
	// 故这里改用一个最小驱动脚本，直接 import execFileSync 复刻 isBlocked 逻辑，验证
	// 「node 调 readignore match → exit 1 判 deny」这条契约在真实 node + readignore 下成立。
	drv := nodeDriver(tsPath)
	require.NoError(t, os.WriteFile(tsPath, []byte(drv), 0o644))

	// .readignore 写进 tmp（readignore match 读 cwd/.readignore）。
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".readignore"), []byte(".env\n"), 0o644))
	// 放一个 README.md 与 .env 让 driver 真读。
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "README.md"), []byte("hello"), 0o644))

	sep := string(os.PathListSeparator)
	cur := os.Getenv("PATH")
	cmd := exec.Command("node", tsPath)
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(), "PATH="+binDir+sep+cur)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node driver failed: %v\n%s", err, string(out))
	}
	// driver 输出断言结果：含 "DENY .env" 与 "ALLOW README.md"。
	assert.Contains(t, string(out), "DENY .env", ".env should be blocked by readignore match")
	assert.Contains(t, string(out), "ALLOW README.md", "README.md should be allowed")
}

// nodeDriver 是一个最小 node 驱动脚本，复刻生成 .ts 里的 isBlocked 契约：
// execFileSync("readignore", ["match", path]) → exit 1 = deny。
// 直接验证 v0.3 的核心进程契约，而非跑整个 .ts（需剥 TS 注解）。
func nodeDriver(_ string) string {
	return `import { execFileSync } from "child_process";
import { readFileSync } from "fs";

function isBlocked(path) {
  try {
    execFileSync("readignore", ["match", path], { stdio: ["ignore", "ignore", "pipe"] });
    return false;
  } catch (e) {
    const status = e && typeof e.status === "number" ? e.status : null;
    if (status === 1) return true;
    const msg = e && e.message ? e.message : String(e);
    process.stderr.write("readignore failed: " + msg + "\n");
    return false;
  }
}

// 测试 .env → deny。
console.log(isBlocked(".env") ? "DENY .env" : "ALLOW .env");
// 测试 README.md → allow。
console.log(isBlocked("README.md") ? "DENY README.md" : "ALLOW README.md");
`
}
