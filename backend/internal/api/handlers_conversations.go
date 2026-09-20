package api

import (
	"net/http"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---- Conversations ----

func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	convs, err := s.Store.ListConversations(store.ConversationFilter{
		Scope:     q.Get("scope"),
		AgentID:   q.Get("agent_id"),
		ProjectID: q.Get("project_id"),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	if convs == nil {
		convs = []*store.Conversation{}
	}
	writeJSON(w, http.StatusOK, convs)
}

func (s *Server) createConversation(w http.ResponseWriter, r *http.Request) {
	var c store.Conversation
	if err := decodeJSON(r, &c); err != nil {
		writeErr(w, err)
		return
	}
	if c.Scope != "agent" && c.Scope != "project" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scope must be agent|project"})
		return
	}
	created, err := s.Store.CreateConversation(&c)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) getConversation(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.GetConversation(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) updateConversation(w http.ResponseWriter, r *http.Request) {
	var c store.Conversation
	if err := decodeJSON(r, &c); err != nil {
		writeErr(w, err)
		return
	}
	c.ID = r.PathValue("id")
	updated, err := s.Store.UpdateConversation(&c)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Store.DeleteConversation(id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	msgs, err := s.Store.ListMessages(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if msgs == nil {
		msgs = []*store.Message{}
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.Store.ListEvents(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if events == nil {
		events = []*store.RunEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}
