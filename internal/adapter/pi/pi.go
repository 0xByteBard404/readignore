// Package pi 实现 pi（Earendil-works coding-agent，earendil-works/pi）适配器：把
// .readignore 翻译成 pi 的 TypeScript extension，通过 override 内置 `read` 工具实现
// 「执行前可编程硬拦」。
//
// 仅覆盖 Read 段。edit/delete 段在 pi 上诚实标注 unsupported（详见下方「edit/delete 限制」）。
//
// pi 是 TypeScript AI agent，extension 系统允许 registerTool 注册与内置工具同名的
// 工具来 override 它（官方 examples/extensions/tool-override.ts 即 override `read`
// 拦截 .env——正是 readignore 想做的）。pi 因此对 Read 段归类为 Hard（最强拦截强度）。
//
// v0.3 起匹配权威统一收敛到 `readignore match`（go-git format/gitignore）：生成的 TS
// extension 不再内嵌 patterns、不再手写 gitignore matcher，而是 override 内置 `read`
// 工具，execute 内调 child_process.execFileSync("readignore", ["match", path])，exit 1
// 即 deny（Access denied）。.readignore 在运行时由 readignore match 直接读盘，故改
// .readignore 不必 re-install 即立即生效（动态读核心价值）。
//
// edit/delete 限制（诚实标注 unsupported，YAGNI）：
//
//	pi 的 override 机制本身能 override write/edit 工具（packages/coding-agent/src/core/tools/
//	下确有 write.ts(name "write") 与 edit.ts(name "edit")），但本适配器当前不支持 [edit]/[delete]
//	段，理由：
//	 1. readignore match 仅查 Read 段（internal/readignore parser.go: Parse 返回 s.Read），
//	    无法按段区分 Edit——要 override write 只阻断 Edit 段文件，需先给 readignore match
//	    增加 --section 查询能力（触及 CLI match 命令与 parser API，属另一独立工作面）；
//	 2. 即便段可区分，override write/edit 需在「放行」分支真正落盘（pi 的 registerTool 是
//	    覆盖而非 super()，无委托原实现的口子）——须在生成的 TS 里复刻 pi write 的全部语义
//	    （建目录、原子写、截断、编码 …），随 pi 上游漂移易碎。
//	故按 YAGNI 不强行实现；InstallInstructions 如实声明 "edit/delete not supported on pi"。
//	delete 段三个 config 型 adapter（opencode/kilocode/pi）本就都 best-effort 不覆盖，
//	仅 claudecode/codex 的 Bash rm 走 Delete 段。
//
// 产物（Generate 返回，由调用方/安装层写入磁盘）：
//   - .pi/extensions/readignore.ts  (0644)  override `read`：调 `readignore match <path>`
//     → exit 1（deny）返回 Access denied，否则委托真正 readFile。
//
// 源码确认要点（pi，earendil-works/pi，packages/coding-agent，commit 见仓库）：
//   - extension API：src/core/extensions/types.ts 的 ExtensionAPI.registerTool(tool: ToolDefinition)。
//     ToolDefinition{name, label, description, parameters: TSchema, execute(toolCallId, params, signal, onUpdate, ctx)}。
//   - override 机制：registerTool 用与内置工具同名的 name 即 override（loader.ts registerTool 实现，
//     tool-override.ts 实证 name:"read" 覆盖内置 read）。
//   - read 工具参数名：tool-override.ts 的 readSchema = Type.Object({ path: ... })，
//     execute 解构 params.path——参数名是 `path`（非 file_path）。
//   - write/edit 工具存在：src/core/tools/write.ts (name:"write", schema path) 与 edit.ts (name:"edit")。
//   - 加载方式：src/core/extensions/loader.ts "Project-local extensions: cwd/${CONFIG_DIR_NAME}/extensions/"，
//     CONFIG_DIR_NAME=".pi"（config.ts）——即 .pi/extensions/*.ts 项目级自动加载。
//
// init() 调 adapter.Register 自登记，CLI 通过 adapter.Get("pi") 发现本适配器。
package pi

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

// Adapter 实现 [adapter.Adapter]，把 .readignore 翻译成 pi TS extension。
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

