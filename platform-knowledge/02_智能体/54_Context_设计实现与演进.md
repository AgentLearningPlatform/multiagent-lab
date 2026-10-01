---
module: 智能体
topic: Context 域事实源（设计总纲·实现现状·演进跟踪）
desc: 上下文层技术沉淀——预算三档/压缩持久化/工具结果剪枝/tool 轮次保全/召回链；代码地图、REQ/M37 映射、开放问题；姊妹档 53 号 Harness/55 号 Loop/56 号 Graph
req: [REQ-201, REQ-210]
docs: ["02 §6 五层总纲", "51 号体检", "37 号范式"]
synced: 2026-10-01
---

# Context：设计、实现与演进（2026-10-01 沉淀）

> **定位**：Context 层（历史预算/压缩/剪枝/工具轮次保全/召回链）的域事实源，与 [53 号 Harness](53_Harness_设计实现与演进.md)、[55 号 Loop](55_Loop_设计实现与演进.md)、[56 号 Graph](56_Graph_设计实现与演进.md) 并列；与 [00 智能体模块导读](00_智能体模块.md) 同目录。技术总纲以 `docs/02 §6 五层总纲` 为准，需求以 docs/01 REQ-201 行为准。
> **层边界**：Context 决定「模型每轮真正看到什么」——历史还原与预算治理、压缩、剪枝、召回注入。**不归**：工具装配与防护（→53 号 Harness）、迭代上限与挂起恢复（→55 号 Loop）。
> **文档地图**：37 号（Harness/Loop/Graph 三层理论）、38 号 A 阶段（上下文补零路线，已全部交付）、51 号体检（A-8 上下文纵深残留）、docs/20 S2.17（上下文工程冒烟行）。

## 一、设计总纲与代码地图

| 组件 | 代码 | 职责 |
| --- | --- | --- |
| 预算三档 | `internal/chat/context.go` ContextBudget | agent.context_mode：compact≈6k / standard≈24k / full 不限量（存量行为）；默认标准 |
| tool 轮次保全（A1） | context.go mergeToolTurns + store.ListToolEvents | 消息+run_event 时间归并：assistant 回填 ToolCalls、配对 tool 消息、悬空调用补「结果未知」合成文本（REQ-210 不变量） |
| 压缩持久化（A3） | context.go summarizeContext/buildRunContext + conversation.context_state（迁移 025 段） | 超预算先应用前缀摘要；仍超保留尾半预算+首条 user，前段≥1500 tokens 用该 agent 模型八段式摘要并持久化复用 |
| 工具结果剪枝（A5） | context.go pruneToolResult | >4000 字符保留头 2400+尾 800+截断标注（模型输入侧；存储侧 run_event 32KB 截断归 53 号 Harness 事件契约） |
| 召回链（A4） | runner.go recallChain | recallKB→recallCompanion 顺序注入（知识库/伴生图；embedding 未配置自动词法兜底），retrieval 事件透出 |
| 侧板 Context 页签 | web AgentSidePanel | context_mode 三档选择 + 进程内固定观察项说明（REQ-219/M50 五层视角） |

## 二、实现现状

| 能力 | REQ | 里程碑 | 状态 | 验证 |
| --- | --- | --- | --- | --- |
| tool 轮次保全/预算三档/压缩/剪枝/召回链 | REQ-201 | M37 | ✅ | 真机回归 11/11（含 compact 触发压缩持久化、轨迹回忆原话可证） |
| 悬空调用「结果未知」合成（运行时不变量） | REQ-210 | M37 同轮 | ✅ | model.step 逐条可见 |
| Context 页签（五层视角） | REQ-219 | M50 | ✅ | headless 16/16 |

## 三、开放问题与诚实边界

1. **压缩策略不可配**——八段式摘要固定复用本 agent 模型连接，无策略/模型可选（51 号 W 系列未立项；Context 视角观察项）。
2. **无 per-section token 归因**——预算消耗只有总账无分节账（39 号 A-8），调优靠 debug 装配快照间接观察。
3. **外部 CLI 后端旁路**——inference_backend 为外部 CLI 时由其自身 historyLimit 治理，context_mode 不生效（REQ-201 交付诚实边界，与 53 号 W5 页签降维同批处理）。
4. **会话级无预算覆盖**——context_mode 仅 agent 级；长会话临时降档需改 agent 配置（会话级覆盖列观察项，沿 REQ-231 生效透出先例可做 run.started 透出）。
5. **召回链深度**——伴生检索 LIMIT/向量缓存冷启动等检索纵深问题归伴生线（40 号 A-3），不在本层重复。

> 体检来源：39 号 A-8「上下文纵深无 per-section 归因」即本档开放问题 2（39 号已于 2026-10-01 清理，结论归档于 00 号）。

## 四、维护约定

新增 Context 层演进（预算策略/压缩可配/会话级覆盖/token 归因）默认落本档续行；里程碑权威 docs/02 §12。
