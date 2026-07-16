// Package opencode 实现 opencode 适配器：把 .readignore 翻译成 opencode 的
// permission 配置，在 opencode 加载时按 glob deny 文件读取/改写。
//
// opencode 的 permission 系统采用「glob → allow/ask/deny」三层模型。其 schema
// （https://opencode.ai/config.json $defs/PermissionConfig）暴露的工具分类含
// read 与 edit 两个 glob→decision 段（均 PermissionRuleConfig 形态），分别控制
// 文件读取与改写工具；无 write 段（opencode 的写工具叫 edit），也无 delete 段
// （删文件走 bash，本适配器不覆盖）。这与 readignore 的 read/edit 分段天然吻合：
//
//   - plan.Rules.Read → permission.read：普通 pattern 标 "deny"，取反行（!pattern，
//     readignore 语义为「放行」）剥掉前导 ! 后标 "allow"，依赖 opencode「更具体
//     glob 覆盖更宽泛」的机制实现放行（如 .env.example 比 .env.* 更具体，allow 胜出）；
//   - plan.Rules.Edit → permission.edit：同上翻译，阻断对 edit 段声明文件的改写；
//   - plan.Rules.Delete → 不支持（opencode 无 delete 权限段；删文件走 bash permission，
//     属不同机制，本适配器不覆盖，InstallInstructions 如实标注）。
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
// 具体反例对照（readignore gitignore 语义 vs opencode glob 特异性近似）：
//
//	.readignore:
//	  *.env          # deny
//	  !a.env         # allow（取反放行）
//
//	- readignore（最后规则胜出）：a.env 命中 !a.env → 放行；b.env 命中 *.env → 拦截。
//	- opencode（glob 特异性）：read["*.env"]="deny" + read["a.env"]="allow"；
//	  a.env 比 *.env 更具体 → allow 胜出 → 放行 a.env；b.env 仍 deny。**本例两者一致**。
//
//	但更复杂的链可能分歧，例如 deny *.env + !a.env + deny *.env 的子集时，
//	gitignore 按声明顺序最后命中者胜，而 opencode 仅按 glob 字面特异性裁决，
//	无顺序概念——此时两者的放行/拦截结果可能不同。这类用例请用 claudecode 适配器。
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
//   - 覆盖 read 段（permission.read）与 edit 段（permission.edit）；delete 段不支持
//     （opencode 无 delete 权限分类，删文件走 bash permission，属不同机制，本适配器不覆盖）；
//   - opencode 的 permission.ask 插件 hook 当前不被触发（issue #7006），故无法做执行前拦截；
//   - opencode glob 无 gitignore 取反/顺序语义，本适配器用「更具体的 allow 覆盖宽泛 deny」
//     近似实现取反，复杂取反链可能有边缘差异；
//   - 带尾斜杠 pattern（如 secrets/）按目录锚定的行为以 opencode glob 引擎为准。
func (Adapter) InstallInstructions() string {
	return "已生成 opencode.json（permission.read + permission.edit deny/allow）。opencode 启动时自动读取该配置并按 glob 决定文件读取/改写。" +
		"覆盖范围：read 段（permission.read 阻断读取）+ edit 段（permission.edit 阻断改写）。delete 段不支持" +
		"（opencode 无 delete 权限分类，删文件走 bash permission，属不同机制，本适配器不覆盖）。" +
		"注意：本适配器强度为 config（非 hard）——opencode 的 permission.ask 可编程 hook 当前不被触发（issue #7006），" +
		"故无法做到执行前硬拦，防护依赖 opencode 忠实加载配置。" +
		"另：opencode glob 无 gitignore 取反/顺序语义，本适配器用「更具体的 allow 覆盖宽泛 deny」近似实现放行；" +
		"复杂取反链（多条 ! 交织）或带尾斜杠目录锚定可能有边缘差异，依赖严格语义请用 claudecode 适配器。"
}

