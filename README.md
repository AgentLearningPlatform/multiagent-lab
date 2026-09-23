# Eino 多智能体学习平台

本地学习用多智能体平台（Go + CloudWeGo Eino/ADK + React + SQLite）。通过**从单 Agent 到多 Agent、从进程内运行到沙箱化、从纯对话到知识/技能/本体增强**的渐进式开发，系统掌握多智能体应用的工程化开发。设计文档见 `docs/`。

## 预期效果

- **六大模块工作台**：顶部导航切换 智能体 / 项目 / 本体 / 知识库 / 技能（+ 右上角设置），各模块统一"左列表 + 右主区"布局，侧栏宽度可拖拽调宽。
- **对话工作台**：SSE 流式回复（Markdown 渲染 + 打字机尾标）、过程完全可观测——工具调用 / 深度思考 / 子智能体委派 / 知识召回 / 本体查询均以来源着色的事件卡呈现（可展开 JSON、调试原始事件）；输入框内嵌豆包式配置 chips（知识库 / 本体 / 技能一键开关）。
- **多智能体协作**：项目编排多 Agent（AgentAsTool 推荐 / Transfer 对照），委派过程在时间线可见。
- **知识增强**：文档上传 → 切分 → 向量化 → 检索召回全链路，回答引用可溯源，支持检索试运行。
- **技能系统**：技能包（提示词指令 + 工具白名单）注入 Agent，支持注入预览与会话级启停。
- **本体对接**：经 MCP facade 挂载本体运行方案，onto_* 工具查询结构化知识，方案不可用自动降级。
- **模型管理**：多供应商多模型（Key 加密存储）、连接测试、按类型设默认、模型自动发现、按模型/智能体/项目维度的 token 使用统计。
- **执行后端可插拔**：进程内装配（默认）或 Docker 沙箱（每 Agent 独立容器）。

## 快速开始

```bash
./run-dev.sh          # Linux / macOS（Windows 用 run-dev.ps1）
# 浏览器访问 http://localhost:8080
```

首次启动自动：
- 初始化 SQLite（`backend/data/platform.db`），含内置示例 Agent「学习助手」
- 预置 DeepSeek 模型连接（名称"DeepSeek 官方"）

## 下一步（必做）

打开顶部导航「设置」→ 模型管理，编辑 **DeepSeek 官方**，填入你的 API Key（https://platform.deepseek.com），保存后点「测试连接」。也可以新增任意 OpenAI 兼容连接（自定义 base_url + model），或从内置厂商预设快速创建。

Key 仅存本地（AES-256-GCM 加密，密钥文件 `backend/data/secret.key`），不会上传。

## 技术栈

| 层 | 选型 |
| --- | --- |
| 后端 | Go 1.22+ · CloudWeGo Eino/ADK · mark3labs/mcp-go · SQLite（Qdrant 可选向量库） |
| 前端 | React 18 · TypeScript · Vite · Ant Design 6 · Ant Design X 2 · x-markdown · zustand |
| 协议 | REST + SSE（POST fetch 流式，事件协议见 docs/02 §7） |

## 目录结构

```
go.work
backend/            Go 后端（:8080，含前端静态托管）
  cmd/
    backend/        主服务入口
    agentd/         沙箱镜像入口（M10，manifest 拉取配置后容器内装配）
  internal/
    store/          SQLite + migrations（001~006：业务/模型连接/知识库/技能/项目文件/技能开关）
    api/            REST API（agents / projects / conversations / model-connections / kb / skills /
                    tools / stats / ontologies 反代 / runs SSE，含连接测试探针）
    chat/           Eino ADK 装配（inprocess + docker 分发）+ SSE 运行器
    kb/             向量索引 / 检索 / 对话召回
    skill/          技能注入与工具合并
    tool/           工具注册表（内置 / MCP / save_file）
    ontology/       本体双反代 + 运行方案挂载
    runtime/        执行后端抽象（inprocess / docker）
    secrets/        AES-256-GCM
ontology-service/   本体构建平面（独立维护，见 docs/04）
runtime-manager/    本体运行平面（独立维护）
web/                React 18 + Vite + TS + antd 6 + Ant Design X + zustand
  src/pages/        agents / projects / ontology / knowledge / skills / settings
  src/components/   ChatWindow / Sidebar / AgentModal / ProjectModal / TopNav / NameModal
docs/               需求与设计文档（见下）
tools/              辅助脚本
```

## 文档索引

