// Package inference M13/D-O13（方案 §6.16）：Agent 推理后端可插拔。
//
// 「谁来推理」与「在哪儿跑」（runtime 层，§6.3/§6.12）正交：本包只负责推理后端抽象与
// 外部 CLI 适配。eino-adk 为平台自研默认后端，由 chat 包内进程管线执行（完整能力）；
// claude-code / opencode / aider 为外部 CLI 后端，能力降级见 Capabilities 与 §6.16.4 表。
//
// 事件模型：后端通过 emit 回调吐 Event（与 chat.Event 同形），由 chat 层转译为 SSE（§7）。
package inference

import (
	"context"
	"sync"
	"time"
)

// DefaultBackend 平台自研默认推理后端名（空值等价）。
const DefaultBackend = "eino-adk"

// ProbeResult 探测结果（PATH + --version）。
type ProbeResult struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
	Reason    string `json:"reason,omitempty"` // 不可用原因 / 说明
}

// Capabilities 能力矩阵（§6.16.4 能力降级表）。
type Capabilities struct {
	Chat        bool   `json:"chat"`         // 单 Agent 对话
	Stream      bool   `json:"stream"`       // 流式输出
	SkillsMode  string `json:"skills_mode"`  // tools=原生工具 | instruction=注入为提示 | none=不支持
	MCPMode     string `json:"mcp_mode"`     // 同上（MCP 工具）
	AgentAsTool bool   `json:"agent_as_tool"` // 多 Agent 编排（AgentAsTool/transfer）
	Workflow    bool   `json:"workflow"`     // 工作流编排
	Resume      bool   `json:"resume"`       // 中断恢复（Cancel 之外）
}

// RunRequest 一次外部推理请求。Prompt 为 chat 层组装好的完整提示
// （指令 + 技能/MCP 降级注入 + 知识库上下文 + 历史 + 用户输入）。
type RunRequest struct {
	Prompt    string
	UserInput string            // 原始用户输入（事件展示用）
	Cwd       string            // 子进程工作目录（空 = 继承）
	Env       map[string]string // 额外环境变量
}

// Event 后端事件（与 chat.Event 同形；chat 层转译 SSE）。
type Event struct {
	Type string
	Data map[string]any
}

// Backend Agent 推理后端接口契约（§6.16.2）。
type Backend interface {
	Name() string
	Probe(ctx context.Context) ProbeResult
	Capabilities() Capabilities
	// Run 阻塞执行一次推理，增量内容通过 emit 流出；ctx 取消时后端应终止子进程。
	Run(ctx context.Context, req *RunRequest, emit func(Event)) error
}

// Status 对外展示的推理后端状态（GET /api/inference-backends）。
type Status struct {
	Name         string       `json:"name"`
	Available    bool         `json:"available"`
	Version      string       `json:"version,omitempty"`
	Path         string       `json:"path,omitempty"`
	Reason       string       `json:"reason,omitempty"`
	Default      bool         `json:"default"` // eino-adk
	Capabilities Capabilities `json:"capabilities"`
}

// Registry 推理后端注册表：内置 eino-adk + 外部 CLI 适配器，探测结果 10min TTL 缓存。
type Registry struct {
	mu       sync.Mutex
	backends map[string]Backend
	order    []string
	cache    map[string]cacheEntry
	ttl      time.Duration
}

type cacheEntry struct {
	res ProbeResult
	at  time.Time
}

// NewRegistry 构造注册表并登记默认与全部内置适配器。
func NewRegistry() *Registry {
	r := &Registry{
		backends: map[string]Backend{},
		cache:    map[string]cacheEntry{},
		ttl:      10 * time.Minute,
	}
	r.Register(newEinoADK())
	for _, b := range newCLIAdapters() {
		r.Register(b)
	}
	return r
}

// Register 登记后端（同名覆盖）。
func (r *Registry) Register(b Backend) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.backends[b.Name()]; !ok {
		r.order = append(r.order, b.Name())
	}
	r.backends[b.Name()] = b
}

// Get 按名取后端；默认名/未知名返回 nil（eino-adk 走平台内管线，不经本注册表执行）。
func (r *Registry) Get(name string) Backend {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "" || name == DefaultBackend {
		return nil
	}
	return r.backends[name]
}

// Known 名称合法性（用于 Agent 保存校验）：默认名或已注册后端。
func (r *Registry) Known(name string) bool {
	if name == "" || name == DefaultBackend {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.backends[name]
	return ok
}

// IsExternal 是否需要走外部后端分发（默认名返回 false，走平台内管线）。
func (r *Registry) IsExternal(name string) bool {
	return name != "" && name != DefaultBackend && r.Known(name)
}

// List 全部后端状态（probe 结果走缓存）；force=true 忽略缓存重新探测。
func (r *Registry) List(ctx context.Context, force bool) []Status {
	r.mu.Lock()
	names := append([]string(nil), r.order...)
	r.mu.Unlock()
	out := make([]Status, 0, len(names))
	for _, n := range names {
		b := r.backends[n] // order 只增不减，无需再持锁
		st := Status{Name: n, Default: n == DefaultBackend, Capabilities: b.Capabilities()}
		pr := r.probe(ctx, b, force)
		st.Available, st.Version, st.Path, st.Reason = pr.Available, pr.Version, pr.Path, pr.Reason
		out = append(out, st)
	}
	return out
}

// probe 带 TTL 缓存的探测。
func (r *Registry) probe(ctx context.Context, b Backend, force bool) ProbeResult {
	r.mu.Lock()
	if !force {
		if c, ok := r.cache[b.Name()]; ok && time.Since(c.at) < r.ttl {
			r.mu.Unlock()
			return c.res
		}
	}
	r.mu.Unlock()
	res := b.Probe(ctx)
	r.mu.Lock()
	r.cache[b.Name()] = cacheEntry{res: res, at: time.Now()}
	r.mu.Unlock()
	return res
}
