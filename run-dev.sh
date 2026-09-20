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

# 前端构建（dist 缺失或 FORCE_BUILD=1 时）
if [ ! -f web/dist/index.html ] || [ "${FORCE_BUILD:-0}" = "1" ]; then
  echo "[run-dev] 构建前端..."
  (cd web && npm install --no-audit --no-fund && npm run build)
fi

echo "[run-dev] 启动 backend: http://localhost:${PORT}"
cd backend
ADDR=":${PORT}" exec go run ./cmd/backend
