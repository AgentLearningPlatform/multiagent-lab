// Package api 提供主平台 REST API 与 SSE。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/companion"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kg"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/ontobuild"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/ontology"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

// Server 聚合依赖并持有路由。
type Server struct {
	Store         *store.Store
	Box           *secrets.Box
	Chat          *chat.Service
	Tools         *tool.Registry
	KB            *kb.Service
	Ontology      *ontology.Service  // M8：本体对接（反代/facade 探测）
	OntoBuild     *ontobuild.Service // O13：由知识库构建本体（KB→spec 编排）
	FilesRoot     string             // M11：项目文件根目录（上传/下载落盘）
	DBPath        string             // REQ-113：SQLite 文件路径（数据与安全概览展示 DB 体积）
	DocsRoot      string             // REQ-140：内部方案文档根目录（只读查看）
	ResearchRoot  string             // REQ-150：research/ 立项依据层根目录（只读查看，2026-09-25 扩展）
	KnowledgeRoot string             // REQ-161：platform-knowledge/ 平台知识根目录（只读查看，2026-09-25 扩展）
	Companion     *companion.Service // REQ-170/M28：伴生本体旁路管线（Run/Resume 收尾触发，低侵入）
	sched         *scheduler         // REQ-204/M39 C5：对话级定时续跑（进程内，重启失效——诚实边界）
	RuntimeEnv    *RuntimeEnv        // REQ-191/M31：运行环境统一配置（DB 覆盖 env，动态沙箱后端解析）
	Mux           *http.ServeMux
	mcpMu         sync.Mutex   // REQ-131/M18：/mcp 工具表缓存锁
	mcpHTTP       http.Handler // REQ-131/M18：Streamable HTTP handler（mcp_serve 变更后重建）
}

// ooProxy M8.5（REQ-100/M9 双轨集成）：/api/oo/* 反代 open-ontologies serve-http（默认 :8092）。
// 用途：①平台同源 MCP 面（Agent 挂载可指向 /api/oo/mcp，免跨端口）；②oo 未来 Web UI 直接嵌入。
// oo 不可达时由反代返回 502（诚实降级，不阻断平台其余功能）。
func (s *Server) ooProxy() http.Handler {
	target := os.Getenv("OO_SVC_URL")
	if target == "" {
		target = "http://127.0.0.1:8092"
	}
	u, err := url.Parse(target)
	if err != nil {
		u, _ = url.Parse("http://127.0.0.1:8092")
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	orig := proxy.Director
	proxy.Director = func(req *http.Request) {
		orig(req)
		req.Host = u.Host
		req.URL.Path = strings.TrimPrefix(req.URL.Path, "/api/oo")
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "open-ontologies 服务不可达（OO_SVC_URL=" + target + "）: " + err.Error()})
	}
	return proxy
}

// NewServer 构造并注册全部路由。
func NewServer(st *store.Store, box *secrets.Box, chatSvc *chat.Service, tools *tool.Registry, kbSvc *kb.Service, onto *ontology.Service, dbPath, docsRoot, researchRoot, knowledgeRoot string) *Server {
	comp := companion.NewService(st, box, nil)
	chatSvc.Companion = comp // REQ-170 P2「KG 检索源并入」：伴生图检索源经接口反转注入 chat（companion→chat 包环约束）
	// REQ-216/M47：存量迁移幂等一次性——①旧 bool 绑定回填（创建空本体）→ ②旧 :9199 agent 图
	// 跨实例复制到宿主方案引擎本体伴生子图 → ③conv 图收敛兜底（companion_meta 标记防重复；
	// 构建平面/引擎不在位时日志告警、下次启动重试，不阻塞启动主链路）
	if err := comp.MigrateAgentGraphsToOntology(context.Background()); err != nil {
		log.Printf("[companion] REQ-216 迁移未完成（不影响启动）: %v", err)
	}
	chatSvc.Community = &kg.Summarizer{Store: st, Box: box} // KB-5③：全局问答社区摘要源（connID 按库经接口参数传入）
	s := &Server{Store: st, Box: box, Chat: chatSvc, Tools: tools, KB: kbSvc, Ontology: onto, OntoBuild: ontobuild.NewService(st, box, kbSvc), DBPath: dbPath, DocsRoot: docsRoot, ResearchRoot: researchRoot, KnowledgeRoot: knowledgeRoot, Companion: comp, Mux: http.NewServeMux()}
	s.sched = newScheduler(s)
	s.routes()
	return s
}

