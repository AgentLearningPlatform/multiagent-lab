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
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
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

	asm := &chat.Assembler{Store: st, Box: box}
	svc := chat.NewService(st, asm)
	srv := api.NewServer(st, box, svc)

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
			fs.ServeHTTP(w, r)
			return
		}
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
