package adapter

import "sync"

// adapterRegistry 是适配器的全局注册表（进程内单例）。
// 用 map 做 ID 索引，用独立切片 order 记录注册顺序，使 All() 输出稳定、可预期。
type adapterRegistry struct {
	mu    sync.RWMutex
	items map[string]Adapter
	order []string
}

// registry 是包级单例。具体适配器在 init() 中调用 [Register] 自登记到此处。
var registry = &adapterRegistry{items: make(map[string]Adapter)}

// Register 登记一个适配器。各具体适配器（claudecode / opencode / ……）应在
// 自身 init() 中调用本函数完成自登记。
//
// 行为：
//   - 同 ID 二次注册时后者覆盖前者（便于测试与覆盖式加载）；
//   - 注册顺序被保留，[All] 按此顺序返回，CLI 列表输出因此稳定；
//   - a 为 nil 或 a.ID()=="" 时跳过（防御性：ID 是 CLI 参数/registry 索引，
//     空串既是无效键也模糊"未指定"，登记它只会污染注册表；选静默跳过 + 注释
//     而非 panic，避免某适配器误实现直接拖垮整个进程启动）。
//
// 实现约束：各适配器的 ID() 应返回全小写、无空格、跨版本不变的稳定短标识；
// 调用 Register 前应自检 ID() 非空。
func Register(a Adapter) {
	if a == nil {
		return
	}
	id := a.ID()
	if id == "" {
		// 空 ID 不登记：它是 registry 的无效键（Get("") 永远 false），
		// 也无法作为 CLI 参数。静默跳过比 panic 更稳妥（init() 不应拖垮进程）。
		return
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.items[id]; !exists {
		registry.order = append(registry.order, id)
	}
	registry.items[id] = a
}

// All 返回所有已注册适配器，顺序遵循注册先后。
// 返回的是新切片，调用方可安全修改而不影响 registry 内部状态。
func All() []Adapter {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	out := make([]Adapter, 0, len(registry.order))
	for _, id := range registry.order {
		if a, ok := registry.items[id]; ok {
			out = append(out, a)
		}
	}
	return out
}

// Get 按 ID 查找适配器。找到返回 (adapter, true)，否则返回 (nil, false)。
func Get(id string) (Adapter, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	a, ok := registry.items[id]
	return a, ok
}
