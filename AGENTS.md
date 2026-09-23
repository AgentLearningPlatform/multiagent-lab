# AGENTS.md — AI 编码助手项目引导

> 本文件面向 AI 编码工具（zcode / Claude Code / Cursor 等）。人类用户请看 README.md。

## 项目定位

「学习智能体构建和本体构建的 Web 平台」——Go + CloudWeGo Eino/ADK 迷你版 Coze，六大模块工作台（智能体 / 项目 / 本体 / 知识库 / 技能 / 设置）。定位是**学习平台**：每条需求的实现都要有教学价值，能力边界诚实标注（如 KG 增强检索「前期够教学即可」）。

## 仓库结构

- `backend/` 主平台后端（Hertz :8080，Eino ADK 编排、SQLite、SSE 流式）
- `ontology-service/` 本体构建平面（:8091，spec_json 多形态资产、LLM 辅助创建、自研 KG：SQLite 三表 + PROV-O 导出）
- `runtime-manager/` 本体运行平面（:8090，多引擎方案管理：Oxigraph / Fuseki）
- `web/` React 前端（Vite + AntD；本体模块五栏 IA：学习中心 / 构建 / 资产 / 运行 / 消费与审计）
- `tools/` 工具脚本（`tools/semantica-worker/` 已归档休眠——代码保留、**勿重新启用**，D-O15）
- `deploy/` Docker Compose + Helm；`seeds/` 种子数据与学习内容包
- `docs/` 项目文档事实源（活文档体系）；`research/` 立项前调研依据

## 开发前必读（按需读，不必全读）

| 文档 | 内容 |
|---|---|
| `docs/03_本体_需求文档.md`（v0.18） | 本体模块**唯一需求事实源**：REQ 全表 + 决策记录 D-O1~15（§5）+ 验收要点（§7）+ 迭代记录（§8） |
| `docs/04_本体_方案设计.md` | 本体模块方案设计（需求与方案分离） |
| `docs/02_智能体_技术方案设计.md`（v0.32） | 智能体平台技术方案、API 面、里程碑 |
| `docs/12_知识库_方案设计.md` / `docs/11_知识库_需求文档.md` | KB 模块（RAG/GraphRAG 双子模块，KG 自研内置 backend） |
| `docs/14_本体_前端改造方案.md` | 前端结构与改造史（当前态以此为准） |
| `docs/16_部署与运行.md` | 本地 / Docker / K8s 三条部署路径 |
| `docs/15_开源项目及论文登记簿.md` | 全局台账：**新引入开源项目/论文必须先登记** |
| `docs/07` 工具链 / `docs/08` 自研边界 / `docs/13` 学习缺口分析 | 决策依据 |
| `research/` | 13 份立项前调研——各决策的 why（为何选 Eino、为何不自研 GraphRAG 等），映射表见 `research/README.md` |

## 硬性纪律

1. **需求先行**：新需求/需求变更先在 03 落档（版本递增 + §8 迭代表留痕），再实现；实现交付后回写相关文档（回写前先读各文档头部最新版本号，避免撞号）。
2. **git**：只 add 本次实际修改的文件路径（禁止 `git add .` / `git add -A`）；commit message 概述变更要点；commit 后 push origin/main。
3. **权威 = origin/main**；本仓库（macOS 正身）是唯一开发地，云盘副本只读。
4. **自研边界**：开源优先、避免重复造轮子（D-O5）；**不接入 Python 运行时依赖**（D-O15：`run-dev.sh` 一条命令启动零 venv，验收 22）。
5. **验收**以 03 §7 验收要点为准；前端改动用 headless Chrome（CDP）冒烟验证。
6. **活文档**：已确认需求不删除，变更标注 `（已变更/已废弃，见 vX.X）`；文档正文与实现不符时，以实现为准并回修文档。

## 当前状态（2026-09-23，由协作 Agent 维护）

- D-O15/REQ-110 去-semantica 化已实现交付（验收 22 达成），待主人验收第五栏「消费与审计」。
- P2 存量池：REQ-78 双轨 TTL 互通 / REQ-83 fork 扩展 / REQ-71 实例画布增强 / NFR-O-5 方案日志面板 / OWL 映射细则 / Cayley 内存图引擎（可选）。
- P3：REQ-79 本体对齐合并 / REQ-77 开放工具注册 / WebVOWL 嵌入 / 本体热加载等。
- 本节随开发推进更新；状态变化时同步 MEMORY 协作线。
