package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

// TestAdapter_ID_Strength 覆盖适配器身份元数据契约。
func TestAdapter_ID_Strength(t *testing.T) {
	a := Adapter{}
	// ID 稳定、全小写、跨版本不变。
	assert.Equal(t, "opencode", a.ID())
	// opencode 走静态 permission config deny（permission.ask hook 当前不触发，issue #7006），
	// 故强度为 config（由工具加载配置后生效），非执行前硬拦。
	assert.Equal(t, adapter.StrengthConfig, a.Strength())
}

// TestAdapter_RegisteredInRegistry 验证 init() 已把本适配器登记进全局 registry，
// CLI 才能通过 adapter.All() / adapter.Get() 发现它。
func TestAdapter_RegisteredInRegistry(t *testing.T) {
	got, ok := adapter.Get("opencode")
	require.True(t, ok, "opencode adapter must self-register in init()")
	assert.Equal(t, adapter.StrengthConfig, got.Strength())
}

// TestAdapter_Detect 验证 Detect 探测 opencode 配置文件存在性。
func TestAdapter_Detect(t *testing.T) {
	a := Adapter{}

	t.Run("opencode.json present", func(t *testing.T) {
		tmp := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmp, "opencode.json"), []byte("{}"), 0o644))
		assert.True(t, a.Detect(tmp))
	})

	t.Run(".opencode dir present", func(t *testing.T) {
		tmp := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(tmp, ".opencode"), 0o755))
		// .opencode/ 目录也算 opencode 痕迹（opencode 支持该目录放配置）。
		assert.True(t, a.Detect(tmp))
	})

	t.Run("absent", func(t *testing.T) {
		tmp := t.TempDir()
		assert.False(t, a.Detect(tmp))
	})

	t.Run("empty repoRoot", func(t *testing.T) {
		assert.False(t, a.Detect(""))
	})
}

// TestAdapter_InstallInstructions 验证安装说明诚实标注限制：
// 走 config deny 路径、提示 permission.ask hook 当前不触发（issue #7006）。
func TestAdapter_InstallInstructions(t *testing.T) {
	a := Adapter{}
	instr := a.InstallInstructions()
	assert.NotEmpty(t, instr)
	assert.Contains(t, instr, "opencode.json")
	// 诚实标注：当前是 config 强度（非 hard）。
	assert.Contains(t, instr, "config")
	// 诚实标注：permission.ask hook 不触发的已知限制。
	assert.Contains(t, instr, "permission.ask")
}

// TestAdapter_Generate 生成单文件 opencode.json，含 permission.read deny 规则，
// JSON 语法必须合法（json.Unmarshal 验证）且把 RawPatterns 如数翻译成 deny glob。
func TestAdapter_Generate(t *testing.T) {
	a := Adapter{}
	patterns := []string{".env", "*.pem", "**/id_rsa", "secrets/"}
	files, err := a.Generate(adapter.Plan{
		RepoRoot:     "/some/repo",
		MatchedPaths: []string{".env", "deploy/key.pem"},
		RawPatterns:  patterns,
	})
	require.NoError(t, err)
	require.Len(t, files, 1, "opencode adapter produces exactly one opencode.json")

	f := files[0]
	assert.Equal(t, "opencode.json", f.Path)
	assert.Equal(t, uint32(0), f.Mode, "Mode 0 = use caller default (0644)")

	// JSON 必须语法合法。
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(f.Content), &doc), "generated config must be valid JSON")

	// 顶层有 $schema 指向 opencode 官方 schema，便于编辑器校验。
	schema, _ := doc["$schema"].(string)
	assert.Contains(t, schema, "opencode.ai/config.json")

	// 顶层有 permission 字段。
	perm, ok := doc["permission"].(map[string]any)
	require.True(t, ok, "permission key must be an object")

	// permission.read 是个 map[glob]string，每条 RawPattern 对应一条 deny。
	read, ok := perm["read"].(map[string]any)
	require.True(t, ok, "permission.read must be an object (glob → decision)")

	// 每条 RawPattern 都应映射到 "deny"。
	for _, p := range patterns {
		v, present := read[p]
		require.True(t, present, "pattern %q must appear as a key in permission.read", p)
		assert.Equal(t, "deny", v, "pattern %q must map to \"deny\"", p)
	}

	// 不应额外塞入未声明的工具（edit/webfetch/bash 等），保持最小集。
	assert.Len(t, perm, 1, "permission should only contain read key")
}