| 文档 | 内容 |
| --- | --- |
| `docs/01_智能体_需求文档_PRD.md` | 需求事实源（REQ 编号体系 + 迭代记录，v0.15） |
| `docs/02_智能体_技术方案设计.md` | 技术方案（架构 / DDL / API / SSE 协议 / 里程碑，v0.14） |
| `docs/04_本体_方案设计.md` | 本体模块（独立维护：构建/运行两平面） |
| `docs/11_知识库_需求文档.md` / `docs/12_知识库_方案设计.md` | 知识库模块（v0.12 起独立成档） |
| `docs/16_部署与运行.md` | 部署事实源（本地开发 / Docker Compose / Helm / 客户端打包 / 环境变量速查） |
| `docs/17_产品_信息架构与界面设计.md` | 界面/产品口径基线（原 prototype/ 原型已移除） |

## Roadmap

里程碑口径见 docs/02 §12；✅ 完成 · ◐ 部分 · ⬜ 未开始。

- ✅ **M0 脚手架**：顶部导航模块化工作台、Agent/项目折叠树、对话 CRUD（已演进为全蚂蚁生态 UI：ProLayout 式骨架 / Splitter 分栏 / 品牌化设计）
- ✅ **M1 模型连接**：CRUD / 测试连接 / AES-256-GCM 加密 / 预置 DeepSeek
- ✅ **M2 单 Agent 对话**：Eino ADK 装配、SSE 流式、多轮历史还原、停止生成
- ✅ **M3 全局默认模型**：按类型设默认、连接停用与引用保护（409）、Agent 未指定时跟随默认
- ✅ **M4 项目多 Agent**：agent_as_tool / transfer 协作装配、子智能体事件时间线
- ✅ **M5 配置驱动 + 工具注册表**：每次运行按配置装配、停止生成、错误态、工具勾选（内置/技能/MCP/本体来源标注）
- ✅ **M6 知识库（开源 RAG）**：向量索引/检索/对话召回（eino-ext + Qdrant，SQLite fallback）、知识库管理页、检索试运行、召回引用卡
- ✅ **M7 技能库**：技能 CRUD + 注入预览 + 内置示例 + 会话级启停（enable_skills）
- ✅ **M8 本体对接**：MCP facade 接入、运行方案挂载（仅 running 可选）、不可用降级、双反代、只读流水线页（S1~S7）
- ◐ **M8.5 OpenOntologies 集成**（P2）：未开始（双轨独立路径，oo-worker :8092）
- ◐ **M9 Agent 级独立配置**：后端 ✅（技能挂载生效 / MCP 多 server 装载 / 前缀防冲突）；前端技能与 MCP 挂载编辑器待补
- ◐ **M10 Docker 沙箱**：后端 ✅（agentd 镜像 + manifest 下发 + 转发分发）；k8s Pod 后端（P2）未开始；沙箱路径 enable_skills 透传待补
- ◐ **M11 项目文件与产物**（P3）：后端 ✅（save_file 工具 + 路径白名单 + files API + artifact.saved 事件）；前端文件面板未实现
- ⬜ **M12 待办排期**：项目工作台增强（本地目录绑定 + git 状态 / 类 VSCode Git Graph 侧边栏 / 项目配置迁入侧栏，REQ-101~103）；模型管理预设增强（国内厂商预设 + 快速引用 + 预置千帆 embeddings-v1，REQ-104~106）
- ⬜ **对话对比模式**（REQ-19e/f，P1）：文档已排期，未实现（多窗格同问不同配置横向对比）

## 开发模式（前端热更新）

```bash
cd backend && go run ./cmd/backend   # 终端 1（:8080）
cd web && npm run dev                # 终端 2（:5173，API 代理到 8080）
```

前端依赖安装使用 npmmirror 镜像（`web/.npmrc`）；Go 模块代理建议 `GOPROXY=https://mirrors.aliyun.com/goproxy/,direct`。

## 常见问题

- **对话报"运行前装配失败"**：未配置可用模型连接，去「设置」填 Key。
- **换端口**：`./run-dev.sh 9090` 或 `.\run-dev.ps1 -Port 9090`。
- **数据库重置**：停止服务后删除 `backend/data/` 目录（会清空所有对话与配置）。
- **知识库检索不可用**：embedding 连接未配置或索引未就绪——知识库页有状态徽标与引导；DeepSeek 不含 embedding，需另配向量服务（如硅基流动 BGE、千帆 embeddings-v1）。
