package kilocode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

func TestAdapter_ID_Strength(t *testing.T) {
	a := Adapter{}
	assert.Equal(t, "kilocode", a.ID())
	assert.Equal(t, adapter.StrengthConfig, a.Strength())
}

func TestAdapter_RegisteredInRegistry(t *testing.T) {
	a, ok := adapter.Get("kilocode")
	require.True(t, ok, "kilocode adapter must self-register")
	assert.Equal(t, adapter.StrengthConfig, a.Strength())
}

func TestAdapter_Detect(t *testing.T) {
	a := Adapter{}
	t.Run("kilo.json", func(t *testing.T) {
		tmp := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmp, "kilo.json"), []byte("{}"), 0o644))
		assert.True(t, a.Detect(tmp))
	})
	t.Run("kilo.jsonc", func(t *testing.T) {
		tmp := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmp, "kilo.jsonc"), []byte("{}"), 0o644))
		assert.True(t, a.Detect(tmp))
	})
	t.Run(".kilo dir", func(t *testing.T) {
		tmp := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(tmp, ".kilo"), 0o755))
		assert.True(t, a.Detect(tmp))
	})
	t.Run(".kilocode dir", func(t *testing.T) {
		tmp := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(tmp, ".kilocode"), 0o755))
		assert.True(t, a.Detect(tmp))
	})
	t.Run("absent", func(t *testing.T) {
		assert.False(t, a.Detect(t.TempDir()))
	})
}

func TestAdapter_Generate(t *testing.T) {
	a := Adapter{}
	plan := adapter.Plan{
		RawPatterns: []string{
			"# comment",
			".env",
			".env.*",
			"!.env.example",
			"*.pem",
			"**/id_rsa",
			".aws/",
		},
	}
	files, err := a.Generate(plan)
	require.NoError(t, err)
	require.Len(t, files, 1)

	f := files[0]
	assert.Equal(t, "kilo.json", f.Path)
	assert.Equal(t, uint32(0), f.Mode)

	assert.Contains(t, f.Content, `"permission"`)
	assert.Contains(t, f.Content, `"read"`)
	assert.Contains(t, f.Content, `".env": "deny"`)
	assert.Contains(t, f.Content, `".env.*": "deny"`)
	assert.Contains(t, f.Content, `".env.example": "allow"`)
	assert.Contains(t, f.Content, `"*.pem": "deny"`)
	// **/id_rsa 降级为 basename id_rsa
	assert.Contains(t, f.Content, `"id_rsa": "deny"`)
	assert.NotContains(t, f.Content, `**`)
	// 注释行不应出现
	assert.NotContains(t, f.Content, `"# comment"`)
}

func TestAdapter_InstallInstructions_NonEmpty(t *testing.T) {
	a := Adapter{}
	s := a.InstallInstructions()
	assert.NotEmpty(t, s)
	assert.Contains(t, s, "config")
	assert.Contains(t, s, "#8293")
}

func TestStripStarStar(t *testing.T) {
	assert.Equal(t, "id_rsa", stripStarStar("**/id_rsa"))
	assert.Equal(t, ".env", stripStarStar(".env"))
	assert.Equal(t, "secrets/", stripStarStar("secrets/"))
	assert.Equal(t, "*.pem", stripStarStar("*.pem"))
}

func TestStripNegation(t *testing.T) {
	actual, ok := stripNegation("!.env.example")
	assert.True(t, ok)
	assert.Equal(t, ".env.example", actual)

	_, ok = stripNegation(".env")
	assert.False(t, ok)

	_, ok = stripNegation("!")
	assert.False(t, ok)
}
