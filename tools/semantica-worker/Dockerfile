# eino-lab/semantica-worker —— Semantica 独立集成薄服务（§4.9 D-O10，:8093）
# 构建上下文=仓库根：
#   docker build -f tools/semantica-worker/Dockerfile -t eino-lab/semantica-worker .
#
# ⚠️ 体积预期：semantica 0.6.8 核心依赖重（torch/transformers/opencv/nvidia 系列等），
#    镜像预计 6~10 GB、构建下载耗时较长——属已知痛点（docs/13 §9 待办），非异常。
#    优化方向见 docs/13 §9（semantica 0.7.0 瘦身核心 / PyInstaller 冻结 / 客户端远连降级）。
FROM python:3.12-slim
# 清华镜像加速（构建机网络不佳时可 ARG 覆盖）
ARG PIP_INDEX=https://pypi.tuna.tsinghua.edu.cn/simple
ENV PIP_INDEX_URL=${PIP_INDEX} \
    PIP_DEFAULT_TIMEOUT=120
# 先装依赖层（requirements 变更才失效缓存；semantica[explorer] 含 Explorer UI 依赖）
COPY tools/semantica-worker/requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir -r /tmp/requirements.txt
# 应用代码（薄，<400 行）
WORKDIR /app
COPY tools/semantica-worker/worker.py /app/worker.py
# 数据卷：图持久化 graph.json + ProvenanceManager audit.db
ENV SEMANTICA_ADDR=:8093 \
    SEMANTICA_DATA_DIR=/app/data/graph.json
VOLUME ["/app/data"]
EXPOSE 8093
# 健康检查：/health（图未加载也返回 ok:true，graph_loaded 区分）
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD python -c "import urllib.request;urllib.request.urlopen('http://127.0.0.1:8093/health',timeout=4)" || exit 1
CMD ["python", "worker.py"]
