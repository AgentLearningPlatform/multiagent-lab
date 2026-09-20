// Package runtime 执行后端抽象（方案 §6.3，M10）。
// inprocess（P0 默认，进程内装配执行）与 docker（P1，每 Agent 一个 agentd 容器）
// 实现同一接口；k8s Pod 后端（P2）可按同接口扩展。
package runtime

import "context"

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

// Backend Agent 执行后端接口（§6.3）。
type Backend interface {
	// Start 确保 agentID 对应的运行实例就绪并返回端点。
	// 沙箱后端负责容器/实例生命周期与对账（已存在且健康则复用）。
	Start(ctx context.Context, agentID string) (Endpoint, error)
	// Stop 停止并清理实例。
	Stop(ctx context.Context, agentID string) error
	// Status 查询实例状态。
	Status(ctx context.Context, agentID string) (BackendStatus, error)
}
