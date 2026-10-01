// Package manager 运行方案编排（方案 04 §4.3）：
// 生命周期（start/stop/reload）、形态分发（从构建平面拉取）、健康检查、日志查询、降级状态维护。
package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/engine"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/engine/oxigraph"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/store"
)

type Manager struct {
	Store         *store.Store
	Engines       map[string]engine.Runtime // engine 名 → 适配器（oxigraph/fuseki；O6 起多引擎）
	BuildURL      string                    // 构建平面地址（拉取本体形态）
	LogDir        string
	InstallBinDir string // 一键安装落点目录（REQ-146，默认 data/bin）
	HTTP          *http.Client
	HealthTries   int // 启动健康检查重试次数

	mu    sync.Mutex
	procs map[string]*engine.Process // profileID → 运行句柄（进程内态，重启 Manager 后按 stopped 处理）

	installMu  sync.Mutex
	installing bool   // oxigraph 一键安装进行中（REQ-146）
	installErr string // 最近一次安装失败原因
}

func New(st *store.Store, buildURL, logDir, installBinDir string) *Manager {
	_ = os.MkdirAll(logDir, 0o755)
	_ = os.MkdirAll(installBinDir, 0o755)
	return &Manager{
		Store: st, Engines: map[string]engine.Runtime{}, BuildURL: strings.TrimRight(buildURL, "/"), LogDir: logDir,
		InstallBinDir: installBinDir,
		HTTP:          &http.Client{Timeout: 30 * time.Second},
		HealthTries:   20,
		procs:         map[string]*engine.Process{},
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
	return nil, fmt.Errorf("引擎 %q 未注册: %s", name, engineHint(name))
}

func engineHint(name string) string {
	switch name {
	case "fuseki":
		return "请下载 apache-jena-fuseki 并设置 FUSEKI_BIN 指向 fuseki-server 脚本（JDK 17+），重启 runtimed"
	case "oxigraph":
		return "请在本体运行页一键安装（写入 data/bin 即时生效），或安装 oxigraph_server 并加入 PATH（或设置 OXIGRAPH_BIN）后重启 runtimed"
	}
	return "请检查 runtimed 启动配置（对应引擎二进制未就绪或未注册）"
}

// knownEngines 引擎状态汇总的固定顺序（未注册的也呈现，REQ-146）。
var knownEngines = []string{"oxigraph", "fuseki"}

// EngineStatuses 引擎自检汇总（REQ-146）：oxigraph/fuseki 全量呈现（未注册=不可用 + 指引），
// 附加一键安装任务态。
func (m *Manager) EngineStatuses() []engine.EngineStatus {
	m.installMu.Lock()
	active, lastErr := m.installing, m.installErr
	m.installMu.Unlock()
	out := make([]engine.EngineStatus, 0, len(knownEngines))
	for _, name := range knownEngines {
		var st engine.EngineStatus
		if eng, ok := m.Engines[name]; ok {
			if p, ok := eng.(engine.StatusProbe); ok {
				st = p.Probe()
			} else {
				st = engine.EngineStatus{Engine: name, Registered: true}
			}
		} else {
			st = engine.EngineStatus{Engine: name, Registered: false, Hint: engineHint(name)}
		}
		if name == "oxigraph" {
			st.Installable = true
			st.Installing = active
			st.LastInstallError = lastErr
		}
		out = append(out, st)
	}
	return out
}

// StartInstall 异步发起一键安装（REQ-146，仅 oxigraph）：进行中返回错误；
// 结果经 EngineStatuses 的 Installing/LastInstallError/Installed 轮询呈现。
func (m *Manager) StartInstall(name string) error {
	if name != "oxigraph" {
		return fmt.Errorf("引擎 %q 暂不支持一键安装（fuseki 需 JDK + apache-jena-fuseki 解压，手动配置 FUSEKI_BIN）", name)
	}
	m.installMu.Lock()
	if m.installing {
		m.installMu.Unlock()
		return fmt.Errorf("安装任务进行中，请稍候")
	}
	m.installing = true
	m.installErr = ""
	m.installMu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		err := m.installOxigraph(ctx)
		m.installMu.Lock()
		m.installing = false
		if err != nil {
			m.installErr = err.Error()
		}
		m.installMu.Unlock()
	}()
	return nil
}

