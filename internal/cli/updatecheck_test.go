package cli

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"0.4.1", "0.4.0", true},  // 落后 → 提示
		{"0.4.0", "0.4.0", false}, // 相同 → 不提示
		{"0.3.9", "0.4.0", false}, // 领先 → 不提示
		{"1.0.0", "0.9.9", true},  // 跨大版本
		{"v0.4.1", "0.4.0", true}, // 带 v 前缀
		{"0.4", "0.4.0", false},   // 缺段按 0
		{"0.4.1", "dev", true},    // current=dev（实际 Check 会先跳过 dev，这里只测纯比较）
	}
	for _, c := range cases {
		assert.Equal(t, c.want, isNewer(c.latest, c.current), "isNewer(%q,%q)", c.latest, c.current)
	}
}

func TestCacheRoundTrip(t *testing.T) {
	// 注入临时缓存目录
	prev := userCacheDir
	t.Cleanup(func() { userCacheDir = prev })
	dir := t.TempDir()
	userCacheDir = func() (string, error) { return dir, nil }

	c := cacheEntry{LastChecked: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC), LatestVersion: "0.4.1"}
	require.NoError(t, saveCache(c))

	got, err := loadCache()
	require.NoError(t, err)
	assert.Equal(t, "0.4.1", got.LatestVersion)
	assert.True(t, got.LastChecked.Equal(c.LastChecked))

	// 文件确实落在 <dir>/readignore/version-check.json
	_, err = os.Stat(filepath.Join(dir, "readignore", "version-check.json"))
	require.NoError(t, err)
}

func TestLoadCache_MissingIsZero(t *testing.T) {
	prev := userCacheDir
	t.Cleanup(func() { userCacheDir = prev })
	userCacheDir = func() (string, error) { return t.TempDir(), nil } // 空目录
	got, err := loadCache()
	require.NoError(t, err)
	assert.Equal(t, "", got.LatestVersion)
	assert.True(t, got.LastChecked.IsZero())
}

func TestFetchLatest_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		fmt.Fprint(w, `{"tag_name":"v0.4.1","name":"readignore 0.4.1"}`)
	}))
	defer srv.Close()
	prev := latestAPIURL
	t.Cleanup(func() { latestAPIURL = prev })
	latestAPIURL = srv.URL

	got, err := fetchLatest()
	require.NoError(t, err)
	assert.Equal(t, "0.4.1", got)
}

func TestFetchLatest_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	prev := latestAPIURL
	t.Cleanup(func() { latestAPIURL = prev })
	latestAPIURL = srv.URL

	_, err := fetchLatest()
	require.Error(t, err)
}

func TestPrintUpgradeNotice_NoReadignoreUpdate(t *testing.T) {
	var buf bytes.Buffer
	printUpgradeNotice(&buf, "0.4.0", "0.4.1")
	out := buf.String()
	// CTA 必须指真实渠道
	assert.Contains(t, out, "brew upgrade readignore")
	assert.Contains(t, out, "npm i -g readignore")
	assert.Contains(t, out, "install.sh")
	// 双语都在
	assert.Contains(t, out, "new version 0.4.1")
	assert.Contains(t, out, "新版本 0.4.1")
	// 绿色 ANSI
	assert.Contains(t, out, "\033[32m")
	// 绝不含误导性的 readignore update
	assert.NotContains(t, out, "readignore update")
}

// withVersion 临时把 Version 设成 v，测试后恢复（Check 的 dev 护栏要用）。
func withVersion(t *testing.T, v string) {
	t.Helper()
	prev := Version
	Version = v
	t.Cleanup(func() { Version = prev })
}

// forceUpdateCheckOn 为集成测试强制开启 update-check 语义：
// isTerminal=true（绕过 non-TTY 护栏）+ 注入缓存命中且落后（latest 0.4.1）。
// 不改排除名单/env/dev 护栏（这些由具体测试场景控制）。
func forceUpdateCheckOn(t *testing.T) {
	t.Helper()
	prevT := isTerminal
	t.Cleanup(func() { isTerminal = prevT })
	isTerminal = func() bool { return true }

	prevDir := userCacheDir
	t.Cleanup(func() { userCacheDir = prevDir })
	dir := t.TempDir()
	userCacheDir = func() (string, error) { return dir, nil }
	require.NoError(t, saveCache(cacheEntry{LastChecked: time.Now(), LatestVersion: "0.4.1"}))
}

// cmdNamed 构造一个指定名字的空 cobra.Command（测护栏用）。
func cmdNamed(name string) *cobra.Command {
	c := &cobra.Command{Use: name}
	return c
}

func TestCheck_DevSkipped(t *testing.T) {
	withVersion(t, "dev")
	var buf bytes.Buffer
	Check(cmdNamed("init"), &buf) // 不注入缓存/HTTP，dev 应最先返回
	assert.Empty(t, buf.String(), "dev 版本不应有任何输出")
}

