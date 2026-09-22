// Package manager 运行方案编排（方案 04 §4.3）：
// 生命周期（start/stop/reload）、形态分发（从构建平面拉取）、健康检查、日志查询、降级状态维护。
package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/engine"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/store"
)

type Manager struct {
	Store       *store.Store
	Engines     map[string]engine.Runtime // engine 名 → 适配器（oxigraph/fuseki；O6 起多引擎）
	BuildURL    string                    // 构建平面地址（拉取本体形态）
	LogDir      string
	HTTP        *http.Client
	HealthTries int // 启动健康检查重试次数

	mu    sync.Mutex
	procs map[string]*engine.Process // profileID → 运行句柄（进程内态，重启 Manager 后按 stopped 处理）
}

func New(st *store.Store, buildURL, logDir string) *Manager {
	_ = os.MkdirAll(logDir, 0o755)
	return &Manager{
		Store: st, Engines: map[string]engine.Runtime{}, BuildURL: strings.TrimRight(buildURL, "/"), LogDir: logDir,
		HTTP:        &http.Client{Timeout: 30 * time.Second},
		HealthTries: 20,
		procs:       map[string]*engine.Process{},
	}
}

// RegisterEngine 注册引擎适配器（main 启动时按环境探测注册）。
func (m *Manager) RegisterEngine(name string, eng engine.Runtime) {
	m.Engines[name] = eng
}

// engineFor 按 profile.engine 取适配器；未注册给出可自助的提示。
func (m *Manager) engineFor(name string) (engine.Runtime, error) {
	if eng, ok := m.Engines[name]; ok {
		return eng, nil
	}
	hint := "请检查 runtimed 启动配置（对应引擎二进制未就绪或未注册）"
	if name == "fuseki" {
		hint = "请下载 apache-jena-fuseki 并设置 FUSEKI_BIN 指向 fuseki-server 脚本（JDK 17+），重启 runtimed"
	}
	if name == "oxigraph" {
		hint = "请安装 oxigraph_server 并加入 PATH（或设置 OXIGRAPH_BIN），重启 runtimed"
	}
	return nil, fmt.Errorf("引擎 %q 未注册: %s", name, hint)
}

// FetchTTL 从构建平面拉取本体 TTL 形态（original turtle 直接回原文；自建经 spec→sidecar 导出）。
func (m *Manager) FetchTTL(ontologyID string) (string, error) {
	resp, err := m.HTTP.Get(fmt.Sprintf("%s/api/ontologies/%s/export?format=turtle", m.BuildURL, ontologyID))
	if err != nil {
		return "", fmt.Errorf("构建平面不可达: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return "", fmt.Errorf("拉取本体 %s 失败: %s %s", ontologyID, resp.Status, string(b))
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Start 启动方案：starting → 拉形态 → 引擎装载 → 健康检查 → running（失败进 error）。
func (m *Manager) Start(ctx context.Context, id string) error {
	p, err := m.Store.Get(id)
	if err != nil {
		return err
	}
	if p.Status == "running" {
		return fmt.Errorf("方案已在运行")
	}
	_ = m.Store.SetStatus(id, "starting", "", "")
	if len(p.OntologyIDs) == 0 {
		_ = m.Store.SetStatus(id, "error", "未配置本体集合", "")
		return fmt.Errorf("方案未配置本体集合")
	}
	ttls := map[string]string{}
	for _, oid := range p.OntologyIDs {
		ttl, err := m.FetchTTL(oid)
		if err != nil {
			_ = m.Store.SetStatus(id, "error", err.Error(), "")
			return err
		}
		ttls[oid] = ttl
	}
	port := p.Port
	if port == 0 {
		port = nextPort(m.Store)
		_ = m.Store.SetPort(id, port)
	}
	eng, err := m.engineFor(p.Engine)
	if err != nil {
		_ = m.Store.SetStatus(id, "error", err.Error(), "")
		return err
	}
	proc, err := m.startEngine(ctx, eng, p, port, ttls)
	if err != nil {
		_ = m.Store.SetStatus(id, "error", err.Error(), "")
		return err
	}
	m.mu.Lock()
	m.procs[id] = proc
	m.mu.Unlock()
	_ = m.Store.SetStatus(id, "starting", "", fmt.Sprint(proc.PID))

	// 健康检查直至就绪
	var lastErr error
	for i := 0; i < m.HealthTries; i++ {
		if ctx.Err() != nil {
			break
		}
		lastErr = eng.HealthCheck(ctx, proc.Endpoint)
		if lastErr == nil {
			_ = m.Store.SetStatus(id, "running", "", "")
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	proc.Stop()
	m.mu.Lock()
	delete(m.procs, id)
	m.mu.Unlock()
	_ = m.Store.SetStatus(id, "error", "健康检查失败: "+errStr(lastErr), "")
	return fmt.Errorf("方案启动后健康检查失败: %v", lastErr)
}

// Stop 停止方案。
func (m *Manager) Stop(id string) error {
	if _, err := m.Store.Get(id); err != nil {
		return err
	}
	m.mu.Lock()
	proc := m.procs[id]
	delete(m.procs, id)
	m.mu.Unlock()
	if proc != nil {
		_ = proc.Stop()
	}
	return m.Store.SetStatus(id, "stopped", "", "")
}

// Reload 显式重载（REQ-87）：重建数据目录并重新拉取最新版本形态。
func (m *Manager) Reload(ctx context.Context, id string) error {
	if err := m.Stop(id); err != nil {
		return err
	}
	return m.Start(ctx, id)
}

// Logs 返回最近 tail 行。
func (m *Manager) Logs(id string, tail int) ([]string, error) {
	if _, err := m.Store.Get(id); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(m.LogDir, id+".log"))
	if err != nil {
		return []string{}, nil // 无日志（从未启动）
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	return lines, nil
}

// ProcEndpoint 返回 running 方案的 SPARQL 端点（facade 路由用）。
func (m *Manager) ProcEndpoint(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p := m.procs[id]; p != nil {
		return p.Endpoint, nil
	}
	return "", fmt.Errorf("方案 %s 未在运行", id)
}

// startEngine 按引擎能力分发启动（实现 ReasoningRuntime 的引擎透传推理开关，O6）。
func (m *Manager) startEngine(ctx context.Context, eng engine.Runtime, p *store.Profile, port int, ttls map[string]string) (*engine.Process, error) {
	if rr, ok := eng.(engine.ReasoningRuntime); ok {
		return rr.StartWithReasoning(ctx, p.ID, port, ttls, profileReasoning(p.Config))
	}
	return eng.Start(ctx, p.ID, port, ttls)
}

// profileReasoning 从 profile config JSON 读 reasoning 开关（O6：fuseki 推理对照基座）。
func profileReasoning(cfgJSON string) bool {
	var cfg struct {
		Reasoning bool `json:"reasoning"`
	}
	if cfgJSON != "" {
		_ = json.Unmarshal([]byte(cfgJSON), &cfg)
	}
	return cfg.Reasoning
}

func nextPort(st *store.Store) int {
	list, _ := st.List()
	max := 9200
	for _, p := range list {
		if p.Port > max {
			max = p.Port
		}
	}
	return max + 1
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
