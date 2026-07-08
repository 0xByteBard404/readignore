// Package readignore 解析 .readignore（gitignore 语法）并判断路径命中。
//
// 本包以 github.com/go-git/go-git/v5/plumbing/format/gitignore 为权威 matcher，
// 保证与 git 实际行为一致（取反、** 任意层级、目录锚定等）。
package readignore

import (
	"bufio"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// Pattern 表示一条解析后的 .readignore 规则。
// Raw 保留原始行文本（已 Trim 前后空白），供后续适配器生成各 AI agent
// 的原生防护配置时引用，避免再次猜测用户书写。
type Pattern struct {
	// Raw 是 .readignore 中的原始规则文本（不含行尾换行；注释/空行不会进入 Pattern）。
	Raw string
	// gitPattern 是 go-git 解析后的模式，负责实际的命中判断。
	gitPattern gitignore.Pattern
}

// Matcher 是 .readignore 解析产物，可对相对路径做命中判断。
type Matcher struct {
	// Patterns 按文件中出现顺序排列（顺序对取反语义至关重要：go-git 的
	// matcher 从最后一条规则向前扫描，最先命中者胜出）。
	Patterns []Pattern
	// matcher 是 go-git 权威 matcher，Matches 委托给它。
	matcher gitignore.Matcher
}

// Parse 解析 .readignore 文本，返回可命中判断的 *Matcher。
//
// 语法遵循 gitignore 规则：
//   - 以 `#` 开头的行为注释，忽略；空行（含仅空白）忽略。
//   - 以 `!` 开头表示取反（放行），最后规则胜出。
//   - `*` / `**` / `?` 通配，`/` 锚定目录，`*.ext` 后缀匹配。
//
// 注释与空行不会进入返回的 Patterns。
func Parse(content string) (*Matcher, error) {
	if content == "" {
		return &Matcher{Patterns: nil, matcher: gitignore.NewMatcher(nil)}, nil
	}

	m := &Matcher{}
	var gitPatterns []gitignore.Pattern

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		raw := strings.TrimRight(line, "\r\n")
		// 保留原始（去掉首尾空白）用于 Raw 字段，同时用原文判断注释。
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// domain 为 nil：.readignore 规则相对仓库根，go-git 据此匹配。
		gp := gitignore.ParsePattern(trimmed, nil)
		m.Patterns = append(m.Patterns, Pattern{Raw: trimmed, gitPattern: gp})
		gitPatterns = append(gitPatterns, gp)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	m.matcher = gitignore.NewMatcher(gitPatterns)
	return m, nil
}

// Matches 判断给定的相对路径是否被 .readignore 命中（即应被忽略/拦截）。
//
// 取反语义由 go-git matcher 保证：从最后一条规则向前扫描，最先命中者
// 决定结果（Exclude=true 命中，Include=false 取反放行）。
//
// 路径分隔符统一处理：传入的 Windows 反斜杠路径会被规范化成 `/`，
// 再按 `/` 切分成路径段交给 go-git，保证跨平台一致。
func (m *Matcher) Matches(p string) bool {
	if m == nil || m.matcher == nil {
		return false
	}
	// 统一分隔符：Windows `\` -> `/`，再用 path 包切分。
	slashPath := p
	if strings.ContainsRune(p, '\\') {
		slashPath = strings.ReplaceAll(p, "\\", "/")
	}
	// path.Split 用于识别是否目录路径（末尾 `/`）。
	isDir := strings.HasSuffix(slashPath, "/")
	// 去掉末尾分隔符后按 / 切分。
	clean := strings.TrimSuffix(slashPath, "/")
	if clean == "" {
		return false
	}
	segments := strings.Split(clean, "/")
	// 过滤可能的空段（连续斜杠或前导斜杠），go-git 不期望空段。
	cleanSegs := make([]string, 0, len(segments))
	for _, s := range segments {
		if s == "" {
			// 视为目录锚定的根，对相对路径语义无意义，跳过空段。
			continue
		}
		cleanSegs = append(cleanSegs, s)
	}
	if len(cleanSegs) == 0 {
		return false
	}
	return m.matcher.Match(cleanSegs, isDir)
}
