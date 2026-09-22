// rest_ontochat.go OntoChat 多轮引导端点（REQ-103 模式 A）。
// 会话状态机：cq（领域描述+CQ）→ domain（逐轮补全）→ draft/refine（草稿+校验回喂）→ done（已入库）。
// 模型能力归主平台（复用 llmcreate.Creator → /api/ontology-llm/generate），校验归构建平面（spec.Validate）。
package rest

import (
	"encoding/json"
	"net/http"
	"strings"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"

	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/ontochat"
)

// listOntoChatSessions GET /api/ontochat/sessions → 会话列表（不含消息体）
func (s *Server) listOntoChatSessions(w http.ResponseWriter, _ *http.Request) {
	sessions, err := s.ontoChatStore().List()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

// createOntoChatSession POST /api/ontochat/sessions {title?} → 201 新会话（stage=cq）
func (s *Server) createOntoChatSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	_ = decodeJSON(r, &req) // body 可省略
	sess, err := s.ontoChatStore().Create(newID(), strings.TrimSpace(req.Title))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

// getOntoChatSession GET /api/ontochat/sessions/{id} → 会话全量（含消息与上下文）
func (s *Server) getOntoChatSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.ontoChatStore().Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

// deleteOntoChatSession DELETE /api/ontochat/sessions/{id}
func (s *Server) deleteOntoChatSession(w http.ResponseWriter, r *http.Request) {
	if err := s.ontoChatStore().Delete(r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": r.PathValue("id")})
}

// ontoChatTurn POST /api/ontochat/sessions/{id}/turn
// {text, feedback?}：text 为用户输入；feedback 非空表示 refine 修正轮（意见回喂重新生成）。
// 返回 {reply, stage, round, draft?, warning?, session}；draft 仅在生成轮产出。
func (s *Server) ontoChatTurn(w http.ResponseWriter, r *http.Request) {
	if s.LLM == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "LLM 辅助创建未配置（PLATFORM_URL）"})
		return
	}
	var req struct {
		Text     string `json:"text"`
		Feedback string `json:"feedback"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	st := s.ontoChatStore()
	sess, err := st.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(req.Text) == "" && strings.TrimSpace(req.Feedback) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text 不能为空"})
		return
	}
	// 用户消息落库（refine 轮 text 可为空，仅意见）
	if strings.TrimSpace(req.Text) != "" {
		if err := st.Append(sess.ID, ontochat.Message{Role: "user", Content: req.Text}, nil, nil, nil); err != nil {
			writeErr(w, err)
			return
		}
	}
	var res *ontochat.TurnResult
	if strings.TrimSpace(req.Feedback) != "" {
		res, err = s.OntoChat.Refine(st, sess, req.Feedback)
	} else {
		res, err = s.OntoChat.Turn(st, sess, req.Text)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	// 重新读取（Append 已更新 stage/round/context）
	fresh, err := st.Get(sess.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := map[string]any{
		"reply":  res.Reply,
		"stage":  res.NextStage,
		"round":  fresh.Round,
		"session": fresh,
	}
	if res.Draft != nil {
		out["draft"] = res.Draft
	}
	if res.Warning != "" {
		out["warning"] = res.Warning
	}
	writeJSON(w, http.StatusOK, out)
}

// ontoChatSave POST /api/ontochat/sessions/{id}/save {name} → 草稿入库（预览确认门控，REQ-82）
func (s *Server) ontoChatSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	st := s.ontoChatStore()
	sess, err := st.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if sess.Context.DraftSpec == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "会话尚无草稿，请先完成生成轮"})
		return
	}
	var sp pkgspec.Spec
	if err := json.Unmarshal(*sess.Context.DraftSpec, &sp); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "草稿解析失败: " + err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = sp.Name
	}
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请提供本体名称"})
		return
	}
	sp.Name = name
	if errs := sp.Validate(); len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "草稿校验未通过", "validation_errors": errs})
		return
	}
	o, err := s.Store.CreateOntology(newID(), name, sp.Description)
	if err != nil {
		writeErr(w, err)
		return
	}
	bts, _ := json.Marshal(sp)
	if err := s.Store.PutArtifact(o.ID, "spec_json", string(bts), true); err != nil {
		writeErr(w, err)
		return
	}
	_ = s.Store.SaveVersion(o.ID, 1, string(bts), "", "")
	_ = st.BindOntology(sess.ID, o.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"ontology": o, "session": mustSession(st, sess.ID)})
}

// ontoChatStore 惰性初始化会话存储（表由 migrations/003_ontochat.sql 建）。
func (s *Server) ontoChatStore() *ontochat.Store {
	if s.ontoChatDB == nil {
		s.ontoChatDB = ontochat.New(s.Store.DB())
	}
	return s.ontoChatDB
}

func mustSession(st *ontochat.Store, id string) *ontochat.Session {
	sess, err := st.Get(id)
	if err != nil {
		return nil
	}
	return sess
}
