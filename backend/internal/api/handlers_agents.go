package api

import (
	"net/http"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/runtime"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/inference"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// normalizeInferenceBackend M13 §6.16：推理后端归一（空 = eino-adk 自研默认）+ 合法性校验。
// PUT 为全量更新（老客户端不带该字段时置空），置空一律归一为默认，不产生破坏。
func (s *Server) normalizeInferenceBackend(a *store.Agent) error {
	a.InferenceBackend = strings.TrimSpace(a.InferenceBackend)
	switch a.InferenceBackend {
	case "", inference.DefaultBackend:
		a.InferenceBackend = inference.DefaultBackend
	default:
		if s.Chat == nil || s.Chat.Inference == nil || !s.Chat.Inference.Known(a.InferenceBackend) {
			return &store.HTTPError{Status: http.StatusBadRequest, Msg: "未知推理后端: " + a.InferenceBackend}
		}
	}
	return nil
}

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
	// REQ-131/M18：mcp_serve 开启时自动生成 Token 由 normalizeMcpServe 处理（见 updateAgent 同款逻辑）
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
	if err := s.normalizeInferenceBackend(&a); err != nil {
		writeErr(w, err)
		return
	}
	if a.McpServe.Enabled && a.McpServe.Token == "" {
		a.McpServe.Token = RandToken()
	}
	created, err := s.Store.CreateAgent(&a)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.mcpSync()
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
	if err := s.normalizeInferenceBackend(&a); err != nil {
		writeErr(w, err)
		return
	}
	if prev, perr := s.Store.GetAgent(a.ID); perr == nil && prev != nil {
		if s.normalizeMcpServe(&a, *prev) {
			defer s.mcpSync()
		}
	} else if a.McpServe.Enabled && a.McpServe.Token == "" {
		a.McpServe.Token = RandToken()
		defer s.mcpSync()
	}
	updated, err := s.Store.UpdateAgent(&a)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mcpSync()
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
	if !normalizeProjectLocalDir(w, &p) {
		return
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
	if !normalizeProjectLocalDir(w, &p) {
		return
	}
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

// ---- M10/10b：沙箱生命周期（状态/启动/停止，per Agent） ----

// sandboxAvailable 沙箱后端是否已启用（SANDBOX_IMAGE 配置即启用）。
func (s *Server) sandboxAvailable() bool {
	return s.Chat != nil && s.Chat.Runtime != nil
}

// sandboxStatus GET /api/agents/{id}/sandbox：容器状态 + 资源限制 + 端点。
func (s *Server) sandboxStatus(w http.ResponseWriter, r *http.Request) {
	if !s.sandboxAvailable() {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	id := r.PathValue("id")
	agent, err := s.Store.GetAgent(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	st, err := s.Chat.Runtime.Status(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": true, "state": st.State, "detail": st.Detail,
		"memory": agent.SandboxMemory, "cpus": agent.SandboxCPUs,
	})
}

// sandboxStart POST /api/agents/{id}/sandbox/start：拉起（或复用）agentd 容器。
func (s *Server) sandboxStart(w http.ResponseWriter, r *http.Request) {
	if !s.sandboxAvailable() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "沙箱后端未启用（需配置 SANDBOX_IMAGE）"})
		return
	}
	id := r.PathValue("id")
	agent, err := s.Store.GetAgent(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if agent.RuntimeBackend != "docker" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "该智能体运行后端不是 docker"})
		return
	}
	ep, err := s.Chat.Runtime.Start(r.Context(), runtime.StartSpec{AgentID: id, Memory: agent.SandboxMemory, CPUs: agent.SandboxCPUs})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"endpoint": ep.URL})
}

// sandboxStop POST /api/agents/{id}/sandbox/stop：停止并移除容器。
func (s *Server) sandboxStop(w http.ResponseWriter, r *http.Request) {
	if !s.sandboxAvailable() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "沙箱后端未启用（需配置 SANDBOX_IMAGE）"})
		return
	}
	if err := s.Chat.Runtime.Stop(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"stopped": r.PathValue("id")})
}
