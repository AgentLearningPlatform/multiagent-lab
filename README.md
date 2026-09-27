# AgentLab —— 智能体与本体构建平台

单机可部署的**智能体与本体构建平台**（Go + CloudWeGo Eino/ADK + React + SQLite）。以「对话式智能体工作台 + 本体全生命周期」双核心组织能力：智能体侧覆盖对话、多智能体协作、知识/技能/本体增强与完全可观测的执行过程；本体侧覆盖构建、资产管理、运行引擎与消费审计的完整闭环。设计文档见 `docs/`。

## 核心能力

- **六大模块工作台**：顶部导航切换 智能体 / 项目 / 本体 / 知识库 / 技能 / 平台知识（+ 右上角设置），统一「左列表 + 右主区」布局，分栏可拖拽。
- **对话工作台**：SSE 流式回复（Markdown 渲染）、执行过程完全可观测——工具调用 / 深度思考 / 子智能体委派 / 知识召回 / 本体查询均以来源着色的事件卡呈现（可展开 JSON、调试原始事件、导出重放）；输入框内嵌配置 chips（知识库 / 本体 / 技能一键开关）与**模型快捷切换**（REQ-174）。
- **对话对比模式**：2~4 窗格同问不同配置横向对照（模型 / 知识库 / 本体方案 / 技能 / 温度 / 提示词覆盖），配置剖面保存复用。
- **多智能体协作**：项目编排多 Agent（AgentAsTool / Transfer），委派过程时间线可见。
- **知识增强**：文档上传 → 切分 → 向量化 → 检索召回（eino-ext + Qdrant，SQLite fallback）；GraphRAG 子模块覆盖图谱浏览、多跳检索、抽取治理与社区摘要全局问答。
- **技能系统**：技能包（提示词指令 + 工具白名单 + 资源）注入 Agent，支持注入预览与会话级启停。
- **本体全生命周期**（独立构建/运行两平面）：
  - **构建**：六条构建路径（自定义 / OntoChat 引导 / OntoExtend / Open Ontologies 双轨 / 由知识库构建 / CSV 灌装），LLM 生成必过质量门禁并自修复（REQ-171 qualitygate：11 检查项 + 三维评分）；
  - **资产**：多形态资产管理、版本 diff、源码视图、SPARQL 工作台（REQ-92）、三维浏览 + WebVOWL 对照（REQ-154）、伴生本体页签；
  - **运行**：Oxigraph / Fuseki 引擎方案管理、引擎一键安装、facade MCP 五工具（`onto_*` + `sparql_query`）；
  - **消费与审计**：本体消费侧观测台（TTL/文本双源 KG 检索）+ 决策审计全程留痕。
- **伴生本体**（动态薄本体）：对话收尾旁路抽取知识候选，人工确认入会话图，自动并入检索源（REQ-170）。
- **模型管理**：多供应商多模型（openai_compat / Anthropic 双协议）、连接测试、自动发现、厂商预设、多实例与别名、按维度 token 用量统计。
- **开放集成**：Agent 对外 MCP 服务化——任意 MCP 客户端（Claude Desktop / Cursor）经 `/mcp` 端点直连对话（REQ-131，直连验证包见 `docs/27`）。
- **执行后端可插拔**：进程内装配（默认）/ Docker 沙箱（每 Agent 独立容器）/ K8s Pod 后端（同接口扩展）。
- **平台知识**：产品设计 / 技术原理 / 模块导读按模块组织，文档互引点击即读。

## 快速开始

```bash
./run-dev.sh          # Linux / macOS（Windows 用 run-dev.ps1）
# 浏览器访问 http://localhost:8080
```

首次启动自动：
- 初始化 SQLite（`backend/data/platform.db`）
- 预置 DeepSeek 模型连接（名称「DeepSeek（预置）」，key 留空待填）

## 下一步（必做）

打开顶部导航「设置」→ 模型管理，编辑 **DeepSeek（预置）**，填入你的 API Key（https://platform.deepseek.com），保存后点「测试连接」。也可以新增任意 OpenAI 兼容 / Anthropic 兼容连接（自定义 base_url + model），或从内置厂商预设快速创建。

Key 仅存本地（AES-256-GCM 加密，密钥文件 `backend/data/secret.key`），不会上传。

## 技术栈

| 层 | 选型 |
| --- | --- |
| 后端 | Go 1.22+ · CloudWeGo Eino/ADK · mark3labs/mcp-go · SQLite（Qdrant 可选向量库；K8s 后端经 kubectl CLI，零新依赖） |
| 前端 | React 18 · TypeScript · Vite · Ant Design 6 · Ant Design X 2 · x-markdown · zustand · 3d-force-graph |
| 协议 | REST + SSE（POST fetch 流式，事件协议见 docs/02 §7）· MCP（facade 消费 + Agent 对外服务化） |

