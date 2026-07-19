package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/0xByteBard404/readignore/internal/adapter"
)

// removalAction 描述单次摘除/删除动作对目标文件的影响，供 removeGeneratedFiles
// 外层决定计数器与「是否清空父目录」。
type removalAction int

const (
	// actionRemoved：文件被整删（摘空、纯产物、或独占文件整删）。
	// 外层据此清理空的父目录。
	actionRemoved removalAction = iota
	// actionModified：文件保留但内容已改（摘除 readignore 段后写回）。
	actionModified
	// actionUnchanged：文件未改动（无 readignore hook / 非纯产物跳过 / missing）。
	actionUnchanged
)

// removeSurgicalJSON 从共享 JSON 文件 absPath 里摘除 readignore 注入的 hook 注册，
// 保留其余内容。displayPath 仅用于日志（通常 = 相对仓库根路径）。
//
// 算法：解析 JSON -> 沿 spec.HookPath 找到 matcher 块数组 -> 删 hooks[] 里
// command 含 spec.Fingerprint 的 entry -> 删空 matcher 块 -> 删空的事件键/hooks 键
// -> root 空则整删文件，否则 MarshalIndent 写回。
//
// 错误降级（铁律）：文件不存在 -> noChange 无错；解析失败 -> 不动文件 + error。
func removeSurgicalJSON(out io.Writer, absPath, displayPath string, spec adapter.SurgicalSpec, dryRun bool) (removalAction, error) {
	raw, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return actionUnchanged, nil
		}
		fmt.Fprintf(out, "  失败 %s：%v\n", displayPath, err)
		return actionUnchanged, err
	}

	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		fmt.Fprintf(out, "  跳过 %s：不是合法 JSON，无法自动摘除，请手动移除 readignore 段\n", displayPath)
		return actionUnchanged, err
	}

	removed := removeHookEntries(root, spec.HookPath, spec.Fingerprint)
	if removed == 0 {
		// 无 readignore hook：不动。
		return actionUnchanged, nil
	}
	pruneEmptyHookContainers(root, spec.HookPath)

	if len(root) == 0 {
		// 摘空 -> 整删文件。
		if dryRun {
			fmt.Fprintf(out, "  将删除 %s（摘除 readignore 段后为空）\n", displayPath)
			return actionRemoved, nil
		}
		if err := os.Remove(absPath); err != nil {
			fmt.Fprintf(out, "  失败 %s：%v\n", displayPath, err)
			return actionUnchanged, err
		}
		fmt.Fprintf(out, "  已删除 %s（摘除 readignore 段后为空）\n", displayPath)
		return actionRemoved, nil
	}

	buf, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		fmt.Fprintf(out, "  失败 %s：%v\n", displayPath, err)
		return actionUnchanged, err
	}
	if !dryRun {
		if err := os.WriteFile(absPath, append(buf, '\n'), 0o644); err != nil {
			fmt.Fprintf(out, "  失败 %s：%v\n", displayPath, err)
			return actionUnchanged, err
		}
	}
	verb := "已摘除"
	if dryRun {
		verb = "将摘除"
	}
	fmt.Fprintf(out, "  %s %s 的 readignore 段\n", verb, displayPath)
	return actionModified, nil
}

// removeHookEntries 沿 hookPath（如 "hooks.PreToolUse"）定位 matcher 块数组，
// 从每个块的 hooks[] 删 command 含 fingerprint 的 entry；块空了删整块。
// 返回删除的 entry 数。原地修改 root。
func removeHookEntries(root map[string]any, hookPath, fingerprint string) int {
	parts := strings.Split(hookPath, ".")
	if len(parts) != 2 {
		return 0
	}
	hooksObj, ok := root[parts[0]].(map[string]any)
	if !ok {
		return 0
	}
	arr, ok := hooksObj[parts[1]].([]any)
	if !ok {
		return 0
	}

	removed := 0
	kept := make([]any, 0, len(arr))
	for _, m := range arr {
		block, ok := m.(map[string]any)
		if !ok {
			kept = append(kept, m)
			continue
		}
		hooksArr, ok := block["hooks"].([]any)
		if !ok {
			kept = append(kept, block)
			continue
		}
		newHooks := make([]any, 0, len(hooksArr))
		blockRemoved := 0
		for _, h := range hooksArr {
			entry, ok := h.(map[string]any)
			if !ok {
				newHooks = append(newHooks, h)
				continue
			}
			cmd, _ := entry["command"].(string)
			if strings.Contains(cmd, fingerprint) {
				blockRemoved++
				continue
			}
			newHooks = append(newHooks, h)
		}
		removed += blockRemoved
		if blockRemoved > 0 && len(newHooks) == 0 {
			// 块内 hook 全是 readignore 且被删空 -> 不保留该 matcher 块。
			continue
		}
		if blockRemoved > 0 {
			block["hooks"] = newHooks
		}
		kept = append(kept, block)
	}

	if removed > 0 {
		if len(kept) == 0 {
			delete(hooksObj, parts[1])
		} else {
			hooksObj[parts[1]] = kept
		}
	}
	return removed
}

// pruneEmptyHookContainers 在摘除后清理空的事件容器：hookPath 顶层对象
// （如 hooks）若已无任何事件键，从 root 删除。
func pruneEmptyHookContainers(root map[string]any, hookPath string) {
	parts := strings.Split(hookPath, ".")
	if len(parts) != 2 {
		return
	}
	hooksObj, ok := root[parts[0]].(map[string]any)
	if !ok {
		return
	}
	if len(hooksObj) == 0 {
		delete(root, parts[0])
	}
}
