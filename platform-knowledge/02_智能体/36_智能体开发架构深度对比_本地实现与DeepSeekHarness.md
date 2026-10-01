---
module: 智能体
topic: 智能体开发架构深度对比——multiagent-lab 本地实现 vs DeepSeek Harness（dsh）
desc: 十层架构逐层对照（装配/循环/上下文/工具/会话/子智能体/工程质量等）——回答「它的架构长什么样、我们差在哪、能借鉴什么」；纯资料类，与 31 号（对接可行性）、41 号（dsh 源码拆解）分工不重复
docs: ["docs/02_智能体_技术方案设计.md §6.16"]
synced: 2026-09-30
---

# 智能体开发架构深度对比：multiagent-lab 本地实现 vs DeepSeek Harness

> **✅ 立项注记（2026-09-30）**：§5 差距清单 13 条中 11 条已经 38 号路线转译立项 **REQ-201~206/M37~M41**；残留两条亦已立项——**REQ-208 可继续子 Agent continuation**（§3.5，挂 M42 触发驱动）与运行时不变量检查（§3.9，入 01 §9 需求池随 REQ-201 同轮评估）。本档建议面至此全部有归属。
>
> 任务来源：开发者指示「分析本地 multiagent-lab 中智能体实现，对比 deepseek harness 的实现，深入研究智能体的开发架构」。
> 性质：**架构研究报告**（纯资料类，开源实现剖析）。与既有 `31_智能体对接DeepSeek-Harness_可行性及方案分析.md`（dsh 的**对接/嵌入可行性**）互补——那一份回答"能不能接进来、接多深"，本档回答"它的架构长什么样、我们的差在哪、能借鉴什么"。
> 一手证据：本地实现全部来自 `backend/` 源码实读；dsh 侧来自官方仓库 `deepseek-ai/deepseek-harness`（master，0.2.0-rc.1）`docs/architecture.zh.md`、`docs/capability-seams.zh.md`、`docs/subsystems/core.zh.md`、`docs/subsystems/session.zh.md`、`docs/subsystems/compaction.zh.md`、`docs/subsystems/subagent.zh.md`、`docs/tool-catalog.zh.md`。

---

## 0. 一句话结论

**两者不在同一层。**

- **multiagent-lab backend** 是**配置驱动的装配器 + 托管执行器**：自身不实现 ReAct 循环，把循环整体下推给 `eino/adk`，自研代码集中在「装配」（`assembler.go` 732 行）与「事件翻译」（`runner.go` 1251 行）。它是**造 agent 的平台**（多租户、配置即行为）。
- **DeepSeek Harness** 是**插件内核 + 可替换循环**：Cordis 微内核只管插件装卸，循环本体（`agent-loop`）也只是插件列表里的一行。它是**能干活的 agent**（单用户、深度能力、编码场景）。

**最大可借鉴点不是"它有哪些工具"，而是它的三条架构纪律：**
1. **唯一真源 = 仅追加事件日志**（模型历史从日志派生，从不单独存储）→ 我们的 `message` 表 + `run_event` 表双写，且历史全量重放；
2. **能力 seam 三角色（定义/提供方/消费方）**→ 我们的工具四来源只是"注册表"，不是"可替换能力"；
3. **上下文工程是一等公民**（compaction / pruner / spill / image offload）→ 我们**零压缩、零裁剪、零 token 预算**，长对话必然爆窗。

---

## 1. 范式分歧：装配器 vs 插件内核

### 1.1 本地：静态装配 + 每次运行全量重建

```
HTTP POST /api/conversations/{id}/runs
  → api.runConversation                handlers_runs.go:52
  → chat.Service.Run                   runner.go:124
      ├─ 沙箱分发（docker/k8s）         runner.go:141-156
      ├─ 外部CLI分发（dsh/claude-code…） runner.go:159-161
      └─ runOnce                        runner.go:192
          ├─ Assembler.Assemble(agent, conv)   ← 每次运行重新装配
          ├─ Store.ListMessages(全量历史)
          ├─ recallKB / recallCompanion → 追加 System 消息
          └─ rt.Runner.Run(histMsgs, WithCheckPointID)
              → consume(adk.AsyncIterator[*AgentEvent])   runner.go:686
                  → SSE 事件 + run_event 落库
```

关键性质：

| 性质 | 证据 | 含义 |
|---|---|---|
| **无状态装配，每次运行全量重建** | `assembler.go:66 Assemble`，无缓存 | 配置改动下一条消息即生效；代价是每次重建 MCP 连接、重拉工具表 |
| **循环不在本仓库** | `assembler.go:594-611 newChatModelAgent` 只构造 `adk.NewChatModelAgent{MaxIterations}` | ReAct 循环、工具并发、终止判定全在 `github.com/cloudwego/eino v0.9.19` 的 `eino/adk` 内 |
| **ADK 在 eino 核心库内**，非独立 `eino-adk` 仓库 | `go.mod:6`；`eino-adk` 只是平台自研管线的**命名** | 术语易混淆，需在文档中固化 |
| **未使用 Hertz** | `main.go:117 http.Server{Handler: withStatic(cors(srv.Mux))}`；`api/server.go:79 http.NewServeMux()` | AGENTS.md 中"Hertz :8080"的描述已与实现不符，建议回修 |
| **最大轮次 25** | `normalizeMaxIter` 默认 25；`001_init.sql:10` DEFAULT 25 | 唯一的循环边界，且是"硬轮次"而非 token 预算 |

