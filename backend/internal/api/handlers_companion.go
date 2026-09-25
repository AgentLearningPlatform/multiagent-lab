package api

import (
	"net/http"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/companion"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---------------------------------------------------------------------------
// REQ-170/M28 伴生本体 P1 API（候选确认流，方案"API 先行"）：
//   GET  /api/companion/candidates?conversation_id=&status=   候选列表
//   POST /api/companion/candidates/{id}/confirm               确认入图（矛盾旧边失效化）
//   POST /api/companion/candidates/{id}/reject                 拒绝（不触达图）
//   GET  /api/companion/status?conversation_id=                管线状态（引擎/游标/pending/标签）
//   POST /api/companion/conversations/{id}/reset               会话级整体摘除（DROP GRAPH+清表）
// 边界（D-O19 第三来源）：伴生图独立于第五栏 KG 检索区，前端页签属资产栏（后续轮次）。
// ---------------------------------------------------------------------------

func (s *Server) listCompanionCandidates(w http.ResponseWriter, r *http.Request) {
	convID := r.URL.Query().Get("conversation_id")
	status := r.URL.Query().Get("status")
	list, err := s.Companion.Store.ListCompanionCandidates(convID, status)
	if err != nil {
		writeErr(w, err)
		return
	}
	if list == nil {
		list = []*store.CompanionCandidate{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) confirmCompanionCandidate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.Companion.ConfirmCandidate(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidate": c, "graph": companion.GraphURI(c.ConversationID)})
}

func (s *Server) rejectCompanionCandidate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.Companion.RejectCandidate(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) companionStatus(w http.ResponseWriter, r *http.Request) {
	convID := r.URL.Query().Get("conversation_id")
	if convID == "" {
		writeErr(w, errBadRequest("conversation_id 必填"))
		return
	}
	st, err := s.Companion.Status(r.Context(), convID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) resetCompanionConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Companion.ResetConversation(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"reset": true})
}

// errBadRequest 简单 400 错误（与既有 writeErr 语义对齐）。
func errBadRequest(msg string) error { return &store.HTTPError{Status: http.StatusBadRequest, Msg: msg} }
