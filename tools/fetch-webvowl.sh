#!/usr/bin/env bash
# WebVOWL 渲染资源离线手动兜底（M21/VIZ-3；2026-09-27 修复下载源）
# 主路径：web/scripts/prepare-vendor.mjs 已从 node_modules 的 angular-webvowl 包自动复制
# （npm install 即自愈，无需运行本脚本）。本脚本仅当 npm 不可用而需要手动获取时使用。
# 注意：原脚本引用的 jsdelivr OWL2VOWL@0.5.3/dist/owl2vowl.js 为无效链接——owl2vowl 是
# 纯 Java 转换器、官方从未发布浏览器分发；TTL→VOWL JSON 转换现由 ontology-service 原生
# 导出（GET /api/ontologies/{id}/export?format=vowljson），无需 owl2vowl.js。
set -e
cd "$(dirname "$0")/.."
DEST="web/public/vendor/webvowl"
mkdir -p "$DEST"
BASE="https://service.tib.eu/webvowl"   # WebVOWL 官方部署（TIB），构建产物 1.1.x
echo "[fetch-webvowl] 下载 $BASE …"
curl -fsSL "$BASE/js/webvowl.js" -o "$DEST/webvowl.js"
curl -fsSL "$BASE/css/webvowl.css" -o "$DEST/webvowl.css"
ls -la "$DEST"
echo "[fetch-webvowl] 完成。刷新页面即可使用 WebVOWL 对照视图。"