### 1.2 dsh：插件树 + 分层配置叠加

```
启动时叠层（后在者覆盖）：
  profile 列出的 bundle 序列
    └─ dsh-base（模型适配器/工具/持久化/沙箱与审批/设置/凭据/遥测）
        └─ dsh-web-app | dsh-headless | dsh-sdk-app | dsh-acp-app
  → profile 的 cordis.patch.yml
  → home 级 patch
  → --patch overlay
一条 patch 按 id 定位某个条目并替换其整个 config。
```

- **不存在需要打补丁的特权内核**：连 `ctx.agentLoop`（默认循环驱动器）都是 `bundle` 角色的一条配置，位于 `packages/core/agent-loop`。
- **四种运行模式**：Standard（完整工具集）/ PTC（模型写一段 TS 编排多轮工具调用）/ Minimal（仅 bash + str_replace_editor，用于模型基准）/ Creator（运行时自检 + 插件实验 + 预设创作）。
- **查看机器实际配置树**：`dsh --profile web --dump-config`——打出来的任一条目都能被自己的 patch 替换。

### 1.3 为什么分歧是合理的（不是"我们落后"）

| 维度 | multiagent-lab | dsh |
|---|---|---|
| 服务对象 | **多租户平台**：用户配一个 Agent 就跑 | **单用户编码 Agent**：开箱即干活 |
| 变化轴 | Agent 配置频繁变 → 每次重建是**特性** | 组合固定、能力深 → 插件热插拔是特性 |
| 能力深度 | 浅而宽（对话/多Agent/KB/本体/技能） | 深而窄（文件系统/bash/LSP/终端/浏览器） |
| 运行时 | Go 单体 + SQLite，单机可部署 | Node + Electron + Python SDK wheel |

**结论**：不应该照抄 dsh 的全插件化（会摧毁"配置即行为"的简洁性），但**应该抄它的三条纪律**（唯一真源 / 能力 seam / 上下文工程）。

---

## 2. 分层对照总表

| 层 | multiagent-lab（backend） | DeepSeek Harness | 差距性质 |
|---|---|---|---|
| **进程/入口** | `net/http` + ServeMux，`cmd/backend` 单体；另有 `cmd/agentd`（沙箱内同构进程） | `dsh --profile web/headless/sdk/sdk-minimal/acp` + Electron 桌面版 | 相当 |
| **内核** | 无（main.go 硬编码装配） | Cordis 微内核（插件装卸 + 依赖 + 可逆副作用） | **架构级差距** |
| **循环** | `eino/adk ChatModelAgent`，`MaxIterations=25` | `ctx.agentLoop`（一个可替换插件），turn/step 模型，无硬轮次上限 | 范式差异 |
| **状态真源** | `message` 表（CRUD）+ `run_event` 表（事件）双写 | `Session` = 仅追加 `SessionEvent` 日志，模型历史 `deriveMessages()` 派生 | **架构级差距** |
| **上下文工程** | 无（全量重放，零压缩/零裁剪/零预算） | `ctx.compaction`（pressure / context-overflow）、`toolResultPruner`、spill、image offload | **最大功能缺口** |
| **工具** | `tool.Registry` 四来源（builtin/skill/mcp/ontology），**内置仅 2 个** | `ctx.tools` 作用域注册表 + 受保护执行流水线；30+ 内置工具 | 能力差距 |
| **多智能体** | `AgentTool`（supervisor-as-tool）/ `SetSubAgents`（handoff）两条，`workflow_mode` 是空壳 | `ctx.subagents` **六提供方** + continuation(Activation) + `agent-team`(roster/mailbox/任务DAG) + `workflow` 脚本编排 | **架构级差距** |
| **沙箱** | 整进程搬迁：`internal/runtime` Backend(docker/k8s/auto)，容器内跑完整 `agentd` | 进程级：`ctx.sandbox` 包 argv；`ctx.fs`/`ctx.shell`/`ctx.subprocess` 三个 seam 共享同一执行世界 | 粒度差异 |
| **技能** | `skill.Composer`：指令注入 + 工具白名单（63 行） | `ctx.skills` seam（多提供方合并目录）+ `skill` 工具懒加载正文 | 中等 |
| **人机协同** | `StatefulInterrupt` + `CheckPointStore` + `ResumeWithParams.Targets` 定向恢复（**进程内内存**） | `ask_user_question` 挂起工具调用等 UI 提供方；`ctx.approval` waterfall；会话 fork/resume | 我们更精细，dsh 更持久 |
| **可观测** | `run_event` 表 + 15 种 SSE 事件 + 三级 debug；**无 OTel、无结构化日志、无重试** | session 日志 + `ctx.otel` + `ctx.invariants`（运行时不变量检查）+ 逐文件 100% 覆盖门禁 | 中等偏大 |
| **扩展机制** | 改 Go 代码 | 挂插件 / 写 patch，不改源码 | **架构级差距** |

---

## 3. 逐维度深剖

### 3.1 循环模型：硬轮次 vs turn/step 数据驱动

**本地**（`assembler.go:594-611`）：

```go
inst, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    Name: name, Description: description,
    Instruction:   instruction,          // 技能/guide/约束注入后的最终文本
    Model:         cm,
    MaxIterations: normalizeMaxIter(maxIter),   // 默认 25
    ToolsConfig: adk.ToolsConfig{
        ToolsNodeConfig:    compose.ToolsNodeConfig{Tools: tools},
        EmitInternalEvents: true,               // 子 Agent 事件流出
    },
})
```

