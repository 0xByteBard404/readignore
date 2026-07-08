// Package opencode 实现 opencode 适配器：把 .readignore 翻译成 opencode 的
// permission 配置，在 opencode 加载时按 glob deny 文件读取。
//
// opencode 的 permission 系统采用「glob → allow/ask/deny」三层模型，其中
// read 权限直接控制文件读取工具（与 readignore「防读敏感文件」目标完全吻合）。
// 故本适配器把 plan.RawPatterns 逐条翻译成 permission.read 的 glob 键：
//   - 普通 pattern（deny）原样写入并标记 "deny"；
//   - 取反行（!pattern，readignore 语义为「放行」）剥掉前导 ! 后写入 actual
//     pattern 并标记 "allow"，依赖 opencode「更具体 glob 覆盖更宽泛」的机制
//     实现放行（如 .env.example 比 .env.* 更具体，allow 胜出）。
//
// 产出单文件 opencode.json（含官方 $schema 便于编辑器校验）。
//
// 取反语义限制声明（诚实标注）：opencode 的 glob 引擎**没有** gitignore 的取反
// 或顺序语义——它不识别 ! 前缀，也不按声明顺序求最后命中者。故本适配器只能用
// 「更具体的 allow glob 覆盖更宽泛的 deny glob」**近似** readignore 的取反。
// 对常见用例（先 deny .env.* 再 !放行单个 .env.example）该近似成立；但对复杂取反链
// （多条 ! 交织、取反后又被更具体 deny 覆盖等）可能与 readignore 语义存在边缘差异，
// 用户若依赖复杂取反链应优先选用 claudecode 适配器（PreToolUse hook 完整实现
// gitignore 取反语义）。
//
// 目录尾斜杠限制声明：opencode 文档未明确带尾斜杠的 pattern（如 secrets/）是否
// 按目录锚定（gitignore 中 secrets/ 仅匹配目录）。本适配器原样透传，行为以 opencode
// 实际 glob 引擎为准；若需严格的目录锚定语义，同样应优先选用 claudecode 适配器。
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
// 并诚实标注当前限制：
//   - 本适配器走 config deny/allow 路径，强度为 config（非 hard）；
//   - opencode 的 permission.ask 插件 hook 当前不被触发（issue #7006），故无法做执行前拦截；
//   - opencode glob 无 gitignore 取反/顺序语义，本适配器用「更具体的 allow 覆盖宽泛 deny」
//     近似实现取反，复杂取反链可能有边缘差异；
//   - 带尾斜杠 pattern（如 secrets/）按目录锚定的行为以 opencode glob 引擎为准。
func (Adapter) InstallInstructions() string {
	return "已生成 opencode.json（permission.read deny/allow）。opencode 启动时自动读取该配置并按 glob 决定文件读取。" +
		"注意：本适配器强度为 config（非 hard）——opencode 的 permission.ask 可编程 hook 当前不被触发（issue #7006），" +
		"故无法做到执行前硬拦，防护依赖 opencode 忠实加载配置。" +
		"另：opencode glob 无 gitignore 取反/顺序语义，本适配器用「更具体的 allow 覆盖宽泛 deny」近似实现放行；" +
		"复杂取反链（多条 ! 交织）或带尾斜杠目录锚定可能有边缘差异，依赖严格语义请用 claudecode 适配器。"
}

// Generate 依据 plan 产出单个 opencode.json：把 plan.RawPatterns 逐条翻译成
// permission.read 的 glob→decision 键。
//
//   - 普通行（如 ".env"）写入 read[".env"] = "deny"；
//   - 取反行（如 "!.env.example"，readignore 语义为放行）剥掉前导 ! 后写入
//     read[".env.example"] = "allow"，依赖 opencode「更具体 glob 覆盖更宽泛」
//     实现放行（.env.example 比 .env.* 更具体，allow 胜出）。
//
// 不在 map 里保留任何带前导 ! 的 key：opencode glob 把 ! 当字面字符，行为未定义，
// 故 ! 必须剥干净（与既有 claudecode 适配器一致——取反语义在剥 ! 后表达）。
//
// 产出形态（示例，RawPatterns=[".env",".env.*","!.env.example"]）：
//
//	{
//	  "$schema": "https://opencode.ai/config.json",
//	  "permission": {
//	    "read": {
//	      ".env": "deny",
//	      ".env.*": "deny",
//	      ".env.example": "allow"
//	    }
//	  }
//	}
//
// 取反语义限制：opencode 无 gitignore 顺序/取反求值，「更具体 allow 胜出」是对常见
// 用例的近似；复杂取反链（多条 ! 交织）可能有边缘差异，详见包 godoc。
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
		if actual, ok := stripNegation(p); ok {
			// readignore 取反 = 放行 → opencode allow（剥掉前导 ! 后的 actual pattern）。
			read[actual] = "allow"
			continue
		}
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
// 与 claudecode 的同名函数语义一致；取反行（!）原样保留——是否剥 ! 由 Generate
// 调 stripNegation 决定，此处不做形态改写，便于上层独立测试。
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

// stripNegation 检测 readignore 取反行：只要 p 以 ! 开头且长度 >1，即视为取反，
// 剥掉首个 ! 返回 actual pattern。ok=true 表示确为取反行（调用方应映射到 allow）；
// ok=false 表示普通行（deny）。
//
// 边界（!!foo 的处理是关键，godoc 必须与代码一致）：
//   - "!!foo" 也触发取反：判据是 HasPrefix(p,"!") && len(p)>1，!!foo 同时满足两者，
//     故只剥首个 ! 得 actual="!foo"、ok=true，最终映射成 allow glob "!foo"。
//     opencode 的 glob 引擎不识别 ! 前缀，会把 ! 当字面字符，于是 "!foo" 这条 allow
//     恰好匹配名为 "!foo" 的文件（即「放行字面 !foo 文件」）。
//   - 此行为与 claudecode 适配器（regex 引擎）一致收敛：claudecode 的 Rule.negated
//     判据同样是 raw.startswith("!")，对 !!foo 也得 negated=True、glob="!foo"，
//     求值时 excluded = not negated = False → 同样允许字面 !foo 文件。
//   - 单个 "!"（len==1）不触发取反，原样返回 ("!", false)：长度判定拦下；
//   - 仅剥前导 !，不动尾斜杠/内部字符（actual pattern 形态 = readignore 去取反后的 glob）。
func stripNegation(p string) (actual string, ok bool) {
	if strings.HasPrefix(p, "!") && len(p) > 1 {
		return p[1:], true
	}
	return p, false
}
