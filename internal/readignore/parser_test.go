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