// ID 返回稳定短标识 "pi"，用作 CLI 参数、配置键与 registry 索引。
// 全小写、无空格、跨版本不变。
func (Adapter) ID() string { return "pi" }

// Strength 返回 [adapter.StrengthHard]：pi extension override 内置 read 工具，
// 在 LLM 真正拿到文件内容前由 TS 判定（调 readignore match）并返回 Access denied，
// 是当前支持的最强拦截强度。
func (Adapter) Strength() adapter.Strength { return adapter.StrengthHard }

// Detect 探测 repoRoot 下是否已存在 pi 痕迹：.pi/ 目录或 .pi/extensions/ 子目录。
// 命中仅影响 CLI 是否默认启用本适配器；Generate 即便未检测到也能产出可手动安装的文件。
func (Adapter) Detect(repoRoot string) bool {
	if repoRoot == "" {
		return false
	}
	if fi, err := os.Stat(filepath.Join(repoRoot, ".pi")); err == nil && fi.IsDir() {
		return true
	}
	if fi, err := os.Stat(filepath.Join(repoRoot, ".pi", "extensions")); err == nil && fi.IsDir() {
		return true
	}
	return false
}

// InstallInstructions 给出「如何让 pi 读取所生成文件」的人类可读说明，并诚实标注
// edit/delete 段当前 unsupported。
//
// pi 启动时自动扫描 .pi/extensions/*.ts 并加载（loader.ts），故文件写入后
// 下次启动 pi 即生效，无需额外配置。也可用 `pi -e ./readignore.ts` 临时加载。
//
// 覆盖范围：仅 Read 段（override read 工具 → readignore match）。edit/delete 段不支持
// （理由见包 godoc「edit/delete 限制」：readignore match 仅查 Read 段，且 override write/edit
// 需在放行分支复刻 pi 写语义，易碎且超本任务范围）。
//
// v0.3 提醒：本 extension 调 `readignore match`（go-git 权威），故 readignore 二进制
// 必须在 pi 进程的 PATH 里；改 .readignore 无需 re-install 即立即生效。
func (Adapter) InstallInstructions() string {
	return "已写入 .pi/extensions/readignore.ts。pi 启动时自动扫描 .pi/extensions/*.ts " +
		"并加载（无需额外配置），下次启动 pi 即生效。也可用 `pi -e ./readignore.ts` 临时加载。" +
		"覆盖范围：仅 Read 段（override read 工具 → readignore match）。" +
		"edit/delete not supported on pi——readignore match 当前仅查 Read 段，且 override write/edit " +
		"需在放行分支复刻 pi 的写工具语义（易碎）；如需 edit/delete 拦截请用 claudecode/codex 适配器。" +
		"注意：本 extension 调 `readignore match`（go-git 权威），readignore 二进制必须在 " +
		"PATH 里；改 .readignore 无需 re-install 即立即生效。"
}

// Generate 依据 plan 产出单个 TS extension 文件。
//
// v0.3 关键设计：
//   - extension 模板是常量（extensionTmpl），不再注入 patterns——匹配判定全在
//     `readignore match` 侧（读 cwd/.readignore），故改 .readignore 不必 re-install
//     即立即生效（动态读核心价值）；
//   - plan 不再参与生成（无 patterns/字面量注入），保留签名仅为满足 adapter.Adapter 契约。
func (Adapter) Generate(plan adapter.Plan) ([]adapter.GeneratedFile, error) {
	_ = plan // v0.3: extension 是常量，不读 plan（readignore match 运行时读 cwd/.readignore）。
	content, err := renderExtension()
	if err != nil {
		return nil, fmt.Errorf("pi: render extension: %w", err)
	}
	return []adapter.GeneratedFile{
		{
			Path:    ".pi/extensions/readignore.ts",
			Mode:    0o644,
			Content: content,
		},
	}, nil
}

// renderExtension 渲染 extension.ts.tmpl。v0.3 起模板无占位符（纯常量），
// 但保留 text/template 解析路径以保持向前兼容（未来若需注入变量可加占位符）。
func renderExtension() (string, error) {
	return extensionTmpl, nil
}
