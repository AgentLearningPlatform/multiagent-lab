// ontologyd 构建平面服务（方案 04 §3，:8091）。
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/importer"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/llmcreate"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/repo"
	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/rest"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	addr := env("ADDR", ":8091")
	dbPath := env("DB_PATH", "data/ontology.db")
	migrationsDir := env("MIGRATIONS_DIR", "migrations")
	platformURL := env("PLATFORM_URL", "http://127.0.0.1:8080")
	python := env("SIDECAR_PYTHON", "python3")
	script := env("SIDECAR_SCRIPT", "../tools/rdf-sidecar/sidecar.py")

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}
	st, err := repo.Open(dbPath, migrationsDir)
	if err != nil {
		log.Fatalf("打开仓库失败: %v", err)
	}
	sc := &importer.Sidecar{Python: python, Script: script}
	llm := llmcreate.New(platformURL)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	rest.New(st, sc, llm).Mount(mux)

	log.Printf("[ontologyd] 构建平面监听 %s (db=%s, platform=%s, sidecar=%s %s)", addr, dbPath, platformURL, python, script)
	log.Fatal(http.ListenAndServe(addr, mux))
}
