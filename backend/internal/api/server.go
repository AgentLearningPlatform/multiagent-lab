// Package api 提供主平台 REST API 与 SSE。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/ontology"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

// Server 聚合依赖并持有路由。
type Server struct {
	Store     *store.Store
	Box       *secrets.Box
	Chat      *chat.Service
	Tools     *tool.Registry
	KB        *kb.Service
	Ontology  *ontology.Service // M8：本体对接（反代/facade 探测）
	FilesRoot string            // M11：项目文件根目录（上传/下载落盘）
	Mux       *http.ServeMux
}

// NewServer 构造并注册全部路由。
func NewServer(st *store.Store, box *secrets.Box, chatSvc *chat.Service, tools *tool.Registry, kbSvc *kb.Service, onto *ontology.Service) *Server {
	s := &Server{Store: st, Box: box, Chat: chatSvc, Tools: tools, KB: kbSvc, Ontology: onto, Mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	m := s.Mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.healthz(r.Context()))
	})

	// Agents
	m.HandleFunc("GET /api/agents", s.listAgents)
	// M10 §6.3：沙箱配置下发（内部端点，一次性 token）
	m.HandleFunc("GET /api/internal/agents/{id}/manifest", s.getManifest)
	// M11 §6.13：项目文件（上传/列表/下载）
	m.HandleFunc("GET /api/projects/{id}/files", s.listProjectFiles)
	m.HandleFunc("POST /api/projects/{id}/files", s.uploadProjectFile)
	m.HandleFunc("GET /api/projects/{id}/files/{fid}/content", s.downloadProjectFile)
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
	m.HandleFunc("POST /api/model-connections/{id}/list-models", s.listConnectionModels)

	// 使用统计（按 model|agent|project 聚合 run_event）
	m.HandleFunc("GET /api/stats/usage", s.usageStats)

	// 工具注册表（REQ-24 工具勾选）
	m.HandleFunc("GET /api/tools", s.listTools)

	// 技能库（M7，§6.12）
	// Knowledge base（M6，§450）
	m.HandleFunc("GET /api/kb", s.listKB)
	m.HandleFunc("POST /api/kb", s.createKB)
	m.HandleFunc("GET /api/kb/{id}", s.getKB)
	m.HandleFunc("PUT /api/kb/{id}", s.updateKB)
	m.HandleFunc("DELETE /api/kb/{id}", s.deleteKB)
	m.HandleFunc("GET /api/kb/{id}/docs", s.listKBDocs)
	m.HandleFunc("POST /api/kb/{id}/docs", s.importKBDoc)
	m.HandleFunc("DELETE /api/kb/{id}/docs/{did}", s.deleteKBDoc)
	m.HandleFunc("POST /api/kb/{id}/docs/{did}/reindex", s.reindexKBDoc)
	m.HandleFunc("POST /api/kb/{id}/search-preview", s.previewKBSearch)

	m.HandleFunc("GET /api/skills", s.listSkills)
	m.HandleFunc("POST /api/skills", s.createSkill)
	m.HandleFunc("GET /api/skills/{id}", s.getSkill)
	m.HandleFunc("PUT /api/skills/{id}", s.updateSkill)
	m.HandleFunc("GET /api/skills/{id}/preview", s.previewSkill)
	m.HandleFunc("DELETE /api/skills/{id}", s.deleteSkill)

	// 本体对接（M8 §6.10）：模型能力代理 + 双反向代理（同源透传免跨域）
	m.HandleFunc("POST /api/ontology-llm/generate", s.generateOntologyLLM)
	if s.Ontology != nil {
		m.Handle("/api/ontologies", s.Ontology.BuildProxy()) // → 构建平面 BUILD_SVC_URL(:8091)
		m.Handle("/api/ontologies/", s.Ontology.BuildProxy())
		m.Handle("/api/runtime-profiles", s.Ontology.RuntimeProxy()) // → 运行平面 RUNTIME_MGR_URL(:8090)
		m.Handle("/api/runtime-profiles/", s.Ontology.RuntimeProxy())
		// Semantica 独立栏（§4.9 D-O10）：剥离前缀反代到 worker，:8093
		m.Handle("/api/semantica", s.Ontology.SemanticaProxy()) // → SEMANTICA_WORKER_URL(:8093)
		m.Handle("/api/semantica/", s.Ontology.SemanticaProxy())
		// Semantica Explorer iframe 嵌入（§4.9.2/§4.9.4）：/semantica/explorer/* → worker /explorer/*（剥离 X-Frame-Options）
		m.Handle("/semantica/explorer", s.Ontology.SemanticaExplorerProxy())
		m.Handle("/semantica/explorer/", s.Ontology.SemanticaExplorerProxy())
	}
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

// healthz 汇总后端依赖状态（§6.10/§8：模型默认连接、本体 facade、知识库向量后端可达性）。
func (s *Server) healthz(ctx context.Context) map[string]any {
	out := map[string]any{"status": "ok"}

	if def, err := s.Store.GetDefaultConnection("chat"); err != nil {
		out["model_default_conn"] = "error"
	} else if def == nil {
		out["model_default_conn"] = "not_configured"
	} else {
		out["model_default_conn"] = "ok"
	}
	if defE, err := s.Store.GetDefaultConnection("embedding"); err != nil {
		out["embedding_default_conn"] = "error"
	} else if defE == nil {
		out["embedding_default_conn"] = "not_configured"
	} else {
		out["embedding_default_conn"] = "ok"
	}

	if s.KB == nil {
		out["knowledge_backend"] = "disabled"
	} else {
		out["knowledge_backend"] = s.KB.Healthz(ctx)
	}

	switch {
	case s.Ontology == nil:
		out["ontology_facade"] = "disabled"
	case s.Ontology.Reachable(ctx):
		out["ontology_facade"] = "ok"
	default:
		out["ontology_facade"] = "unreachable"
	}
	return out
}
