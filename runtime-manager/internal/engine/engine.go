// Package engine 运行时适配器统一接口（方案 04 §4.2）。
// P1 唯一引擎 oxigraph；fuseki(P2/O6)、memory_graph(P2/O7) 后续按同接口接入。
package engine

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

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

// ReasoningRuntime 支持推理开关的引擎（O6：fuseki；config.reasoning=true 时启用 RDFS/OWL 推理）。
type ReasoningRuntime interface {
	Runtime
	StartWithReasoning(ctx context.Context, profileID string, port int, ttls map[string]string, reasoning bool) (*Process, error)
}

// EngineStatus 引擎可执行文件自检结果（REQ-146）。
type EngineStatus struct {
	Engine           string   `json:"engine"`
	Registered       bool     `json:"registered"`                   // runtimed 启动时是否注册了适配器
	Installed        bool     `json:"installed"`                    // 本地是否探测到可执行文件
	Installing       bool     `json:"installing,omitempty"`         // 一键安装进行中
	LastInstallError string   `json:"last_install_error,omitempty"` // 最近一次一键安装失败原因
	Binary           string   `json:"binary,omitempty"`             // 命中的可执行文件路径
	Version          string   `json:"version,omitempty"`            // 引擎版本（best-effort）
	Searched         []string `json:"searched,omitempty"`           // 探测过的候选路径
	Hint             string   `json:"hint,omitempty"`               // 缺失时的安装指引
	Installable      bool     `json:"installable"`                  // 是否支持一键下载安装
}

// StatusProbe 引擎自检能力（适配器可选实现；未实现按未注册呈现）。
type StatusProbe interface {
	Probe() EngineStatus
}

// FindExecutable 按候选序找可执行文件（REQ-146 共享助手）：
// 含路径分隔符的候选做 Stat + 可执行位检查；裸名走 PATH 查找。返回命中的路径。
func FindExecutable(paths []string) (string, bool) {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if strings.ContainsRune(p, '/') || strings.ContainsRune(p, '\\') {
			if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
				return p, true
			}
			continue
		}
		if lp, err := exec.LookPath(p); err == nil {
			return lp, true
		}
	}
	return "", false
}
