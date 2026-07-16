// Package kilocode 实现 kilocode 适配器：把 .readignore 翻译成 kilo.json 的
// permission 配置，在 kilocode 加载时按 glob deny 文件读取/改写。
//
// kilocode（kilo.ai，开源 MIT，OpenCode fork；Kilo-Org/kilocode monorepo 内
// packages/opencode 即该 fork）的 permission 系统与 opencode 一脉相承：permission
// 暴露 read 与 edit 两个 glob→allow/deny 段（kilocode 自身 test/kilocode/config-injector.test.ts
// 与 ignore-migrator.test.ts 均断言 permission.read / permission.edit 为 Record<string,string>）。
// 这与 readignore 的 read/edit 分段天然吻合：
//
//   - plan.Rules.Read → permission.read：普通 pattern 标 "deny"，取反行（!pattern，
//     readignore 语义为「放行」）剥掉前导 ! 后标 "allow"，依赖 kilocode「更具体 glob
//     覆盖更宽泛」的机制实现放行；
//   - plan.Rules.Edit → permission.edit：同上翻译，阻断对 edit 段声明文件的改写；
//   - plan.Rules.Delete → 不支持（kilocode/opencode fork 同样无 delete 权限段；
//     删文件走 bash permission，属不同机制，本适配器不覆盖，InstallInstructions 如实标注）。
//
// 产出单文件 kilo.json（kilocode 项目级配置）。
//
// 取反语义限制声明（诚实标注）：kilocode 的 glob 引擎（util/wildcard.ts）没有
// gitignore 的取反或顺序语义——它不识别 ! 前缀，也不按声明顺序求最后命中者。
// 故本适配器只能用「更具体的 allow glob 覆盖更宽泛的 deny glob」**近似** readignore
// 的取反。对常见用例正确，复杂取反链可能有边缘差异；用户若依赖复杂取反链应优先
// 选用 claudecode 适配器（PreToolUse hook 完整实现 gitignore 取反语义）。
//
// glob 语法差异：kilocode 的 wildcard 是简单 glob（`*`→`.*`、`?`→`.`，全匹配），
// **不支持 `**` 目录穿越**。含 `**/` 的 pattern（如 `**/id_rsa`）需剥 `**/` 前缀
// 降级为 basename 匹配（`id_rsa`）。read 段与 edit 段共用此降级。
//
// 强度声明（诚实标注）：kilocode 当前有 permission deny 配置，且有 hardRuleset +
// ReadPermission.harden 机制（甚至已对 *.env 做了 hardening 先例）。但：
//  1. 本适配器走 config 路径（生成 kilo.json），不修改 kilocode 源码，无法注入
//     hardRuleset（那需要 fork/PR kilocode CLI）；
//  2. kilocode 有已知 bug：deny 规则有时被绕过（GitHub #8293、#11637）；
//  3. shell/bash 命令（cat .env）走 bash permission，不经过 read permission。
//
// 故本适配器强度标为 config，而非 hard。未来若 kilocode 上游 PR 接受 .readignore
// 注入 hardRuleset，可升级为 hard。
//
// init() 调 adapter.Register 自登记，CLI 通过 adapter.Get("kilocode") 发现本适配器。
package kilocode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

// Adapter 实现 [adapter.Adapter]，把 .readignore 翻译成 kilocode permission 配置。
//
// 零字段、无状态：所有产物在 Generate 时根据 plan 即时生成，便于测试与并发安全。
type Adapter struct{}

// 编译期保证 Adapter 满足接口契约。
var _ adapter.Adapter = Adapter{}

// init 把本适配器登记进全局 registry。
func init() {
	adapter.Register(Adapter{})
}

// ID 返回稳定短标识 "kilocode"。
func (Adapter) ID() string { return "kilocode" }

// Strength 返回 [adapter.StrengthConfig]：kilocode 通过静态 permission 配置 deny，
// 由 kilocode 加载配置后生效。kilocode 有 hardRuleset + ReadPermission.harden 机制
// （甚至已对 *.env 做了 hardening 先例），但本适配器不修改 kilocode 源码，只能走
// config 路径，且有已知 deny 绕过 bug（GitHub #8293、#11637），故标 config。
func (Adapter) Strength() adapter.Strength { return adapter.StrengthConfig }