// Generate 依据 plan 产出单个 opencode.json：把 plan.Rules.Read 与 plan.Rules.Edit
// 分别翻译成 permission.read / permission.edit 的 glob→decision 键。
//
//	read 段（plan.Rules.Read）→ permission.read：
//	  - 普通行（如 ".env"）写入 read[".env"] = "deny"；
//	  - 取反行（如 "!.env.example"，readignore 语义为放行）剥掉前导 ! 后写入
//	    read[".env.example"] = "allow"，依赖 opencode「更具体 glob 覆盖更宽泛」
//	    实现放行（.env.example 比 .env.* 更具体，allow 胜出）。
//	edit 段（plan.Rules.Edit）→ permission.edit：同上翻译，阻断对 edit 段声明文件的改写。
//
// 分段读取（而非全集 RawPatterns）：opencode 的 permission 区分 read 与 edit 工具，
// 故 [edit] 段声明的 pattern 应只阻断改写、不阻断读取——读 plan.Rules.Read/Edit
// 分桶写入对应段，语义才正确（全集写进 read 会把 edit-only 文件也挡读，与用户意图相左）。
// 对无段头的 .readignore（裸 pattern 全归 Read 段），行为与历史一致。
//
// 不在 map 里保留任何带前导 ! 的 key：opencode glob 把 ! 当字面字符，行为未定义，
// 故 ! 必须剥干净（与既有 claudecode 适配器一致——取反语义在剥 ! 后表达）。
//
// 产出形态（示例，Rules.Read=[".env",".env.*","!.env.example"], Rules.Edit=["secrets/*.key"]）：
//
//	{
//	  "$schema": "https://opencode.ai/config.json",
//	  "permission": {
//	    "edit": {
//	      "secrets/*.key": "deny"
//	    },
//	    "read": {
//	      ".env": "deny",
//	      ".env.*": "deny",
//	      ".env.example": "allow"
//	    }
//	  }
//	}
//
// 注意：上面示例里键顺序看似与声明相关，但那是巧合——Go map 经 encoding/json
// 序列化时**按 key 字母序**输出（段名 edit/read、glob 键均字母序），与本适配器写入
// 先后无关。此处顺序无关紧要：opencode 的 glob 匹配不依赖键顺序（靠 glob 特异性
// 而非声明顺序求值），故字母序排列不影响最终 deny/allow 决策。
//
// 设计：
//   - 只 Generate 配置片段，与既有 opencode.json 的深度合并由阶段5 CLI 完成
//     （与 claudecode 处理 settings.json 的策略一致）；
//   - read/edit 即便为空也保留（map[string]string 序列化为 {}），保证配置合法、
//     段存在可被 opencode 解析；
//   - Mode 0 表示用调用方默认权限（典型 0644）；
//   - 文件路径恒为 opencode.json（相对仓库根），不依赖 plan.RepoRoot；
//   - 用 encoding/json 序列化保证 JSON 语法合法，不手拼字符串。
func (Adapter) Generate(plan adapter.Plan) ([]adapter.GeneratedFile, error) {
	read := buildDecisionMap(plan.Rules.Read)
	edit := buildDecisionMap(plan.Rules.Edit)

	// 顶层结构：{"$schema": ..., "permission": {"read": {...}, "edit": {...}}}。
	// 两段即便为空也保留（序列化为 {}），保证配置合法、段存在可被 opencode 解析。
	doc := map[string]any{
		"$schema": configSchema,
		"permission": map[string]any{
			"read": read,
			"edit": edit,
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

// buildDecisionMap 把一段原始 pattern 行翻译成 opencode 的 glob→decision map：
//   - 去注释/空行（sanitizePatterns）；
//   - 普通行 → "deny"；取反行（!pattern，readignore 语义为放行）剥 ! 后 → "allow"。
//
// read 段与 edit 段共用此翻译逻辑（两段在 opencode 侧的 glob→decision 形态一致）。
func buildDecisionMap(raw []string) map[string]string {
	patterns := sanitizePatterns(raw)
	m := make(map[string]string, len(patterns))
	for _, p := range patterns {
		if actual, ok := stripNegation(p); ok {
			// readignore 取反 = 放行 → opencode allow（剥掉前导 ! 后的 actual pattern）。
			m[actual] = "allow"
			continue
		}
		m[p] = "deny"
	}
	return m
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
