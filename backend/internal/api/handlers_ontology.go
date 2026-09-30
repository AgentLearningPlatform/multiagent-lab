package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
)

// ---- 本体对接（M8 §6.10）----

// deleteOntologyGuard REQ-216 增量②b：本体删除伴生拦截——被绑定为伴生归属的本体
// 直接删除会留下悬挂绑定（宿主方案 start 永远失败）。有绑定者 → 409 附绑定者清单
// （先解绑/换绑再删，与模型连接删除 409 引用保护同口径）；无绑定者透传构建平面删除，
// 成功后清理伴生侧快照与端点缓存。注册为精确路由（压过 /api/ontologies/ 反代前缀）。
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
