# Eino 多智能体学习平台

本地学习用多智能体平台（Go + CloudWeGo Eino/ADK + React + SQLite）。设计文档见 `docs/`。

## 快速开始

```bash
./run-dev.sh          # Linux / macOS（Windows 用 run-dev.ps1）
# 浏览器访问 http://localhost:8080
```

首次启动自动：
- 初始化 SQLite（`backend/data/platform.db`），含内置示例 Agent「学习助手」
- 预置 DeepSeek 模型连接（名称"DeepSeek 官方"）

## 下一步（必做）

打开顶部导航「设置」→ 模型连接，编辑 **DeepSeek 官方**，填入你的 API Key（https://platform.deepseek.com），保存后点「测试连接」。也可以新增任意 OpenAI 兼容连接（自定义 base_url + model）。

Key 仅存本地（AES-256-GCM 加密，密钥文件 `backend/data/secret.key`），不会上传。

## 目录结构

```
go.work
backend/            Go 后端（:8080，含前端静态托管）
  cmd/backend/      入口
  internal/
    store/          SQLite + migrations（001 业务表 / 002 模型连接）
    api/            REST API（agents / projects / conversations / model-connections / runs SSE）
    chat/           Eino ADK inprocess 装配 + SSE 运行器
    secrets/        AES-256-GCM
    llm/            ChatModel/Embedder 工厂 + 连接测试
web/                React 18 + Vite + TS + zustand
docs/               设计文档（PRD / 技术方案 / 本体方案 / 原型）
prototype/          原型快照
```

## 开发模式（前端热更新）

```bash
cd backend && go run ./cmd/backend   # 终端 1（:8080）
cd web && npm run dev                # 终端 2（:5173，API 代理到 8080）
```

## 当前里程碑状态

- ✅ M0 脚手架：五模块导航、Agent/项目视图折叠树、对话 CRUD
- ✅ M1 模型连接：CRUD / 测试连接 / AES-256-GCM 加密 / 预置 DeepSeek
- ✅ M2 单 Agent 对话：Eino ADK 装配、SSE 流式、多轮历史、停止生成
- ⬜ M3+ 项目编排、知识库、本体、技能（见 docs/02 里程碑规划）

## 常见问题

- **对话报"运行前装配失败"**：未配置可用模型连接，去「设置」填 Key。
- **换端口**：`./run-dev.sh 9090` 或 `.\run-dev.ps1 -Port 9090`。
- **数据库重置**：停止服务后删除 `backend/data/` 目录（会清空所有对话与配置）。
