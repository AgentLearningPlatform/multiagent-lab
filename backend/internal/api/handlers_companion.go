package api

import (
	"net/http"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/companion"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---------------------------------------------------------------------------
// REQ-170/M28 伴生本体 API（候选确认流）+ REQ-211/M44 图作用域 agent 化：
//   GET  /api/companion/candidates?conversation_id=&agent_id=&status=  候选列表（agent 维度跨会话铺平）
//   POST /api/companion/candidates/{id}/confirm                        确认入图（写 agent 图）
//   POST /api/companion/candidates/{id}/reject                          拒绝（不触达图）
//   GET  /api/companion/status?agent_id=                                agent 视角管线状态
//   GET  /api/companion/graph?agent_id=                                 agent 伴生图（成长可视化）
//   POST /api/companion/agents/{id}/reset                               agent 级整体摘除（DROP agent 图+清候选游标）
//   GET  /api/companion/graph-owner?conversation_id=                    会话→所属 agent 解析（runtime-manager facade 兼容消费）
// 边界（D-O19 第三来源）：伴生图独立于第五栏 KG 检索区。
// ---------------------------------------------------------------------------

// companionGraph GET /api/companion/graph?agent_id=（REQ-154 成长可视化；REQ-211 作用域=智能体）。
func (s *Server) companionGraph(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent_id 必填（REQ-211 起伴生图按智能体隔离）"})
		return
	}
	out, err := s.Companion.Graph(r.Context(), agentID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listCompanionCandidates(w http.ResponseWriter, r *http.Request) {
	convID := r.URL.Query().Get("conversation_id")
	agentID := r.URL.Query().Get("agent_id") // REQ-193/M33：agent 维度铺平过滤
	status := r.URL.Query().Get("status")
	list, err := s.Companion.Store.ListCompanionCandidates(convID, agentID, status)
	if err != nil {
		writeErr(w, err)
		return
	}
	// REQ-194⑥确认桶聚类：group_by=entity 按实体 slug 归组（代表候选=组内置信最高+组内计数）
	if r.URL.Query().Get("group_by") == "entity" {
		writeJSON(w, http.StatusOK, map[string]any{"groups": companion.GroupCandidatesByEntity(list)})
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
	writeJSON(w, http.StatusOK, map[string]any{"candidate": c, "graph": companion.GraphURI(c.AgentID)})
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
	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		writeErr(w, errBadRequest("agent_id 必填（REQ-211 起状态按智能体聚合）"))
		return
	}
	st, err := s.Companion.StatusByAgent(r.Context(), agentID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) resetCompanionAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Companion.ResetAgent(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"reset": true})
}

// companionGraphOwner GET /api/companion/graph-owner?conversation_id=（REQ-211 facade 兼容）：
// 会话→所属 agent 解析。agent 会话=conv.AgentID；项目会话无唯一归属（多成员 agent 各自伴生图）
// → 400 提示改传 agent_id（facade sparql_query 支持 agent_id 直传）。
func (s *Server) companionGraphOwner(w http.ResponseWriter, r *http.Request) {
	convID := r.URL.Query().Get("conversation_id")
	if convID == "" {
		writeErr(w, errBadRequest("conversation_id 必填"))
		return
	}
	conv, err := s.Store.GetConversation(convID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if conv.Scope == "project" || conv.AgentID == nil || *conv.AgentID == "" {
		writeErr(w, errBadRequest("项目会话无唯一所属智能体（多成员各自伴生图）——请按 agent_id 直传"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": *conv.AgentID, "graph": companion.GraphURI(*conv.AgentID)})
}

// errBadRequest 简单 400 错误（与既有 writeErr 语义对齐）。
func errBadRequest(msg string) error {
	return &store.HTTPError{Status: http.StatusBadRequest, Msg: msg}
}
