// Package adapter 定义「工具适配器」抽象层。
//
// readignore 把 `.readignore`（gitignore 语法）适配成各 AI coding agent 原生
// 防护配置（Claude Code PreToolUse hook、opencode permissions、Cursor rules 等）。
// 本包声明所有具体适配器（claudecode / opencode / ……）共同实现的 [Adapter] 接口，
// 以及配套的 [Strength] / [GeneratedFile] / [Plan] 值对象与全局 [Register] 注册表。
//
// 本包不依赖任何具体适配器实现；具体适配器在各自 init() 中调用 [Register] 自登记。
package adapter

// Strength 描述一个适配器对工具行为的「拦截强度」。
//
// 强度由弱到强分三档，用于 CLI 排序、文档展示与用户预期管理：
//   - StrengthHard：执行前可编程拦截（最强，运行时阻断）；
//   - StrengthConfig：生成原生 deny 配置（中，由工具加载时生效）；
//   - StrengthSoft：仅自然语言规则（最弱，依赖模型自觉遵守）。
type Strength string

const (
	// StrengthHard 表示适配器通过「执行前可编程拦截」阻止越权访问，
	// 例如 Claude Code 的 PreToolUse hook：在工具真正执行前由脚本/进程判断
	// 并阻断，是当前支持的最强强度。
	StrengthHard Strength = "hard"

	// StrengthConfig 表示适配器生成工具「原生 deny 配置」，
	// 例如 opencode 的 permissions 字段：工具加载该配置后按规则拒绝访问。
	// 强度居中，依赖工具忠实读取配置。
	StrengthConfig Strength = "config"

	// StrengthSoft 表示适配器仅产出「自然语言规则」，
	// 例如 Cursor 的 .cursor/rules：无强制力，依赖模型自觉遵守，强度最弱。
	StrengthSoft Strength = "soft"
)

// GeneratedFile 描述适配器 Generate 产出的单个待写入文件。
//
// 调用方（安装层/cmd）负责把这些文件写到磁盘并设置权限。Mode 用 uint32
// 而非 os.FileMode，避免把 OS 概念泄漏进 domain 层；Mode 为 0 表示
// 「使用调用方默认」（典型场景：配置文件用 0644、可执行 hook 用 0755）。
type GeneratedFile struct {
	// Path 相对仓库根的 POSIX 风格路径（用 `/` 分隔），例如 ".claude/hooks/hook.json"。
	Path string
	// Content 文件完整内容（UTF-8 文本）。
	Content string
	// Mode 文件权限位（如 0644 / 0755）；为 0 表示由调用方使用默认权限
	// （典型：配置文件 0644、可执行 hook 0755）。详见类型级 doc 的「默认」语义。
	Mode uint32
}

// ClassifiedPatterns 是按权限分类的原始规则（供 opencode/kilo 等需要把规则
// 写进配置文件的 adapter 使用）。每段 = ParseSections 对应段的原始 pattern 行。
type ClassifiedPatterns struct {
	Read   []string
	Edit   []string
	Delete []string
}

// Plan 是传递给 Adapter.Generate 的输入：把 .readignore 的解析结果与
// 仓库上下文打包，供适配器据此生成各工具原生防护配置。
type Plan struct {
	// RepoRoot 仓库根的绝对路径。适配器可据此 Detect 已安装工具、解析相对路径。
	RepoRoot string
	// MatchedPaths 当前仓库根下命中 .readignore 规则的路径集合
	// （相对仓库根、POSIX 风格 `/` 分隔；由调用方/CLI 用 gitignore 语义对工作树
	// 扫描得出，通常按字典序去重）。主要供日志/上下文展示与未来内容差异比对使用；
	// 适配器做匹配时应**直接用 RawPatterns**——以原始规则为单一事实源更稳定、
	// 无遗漏（MatchedPaths 是某次扫描的快照，可能因仓库内容变化而过时）。
	MatchedPaths []string
	// RawPatterns .readignore 的原始规则行（已去注释/空行，保留取反行）。
	// 适配器生成各工具原生防护配置时应直接引用本字段，而非再次猜测用户书写。
	// 语义：Read+Edit+Delete 全集（保持单一事实源；历史 adapter 仅消费 Read 段时
	// 与 Rules.Read 等价，故保持全集不破坏既有行为）。
	RawPatterns []string
	// Rules 是按权限分类的分段原始规则（readignore.ParseSections 的三段产物）。
	// 需要把规则分别写进配置文件不同字段段的 adapter（如 opencode permissions、
	// kilocode）应读 Rules 而非 RawPatterns；只关心「全集 deny」的 adapter（如
	// claudecode 的 hook）继续用 RawPatterns 即可。
	Rules ClassifiedPatterns
}

// Adapter 是「工具适配器」抽象：把 .readignore 规则翻译成某个 AI coding agent
// 的原生防护配置（hook / 配置文件 / 规则文档）。
//
// 实现方（claudecode / opencode / ……）在自身 init() 中调用 [Register] 自登记。
// 接口刻意保持最小：仅含身份、强度、检测、生成、安装说明五个方法；Verify 类
// 运行时校验目前无消费者，按 YAGNI 暂不加入。
type Adapter interface {
	// ID 返回适配器的稳定短标识（如 "claudecode"、"opencode"），
	// 用作 CLI 参数、配置键与 registry 索引；应全小写、无空格、跨版本不变。
	ID() string

	// Strength 返回该适配器对工具行为的拦截强度（Hard/Config/Soft）。
	// 用于 CLI 排序、文档展示与用户预期管理。
	Strength() Strength

	// Detect 探测仓库 repoRoot 下是否已安装对应工具（例如存在 .claude/ 目录）。
	// 返回 true 表示「检测到该工具」，Generate 通常仍应能运行（即使未检测到，
	// 也可生成供用户手动安装的文件）；Detect 主要供 CLI 决定默认启用的适配器。
	Detect(repoRoot string) bool

	// Generate 依据 plan 生成该工具的原生防护文件（可能多个）。
	// 返回的文件由调用方负责写入磁盘；error 非 nil 表示生成失败。
	Generate(plan Plan) ([]GeneratedFile, error)

	// InstallInstructions 返回「如何让该工具读取/生效所生成文件」的人类可读说明，
	// 通常包含目标路径、是否需要重启、可选的环境变量等。CLI 在生成后打印。
	InstallInstructions() string
}
