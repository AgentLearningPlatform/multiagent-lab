package companion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// REQ-216/M47 批次一③ 引擎归一运行平面：伴生 SPARQL 读写面 = 运行平面（runtime-manager）
// 伴生宿主方案的引擎端点。绑定本体时确保宿主方案——查找含该本体的 running 方案复用；
// 无 running 则复用同本体存量方案拉起；再无则自动创建「伴生·{本体名}」方案
// （engine=oxigraph，ontology_ids=[本体]）并 start。读侧兜底拉起 = 读路径同样走 Ensure
// （宿主方案未 running 自动拉起，治「读路径引擎死了永远空转」）。
// 内置独立实例（懒启动/领养/:9199/COMPANION_OXIGRAPH_BIN/COMPANION_GRAPH_ENDPOINT）随迁移退役。
// 边界：默认 oxigraph（SPARQL 1.1 标准写法，fuseki 等标准兼容引擎留可选）；运维由
// 运行平面方案管理界面自然承载（伴生宿主方案在运行页可见可启停），不新增设置页配置。
// ---------------------------------------------------------------------------

// PlanEngines 运行平面方案引擎客户端（伴生读写面）。
type PlanEngines struct {
	// RuntimeURL 运行平面基址（RUNTIME_MGR_URL，默认 http://127.0.0.1:8090）。
	RuntimeURL string
	// BuildURL 构建平面基址（BUILD_SVC_URL，默认 http://127.0.0.1:8091；建宿主方案时取本体名）。
	BuildURL string
	HTTP     *http.Client

	mu      sync.Mutex
	eps     map[string]string // ontologyID → 引擎基址缓存（查询失败失效重查；宿主方案重建后自愈）
	hostIDs map[string]string // ontologyID → 宿主方案 id（status 透出/观测）
	names   map[string]string // ontologyID → 宿主方案名（status 透出，增量轮③可观测）
	locks   map[string]*sync.Mutex // REQ-216 增量④：每本体串行锁（Ensure 单飞化——防并发重复建方案）
	// 增量轮④：拉起失败冷却（error 态方案在冷却期内不重试 start——防每次读路径同步重试风暴）
	startFailAt  map[string]time.Time
	startFailErr map[string]string
}

// startCooldown 拉起失败冷却期（读过路径遇 error 态宿主方案期间快速失败，不阻塞接口；var 便于测试收紧）。
var startCooldown = 30 * time.Second

// NewPlanEngines 构造（URL 空取默认同机端口）。
func NewPlanEngines(runtimeURL, buildURL string) *PlanEngines {
	if runtimeURL == "" {
		runtimeURL = getenvDefault("RUNTIME_MGR_URL", "http://127.0.0.1:8090")
	}
	if buildURL == "" {
		buildURL = getenvDefault("BUILD_SVC_URL", "http://127.0.0.1:8091")
	}
	return &PlanEngines{
		RuntimeURL: strings.TrimRight(runtimeURL, "/"),
		BuildURL:   strings.TrimRight(buildURL, "/"),
		HTTP:       &http.Client{Timeout: 60 * time.Second}, // start 为同步健康等待，预算放宽
		eps:        map[string]string{},
		hostIDs:    map[string]string{},
		names:      map[string]string{},
		locks:      map[string]*sync.Mutex{},
		startFailAt:  map[string]time.Time{},
		startFailErr: map[string]string{},
	}
}

// HostPlanName 当前本体宿主方案名（Ensure 成功后可读；未解析为空）。
func (p *PlanEngines) HostPlanName(ontologyID string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.names[ontologyID]
}

func getenvDefault(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

// runtimeProfile 运行平面方案快照（REST 形态子集）。
type runtimeProfile struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Engine      string   `json:"engine"`
	OntologyIDs []string `json:"ontology_ids"`
	Port        int      `json:"port"`
	Status      string   `json:"status"`
	LastError   string   `json:"last_error,omitempty"`
}

// engineBase 方案引擎 SPARQL 基址（oxigraph/fuseki 同形：/query 与 /update）。
func engineBase(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }

// HostPlan 当前本体宿主方案 id（Ensure 成功后可读；未解析为空）。
func (p *PlanEngines) HostPlan(ontologyID string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hostIDs[ontologyID]
}

// ontologyLock 每本体串行锁（REQ-216 增量④：Ensure 单飞化——同本体并发确认时
// list→create 段不再竞态重复建方案）。
func (p *PlanEngines) ontologyLock(ontologyID string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	if lk, ok := p.locks[ontologyID]; ok {
		return lk
	}
	lk := &sync.Mutex{}
	p.locks[ontologyID] = lk
	return lk
}

