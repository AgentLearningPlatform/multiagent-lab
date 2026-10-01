---
module: 智能体
topic: Loop 域事实源（设计总纲·实现现状·演进跟踪）
desc: 循环层技术沉淀——ReAct 迭代上限/挂起恢复 checkpoint/定时续跑/进度产物/usage；代码地图、REQ/M39·M52 映射、C3 LongRun/C4 Evaluator 后置；姊妹档 53 号 Harness/54 号 Context/56 号 Graph
req: [REQ-204, REQ-49, REQ-210]
docs: ["02 §6 五层总纲", "38 号 C 阶段", "51 号体检"]
synced: 2026-10-01
---

# Loop：设计、实现与演进（2026-10-01 沉淀）

> **定位**：Loop 层（ReAct 迭代治理/挂起恢复/长任务推进/成本护栏）的域事实源，与 [53 号 Harness](53_Harness_设计实现与演进.md)、[54 号 Context](54_Context_设计实现与演进.md)、[56 号 Graph](56_Graph_设计实现与演进.md) 并列。技术总纲以 `docs/02 §6 五层总纲` 为准，需求以 docs/01 REQ-204 行为准。
> **层边界**：Loop 管「跑多久、断了怎么办、花了多少」——迭代上限、checkpoint 挂起恢复、定时续跑、进度产物、usage 统计。**不归**：审批挂起的 gating 策略（→53 号 Harness，挂起机制本身是 Loop 层 checkpoint 承载的）；编排（→56 号 Graph）。
> **文档地图**：38 号 C 阶段（Loop 最小闭环路线）、51 号体检（P-4 长任务可控断层残留）、docs/20 S2.16 上下文行相邻、docs/02 §12 M39/M52。

## 一、设计总纲与代码地图

| 组件 | 代码 | 职责 |
| --- | --- | --- |
| 迭代上限 | runner.go→adk ChatModelAgentConfig.MaxIterations | agent.max_iteration（normalizeMaxIter 默认 25，create 兜底）；侧板 Loop 页签配置 |
| checkpoint 持久化（C1） | internal/chat/checkpoint.go（NewStoreCheckPointStore，迁移 028） | 挂起中断快照落 SQLite，后端重启仍可恢复；多级审批沿用 cpAnchors 锚点（REQ-214 顺修：Resume 后再挂起沿用锚点 id 覆盖保存） |
| 挂起恢复 | runner.go handleInterrupted/Resume + conversation.interrupt_state | run.interrupted（kind=approval/ask_human）→resume 定向投递；新消息放弃挂起（abandon）；挂起超时自动 deny（REQ-231③，判定语义归 53 号审批面） |
| 定时续跑（C5） | api/scheduler.go + conversation_schedule 表（迁移 038） | interval≥1min/max_runs≤50 硬校验、cap 即摘、活跃会话跳过；**调度状态出进程入 DB（REQ-224/M52 收口——重启失效诚实边界闭合）** |
| 进度产物（C2） | tool/todo.go + handlers_export.go todo.md 导出 | todo_write 落 conversation.todo_json + GET /api/conversations/{id}/todo.md |
| usage 统计（C6） | runner.go run.finished + api/handlers_stats.go UsageStats | run 级 prompt/completion/total tokens；GET /api/stats/usage 按 model/agent/project/conversation 聚合（REQ-49 扩展出池兑现） |
| 侧板 Loop 页签 | web AgentSidePanel | max_iteration + 挂起恢复/续跑说明（REQ-219/M50） |

## 二、实现现状

| 能力 | REQ | 里程碑 | 状态 | 验证 |
| --- | --- | --- | --- | --- |
| C1 持久化 checkpoint/C2 todo.md/C5 定时续跑/C6 usage | REQ-204 | M39 | ✅（核心四项） | 真机 9/9 |
| C5 调度持久化（重启失效收口） | REQ-224 | M52 | ✅ | 迁移 038+DB 往返 |
| 挂起超时自动 deny | REQ-231③ | M58 | ✅ | 真机 danger 链（超时判定=恢复重入时） |
| Loop 页签（五层视角） | REQ-219 | M50 | ✅ | headless 16/16 |
| C3 LongRun 有界增量 | REQ-204 | 后置 | ⏳ 与模型能力共演化 | — |
| C4 Evaluator 评估器 | REQ-204 | 后置 | ⏳ 与 REQ-223 评估基建衔接评估 | — |

## 三、开放问题与诚实边界

1. **C3/C4 后置**——LongRun 有界增量与 Evaluator 按「与模型能力共演化」暂缓；C4 出现时优先评估复用 REQ-223 agenteval 的 Judge 契约而非新面。
2. **per-tool 成本无归因**——usage 只到 run 级（REQ-204 C6），工具级 token/成本归因列观察项（与 53 号 W6 执行期超时同批评估）。
3. **挂起触达缺失**——审批/追问挂起无提醒通道（桌面客户端未覆盖推送）；超时自动 deny 已有，主动触达仍缺（51 号 P-H6 残留项）。
4. **abandon 语义单一**——仅新消息触发放弃挂起；显式「放弃本次挂起」按钮列观察项。

## 四、维护约定

新增 Loop 层演进（LongRun/Evaluator/成本归因/挂起触达）默认落本档续行；里程碑权威 docs/02 §12。
