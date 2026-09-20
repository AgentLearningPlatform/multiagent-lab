#!/usr/bin/env bash
# eino-multiagent-lab 一键启动（Linux / macOS / 云端沙箱）
# 用法: ./run-dev.sh [PORT]
#   默认端口 8080；前端已由 backend 静态托管，浏览器访问 http://localhost:8080
set -e
cd "$(dirname "$0")"

PORT="${1:-8080}"

if ! command -v go >/dev/null 2>&1; then
  echo "[run-dev] 未找到 go，请先安装 Go 1.25+（https://go.dev/dl/）" >&2
  exit 1
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

echo "[run-dev] 启动 backend: http://localhost:${PORT}"
cd backend
ADDR=":${PORT}" exec go run ./cmd/backend
