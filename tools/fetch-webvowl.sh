#!/usr/bin/env bash
# M21/VIZ-3（REQ-154）：拉取 WebVOWL + OWL2VOWL 独立分发包到 web/public/vendor/webvowl/
# （15 号登记簿在册项目，MIT；一次性资源获取脚本——产物不入库，.gitignore 覆盖）
set -e
cd "$(dirname "$0")/.."
BASE="https://github.com/VisualDataWeb/WebVOWL/releases/download/v1.1.7"
DEST="web/public/vendor/webvowl"
mkdir -p "$DEST"
echo "[fetch-webvowl] 下载 $BASE …"
curl -fsSL "$BASE/webvowl.js" -o "$DEST/webvowl.js"
curl -fsSL "$BASE/webvowl.css" -o "$DEST/webvowl.css"
# owl2vowl 浏览器分发包（WebVOWL 仓库内 converter 产物）
curl -fsSL "https://cdn.jsdelivr.net/gh/VisualDataWeb/OWL2VOWL@0.5.3/dist/owl2vowl.js" -o "$DEST/owl2vowl.js" \
  || echo "[fetch-webvowl] 警告: owl2vowl jsdelivr 获取失败，请手动放置 $DEST/owl2vowl.js（参考 15 号登记簿）"
ls -la "$DEST"
echo "[fetch-webvowl] 完成。刷新页面即可使用 WebVOWL 对照视图。"
