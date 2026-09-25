# eino-lab/agentd —— Docker 沙箱镜像（M10）
# 沙箱内仅 agentd 二进制（Eino 编译进二进制，无外部依赖）；
# 运行时由主平台注入环境：AGENT_ID / PLATFORM_URL / MANIFEST_TOKEN / ADDR
# 构建上下文=仓库根：
#   docker build -f backend/cmd/agentd/Dockerfile -t eino-lab/agentd .
#   （并在「设置 → 执行后端」或 SANDBOX_IMAGE=eino-lab/agentd 引用）
FROM golang:1.25-alpine AS build
ARG GOPROXY=https://mirrors.aliyun.com/goproxy/,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.work go.work.sum ./
COPY pkg/ ./pkg/
COPY backend/ ./backend/
COPY ontology-service/ ./ontology-service/
COPY runtime-manager/ ./runtime-manager/
RUN cd backend && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/agentd ./cmd/agentd

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/agentd /app/agentd
ENV ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/app/agentd"]
