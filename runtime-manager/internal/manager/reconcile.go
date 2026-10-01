package manager

// ---------------------------------------------------------------------------
// REQ-236①/M63：启动对账——runtimed 重启后 procs 内存句柄丢失，DB running 态与真实
// 引擎双向漂移（孤儿引擎存活而句柄丢失 → facade ProcEndpoint miss；进程已死状态残留
// 僵尸 running）。启动时逐方案探测收敛：
//   docker 容器存活（DockerRunning，REQ-179 预留的对账函数就此接线）→ 领养重建句柄；
//   native 引擎端口可 ping（引擎 HealthCheck 同口径）→ 领养（Stop=kill DB pid）；
//   皆不可达 → 收敛 stopped（last_error 标注对账语义，不误标 error——重启不是方案故障）。
// 伴生线 EnsureHostPlan 三段式/单飞锁先例（backend companion/plan.go）的反哺落点。
// ---------------------------------------------------------------------------

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/engine"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/engine/oxigraph"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/store"
)

// Reconcile 启动对账（main 在路由装配前调用一次；阻塞但每方案探测有 2s 超时上限）。
func (m *Manager) Reconcile() {
	list, err := m.Store.List()
	if err != nil {
		log.Printf("[manager] 启动对账跳过（方案清单读取失败）: %v", err)
		return
	}
	for _, p := range list {
		if p.Status != "running" {
			continue
		}
		m.reconcileOne(p)
	}
}

func (m *Manager) reconcileOne(p *store.Profile) {
	// ① docker 容器对账（REQ-179 DockerRunning 接线）：容器存活 → 领养（Stop=docker rm -f）。
	if ep, ok := oxigraph.DockerRunning(p.ID, p.Port); ok {
		m.adopt(p, ep, func() error { return oxigraph.DockerStop(p.ID) }, "docker 容器")
		return
	}
	// ② native 孤儿进程对账：引擎健康探测（与启动健康检查同口径；port>0 排除从未分配端点的脏行）。
	if eng, ok := m.Engines[p.Engine]; ok && p.Port > 0 {
		ep := fmt.Sprintf("http://127.0.0.1:%d/query", p.Port)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := eng.HealthCheck(ctx, ep)
		cancel()
		if err == nil {
			m.adopt(p, ep, func() error { return killPID(p.PID) }, "native 进程")
			return
		}
	}
	// ③ 皆不可达 → 收敛 stopped（对账语义标注，非方案故障不进 error）。
	_ = m.Store.SetStatus(p.ID, "stopped", "启动对账：运行态失活，已收敛为 stopped", "")
	log.Printf("[manager] 启动对账：方案 %s（%s）运行态失活，收敛 stopped", p.ID, p.Name)
}

// adopt 领养存活引擎：重建进程句柄（facade ProcEndpoint / sparql 工作台 / Stop 全链恢复）。
func (m *Manager) adopt(p *store.Profile, endpoint string, stop func() error, kind string) {
	proc := engine.NewProcess(endpoint, 0, stop)
	m.mu.Lock()
	m.procs[p.ID] = proc
	m.mu.Unlock()
	log.Printf("[manager] 启动对账：方案 %s（%s）领养存活%s（endpoint=%s）", p.ID, p.Name, kind, endpoint)
}

// killPID 领养句柄的停止（native 孤儿进程按 DB pid kill；pid 缺失/平台差异时如实报错——
// 停止失败不炸 Stop 链，方案状态照常收敛）。
func killPID(pidStr string) error {
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return fmt.Errorf("对账领养的方案无有效 pid（%q），请手动停止引擎进程", pidStr)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