func TestCheck_SkipCommands(t *testing.T) {
	withVersion(t, "0.4.0")
	// 注入缓存让"若执行到查询"会提示；但 skip 命令应在查询前返回
	prevCache := userCacheDir
	t.Cleanup(func() { userCacheDir = prevCache })
	userCacheDir = func() (string, error) { return t.TempDir(), nil }

	for _, name := range []string{"match", "hook-check", "update"} {
		var buf bytes.Buffer
		Check(cmdNamed(name), &buf)
		assert.Empty(t, buf.String(), "命令 %q 应被跳过无输出", name)
	}
}

func TestCheck_EnvOptOut(t *testing.T) {
	withVersion(t, "0.4.0")
	require.NoError(t, os.Setenv(noUpdateCheckEnv, "1"))
	t.Cleanup(func() { os.Unsetenv(noUpdateCheckEnv) })
	var buf bytes.Buffer
	Check(cmdNamed("init"), &buf)
	assert.Empty(t, buf.String(), "env 禁用应无输出")
}

func TestCheck_NonTTYOptOut(t *testing.T) {
	withVersion(t, "0.4.0")
	prev := isTerminal
	t.Cleanup(func() { isTerminal = prev })
	isTerminal = func() bool { return false }
	var buf bytes.Buffer
	Check(cmdNamed("init"), &buf)
	assert.Empty(t, buf.String(), "non-TTY 应无输出")
}

func TestCheck_CacheHit_NotifiesWhenBehind(t *testing.T) {
	withVersion(t, "0.4.0")
	// 注入缓存：命中（last_checked=now），latest=0.4.1 → 提示
	prevCache := userCacheDir
	t.Cleanup(func() { userCacheDir = prevCache })
	dir := t.TempDir()
	userCacheDir = func() (string, error) { return dir, nil }
	require.NoError(t, saveCache(cacheEntry{LastChecked: time.Now(), LatestVersion: "0.4.1"}))

	// 注入 isTerminal=true：go test 下 os.Stderr 非 TTY，须强制越过 non-TTY 护栏
	// 才能真正走到缓存/提示路径（否则空 buf 是护栏跳过，不是缓存逻辑）。
	prevTerm := isTerminal
	t.Cleanup(func() { isTerminal = prevTerm })
	isTerminal = func() bool { return true }

	var buf bytes.Buffer
	Check(cmdNamed("init"), &buf)
	out := buf.String()
	assert.Contains(t, out, "new version 0.4.1")
	assert.Contains(t, out, "brew upgrade readignore") // CTA 真实渠道
}

func TestCheck_CacheHit_NoNoticeWhenCurrent(t *testing.T) {
	withVersion(t, "0.4.0")
	prevCache := userCacheDir
	t.Cleanup(func() { userCacheDir = prevCache })
	dir := t.TempDir()
	userCacheDir = func() (string, error) { return dir, nil }
	require.NoError(t, saveCache(cacheEntry{LastChecked: time.Now(), LatestVersion: "0.4.0"})) // 相同

	// 同上：注入 isTerminal=true，确保真正走到缓存比较（而非被 non-TTY 护栏跳过）。
	prevTerm := isTerminal
	t.Cleanup(func() { isTerminal = prevTerm })
	isTerminal = func() bool { return true }

	var buf bytes.Buffer
	Check(cmdNamed("init"), &buf)
	assert.Empty(t, buf.String(), "版本相同不应提示")
}

// TestCheck_FirstFetchFailure_NoRetryStorm 验证 spec §7：首次 fetch 失败后
// （latest 保持空、last_checked 已写 now）不应在 24h 内重发 HTTP。
// 修前 fetch 触发条件的左子句 `c.LatestVersion == ""` 让条件每次短路 true，
// 无视 24h 退避——本测试在修前会 FAIL（hits=2）。
func TestCheck_FirstFetchFailure_NoRetryStorm(t *testing.T) {
	withVersion(t, "0.4.0")

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	prevURL := latestAPIURL
	t.Cleanup(func() { latestAPIURL = prevURL })
	latestAPIURL = srv.URL

	// 模拟首次 fetch 失败后的状态：latest 空、last_checked 刚写（24h 内）
	prevCache := userCacheDir
	t.Cleanup(func() { userCacheDir = prevCache })
	dir := t.TempDir()
	userCacheDir = func() (string, error) { return dir, nil }
	require.NoError(t, saveCache(cacheEntry{LastChecked: time.Now(), LatestVersion: ""}))

	prevTerm := isTerminal
	t.Cleanup(func() { isTerminal = prevTerm })
	isTerminal = func() bool { return true }

	var buf bytes.Buffer
	Check(cmdNamed("init"), &buf)
	Check(cmdNamed("init"), &buf)
	assert.Equal(t, 0, hits, "24h 内不应重试 fetch（last_checked 退避）")
	assert.Empty(t, buf.String(), "latest 空 → 不提示")
}
