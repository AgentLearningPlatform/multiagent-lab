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
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/inference"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kg"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/ontology"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/runtime"
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
		Store:     st,
		Box:       box,
		Tools:     reg,
		Composer:  &skill.Composer{Store: st},              // M9：技能注入
		Ontology:  ontology.NewService(),                   // M8：本体对接（facade/双反代/guide）
		FilesRoot: getenv("FILES_ROOT", "./data/projects"), // M11：项目文件根目录
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
	kbSvc.SetKGExtractor((&kg.Extractor{Store: st, Box: box, ConnID: getenv("KG_LLM_CONN_ID", "")}).ExtractForDoc)

	svc := chat.NewService(st, asm, kbSvc)
	svc.Inference = inference.NewRegistry() // M13/D-O13 §6.16：推理后端注册表（eino-adk + 外部 CLI）
	srv := api.NewServer(st, box, svc, reg, kbSvc, asm.Ontology)
	// M10 §6.3：Docker 沙箱执行后端（SANDBOX_IMAGE 配置即启用；PLATFORM_URL_EXTERNAL 为容器内回访主平台地址）
	if img := getenv("SANDBOX_IMAGE", ""); img != "" {
		platformURL := getenv("PLATFORM_URL_EXTERNAL", "http://host.docker.internal"+addr)
		svc.Runtime = &runtime.DockerBackend{
			Image:       img,
			PlatformURL: platformURL,
			TokenIssue:  srv.IssueManifestToken,
		}
		log.Printf("[backend] docker sandbox backend enabled: image=%s platform_url=%s", img, platformURL)
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           withStatic(cors(srv.Mux)),
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
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/healthz") {
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
