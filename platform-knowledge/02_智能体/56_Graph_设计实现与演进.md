---
module: 智能体
topic: Graph 域事实源（设计总纲·实现现状·演进跟踪）
desc: 协作层技术沉淀——成员装配（agent_as_tool/transfer）/subagent 事件/项目侧编排配置；现状=能力在装配、编排未启动（REQ-205/M40 触发驱动）；姊妹档 53 号 Harness/54 号 Context/55 号 Loop
req: [REQ-33, REQ-205, REQ-208]
docs: ["02 §6 五层总纲", "02 §6.4 多 Agent 装配", "38 号 D 阶段", "39 号 E2", "44 号 Eino 分析"]
synced: 2026-10-01
---

# Graph：设计、实现与演进（2026-10-01 沉淀）

> **定位**：Graph 层（多智能体协作编排）的域事实源，与 [53 号 Harness](53_Harness_设计实现与演进.md)、[54 号 Context](54_Context_设计实现与演进.md)、[55 号 Loop](55_Loop_设计实现与演进.md) 并列。技术总纲以 `docs/02 §6 五层总纲`+§6.4 为准，需求以 docs/01 REQ-205/REQ-33 行为准。
> **层边界**：Graph 管「多个智能体怎么组队与流转」——成员装配语义、委派机制、工作流编排。**不归**：单 agent 内的审批/hooks（→53 号）、迭代与恢复（→55 号）。
> **文档地图**：38 号 D 阶段（Graph 兑现路线，未启动）、39 号 E2（编排收口，并入 REQ-208 扩注）、44 号 Eino 深度分析（agent_as_tool/transfer 机制对照，44 号定案 transfer=NOT RECOMMENDED）、docs/02 §6.4。

## 一、设计总纲与代码地图

| 组件 | 代码 | 职责 |
| --- | --- | --- |
| agent_as_tool（推荐路径） | assembler.go（adk.NewAgentTool 包装，function name=成员名） | 项目成员全量装配后包装为工具，协调者按描述路由调用；ADK 只回传末条文本（REQ-203 代码实测定案，子 Agent 结果压缩无需开发） |
| transfer_to_agent（对照路径） | assembler.go（adk.SetSubAgents） | ADK 内建转移机制；44 号 NOT RECOMMENDED 路线，**治理定案并入 REQ-208 扩注**（迁移 agent_as_tool 或保留对照+UI 风险标注） |
| subagent 事件 | runner.go trackAgent（EmitInternalEvents 开启） | subagent.enter/exit 按 AgentName 变化推导；轨迹面板嵌套缩进呈现（REQ-217/M48） |
| 项目编排配置 | 项目侧板 ConfigView（collab_mode/workflow_mode/成员+协调者） | 协作模式与工作流模式的产品配置面（agent 侧板 Graph 页签=占位指引，REQ-219 诚实标注） |
| workflow 引擎 | —（models.go workflow_mode 字段空壳） | **REQ-205/M40 未启动**：三值落地/图状态持久化/HITL 节点化/只读回放均待触发 |

## 二、实现现状

| 能力 | REQ | 里程碑 | 状态 | 验证 |
| --- | --- | --- | --- | --- |
| 多成员装配（agent_as_tool+transfer 双轨） | REQ-33/124 | M5 起 | ✅（装配能力） | 多智能体项目会话 |
| subagent 嵌套事件与轨迹呈现 | REQ-117/217 | M17/M48 | ✅ | headless |
| Graph 页签占位指引（agent 级诚实空态） | REQ-219 | M50 | ✅ | headless 16/16 |
| 工作流编排兑现（三值/图状态/HITL/回放） | REQ-205=REQ-33 兑现 | M40 | 📋 **触发驱动**（出现真实编排诉求才启动） | — |
| transfer 治理 + continuation 子智能体 | REQ-208 | 39 号 E2 并入 | 📋 触发驱动 | — |

## 三、开放问题与诚实边界

1. **workflow_mode 空壳**——字段存在无实现（39 号体检实证）；M40 启动前 UI 配置不产生运行语义，项目侧板已按现状口径呈现。
2. **transfer 双轨并存**——与 agent_as_tool 语义重叠，风险=编排语义不清晰；治理定案在 REQ-208 领取时执行（二选一或标注）。
3. **agent_as_tool 同步阻塞**——子智能体调用占协调者一轮（39 号 P-3），长任务子委派体验差；LongRun（55 号 C3）落地前无解，跨层联动列 M40 设计时输入。
4. **agent 级 Graph 配置为空**——成员/协作/工作流均在项目侧配置；agent 侧板 Graph 页签一期为占位指引（REQ-219 定案），M40 落地时再评估是否提级配置面。
5. **子智能体上下文隔离**——成员独立装配（各自 harness/context），协调者不可见子内部过程（仅 enter/exit 事件）——防火墙语义是特性非缺陷，调试依赖成员侧轨迹。

## 四、维护约定

Graph 层为五层中最薄一层：REQ-205/M40 启动即本档正式开篇（工作流引擎选型/图状态/HITL 设计输入在此沉淀）；里程碑权威 docs/02 §12。
