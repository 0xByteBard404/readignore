package adapter

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// stubAdapter 是测试用 Adapter 实现：用字段记录调用，验证接口契约与 registry 行为。
// 故意放在本包内（非 _test 子包），以便同时验证导出的 Register/All/Get。
type stubAdapter struct {
	stubID          string
	stubStrength    Strength
	stubDetect      bool
	stubFiles       []GeneratedFile
	stubErr         error
	stubInstructions string

	// 调用记录
	detectRepoRoot  string
	generateCalled  bool
	generatePlan    Plan
}

func (s *stubAdapter) ID() string         { return s.stubID }
func (s *stubAdapter) Strength() Strength { return s.stubStrength }

// Detect 记录传入的 repoRoot，返回预设的布尔。
func (s *stubAdapter) Detect(repoRoot string) bool {
	s.detectRepoRoot = repoRoot
	return s.stubDetect
}

// Generate 记录调用与 plan，返回预设的文件切片/错误。
func (s *stubAdapter) Generate(plan Plan) ([]GeneratedFile, error) {
	s.generateCalled = true
	s.generatePlan = plan
	return s.stubFiles, s.stubErr
}

func (s *stubAdapter) InstallInstructions() string { return s.stubInstructions }

// withCleanRegistry 把 registry 重置为空并在测试结束恢复，保证测试间隔离。
func withCleanRegistry(t *testing.T) {
	t.Helper()
	origItems := registry.items
	origOrder := registry.order
	registry.mu.Lock()
	registry.items = make(map[string]Adapter)
	registry.order = nil
	registry.mu.Unlock()
	t.Cleanup(func() {
		registry.mu.Lock()
		registry.items = origItems
		registry.order = origOrder
		registry.mu.Unlock()
	})
}

func TestStrength_Constants(t *testing.T) {
	// 锁定常量字符串值：被 CLI 用于输出/排序，不可静默变更。
	assert.Equal(t, Strength("hard"), StrengthHard)
	assert.Equal(t, Strength("config"), StrengthConfig)
	assert.Equal(t, Strength("soft"), StrengthSoft)
}

func TestRegister_All_Get_Contract(t *testing.T) {
	withCleanRegistry(t)

	stub := &stubAdapter{
		stubID:          "stub-tool",
		stubStrength:    StrengthHard,
		stubDetect:      true,
		stubFiles:       []GeneratedFile{{Path: "a.txt", Content: "x", Mode: 0644}},
		stubInstructions: "do X",
	}
	Register(stub)

	// All() 含已注册的 stub
	all := All()
	assert.Len(t, all, 1)
	assert.Equal(t, stub, all[0])

	// Get(stubID) 命中
	got, ok := Get("stub-tool")
	assert.True(t, ok)
	assert.Equal(t, stub, got)

	// Get 透传 ID、Strength、InstallInstructions
	assert.Equal(t, "stub-tool", got.ID())
	assert.Equal(t, StrengthHard, got.Strength())
	assert.Equal(t, "do X", got.InstallInstructions())
}

