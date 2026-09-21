#!/usr/bin/env bash
# semantica-worker 环境准备（§4.9 D-O10）
#
# ⚠️ semantica 核心依赖较重（torch / transformers 等），首次安装需下载数 GB，耗时较长，属预期。
#    生成的 .venv 体积同样达数 GB；要求 Python 3.10–3.12。
set -e
cd "$(dirname "$0")"

# 选择 3.10–3.12 解释器
PY=python3
for cand in python3.12 python3.11 python3.10; do
  if command -v "$cand" >/dev/null 2>&1; then PY="$cand"; break; fi
done

# 版本校验（semantica 要求 Python 3.10–3.12）
"$PY" - <<'EOF'
import sys
v = sys.version_info
if not (3, 10) <= (v.major, v.minor) <= (3, 12):
    sys.exit(f"[setup] 需要 Python 3.10–3.12，当前 {v.major}.{v.minor}")
EOF

echo "[setup] 创建 venv: .venv（Python $("$PY" -V 2>&1)）"
"$PY" -m venv .venv
# shellcheck disable=SC1091
. .venv/bin/activate

PIP_INDEX="-i https://pypi.tuna.tsinghua.edu.cn/simple"
python -m pip install --upgrade pip $PIP_INDEX
echo "[setup] 安装依赖（⚠️ 多 GB 下载，请耐心等待）..."
pip install -r requirements.txt $PIP_INDEX

echo "[setup] 校验 semantica 可导入..."
python -c "import semantica; print('[setup] semantica', getattr(semantica, '__version__', '0.6.8'), 'OK')"
echo "[setup] 完成。启动：bash run.sh（或由 run-dev.sh 统一编排）"
