// Package cli 的 updatecheck 子模块：跑非热路径命令时检测 GitHub 最新版，
// 落后则绿色双语提示真实升级渠道到 stderr。绝不阻断主命令、绝不报错。
// 详见 docs/superpowers/specs/2026-07-14-update-notification-design.md。
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	updateCheckInterval = 24 * time.Hour
	noUpdateCheckEnv    = "READIGNORE_NO_UPDATE_CHECK"
)

// userCacheDir 可注入（测试覆盖到 t.TempDir()），生产用 os.UserCacheDir。
var userCacheDir = os.UserCacheDir

// cacheEntry 是 version-check.json 的结构。
// latest_version 仅 HTTP 成功时更新；last_checked 每次都更新（含失败，防重试风暴）。
type cacheEntry struct {
	LastChecked   time.Time `json:"last_checked"`
	LatestVersion string    `json:"latest_version"`
}

// cachePath 返回缓存文件路径：<UserCacheDir>/readignore/version-check.json。
// 这是 readignore 首个用户级持久状态文件（全仓此前不写 UserCacheDir）。
func cachePath() (string, error) {
	dir, err := userCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "readignore", "version-check.json"), nil
}

// loadCache 读缓存；不存在/解析失败返回零值 + nil（静默，不报错）。
func loadCache() (cacheEntry, error) {
	p, err := cachePath()
	if err != nil {
		return cacheEntry{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return cacheEntry{}, nil // 不存在等 → 静默零值
	}
	var c cacheEntry
	if err := json.Unmarshal(b, &c); err != nil {
		return cacheEntry{}, nil // 畸形 → 静默零值
	}
	return c, nil
}

// saveCache 写缓存（建父目录）；失败静默（调用方忽略 error）。
func saveCache(c cacheEntry) error {
	p, err := cachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// isNewer 报告 latest 是否严格新于 current（major.minor.patch 三段整数比较）。
// 去 v 前缀；非数字/缺段按 0 处理。仅 latest>current 返回 true。
func isNewer(latest, current string) bool {
	lt := strings.Split(strings.TrimPrefix(latest, "v"), ".")
	ct := strings.Split(strings.TrimPrefix(current, "v"), ".")
	for i := 0; i < 3; i++ {
		lv := atoiOrZero(get(lt, i))
		cv := atoiOrZero(get(ct, i))
		if lv != cv {
			return lv > cv
		}
	}
	return false
}

func get(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
