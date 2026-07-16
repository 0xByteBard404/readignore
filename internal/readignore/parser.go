// Package readignore 解析 .readignore（gitignore 语法）并判断路径命中。
//
// 本包以 github.com/go-git/go-git/v5/plumbing/format/gitignore 为权威 matcher，
// 保证与 git 实际行为一致（取反、** 任意层级、目录锚定等）。
package readignore

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// Pattern 表示一条解析后的 .readignore 规则。
// Raw 保留原始行文本（已 Trim 前后空白），供后续适配器生成各 AI agent
// 的原生防护配置时引用，避免再次猜测用户书写。
type Pattern struct {
	// Raw 是 .readignore 中的原始规则文本（不含行尾换行；注释/空行不会进入 Pattern）。
	Raw string
	// gitPattern 持有 go-git 解析后的模式，负责实际的命中判断。
	// 未导出以避免泄漏 go-git 实现细节，保持本包对外 API 稳定。
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
//
// 当前实现不会返回非 nil error；保留 error 返回值供未来语法校验扩展。
//
// 包装 ParseSections：返 Read 段（即无段头的裸 pattern + 显式 [read] 段），
// 保持现有消费者行为不变。
func Parse(content string) (*Matcher, error) {
	s, err := ParseSections(content)
	if err != nil {
		return nil, err
	}
	return s.Read, nil
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
	// 统一分隔符：Windows `\` -> `/`，再用 strings.Split 按 / 切分。
	slashPath := p
	if strings.ContainsRune(p, '\\') {
		slashPath = strings.ReplaceAll(p, "\\", "/")
	}
	// 末尾 `/` 用于识别是否目录路径。
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

// Op 是 .readignore 的权限分类（分段式语法的段头）。
type Op string

const (
	OpRead   Op = "read"
	OpEdit   Op = "edit"
	OpDelete Op = "delete"
)

// Sections 是 .readignore 按权限分类的解析结果，每段一个独立 *Matcher。
// Read 段含 [read] 段规则 + 无段头的裸 pattern（向后兼容）。
type Sections struct {
	Read   *Matcher
	Edit   *Matcher
	Delete *Matcher
}

var sectionHeaderRe = regexp.MustCompile(`(?i)^\[(read|edit|delete)\]\s*(#.*)?$`)

// unknownSectionRe 收紧「未知段头」判定为「整行是 [word]」（可选尾随注释 # ...）。
// 旧启发式 "HasPrefix [ 且 Contains ]" 过宽：把合法 gitignore 行首字符类 pattern
// （如 [abc].txt、[0-9].log、[!ab].env）误判为未知段头并丢弃 → 静默数据丢失。
// 此正则要求 [ 之后到第一个 ] 之间无 ]，且 ] 后只剩空白/注释，从而放过 [abc].txt 这类。
var unknownSectionRe = regexp.MustCompile(`^\[[^\]]+\]\s*(#.*)?$`)

// ParseSections 解析含段头的 .readignore，返回三段 matcher。
// 段头前/无段头的裸 pattern 归 Read（向后兼容）。
// 未知段头（如 [write]）→ stderr 警告 + 该段 pattern 忽略（不进任何段）。
func ParseSections(content string) (*Sections, error) {
	s := &Sections{
		Read:   &Matcher{matcher: gitignore.NewMatcher(nil)},
		Edit:   &Matcher{matcher: gitignore.NewMatcher(nil)},
		Delete: &Matcher{matcher: gitignore.NewMatcher(nil)},
	}
	// 三桶累积 gitignore.Pattern + Raw
	type bucket struct {
		m           *Matcher
		gitPatterns []gitignore.Pattern
	}
	buckets := map[Op]*bucket{
		OpRead:   {m: s.Read},
		OpEdit:   {m: s.Edit},
		OpDelete: {m: s.Delete},
	}

	currentOp := OpRead // 默认 Read（无段头归 read）
	var discard bool    // 未知段头 → true，丢弃后续直到下一个已知段头

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// 段头检测
		if m := sectionHeaderRe.FindStringSubmatch(trimmed); m != nil {
			currentOp = Op(strings.ToLower(m[1]))
			discard = false // 已知段头，恢复收集
			continue
		}
		// 未知段头（整行 [xxx] 但不是 read/edit/delete）
		if unknownSectionRe.MatchString(trimmed) {
			fmt.Fprintf(os.Stderr, "readignore: warning: unknown section %s, ignored\n", trimmed)
			discard = true
			continue
		}
		if discard {
			continue // 未知段的 pattern 丢弃
		}
		// pattern 归当前段
		gp := gitignore.ParsePattern(trimmed, nil)
		b := buckets[currentOp]
		b.m.Patterns = append(b.m.Patterns, Pattern{Raw: trimmed, gitPattern: gp})
		b.gitPatterns = append(b.gitPatterns, gp)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	// 各段建 matcher
	for _, b := range buckets {
		b.m.matcher = gitignore.NewMatcher(b.gitPatterns)
	}
	return s, nil
}
