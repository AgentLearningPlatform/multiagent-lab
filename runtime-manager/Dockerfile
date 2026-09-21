# eino-lab/runtime-manager —— 本体运行平面（Go + oxigraph 引擎子进程）
# 构建上下文=仓库根：
#   docker build -f runtime-manager/Dockerfile -t eino-lab/runtime-manager .
# 全局 ARG（供 FROM 引用；BuildKit 不支持 COPY --from 中的变量展开，经命名阶段中转）
ARG OXIGRAPH_VERSION=0.5.11
FROM ghcr.io/oxigraph/oxigraph:${OXIGRAPH_VERSION} AS oxigraph

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
RUN cd runtime-manager && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/runtime-manager ./cmd/runtimed

# stage2: 运行时（debian-slim：oxigraph 0.5+ 官方二进制为 glibc 链接，无 musl 资产）
# 引擎二进制取自官方镜像（v0.5 起改名 oxigraph，serve/load 为子命令——与 engine 适配器一致）
FROM debian:12-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=oxigraph /usr/local/bin/oxigraph /usr/local/bin/oxigraph
WORKDIR /app
COPY --from=build /out/runtime-manager ./runtime-manager
COPY runtime-manager/migrations ./migrations
ENV ADDR=:8090 \
    DB_PATH=/app/data/runtime.db \
    MIGRATIONS_DIR=/app/migrations \
    OXIGRAPH_BIN=/usr/local/bin/oxigraph \
    ENGINE_DATA_DIR=/app/data/engines \
    ENGINE_LOG_DIR=/app/logs \
    BUILD_SVC_URL=http://ontology-service:8091
VOLUME ["/app/data", "/app/logs"]
EXPOSE 8090
ENTRYPOINT ["/app/runtime-manager"]
