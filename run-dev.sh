#!/usr/bin/env bash
# eino-multiagent-lab 一键启动（Linux / macOS / 云端沙箱）
# 用法: ./run-dev.sh [PORT]
#   默认端口 8080；前端已由 backend 静态托管，浏览器访问 http://localhost:8080
#   同时启动本体侧后端：ontology-service(:8091) + runtime-manager(:8090)
#   D-O15/REQ-110：KG 抽取/消费/审计自研内置 backend，零 Python venv 依赖
#   （semantica worker 已归档休眠；python3/rdflib 仅本体导入导出仍在用）
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
# oxigraph 引擎（0.5+ 二进制改名 oxigraph；engine 适配器用 load/serve 子命令）
if command -v oxigraph >/dev/null 2>&1; then
  OXIGRAPH_BIN_CMD="oxigraph"
elif [ -x tools/bin/oxigraph ]; then
  OXIGRAPH_BIN_CMD="$PWD/tools/bin/oxigraph"
  echo "[run-dev] 使用本地引擎二进制: $OXIGRAPH_BIN_CMD"
elif [ -x data/bin/oxigraph_server ]; then
  OXIGRAPH_BIN_CMD="$PWD/data/bin/oxigraph_server"   # 旧版二进制名（≤0.3）
  echo "[run-dev] 使用本地引擎二进制(旧版命名): $OXIGRAPH_BIN_CMD"
else
  # REQ-146：平台自适应安装提示（此前写死 linux 资产，macOS 照抄会得到不可用的 ELF；
  # 且提示含相对路径，从非仓库根目录执行会散落 web/tools 等目录）
  case "$(uname -s)-$(uname -m)" in
    Darwin-x86_64) OXI_ASSET="oxigraph_v0.5.11_x86_64_apple" ;;
    Darwin-arm64)  OXI_ASSET="oxigraph_v0.5.11_aarch64_apple" ;;
    Linux-x86_64)  OXI_ASSET="oxigraph_v0.5.11_x86_64_linux_gnu" ;;
    Linux-aarch64) OXI_ASSET="oxigraph_v0.5.11_aarch64_linux_gnu" ;;
    *) OXI_ASSET="" ;;
  esac
  echo "[run-dev] 警告: 未找到 oxigraph 引擎，运行方案启动(start)不可用。两种安装方式:" >&2
  echo "[run-dev]   ① 推荐：本体模块「运行」页 → 引擎未安装 Alert → 「一键下载安装」（自动按平台选择官方 release，写入 data/bin 即时生效）" >&2
  if [ -n "$OXI_ASSET" ]; then
    echo "[run-dev]   ② 手动（须在仓库根目录执行，当前平台资产 $OXI_ASSET）:" >&2
    echo "[run-dev]      mkdir -p tools/bin && curl -fsSL https://github.com/oxigraph/oxigraph/releases/download/v0.5.11/$OXI_ASSET -o tools/bin/oxigraph && chmod +x tools/bin/oxigraph" >&2
  else
    echo "[run-dev]   ② 手动：当前平台无预编译资产，参考 https://github.com/oxigraph/oxigraph/releases" >&2
  fi
  echo "[run-dev]   （详见 docs/13 §2）" >&2
fi
if [ -n "${OXIGRAPH_BIN_CMD:-}" ]; then
  export OXIGRAPH_BIN="$OXIGRAPH_BIN_CMD"
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

# ---- KG 消费/审计（D-O15/REQ-110）：自研内置于 backend，无独立进程 ----
# semantica worker 已归档休眠（tools/semantica-worker/ 代码保留、不进启动链路），一条命令启动零 Python venv 依赖。

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

echo "[run-dev] 启动 backend: http://localhost:${PORT}（本体页面经反代对接 :8091/:8090；KG/审计自研内置，D-O15）"
cd backend
ADDR=":${PORT}" go run ./cmd/backend
