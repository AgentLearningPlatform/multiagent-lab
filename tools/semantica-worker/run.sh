#!/usr/bin/env bash
# 启动 semantica-worker（:8093，§4.9 D-O10）
set -e
cd "$(dirname "$0")"

if [ ! -x .venv/bin/python ]; then
  echo "[semantica-worker] 未找到 .venv，请先执行: bash tools/semantica-worker/setup.sh" >&2
  echo "[semantica-worker] ⚠️ 首次安装需下载数 GB 依赖（torch/transformers 等）" >&2
  exit 1
fi

# shellcheck disable=SC1091
. .venv/bin/activate

# 默认持久化到仓库 data/semantica/graph.json（worker 位于 tools/semantica-worker）
export SEMANTICA_DATA_DIR="${SEMANTICA_DATA_DIR:-$PWD/../../data/semantica/graph.json}"
exec uvicorn worker:app --host 0.0.0.0 --port "${SEMANTICA_PORT:-8093}"