// installOxigraph 下载官方 release（GOOS/GOARCH 映射资产，pin PinnedVersion）流式落盘：
// temp + rename 原子替换，装后 oxigraph 适配器动态解析即时命中（Start 无需重启 runtimed）。
func (m *Manager) installOxigraph(ctx context.Context) error {
	asset, err := oxigraph.ReleaseAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	url := oxigraph.ReleaseBaseURL + "/" + asset
	target := filepath.Join(m.InstallBinDir, "oxigraph")
	if runtime.GOOS == "windows" {
		target += ".exe"
	}
	if err := downloadTo(ctx, url, target, 1<<20); err != nil {
		return err
	}
	log.Printf("[manager] oxigraph 一键安装完成: %s (%s)", target, asset)
	return nil
}

// downloadTo 流式下载 url → target（temp+rename 原子替换，非 windows 补执行位）。
// minBytes 下限防截断/错误页落盘。独立成函数便于对下载机制做零网络依赖单测（httptest）。
func downloadTo(ctx context.Context, url, target string, minBytes int64) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("创建安装目录失败: %w", err)
	}
	tmp := target + ".download"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := installHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败（%s）: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: %s（%s）", resp.Status, url)
	}
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("下载中断（已收 %d 字节）: %w", n, err)
	}
	if n < minBytes {
		_ = os.Remove(tmp)
		return fmt.Errorf("下载内容异常（仅 %d 字节，预期更大）: %s", n, url)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmp, 0o755); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

var installHTTP = &http.Client{Timeout: 10 * time.Minute}

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

// FetchVersion 拉取本体当前版本号（构建平面 meta；REQ-155 阶段二 drift 检测用）。
func (m *Manager) FetchVersion(ontologyID string) (int, error) {
	resp, err := m.HTTP.Get(fmt.Sprintf("%s/api/ontologies/%s", m.BuildURL, ontologyID))
	if err != nil {
		return 0, fmt.Errorf("构建平面不可达: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var meta struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(b, &meta) != nil {
		return 0, nil // 解析失败不阻断启动（版本快照尽力而为）
	}
	return meta.Version, nil
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
	loadedVersions := map[string]int{}
	for _, oid := range p.OntologyIDs {
		ttl, err := m.FetchTTL(oid)
		if err != nil {
			_ = m.Store.SetStatus(id, "error", err.Error(), "")
			return err
		}
		ttls[oid] = ttl
		if v, verr := m.FetchVersion(oid); verr == nil {
			loadedVersions[oid] = v // 尽力而为：版本快照失败不阻断启动
		}
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
			if b, jerr := json.Marshal(loadedVersions); jerr == nil {
				_ = m.Store.SetLoadedVersions(id, string(b)) // REQ-155 阶段二：加载版本快照（drift 检测）
			}
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
	// REQ-179/M-O16：执行方式为系统级全局配置——docker=容器执行（oxigraph 官方镜像）；
	// native=内置二进制子进程；k8s=接口预留。
	// REQ-236④/M63（D-O20 v0.73 变更，2026-10-01 开发者拍板）：默认执行方式改 docker，
	// **未安装 docker 时启动期降级进程内 native**（迁移 006 翻转存量默认值；此处为配置 docker
	// 但运行环境探测不可用的运行期兜底——配置值不写回，用户显式配置优先）。
	switch m.Store.GetConfig().ExecutionMethod {
	case "docker":
		if ox, ok := eng.(*oxigraph.Runtime); ok {
			if !oxigraph.DockerAvailable() {
				log.Printf("[manager] 方案 %s：执行方式配置为 docker 但 docker 不可用，降级进程内 native（REQ-236④）", p.ID)
				break
			}
			return ox.DockerStart(ctx, p.ID, port, ttls)
		}
		return nil, fmt.Errorf("执行方式 docker 当前仅支持 oxigraph 引擎（fuseki docker 化随需求推进）")
	case "k8s":
		return nil, fmt.Errorf("执行方式 k8s 为接口预留（复用 M10 10d K8sBackend 模式，随集群环境落地）——当前请在系统配置切换 docker 容器或内置二进制")
	}
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

// nextPort 端口分配（REQ-236③/M63）：在 max+1 基线上逐个 bind 试探（127.0.0.1 回环探测，
// 占用即跳过），治「手工指定/残留进程占用同端口只能靠引擎 bind 失败进 error」——分配即确认可用。
func nextPort(st *store.Store) int {
	list, _ := st.List()
	max := 9200
	taken := map[int]bool{}
	for _, p := range list {
		if p.Port > max {
			max = p.Port
		}
		if p.Port > 0 {
			taken[p.Port] = true
		}
	}
	for cand := max + 1; cand < max+64; cand++ {
		if taken[cand] {
			continue
		}
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cand)))
		if err != nil {
			continue // 占用（引擎残留/其他服务），跳过
		}
		_ = ln.Close()
		return cand
	}
	return max + 1 // 兜底（探测全部失败交由引擎启动报错，不静默吞）
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
