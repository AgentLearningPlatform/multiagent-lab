package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
)

// ---- 本体对接（M8 §6.10）----

// ontProfileRow 运行平面方案行的最小投影（GET /api/runtime-profiles，引用聚合与删除保护共用）。
type ontProfileRow struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Engine      string   `json:"engine"`
	OntologyIDs []string `json:"ontology_ids"`
	Status      string   `json:"status"`
}

// fetchOntologyPlans 运行平面方案全清单（不可达返回 err，由调用方决定拦截或降级）。
func (s *Server) fetchOntologyPlans(ctx context.Context) ([]ontProfileRow, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Ontology.RuntimeURL+"/api/runtime-profiles", nil)
	if err != nil {
		return nil, err
	}
	out, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(out.Body, 4<<20))
	if out.StatusCode >= 300 {
		return nil, fmt.Errorf("运行平面返回 %s", out.Status)
	}
	var rows []ontProfileRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// ontologyReferences REQ-233②/M60：本体「被引用」三源聚合——运行方案挂载（运行平面全清单
// 过滤）/ KB 约束词表（knowledge_base.kg_ontology_id）/ 智能体伴生绑定（companion_ontology_id），
// 详情页「被引用」区块与删除确认预检共用。运行平面不可达不阻断（方案段置空 + warnings 如实标注）。
func (s *Server) ontologyReferences(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	binders, err := s.Store.ListAgentsByCompanionOntology(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	kbs, err := s.Store.ListKBsByKGOntology(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	agents := make([]map[string]string, 0, len(binders))
	for _, a := range binders {
		agents = append(agents, map[string]string{"id": a.ID, "name": a.Name})
	}
	vocabs := make([]map[string]string, 0, len(kbs))
	for _, k := range kbs {
		vocabs = append(vocabs, map[string]string{"id": k.ID, "name": k.Name, "mode": k.Mode})
	}
	var warnings []string
	plans := []ontProfileRow{}
	rows, err := s.fetchOntologyPlans(r.Context())
	if err != nil {
		warnings = append(warnings, "运行平面不可达，方案挂载引用暂不可见: "+err.Error())
	} else {
		for _, p := range rows {
			if slices.Contains(p.OntologyIDs, id) {
				plans = append(plans, p)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ontology_id":      id,
		"runtime_plans":    plans,
		"kb_vocabs":        vocabs,
		"companion_agents": agents,
		"warnings":         warnings,
	})
}

// deleteOntologyGuard REQ-216 增量②b：本体删除伴生拦截——被绑定为伴生归属的本体
// 直接删除会留下悬挂绑定（宿主方案 start 永远失败）。有绑定者 → 409 附绑定者清单
// （先解绑/换绑再删，与模型连接删除 409 引用保护同口径）；无绑定者透传构建平面删除，
// 成功后清理伴生侧快照与端点缓存。注册为精确路由（压过 /api/ontologies/ 反代前缀）。
// REQ-233③/M60 删除保护扩展：running 方案引用拦截 409（删除会让方案重载/再启动必失败，
// 沿伴生 409 先例附引用者清单）；stopped/created/error 方案引用与 KB 词表引用警示放行——
// 删除时点的前端预检展示走 GET /{id}/references（同一聚合源）。
func (s *Server) deleteOntologyGuard(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	binders, err := s.Store.ListAgentsByCompanionOntology(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(binders) > 0 {
		names := make([]string, 0, len(binders))
		for _, a := range binders {
			names = append(names, a.Name)
		}
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "该本体被 " + fmt.Sprint(len(binders)) + " 个智能体绑定为伴生归属，须先在智能体侧板解绑/换绑后再删除",
			"binders": names,
		})
		return
	}
	// REQ-233③：running 方案引用硬闸。运行平面不可达时不拦删除（删除主链路在构建平面，
	// 不可达的引用可见性由 references 端点 warnings 诚实标注，不阻塞管理动作）。
	if rows, err := s.fetchOntologyPlans(r.Context()); err == nil {
		blocking := []ontProfileRow{}
		for _, p := range rows {
			if p.Status == "running" && slices.Contains(p.OntologyIDs, id) {
				blocking = append(blocking, p)
			}
		}
		if len(blocking) > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":         "该本体被 " + fmt.Sprint(len(blocking)) + " 个运行中方案挂载，须先在「本体运行」栏停止方案后再删除",
				"runtime_plans": blocking,
			})
			return
		}
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodDelete, s.Ontology.BuildURL+"/api/ontologies/"+id, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	out, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "构建平面不可达: " + err.Error()})
		return
	}
	defer out.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(out.Body, 64<<10))
	w.Header().Set("Content-Type", out.Header.Get("Content-Type"))
	w.WriteHeader(out.StatusCode)
	_, _ = w.Write(body)
	if out.StatusCode < 300 {
		s.Companion.OnOntologyDeleted(id) // 快照 + 端点缓存清理（伴生侧收尾）
	}
}

// generateOntologyLLM POST /api/ontology-llm/generate
// {prompt, schema, conn_id?} → {draft_json, usage}（REQ-98 模型能力代理）。
func (s *Server) generateOntologyLLM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt string `json:"prompt"`
		Schema string `json:"schema"`
		ConnID string `json:"conn_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt 不能为空"})
		return
	}
	if strings.TrimSpace(req.Schema) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "schema 不能为空"})
		return
	}
	// schema 必须是合法 JSON
	if !json.Valid([]byte(req.Schema)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "schema 必须是合法的 JSON 文本"})
		return
	}
	res, err := chat.GenerateStructured(r.Context(), s.Store, s.Box, req.ConnID, req.Prompt, req.Schema)
	if err != nil {
		writeErr(w, err)
		return
	}
	// draft_json 契约 = 草稿 JSON **字符串**（02 §6.10）：消费方 ontology-service llmcreate
	// 以 string 解码后再 Unmarshal。曾直嵌 json.RawMessage（对象形态）致对端
	// 「cannot unmarshal object into .draft_json of type string」——OntoChat 生成轮
	// 从未走通（2026-09-27 OntoChat 报障排查中暴露，第二层问题）。
	if res.Usage == nil {
		writeJSON(w, http.StatusOK, map[string]any{"draft_json": string(res.DraftJSON), "usage": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"draft_json": string(res.DraftJSON), "usage": res.Usage})
}