func TestGet_UnknownID(t *testing.T) {
	withCleanRegistry(t)

	got, ok := Get("does-not-exist")
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestRegister_DuplicateID_Overwrites(t *testing.T) {
	// 同 ID 二次注册：后者覆盖前者，且 All() 不出现重复。
	withCleanRegistry(t)

	first := &stubAdapter{stubID: "dup", stubInstructions: "first"}
	second := &stubAdapter{stubID: "dup", stubInstructions: "second"}

	Register(first)
	Register(second)

	all := All()
	assert.Len(t, all, 1, "duplicate ID must not duplicate in All()")
	got, ok := Get("dup")
	assert.True(t, ok)
	assert.Equal(t, "second", got.InstallInstructions())
}

func TestAll_Empty(t *testing.T) {
	withCleanRegistry(t)

	assert.Empty(t, All())
}

// Register(nil) 是空操作：不登记、不 panic、不污染 order。
// 防御性契约，避免 init() 误登记空适配器导致后续空指针。
func TestRegister_NilIsNoop(t *testing.T) {
	withCleanRegistry(t)

	Register(nil)

	assert.Empty(t, All())
	_, ok := Get("")
	assert.False(t, ok)
}

// All_StableOrder 验证 All() 返回顺序遵循注册顺序（CLI 按稳定顺序列出适配器）。
func TestAll_StableOrder(t *testing.T) {
	withCleanRegistry(t)

	a := &stubAdapter{stubID: "alpha"}
	b := &stubAdapter{stubID: "beta"}
	c := &stubAdapter{stubID: "gamma"}
	Register(a)
	Register(b)
	Register(c)

	got := All()
	assert.Len(t, got, 3)
	assert.Equal(t, "alpha", got[0].ID())
	assert.Equal(t, "beta", got[1].ID())
	assert.Equal(t, "gamma", got[2].ID())
}

// Adapter 接口行为：Detect 记录 repoRoot；Generate 记录 plan 并返回预设文件/错误。
// 这保证接口方法被真实调用、字段被真实传递（非空跑）。
func TestStubAdapter_DetectAndGenerate_Delegate(t *testing.T) {
	withCleanRegistry(t)

	wantFiles := []GeneratedFile{
		{Path: "dir/cfg.json", Content: "{}", Mode: 0600},
		{Path: "scripts/hook.sh", Content: "#!/bin/sh", Mode: 0755},
	}
	stub := &stubAdapter{
		stubID:       "recorder",
		stubStrength: StrengthConfig,
		stubDetect:   true,
		stubFiles:    wantFiles,
	}
	Register(stub)

	// Detect 透传 repoRoot
	got, ok := Get("recorder")
	assert.True(t, ok)
	assert.True(t, got.Detect("/repo/root"))
	assert.Equal(t, "/repo/root", stub.detectRepoRoot)

	// Generate 透传 plan 并返回预设文件
	plan := Plan{
		RepoRoot:       "/repo/root",
		MatchedPaths:   []string{".env", "secret.pem"},
		RawPatterns:    []string{".env", "*.pem"},
	}
	files, err := got.Generate(plan)
	assert.NoError(t, err)
	assert.Equal(t, wantFiles, files)
	assert.True(t, stub.generateCalled)
	assert.Equal(t, plan, stub.generatePlan)
}

// 验证 Generate 也能如实回传错误（适配器在生成失败时的契约）。
func TestStubAdapter_GeneratePropagatesError(t *testing.T) {
	withCleanRegistry(t)

	wantErr := errors.New("boom")
	stub := &stubAdapter{stubID: "failer", stubErr: wantErr}
	Register(stub)

	got, _ := Get("failer")
	files, err := got.Generate(Plan{RepoRoot: "/r"})
	assert.ErrorIs(t, err, wantErr)
	assert.Nil(t, files)
}

// GeneratedFile 默认零值语义：Mode == 0 表示「用默认」（由调用方/安装层决定）。
func TestGeneratedFile_DefaultModeMeansUseDefault(t *testing.T) {
	f := GeneratedFile{Path: "p", Content: "c"}
	assert.Equal(t, uint32(0), f.Mode, "zero Mode means 'use default'")
}

// 三种 Strength 各自可被适配器返回并查询，验证枚举完整性。
func TestStrength_AllVariantsUsable(t *testing.T) {
	withCleanRegistry(t)

	for _, tc := range []struct {
		id    string
		st    Strength
		want  Strength
	}{
		{"h", StrengthHard, StrengthHard},
		{"c", StrengthConfig, StrengthConfig},
		{"s", StrengthSoft, StrengthSoft},
	} {
		Register(&stubAdapter{stubID: tc.id, stubStrength: tc.st})
	}
	got, ok := Get("h")
	assert.True(t, ok)
	assert.Equal(t, StrengthHard, got.Strength())
	got, ok = Get("c")
	assert.True(t, ok)
	assert.Equal(t, StrengthConfig, got.Strength())
	got, ok = Get("s")
	assert.True(t, ok)
	assert.Equal(t, StrengthSoft, got.Strength())
}
