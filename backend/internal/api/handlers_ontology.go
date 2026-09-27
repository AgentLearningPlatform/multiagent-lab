package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
)

// ---- 本体对接（M8 §6.10）----

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
