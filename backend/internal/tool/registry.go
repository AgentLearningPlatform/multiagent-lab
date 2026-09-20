// Package tool 工具注册表（方案 §6.8）：内置工具统一登记，按 Agent 勾选装配。
// 动态来源（v0.6）：本体 facade onto_*（§6.10）、Agent 级 MCP servers（§6.11，P2）、
// 技能白名单（§6.12）在装配期以 Entry 或直接 Eino BaseTool 形式合并，本包提供统一入口。
package tool

import (
	"context"
	"fmt"
	"sort"
	"sync"

	einotool "github.com/cloudwego/eino/components/tool"
)

// SourceKind 工具来源类型（SSE tool.call 事件 source 字段，方案 §6.5）。
type SourceKind string

const (
	SourceBuiltin  SourceKind = "builtin"
	SourceSkill    SourceKind = "skill"
	SourceMCP      SourceKind = "mcp"
	SourceOntology SourceKind = "ontology"
)

// Entry 注册表条目。
type Entry struct {
	ID          string     // 注册表 id（agent.tools / skill.tools 勾选存这个）
	Name        string     // 模型可见 function name（默认同 ID）
	Description string     // 管理列表展示 + 模型可见
	Source      SourceKind // 默认来源标注（skill/mcp 动态来源在装配期覆盖）
	New         func(ctx context.Context) (einotool.BaseTool, error)
}

// Registry 内存注册表。内置工具启动时注册；动态来源按会话装配临时并入。
type Registry struct {
	mu    sync.RWMutex
	items map[string]*Entry
}

// NewRegistry 构造空注册表。
func NewRegistry() *Registry {
	return &Registry{items: map[string]*Entry{}}
}

// Register 登记条目（同名覆盖）。
func (r *Registry) Register(e *Entry) error {
	if e == nil || e.ID == "" || e.New == nil {
		return fmt.Errorf("tool entry invalid: id and New required")
	}
	if e.Name == "" {
		e.Name = e.ID
	}
	if e.Source == "" {
		e.Source = SourceBuiltin
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[e.ID] = e
	return nil
}

// Get 按 ID 查询。
func (r *Registry) Get(id string) (*Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.items[id]
	return e, ok
}

// List 返回全部条目（按 ID 排序，管理列表用）。
func (r *Registry) List() []*Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Entry, 0, len(r.items))
	for _, e := range r.items {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ComposeResult 装配产物：实例化工具 + 名称来源映射 + 告警。
type ComposeResult struct {
	Tools    []einotool.BaseTool
	SourceOf map[string]string // function name -> source（builtin / skill:{id} / mcp:{server} / ontology:{profile_id}）
	Warnings []string          // 装配期告警（未知工具 id 等，发装配期事件用）
}

// Compose 按 ID 列表实例化工具。未知 ID 跳过并记告警（注册表校验存在性，§6.12）。
func (r *Registry) Compose(ctx context.Context, ids []string) (*ComposeResult, error) {
	res := &ComposeResult{SourceOf: map[string]string{}}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		e, ok := r.Get(id)
		if !ok {
			res.Warnings = append(res.Warnings, fmt.Sprintf("未知工具 %q，已跳过", id))
			continue
		}
		bt, err := e.New(ctx)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("工具 %q 实例化失败: %v", id, err))
			continue
		}
		res.Tools = append(res.Tools, bt)
		res.SourceOf[e.Name] = string(e.Source)
	}
	return res, nil
}
