// updatecheck 子模块：跑非热路径命令时检测 GitHub 最新版，
// 落后则绿色双语提示真实升级渠道到 stderr。绝不阻断主命令、绝不报错。
// 详见 docs/superpowers/specs/2026-07-14-update-notification-design.md。
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	updateCheckInterval = 24 * time.Hour
	noUpdateCheckEnv    = "READIGNORE_NO_UPDATE_CHECK"
)

const (
	updateCheckTimeout = 1 * time.Second
	latestReleaseURL   = "https://api.github.com/repos/0xByteBard404/readignore/releases/latest"
)

// userCacheDir 可注入（测试覆盖到 t.TempDir()），生产用 os.UserCacheDir。
var userCacheDir = os.UserCacheDir

// latestAPIURL / httpClient 可注入（测试指向 httptest.Server）。
var (
	latestAPIURL = latestReleaseURL
	httpClient   = &http.Client{Timeout: updateCheckTimeout}
)

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

// fetchLatest 查 GitHub releases/latest，返回 tag_name 去掉 v 前缀的版本号。
// 任何错误（网络/超时/HTTP 非 200/JSON 缺 tag_name）都返回 error，由 Check 静默。
func fetchLatest() (string, error) {
	req, err := http.NewRequest(http.MethodGet, latestAPIURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api status %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.TagName == "" {
		return "", fmt.Errorf("empty tag_name")
	}
	return strings.TrimPrefix(body.TagName, "v"), nil
}

// printUpgradeNotice 把绿色双语提示写到 w（生产是 os.Stderr）。
// CTA 指真实升级渠道，绝不指 readignore update（它不升级二进制）。
func printUpgradeNotice(w io.Writer, current, latest string) {
	green := "\033[32m"
	reset := "\033[0m"
	fmt.Fprintf(w, "%sreadignore: new version %s available (current %s). "+
		"Upgrade: brew upgrade readignore / npm i -g readignore / re-run install.sh.%s\n",
		green, latest, current, reset)
	fmt.Fprintf(w, "%sreadignore：新版本 %s 可用（当前 %s）。"+
		"升级：brew upgrade readignore / npm i -g readignore / 重跑 install.sh。%s\n",
		green, latest, current, reset)
}

// updateCheckSkip 是不触发 update-check 的命令名集合：
// match / hook-check 是 hook 热路径（每秒多次调用，联网不可接受）；
// update 是维护命令（刷新适配器产物），且常被误作升级命令，提示打扰 + 突兀。
var updateCheckSkip = map[string]bool{
	"match":      true,
	"hook-check": true,
	"update":     true,
}

// isTerminal 可注入（测试覆盖）。生产查 os.Stderr 是否 TTY。
var isTerminal = func() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// Check 在命令执行前做新版本检测。绝不返回 error、绝不阻断主命令
// （网络/解析失败一律静默）。stderr 是提示输出（生产 os.Stderr）。
//
// 护栏顺序（任一命中即静默返回）：dev 版本 → 热路径/update 命令 →
// 禁用 env → non-TTY。然后查缓存：24h 内用缓存 latest_version；
// 到期则 HTTP 查（成功才更新 latest_version，但 last_checked 总更新）。
// 落后则绿色双语提示。
func Check(cmd *cobra.Command, stderr io.Writer) {
	if Version == "dev" {
		return
	}
	if updateCheckSkip[cmd.Name()] {
		return
	}
	if os.Getenv(noUpdateCheckEnv) != "" {
		return
	}
	if !isTerminal() {
		return
	}

	c, _ := loadCache() // 失败零值，静默
	if c.LatestVersion == "" || time.Since(c.LastChecked) >= updateCheckInterval {
		if latest, err := fetchLatest(); err == nil {
			c.LatestVersion = latest
		}
		c.LastChecked = time.Now()
		_ = saveCache(c) // 失败静默
	}

	if c.LatestVersion != "" && isNewer(c.LatestVersion, Version) {
		printUpgradeNotice(stderr, Version, c.LatestVersion)
	}
}
