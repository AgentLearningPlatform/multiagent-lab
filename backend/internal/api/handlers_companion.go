package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/companion"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---------------------------------------------------------------------------
// REQ-170/M28 伴生本体 API（候选确认流）+ REQ-216/M47 伴生本体资产化：
//   GET  /api/companion/candidates?conversation_id=&agent_id=&ontology_id=&status=  候选列表
//        （agent 维度铺平=侧板；ontology_id 维度=资产详情页「伴生候选」页签——双入口同表）
//   POST /api/companion/candidates/{id}/confirm                        确认入图（写本体伴生子图）
//   POST /api/companion/candidates/{id}/reject                          拒绝（不触达图）
//   GET  /api/companion/status?agent_id=                                agent 视角管线状态（宿主方案可观测）
//   GET  /api/companion/graph?agent_id=                                 本体伴生子图（成长可视化）
//   POST /api/companion/agents/{id}/bind                                绑定伴生本体（选择/一键创建空本体；确保宿主方案）
//   POST /api/companion/agents/{id}/reset                               agent 级解绑（清该 agent 候选游标；图数据留本体）
//   GET  /api/companion/ontologies/{id}/candidates                      本体视角候选（跨 agent）
//   GET  /api/companion/ontologies/{id}/agents                          绑定该本体的 agent 清单
//   POST /api/companion/ontologies/{id}/reset                           本体级伴生图清空（DROP 子图+清全部绑定 agent 候选游标）
//   GET  /api/companion/bound-ontologies                                伴生绑定本体 id 清单（资产列表「对话生长」徽标）
//   GET  /api/companion/graph-owner?conversation_id=|agent_id=          解析伴生图归属（runtime-manager facade 兼容消费）
// ---------------------------------------------------------------------------