终止条件全在 ADK 内：模型不再发 tool_calls / 到达 25 轮 / ctx cancel / 中断 / 错误。平台侧只做事件翻译与终态判定（`runner.go:311-327`：stopped / runErr / interrupted / completed）。

**dsh**（`docs/architecture.zh.md` 轮次流程）：

```text
turn/start
  claim next-step input plus one queued message
  assemble prompt sections + tool schemas; project runtime context
  -> agent/pre-step        reject | enter(messages, startsRequestSeries?)
     step/start
     agent/request -> prepareCall
     append entered messages as user/message; log request/header + request/context
     derive and freeze model history from the log
     stream -> llm/stream -> agent/assistant-stream
     tool/call* -> tools/pre-execute -> tools/execute -> tools/post-execute -> tool/result*
     step/end
     tools owe another request, or next-step input arrived -> claim -> next step
  -> agent/turn-stopping
turn/end
```

关键差异：

| 点 | dsh | 本地 |
|---|---|---|
| **轮次/步骤是持久事件** | `turn/start`、`step/start`、`turn/end`、`step/end` 写进日志 | 只在内存里，落库的只有 `run.started`/`run.finished` |
| **终止由数据决定** | 工具结果带 `concludesTurn` 即结束轮次；`agent/turn-stopping` 监听器可 `steer()` 续一步 | 只能靠 25 轮硬上限或模型自觉 |
| **拦截点** | `agent/pre-step`（waterfall，可改写/拒绝进入步骤的消息）、`agent/request`（可替换冻结的调用配置）、`agent/request-error`（返回 `{kind:'retry'}` 即可重试）、`agent/turn-stopping`（serial） | 无对应扩展点；重试被**明确拒绝**（"单次失败即降级，不重试风暴"，`assembler.go:26/374/455`） |
| **取消语义** | `AgentCancelCause = user \| parent \| hook \| disposed`，第一因胜出，`keepInbox` 保留待办，取消原因随 `turn/end` 持久化 | `cancels map[convID]CancelFunc`，已生成内容照常落库，原因不持久化 |

**可借鉴**：`agent/pre-step` 这个 waterfall 钩子是"上下文准入"的天然位置——我们现在的 KB 召回 / 伴生召回是在 `runOnce` 里硬编码追加 System 消息（`runner.go:271/274`），改成 pre-step 钩子后，技能、MCP、第三方插件都能往同一步骤注入上下文而不用改 `runOnce`。

### 3.2 唯一真源：双写表 vs 仅追加事件日志

**本地**：`message` 表（`id, conversation_id, role, content, meta, created_at`）承载对话，`run_event` 表（`id, conversation_id, run_id, type, data, created_at`）承载事件。`BuildHistoryMessages`（`assembler.go:718`）从 `message` 表重建历史，**只处理 user/assistant/system，tool 消息被丢弃**。

问题：
- 历史与事件是**两套数据**，无法互相验证；
- `tool` 消息被丢弃 → 模型看不到自己的工具调用轨迹（对多步工具链是硬伤）；
- `Store.ListMessages` 无 LIMIT，`ORDER BY created_at, id` 全量返回。

**dsh**：`Session` 是一份**仅追加**的 `SessionEvent` 日志，**是唯一真源**。LLM 消息历史从日志*派生*（`deriveMessages()`），从不单独存储；回放即从同一组事件重新派生。

```ts
interface SessionEventMap {
  'turn/start': { turn: number }
  'turn/end':   { turn: number; reason: TurnEndReason }
  'step/start' / 'step/end': { turn: number; step: number }
  'user/message' / 'developer/message' / 'system/message'
  'assistant/message': { turn, step, message, stream, usage?, interrupted? }
  'assistant/attempt': { turn, step, stream }          // 失败/重试/取消的尝试也留痕
  'tool/call':  { turn, step, callId, name, arguments }
  'tool/result':{ turn, step, message, error?, meta? }
  'request/header' / 'request/context'                  // log-only
  'session/end-seed': { inherited?: true }               // fork 谱系边界
}
```

三条设计纪律值得逐字抄：

1. **surface 投影**：只有 5 种事件（`system/message`、`developer/message`、`user/message`、`assistant/message`、`tool/result`）产生消息，每条带 `surfaceOp`（`append` 或 `{op:'replace', startSeq, endSeq}`）。压缩即一次 `replace` 遮蔽一片节点——**历史裁剪与历史存储是同一套机制**。
2. **模型可见即已记录**（运行时不变量）：新增任何模型可见输入都必须落会话事件；插件想改消息内容要注册**纯消息投影**（`@messageProjection`），独立读取器传同样的处理器才能得到相同结果。
3. **请求可重建**：`request/header`（配置 + 工具 schema）+ `request/context`（provider/model/contextWindow/systemPromptUpdate）+ 派生的历史 = 每个请求都是日志的纯函数。崩溃后能精确重放。

**可借鉴（低成本路径）**：不一定立刻改成事件溯源，但至少——
- `BuildHistoryMessages` **不要把 tool 消息丢掉**（一行改动，多步工具链可用性直接上一个台阶）；
- `ListMessages` 加 LIMIT + 游标，别全量；
- 让 `run_event` 成为权威，`message` 退化为它的投影（或反之），消除双写歧义。

### 3.3 上下文工程：我们从零到一的最大缺口

**本地：没有任何上下文工程。** 实测 grep（`internal/` 全量，排除测试）：`compact` / `Trim` / `summariz` / `maxToken` / `contextWindow` 零命中（命中的全是 `strings.TrimSpace`）。唯一的历史窗口限制在**外部 CLI 路径**（`external.go:28 const historyLimit = 10`），主链路（`inprocess`）是全量重放。