// Detect 探测 repoRoot 下是否已存在 kilocode 痕迹：
//   - 项目根的 kilo.json / kilo.jsonc（kilocode 项目级配置文件）；
//   - .kilo/ 目录（kilocode 项目级配置目录）；
//   - .kilocode/ 目录（kilocode legacy 配置目录）。
//
// 命中仅影响 CLI 是否默认启用本适配器。
func (Adapter) Detect(repoRoot string) bool {
	if repoRoot == "" {
		return false
	}
	for _, name := range []string{"kilo.json", "kilo.jsonc"} {
		if fi, err := os.Stat(filepath.Join(repoRoot, name)); err == nil && !fi.IsDir() {
			return true
		}
	}
	for _, dir := range []string{".kilo", ".kilocode"} {
		if fi, err := os.Stat(filepath.Join(repoRoot, dir)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// InstallInstructions 给出人类可读说明，并诚实标注当前限制：
//   - 覆盖 read 段（permission.read）与 edit 段（permission.edit）；delete 段不支持
//     （kilocode 无 delete 权限分类，删文件走 bash permission，属不同机制，本适配器不覆盖）；
//   - config 强度（非 hard），有已知 deny 绕过 bug（#8293、#11637）；
//   - glob 无 gitignore 取反/顺序语义，用「更具体 allow 覆盖宽泛 deny」近似；
//   - glob 不支持 **，含 ** 的 pattern 已降级为 basename 匹配；
//   - shell 命令（cat .env）走 bash permission，不经过 read permission。
func (Adapter) InstallInstructions() string {
	return "已生成 kilo.json（permission.read + permission.edit deny/allow）。kilocode 启动时自动读取该配置并按 glob 决定文件读取/改写。" +
		"覆盖范围：read 段（permission.read 阻断读取）+ edit 段（permission.edit 阻断改写）。delete 段不支持" +
		"（kilocode 无 delete 权限分类，删文件走 bash permission，属不同机制，本适配器不覆盖）。" +
		"注意：本适配器强度为 config（非 hard）——kilocode 有已知 deny 绕过 bug（GitHub #8293、#11637），" +
		"防护依赖 kilocode 忠实加载配置。" +
		"另：kilocode glob 不支持 **（含 ** 的 pattern 已降级为 basename 匹配）、无 gitignore 取反/顺序语义；" +
		"shell 命令（cat .env）走 bash permission 不经 read permission。" +
		"依赖严格语义请用 claudecode 适配器。"
}

// Generate 依据 plan 产出单个 kilo.json：把 plan.Rules.Read 与 plan.Rules.Edit
// 分别翻译成 permission.read / permission.edit 的 glob→decision 键。
//
//   - 普通行（如 ".env"）写入 read[".env"] = "deny"；
//   - 取反行（如 "!.env.example"）剥 ! 后写入 read[".env.example"] = "allow"；
//   - 含 **/ 前缀的 pattern（如 "**/id_rsa"）剥 **/ 后降级为 basename（"id_rsa"）。
//
// edit 段（plan.Rules.Edit → permission.edit）同上翻译（含 stripStarStar 降级）。
// 分段读取（而非全集 RawPatterns）：kilocode permission 区分 read 与 edit 工具，
// [edit] 段声明的 pattern 应只阻断改写、不阻断读取。对无段头的 .readignore
// （裸 pattern 全归 Read 段），行为与历史一致。
//
// 产出形态（示例，Rules.Read=[".env",".env.*","!.env.example","**/id_rsa"], Rules.Edit=["secrets/*.key"]）：
//
//	{
//	  "permission": {
//	    "edit": {
//	      "secrets/*.key": "deny"
//	    },
//	    "read": {
//	      ".env": "deny",
//	      ".env.*": "deny",
//	      ".env.example": "allow",
//	      "id_rsa": "deny"
//	    }
//	  }
//	}
func (Adapter) Generate(plan adapter.Plan) ([]adapter.GeneratedFile, error) {
	read := buildDecisionMap(plan.Rules.Read)
	edit := buildDecisionMap(plan.Rules.Edit)

	doc := map[string]any{
		"permission": map[string]any{
			"read": read,
			"edit": edit,
		},
	}

	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}

	return []adapter.GeneratedFile{{
		Path:    "kilo.json",
		Content: string(buf),
		Mode:    0,
	}}, nil
}

// buildDecisionMap 把一段原始 pattern 行翻译成 kilocode 的 glob→decision map：
//   - 去注释/空行（sanitizePatterns）；
//   - 普通行 → "deny"；取反行（!pattern，readignore 语义为放行）剥 ! 后 → "allow"；
//   - 含 **/ 前缀的 pattern 剥 **/ 降级为 basename（kilocode wildcard 不支持 ** 目录穿越）。
//
// read 段与 edit 段共用此翻译逻辑（两段在 kilocode 侧的 glob→decision 形态一致）。
func buildDecisionMap(raw []string) map[string]string {
	patterns := sanitizePatterns(raw)
	m := make(map[string]string, len(patterns))
	for _, p := range patterns {
		if actual, ok := stripNegation(p); ok {
			m[stripStarStar(actual)] = "allow"
			continue
		}
		m[stripStarStar(p)] = "deny"
	}
	return m
}

// sanitizePatterns 规整待翻译的 patterns：去空白行与注释行。
func sanitizePatterns(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// stripNegation 检测 readignore 取反行：p 以 ! 开头且长度 >1，剥掉首个 ! 返回 actual。
func stripNegation(p string) (actual string, ok bool) {
	if strings.HasPrefix(p, "!") && len(p) > 1 {
		return p[1:], true
	}
	return p, false
}

// stripStarStar 剥掉 pattern 开头的 **/ 前缀：kilocode 的 wildcard 不支持 ** 目录穿越，
// 含 **/ 的 pattern（如 **/id_rsa）降级为 basename 匹配（id_rsa）。
// 多层 **/ 只剥第一层（与 basename 语义一致）。
func stripStarStar(p string) string {
	return strings.TrimPrefix(p, "**/")
}
