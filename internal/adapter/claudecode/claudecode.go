// Package claudecode 实现 Claude Code 适配器：把 .readignore 翻译成 Claude Code
// 的 PreToolUse hook，实现「执行前可编程硬拦」—— 五个目标工具里唯一能在工具真正
// 执行前用脚本判定并阻断的，故本包是 readignore 的参考实现。
//
// 产物三件套（Generate 返回，由调用方/安装层写入磁盘）：
//   - .claude/hooks/readignore.sh  (0755)  从 tool_input JSON 抽取目标路径/命令，
//     交 readignore.py 判定，命中即输出 PreToolUse deny JSON；
//   - .claude/hooks/readignore.py  (0644)  匹配引擎：用标准库实现 gitignore 语义
//     (*、**、!、目录尾斜杠)，零第三方依赖；规则在 Generate 时内嵌；
//   - .claude/settings.json        (0)     PreToolUse 注册片段（与既有 settings.json
//     的合并留给阶段5 CLI install 层，本适配器只 Generate 片段）。
//
// v0.2 起 sh 与 py 内容由 [github.com/0xByteBard404/readignore/internal/adapter/shared/hookengine]
// 生成（与 codex 适配器共用 Claude-style PreToolUse 协议）；本包只负责 Claude Code 专属的
// 配置包装（settings.json 片段）与文件路径/权限位的拼装。
//
// init() 调 adapter.Register 自登记，CLI 通过 adapter.Get("claude-code") 发现本适配器。
package claudecode

import (
	"os"
	"path/filepath"

	"github.com/0xByteBard404/readignore/internal/adapter"
	"github.com/0xByteBard404/readignore/internal/adapter/shared/hookengine"
)

// Adapter 实现 [adapter.Adapter]，把 .readignore 翻译成 Claude Code PreToolUse hook。
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

// ID 返回稳定短标识 "claude-code"，用作 CLI 参数、配置键与 registry 索引。
// 全小写、无空格、跨版本不变。
func (Adapter) ID() string { return "claude-code" }

// Strength 返回 [adapter.StrengthHard]：Claude Code PreToolUse hook 在工具真正
// 执行前由 bash/python 判定并阻断，是当前支持的最强拦截强度。
func (Adapter) Strength() adapter.Strength { return adapter.StrengthHard }

// Detect 探测 repoRoot 下是否已存在 Claude Code 痕迹：.claude/ 目录或 CLAUDE.md。
// 命中仅影响 CLI 是否默认启用本适配器；Generate 即便未检测到也能产出可手动安装的文件。
func (Adapter) Detect(repoRoot string) bool {
	if repoRoot == "" {
		return false
	}
	if fi, err := os.Stat(filepath.Join(repoRoot, ".claude")); err == nil && fi.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "CLAUDE.md")); err == nil {
		return true
	}
	return false
}

// InstallInstructions 给出「如何让 Claude Code 读取所生成文件」的人类可读说明。
// Claude Code 的 settings watcher 实时加载 .claude/ 下变更，故无需重启。
func (Adapter) InstallInstructions() string {
	return "已写入 .claude/。Claude Code settings watcher 实时加载，无需重启。"
}

// Generate 依据 plan 产出三个文件（sh / py / settings.json）。
//
// 关键设计：
//   - patterns 在此刻以合法 Python 字面量内嵌进 readignore.py（generate 时即冻结），
//     运行时不再读盘，避免 .readignore 缺失/漂移导致 hook 行为不确定；
//   - sh 仅做 JSON 字段抽取（grep，无 jq 依赖），匹配判定全在 py 里，便于跨平台
//     （sh 里只调 python，不在 bash 里重写匹配逻辑）；
//   - settings.json 只 Generate PreToolUse 片段，与既有 settings 的合并由 CLI 完成。
//
// v0.2 起 sh/py 内容由 [hookengine] 包生成（codex 适配器共用同一引擎）；本方法只负责
// 文件路径与权限位的 Claude Code 专属拼装。
//
// 已知限制（YAGNI，不在 v0.1.0/v0.2.0 修功能，仅文档诚实标注）：生成的 python 引擎不区分
// 目录与文件——`foo/` 这类「仅目录」模式会命中非目录的 `foo`（hook 拿到的候选路径
// 不带尾斜杠，引擎也无 stat 调用）。这是安全侧偏置（多拦而非少拦）：被多拦的普通
// 文件 foo 仍可由用户加 `!foo` 取反放行。严格的目录/文件区分留待后续版本。
func (Adapter) Generate(plan adapter.Plan) ([]adapter.GeneratedFile, error) {
	return []adapter.GeneratedFile{
		{
			Path:    ".claude/hooks/readignore.sh",
			Mode:    0o755,
			Content: hookengine.BuildShScript(plan.RawPatterns),
		},
		{
			Path:    ".claude/hooks/readignore.py",
			Mode:    0o644,
			Content: hookengine.BuildPyEngine(plan.RawPatterns),
		},
		{
			Path:    ".claude/settings.json",
			Mode:    0,
			Content: settingsJSON(),
		},
	}, nil
}

// settingsJSON 返回 .claude/settings.json 片段：仅 PreToolUse 注册项。
// 与既有 settings.json 的深度合并由阶段5 CLI install 层负责，本适配器只产片段。
//
// 这是 Claude Code 专属的配置包装（matcher 字段、hooks 数组结构），
// 故不随 sh/py 一起搬到 hookengine——codex 等其它适配器各有各的配置包装方式。
func settingsJSON() string {
	return `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Read|Grep|Glob|Bash",
        "hooks": [
          {
            "type": "command",
            "command": "bash .claude/hooks/readignore.sh",
            "shell": "bash",
            "timeout": 5
          }
        ]
      }
    ]
  }
}
`
}