**dsh 把上下文工程做成了四个可替换部件**：

| 部件 | 位置 | 机制 |
|---|---|---|
| `ctx.compaction` | seam，实现 `compaction-basic` | `compactIfNeeded(agent, trigger, signal)`，`trigger = 'pressure' \| 'context-overflow'`；`compactNow()` 在轮次间作为 agent maintenance 运行；`compactRegion(start, end)` 强制压缩区间 |
| `ctx.toolResultPruner` | `compaction-tool-result-pruner` | 无模型的确定性 head/middle/tail 剪枝，按 Unicode code point 切（不切 surrogate pair），每次替换前写 `compaction/prune` 计价事件 |
| `ctx.spillStore` | `spill` + `spill-local` + `spill-policy` | 过大的工具文本落库，返回"面向模型的定位信息 + 取回提示"，`spill-policy` 是 `tools/post-execute` 消费方 |
| `image/offload` | `compaction-image-offload` | 按深度优先计数精确定位输入图片并卸载，事件保留节点与消息身份 |

压缩的事务设计尤其值得学：**锁即事件对**。

```text
compaction/start  →  获取锁（turn 数字 = 未结束的自动轮次，null = 独立手动尝试）
compaction/summary →  摘要投影 + 被遮蔽范围 + token 数 + 调用的 provider/model/usage
compaction/end    →  释放锁
```

最后才释放锁，意味着**中途崩溃表现为"可检测的遗留锁"（有 start 无 end），而不是一个虚假声称压缩已完成的 end**。区域边界保持 tool call/result 配对（`toolPairingBalancedBefore/After`），但不保持整个轮次——所以一个过大轮次中较早关闭的步骤也能被压缩。

**可借鉴（分阶段）**：
- **P0（半天）**：`runOnce` 前按粗估 token 预算截断历史（保留系统 + 最近 N 轮），超预算时给一条 `run.warning`。先止血。
- **P1（2~3 人日）**：`ctx.compaction` 的极简版——一个 `Compactor` 接口 + 一个 LLM 摘要实现，压完追加一条 `system` 摘要消息并把被压轮次标 `compacted`，`ListMessages` 投影时跳过。
- **P2**：工具结果剪枝（超长 tool.result 只留 head/tail）——这是投入产出比最高的一招，编码/检索类工具动辄几十 KB。

### 3.4 工具系统：注册表 vs 受保护执行流水线

**本地**（`assembler.go:383-527 assembleTools`，7 步管线）：

| 步 | 内容 | source | 失败策略 |
|---|---|---|---|
| 1 | `Tools.Compose(agent.tools)` 内置勾选 | `builtin` | 未知 ID → 告警跳过 |
| 2 | 技能白名单并集 | `skill:{id}` | 实例化失败 → 告警 |
| 3 | MCP servers | `mcp:{server}` | 连不上 → 告警降级，不阻断 |
| 4 | 本体 facade | `ontology:facade` | 失败 → `ontology.unavailable` 事件 |
| 5 | 项目文件（save/list/read） | `builtin` | 仅项目会话 |
| 6 | `NewApprovalTool` 人工审批包装 | — | `tool_approval="all"` |
| 7 | `normalizeEmptyArgsTool` | — | 全量 |

- **冲突策略：function name 先到先得 + 告警**（`:407/445/466`）。
- **安全边界**：`isSelfMCPEndpoint`（`:349-366`）拦截指向本平台 `/mcp` 的自引用，防 agent→server→agent 递归。
- **执行不在平台层**：全部交给 ADK `compose.ToolsNodeConfig`，平台只在 `consume` 里**观测** `tool.call`/`tool.result`（`runner.go:761-783/948-972`）并算耗时。
- **内置工具只有 2 个**：`current_time`、`ask_human`（`tool/builtin.go:41`）。助手 L0/L1 工具是启动期动态注册的 8 个。

**dsh**（`ctx.tools` + 事件门禁）：

```text
tool/call → tools/pre-execute → tools/execute → tools/post-execute → tool/result
```
三个 `tools/*` 都是 **waterfall 事件**，监听器必须调 `next()` 才能委托下去。配套：
- `ctx.approval` seam —— 一次性权限决策通过 `approval/request` waterfall 分派，没有回答方则以 `unavailable` 关闭失败（**不留悬空等待**）。
- `ctx.permissionPresets` —— `workspace-write` / `danger-full-access` 预设表，一次切换贯通沙箱模式与审批策略。
- `ctx.userQuestions` —— `ask_user_question` 在**提供方无关**的 `ask()` promise 上挂起工具调用，UI 前端提供当前生效的人类回答提供方。

内置工具 30+（按包）：

| 包 | 工具 |
|---|---|
| fs | `read` `write` `edit` `read_image` |
| bash | `bash`（带 `run_in_background`） |
| terminal | `terminal_open/send/read/close/list/signal`（6 个，持久 PTY） |
| web | `web_search` `web_fetch` |
| todo | `todo_write` |
| subagent | `subagent` `list_subagent_models` |
| skill | `skill`（懒加载正文） |
| jobs | `job_list` `job_output` `job_kill` |
| lsp | `lsp`（goToDefinition/findReferences/goToImplementation/hover） |
| workflow | `workflow`（JS 脚本编排） |
| ask-user | `ask_user_question` |
| session-query | `session_search` `session_event_read/search/trace` `session_trace` |
| cordis | `cordis_inspect_list/query`（Creator 模式自检） |
| ralph | `ralph`（全新 agent 迭代循环） |
| agent-team | `spawn_teammate` `send_message` `list_agents` `interrupt_agent` `wait_agent` `team_task_*`（4 个） |

