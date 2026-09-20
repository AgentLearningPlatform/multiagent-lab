package api

import (
	"encoding/json"
	"net/http"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/api/sse"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// runConversation 发起一次运行（SSE 流式返回平台事件，方案 §7 统一协议）。
func (s *Server) runConversation(w http.ResponseWriter, r *http.Request) {
	var in chat.RunInput
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	conv, err := s.Store.GetConversation(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	var agent *store.Agent
	if conv.Scope == "agent" {
		if conv.AgentID == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "conversation is not bound to an agent"})
			return
		}
		agent, err = s.Store.GetAgent(*conv.AgentID)
		if err != nil {
			writeErr(w, err)
			return
		}
	} else {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "项目对话运行将在多 Agent 协作里程碑（M4）支持，当前请使用 Agent 直聊"})
		return
	}

	sw, err := sse.NewWriter(w)
	if err != nil {
		writeErr(w, err)
		return
	}
	runID := store.NewID()

	// 元信息事件（供前端校验会话与运行归属）
	_ = sw.Event("meta", map[string]any{"run_id": runID, "conversation_id": conv.ID, "agent_name": agent.Name})

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

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
