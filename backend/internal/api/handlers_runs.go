package api

import (
	"encoding/json"
	"net/http"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/api/sse"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// resolveRunTarget 解析会话的运行目标 Agent（agent 直聊 / 项目协调者），返回会话与智能体。
func (s *Server) resolveRunTarget(r *http.Request) (*store.Conversation, *store.Agent, string, error) {
	conv, err := s.Store.GetConversation(r.PathValue("id"))
	if err != nil {
		return nil, nil, "", err
	}
	if conv.Scope == "agent" {
		if conv.AgentID == nil {
			return nil, nil, "", &store.HTTPError{Status: 400, Msg: "conversation is not bound to an agent"}
		}
		agent, err := s.Store.GetAgent(*conv.AgentID)
		if err != nil {
			return nil, nil, "", err
		}
		return conv, agent, "", nil
	}
	// 项目会话：解析运行成员——主智能体优先，缺省取第一个成员
	if conv.ProjectID == nil || *conv.ProjectID == "" {
		return nil, nil, "", &store.HTTPError{Status: 400, Msg: "conversation is not bound to a project"}
	}
	p, err := s.Store.GetProject(*conv.ProjectID)
	if err != nil {
		return nil, nil, "", err
	}
	agentID := p.Coordinator
	if agentID == "" && len(p.AgentIDs) > 0 {
		agentID = p.AgentIDs[0]
	}
	if agentID == "" {
		return nil, nil, "", &store.HTTPError{Status: 400, Msg: "项目还没有成员智能体，请先在项目配置中添加成员"}
	}
	agent, err := s.Store.GetAgent(agentID)
	if err != nil {
		return nil, nil, "", err
	}
	return conv, agent, p.Name, nil
}

// runConversation 发起一次运行（SSE 流式返回平台事件，方案 §7 统一协议）。
func (s *Server) runConversation(w http.ResponseWriter, r *http.Request) {
	var in chat.RunInput
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	conv, agent, projectName, err := s.resolveRunTarget(r)
	if err != nil {
		writeErr(w, err)
		return
	}

	sw, err := sse.NewWriter(w)
	if err != nil {
		writeErr(w, err)
		return
	}
	runID := store.NewID()

	// 元信息事件（供前端校验会话与运行归属）
	meta := map[string]any{"run_id": runID, "conversation_id": conv.ID, "agent_name": agent.Name}
	if projectName != "" {
		meta["project_name"] = projectName
	}
	_ = sw.Event("meta", meta)

	// 统一事件协议：{type, run_id, ts, data}
	emit := func(ev *chat.Event) {
		if ev == nil {
			return
		}
		_ = sw.Event(ev.Type, ev)
	}

	if _, err := s.Chat.Run(r.Context(), conv, agent, runID, in.Input, emit); err != nil {
		ev := chat.NewErrorEvent(runID, "run_failed", err.Error())
		_ = sw.Event(ev.Type, ev)
	}
}

// stopConversation 停止运行。
func (s *Server) stopConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok := s.Chat.Stop(id)
	writeJSON(w, http.StatusOK, map[string]bool{"stopped": ok})
}

// resumeConversation 恢复挂起的中断（M11 收尾 · 中断恢复）：以用户答复定向续跑，SSE 事件与运行同构。
func (s *Server) resumeConversation(w http.ResponseWriter, r *http.Request) {
	var in chat.RunInput
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	conv, agent, projectName, err := s.resolveRunTarget(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	sw, err := sse.NewWriter(w)
	if err != nil {
		writeErr(w, err)
		return
	}
	runID := store.NewID()
	meta := map[string]any{"run_id": runID, "conversation_id": conv.ID, "agent_name": agent.Name, "resumed": true}
	if projectName != "" {
		meta["project_name"] = projectName
	}
	_ = sw.Event("meta", meta)
	emit := func(ev *chat.Event) {
		if ev == nil {
			return
		}
		_ = sw.Event(ev.Type, ev)
	}
	if _, err := s.Chat.Resume(r.Context(), conv, agent, runID, in.Input, emit); err != nil {
		ev := chat.NewErrorEvent(runID, "resume_failed", err.Error())
		_ = sw.Event(ev.Type, ev)
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
