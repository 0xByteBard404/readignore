// Package pi 实现 pi（Earendil-works coding-agent）适配器：把 .readignore 翻译成
// pi 的 TypeScript extension，通过 override 内置 `read` 工具实现「执行前可编程硬拦」。
//
// pi 是 TypeScript AI agent，extension 系统允许 registerTool 注册与内置工具同名的
// 工具来 override 它（官方 examples/extensions/tool-override.ts 即 override `read`
// 拦截 .env——正是 readignore 想做的）。pi 因此归类为 Hard（最强拦截强度）。
//
// 与 claudecode/codex 不同，pi 是 TS 而非 sh+py：shared/hookengine（sh+py）不适用，
// 本适配器单独实现一份手写 gitignore 匹配的 TS extension，零 npm 依赖（pi extension
// 单文件加载，避免运行时 require 第三方 glob 库）。
//
// 产物（Generate 返回，由调用方/安装层写入磁盘）：
//   - .pi/extensions/readignore.ts  (0644)  override `read`：检查 path 命中
//     patterns → 命中返回 Access denied，否则委托真正读取。
//
// 源码确认要点（pi，packages/coding-agent，commit 见仓库）：
//   - extension API：src/core/extensions/types.ts 的 ExtensionAPI.registerTool(tool: ToolDefinition)。
//     ToolDefinition{name, label, description, parameters: TSchema, execute(toolCallId, params, signal, onUpdate, ctx)}。
//   - override 机制：registerTool 用与内置工具同名的 name 即 override（loader.ts:228 registerTool 实现，
//     tool-override.ts 实证 name:"read" 覆盖内置 read）。
//   - read 工具参数名：examples/extensions/tool-override.ts 的 readSchema = Type.Object({ path: ... })，
//     execute 解构 params.path——参数名是 `path`（非 file_path）。
//   - 加载方式：src/core/extensions/loader.ts:672 "Project-local extensions: cwd/${CONFIG_DIR_NAME}/extensions/"，
//     CONFIG_DIR_NAME=".pi"（config.ts:491）——即 .pi/extensions/*.ts 项目级自动加载。
//
// init() 调 adapter.Register 自登记，CLI 通过 adapter.Get("pi") 发现本适配器。
package pi

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"

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
// 在 LLM 真正拿到文件内容前由 TS 判定并返回 Access denied，是当前支持的最强拦截强度。
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

// InstallInstructions 给出「如何让 pi 读取所生成文件」的人类可读说明。
//
// pi 启动时自动扫描 .pi/extensions/*.ts 并加载（loader.ts:672），故文件写入后
// 下次启动 pi 即生效，无需额外配置。也可用 `pi -e ./readignore.ts` 临时加载。
func (Adapter) InstallInstructions() string {
	return "已写入 .pi/extensions/readignore.ts。pi 启动时自动扫描 .pi/extensions/*.ts " +
		"并加载（无需额外配置），下次启动 pi 即生效。也可用 `pi -e ./readignore.ts` 临时加载。"
}

// Generate 依据 plan 产出单个 TS extension 文件。
//
// 关键设计：
//   - patterns 在此刻以 TS 字符串字面量数组内嵌进 readignore.ts（generate 时即冻结），
//     运行时不读盘，避免 .readignore 缺失/漂移导致 override 行为不确定；
//   - TS 匹配引擎**手写**（零 npm 依赖），覆盖 gitignore 语义子集：
//   - `**/`  → 任意层级目录前缀（含零层）；
//   - `**`   → 跨任意层级；
//   - `*`    → 单层内任意非 / 字符；
//   - `?`    → 单个非 / 字符；
//   - 尾 `/` → 仅匹配目录（运行时无 stat，安全侧偏置：多拦而非少拦）；
//   - 无 `/` → basename 匹配（任意层级同名条目）；
//   - 取反 (`!`)：按文件顺序求值，最后一条命中者决定结果（与 go-git/py 引擎一致）。
//   - override `read`：execute 内 isBlocked(params.path) → 命中返回 Access denied，
//     否则委托真正 readFile（参考官方 tool-override.ts）。
func (Adapter) Generate(plan adapter.Plan) ([]adapter.GeneratedFile, error) {
	content, err := renderExtension(plan.RawPatterns)
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

// renderExtension 用 text/template 渲染 extension.ts.tmpl，注入 patterns 的 TS 字面量数组。
func renderExtension(rawPatterns []string) (string, error) {
	literals := patternsAsTSLiterals(rawPatterns)
	tmpl, err := template.New("pi-extension").Parse(extensionTmpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{
		"PatternsAsTSLiterals": literals,
	}); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// patternsAsTSLiterals 把 patterns 渲染成 TS 字符串字面量数组（双引号 + 转义）。
//
// 取反规则（!.env.example）原样透传，不在生成端折叠——运行时按文件顺序 last-match-wins
// 求值更稳定（与 py 引擎语义一致）。空 patterns 渲染为 `[]`。
//
// 安全：逐字符转义反斜杠、双引号、换行、控制字符（NUL/VT/DEL 等），保证含特殊字符的
// pattern 不会破坏生成的 TS 语法（与 hookengine.pythonRepr 同等严格）。
func patternsAsTSLiterals(patterns []string) string {
	if len(patterns) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range patterns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(tsStringLiteral(s))
	}
	b.WriteByte(']')
	return b.String()
}

// tsStringLiteral 渲染单个字符串为 TS 双引号字面量，转义反斜杠、双引号、换行、
// 及所有控制字符（C0 0x00-0x1F 与 DEL 0x7F），保证含特殊字符的 pattern 不会破坏
// 生成的 TS 语法。等价于 hookengine.pythonRepr 的转义策略，但产出 TS 合法（TS 与 JS
// 双引号字符串转义规则一致：\ \" \n \r \t \xNN 均合法）。
func tsStringLiteral(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else if unicode.IsControl(r) {
				// 其它 C1 控制字符（0x80-0x9F）也转义，避免生成 TS 含裸控制字符。
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
