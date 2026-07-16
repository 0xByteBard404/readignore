package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xByteBard404/readignore/internal/readignore"
)

// loadPatterns 读取仓库根的 .readignore，解析后返回其 Raw 规则行列表。
//
// 这是 generate/check 命令使用的公共逻辑：从磁盘读规则文件 → 交给
// readignore（ParseSections 的 Read 段）→ 提取每条 Pattern 的 Raw 文本（已去注释/空行）。
// install 命令需要分段（Edit/Delete）信息，故改用 [loadSections]。
//
// 设计要点：
//   - 文件不存在时返回友好 error（含路径与「先跑 readignore init」的可操作提示），
//     而非裸 *PathError，让 CLI 输出对用户友好；
//   - 不返回 *Matcher：CLI 各命令只需要 Raw 行（适配器 Generate 也以 RawPatterns
//     为准），matcher 命中能力留给 readignore 包自身测试覆盖；
//   - repoRoot 为空或路径不可达时同样返回友好 error。
//
// 返回的切片保持文件中出现顺序（readignore 已保证），顺序对取反语义敏感。
//
// 仅返 Read 段的 Raw 行（与历史「无段头 = read」语义一致）；需要 Edit/Delete 段请用 [loadSections]。
// 实现为对 [loadSections] 的薄封装（取 Read 段 → patternStrings）。
func loadPatterns(repoRoot string) ([]string, error) {
	sections, err := loadSections(repoRoot)
	if err != nil {
		return nil, err
	}
	return patternStrings(sections.Read), nil
}

// loadSections 读取仓库根的 .readignore，用 readignore.ParseSections 解析为
// Read/Edit/Delete 三段，返回完整的 *Sections（install 命令据此填 Plan.Rules）。
//
// 文件读取与友好错误处理与 [loadPatterns] 一致（不存在/不可达/解析失败均返回
// 可操作 error）；区别仅在返回分段结果而非仅 Read 段的 Raw 行。
func loadSections(repoRoot string) (*readignore.Sections, error) {
	if repoRoot == "" {
		return nil, fmt.Errorf("仓库根路径为空")
	}
	path := filepath.Join(repoRoot, readignoreFileName)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("未找到 %s：请先运行 `readignore init` 生成模板", path)
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}

	sections, err := readignore.ParseSections(string(content))
	if err != nil {
		// 当前 readignore.ParseSections 不会返回非 nil error（保留为未来语法校验扩展），
		// 但仍如实向上传递，保持契约完整。
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	return sections, nil
}

// patternStrings 从一个 Matcher 提取其 Patterns 的 Raw 文本列表（保持顺序）。
// m 为 nil 时返 nil（如某段在 .readignore 中完全缺席时 sections.<段> 仍为非 nil
// 空 Matcher，但防御性处理 nil 以备未来变更）。
func patternStrings(m *readignore.Matcher) []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m.Patterns))
	for _, p := range m.Patterns {
		out = append(out, p.Raw)
	}
	return out
}
