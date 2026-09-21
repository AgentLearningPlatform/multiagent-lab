# eino-lab/backend —— 主平台（含前端静态托管）
# 构建上下文=仓库根（go.work 工作区 + 共享 pkg + web 前端源码）：
#   docker build -f backend/Dockerfile -t eino-lab/backend .
# stage1: 前端构建（npmmirror 镜像见 web/.npmrc）
FROM node:20-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json web/.npmrc ./
RUN npm ci
COPY web/ ./
RUN npm run build

# stage2: 后端编译（modernc.org/sqlite 纯 Go，CGO_ENABLED=0）
FROM golang:1.25-alpine AS build
ARG GOPROXY=https://mirrors.aliyun.com/goproxy/,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.work go.work.sum ./
COPY pkg/ ./pkg/
COPY backend/ ./backend/
COPY ontology-service/ ./ontology-service/
COPY runtime-manager/ ./runtime-manager/
RUN cd backend && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/backend ./cmd/backend

# stage3: 运行时
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/backend ./backend
COPY --from=web /web/dist ./web/dist
ENV ADDR=:8080 \
    WEB_DIST=/app/web/dist \
    DB_PATH=/app/data/platform.db \
    SECRET_KEY_FILE=/app/data/.secret \
    FILES_ROOT=/app/data/projects
VOLUME ["/app/data"]
EXPOSE 8080
ENTRYPOINT ["/app/backend"]
