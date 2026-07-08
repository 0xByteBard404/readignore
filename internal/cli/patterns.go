package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xByteBard404/readignore/internal/readignore"
)

// loadPatterns 读取仓库根的 .readignore，解析后返回其 Raw 规则行列表。
//
// 这是 generate/install/check 命令共享的公共逻辑：从磁盘读规则文件 → 交给
// readignore.Parse → 提取每条 Pattern 的 Raw 文本（已去注释/空行）。
//
// 设计要点：
//   - 文件不存在时返回友好 error（含路径与「先跑 readignore init」的可操作提示），
//     而非裸 *PathError，让 CLI 输出对用户友好；
//   - 不返回 *Matcher：CLI 各命令只需要 Raw 行（适配器 Generate 也以 RawPatterns
//     为准），matcher 命中能力留给 readignore 包自身测试覆盖；
//   - repoRoot 为空或路径不可达时同样返回友好 error。
//
// 返回的切片保持文件中出现顺序（readignore.Parse 已保证），顺序对取反语义敏感。
func loadPatterns(repoRoot string) ([]string, error) {
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

	matcher, err := readignore.Parse(string(content))
	if err != nil {
		// 当前 readignore.Parse 不会返回非 nil error（保留为未来语法校验扩展），
		// 但仍如实向上传递，保持契约完整。
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}

	raws := make([]string, 0, len(matcher.Patterns))
	for _, p := range matcher.Patterns {
		raws = append(raws, p.Raw)
	}
	return raws, nil
}
