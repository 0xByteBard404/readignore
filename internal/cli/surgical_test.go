package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

func TestRemoveSurgicalJSON(t *testing.T) {
	spec := adapter.SurgicalSpec{HookPath: "hooks.PreToolUse", Fingerprint: "readignore.sh"}

	// 一个「纯 readignore PreToolUse」的 settings.json（摘除后应为空 -> 删文件）。
	pureReadignore := `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Read|Grep|Glob|Bash|Edit|Write|NotebookEdit",
        "hooks": [
          {"type": "command", "command": "bash .claude/hooks/readignore.sh", "shell": "bash", "timeout": 5}
        ]
      }
    ]
  }
}`

	// 用户 permissions + 别的工具 hook + readignore hook（摘 readignore，保留其余）。
	mixed := `{
  "permissions": {"allow": ["Bash(ls:*)"], "deny": ["Bash(rm -rf:*)"]},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "/usr/local/bin/other.sh"}]},
      {"matcher": "Read|Grep|Glob|Bash", "hooks": [{"type": "command", "command": "bash .claude/hooks/readignore.sh", "shell": "bash", "timeout": 5}]}
    ]
  }
}`

	// matcher 块里同时有 readignore hook 和别的 hook（只删 readignore entry，块不空）。
	blockMixed := `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [
        {"type": "command", "command": "/usr/local/bin/other.sh"},
        {"type": "command", "command": "bash .claude/hooks/readignore.sh"}
      ]}
    ]
  }
}`

	tests := []struct {
		name       string
		input      string
		dryRun     bool
		wantAction removalAction
		wantExists bool   // 处理后文件是否存在
		wantRemain []byte // 期望剩余内容（用 JSONEq 做等价断言）；nil 不校验
		wantErr    bool
	}{
		{
			name: "纯 readignore hook -> 摘空删文件", input: pureReadignore,
			wantAction: actionRemoved, wantExists: false,
		},
		{
			name: "permissions + 别的 hook + readignore -> 只摘 readignore", input: mixed,
			wantAction: actionModified, wantExists: true,
			wantRemain: []byte(`{
  "permissions": {"allow": ["Bash(ls:*)"], "deny": ["Bash(rm -rf:*)"]},
  "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "/usr/local/bin/other.sh"}]}]}
}`),
		},
		{
			name: "同块混 hook -> 只删 readignore entry 块不空", input: blockMixed,
			wantAction: actionModified, wantExists: true,
			wantRemain: []byte(`{
  "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "/usr/local/bin/other.sh"}]}]}
}`),
		},
		{
			name: "无 readignore hook -> noChange", input: `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "/usr/local/bin/other.sh"}]}]}}`,
			wantAction: actionUnchanged, wantExists: true, wantRemain: nil,
		},
		{
			name: "无效 JSON -> 不动文件 + error", input: `{not valid json`,
			wantAction: actionUnchanged, wantExists: true, wantErr: true,
		},
		{
			name: "dry-run 纯 readignore -> 报告 removed 但不真删", input: pureReadignore, dryRun: true,
			wantAction: actionRemoved, wantExists: true, // dry-run 不真删，文件还在
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			require.NoError(t, os.WriteFile(path, []byte(tt.input), 0o644))

			buf := &bytes.Buffer{}
			action, err := removeSurgicalJSON(buf, path, "settings.json", spec, tt.dryRun)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantAction, action)

			_, statErr := os.Stat(path)
			gotExists := !os.IsNotExist(statErr)
			assert.Equal(t, tt.wantExists, gotExists, "文件存在性")
			if tt.wantRemain != nil {
				raw, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.JSONEq(t, string(tt.wantRemain), string(raw), "剩余内容（JSON 等价）")
			}
			if tt.dryRun {
				assert.Contains(t, buf.String(), "将")
			}
		})
	}
}

// 文件不存在 -> noChange，无 error（上层计 missing）。
func TestRemoveSurgicalJSON_MissingFile(t *testing.T) {
	spec := adapter.SurgicalSpec{HookPath: "hooks.PreToolUse", Fingerprint: "readignore.sh"}
	buf := &bytes.Buffer{}
	action, err := removeSurgicalJSON(buf, filepath.Join(t.TempDir(), "nope.json"), "nope.json", spec, false)
	require.NoError(t, err)
	assert.Equal(t, actionUnchanged, action)
}

func TestRemovePureProduct(t *testing.T) {
	// readignore 空规则下 Generate 的 opencode.json 形态（纯产物）。
	opencodePure := `{
  "$schema": "https://opencode.ai/config.json",
  "permission": {
    "read": {},
    "edit": {}
  }
}`
	opencodeWithRules := `{
  "$schema": "https://opencode.ai/config.json",
  "permission": {"read": {".env": "deny"}, "edit": {}}
}`
	// 用户在 readignore 产物基础上加了顶层 model 键 -> 非纯产物。
	opencodeWithUserTopKey := `{
  "$schema": "https://opencode.ai/config.json",
  "model": "gpt-4",
  "permission": {"read": {}, "edit": {}}
}`
	kiloPure := `{
  "permission": {
    "read": {},
    "edit": {}
  }
}`

	tests := []struct {
		name       string
		adapterID  string
		input      string
		expected   string // expectedContent；"" 表示只用结构判定
		dryRun     bool
		wantAction removalAction
		wantExists bool
		wantErr    bool
	}{
		{
			name: "opencode 纯产物 + expected 匹配 -> 整删", adapterID: "opencode",
			input: opencodePure, expected: opencodePure,
			wantAction: actionRemoved, wantExists: false,
		},
		{
			name: "opencode 含用户顶层键 -> 跳过", adapterID: "opencode",
			input: opencodeWithUserTopKey, expected: "",
			wantAction: actionUnchanged, wantExists: true,
		},
		{
			name: "opencode 结构纯但字节被改（用户追加 glob）+ expected 不匹配 -> 跳过", adapterID: "opencode",
			input: opencodeWithRules, expected: opencodePure, // 用户文件含 .env，expected 是空规则 -> 不等
			wantAction: actionUnchanged, wantExists: true,
		},
		{
			name: "opencode 结构纯、expected 为空（.readignore 不可读）-> 退回结构判定 -> 整删", adapterID: "opencode",
			input: opencodePure, expected: "",
			wantAction: actionRemoved, wantExists: false,
		},
		{
			name: "kilo 纯产物 -> 整删", adapterID: "kilocode",
			input: kiloPure, expected: "",
			wantAction: actionRemoved, wantExists: false,
		},
		{
			name: "无效 JSON -> 不动 + error", adapterID: "opencode",
			input: `{broken`, expected: "",
			wantAction: actionUnchanged, wantExists: true, wantErr: true,
		},
		{
			name: "dry-run 纯产物 -> 报告 removed 但不真删", adapterID: "opencode",
			input: opencodePure, expected: opencodePure, dryRun: true,
			wantAction: actionRemoved, wantExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "opencode.json")
			require.NoError(t, os.WriteFile(path, []byte(tt.input), 0o644))

			buf := &bytes.Buffer{}
			action, err := removePureProduct(buf, path, "opencode.json", tt.adapterID, tt.expected, tt.dryRun)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantAction, action)

			_, statErr := os.Stat(path)
			gotExists := !os.IsNotExist(statErr)
			assert.Equal(t, tt.wantExists, gotExists, "文件存在性")
		})
	}
}
