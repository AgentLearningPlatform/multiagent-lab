// Package runtime 执行后端抽象（方案 §6.3，M10）。
// inprocess（P0 默认，进程内装配执行）与 docker（P1，每 Agent 一个 agentd 容器）
// 实现同一接口；k8s Pod 后端（P2）可按同接口扩展。
package runtime

import (
	"context"
	"strings"
)

// Endpoint 执行后端端点。
type Endpoint struct {
	URL   string // 沙箱 http endpoint（http://host:port）；inprocess 为空
	Local bool   // true=inprocess 内存句柄（进程内装配，直接走本地 Runner）
}

// BackendStatus 后端状态。
type BackendStatus struct {
	State  string `json:"state"`            // running | stopped | error
	Detail string `json:"detail,omitempty"` // 补充信息（容器 ID、错误原因等）
}

// StartSpec 启动规格（M10/10b：资源限制参数化；10c：RunID 支撑 per-run 作用域）。
type StartSpec struct {
	AgentID string
	RunID   string  // 10c：run 域实例标识（Scope()=run 时生效；空 = agent 域常驻实例）
	Memory  string  // 容器内存上限（如 512m）；空 = 默认 512m
	CPUs    float64 // CPU 核数上限；0 = 默认 1
}

// StopSpec 停止规格（10c：与 StartSpec 对应，run 域需按 (AgentID, RunID) 定位实例）。
type StopSpec struct {
	AgentID string
	RunID   string // 与 Start 对应；agent 域清理留空
}

// Backend Agent 执行后端接口（§6.3）。
type Backend interface {
	// Name 后端形态标识（docker|k8s）——chat 侧 run.started.backend 事件标注与
	// agent 配置形态对照提示用（2026-09-29 k8s 真机验证轮补充）。
	Name() string
	// Start 确保实例就绪并返回端点（沙箱后端负责生命周期与对账；agent 域已存在且健康则复用，
	// run 域每次 Start 建新实例——调用方在 Run 收尾负责 Stop 清理）。
	Start(ctx context.Context, spec StartSpec) (Endpoint, error)
	// Stop 停止并清理实例。
	Stop(ctx context.Context, spec StopSpec) error
	// Status 查询实例状态（agent 域口径：agent 常驻实例；per-run 实例不做面板跟踪）。
	Status(ctx context.Context, agentID string) (BackendStatus, error)
}

// Prober 可用性探测（REQ-190 自动检测）：AutoBackend 按候选序调用，秒级超时由调用方
// ctx 控制；未实现此接口的后端视为恒可用（显式构造即启用）。
type Prober interface {
	Available(ctx context.Context) bool
}

// Scope 沙箱实例作用域（M10 10c，SANDBOX_SCOPE）：agent（默认，每 Agent 一个常驻实例复用）
// | run（每次 Run 一个独立实例，Run 收尾即清——更强隔离，免去跨 Run 状态残留）。
// 非法/缺省一律回退 agent。
func Scope() string {
	switch strings.ToLower(envOf("SANDBOX_SCOPE")) {
	case "run":
		return "run"
	default:
		return "agent"
	}
}
