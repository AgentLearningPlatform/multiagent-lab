# eino-lab/ontology-service —— 本体构建平面（Go + Python rdflib sidecar）
# 构建上下文=仓库根（需 tools/rdf-sidecar/sidecar.py 与 go.work 工作区）：
#   docker build -f ontology-service/Dockerfile -t eino-lab/ontology-service .
# stage1: Go 编译
FROM golang:1.25-alpine AS build
ARG GOPROXY=https://mirrors.aliyun.com/goproxy/,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.work go.work.sum ./
COPY pkg/ ./pkg/
COPY backend/ ./backend/
COPY ontology-service/ ./ontology-service/
COPY runtime-manager/ ./runtime-manager/
RUN cd ontology-service && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ontology-service ./cmd/ontologyd

# stage2: 运行时（python3 + rdflib，清华镜像）
FROM python:3.12-slim
RUN pip install --no-cache-dir rdflib -i https://pypi.tuna.tsinghua.edu.cn/simple
WORKDIR /app
COPY --from=build /out/ontology-service ./ontology-service
COPY ontology-service/migrations ./migrations
COPY tools/rdf-sidecar/sidecar.py ./sidecar/sidecar.py
ENV ADDR=:8091 \
    DB_PATH=/app/data/ontology.db \
    MIGRATIONS_DIR=/app/migrations \
    SIDECAR_PYTHON=python3 \
    SIDECAR_SCRIPT=/app/sidecar/sidecar.py \
    PLATFORM_URL=http://backend:8080
VOLUME ["/app/data"]
EXPOSE 8091
ENTRYPOINT ["/app/ontology-service"]