// EnsureHostPlan 确保本体宿主方案 running，返回引擎基址。三段式：
// 缓存命中 → 直返；含该本体的 running 方案 → 复用；同本体存量方案 → 拉起；
// 无任何方案 → 创建「伴生·{本体名}」并 start。运行平面不可达返回错误（调用方降级）。
// 全程持每本体锁（REQ-216 增量④单飞化；缓存命中路径无开销差异）。
func (p *PlanEngines) EnsureHostPlan(ctx context.Context, ontologyID string) (string, error) {
	if ontologyID == "" {
		return "", fmt.Errorf("本体 id 为空（伴生未绑定本体）")
	}
	lk := p.ontologyLock(ontologyID)
	lk.Lock()
	defer lk.Unlock()

	p.mu.Lock()
	if base, ok := p.eps[ontologyID]; ok {
		p.mu.Unlock()
		return base, nil
	}
	if at, ok := p.startFailAt[ontologyID]; ok && time.Since(at) < startCooldown {
		errMsg := p.startFailErr[ontologyID]
		p.mu.Unlock()
		return "", fmt.Errorf("宿主方案拉起冷却中（%s 内不重试）: %s", startCooldown, errMsg)
	}
	p.mu.Unlock()

	profiles, err := p.listProfiles(ctx)
	if err != nil {
		return "", fmt.Errorf("运行平面不可达（%s）: %w", p.RuntimeURL, err)
	}
	// ① running 方案复用（用户手动组建含该本体的方案优先——不重复建实例）
	for i := range profiles {
		pr := &profiles[i]
		if pr.Status == "running" && containsOID(pr.OntologyIDs, ontologyID) {
			return p.remember(ontologyID, pr), nil
		}
	}
	// ② 同本体存量方案拉起（stopped/error/created——上次宿主或用户建后未启；失败进冷却）
	for i := range profiles {
		pr := &profiles[i]
		if containsOID(pr.OntologyIDs, ontologyID) {
			started, err := p.startProfile(ctx, pr.ID)
			if err != nil {
				p.recordStartFail(ontologyID, err.Error())
				return "", fmt.Errorf("宿主方案 %s（%s）拉起失败: %w", pr.Name, pr.ID, err)
			}
			p.clearStartFail(ontologyID)
			if started.Port == 0 { // 启动响应异常兜底：回读快照
				started, _ = p.getProfile(ctx, pr.ID)
			}
			return p.remember(ontologyID, started), nil
		}
	}
	// ③ 自动创建「伴生·{本体名}」并 start
	name, _ := p.OntologyName(ctx, ontologyID)
	if name == "" {
		name = ontologyID
	}
	// ②命名去叠加（增量轮②）：本体名已带「伴生·」前缀（如历史宿主名直接复用为本体名）不重复拼接
	planName := name
	if !strings.HasPrefix(name, "伴生·") {
		planName = "伴生·" + name
	}
	created, err := p.createProfile(ctx, planName, ontologyID)
	if err != nil {
		return "", fmt.Errorf("创建伴生宿主方案失败: %w", err)
	}
	started, err := p.startProfile(ctx, created.ID)
	if err != nil {
		p.recordStartFail(ontologyID, err.Error())
		return "", fmt.Errorf("伴生宿主方案 %s 启动失败: %w", created.Name, err)
	}
	p.clearStartFail(ontologyID)
	if started.Port == 0 { // 启动响应异常兜底：回读快照
		started, _ = p.getProfile(ctx, created.ID)
	}
	return p.remember(ontologyID, started), nil
}

// remember 登记端点/方案 id/方案名缓存（并发下后写胜出——同一本体的宿主方案是收敛单解）。
func (p *PlanEngines) remember(ontologyID string, pr *runtimeProfile) string {
	base := engineBase(pr.Port)
	p.mu.Lock()
	p.eps[ontologyID] = base
	p.hostIDs[ontologyID] = pr.ID
	p.names[ontologyID] = pr.Name
	delete(p.startFailAt, ontologyID)
	p.mu.Unlock()
	return base
}

// recordStartFail / clearStartFail 拉起失败冷却登记（增量轮④）。
func (p *PlanEngines) recordStartFail(ontologyID, msg string) {
	p.mu.Lock()
	p.startFailAt[ontologyID] = time.Now()
	p.startFailErr[ontologyID] = msg
	p.mu.Unlock()
}

func (p *PlanEngines) clearStartFail(ontologyID string) {
	p.mu.Lock()
	delete(p.startFailAt, ontologyID)
	delete(p.startFailErr, ontologyID)
	p.mu.Unlock()
}

// Invalidate 端点缓存失效（查询失败时调用；下次操作重解析宿主方案）。
func (p *PlanEngines) Invalidate(ontologyID string) {
	p.mu.Lock()
	delete(p.eps, ontologyID)
	p.mu.Unlock()
}

