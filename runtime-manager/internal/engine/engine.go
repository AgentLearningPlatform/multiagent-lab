// Package engine 运行时适配器统一接口（方案 04 §4.2）。
// P1 唯一引擎 oxigraph；fuseki(P2/O6)、memory_graph(P2/O7) 后续按同接口接入。
package engine

import "context"

// Process 引擎实例句柄（Manager 持有，供健康检查与停止）。
type Process struct {
	Endpoint string // SPARQL 查询端点（POST SPARQL）
	PID      int
	stop     func() error
}

// NewProcess 引擎适配器构造运行句柄。
func NewProcess(endpoint string, pid int, stop func() error) *Process {
	return &Process{Endpoint: endpoint, PID: pid, stop: stop}
}

// Stop 停止引擎子进程。
func (p *Process) Stop() error {
	if p.stop == nil {
		return nil
	}
	return p.stop()
}

// Runtime 引擎适配器接口：Prepare→Start→HealthCheck→Stop。
type Runtime interface {
	// Start 装载本体 TTL 集合并启动服务，返回运行句柄。
	Start(ctx context.Context, profileID string, port int, ttls map[string]string) (*Process, error)
	// HealthCheck 探测引擎可用性（SPARQL ping）。
	HealthCheck(ctx context.Context, endpoint string) error
}