// TestAdapter_Generate_SkipsCommentAndBlank 确认 Generate 过滤掉注释行与空行，
// 不把它们写进 JSON 当作 glob（否则 opencode 会按字面匹配 '#'）。
func TestAdapter_Generate_SkipsCommentAndBlank(t *testing.T) {
	a := Adapter{}
	files, err := a.Generate(adapter.Plan{
		RawPatterns: []string{".env", "# a comment", "  ", "*.pem", ""},
	})
	require.NoError(t, err)
	require.Len(t, files, 1)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(files[0].Content), &doc))
	read := doc["permission"].(map[string]any)["read"].(map[string]any)
	// 只剩两条真实 pattern。
	assert.Len(t, read, 2)
	assert.Contains(t, read, ".env")
	assert.Contains(t, read, "*.pem")
	// 注释字符不应作为 key 出现。
	_, hasHash := read["# a comment"]
	assert.False(t, hasHash)
}

// TestAdapter_Generate_EmptyPatterns 仍产出合法 JSON（空 read map）。
func TestAdapter_Generate_EmptyPatterns(t *testing.T) {
	a := Adapter{}
	files, err := a.Generate(adapter.Plan{RawPatterns: nil})
	require.NoError(t, err)
	require.Len(t, files, 1)

	var doc map[string]any
	// JSON 必须合法，即便没有 pattern。
	require.NoError(t, json.Unmarshal([]byte(files[0].Content), &doc))
	perm := doc["permission"].(map[string]any)
	read, ok := perm["read"].(map[string]any)
	require.True(t, ok)
	assert.Empty(t, read)
}

// TestAdapter_Generate_RepoRootAgnostic Generate 产出的文件路径不依赖 RepoRoot
// （恒为 opencode.json），交给调用方按 RepoRoot 拼绝对路径。
func TestAdapter_Generate_RepoRootAgnostic(t *testing.T) {
	a := Adapter{}
	files, err := a.Generate(adapter.Plan{RepoRoot: "/x/y", RawPatterns: []string{".env"}})
	require.NoError(t, err)
	assert.Equal(t, "opencode.json", files[0].Path)

	files2, err := a.Generate(adapter.Plan{RepoRoot: "/totally/different", RawPatterns: []string{".env"}})
	require.NoError(t, err)
	assert.Equal(t, "opencode.json", files2[0].Path)
}

// TestAdapter_Generate_NegationBecomesAllow 验证 readignore 的取反行（!pattern）
// 在 opencode 适配器里被翻译成剥掉 ! 的 allow 键，而非字面 ! + deny（那是语义反转：
// readignore 的 ! 是放行，字面 !+deny 反而成了虚假保护）。opencode glob 不支持 !
// 前缀，本适配器依赖「更具体 glob 覆盖更宽泛」的机制实现放行（如 .env.example 比
// .env.* 更具体，会胜出），近似 readignore 的取反语义。
func TestAdapter_Generate_NegationBecomesAllow(t *testing.T) {
	a := Adapter{}
	files, err := a.Generate(adapter.Plan{
		// 典型用例：deny 全部 .env / .env.*，但放行示例文件 .env.example。
		RawPatterns: []string{".env", ".env.*", "!.env.example"},
	})
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(files[0].Content), &doc))
	read := doc["permission"].(map[string]any)["read"].(map[string]any)

	// 非取反行仍映射到 deny。
	assert.Equal(t, "deny", read[".env"], "plain pattern must map to deny")
	assert.Equal(t, "deny", read[".env.*"], "plain pattern must map to deny")

	// 取反行：剥掉前导 ! 后映射到 allow（不是 deny）。
	assert.Equal(t, "allow", read[".env.example"],
		"negated pattern must become an allow key (readignore ! = unblock)")

	// 关键：! 前缀必须剥干净，不能出现字面 "!pattern" 这个 key（opencode glob 不认 !）。
	_, hasBang := read["!.env.example"]
	assert.False(t, hasBang,
		"literal \"!pattern\" must not leak into the map; opencode glob has no negation")
}

// TestAdapter_Generate_NegationBecomesAllow_MultipleCases 覆盖多条取反 pattern，
// 确认 ! 剥除 + allow 映射对各种路径形态（basename、子目录、扩展名）都成立。
func TestAdapter_Generate_NegationBecomesAllow_MultipleCases(t *testing.T) {
	a := Adapter{}
	files, err := a.Generate(adapter.Plan{
		RawPatterns: []string{
			"*.env",
			"!.env.sample",      // basename 取反
			"!deploy/secret.pem", // 子目录路径取反
		},
	})
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(files[0].Content), &doc))
	read := doc["permission"].(map[string]any)["read"].(map[string]any)

	// 非取反行 → deny。
	assert.Equal(t, "deny", read["*.env"])
	// 取反行：剥 ! 后映射到 allow。
	assert.Equal(t, "allow", read[".env.sample"], "basename negation → allow")
	assert.Equal(t, "allow", read["deploy/secret.pem"], "subdir negation → allow")

	// 任何字面 ! 前缀都不应残留。
	for k := range read {
		assert.False(t, strings.HasPrefix(k, "!"),
			"key %q still has leading '!'; opencode glob has no negation", k)
	}
}
