// Package opencode 实现 opencode 适配器：把 .readignore 翻译成 opencode 的
// permission 配置，在 opencode 加载时按 glob deny 文件读取。
//
// opencode 的 permission 系统采用「glob → allow/ask/deny」三层模型，其中
// read 权限直接控制文件读取工具（与 readignore「防读敏感文件」目标完全吻合）。
// 故本适配器把 plan.RawPatterns 逐条翻译成 permission.read 的 "deny" 键，
// 产出单文件 opencode.json（含官方 $schema 便于编辑器校验）。
//
// 强度声明（诚实标注）：opencode 当前**没有**执行前可编程拦截。
// 其 permission.ask 插件 hook 虽在 @opencode-ai/plugin 类型定义中存在，
// 但运行时从不被触发（anomalyco/opencode #7006），故本适配器只能走静态配置
// 路径，强度为 [adapter.StrengthConfig]（依赖 opencode 忠实读取配置），
// 而非 claudecode 的 StrengthHard。README 与 InstallInstructions 均如实标注。
//
// init() 调 adapter.Register 自登记，CLI 通过 adapter.Get("opencode") 发现本适配器。
package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

// configSchema 是 opencode 官方 JSON Schema URL，写入生成的配置便于编辑器校验/补全。
// 来源：https://opencode.ai/docs/config/ （schema 引用在 config 文档中给出）。
const configSchema = "https://opencode.ai/config.json"

// Adapter 实现 [adapter.Adapter]，把 .readignore 翻译成 opencode permission 配置。
//
// 零字段、无状态：所有产物在 Generate 时根据 plan 即时生成，便于测试与并发安全。
type Adapter struct{}

// 编译期保证 Adapter 满足接口契约；缺失方法在编译时即报错，而非运行时。
var _ adapter.Adapter = Adapter{}

// init 把本适配器登记进全局 registry，使 adapter.All()/Get() 可发现。
// 放在包 init（而非显式调用）符合「具体适配器自登记」的设计约定。
func init() {
	adapter.Register(Adapter{})
}

// ID 返回稳定短标识 "opencode"，用作 CLI 参数、配置键与 registry 索引。
// 全小写、无空格、跨版本不变。
func (Adapter) ID() string { return "opencode" }

// Strength 返回 [adapter.StrengthConfig]：opencode 通过静态 permission 配置 deny，
// 由 opencode 加载配置后生效；其 permission.ask hook 当前不被触发（issue #7006），
// 故强度为 config，而非 claudecode 的 hard。
func (Adapter) Strength() adapter.Strength { return adapter.StrengthConfig }

// Detect 探测 repoRoot 下是否已存在 opencode 痕迹：
//   - 项目根的 opencode.json（opencode 项目级配置文件，见 https://opencode.ai/docs/config/）；
//   - .opencode/ 目录（opencode 也支持该目录放置配置）。
//
// 命中仅影响 CLI 是否默认启用本适配器；Generate 即便未检测到也能产出可手动安装的文件。
func (Adapter) Detect(repoRoot string) bool {
	if repoRoot == "" {
		return false
	}
	if fi, err := os.Stat(filepath.Join(repoRoot, "opencode.json")); err == nil && !fi.IsDir() {
		return true
	}
	if fi, err := os.Stat(filepath.Join(repoRoot, ".opencode")); err == nil && fi.IsDir() {
		return true
	}
	return false
}

// InstallInstructions 给出「如何让 opencode 读取所生成配置」的人类可读说明，
// 并诚实标注当前限制：本适配器走 config deny 路径，强度为 config（非 hard）；
// opencode 的 permission.ask 插件 hook 当前不被触发（issue #7006），故无法做执行前拦截。
func (Adapter) InstallInstructions() string {
	return "已生成 opencode.json（permission.read deny）。opencode 启动时自动读取该配置并按 glob 拒绝文件读取。" +
		"注意：本适配器强度为 config（非 hard）——opencode 的 permission.ask 可编程 hook 当前不被触发（issue #7006），" +
		"故无法做到执行前硬拦，防护依赖 opencode 忠实加载配置。"
}

// Generate 依据 plan 产出单个 opencode.json：把 plan.RawPatterns 逐条翻译成
// permission.read 的 deny 键（保留取反行 !，opencode 无取反语义但保留以备用户手改）。
//
// 产出形态（示例，RawPatterns=[".env","*.pem"]）：
//
//	{
//	  "$schema": "https://opencode.ai/config.json",
//	  "permission": {
//	    "read": {
//	      ".env": "deny",
//	      "*.pem": "deny"
//	    }
//	  }
//	}
//
// 设计：
//   - 只 Generate 配置片段，与既有 opencode.json 的深度合并由阶段5 CLI 完成
//     （与 claudecode 处理 settings.json 的策略一致）；
//   - Mode 0 表示用调用方默认权限（典型 0644）；
//   - 文件路径恒为 opencode.json（相对仓库根），不依赖 plan.RepoRoot；
//   - 用 encoding/json 序列化保证 JSON 语法合法，不手拼字符串。
func (Adapter) Generate(plan adapter.Plan) ([]adapter.GeneratedFile, error) {
	patterns := sanitizePatterns(plan.RawPatterns)

	read := make(map[string]string, len(patterns))
	for _, p := range patterns {
		read[p] = "deny"
	}

	// 顶层结构：{"$schema": ..., "permission": {"read": {glob: "deny", ...}}}。
	// read 即便为空也保留（map[string]string 序列化为 {}），保证配置合法、可被 opencode 解析。
	doc := map[string]any{
		"$schema": configSchema,
		"permission": map[string]any{
			"read": read,
		},
	}

	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		// map[string]string/any 的 MarshalIndent 在标准库下不会失败；
		// 仍返回 error 以保持接口契约，调用方无需感知内部序列化细节。
		return nil, err
	}

	return []adapter.GeneratedFile{{
		Path:    "opencode.json",
		Content: string(buf),
		Mode:    0,
	}}, nil
}

// sanitizePatterns 规整待翻译的 patterns：去空白行与注释行（# 开头）。
// 与 claudecode 的同名函数语义一致；取反行（!）按调用方意图保留——opencode 无
// 取反语义，但丢弃会丢失信息（用户可能在 opencode 里手工改成 allow），故原样透传。
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