func (s *Server) routes() {
	m := s.Mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.healthz(r.Context()))
	})

	// Agents
	// REQ-131/M18：Agent 对外 MCP 服务化——/mcp Streamable HTTP + 管理端点
	m.Handle("/mcp", s.mcpAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 懒重建：mcp_serve 变更后 invalidateMCP() 生效，工具表按最新 enabled Agent 组装
		s.mcpHandler().ServeHTTP(w, r)
	})))
	m.HandleFunc("GET /api/agents/{id}/mcp-serve", s.getAgentMcpServe)
	m.HandleFunc("POST /api/agents/{id}/mcp-serve/reset", s.resetAgentMcpToken)

	m.HandleFunc("GET /api/agents", s.listAgents)
	// M10 §6.3：沙箱配置下发（内部端点，一次性 token）
	m.HandleFunc("GET /api/internal/agents/{id}/manifest", s.getManifest)
	// M11 §6.13：项目文件（上传/列表/下载）
	m.HandleFunc("GET /api/projects/{id}/files", s.listProjectFiles)
	m.HandleFunc("POST /api/projects/{id}/files", s.uploadProjectFile)
	m.HandleFunc("GET /api/projects/{id}/files/{fid}/content", s.downloadProjectFile)
	// M12 REQ-101/102：项目绑定本地目录 + 目录文件视图
	m.HandleFunc("POST /api/projects/validate-dir", s.validateProjectDir)
	// REQ-133：同机部署系统目录选择对话框（远程部署由前端降级手输）
	m.HandleFunc("POST /api/projects/pick-dir", s.pickProjectDir)
	m.HandleFunc("GET /api/projects/{id}/dir-files", s.listDirFiles)
	m.HandleFunc("GET /api/projects/{id}/dir-file", s.getDirFile)
	// M12 REQ-102 深度版：Git 视图（提交历史 / 分支 / 变更明细）
	m.HandleFunc("GET /api/projects/{id}/git-log", s.gitProjectLog)
	m.HandleFunc("GET /api/projects/{id}/git-branches", s.gitProjectBranches)
	m.HandleFunc("GET /api/projects/{id}/git-commit-files", s.gitProjectCommitFiles)
	m.HandleFunc("GET /api/projects/{id}/git-commit-patch", s.gitProjectCommitPatch)
	m.HandleFunc("GET /api/projects/{id}/git-working", s.gitProjectWorking)
	m.HandleFunc("POST /api/agents", s.createAgent)
	m.HandleFunc("GET /api/agents/{id}", s.getAgent)
	m.HandleFunc("PUT /api/agents/{id}", s.updateAgent)
	m.HandleFunc("DELETE /api/agents/{id}", s.deleteAgent)

	// REQ-170/M28：伴生本体（候选确认流 API 先行；绑定经 REQ-216 companion_ontology_id）
	m.HandleFunc("GET /api/companion/candidates", s.listCompanionCandidates)
	m.HandleFunc("POST /api/companion/candidates/{id}/confirm", s.confirmCompanionCandidate)
	m.HandleFunc("POST /api/companion/candidates/{id}/reject", s.rejectCompanionCandidate)
	m.HandleFunc("GET /api/companion/status", s.companionStatus)
	m.HandleFunc("GET /api/companion/graph", s.companionGraph)
	m.HandleFunc("POST /api/companion/agents/{id}/bind", s.bindCompanionAgent)
	m.HandleFunc("POST /api/companion/agents/{id}/reset", s.resetCompanionAgent)
	m.HandleFunc("POST /api/companion/agents/{id}/reseed", s.reseedCompanionAgent)
	m.HandleFunc("GET /api/companion/ontologies/{id}/candidates", s.ontologyCompanionCandidates)
	m.HandleFunc("GET /api/companion/ontologies/{id}/agents", s.ontologyCompanionAgents)
	m.HandleFunc("POST /api/companion/ontologies/{id}/reset", s.resetCompanionOntology)
	m.HandleFunc("GET /api/companion/bound-ontologies", s.companionBoundOntologies)
	m.HandleFunc("GET /api/companion/graph-owner", s.companionGraphOwner)

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
	m.HandleFunc("GET /api/conversations/{id}/export", s.exportConversation) // REQ-113①：对话导出 Markdown
	m.HandleFunc("GET /api/docs/read", s.docRead)
	m.HandleFunc("GET /api/assistant/config", s.assistantConfigGet) // M27/REQ-166
	m.HandleFunc("PUT /api/assistant/config", s.assistantConfigPut)
	m.HandleFunc("GET /api/assistant/proposal", s.assistantProposalGet)   // M-O14 阶段三：L1 提案两段式（查看）
	m.HandleFunc("POST /api/assistant/proposal/{id}/apply", s.assistantProposalApply)   // 确认应用
	m.HandleFunc("POST /api/assistant/proposal/{id}/discard", s.assistantProposalDiscard) // 忽略
	m.Handle("/api/oo/", s.ooProxy()) // M8.5：oo 双轨同源反代（MCP 面 /api/oo/mcp + 工作台资源）
	m.HandleFunc("POST /api/assistant/optimize", s.assistantOptimize)              // M27/REQ-167                                  // REQ-140：内部方案文档只读查看
	m.HandleFunc("POST /api/conversations/{id}/auto-name", s.autoNameConversation) // REQ-136：对话自动命名
	// M11 收尾：中断恢复（ask_human 答复定向续跑）
	m.HandleFunc("POST /api/conversations/{id}/resume", s.resumeConversation)

	// Model connections
	m.HandleFunc("GET /api/model-connections", s.listConnections)
	m.HandleFunc("POST /api/model-connections", s.createConnection)
	m.HandleFunc("GET /api/model-connections/{id}", s.getConnection)
	m.HandleFunc("PUT /api/model-connections/{id}", s.updateConnection)
	m.HandleFunc("DELETE /api/model-connections/{id}", s.deleteConnection)
	m.HandleFunc("PUT /api/model-connections/{id}/default", s.setDefaultConnection)
	m.HandleFunc("POST /api/model-connections/test", s.testConnection)
	m.HandleFunc("POST /api/model-connections/{id}/list-models", s.listConnectionModels)
	// M10/10b：沙箱生命周期（状态/启动/停止，per Agent）
	m.HandleFunc("GET /api/agents/{id}/sandbox", s.sandboxStatus)
	// REQ-218④/M49：智能体文件视图（安全根=agent.work_dir，沿项目 dir-files 先例）
	m.HandleFunc("GET /api/agents/{id}/dir-files", s.listAgentDirFiles)
	m.HandleFunc("GET /api/agents/{id}/dir-file", s.getAgentDirFile)
	m.HandleFunc("POST /api/agents/{id}/sandbox/start", s.sandboxStart)
	m.HandleFunc("POST /api/agents/{id}/sandbox/stop", s.sandboxStop)
	// REQ-191/M31：运行环境统一配置（设置页「运行环境」分区；本体引擎执行方式仍走 /api/runtime-config 反代）
	m.HandleFunc("GET /api/runtime-env", s.runtimeEnvGet)
	m.HandleFunc("PUT /api/runtime-env", s.runtimeEnvPut)
	m.HandleFunc("POST /api/runtime-env/test", s.runtimeEnvTest)
	// REQ-214/M46：外部连接器（设置页「连接器」分区 + 侧板授权勾选数据源；凭据加密落库永不回传）
	m.HandleFunc("GET /api/connectors", s.listConnectors)
	m.HandleFunc("POST /api/connectors", s.createConnector)
	m.HandleFunc("PUT /api/connectors/{id}", s.updateConnector)
	m.HandleFunc("DELETE /api/connectors/{id}", s.deleteConnector)
	m.HandleFunc("POST /api/connectors/{id}/test", s.testConnector)
	m.HandleFunc("POST /api/connectors/preview", s.previewConnector) // REQ-214 P2：创建前预检（不落库）
	// REQ-148 供应商分组：同一供应商可多实例（分组标识与 BaseURL 解耦）+ 别名（展示层）
	m.HandleFunc("GET /api/provider-groups", s.listProviderGroups)
	m.HandleFunc("POST /api/provider-groups", s.createProviderGroup)
	m.HandleFunc("PUT /api/provider-groups/{id}", s.updateProviderGroup)
	// M13/D-O13 §6.16：推理后端探测清单
	m.HandleFunc("GET /api/inference-backends", s.listInferenceBackends)
	m.HandleFunc("POST /api/inference-backends/reprobe", s.reprobeInferenceBackends)

	// 使用统计（按 model|agent|project 聚合 run_event）
	m.HandleFunc("GET /api/stats/storage", s.storageOverview) // REQ-113②：数据量概览
	m.HandleFunc("GET /api/stats/usage", s.usageStats)
	// REQ-204/M39：进度产物人类侧导出 + 对话级定时续跑（进程内调度器，重启失效）
	m.HandleFunc("GET /api/conversations/{id}/todo.md", s.exportTodo)
	m.HandleFunc("POST /api/conversations/{id}/schedule", s.createSchedule)
	m.HandleFunc("DELETE /api/conversations/{id}/schedule", s.deleteSchedule)
	m.HandleFunc("GET /api/schedules", s.listSchedules)

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
	m.HandleFunc("POST /api/kb/{id}/graphrag-search", s.graphragSearchKB) // M14 D-KB4：GraphRAG 子模块直查

	// O13 由知识库构建本体（D-O14/REQ-108，M15）：精确路由压过 /api/ontologies* 反代前缀
	m.HandleFunc("GET /api/kbs/selectable-for-ontology-build", s.selectableForOntologyBuild)
	m.HandleFunc("POST /api/ontologies/build-from-kb", s.buildFromKB)
	m.HandleFunc("POST /api/ontologies/build-from-structured", s.buildFromStructured)
	m.HandleFunc("POST /api/ontologies/kg-to-spec-json", s.kgToSpecJSON)

	// KG 自存 + 消费/审计（D-O15/REQ-110：去-semantica 化，零外部进程）
	m.HandleFunc("GET /api/kg/{kbID}", s.kgRead)
	m.HandleFunc("POST /api/kg/{kbID}/rebuild", s.kgRebuild)
	// M16 阶段一（REQ-127）：图谱浏览与统计
	m.HandleFunc("GET /api/kg/{kbID}/stats", s.kgStats)
	m.HandleFunc("GET /api/kg/{kbID}/entities", s.kgEntitySearch)
	m.HandleFunc("GET /api/kg/{kbID}/neighborhood", s.kgNeighborhood)
	// M16 阶段二（REQ-129）：抽取治理与人工反馈
	m.HandleFunc("POST /api/kg/{kbID}/review", s.kgReview)
	m.HandleFunc("POST /api/kg/{kbID}/merge", s.kgMerge)
	m.HandleFunc("GET /api/kg/{kbID}/quality", s.kgQuality)
	m.HandleFunc("GET /api/kg/{kbID}/merge-suggestions", s.kgMergeSuggestions)
	m.HandleFunc("PUT /api/kg/{kbID}/entity-alias", s.kgEntityAlias) // M36/KB-7②：别名人工标注
	m.HandleFunc("POST /api/kg/{kbID}/communities/rebuild", s.kgCommunitiesRebuild) // M16/REQ-130：社区重建
	m.HandleFunc("GET /api/kg/{kbID}/communities", s.kgCommunities)
	m.HandleFunc("POST /api/kb/{id}/global-search", s.kgGlobalSearch) // REQ-130：全局问答
	m.HandleFunc("GET /api/audit/decisions", s.listDecisions)
	m.HandleFunc("POST /api/audit/decisions", s.createDecision)
	m.HandleFunc("GET /api/audit/decisions/{id}/chain", s.decisionChain)
	m.HandleFunc("GET /api/audit/prov-export", s.provExport)

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
		// REQ-216 增量②b：本体删除伴生拦截（精确路由压过反代前缀——绑定者 409 保护+删除后快照清理）
		m.HandleFunc("DELETE /api/ontologies/{id}", s.deleteOntologyGuard)
		// REQ-103 模式 A：OntoChat 会话/turn/save 全在构建平面 /api/ontochat/*（bugfix：此前漏注册反代，
		// 同源请求命中主后端 404 文本，前端 JSON.parse 报 "Unexpected non-whitespace character after JSON"）
		m.Handle("/api/ontochat", s.Ontology.BuildProxy())
		m.Handle("/api/ontochat/", s.Ontology.BuildProxy())
		// REQ-171/156/157/M-O15：质量门禁（quality check/report）+ 工具链（toolchain 五工具）+
		// LOV 词表搜索 + 导入合并（merge preview/apply）+ 质量门禁开关（quality-config）——构建平面新端点前缀
		m.Handle("/api/ontology/", s.Ontology.BuildProxy())
		// M-O17 重构补遗（工具链配置页 404 修复）：PipelinePane 走 /api/pipelines*（REST 工具链配置，:8091）——
		// REQ-75/76 交付时页面挂本体模块内但平台反代从未覆盖该前缀
		m.Handle("/api/pipelines", s.Ontology.BuildProxy())
		m.Handle("/api/pipelines/", s.Ontology.BuildProxy())
		m.Handle("/api/runtime-profiles", s.Ontology.RuntimeProxy()) // → 运行平面 RUNTIME_MGR_URL(:8090)
		m.Handle("/api/runtime-profiles/", s.Ontology.RuntimeProxy())
		// REQ-179/M-O16：全局运行配置（执行方式）——运行平面单行配置表
		m.Handle("/api/runtime-config", s.Ontology.RuntimeProxy())
		// REQ-146：引擎自检与一键安装（install 为 202 异步任务，轮询 /api/engines 无长连接）
		m.Handle("/api/engines", s.Ontology.RuntimeProxy())
		m.Handle("/api/engines/", s.Ontology.RuntimeProxy())
		// D-O15：/api/semantica* 与 /semantica/explorer* 反代已随「去-semantica 化」移除，
		// 消费/审计改走上方自研 /api/kg、/api/audit 端点（§4.9 反转注记）
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
