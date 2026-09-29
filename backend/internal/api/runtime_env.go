// 运行环境统一配置（REQ-191/M31）：DB(runtime_settings) 覆盖启动期 env 快照（env 兜底），
// 按合并配置动态构造沙箱后端（配置变更即重建——auto 探测粘滞缓存随之重置），连接测试。
// 本体引擎执行方式不在本文件——runtime_config 仍由 runtime-manager 承载（REQ-179/M-O16），
// 设置页「运行环境」分区经既有反代读写（存储与 API 不变）。
package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/runtime"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// RuntimeEnv 运行环境配置解析器：合并视图 + 沙箱后端动态装配（hash 缓存，稳态零重建）。
type RuntimeEnv struct {
	Store      *store.Store
	TokenIssue func(agentID string) (string, error) // 一次性 manifest token 签发（透传沙箱后端）
	Defaults   store.RuntimeSettings                // 启动期 env 快照（main.go 装配时读取，含 env 语义默认值）

	mu   sync.Mutex
	hash string
	cur  runtime.Backend
}

// Effective 合并视图：DB 配置覆盖 env 兜底快照。
func (e *RuntimeEnv) Effective() store.RuntimeSettings {
	db, err := e.Store.GetRuntimeSettings()
	if err != nil || db == nil {
		db = &store.RuntimeSettings{}
	}
	return db.MergeOver(e.Defaults)
}

// sandboxEnabled 生效形态是否启用沙箱（inprocess/未配置镜像 = 未启用，与 env 语义一致：
// SANDBOX_IMAGE 空 = 平台不装配沙箱）。
func sandboxEnabled(cfg store.RuntimeSettings) bool {
	mode := cfg.SandboxMode
	if mode == "" {
		mode = "docker" // env 语义：SANDBOX_BACKEND 缺省 docker（SANDBOX_IMAGE 空则整体未启用）
	}
	if mode == "inprocess" || cfg.SandboxImage == "" {
		return false
	}
	return true
}

// Build 按合并配置构造沙箱后端（未启用返回 nil；每次配置变更重建，粘滞探测态自然重置）。
func (e *RuntimeEnv) Build(cfg store.RuntimeSettings) runtime.Backend {
	if !sandboxEnabled(cfg) {
		return nil
	}
	mode := cfg.SandboxMode
	if mode == "" {
		mode = "docker"
	}
	inCluster := cfg.PlatformURLInCluster
	if inCluster == "" {
		inCluster = cfg.PlatformURLExternal
	}
	k8s := &runtime.K8sBackend{
		Image:        cfg.SandboxImage,
		Bin:          cfg.KubectlBin,
		Kubeconfig:   cfg.K8sKubeconfig,
		Namespace:    cfg.K8sNamespace,
		Context:      cfg.K8sContext,
		EndpointMode: cfg.K8sEndpointMode,
		PlatformURL:  inCluster,
		Scope:        cfg.SandboxScope,
		TokenIssue:   e.TokenIssue,
	}
	switch mode {
	case "k8s":
		return k8s
	case "auto":
		// REQ-190：k8s pod 优先 → docker 次之 → 均不可用进程内兜底
		return &runtime.AutoBackend{Candidates: []runtime.Backend{k8s, &runtime.DockerBackend{
			Image:       cfg.SandboxImage,
			Bin:         cfg.DockerBin,
			PlatformURL: cfg.PlatformURLExternal,
			Scope:       cfg.SandboxScope,
			TokenIssue:  e.TokenIssue,
		}}}
	default: // docker（env SANDBOX_BACKEND 缺省同语义）
		return &runtime.DockerBackend{
			Image:       cfg.SandboxImage,
			Bin:         cfg.DockerBin,
			PlatformURL: cfg.PlatformURLExternal,
			Scope:       cfg.SandboxScope,
			TokenIssue:  e.TokenIssue,
		}
	}
}

// Current 当前沙箱后端（配置指纹缓存：未变化复用实例——AutoBackend 粘滞态保留）。
func (e *RuntimeEnv) Current() runtime.Backend {
	cfg := e.Effective()
	h := cfg.Hash()
	e.mu.Lock()
	defer e.mu.Unlock()
	if h == e.hash {
		return e.cur
	}
	b := e.Build(cfg)
	e.hash, e.cur = h, b
	return b
}

// DynamicRuntime chat.Service.Runtime 的动态形态（REQ-191）：每次调用经 RuntimeEnv 按
// 当前配置解析后端；inprocess/未启用形态 Name()="inprocess"——chat 分发据此视为「未启用
// 沙箱」回退进程内（与静态 nil 装配同语义），存量静态装配 Name 恒非 inprocess 零回归。
type DynamicRuntime struct {
	Env *RuntimeEnv
}

func (d *DynamicRuntime) current() runtime.Backend { return d.Env.Current() }

func (d *DynamicRuntime) Name() string {
	if b := d.current(); b != nil {
		return b.Name()
	}
	return "inprocess"
}

func (d *DynamicRuntime) Start(ctx context.Context, spec runtime.StartSpec) (runtime.Endpoint, error) {
	b := d.current()
	if b == nil {
		return runtime.Endpoint{}, runtime.ErrSandboxDisabled
	}
	return b.Start(ctx, spec)
}

func (d *DynamicRuntime) Stop(ctx context.Context, spec runtime.StopSpec) error {
	b := d.current()
	if b == nil {
		return nil // 未启用即无实例，幂等成功
	}
	return b.Stop(ctx, spec)
}

