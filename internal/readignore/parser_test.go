package readignore

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const testContent = `# comment
.env
.env.*
!.env.example
*.pem
**/id_rsa
`

func TestParse_ReturnsMatcherWithoutError(t *testing.T) {
	p, err := Parse(testContent)
	assert.NoError(t, err)
	assert.NotNil(t, p)
}

func TestParse_EmptyContent(t *testing.T) {
	p, err := Parse("")
	assert.NoError(t, err)
	assert.NotNil(t, p)
	// 空解析器不匹配任何路径
	assert.False(t, p.Matches("anything"))
}

func TestMatches_GitignoreSemantics(t *testing.T) {
	p, err := Parse(testContent)
	assert.NoError(t, err)

	cases := []struct {
		path    string
		matched bool
	}{
		{".env", true},
		{".env.local", true},
		{".env.example", false}, // 取反放行
		{"secret.pem", true},
		{"sub/id_rsa", true},
		{"main.go", false},
		{"README.md", false},
		{"sub/dir/.env", true}, // 任意层级
	}
	for _, c := range cases {
		assert.Equal(t, c.matched, p.Matches(c.path), "path=%q", c.path)
	}
}

func TestMatches_WindowsBackslashPath(t *testing.T) {
	p, err := Parse(testContent)
	assert.NoError(t, err)

	// Windows 风格路径分隔符也应正常匹配
	assert.True(t, p.Matches(`sub\id_rsa`))
	assert.True(t, p.Matches(`sub\dir\.env`))
}

func TestParse_PreservesPatternsRaw(t *testing.T) {
	p, err := Parse(testContent)
	assert.NoError(t, err)

	// 取反行与模式行都应在 Patterns 中保留原始文本（适配器生成配置时要用）
	var raws []string
	for _, pat := range p.Patterns {
		raws = append(raws, pat.Raw)
	}
	assert.Contains(t, raws, ".env")
	assert.Contains(t, raws, ".env.*")
	assert.Contains(t, raws, "!.env.example")
	assert.Contains(t, raws, "*.pem")
	assert.Contains(t, raws, "**/id_rsa")
	// 注释和空行不构成 pattern
	assert.NotContains(t, raws, "# comment")
}

func TestParse_OnlyCommentsAndBlanks(t *testing.T) {
	p, err := Parse("# just a comment\n\n   \n# another\n")
	assert.NoError(t, err)
	assert.NotNil(t, p)
	assert.Empty(t, p.Patterns)
	assert.False(t, p.Matches("foo"))
}

// TestMatches_DirectoryTrailingSlash 锁定 go-git 对「目录尾斜杠」语义的真实行为：
//
//	模式 `build/`（以 `/` 结尾）仅匹配名为 build 的【目录】，不匹配同名普通文件。
//
// 实测（go-git v5.19.1）：
//   - Matches("build/")    == true  （目录路径命中目录模式）
//   - Matches("build")     == false （无尾斜杠视为普通文件，不命中目录模式）
//
// 该契约为阶段3/4 适配器（生成各 agent 原生防护配置）所依赖，特此钉住。
func TestMatches_DirectoryTrailingSlash(t *testing.T) {
	p, err := Parse("build/\n")
	assert.NoError(t, err)

	// 目录路径（带尾斜杠）命中目录模式 build/
	assert.True(t, p.Matches("build/"))
	// 普通文件路径（无尾斜杠）不命中目录模式 build/
	assert.False(t, p.Matches("build"))
	// 目录内子路径仍命中
	assert.True(t, p.Matches("build/foo"))
	assert.True(t, p.Matches("build/sub/"))
}

// TestMatches_GlobWildcards 覆盖 gitignore 通配符语义：? 单字符、[abc] 字符类、
// [!abc] 取反字符类、* 任意。这些是 .readignore 规则匹配的地基，之前完全没测。
func TestMatches_GlobWildcards(t *testing.T) {
	// ? 单字符通配
	p, err := Parse("secr?t\n")
	assert.NoError(t, err)
	assert.True(t, p.Matches("secret"))  // ? = e
	assert.False(t, p.Matches("secrt"))  // ? 要求恰好一个字符
	assert.False(t, p.Matches("secreet")) // ? 只要一个

	// [ab] 字符类
	p, err = Parse(".[ab]nv\n")
	assert.NoError(t, err)
	assert.True(t, p.Matches(".anv"))
	assert.True(t, p.Matches(".bnv"))
	assert.False(t, p.Matches(".cnv")) // c 不在 [ab]

	// [!ab] —— 钉住 go-git 实际行为：它把 [!ab] 当字面字符类（匹配 ! a b），
	// 不实现 Fnmatch 风格的 [!...] 取反语义。要"匹配非 a/b"得用多条 ! 取反规则。
	// 钉住这个行为，防未来误以为 [!ab] 能取反。
	p, err = Parse(".[!ab]nv\n")
	assert.NoError(t, err)
	assert.True(t, p.Matches(".!nv"))  // ! 在字面字符类 [!ab] 里
	assert.True(t, p.Matches(".anv"))  // a 在 [!ab] 里
	assert.False(t, p.Matches(".cnv")) // c 不在 [!ab]

	// * 任意字符序列
	p, err = Parse("*.pem\n")
	assert.NoError(t, err)
	assert.True(t, p.Matches("a.pem"))
	assert.True(t, p.Matches("secret.pem"))
	assert.False(t, p.Matches("a.key"))
}

// TestMatches_NegationChain 钉住「最后匹配规则胜出」的取反链语义。
// .env deny → !.env.prod 放行 → .env.prod.bak 重新 deny。
func TestMatches_NegationChain(t *testing.T) {
	p, err := Parse(".env\n!.env.prod\n.env.prod.bak\n")
	assert.NoError(t, err)
	assert.True(t, p.Matches(".env"))          // 第一条 .env 命中 → deny
	assert.False(t, p.Matches(".env.prod"))    // !.env.prod 取反覆盖 → 放行
	assert.True(t, p.Matches(".env.prod.bak")) // 第三条命中 → deny
	assert.False(t, p.Matches(".env.local"))   // 不匹配任何字面规则 → 放行
}

// TestParse_EdgeInputs 覆盖解析层的边界输入：CRLF 行尾（Windows）、大小写敏感性。
func TestParse_EdgeInputs(t *testing.T) {
	// CRLF 行尾（Windows checkout）：parser 的 TrimRight("\r\n") 应清理 \r，
	// 规则正常生效，不能因为 \r 把 *.pem 变成 *.pem\r 而失配。
	p, err := Parse("*.pem\r\n.env\r\n")
	assert.NoError(t, err)
	assert.True(t, p.Matches("secret.pem"), "CRLF 不应破坏 *.pem 匹配")
	assert.True(t, p.Matches(".env"), "CRLF 不应破坏 .env 匹配")

	// 大小写敏感（gitignore 默认）：.env ≠ .ENV
	p, err = Parse(".env\n")
	assert.NoError(t, err)
	assert.True(t, p.Matches(".env"))
	assert.False(t, p.Matches(".ENV"), ".ENV 不应命中 .env（大小写敏感）")
}
