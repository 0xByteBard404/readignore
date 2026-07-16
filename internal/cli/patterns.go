package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xByteBard404/readignore/internal/readignore"
)

// loadSections 读取仓库根的 .readignore，用 readignore.ParseSections 解析为
// Read/Edit/Delete 三段，返回完整的 *Sections（install 与 generate 命令据此填 Plan.Rules）。
//
// 文件不存在/不可达/解析失败均返回可操作 error（含路径与「先跑 readignore init」提示），
// 而非裸 *PathError，让 CLI 输出对用户友好。
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