**差距诊断**：我们的工具系统是"**注册表**"，dsh 的是"**受保护执行流水线 + 能力 seam**"。差别在于——换掉 `ctx.fs` 的提供方（`fs-local` → `fs-sandbox` → `fs-ssh`），bash、PTY、LSP 一起搬过去，**消费方零改动**。而我们换沙箱是要把整个 agent 进程搬走（`runDocker`）。

**可借鉴**：
- **P1**：给 `tool.Registry` 加"来源无关的执行前/后钩子"（对应 `tools/pre-execute` / `tools/post-execute`），把审批、spill、审计、耗时统计从 `consume()` 里挪出来——现在这些逻辑和 SSE 事件翻译糊在一起（`runner.go` 1251 行的主要成因）。
- **P1**：补 `todo_write`（多步任务的进度外显，前端已有时间线，接一个即可）。
- **P2**：补文件类工具（`read/write/edit`）——目前只有项目会话下的 `save_file/list_files/read_file`，非项目会话完全没有文件能力。

### 3.5 多智能体：两条路径 vs 六提供方 + continuation

**本地**（`assembler.go:191-200` 按 `project.collab_mode` 分发）：

| 模式 | 实现 | 事件 |
|---|---|---|
| `agent_as_tool`（默认） | 成员包成 `adk.NewAgentTool` 挂进协调者工具列表（`:230`） | `subagent.enter/exit` |
| `transfer`（对照） | `adk.SetSubAgents(ctx, inst, subAgents)`（`:292`） | `subagent.enter{via:"transfer"}` |
| `single` | 单成员降级 | — |

`Project.WorkflowMode` 字段存在但**明确未实现**：

```go
// assembler.go:175-178
if p.WorkflowMode != "" && p.WorkflowMode != "free" {
    warns = append(warns, fmt.Sprintf("工作流模式 %q 暂未启用，按自由协作处理", p.WorkflowMode))
}
```

**dsh**（`ctx.subagents` seam）：

- **六提供方并存并按名注册**：`spawn-in-process`、`fork-in-process`、`acp`、`codex`、`claude-code`、`dsh-sdk`。
  - `fork` 会注入父级**平衡的已完成轮次前缀**作为种子；`spawn`/`acp` 不注入。
  - `inheritsParentContext` **仅描述种子注入**，明确"不暗示继承工具、服务或权限"。
  - **每个子 agent 获得一个新的扁平作用域，而非继承父级注册。**
  - 委派权限在首次 await 前捕获并**独立审查**；`delegationDepth` 持久在 `SessionHeader`，冷恢复无法降低深度。
  - 描述符事件只进日志，**绝不进入模型历史**，且跨压缩保留。
- **continuation 模型**（这是最值得学的一点）：

```text
persisted Session
  -> optional live Activation
       -> one retained AgentHandle
       -> Agent inbox as the only turn FIFO
       -> zero or more owned child Activations
```

  `startContinuable()` 预留稳定子 id → 向提供方索取分离的 `ContinuableCreateSpec` → 建子 Agent → 提交初始提示词 → **inbox 一接受就 resolve `{childId, messageId}`，不等轮次开始**。`sendMessage()` 按目标 Activation 状态路由：`running` → 在最近步骤边界 steer；`waiting` → 唤醒并 steer；无 Activation → 冷恢复再 steer。`interrupt()` 是唯一停止操作，发 `Agent.cancel(cause, {keepInbox:true})` 后**不等停稳就返回**。子级结算时向父级投递一条 `SubagentSettledMessageSource` 通知（"它留下了什么"），**用一个独立的 kind**，所以 transcript 绝不会把运行时的记账呈现成子级自己写的东西。

- **agent-team**（实验性）：隐式 Team Lead + 持久 teammate roster + peer mailbox + 共享任务 DAG（4 个 `team_task_*` 工具，带 revision 的 compare-and-set）+ `wait_agent`（不轮询，等状态/mailbox/任务变更）。
- **workflow**（PTC 编排）：模型写一段 JS，钩子是 `agent(prompt, opts?)` / `pipeline(items, ...stages)` / `parallel(thunks)` / `phase(title)` / `log(msg)`。脚本**没有文件系统、网络、定时器或 Node API**，具体工作全由 agent 完成。子级失败解析为 `null`（不是抛异常），配 `.filter(Boolean)`。

**可借鉴（分级）**：
- **P1（小）**：`workflow_mode` 空壳要么实现要么删字段——留着会误导。
- **P1**：引入"**可继续子 Agent**"概念——我们现在的 `AgentTool` 是一次性同步调用，协调者必须干等。dsh 的 `startContinuable` + `sendMessage` + `interrupt` 三件套，正好对应"委派给后台成员、继续干别的、回头收结果"的协作形态，且它天然复用同一套 `cancels` 机制。
- **P2**：workflow 脚本编排（`agent()/pipeline()/parallel()`）是"多智能体编排"成本最低的形态，比 Sequential/Parallel/Loop 的图编排更贴合 LLM。

### 3.6 沙箱：整进程搬迁 vs 进程级 argv 包装

**本地**（`internal/runtime`）：

