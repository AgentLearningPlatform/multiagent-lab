#!/usr/bin/env bash
# eino-multiagent-lab 一键启动（Linux / macOS / 云端沙箱）
# 用法: ./run-dev.sh [PORT]
#   默认端口 8080；前端已由 backend 静态托管，浏览器访问 http://localhost:8080
#   同时启动本体侧后端：ontology-service(:8091) + runtime-manager(:8090)
set -e
cd "$(dirname "$0")"

PORT="${1:-8080}"

if ! command -v go >/dev/null 2>&1; then
  echo "[run-dev] 未找到 go，请先安装 Go 1.25+（https://go.dev/dl/）" >&2
  exit 1
fi

# 本体侧可选依赖提示（缺了不阻塞：影响的是本体导入/AI草稿/运行方案启动）
if ! command -v python3 >/dev/null 2>&1; then
  echo "[run-dev] 警告: 未找到 python3，本体导入(OWL/TTL)与导出不可用" >&2
elif ! python3 -c "import rdflib" >/dev/null 2>&1; then
  echo "[run-dev] 警告: python3 缺少 rdflib，本体导入(OWL/TTL)与导出不可用（pip install rdflib）" >&2
fi
if command -v oxigraph_server >/dev/null 2>&1; then
  OXIGRAPH_BIN_CMD="oxigraph_server"
elif [ -x data/bin/oxigraph_server ]; then
  OXIGRAPH_BIN_CMD="$PWD/data/bin/oxigraph_server"
  echo "[run-dev] 使用本地引擎二进制: $OXIGRAPH_BIN_CMD"
else
  OXIGRAPH_BIN_CMD="oxigraph_server"
  echo "[run-dev] 警告: 未找到 oxigraph_server，运行方案启动(start)不可用。安装: https://github.com/oxigraph/oxigraph/releases 下载后加入 PATH，或放到 data/bin/oxigraph_server，或设置 OXIGRAPH_BIN（见 docs/04 §4.2）" >&2
fi

# 前端构建：dist 缺失、源码比 dist 新（如 git pull 之后）、或 FORCE_BUILD=1 时执行
need_build=0
if [ ! -f web/dist/index.html ]; then
  need_build=1
elif [ "${FORCE_BUILD:-0}" = "1" ]; then
  need_build=1
elif [ -n "$(find web/src web/index.html web/package.json -newer web/dist/index.html -print -quit 2>/dev/null)" ]; then
  need_build=1
fi
if [ "$need_build" = "1" ]; then
  echo "[run-dev] 构建前端..."
  (cd web && npm install --no-audit --no-fund && npm run build)
fi

# ---- 本体侧后端（构建平面 + 运行平面）----
mkdir -p data/bin data/engines data/engine_logs
echo "[run-dev] 构建本体侧服务..."
(cd ontology-service && go build -o ../data/bin/ontologyd ./cmd/ontologyd)
(cd runtime-manager && go build -o ../data/bin/runtimed ./cmd/runtimed)

echo "[run-dev] 启动 ontology-service: http://localhost:8091"
ADDR=":8091" DB_PATH=data/ontology.db MIGRATIONS_DIR=ontology-service/migrations \
  SIDECAR_SCRIPT="$PWD/tools/rdf-sidecar/sidecar.py" \
  data/bin/ontologyd >data/ontology-service.log 2>&1 &
ONT_PID=$!

echo "[run-dev] 启动 runtime-manager: http://localhost:8090"
ADDR=":8090" DB_PATH=data/runtime.db MIGRATIONS_DIR=runtime-manager/migrations \
  BUILD_SVC_URL="http://127.0.0.1:8091" OXIGRAPH_BIN="$OXIGRAPH_BIN_CMD" \
  ENGINE_DATA_DIR=data/engines ENGINE_LOG_DIR=data/engine_logs \
  data/bin/runtimed >data/runtime-manager.log 2>&1 &
RT_PID=$!

cleanup() {
  kill "$ONT_PID" "$RT_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# 等本体侧就绪（最多 ~10s，失败不阻塞主平台启动，仅提示）
for i in $(seq 1 20); do
  ok=1
  curl -sf "http://127.0.0.1:8091/healthz" >/dev/null 2>&1 || ok=0
  curl -sf "http://127.0.0.1:8090/healthz" >/dev/null 2>&1 || ok=0
  [ "$ok" = "1" ] && break
  sleep 0.5
done
if [ "$ok" != "1" ]; then
  echo "[run-dev] 警告: 本体侧服务未完全就绪（详情见 data/ontology-service.log、data/runtime-manager.log）" >&2
fi

echo "[run-dev] 启动 backend: http://localhost:${PORT}（本体页面经反代对接 :8091/:8090）"
cd backend
ADDR=":${PORT}" go run ./cmd/backend
