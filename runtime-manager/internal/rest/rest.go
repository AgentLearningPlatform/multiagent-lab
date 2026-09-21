// Package rest 运行平面 REST（方案 04 §4.5）。
package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/manager"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/store"
)

type Server struct {
	Store   *store.Store
	Manager *manager.Manager
}

func New(st *store.Store, mg *manager.Manager) *Server { return &Server{Store: st, Manager: mg} }

func (s *Server) Mount(m *http.ServeMux) {
	m.HandleFunc("GET /api/runtime-profiles", s.list)
	m.HandleFunc("POST /api/runtime-profiles", s.create)
	m.HandleFunc("GET /api/runtime-profiles/{id}", s.get)
	m.HandleFunc("PUT /api/runtime-profiles/{id}", s.update)
	m.HandleFunc("DELETE /api/runtime-profiles/{id}", s.remove)
	m.HandleFunc("POST /api/runtime-profiles/{id}/start", s.start)
	m.HandleFunc("POST /api/runtime-profiles/{id}/stop", s.stop)
	m.HandleFunc("POST /api/runtime-profiles/{id}/reload", s.reload)
	m.HandleFunc("GET /api/runtime-profiles/{id}/logs", s.logs)
	m.HandleFunc("GET /api/runtime-profiles/{id}/trace", s.trace)    // 翻译透视（REQ-94）
	m.HandleFunc("GET /api/runtime-profiles/{id}/sparql", s.sparql)  // SPARQL 工作台端点（REQ-92，Yasgui）
	m.HandleFunc("POST /api/runtime-profiles/{id}/sparql", s.sparql) // 同上
	m.HandleFunc("GET /api/runtime-profiles/{id}/guide", s.guide)
	m.HandleFunc("GET /healthz", s.healthz)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, store.ErrNotFound) {
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.List()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string   `json:"name"`
		Engine      string   `json:"engine"`
		OntologyIDs []string `json:"ontology_ids"`
		Config      string   `json:"config"`
		Port        int      `json:"port"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name 必填"})
		return
	}
	if req.Engine == "" {
		req.Engine = "oxigraph" // P1 唯一引擎
	}
	if req.Engine != "oxigraph" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "P1 仅支持 oxigraph 引擎（fuseki/memory_graph 为 P2）"})
		return
	}
	if req.Config == "" {
		req.Config = "{}"
	}
	id := "rt_" + newID()
	p := &store.Profile{ID: id, Name: req.Name, Engine: req.Engine, OntologyIDs: req.OntologyIDs, Config: req.Config, Port: req.Port}
	if err := s.Store.Create(p); err != nil {
		writeErr(w, err)
		return
	}
	out, _ := s.Store.Get(id)
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Name        string   `json:"name"`
		OntologyIDs []string `json:"ontology_ids"`
		Config      string   `json:"config"`
		Port        int      `json:"port"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	// 配置变更需重启生效：running 方案先停
	p, err := s.Store.Get(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if p.Status == "running" || p.Status == "starting" {
		if err := s.Manager.Stop(id); err != nil {
			writeErr(w, err)
			return
		}
	}
	if err := s.Store.UpdateMeta(id, req.Name, orDefault(req.Config, p.Config), req.OntologyIDs, req.Port); err != nil {
		writeErr(w, err)
		return
	}
	out, _ := s.Store.Get(id)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.Store.Get(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if p.Status == "running" || p.Status == "starting" {
		_ = s.Manager.Stop(id)
	}
	if err := s.Store.Delete(id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Manager.Start(r.Context(), id); err != nil {
		// 失败细节已落库（status=error + last_error），返回当前快照
		p, _ := s.Store.Get(id)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "profile": p})
		return
	}
	out, _ := s.Store.Get(id)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	if err := s.Manager.Stop(r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	out, _ := s.Store.Get(r.PathValue("id"))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) reload(w http.ResponseWriter, r *http.Request) {
	if err := s.Manager.Reload(r.Context(), r.PathValue("id")); err != nil {
		p, _ := s.Store.Get(r.PathValue("id"))
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "profile": p})
		return
	}
	out, _ := s.Store.Get(r.PathValue("id"))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail <= 0 {
		tail = 200
	}
	lines, err := s.Manager.Logs(r.PathValue("id"), tail)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

// trace GET /api/runtime-profiles/{id}/trace?limit=50：翻译透视记录，时间倒序（REQ-94）。
func (s *Server) trace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.Store.Get(id); err != nil {
		writeErr(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	traces, err := s.Store.ListTraces(id, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile_id": id, "traces": traces})
}

// sparql GET|POST /api/runtime-profiles/{id}/sparql：标准 SPARQL protocol 端点
// （REQ-92，§4.8.1 Yasgui 内嵌工作台数据源）。GET ?query= 与 POST
// application/sparql-query 均支持；响应头与状态码透传引擎返回。
func (s *Server) sparql(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.Store.Get(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if p.Status != "running" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "方案未运行（当前状态 " + p.Status + "），启动后再使用 SPARQL 工作台"})
		return
	}
	ep, err := s.Manager.ProcEndpoint(id)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	var req *http.Request
	if r.Method == http.MethodGet {
		q := r.URL.Query().Get("query")
		if strings.TrimSpace(q) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 query 参数"})
			return
		}
		req, err = http.NewRequestWithContext(r.Context(), "GET", ep+"?query="+url.QueryEscape(q), nil)
	} else {
		body, err2 := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err2 != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "读取请求体失败"})
			return
		}
		req, err = http.NewRequestWithContext(r.Context(), "POST", ep, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/sparql-query")
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	accept := r.Header.Get("Accept")
	if accept == "" {
		accept = "application/sparql-results+json"
	}
	req.Header.Set("Accept", accept)
	resp, err := sparqlHTTP.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "引擎请求失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

var sparqlHTTP = &http.Client{Timeout: 30 * time.Second}

// guide 代理构建平面本体指引（方案 04 §5 指引注入）：
// 主平台装配时 GET /api/runtime-profiles/{id}/guide[?ontology_id=…]。
//
// 显式传 ontology_id：校验方案存在后透传构建平面 guide（JSON 原样返回，行为不变）。
// 不传（生产装配调用方只知挂载方案，方案可绑定多个本体）：加载方案并遍历
// ontology_ids 逐个拉取，best-effort 拼接后返回 {ontology_id, guide} 统一形状。
func (s *Server) guide(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ontologyID := strings.TrimSpace(r.URL.Query().Get("ontology_id"))

	// ---- 显式指定 ontology_id：保持原行为（原样透传构建平面 JSON）----
	if ontologyID != "" {
		if _, err := s.Store.Get(id); err != nil {
			writeErr(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		url := fmt.Sprintf("%s/api/ontologies/%s/guide", s.Manager.BuildURL, ontologyID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "构建平面不可达: " + err.Error()})
			return
		}
		resp, err := s.Manager.HTTP.Do(req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "构建平面不可达: " + err.Error()})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("构建平面不可达: %s %s", resp.Status, string(b))})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	// ---- 未指定 ontology_id：遍历方案绑定本体，best-effort 拼接 ----
	p, err := s.Store.Get(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(p.OntologyIDs) == 0 {
		// 无绑定本体：明确 400，装配侧按失败降级为 ontology.unavailable。
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "方案未绑定本体"})
		return
	}
	var (
		parts   []string // 非空指引片段（已加来源前缀）
		okIDs   []string // 拉取成功的 ontology_id（含指引为空的）
		lastErr error
	)
	for _, oid := range p.OntologyIDs {
		oid = strings.TrimSpace(oid)
		if oid == "" {
			continue
		}
		gotID, guide, ferr := s.fetchOntologyGuide(r.Context(), oid)
		if ferr != nil {
			lastErr = ferr
			continue // best-effort：单个本体失败跳过，不阻断整体注入
		}
		if gotID == "" {
			gotID = oid
		}
		okIDs = append(okIDs, gotID)
		if g := strings.TrimSpace(guide); g != "" {
			parts = append(parts, fmt.Sprintf("来源：%s\n%s", gotID, g))
		}
	}
	if len(okIDs) == 0 {
		// 全部本体拉取失败 → 构建平面不可达（装配侧降级）。
		msg := "构建平面不可达"
		if lastErr != nil {
			msg += ": " + lastErr.Error()
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": msg})
		return
	}
	respID := strings.Join(okIDs, ",")
	if respID == "" {
		respID = p.ID
	}
	writeJSON(w, http.StatusOK, map[string]string{"ontology_id": respID, "guide": strings.Join(parts, "\n\n")})
}

// fetchOntologyGuide 从构建平面拉取单个本体指引（5s 预算），
// 解析统一形状 {ontology_id, guide}；非 200 或解析失败返回错误（由调用方 best-effort 处理）。
func (s *Server) fetchOntologyGuide(ctx context.Context, ontologyID string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	url := fmt.Sprintf("%s/api/ontologies/%s/guide", s.Manager.BuildURL, ontologyID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := s.Manager.HTTP.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return "", "", fmt.Errorf("%s %s", resp.Status, string(b))
	}
	var out struct {
		OntologyID string `json:"ontology_id"`
		Guide      string `json:"guide"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	return out.OntologyID, out.Guide, nil
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.List()
	if err != nil {
		writeErr(w, err)
		return
	}
	summaries := []map[string]any{}
	for _, p := range list {
		summaries = append(summaries, map[string]any{"id": p.ID, "status": p.Status, "engine": p.Engine, "port": p.Port})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "profiles": summaries})
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func newID() string {
	return strings.ReplaceAll(time.Now().UTC().Format("060102150405.000000000"), ".", "")
}