```go
type Backend interface {
    Name() string                                          // docker|k8s|auto
    Start(ctx, StartSpec{AgentID, RunID, Memory, CPUs}) (Endpoint, error)
    Stop(ctx, StopSpec{AgentID, RunID}) error
    Status(ctx, agentID) (BackendStatus, error)
}
```

- docker：`docker run -d --name agt-{agentID} --memory=512m --cpus=1 -P {image}`，`-P` 随机映射后 `docker port` 取端点，healthz 轮询 ≤60s。
- k8s：`kubectl` CLI（无 client-go），Pod 清单经 stdin apply，端点模式 `port-forward`/`pod-ip`，healthz ≤90s，状态映射含 `CrashLoopBackOff/ImagePullBackOff`。
- auto：候选序 k8s→docker，**粘滞缓存 `lastGood`**。
- 作用域：`agent`（常驻复用）/ `run`（per-run，用后即清）。
- **容器内同构**：`cmd/agentd` 用**同一份** `chat.Assembler`，拉 manifest（一次性 token 5 分钟）→ 内存 SQLite → 强制 `RuntimeBackend="inprocess"` 防二次分发。

所以我们的沙箱粒度是 **Agent/Run 级**（整个 agent 进程搬进容器）。

**dsh**：沙箱是**进程级的 argv 包装**：

> 消费方交出即将执行 spawn 的确切 argv；与配套子进程提供方共享执行环境的后端按每次调用的策略包装该 argv，并报告强制执行情况。

而 **fs 与 process 提供方共享同一个执行世界**——把它们指向远程沙箱，bash、PTY、LSP 就一起搬过去了，**无需提供方专用 fork**。`ctx.sandboxPolicy` 统一保存部署默认模式和工作区根目录，"两类强制执行组件都读取该服务，因此 bash 与 fs 不会限制到不同的根目录"。

**评价**：这是**目标不同**造成的差异，不是优劣。我们是多租户平台，整进程搬迁的隔离强度更高、实现更省事（复用同一份代码）；dsh 是本机编码 agent，进程级包装的开销更小、能力组合更自由。**保持现状即可**，但建议把 `Backend` 接口里的 `Scoper`/`Prober` 也纳入 seam 化思考（现在已经做了：`runtime.Scoper` / `runtime.Prober`，方向是对的）。

### 3.7 技能：指令注入 vs 提供方目录 + 懒加载

- **本地**（`skill/composer.go`，63 行）：技能 = **指令注入 + 工具白名单**。`ComposeInstruction` 拼 `base + "\n\n# 启用技能\n" + <skill name="X">…</skill>`；`SkillTools` 取工具白名单并集。**全部正文全量注入**，无懒加载。会话级开关 `SkillsDisabled` → `skillGated` 返回 `Skills=[]` 的 Agent 副本。
- **dsh**：`ctx.skills` 是 **seam**（4 个提供方：`skill-filesystem`、`skill-badge`、`skill-office`、`sandbox-windows-acl`），服务合并各提供方的目录；`tool-skill` 渲染**会话前缀目录**（只有名字和描述），模型要用时调 `skill` 工具**加载完整正文**。

**可借鉴（P1，低成本高收益）**：把技能改成"**目录常驻 + 正文懒加载**"。当前全量注入在多技能挂载时会把 system prompt 撑爆——而我们已经没有任何上下文工程兜底。改法：`<skill>` 标签里只放 `name` + 一行描述，正文通过一个新工具 `load_skill` 按需取。

### 3.8 中断 / 恢复 / 人机协同

**本地其实是超额设计**，这块我们比 dsh 细：

```go
// 1. 工具侧挂起
compose.StatefulInterrupt(ctx, info, state)      // askhuman.go:68 / approval.go:75
schema.RegisterName[*AskHumanInfo](...)          // gob 具名注册，checkpoint 跨编解码需要

// 2. 平台侧捕获（runner.go:703-707 → handleInterrupted:535）
ii.InterruptContexts → 取 IsRootCause → 解析 chosen.Info 判 kind
  → 落 conversation.interrupt_state（:574）
  → 发 run.interrupted{kind, checkpoint_id, target_id, question, choices, tool_name, arguments}

// 3. 定向恢复（runner.go:809 Resume）
rt.Runner.ResumeWithParams(runCtx, st.CheckpointID, &adk.ResumeParams{
    Targets: map[string]any{st.TargetID: answer},   // 按 InterruptCtx.ID 定向投递
})

// 4. 工具重入（askhuman.go:54-62）
compose.GetInterruptState[*AskHumanState](ctx) + compose.GetResumeContext[string](ctx)
// 未被定向恢复则原样再中断，保持等待

// 5. 放弃（runner.go:585 abandonInterrupt）：新消息运行清 checkpoint + 状态，发 run.warning
```

**唯一硬伤**：`CheckPoints` 是 `NewMemCheckPointStore()`——**进程内内存，重启即失效**（代码注释诚实标注）。dsh 那边中断状态走的是持久 session 日志 + fork/resume，天然可跨重启。

**建议（P1）**：把 `CheckPoints` 换成 SQLite 持久化（`checkpoint` 表一行 + gob blob），成本 1 人日以内，直接消除"重启即丢"的诚实边界。

**dsh 侧可借鉴的一点**：`ctx.approval` 的 waterfall **没有回答方就以 `unavailable` 关闭失败，而不是悬空等待**。我们的审批工具在无人应答时的行为值得对照检查。

### 3.9 可观测与治理