## 目录结构

```
go.work
backend/              Go 主平台后端（:8080，含前端静态托管）
  cmd/backend/        主服务入口
  cmd/agentd/         沙箱镜像入口（manifest 拉取配置后容器内装配）
  internal/
    api/              REST API（agents / conversations / runs SSE / model-connections / kb /
                      companion / docs 读取 / ontology 反代 / runs / mcp 服务化）
    chat/             Eino ADK 装配（inprocess + docker 分发）+ SSE 运行器 + 对比模式
    inference/        推理后端注册表（eino-adk / claude-code / opencode / aider / deepseek-harness）
    modelproto/       模型连接协议层（openai_compat / anthropic）
    kb/ kg/           知识库向量检索 + GraphRAG / 自研 KG 抽取与治理
    companion/        伴生本体旁路管线（抽取 / 候选确认 / 会话图引擎 / 检索源并入）
    runtime/          执行后端抽象（inprocess / docker / k8s）
    skill/ tool/      技能注入 / 工具注册表
    ontology/         本体双反代 + 运行方案挂载 + MCP 工具桥接
    store/ secrets/   SQLite + migrations / AES-256-GCM
ontology-service/     本体构建平面（:8091，spec_json 多形态资产 / 六路径 / qualitygate 质量门禁 / toolchain）
runtime-manager/      本体运行平面（:8090，Oxigraph / Fuseki 引擎方案管理）
web/                  React 18 + Vite + TS + AntD 6（src/pages 含 ontology 五栏与 settings/ 组件族）
platform-knowledge/   平台知识（产品设计 / 技术原理 / 模块导读，平台知识页内联渲染）
docs/                 需求与设计文档（事实源，见下）
seeds/ learning/      技能与示例本体内容单源
deploy/               Docker Compose + Helm
tools/ research/      辅助脚本 / 立项前调研存档
```

## 文档索引

| 文档 | 内容 |
| --- | --- |
| `docs/01_智能体_需求文档_PRD.md` | 智能体需求事实源（REQ 全表 + 迭代记录） |
| `docs/02_智能体_技术方案设计.md` | 技术方案（架构 / API / SSE 协议 / §12 里程碑状态列） |
| `docs/03_本体_需求文档.md` / `docs/04_本体_方案设计.md` | 本体模块需求与方案（独立维护） |
| `docs/11` / `docs/12` | 知识库模块需求与方案 |
| `docs/14_本体_前端改造方案.md` | 前端结构与改造史 |
| `docs/16_部署与运行.md` | 部署事实源（本地 / Docker Compose / Helm / 环境变量速查） |
| `docs/18_REQ编号注册表.md` | REQ 编号唯一分配权威（全局台账） |
| `docs/20_回归冒烟清单.md` | 交付质量资产（按模块节执行冒烟动线） |
| `docs/27_外部MCP客户端直连验证包.md` | Claude Desktop / Cursor 直连配置与实测 |
| `platform-knowledge/产品设计/17_产品_信息架构与界面设计.md` | 界面/产品口径事实源 |

## Roadmap

里程碑与需求状态以 **docs/02 §12 状态列** 与 **docs/18 注册表** 为准（M1~M29 演进全程可追溯）。近期重点：本体多途径构建 Phase 2~3（M-O15：导入审查 UI / 工具链生命周期）、执行沙箱分级选型（REQ-159）。

## 开发模式（前端热更新）

```bash
cd backend && go run ./cmd/backend   # 终端 1（:8080）
cd web && npm run dev                # 终端 2（:5173，API 代理到 8080）
```

前端依赖安装使用 npmmirror 镜像（`web/.npmrc`）；Go 模块代理建议 `GOPROXY=https://mirrors.aliyun.com/goproxy/,direct`。

## 常见问题

- **对话报「运行前装配失败」**：未配置可用模型连接，去「设置」填 Key。
- **换端口**：`./run-dev.sh 9090` 或 `.\run-dev.ps1 -Port 9090`。
- **数据库重置**：停止服务后删除 `backend/data/` 目录（会清空所有对话与配置）。
- **知识库检索不可用**：embedding 连接未配置或索引未就绪——知识库页有状态徽标与引导；DeepSeek 不含 embedding，需另配向量服务（如硅基流动 BGE、千帆 embeddings-v1）。
