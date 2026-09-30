// 主平台 backend 入口：装配依赖并启动 HTTP 服务（默认 :8080）。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/api"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/connector"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/inference"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kg"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/ontology"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/skill"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

func main() {
	addr := getenv("ADDR", ":8080")
	dbPath := getenv("DB_PATH", "./data/platform.db")
	keyFile := getenv("SECRET_KEY_FILE", "./data/.secret")

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	box, err := secrets.LoadKeyFile(keyFile)
	if err != nil {
		log.Fatalf("load secret key: %v", err)
	}

	// 工具注册表（M5，方案 §6.8）：内置工具启动时登记
	reg := tool.NewRegistry()
	if err := tool.RegisterBuiltin(reg); err != nil {
		log.Fatalf("register builtin tools: %v", err)
	}

	asm := &chat.Assembler{
		Store:       st,
		Box:         box,
		Tools:       reg,
		Composer:    &skill.Composer{Store: st},              // M9：技能注入
		Ontology:    ontology.NewService(),                   // M8：本体对接（facade/双反代/guide）
		FilesRoot:   getenv("FILES_ROOT", "./data/projects"), // M11：项目文件根目录
		CheckPoints: chat.NewStoreCheckPointStore(st),       // REQ-204/M39 C1：SQLite 持久化（重启后挂起中断可恢复）
	}
	// 知识库服务（M6，§6.9）：向量后端按 KB_VECTOR_BACKEND（qdrant|sqlite），Qdrant 走 REST（QDRANT_URL）
	kbSvc, err := kb.NewService(st, box,
		getenv("KB_VECTOR_BACKEND", "qdrant"),
		getenv("QDRANT_URL", "http://127.0.0.1:6333"))
	if err != nil {
		log.Fatalf("init knowledge base service: %v", err)
	}
	// D-O15/REQ-110：KG 自研抽取器注入（REQ-98 LLM 能力代理主路径 + 规则抽取回退，零外部进程；
	// KG_LLM_CONN_ID 可选指定模型连接，缺省走默认 chat 连接）
	// M36/KB-6③：本体约束抽取词表注入（I3 裁定：构建平面 spec 只读投影；构建平面不可达时降级自由抽取）。
	ontoSvc := ontology.NewService()
	kbSvc.SetKGExtractor((&kg.Extractor{
		Store:  st,
		Box:    box,
		ConnID: getenv("KG_LLM_CONN_ID", ""),
		OntoVocab: func(ctx context.Context, ontologyID string) (*kg.OntoVocab, error) {
			v, err := ontoSvc.FetchSpecVocab(ctx, ontologyID)
			if err != nil {
				return nil, err
			}
			return &kg.OntoVocab{Name: v.Name, Concepts: v.Concepts, Relations: v.Relations}, nil
		},
	}).ExtractForDoc)

	svc := chat.NewService(st, asm, kbSvc)
	// REQ-192/M32：平台助手配置单源归一引导——builtin 行 instruction 空时写入当前默认基座
	//（一次性移植 assistant_config 存量微调），此后 agent 内置行即配置单源、assistant_config 退役。
	if err := st.EnsureBuiltinAssistantInstruction(api.AssistantDefaultPrompt); err != nil {
		log.Printf("[backend] ensure builtin assistant instruction: %v", err)
	}
	// REQ-213：平台助手工具面归一引导——内置行 tools 幂等并入 L0+L1 基座八工具（迁移 030 存量修正的兜底自愈）。
	if err := st.EnsureAssistantTools(); err != nil {
		log.Printf("[backend] ensure assistant tools: %v", err)
	}
	svc.Inference = inference.NewRegistry() // M13/D-O13 §6.16：推理后端注册表（eino-adk + 外部 CLI）
	srv := api.NewServer(st, box, svc, reg, kbSvc, asm.Ontology, dbPath, getenv("DOCS_ROOT", "../docs"), getenv("RESEARCH_ROOT", "../research"), getenv("KNOWLEDGE_ROOT", "../platform-knowledge"))
	// REQ-186 阶段二：平台助手 L0 只读工具面（doc_read/列表×3/查配置；写类工具不开放）
	if err := chat.RegisterAssistantTools(reg, chat.AssistantDeps{Store: st, DocsRoot: getenv("DOCS_ROOT", "../docs"), ResearchRoot: getenv("RESEARCH_ROOT", "../research"), KnowledgeRoot: getenv("KNOWLEDGE_ROOT", "../platform-knowledge"), DefaultPrompt: api.AssistantDefaultPrompt}); err != nil {
		log.Printf("[backend] assistant tools register: %v", err)
	}
	// REQ-186 阶段一/三（M-O14 流水线）：平台知识 KB 化工具（sync/search_platform_kb）与 L1 提案工具（propose_assistant_config 两段式）
	if err := chat.RegisterAssistantL1Tools(reg, chat.AssistantDeps{Store: st, KnowledgeRoot: getenv("KNOWLEDGE_ROOT", "../platform-knowledge")}, kbSvc); err != nil {
		log.Printf("[backend] assistant L1 tools register: %v", err)
	}
	// M10 §6.3 + REQ-191/M31：沙箱执行后端动态装配——运行方式（进程内嵌/docker/k8s/auto）与
	// K8s 访问认证集中为「运行环境」配置（DB runtime_settings 覆盖启动期 env，env 为初始值
	// 兜底）；设置页修改后新 Run 生效（resolver 按配置重建后端，auto 探测缓存随之重置），
	// 运行中实例不受影响。inprocess/未配置镜像 = 未启用沙箱（与原 SANDBOX_IMAGE 空语义一致）。
	// 注：Defaults.SandboxMode 存 env SANDBOX_BACKEND 原值（docker|k8s|auto），空时由
	// api.RuntimeEnv.Build 按「SANDBOX_IMAGE 非空即启用，缺省 docker」语义判定——存量零回归。
	platformURL := getenv("PLATFORM_URL_EXTERNAL", "http://host.docker.internal"+addr)
	srv.RuntimeEnv = &api.RuntimeEnv{
		Store:      st,
		TokenIssue: srv.IssueManifestToken,
		Defaults: store.RuntimeSettings{
			SandboxMode:          getenv("SANDBOX_BACKEND", ""),
			SandboxImage:         getenv("SANDBOX_IMAGE", ""),
			SandboxScope:         getenv("SANDBOX_SCOPE", ""),
			DockerBin:            getenv("DOCKER_BIN", ""),
			KubectlBin:           getenv("KUBECTL_BIN", ""),
			K8sKubeconfig:        getenv("SANDBOX_K8S_KUBECONFIG", ""),
			K8sContext:           getenv("SANDBOX_K8S_CONTEXT", ""),
			K8sNamespace:         getenv("SANDBOX_K8S_NAMESPACE", ""),
			K8sEndpointMode:      getenv("SANDBOX_K8S_ENDPOINT_MODE", ""),
			PlatformURLInCluster: getenv("PLATFORM_URL_IN_CLUSTER", ""),
			PlatformURLExternal:  platformURL,
		},
	}
	svc.Runtime = &api.DynamicRuntime{Env: srv.RuntimeEnv}
	if img := srv.RuntimeEnv.Defaults.SandboxImage; img != "" {
		log.Printf("[backend] runtime env resolver enabled: env_mode=%s image=%s（运行方式可在设置页「运行环境」分区动态切换）", getenv("SANDBOX_BACKEND", "(按镜像自动判定)"), img)
	} else {
		log.Printf("[backend] runtime env resolver enabled: sandbox not configured（可在设置页「运行环境」分区启用）")
	}

	// REQ-214/M46：外部连接器启动引导——存量 mcp_servers 挂载幂等迁移为连接器实例+引用
	//（实例名沿用原 name，装配前缀槽位不变=工具名零破坏；mcp_servers 置空即完成）+ oo 预设内置化。
	if n, err := st.MigrateAgentMCPToConnectors(); err != nil {
		log.Printf("[backend] migrate agent mcp_servers to connectors: %v", err)
	} else if n > 0 {
		log.Printf("[backend] REQ-214: 已迁移 %d 个 agent 的存量 MCP 挂载为连接器引用", n)
	}
	if err := st.EnsureBuiltinOpenOntologiesConnector(); err != nil {
		log.Printf("[backend] ensure builtin open-ontologies connector: %v", err)
	}
	// REQ-214/M46 阶段二：平台托管自研 Go MCP 插件服务（进程内嵌、loopback 独立端口）——
	// kubernetes/ssh 连接器经此走标准 MCP 客户端管线，凭据服务端绑定不进 LLM 上下文不进工具参数。
	pluginAddr := getenv("CONNECTOR_PLUGIN_ADDR", "127.0.0.1:8093")
	asm.PluginEndpoint = "http://" + pluginAddr
	plugin := connector.NewPlugin(st, box, srv.RuntimeEnv.Defaults.KubectlBin)
	pluginSrv := &http.Server{Addr: pluginAddr, Handler: plugin.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("[connector-plugin] listening on %s（kubernetes/ssh 连接器运行平面）", pluginAddr)
		if err := pluginSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[connector-plugin] 插件服务启动失败（k8s/ssh 连接器装配将降级告警）: %v", err)
		}
	}()

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           withStatic(cors(accessAuth(srv.Mux))), // accessAuth：REQ-209 远程访问凭证（PLATFORM_TOKEN 未设置时零开销透传）
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("[backend] listening on %s (db=%s)", addr, dbPath)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	// 优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("[backend] shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	_ = pluginSrv.Shutdown(ctx)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// withStatic 若存在 ../web/dist 则托管前端产物（单进程运行整个平台）；SPA fallback 到 index.html。
func withStatic(next http.Handler) http.Handler {
	dist := getenv("WEB_DIST", "../web/dist")
	abs, err := filepath.Abs(dist)
	exists := err == nil
	if exists {
		if _, err := os.Stat(abs); err != nil {
			exists = false
		}
	}
	if !exists {
		return next
	}
	fs := http.FileServer(http.Dir(abs))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// REQ-131/M18：/mcp（Agent 对外 MCP 服务化）与 /api、/healthz 同属 API 面，绕过静态托管
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/healthz") || strings.HasPrefix(r.URL.Path, "/mcp") {
			next.ServeHTTP(w, r)
			return
		}
		p := filepath.Join(abs, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			// 入口页不缓存，避免前端更新后浏览器仍引用旧产物；assets 文件名带 hash 可缓存
			if strings.HasSuffix(r.URL.Path, "index.html") {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fs.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(abs, "index.html"))
	})
}

// cors 允许本地前端 dev server 跨域（学习平台，本地使用）。
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