| 维度 | 本地 | dsh |
|---|---|---|
| 事件流 | `run_event` 表 + 15 种 SSE 事件（`meta`/`run.started`/`skill.loaded`/`retrieval`/`message.delta`/`reasoning.delta`/`tool.call`/`tool.result`/`subagent.enter|exit`/`run.interrupted`/`run.warning`/`run.error`/`run.finished`…） | session 日志（持久）+ `agent/assistant-stream`（进程内实时）+ `session/event` |
| 调试 | 自制三级 `DebugLevel{0,1,2}` + `model.step` 事件（`debugmodel.go`） | `ctx.inspector`（跨 realm 运行时检查 + CDP target）+ Creator 模式 `cordis_inspect_*` |
| Trace | ❌ 无（otel 仅 indirect 依赖） | ✅ `ctx.otel` 共享 OTel 通道 + `ctx.sessionTelemetry` seam |
| 不变量检查 | ❌ 无 | ✅ `ctx.invariants`：包本地注册检查，服务负责选择/唯一性/子 fiber，"模型可见即已记录"是运行时不变量 |
| 日志 | `log.Printf` 34 处，无结构化、无级别 | — |
| 限流 | ❌ 无 | — |
| 重试 | ❌ 明确拒绝（"单次失败即降级"） | ✅ `agent/request-error` waterfall，监听器返回 `{kind:'retry'}` 即接管恢复 |
| 用量 | ✅ 从 `run_event` 反查聚合（`store/stats.go:48`，按 model/agent/project） | `ctx.tokenMeter` 按会话隔离的回放折叠区 |
| 审计 | ✅ PROV-O 导出（`/api/audit/prov-export`）+ 伴生图 prov 三元组 | session 日志即可回放 |
| 测试门禁 | 25 个测试文件 / 3047 行 | 逐文件 100% 覆盖门禁（多套 vitest config：unit/e2e/snapshot/bench/web/web-stress） |

**可借鉴（P2）**：`ctx.invariants` 的思路很划算——**把架构约束写成运行时检查**。我们的"模型可见即已记录"等价物可以是：每次 `runOnce` 结束时校验 `message` 表投影与 `run_event` 里的 `tool.call/tool.result` 数量一致，不一致就发 `run.warning`。几十行代码，能提前抓出双写漂移。

### 3.10 扩展机制

**本地**：加能力 = 改 Go 代码。已有 4 个来源（builtin/skill/mcp/ontology）+ MCP 反向暴露（`/mcp`，每个开启 `McpServe` 的 Agent → 一个 `agent_{id}` 工具，Bearer Token 鉴权），但**没有插件运行时**。

**dsh**：加能力 = 挂插件或写 patch。映射表（`architecture.zh.md`「新行为的归属位置」）：

| 目标 | 机制 |
|---|---|
| 添加模型提供方 | 在 `ctx.llm` 上注册适配器 |
| 添加面向模型的能力 | 在 `ctx.tools` 上注册；schema 自动进提示词组装 |
| 让某个会话拥有不同能力集合 | 组装 agent preset（服务行需 `isolate` realm） |
| 添加 shell 执行 | 注册 `ctx.shell` 后端 |
| 限制所启动的进程 | 用 `ctx.sandbox` 后端，消费方在启动前包装 argv |
| 拦截请求/工具/轮次 | 用 `agent/*` 或 `tools/*` 事件 |
| 添加模型可见上下文 | 调 `agent.inject()`，落到下一次获准的请求 |
| 在轮次边界 fork 会话 | `ctx.agents.create({sessionId, seed, meta})` |
| 将注册项限定到单个 agent | 使用该 agent 的 `agent.ctx` |

**诚实评价**：全面插件化会摧毁我们"配置即行为"的简洁性，**不建议照抄**。但 **`agent.inject()` 这一个 API 值得抄**——它是"任何插件都能往下一步骤注入模型可见上下文"的统一入口，正好解决我们现在 `recallKB` / `recallCompanion` 硬编码在 `runOnce` 里的问题。

---

## 4. 提炼：智能体开发架构的参考骨架

从两套实现抽象出的**十层参考模型**（可用于评估任何 agent 框架，也可作为我们自己演进的坐标系）：

| # | 层 | 职责 | 本地 | dsh |
|---|---|---|---|---|
| 1 | **Transport** | HTTP/SSE/RPC/CLI 入口 | ✅ net/http + SSE | ✅ web/headless/sdk/acp/桌面 |
| 2 | **Assembly** | 把配置组装成可运行实例 | ✅ `Assembler`（强项） | ✅ profile/bundle/patch |
| 3 | **Loop** | think-act-observe 循环 | ⚠️ 外包给 ADK | ✅ `ctx.agentLoop`（可替换） |
| 4 | **State** | 唯一真源与历史派生 | ⚠️ 双写表 | ✅ 仅追加事件日志 |
| 5 | **Context** | 压缩/剪枝/溢出/注入 | ❌ 无 | ✅ compaction/pruner/spill/offload |
| 6 | **Tools** | 注册表 + 受保护执行流水线 | ⚠️ 注册表 | ✅ seam + 事件门禁 |
| 7 | **Capability seams** | fs/shell/subprocess/sandbox/subagent/web/lsp… | ⚠️ 仅 sandbox | ✅ 20+ seam |
| 8 | **Multi-agent** | 委派/协作/编排 | ⚠️ 两条路径 | ✅ 六提供方 + continuation + team + workflow |
| 9 | **Interaction** | 审批/提问/计划/目标/任务 | ⚠️ 审批+ask_human | ✅ approval/ask-user/plan/goal/todo/jobs |
| 10 | **Observability** | 事件流/Trace/不变量/用量/审计 | ⚠️ 事件流 + 用量 + PROV-O | ✅ + OTel + invariants |

