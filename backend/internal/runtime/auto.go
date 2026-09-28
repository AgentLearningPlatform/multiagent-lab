package runtime

import (
	"context"
	"errors"
	"sync"
	"time"
)

// AutoBackend 自动检测沙箱后端（REQ-190）：按候选顺序探测（k8s pod 优先 → docker 次之），
// 可用即用、失败顺延；均不可用时 Available()=false，chat 侧回退进程内执行（进程内兜底，
// 发 run.warning 诚实提示）。探测结果粘滞缓存（lastGood）：后续 Start/Available 先探上次
// 成功的后端，失败再按序全量探测——环境变化（集群/守护进程停止）自动换档，稳态零重复探测。
type AutoBackend struct {
	Candidates []Backend     // 优先级序（index 0 最优先）
	ProbeWait  time.Duration // 单候选探测超时（默认 3s）

	mu       sync.Mutex
	lastGood string // 粘滞：上次成功后端的 Name()
}

// ErrNoSandbox 全部候选不可用（chat 侧显式沙箱 agent 收到此错误如实报 run.error；
// auto agent 在分发前经 Available 预检回退进程内，正常不会走到）。
var ErrNoSandbox = errors.New("auto: no sandbox backend available")

func (a *AutoBackend) Name() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lastGood != "" {
		return a.lastGood
	}
	return "auto"
}

func (a *AutoBackend) probeWait() time.Duration {
	if a.ProbeWait > 0 {
		return a.ProbeWait
	}
	return 3 * time.Second
}

// pick 返回当前应使用的后端（粘滞优先，失效按序换档）；均不可用返回 nil。
func (a *AutoBackend) pick(ctx context.Context) Backend {
	wait := a.probeWait()
	a.mu.Lock()
	sticky := a.lastGood
	a.mu.Unlock()
	if sticky != "" {
		for _, b := range a.Candidates {
			if b.Name() == sticky {
				if probeAvailable(ctx, b, wait) {
					return b
				}
				break // 粘滞档失效，转全量按序探测
			}
		}
	}
	for _, b := range a.Candidates {
		if probeAvailable(ctx, b, wait) {
			a.mu.Lock()
			a.lastGood = b.Name()
			a.mu.Unlock()
			return b
		}
	}
	return nil
}

func probeAvailable(ctx context.Context, b Backend, wait time.Duration) bool {
	p, ok := b.(Prober)
	if !ok {
		return true
	}
	pctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	return p.Available(pctx)
}

// Available 自动检测候选中是否有可用沙箱（REQ-190 chat 分发预检；带粘滞缓存）。
func (a *AutoBackend) Available(ctx context.Context) bool {
	return a.pick(ctx) != nil
}

// Start 委派当前探测可用的候选（探测已过滤主要不可用场景；Start 级失败如实返回——
// 此时已选定形态，静默换档会留下半创建实例，诚实报错优于暗改）。
func (a *AutoBackend) Start(ctx context.Context, spec StartSpec) (Endpoint, error) {
	b := a.pick(ctx)
	if b == nil {
		return Endpoint{}, ErrNoSandbox
	}
	return b.Start(ctx, spec)
}

// Stop 对全部候选执行（幂等：未使用候选删不到任何实例；进程重启后无法记忆
// 实例归属，双删保证 run 域 per-run 实例不残留）。
func (a *AutoBackend) Stop(ctx context.Context, spec StopSpec) error {
	var firstErr error
	for _, b := range a.Candidates {
		if err := b.Stop(ctx, spec); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Status 报告当前应使用后端的实例状态（detail 前缀实际形态名，面板可读）。
func (a *AutoBackend) Status(ctx context.Context, agentID string) (BackendStatus, error) {
	b := a.pick(ctx)
	if b == nil {
		return BackendStatus{State: "stopped", Detail: "auto: 无可用沙箱（k8s/docker 均不可达）"}, nil
	}
	st, err := b.Status(ctx, agentID)
	st.Detail = b.Name() + ": " + st.Detail
	return st, err
}
