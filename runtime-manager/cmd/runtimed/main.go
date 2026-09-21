// runtimed 运行平面服务（方案 04 §4，:8090）：
// 运行方案编排 + Oxigraph 引擎适配 + 统一 MCP facade（/mcp，4 个 onto_* 工具）。
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/engine/oxigraph"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/facade"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/manager"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/rest"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/store"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	addr := env("ADDR", ":8090")
	dbPath := env("DB_PATH", "data/runtime.db")
	migrationsDir := env("MIGRATIONS_DIR", "migrations")
	buildURL := env("BUILD_SVC_URL", "http://127.0.0.1:8091")
	oxigraphBin := env("OXIGRAPH_BIN", "oxigraph_server")
	dataDir := env("ENGINE_DATA_DIR", "data/engines")
	logDir := env("ENGINE_LOG_DIR", "logs")

	if err := os.MkdirAll(dbPath[:len(dbPath)-len(basename(dbPath))], 0o755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}
	st, err := store.Open(dbPath, migrationsDir)
	if err != nil {
		log.Fatalf("打开存储失败: %v", err)
	}
	eng := oxigraph.New(oxigraphBin, dataDir, logDir)
	mg := manager.New(st, eng, buildURL, logDir)

	mux := http.NewServeMux()
	mux.Handle("/mcp", facade.New(st, mg.ProcEndpoint).Mount())
	rest.New(st, mg).Mount(mux)

	log.Printf("[runtimed] 运行平面监听 %s (build=%s, oxigraph=%s)", addr, buildURL, oxigraphBin)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func basename(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