func (d *DynamicRuntime) Status(ctx context.Context, agentID string) (runtime.BackendStatus, error) {
	b := d.current()
	if b == nil {
		return runtime.BackendStatus{State: "stopped", Detail: "运行环境配置为进程内嵌（无沙箱实例）"}, nil
	}
	return b.Status(ctx, agentID)
}

// Available 探测当前后端可用性（chat 分发预检；未启用= false → 回退进程内并告警）。
func (d *DynamicRuntime) Available(ctx context.Context) bool {
	b := d.current()
	if b == nil {
		return false
	}
	if p, ok := b.(runtime.Prober); ok {
		return p.Available(ctx)
	}
	return true
}

// ---- REST handlers（设置页「运行环境」分区） ----

// runtimeEnvGet GET /api/runtime-env：{settings(DB 原值，空=跟随环境), effective(合并生效值), defaults(env 快照), sandbox_enabled}。
func (s *Server) runtimeEnvGet(w http.ResponseWriter, r *http.Request) {
	if s.RuntimeEnv == nil {
		writeErr(w, errRuntimeEnvNotWired)
		return
	}
	db, err := s.Store.GetRuntimeSettings()
	if err != nil {
		writeErr(w, err)
		return
	}
	eff := s.RuntimeEnv.Effective()
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":        db,
		"effective":       eff,
		"defaults":        s.RuntimeEnv.Defaults,
		"sandbox_enabled": sandboxEnabled(eff),
	})
}

var errRuntimeEnvNotWired = errStr("运行环境配置未装配（进程内嵌固定形态）")

func errStr(msg string) error { return &plainError{msg} }

type plainError struct{ msg string }

func (e *plainError) Error() string { return e.msg }

var runtimeModeEnum = map[string]bool{"": true, "inprocess": true, "docker": true, "k8s": true, "auto": true}
var runtimeScopeEnum = map[string]bool{"": true, "agent": true, "run": true}
var runtimeEndpointEnum = map[string]bool{"": true, "port-forward": true, "pod-ip": true}

// runtimeEnvPut PUT /api/runtime-env：全量保存（前端表单整单提交；枚举字段白名单校验）。
func (s *Server) runtimeEnvPut(w http.ResponseWriter, r *http.Request) {
	if s.RuntimeEnv == nil {
		writeErr(w, errRuntimeEnvNotWired)
		return
	}
	var in store.RuntimeSettings
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	in.SandboxMode = strings.TrimSpace(in.SandboxMode)
	if !runtimeModeEnum[in.SandboxMode] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sandbox_mode 取值须为 inprocess|docker|k8s|auto 或空（跟随启动环境）"})
		return
	}
	if !runtimeScopeEnum[strings.TrimSpace(in.SandboxScope)] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sandbox_scope 取值须为 agent|run 或空"})
		return
	}
	if !runtimeEndpointEnum[strings.TrimSpace(in.K8sEndpointMode)] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "k8s_endpoint_mode 取值须为 port-forward|pod-ip 或空"})
		return
	}
	in.UpdatedAt = ""
	if err := s.Store.SaveRuntimeSettings(&in); err != nil {
		writeErr(w, err)
		return
	}
	db, err := s.Store.GetRuntimeSettings()
	if err != nil {
		writeErr(w, err)
		return
	}
	eff := s.RuntimeEnv.Effective()
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":        db,
		"effective":       eff,
		"defaults":        s.RuntimeEnv.Defaults, // 与 GET 同构（前端保存后 setPayload 直用）
		"sandbox_enabled": sandboxEnabled(eff),
	})
}

// runtimeEnvTest POST /api/runtime-env/test {target:"docker"|"k8s"}：按当前合并配置构造
// 对应后端探测可用性（临时实例不复用缓存——探测不污染 auto 粘滞态）。诚实边界：探测仅
// 验证 CLI 与守护进程/集群 API 可达，镜像可拉取性等启动期问题在 Start 时如实报错。
func (s *Server) runtimeEnvTest(w http.ResponseWriter, r *http.Request) {
	if s.RuntimeEnv == nil {
		writeErr(w, errRuntimeEnvNotWired)
		return
	}
	var in struct {
		Target string `json:"target"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	cfg := s.RuntimeEnv.Effective()
	var b runtime.Backend
	switch strings.TrimSpace(in.Target) {
	case "docker":
		b = &runtime.DockerBackend{Bin: cfg.DockerBin, PlatformURL: cfg.PlatformURLExternal, Scope: cfg.SandboxScope, TokenIssue: s.RuntimeEnv.TokenIssue}
	case "k8s":
		b = &runtime.K8sBackend{Bin: cfg.KubectlBin, Kubeconfig: cfg.K8sKubeconfig, Context: cfg.K8sContext, Namespace: cfg.K8sNamespace, EndpointMode: cfg.K8sEndpointMode, PlatformURL: cfg.PlatformURLInCluster, Scope: cfg.SandboxScope, TokenIssue: s.RuntimeEnv.TokenIssue}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target 取值须为 docker|k8s"})
		return
	}
	pctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	ok := true
	detail := "可达"
	if p, isP := b.(runtime.Prober); isP {
		ok = p.Available(pctx)
		if !ok {
			detail = "不可达（检查 CLI 安装、守护进程/集群连接与认证配置）"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"target": in.Target, "ok": ok, "detail": detail})
}
