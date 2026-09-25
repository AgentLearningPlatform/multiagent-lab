# research/ — 立项前调研与选型依据

本目录收存项目立项/决策前的调研文档（2026-09-23 自外部资料区一次性导入，快照性质）。阅读它们可以理解各决策的 **why**——为什么选 Eino、为什么 KG 自研而不是接开源框架、为什么 D-O15 反转 D-O10。

## 文档 → 决策映射

| 文档 | 对应决策 / 需求 |
|---|---|
| 本体对话Agent技术选型_Eino_vs_DeepSeekHarness_20260915.md | Eino ADK 技术选型依据（docs/02 技术方案） |
| 扣子多Agent智能体平台实现架构反推_20260915.md | 平台架构对标参考（本项目即迷你版 Coze） |
| 开源本体构建与运行工具链调研_report.md | docs/07 工具链清单；D-O5 避免自研 / D-O7 引导执行 |
| 开源本体可视化工具调研_report.md | 可视化选型；D-O12 收口（ReactFlow 内置，WebVOWL 降 P3） |
| 本体运行时与MCP服务开源方案调研_report.md | 运行平面引擎选型（Oxigraph/Fuseki）；docs/08 自研边界 |
| 开源本体与语义层赛道全景.md | 赛道背景；docs/15 登记簿 |
| 对话式与LLM本体构建工具调研_OntoChat_OntoExtend_20260922.md | REQ-103 OntoChat 流程 / OntoExtend 引导路径 |
| 对话伴生本体生长可行性分析_20260922.md | REQ-103 立项依据 |
| openbkn-ai-分析与借鉴.md | OpenBKN 借鉴点（D-O12 收口：仅参考 🔭） |
| OpenBKN借鉴映射_eino-multiagent-lab_20260922.md | OpenBKN → 本项目逐项借鉴映射 |
| open-ontologies借鉴与引入分析.md | open-ontologies 独立集成分析（运行栏引导页沿用） |
| 本体平台与学习资源地图_20260911.md | REQ-109 学习中心外部资源导航；docs/15 登记簿 |
| ChatWindow渲染与SSE链路审查_20260925.md | NFR-O-6/REQ-145 前端体验优化与 REQ-150 的实现输入（ChatWindow 性能/正确性/架构审查：P1~P6、B1~B7、A1~A4 及修复优先级） |
| 本体Agent开发上手指南.html | 本体开发教学上手材料（独立 HTML，浏览器打开） |
| 智能体沙箱方案调研_20260925.md | M10/REQ-122 沙箱方案比选与阶段化路线（容器/Pod 运行智能体） |
| DeepSeek_Harness接入可行性_20260925.md | M24/REQ-160 dsh 推理后端适配器可行性（M13 外部 CLI 骨架复用，PoC 先行） |
| 本体可视化方案调研_20260925.md | REQ-154 可视化 3.0 选型（3D/伪三维/聚焦交互；3d-force-graph 基线 + WebVOWL 对照激活） |

## 维护约定

- 新调研报告**直接产出至本目录**并更新上表；本目录为单源。
- 调研结论进入项目文档（docs/）后，本文档保留原貌作为决策溯源，不做二次编辑。