三条跨层纪律：
1. **唯一真源**：状态只写一份，其余全部派生。
2. **能力三角色**：定义 / 提供方 / 消费方分离——换实现不动消费方。
3. **模型可见即已记录**：任何进入模型上下文的东西都要可追溯、可回放。

---

## 5. 差距清单与建议优先级

| 级别 | 项 | 说明 | 量级 |
|---|---|---|---|
| **P0** | 历史不再丢 tool 消息 | `BuildHistoryMessages` 一行级改动，多步工具链可用性直接上台阶 | ~0.5 人日 |
| **P0** | 历史限量 + 超预算警告 | `ListMessages` 加 LIMIT/游标；`run_once` 前粗估 token，超预算发 `run.warning` | ~1 人日 |
| **P1** | CheckPoint 持久化 | `NewMemCheckPointStore` → SQLite 表，消除"重启即丢" | ~1 人日 |
| **P1** | 技能目录常驻 + 正文懒加载 | 避免多技能撑爆 system prompt | ~1.5 人日 |
| **P1** | 工具执行前/后钩子 | 把审批/审计/耗时从 `consume()` 剥离，给 `runner.go` 减重 | ~2 人日 |
| **P1** | `todo_write` 工具 | 前端时间线已具备，接一个即可 | ~0.5 人日 |
| **P1** | `workflow_mode` 空壳处置 | 实现或删字段，别留误导 | ~0.5 人日 |
| **P1** | 压缩 seam 骨架 | `Compactor` 接口 + LLM 摘要实现 + 被压轮次标记 | ~3 人日 |
| **P2** | 可继续子 Agent | `startContinuable`/`sendMessage`/`interrupt` 三件套 | ~5 人日 |
| **P2** | 工具结果剪枝 | 超长 tool.result 留 head/tail | ~1 人日 |
| **P2** | 文件类工具（read/write/edit） | 非项目会话目前无文件能力 | ~2 人日 |
| **P2** | `agent.inject()` 式统一上下文注入口 | 替代 `recallKB`/`recallCompanion` 硬编码 | ~2 人日 |
| **P2** | 运行时不变量检查 | 校验 `message` 投影与 `run_event` 工具计数一致 | ~0.5 人日 |
| **不推荐** | 全面插件化 / Cordis 式微内核 | 会摧毁"配置即行为"的简洁性，与平台定位冲突 | — |
| **不推荐** | 换掉 `eino/adk` 自研循环 | D-O5 开源优先；ADK 已是托管执行器，自研无增益 | — |

---

## 6. 附：dsh 关键事实速查

| 项 | 值 |
|---|---|
| 定位 | DeepSeek 官方开源 agent harness（MIT），CLI 名 `dsh` |
| 内核 | Cordis（"时空可组合性"编程范式，arXiv 2608.25512）；**无特权内核** |
| 公式 | `Agent = Model + Harness` |
| 仓库规模 | monorepo：`apps/`（CLI + Web + 桌面）、`packages/`（60+ 可组合包）、`python/`（SDK wheel）、`native/`、`vendor/` |
| 版本 | 0.2.0-rc.1（2026-09-28 发布；本平台 `31_智能体对接DeepSeek-Harness_可行性及方案分析.md` 锁定 0.1.5-rc.3 作外部 CLI 后端） |
| 四种模式 | Standard / PTC（模型写 TS 编排）/ Minimal（bash + str_replace_editor，基准测试）/ Creator（运行时自检 + 插件实验） |
| Profile | `web` `headless` `sdk` `sdk-minimal` `acp`；组合包 `dsh-base` 为共享第一层 |
| 核心包 | `core/session`、`core/system-prompt`、`core/tools`、`core/agent`、`core/agent-loop`、`core/scope`、`llm/llm`、`webhook/webhook` |
| 会话事件 | 13 核心变体 + `session/end-seed` 谱系标记；插件可声明合并扩展（如 `compaction/*`、`hook/*`） |
| 事件域 | 会话事件（持久事实）/ `agent/*`（进行中工作）/ 能力事件（`fs/*`、`tools/*`、`telemetry/*`） |
| 本平台接入 | `internal/inference/adapters.go:262-274` 第 4 个 `cliAdapter`（`dsh --profile headless <task>`，answer-once-and-exit）；凭据 dsh 侧自管 |

### 相关资料

- [DeepSeek Harness 官方仓库](https://github.com/deepseek-ai/deepseek-harness)（MIT，developer preview）
- 官方文档：`docs/architecture.zh.md`、`docs/capability-seams.zh.md`、`docs/subsystems/{core,session,compaction,subagent}.zh.md`、`docs/tool-catalog.zh.md`、`docs/agent-lifecycle.zh.md`
- 本站既有：[31_智能体对接DeepSeek-Harness_可行性及方案分析.md](31_智能体对接DeepSeek-Harness_可行性及方案分析.md)（对接/嵌入可行性及方案选型，REQ-160 已交付现状与 ACP/SDK 深化建议）
- 本站既有：[22_多类型智能体方案研究](../../02_智能体/08_调研预研/22_多类型智能体方案研究.md)（REQ-142）
- [docs/02_智能体_技术方案设计.md](../../docs/02_智能体_技术方案设计.md) §6.16（推理后端可插拔契约与能力矩阵）
