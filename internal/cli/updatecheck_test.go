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
