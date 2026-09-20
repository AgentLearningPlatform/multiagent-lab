// Package api 提供主平台 REST API 与 SSE。
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// Server 聚合依赖并持有路由。
type Server struct {
	Store *store.Store
	Box   *secrets.Box
	Chat  *chat.Service
	Mux   *http.ServeMux
}

// NewServer 构造并注册全部路由。
func NewServer(st *store.Store, box *secrets.Box, chatSvc *chat.Service) *Server {
	s := &Server{Store: st, Box: box, Chat: chatSvc, Mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	m := s.Mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, map[string]string{"status": "ok"}) })

	// Agents
	m.HandleFunc("GET /api/agents", s.listAgents)
	m.HandleFunc("POST /api/agents", s.createAgent)
	m.HandleFunc("GET /api/agents/{id}", s.getAgent)
	m.HandleFunc("PUT /api/agents/{id}", s.updateAgent)
	m.HandleFunc("DELETE /api/agents/{id}", s.deleteAgent)

	// Projects
	m.HandleFunc("GET /api/projects", s.listProjects)
	m.HandleFunc("POST /api/projects", s.createProject)
	m.HandleFunc("GET /api/projects/{id}", s.getProject)
	m.HandleFunc("PUT /api/projects/{id}", s.updateProject)
	m.HandleFunc("PUT /api/projects/{id}/agents", s.setProjectAgents)
	m.HandleFunc("DELETE /api/projects/{id}", s.deleteProject)

	// Conversations
	m.HandleFunc("GET /api/conversations", s.listConversations)
	m.HandleFunc("POST /api/conversations", s.createConversation)
	m.HandleFunc("GET /api/conversations/{id}", s.getConversation)
	m.HandleFunc("PUT /api/conversations/{id}", s.updateConversation)
	m.HandleFunc("DELETE /api/conversations/{id}", s.deleteConversation)
	m.HandleFunc("GET /api/conversations/{id}/messages", s.listMessages)
	m.HandleFunc("GET /api/conversations/{id}/events", s.listEvents)
	m.HandleFunc("POST /api/conversations/{id}/runs", s.runConversation)
	m.HandleFunc("POST /api/conversations/{id}/stop", s.stopConversation)

	// Model connections
	m.HandleFunc("GET /api/model-connections", s.listConnections)
	m.HandleFunc("POST /api/model-connections", s.createConnection)
	m.HandleFunc("GET /api/model-connections/{id}", s.getConnection)
	m.HandleFunc("PUT /api/model-connections/{id}", s.updateConnection)
	m.HandleFunc("DELETE /api/model-connections/{id}", s.deleteConnection)
	m.HandleFunc("PUT /api/model-connections/{id}/default", s.setDefaultConnection)
	m.HandleFunc("POST /api/model-connections/test", s.testConnection)
}

// ---- JSON 工具 ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[api] encode response: %v", err)
	}
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return &store.HTTPError{Status: http.StatusBadRequest, Msg: "invalid JSON body: " + err.Error()}
	}
	return nil
}

func writeErr(w http.ResponseWriter, err error) {
	var he *store.HTTPError
	if errors.As(err, &he) {
		writeJSON(w, he.Status, map[string]string{"error": he.Msg})
		return
	}
	var mnc *chat.ModelNotConfiguredError
	if errors.As(err, &mnc) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": mnc.Error()})
		return
	}
	log.Printf("[api] internal error: %v", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
