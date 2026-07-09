// Package codex 实现 OpenAI Codex CLI 适配器：把 .readignore 翻译成 codex 的
// Claude-style PreToolUse hook，实现「执行前可编程硬拦」。
//
// codex 的 hook 协议与 Claude Code 高度近似（codex-rs/features/src/lib.rs:87 明示
// "Claude-style lifecycle hooks"）：同为 PreToolUse 事件 + permissionDecision:"deny"
// 决策。本适配器因此与 claudecode 共用 shared/hookengine 生成的 sh+py，仅在配置包装
// （hooks.json schema、文件落点 .codex/、hook 信任提示）上做 codex 专属处理。
//
// 产物三件套（Generate 返回，由调用方/安装层写入磁盘）：
//   - .codex/hooks/readignore.sh  (0755)  从 tool_input JSON 抽取目标路径/命令，
//    交 readignore.py 判定，命中即输出 PreToolUse deny JSON；
//   - .codex/hooks/readignore.py  (0644)  匹配引擎：标准库实现 gitignore 语义；
//   - .codex/hooks.json           (0)     PreToolUse 注册（Claude-style 结构）。
//
// 源码确认要点（codex-rs，commit 见仓库）：
//   - hooks.json schema：config/src/hook_config.rs 的 HooksFile{description?, hooks: HookEventsToml}。
//     HookEventsToml 用 "PreToolUse" 等 PascalCase 键 → Vec<MatcherGroup>；
//     MatcherGroup{matcher?: string, hooks: Vec<HookHandlerConfig>}；
//     HookHandlerConfig 为 #[serde(tag="type")] enum，Command 变体字段：
//       command / commandWindows? / timeout(对应 Rust 字段 timeout_sec) / async? / statusMessage?。
//     故 codex 的 timeout 字段名是 "timeout"（与 Claude Code 一致），且**无** "shell" 字段。
//   - matcher 语义（hooks/src/events/common.rs）：若 matcher 仅含 [A-Za-z0-9_|]
//     则按 exact pipe 匹配（"Read|Grep|Glob|Bash" 等价精确匹配任一）；否则当正则。
//   - PreToolUse 工具入参（schema.rs PreToolUseCommandInput）：tool_name + tool_input。
//     codex 把 shell 工具的 hook 事件以 tool_name="Bash"、tool_input={"command":"..."}
//     暴露（core/tests/suite/hooks.rs:3242 实证），与 Claude Code 字段名一致——
//     故 shared sh 的 extract_field(file_path/path/pattern/command) 直接复用，无需包装。
//
// init() 调 adapter.Register 自登记，CLI 通过 adapter.Get("codex") 发现本适配器。
package codex

import (
	"os"
	"path/filepath"

	"github.com/0xByteBard404/readignore/internal/adapter"
	"github.com/0xByteBard404/readignore/internal/adapter/shared/hookengine"
)

// Adapter 实现 [adapter.Adapter]，把 .readignore 翻译成 codex PreToolUse hook。
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

// ID 返回稳定短标识 "codex"，用作 CLI 参数、配置键与 registry 索引。
// 全小写、无空格、跨版本不变。
func (Adapter) ID() string { return "codex" }

// Strength 返回 [adapter.StrengthHard]：codex PreToolUse hook 在工具真正执行前
// 由 bash/python 判定并阻断，是当前支持的最强拦截强度。
func (Adapter) Strength() adapter.Strength { return adapter.StrengthHard }

// Detect 探测 repoRoot 下是否已存在 codex 痕迹：.codex/ 目录或 AGENTS.md。
// 命中仅影响 CLI 是否默认启用本适配器；Generate 即便未检测到也能产出可手动安装的文件。
func (Adapter) Detect(repoRoot string) bool {
	if repoRoot == "" {
		return false
	}
	if fi, err := os.Stat(filepath.Join(repoRoot, ".codex")); err == nil && fi.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "AGENTS.md")); err == nil {
		return true
	}
	return false
}

// InstallInstructions 给出「如何让 codex 读取所生成文件」的人类可读说明。
//
// codex 加载 ~/.codex 与项目 .codex/hooks.json 作为 hook 源；首次执行项目级 hook
// 时会做信任校验（用户需在交互提示里确认信任，或以 --dangerously-bypass-hook-trust
// 绕过——见 codex-rs/exec/tests/suite/hooks.rs:36 的同名义旗）。
func (Adapter) InstallInstructions() string {
	return "已写入 .codex/。codex 加载 .codex/hooks.json 作为 hook 源。" +
		"首次执行项目级 hook 时 codex 会做信任校验：在交互提示中确认信任该 hook，" +
		"或以 --dangerously-bypass-hook-trust 绕过信任检查。"
}

// Generate 依据 plan 产出三个文件（sh / py / hooks.json）。
//
// 关键设计：
//   - patterns 在此刻以合法 Python 字面量内嵌进 readignore.py（generate 时即冻结），
//     运行时不再读盘，避免 .readignore 缺失/漂移导致 hook 行为不确定；
//   - sh/py 内容由 [hookengine] 生成（与 claudecode 共用 Claude-style PreToolUse 协议）；
//     codex 的 tool_input 字段名（command 等）与 Claude Code 一致，故 sh 直接复用；
//   - hooks.json 是 codex 专属的配置包装（HooksFile schema），matcher 用 exact pipe，
//     handler 字段仅 command/timeout（无 codex 不支持的 "shell" 字段）。
func (Adapter) Generate(plan adapter.Plan) ([]adapter.GeneratedFile, error) {
	return []adapter.GeneratedFile{
		{
			Path:    ".codex/hooks/readignore.sh",
			Mode:    0o755,
			Content: hookengine.BuildShScriptAt(plan.RawPatterns, ".codex/hooks/readignore.py"),
		},
		{
			Path:    ".codex/hooks/readignore.py",
			Mode:    0o644,
			Content: hookengine.BuildPyEngine(plan.RawPatterns),
		},
		{
			Path:    ".codex/hooks.json",
			Mode:    0,
			Content: hooksJSON(),
		},
	}, nil
}

// hooksJSON 返回 .codex/hooks.json：codex 的 Claude-style PreToolUse 注册。
//
// schema 严格对齐 codex-rs/config/src/hook_config.rs：
//   - 顶层 {"hooks": {<EventName>: [MatcherGroup]}}；
//   - MatcherGroup = {matcher?: string, hooks: [HookHandlerConfig]}；
//   - HookHandlerConfig = {"type":"command","command":string,"timeout"?:u64,...}；
//   - 字段名 "timeout"（Rust 字段 timeout_sec 经 #[serde(rename="timeout")] 序列化）；
//   - **不**写 "shell" 字段：codex 的 Command 变体无此字段（与 Claude Code 不同）。
//
// matcher "Read|Grep|Glob|Bash" 是 codex 的 exact pipe 写法（仅含字母与 |，
// hooks/src/events/common.rs::is_exact_matcher 判定为精确匹配，等价正则
// ^(Read|Grep|Glob|Bash)$ 但更快且无需转义）。
func hooksJSON() string {
	return `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Read|Grep|Glob|Bash",
        "hooks": [
          {
            "type": "command",
            "command": "bash .codex/hooks/readignore.sh",
            "timeout": 5
          }
        ]
      }
    ]
  }
}
`
}