// companionGraph GET /api/companion/graph?agent_id=（REQ-154 成长可视化；REQ-216 图=本体伴生子图）。
func (s *Server) companionGraph(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent_id 必填"})
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
	ontID := r.URL.Query().Get("ontology_id") // REQ-216⑥：本体维度跨 agent（资产详情页）
	status := r.URL.Query().Get("status")
	if ontID != "" {
		list, err := s.Companion.Store.ListCompanionCandidatesByOntology(ontID, status)
		if err != nil {
			writeErr(w, err)
			return
		}
		// REQ-216 增量③：本体视角批量——group_by=entity 按实体归组（详情页「全部入图/拒绝」，
		// 与侧板 agent 视角批量同构；REQ-216⑥「confirm/reject+批量」范围补齐）
		if r.URL.Query().Get("group_by") == "entity" {
			writeJSON(w, http.StatusOK, map[string]any{"groups": companion.GroupCandidatesByEntity(list)})
			return
		}
		if list == nil {
			list = []*store.CompanionCandidate{}
		}
		writeJSON(w, http.StatusOK, list)
		return
	}
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
	graph := ""
	if agent, aerr := s.Store.GetAgent(c.AgentID); aerr == nil {
		graph = companion.GraphURI(agent.CompanionOntologyID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidate": c, "graph": graph})
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

// resetCompanionAgent POST /api/companion/agents/{id}/reset（REQ-216 摘除→解绑语义）：
// 清该 agent 全部候选与游标 + 断开绑定；本体伴生子图与同本体其他 agent 沉淀保留。
func (s *Server) resetCompanionAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Companion.ResetAgent(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"reset": true})
}

// bindCompanionAgent POST /api/companion/agents/{id}/bind（REQ-216②绑定交互）：
// {"ontology_id": "..."} 选择既有本体；{"create": {"name"?: "..."}} 一键创建空本体
// （缺省名「{agent 名}的伴生本体」）。绑定成功即确保宿主方案 running（读侧立即可查）。
func (s *Server) bindCompanionAgent(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	agent, err := s.Store.GetAgent(agentID)
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		OntologyID string `json:"ontology_id"`
		Create     *struct {
			Name string `json:"name"`
		} `json:"create"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	ontID := strings.TrimSpace(body.OntologyID)
	if ontID != "" {
		// REQ-216 增量②a：绑定校验——本体必须真实存在（防脏绑定悬挂：宿主方案 start 永远失败）
		if _, err := s.Companion.Plans.OntologyName(r.Context(), ontID); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "本体不存在或构建平面不可达（" + ontID + "）: " + err.Error()})
			return
		}
	}
	if ontID == "" {
		if body.Create == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ontology_id 必填，或传 create 创建空本体"})
			return
		}
		name := strings.TrimSpace(body.Create.Name)
		if name == "" {
			name = companion.CompanionOntologyName(agent.Name)
		}
		if ontID, err = s.Companion.Plans.CreateEmptyOntology(context.Background(), name, "伴生本体（智能体 "+agent.Name+" 对话生长产物归属容器）"); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
	}
	if err := s.Store.SetAgentCompanionBinding(agentID, ontID); err != nil {
		writeErr(w, err)
		return
	}
	// 绑定即确保宿主方案（REQ-216③：查找复用/自动创建并 start）——失败不回滚绑定，
	// 错误透出由读侧兜底拉起兜底（诚实呈现）
	planErr := ""
	if _, err := s.Companion.Plans.EnsureHostPlan(r.Context(), ontID); err != nil {
		planErr = err.Error()
	}
	updated, _ := s.Store.GetAgent(agentID)
	out := map[string]any{"agent": updated, "ontology_id": ontID, "graph": companion.GraphURI(ontID)}
	if planErr != "" {
		out["plan_error"] = planErr
	}
	writeJSON(w, http.StatusOK, out)
}

// ontologyCompanionCandidates GET /api/companion/ontologies/{id}/candidates（REQ-216⑥）。
func (s *Server) ontologyCompanionCandidates(w http.ResponseWriter, r *http.Request) {
	ontID := r.PathValue("id")
	status := r.URL.Query().Get("status")
	list, err := s.Companion.Store.ListCompanionCandidatesByOntology(ontID, status)
	if err != nil {
		writeErr(w, err)
		return
	}
	// REQ-216 增量③：本体视角批量——group_by=entity 按实体归组（详情页「全部入图/拒绝」）
	if r.URL.Query().Get("group_by") == "entity" {
		writeJSON(w, http.StatusOK, map[string]any{"groups": companion.GroupCandidatesByEntity(list)})
		return
	}
	if list == nil {
		list = []*store.CompanionCandidate{}
	}
	writeJSON(w, http.StatusOK, list)
}

// ontologyCompanionAgents GET /api/companion/ontologies/{id}/agents（REQ-216：绑定者清单）。
func (s *Server) ontologyCompanionAgents(w http.ResponseWriter, r *http.Request) {
	list, err := s.Companion.Store.ListAgentsByCompanionOntology(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if list == nil {
		list = []*store.Agent{}
	}
	writeJSON(w, http.StatusOK, list)
}

// resetCompanionOntology POST /api/companion/ontologies/{id}/reset（REQ-216 本体级清空）：
// DROP 本体伴生子图 + 清全部绑定 agent 候选与游标；本体资产本身不受影响。
func (s *Server) resetCompanionOntology(w http.ResponseWriter, r *http.Request) {
	ontID := r.PathValue("id")
	if err := s.Companion.ResetOntology(r.Context(), ontID); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"reset": true})
}

// companionBoundOntologies GET /api/companion/bound-ontologies（REQ-216⑦：对话生长徽标数据源）。
func (s *Server) companionBoundOntologies(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListCompanionBoundOntologies()
	if err != nil {
		writeErr(w, err)
		return
	}
	if list == nil {
		list = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ontology_ids": list})
}

// companionGraphOwner 解析伴生图归属（REQ-216：facade 兼容消费——返回 agent 与其绑定本体）。
// agent_id 直传优先；conversation_id 经 conv.AgentID 解析（项目会话无唯一归属 → 400 提示直传）。
func (s *Server) companionGraphOwner(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	convID := r.URL.Query().Get("conversation_id")
	if agentID == "" && convID == "" {
		writeErr(w, errBadRequest("agent_id / conversation_id 至少传一"))
		return
	}
	if agentID == "" {
		conv, err := s.Store.GetConversation(convID)
		if err != nil {
			writeErr(w, err)
			return
		}
		if conv.Scope == "project" || conv.AgentID == nil || *conv.AgentID == "" {
			writeErr(w, errBadRequest("项目会话无唯一所属智能体（多成员各自伴生本体）——请按 agent_id 直传"))
			return
		}
		agentID = *conv.AgentID
	}
	agent, err := s.Store.GetAgent(agentID)
	if err != nil {
		writeErr(w, err)
		return
	}
	ontID := strings.TrimSpace(agent.CompanionOntologyID)
	if ontID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "该智能体未绑定伴生本体"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":    agentID,
		"ontology_id": ontID,
		"graph":       companion.GraphURI(ontID),
	})
}

// errBadRequest 简单 400 错误（与既有 writeErr 语义对齐）。
func errBadRequest(msg string) error {
	return &store.HTTPError{Status: http.StatusBadRequest, Msg: msg}
}
