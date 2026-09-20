package api

import (
	"net/http"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---- Agents ----

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.Store.ListAgents()
	if err != nil {
		writeErr(w, err)
		return
	}
	if agents == nil {
		agents = []*store.Agent{}
	}
	writeJSON(w, http.StatusOK, agents)
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	var a store.Agent
	if err := decodeJSON(r, &a); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(a.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if a.MaxIteration <= 0 {
		a.MaxIteration = 25
	}
	if a.RuntimeBackend == "" {
		a.RuntimeBackend = "inprocess"
	}
	created, err := s.Store.CreateAgent(&a)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.GetAgent(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	var a store.Agent
	if err := decodeJSON(r, &a); err != nil {
		writeErr(w, err)
		return
	}
	a.ID = r.PathValue("id")
	updated, err := s.Store.UpdateAgent(&a)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Store.DeleteAgent(id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// ---- Projects ----

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Store.ListProjects()
	if err != nil {
		writeErr(w, err)
		return
	}
	if ps == nil {
		ps = []*store.Project{}
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var p store.Project
	if err := decodeJSON(r, &p); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(p.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if p.CollabMode == "" {
		p.CollabMode = "agent_as_tool"
	}
	if p.WorkflowMode == "" {
		p.WorkflowMode = "free"
	}
	created, err := s.Store.CreateProject(&p)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.GetProject(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	var p store.Project
	if err := decodeJSON(r, &p); err != nil {
		writeErr(w, err)
		return
	}
	p.ID = r.PathValue("id")
	updated, err := s.Store.UpdateProject(&p)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) setProjectAgents(w http.ResponseWriter, r *http.Request) {
	var members []store.ProjectMember
	if err := decodeJSON(r, &members); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Store.SetProjectAgents(r.PathValue("id"), members); err != nil {
		writeErr(w, err)
		return
	}
	p, err := s.Store.GetProject(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Store.DeleteProject(id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}