func containsOID(ids []string, oid string) bool {
	for _, id := range ids {
		if id == oid {
			return true
		}
	}
	return false
}

// listProfiles GET /api/runtime-profiles。
func (p *PlanEngines) listProfiles(ctx context.Context) ([]runtimeProfile, error) {
	var out struct {
		Profiles []runtimeProfile `json:"profiles"`
	}
	// 运行平面 list 返回裸数组（rest.go list 直出 Store.List()）
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.RuntimeURL+"/api/runtime-profiles", nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("list 返回 %s: %s", resp.Status, truncateStr(string(body), 200))
	}
	if err := json.Unmarshal(body, &out.Profiles); err != nil {
		return nil, fmt.Errorf("方案列表解析失败: %w", err)
	}
	return out.Profiles, nil
}

// getProfile GET /api/runtime-profiles/{id}（启动响应缺 port 时回读）。
func (p *PlanEngines) getProfile(ctx context.Context, id string) (*runtimeProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/runtime-profiles/%s", p.RuntimeURL, id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get 返回 %s", resp.Status)
	}
	var out runtimeProfile
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// startProfile POST /api/runtime-profiles/{id}/start（同步健康等待）。
func (p *PlanEngines) startProfile(ctx context.Context, id string) (*runtimeProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api/runtime-profiles/%s/start", p.RuntimeURL, id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("start 返回 %s: %s", resp.Status, truncateStr(string(body), 300))
	}
	var out runtimeProfile
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// createProfile POST /api/runtime-profiles（engine=oxigraph 默认档）。
func (p *PlanEngines) createProfile(ctx context.Context, name, ontologyID string) (*runtimeProfile, error) {
	payload, _ := json.Marshal(map[string]any{"name": name, "engine": "oxigraph", "ontology_ids": []string{ontologyID}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.RuntimeURL+"/api/runtime-profiles", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("create 返回 %s: %s", resp.Status, truncateStr(string(body), 300))
	}
	var out runtimeProfile
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// OntologyName 构建平面取本体名（建宿主方案命名/绑定存在性校验用；失败返回错误——Ensure 忽略回退 ontologyID）。
func (p *PlanEngines) OntologyName(ctx context.Context, ontologyID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/ontologies/%s", p.BuildURL, ontologyID), nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("构建平面返回 %s", resp.Status)
	}
	var out struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Name, nil
}

// FindOntologyByName 构建平面按名查找本体（绑定回填幂等复用；不命中返回空 id，非错误）。
func (p *PlanEngines) FindOntologyByName(ctx context.Context, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BuildURL+"/api/ontologies", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("构建平面返回 %s", resp.Status)
	}
	var list []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return "", err
	}
	for _, o := range list {
		if o.Name == name {
			return o.ID, nil
		}
	}
	return "", nil
}

// OntologyInfo 构建平面取本体名+描述（迁移同名复用的描述标记校验用）。
func (p *PlanEngines) OntologyInfo(ctx context.Context, ontologyID string) (name, desc string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/ontologies/%s", p.BuildURL, ontologyID), nil)
	if err != nil {
		return "", "", err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("构建平面返回 %s", resp.Status)
	}
	var out struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	return out.Name, out.Description, nil
}

// CreateEmptyOntology REQ-216②：绑定交互「一键创建空本体」——经构建平面 POST /api/ontologies。
func (p *PlanEngines) CreateEmptyOntology(ctx context.Context, name, description string) (string, error) {
	payload, _ := json.Marshal(map[string]any{"name": name, "description": description})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BuildURL+"/api/ontologies", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("构建平面不可达（%s）: %w", p.BuildURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("创建本体返回 %s: %s", resp.Status, truncateStr(string(body), 300))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("创建本体响应缺 id")
	}
	return out.ID, nil
}

// Query 对宿主方案引擎执行 SPARQL SELECT（base 由 EnsureHostPlan 解析）。
func (p *PlanEngines) Query(ctx context.Context, base, sparql string) ([]byte, error) {
	tr := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/query", strings.NewReader(sparql))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")
	resp, err := tr.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("伴生图查询被拒（%s）", resp.Status)
	}
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
		if len(buf) > 4<<20 {
			return nil, fmt.Errorf("伴生图查询结果过大")
		}
	}
	return buf, nil
}

// Update 对宿主方案引擎发 SPARQL UPDATE（INSERT/DELETE/DROP）。
func (p *PlanEngines) Update(ctx context.Context, base, sparql string) error {
	tr := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/update", strings.NewReader(sparql))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/sparql-update")
	resp, err := tr.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return fmt.Errorf("伴生图更新被拒（%s）: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
