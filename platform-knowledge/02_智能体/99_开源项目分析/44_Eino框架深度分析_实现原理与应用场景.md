---
module: 智能体
topic: CloudWeGo Eino 实现原理深度拆解与应用场景/能力地图
desc: 源码级剖析（schema/components/compose/ADK 四层）+ 应用场景选型 + 能力四档成熟度分级（基础面/被忽略/新近演进/官方不推荐）
synced: 2026-09-29
---

# CloudWeGo Eino 深度分析：实现原理与应用场景

> 分析对象：**Eino**（CloudWeGo / 字节跳动开源的 Go 语言 LLM 应用框架），版本 **v0.9.19**
> 一手证据：本地克隆仓库源码实读（commit `9d983b3`，该 commit 同时被打上 `v0.9.19` 与 `v0.10.0-alpha.29` 两个 tag）+ 官方文档 cloudwego.io/docs/eino + eino-ext 生态资料。文中所有 `路径:行号` 均指 Eino 仓库内的相对路径。
> 文档定位：**框架原理研究报告**，同时是一份**能力地图**。第 6 章把 Eino 的全部能力按「基础面 / 已具备但常被忽略 / 新近演进 / 官方不推荐」四档分级，便于读者对照自身现状评估哪些技术点已经用上、哪些还值得引入。
> 相关文档：`31_智能体对接DeepSeek-Harness`、`36_智能体开发架构深度对比`、`37_AI应用工程范式演进`、`41_DeepSeek-Harness_实现方案深度调研`。

---

## 0. 结论前置

**三句话概括 Eino：**

1. **它是一个「类型对齐优先」的编排内核，不是一个 Agent 框架。** Agent（ADK）是长在编排内核之上的一层薄封装——`ChatModelAgent` 在构造期就把「ChatModel → Branch → ToolsNode → 回边」这张固定拓扑编译成了一张真实的 compose 图（`adk/react.go:354-562`、`adk/chatmodel.go:1157-1184`）。
2. **它把三件最容易写脏的事做成了内核能力**：流式范式自动转换（组件只实现自己有意义的范式，上下游不匹配由框架补）、类型安全（编译期反射比对 + 运行时兜底转换）、中断与恢复（checkpoint 落盘的是「通道快照 + 待跑节点输入 + 状态」，不是 goroutine）。
3. **它的复杂度集中在编排层，Agent 层反而刻意做薄**：`TypedAgent[M]` 接口只有 `Name / Description / Run` 三个方法（`adk/interface.go:453-464`）。多智能体、上下文工程、可靠性这些"厚能力"被拆到 **Handlers 中间件**、**prebuilt 预置**、**TurnLoop** 三处，且官方明确标注了若干"NOT RECOMMENDED"路线。

**最容易被低估的三处：**

- `BeforeModelRewriteState` / `AfterModelRewriteState`（`adk/handler.go:171,181`）：这是 Eino 官方为**上下文压缩、历史裁剪、动态工具过滤**留的位置。很多团队自己在外面拼 prompt，其实框架内就有标准钩子。
- `adk/middlewares/` 下已经躺着 **summarization（历史摘要）、reduction（工具结果裁剪 + 上下文卸载）、patchtoolcalls（修补悬空 tool call）、toolsearch（动态工具检索）、plantask（任务清单）、filesystem（文件后端 + 大结果卸载）、skill、agentsmd** 八个开箱中间件。
- `TurnLoop`（`adk/turn_loop.go`）是一套 **push-based 抢占式长时运行循环**（安全点抢占、优雅停止、checkpoint 落盘），这是"Loop 工程"在 Eino 里的正式形态，与大多数人认知中的"ReAct for 循环"完全不是一个东西。

---

## 1. 定位与分层

### 1.1 四层结构

```
┌─────────────────────────────────────────────────────────┐
│ ADK（Agent 开发套件）  adk/                              │
│   ChatModelAgent / DeepAgent / PlanExecute / Supervisor  │
│   Runner · TurnLoop · flow · AgentTool · middlewares      │
├─────────────────────────────────────────────────────────┤
│ compose（编排）        compose/                          │
│   Graph / Chain / Workflow · Branch · State              │
│   CheckPoint · Interrupt · Callbacks · 流式自动转换       │
├─────────────────────────────────────────────────────────┤
│ components（组件抽象）  components/                       │
│   ChatModel · Tool · Retriever · Embedder · Indexer       │
│   Loader · Transformer · ChatTemplate                    │
├─────────────────────────────────────────────────────────┤
│ schema（类型与流）      schema/                           │
│   Message · AgenticMessage · StreamReader · ToolInfo      │
└─────────────────────────────────────────────────────────┘
```

**关键事实：ADK 依赖 compose，compose 不依赖 ADK。** `adk/chatmodel.go:37` 直接 import compose，ReAct 循环是用 `compose.NewGraph` / `compose.NewChain` 编译出来的（`adk/react.go:366`、`adk/chatmodel.go:1157-1184`）。这意味着：**在 Eino 里，Agent 是一种特殊的图，图不必是 Agent。**

### 1.2 仓库分工

| 仓库 | 职责 |
|---|---|
| `cloudwego/eino` | 类型定义、流机制、组件抽象、编排、Agent 实现、切面 |
| `cloudwego/eino-ext` | 组件实现（OpenAI/Claude/Gemini/Ark/Ollama/ES/Milvus/Qdrant/MCP…）、回调处理器（Cozeloop / APMPlus / Langfuse / Langsmith）、评估器、提示优化器 |
| `eino-ext/devops` | 可视化开发与调试（IDE 插件 / 可视化编排 / 可视化调试） |
| `cloudwego/eino-examples` | 示例与最佳实践 |

注意：**可视化不在 core 仓库**。core 只产出 `GraphInfo` 结构化拓扑（`compose/introspect.go:41-52`），渲染由 devops 侧消费。

### 1.3 坐标系：一句话对比

| 框架 | 主控范式 | 关键差异 |
|---|---|---|
| **Eino** | 类型对齐的图 + 组件一等公民 | 编译期反射类型校验；流式范式自动派生；checkpoint 落"通道快照"而非 goroutine；Go 泛型贯穿 |
| **LangGraph** | 状态机 + 持久化执行 | 与 LangChain 生态强绑定；Python 为主；`State` 是中心概念，Eino 的 State 是旁路（数据边才是主通道） |
| **Google ADK** | Agent 树 + 移交 | Eino ADK 借鉴其形态，但底层换成自己的 compose；`transfer_to_agent` 在 Eino 中已被官方标注 NOT RECOMMENDED |
| **Temporal** | 持久化工作流 | Eino 不做跨进程持久执行，checkpoint 需业务方自实现 `CheckPointStore` |

---

## 2. 底座：schema 与 components

### 2.1 Message 与 AgenticMessage 双轨

`schema.Message`（`schema/message.go`）是经典 OpenAI 形态：

```go
type Message struct {
    Role       RoleType        // system / user / assistant / tool
    Content    string
    ToolCalls  []ToolCall      // 仅 assistant
    ToolCallID string          // 仅 tool
    ToolName   string          // 仅 tool
    ReasoningContent string    // 思维链
    ResponseMeta *ResponseMeta // 含 TokenUsage
    UserInputMultiContent / AssistantGenMultiContent // 多模态
    ...
}
```

`schema.AgenticMessage`（`schema/agentic_message.go:63-83`）是**为多智能体设计的第二轨**：
- `Role AgenticRoleType` 只有 **system / user / assistant** 三种；
- 工具调用与工具结果**不再是独立消息**，而是 `ContentBlocks` 里的 `function_tool_call` / `function_tool_result` 块（`schema/agentic_message.go:52-53`）。

**为什么需要第二轨？** 多智能体场景下，一条 `tool` 消息"属于谁"是模糊的——父 Agent 调子 Agent，子 Agent 调工具，工具结果回到谁的上下文？AgenticMessage 把工具交互折叠进 assistant 消息内部，从类型上消掉了这个歧义。

代价写在源码注释里（`adk/interface.go:66-68,448-452`）：**AgenticMessage 不支持 agent transfer，且 cancel 监控与 retry 在该路径尚未接入**（`adk/chatmodel.go:421-424`）。所以用 AgenticMessage 之前要清楚：换来的是多智能体语义清晰，丢掉的是一部分可靠性能力。

### 2.2 StreamReader：四种流式范式

| 范式 | 输入 | 输出 | 构造器 | 类比 |
|---|---|---|---|---|
| Invoke | 非流 | 非流 | `compose.InvokableLambda()` | Ping-Pong |
| Stream | 非流 | 流 | `compose.StreamableLambda()` | Server-Streaming |
| Collect | 流 | 非流 | `compose.CollectableLambda()` | Client-Streaming |
| Transform | 流 | 流 | `compose.TransformableLambda()` | Bidirectional |

**组件只需实现有业务意义的范式。** 官方组件里只有 ChatModel 与 Tool 额外实现了 Stream，其余（Retriever / Embedder / Indexer / Loader / Transformer / ChatTemplate）只实现 Invoke（`components/*/interface.go`）。编排时缺失的范式由 `newRunnablePacker` 自动派生（`compose/runnable.go:345-409`：`invokeByStream` = 跑流再 concat、`streamByInvoke` = 结果包成单元素流、`collectByInvoke` = 先 concat 输入、`transformByInvoke` = 输入 concat + 输出装箱）。

### 2.3 组件接口清单

| 组件 | 接口 | 说明 |
|---|---|---|
| `model.ChatModel` | `Generate / Stream` | `components/model/interface.go:80` |
| `model.BaseModel[M]` | 泛型基（含 Agentic） | `:36` |
| `model.ToolCallingChatModel` | 带 WithTools 约束 | `:99` |
| `tool.BaseTool` | `Info(ctx) *schema.ToolInfo` | 只要元信息即可交给模型决策 |
| `tool.InvokableTool` | `InvokableRun(ctx, json) (string, error)` | `:42` |
| `tool.StreamableTool` | `StreamableRun → StreamReader[string]` | `:53` |
| `tool.EnhancedInvokableTool` | 入参 `ToolArgument`，返回 `ToolResult`（多模态） | `:67` |
| `tool.EnhancedStreamableTool` | 流式多模态结果 | `:76` |
| `retriever.Retriever` | `Retrieve(ctx, query) []*Document` | `components/retriever/interface.go:48` |
| `embedding.Embedder` | 向量化 | |
| `indexer.Indexer` | 写入向量库 | |
| `document.Loader / Transformer` | 加载与切分转换 | `:43,:53` |
| `prompt.ChatTemplate` | 提示词模板 | `:43` |

工具构造推荐用 `components/tool/utils`：`NewTool[T,D]`（由 Go 结构体反射推导 JSON Schema，`invokable_func.go:143`）、`NewStreamTool`、`NewEnhancedTool`（多模态，`:248`），以及 `WrapToolWithErrorHandler`（把工具错误转成"可重试的模型可见信息"，`error_handler.go:42`）。

> **要点**：`BaseTool` 只要求 `Info`。这意味着「**只给模型看、不真执行**」的工具（用于引导模型输出结构化意图）是一等支持的一等公民。

---

## 3. 编排内核 compose

### 3.1 数据结构

```go
type graph struct {
    nodes      map[string]*graphNode
    controlEdges map[string][]string   // 控制流边
    dataEdges    map[string][]string   // 数据流边
    branches   map[string][]*GraphBranch
    startNodes / endNodes []string
    toValidateMap map[string][]*edgeInfo  // 待校验类型
    stateType / stateGenerator
    expectedInputType / expectedOutputType
    buildError error                   // 任一 add* 失败即固化
    ...
}
```
（`compose/graph.go:57-89`）

三个设计点值得注意：

1. **控制边与数据边是分开存的两张表**（`graph.go:59-60`），这是 Workflow 能"控制流与数据流分离"的物理基础。
2. `START = "start"` / `END = "end"` 是**普通字符串常量**（`graph.go:37,40`），不是真实节点（不允许 addNode，`graph.go:177-179`），编译时才为它们构造伪 `chanCall`。
3. `buildError` 一旦置位，compile 直接返回——**图是"一次成型、不可修补"的**，编译产物不可变。

**Pregel vs DAG**（`graph.go:43-50`）：默认 **Pregel**（允许环、AnyPredecessor 触发）；仅当显式 `WithNodeTriggerMode(AllPredecessor)` 或组件是 Workflow 时切 DAG。Chain/Workflow 禁止传 nodeTriggerMode（`graph.go:682-686`）。

### 3.2 类型对齐（Eino 的核心卖点）

Eino 官方对编排的核心主张是：**「编排应建立在类型对齐之上」**。实现落在三个地方：

**（1）加边即校验**（`graph.go:232-294`）：`addEdgeWithMappings` 只有数据边才进校验流水线，用 `checkAssignable` 判定三态（`compose/utils.go:233-253`）：

| 判定 | 含义 | 处理 |
|---|---|---|
| `Must` | 类型相同或目标实现源接口 | 直接连通 |
| `May` | 源是接口、目标实现它（或多态） | **在边上挂运行时转换器** `inputConverter`（`graph.go:596-602`） |
| `MustNot` | 不匹配 | 立即报错：`graph edge[%s]-[%s]: start node's output type[%s] and end node's input type[%s] mismatch` |

**（2）延迟校验的不动点传播**：`toValidateMap`（`graph.go:65-68`）记录尚未定型的边——典型场景是 Passthrough 节点的类型还没确定。`updateToValidateMap()`（`graph.go:561-637`）用 `for { hasChanged }` 反复扫描：两端都 nil 就继续留着；一端已知就反向传播给 Passthrough 定型；都已知就走 `checkAssignable`。

**（3）编译期的最后兜底**：compile 开始时若 `toValidateMap` 非空，报 `some node's input or output types cannot be inferred`（`graph.go:708-713`）。

> **理解要点**：Eino 的类型检查**绝大多数发生在 addEdge/addBranch 当下**，而非编译期。编译期只做兜底。把类型错误前移到"写图的那一行"，这是它相对"运行时才发现字段对不上"的框架的实质优势。

### 3.3 编译流程（`graph.go:674-892`）

1. 判定 runType / eager / channelBuilder（680-699）
2. 校验 startNodes / endNodes 非空（701-706）
3. `toValidateMap` 残留检查（708-713）
4. 字段映射收敛：重复目标字段报错，挂 `inputFieldMappingConverter`（715-727）
5. 子图回调预埋 → 逐节点 `compileIfNeeded`（**递归编译子图**）→ 构造 `chanCall` → `chanSubscribeTo`（729-757）
6. 反向构建控制/数据前驱索引（759-797）
7. 组装 runner 与 successors（814-840）
8. 启用 State 时生成 `runCtx`，用 parent 指针串联父图 state（842-854）
9. DAG 模式 `validateDAG`，有环报 `DAGInvalidLoopErr` 并打印环路径（856-862、1077-1182）
10. 构建 checkPointer（864-873）；**Pregel 下默认 `maxRunSteps = 节点数 + 10`**，DAG 下不允许设置（881-885）
11. 置 `compiled=true`，触发 `onCompileFinish`，返回 `composableRunnable`

产物是 `Runnable[I,O]`，提供 Invoke / Stream / Collect / Transform 四入口（`compose/runnable.go:32-37,166-190`）。

### 3.4 执行引擎

**不是"每节点一个常驻 goroutine + 点对点 channel"，而是"全局 channel 集合 + 任务管理器 + 编排主循环"。**

- 主循环 `runner.run`（`graph_run.go:108-381`）：每轮检查 ctx.Done → 检查 step 上限 → `tm.submit(nextTasks)` → `tm.wait()` 取回完成集 → `calculateNextTasks` → 命中 END 返回。
- **一个 task 一个 goroutine**：`taskManager.execute` 中 `go t.execute`（`graph_manager.go:288-302,352`），panic 由 recover 转成 `safe.NewPanicErr`（289-297）。
- 完成队列是无界 channel `internal.UnboundedChan[*task]`（`graph_manager.go:278`）。
- **超步（superstep）**：Pregel 非 eager 时 `needAll = true`，走 `waitAll()`，`tm.num` 归零才算一个超步；叠加 `pregelChannel.get`「有值就 ready，取后清空」（`pregel.go:55-60`）→ 天然支持回边，即天然支持 ReAct 循环。
- DAG 走 `dagChannel`，必须等所有控制前驱 `dependencyStateReady` 且数据前驱就绪（`dag.go:128-157`）；被分支跳过的节点通过 `reportSkip` 传播跳过（`dag.go:106-126`）。
- **环的兜底**：Pregel 不做 DAG 校验，靠 `step >= maxSteps` 报 `ErrExceedMaxSteps`（`graph_run.go:259-261`）。
- **分支路由**：`calculateBranch` 执行 `branch.invoke`/`branch.collect` 得选中节点（`graph_run.go:987-1052`）；多分支冲突以被选中者为准（1034-1045）；未选中节点传播跳过（1047）。
- **错误传播**：普通错误包装为节点错误返回（`wrapGraphNodeError`，`graph_run.go:516`），**不会取消其它并行任务**；外部取消走 `WithGraphInterrupt` 注入的 `graphCancelSignal`。
- **fan-in 合并**：`mergeValues`（`values_merge.go:39-82`），map 按 key 合并且重复 key 报错，流类型走 `StreamReader.merge`。

### 3.5 State：旁路共享状态

```go
type internalState struct {
    state  any
    mu     sync.Mutex
    parent *internalState   // 指向父图 state
}
```
（`compose/state.go:34-38`）

- 并发安全由**一把 Mutex 覆盖整个用户闭包**：`ProcessState[S](ctx, handler)`（`state.go:165-173`）。
- **子图可访问父图 state**：`getState[S]` 沿 parent 链按类型线性查找（`state.go:175-196`），实现词法作用域语义。
- 前后处理器：`WithStatePreHandler` / `WithStatePostHandler`（`state.go:42-82`），挂在 `chanCall.preProcessor/postProcessor`，由 taskManager 在 task 生命周期内执行；**节点出错时 postHandler 直接跳过**（`graph_manager.go:625-638`）。
- 流式版本 `WithStreamStatePreHandler/PostHandler` 保持真流式而不 concat（`state.go:84-112`）。

**为什么需要 State？** 因为 Runnable 的 I/O 只能沿数据边走。`*Message` → `[]*Message` 这类无法对齐的旁路数据（历史消息、计数器、跨分支中间结果）只能靠共享 state 交换。**State 是对"类型对齐"这条铁律的官方逃生舱。**

### 3.6 流式：concat 与 copy

**Concat（流 → 值）**规则（`compose/stream_concat.go:50-95`、`internal/concat.go`）：
- map 递归按 key 合并
- string 拼接
- 数值 / bool / time 取**最后一个**
- 未知结构类型若有多个非零值 → 报错 `cannot concat multiple non-zero value of type`
- 空流 → `emptyStreamConcatErr`
- 可自定义：`RegisterStreamChunkConcatFunc`（`stream_concat.go:44-46`）

**Copy（一份流 → 多个下游）**（`graph_run.go:1141-1161` → `schema/stream.go:792-821`）：底层是一条**共享链表** `cpStreamElement`，每个 child 自己维护读位置；任一 child close 只减计数，全部 close 才关闭父流。这是"一份模型流同时给多个下游 + 多个回调 handler"而不复制数据的关键。

### 3.7 中断 / Checkpoint / 恢复（重点）

这是 Eino 最独特的一块，也是人机协同（Human-in-the-Loop）的实现基础。

**三类中断源：**
1. **编译期声明**：`WithInterruptBeforeNodes` / `WithInterruptAfterNodes`（`interrupt.go:31-42`），命中于 `graph_run.go:232-244,519-524`
2. **节点内主动抛出**：`Interrupt(ctx, info)` / `StatefulInterrupt(ctx, info, state)`（`interrupt.go:110-137`），底层生成带 uuid ID 与当前 `Address` 的 `InterruptSignal`
3. **外部取消**：`WithGraphInterrupt` 触发 `graphCancelSignal`（`graph_call_options.go:78-103`）

**checkpoint 里存什么**（`checkpoint.go:108-119`）：

| 字段 | 内容 |
|---|---|
| `Channels` | 所有 channel 的待消费值快照 |
| `Inputs` | 待执行 / 待重跑节点的原始输入 |
| `State` | 图 state 深拷贝 |
| `RerunNodes` | 需要重跑的节点 |
| `SubGraphs` | 嵌套子图 checkpoint |
| `InterruptID2Addr` / `InterruptID2State` | 中断信号树拍平成两表 |

> **核心洞察**：`convertCheckPoint`（`checkpoint.go:326-347`）在写盘前把所有 channel 值与待跑节点输入**用 concat 转成值**；恢复时反向 `restoreStream` 把值重新包成单元素流（`checkpoint.go:369-385,439-458`）。
> 也就是说：**Eino 不保存 goroutine，只保存"通道快照 + 待跑节点输入 + state"，恢复时在新的 goroutine 里重建 task。** 这是它能做跨进程恢复的根本原因，也是代价所在——流被压成值，恢复后重新变成流。

**定向恢复**：compose 层 API 是 `Resume(ctx, interruptIDs...)` / `ResumeWithData(ctx, id, data)` / `BatchResumeWithData(ctx, map[string]any)`（`resume.go:94-121`）。每次进入节点/工具都调用 `AppendAddressSegment` 累加地址段（`resume.go:150-152`）：
- 用当前 address 去 `id2Addr` 反查 interruptID → 命中则取出 `id2State` 作为 `interruptState`
- 消费 `id2ResumeData[id]` → 置 `resumeData` + `isResumeTarget=true`
- 若自身未被指定但**存在以自己为前缀的后代地址**，也置 `isResumeTarget=true` 以便向下透传（`resume.go:175-184`）

组件侧读取 `GetInterruptState[T]` / `GetResumeContext[T]`（`resume.go:32-34,77-79`）。

**ToolsNode 的中断协作**（`tool_node.go`）：单个工具返回 Interrupt 时，把该工具的 `callID` 记为 rerun 目标并附上 `AddressSegment{Type: tool, ID: callID}`（1111-1113），最后 `CompositeInterrupt` 聚合（1140）。**已成功工具的结果被 concat 保存**，恢复时由 `GetInterruptState[*toolsInterruptAndRerunState]` 还原输入并**跳过已执行工具**（1061-1069、793-818）。

这个细节很关键：一次工具批里 5 个工具，第 3 个要求人工审批，恢复时**前 2 个不会重跑**。

### 3.8 Chain 与 Workflow

**Chain** 是 Graph 的语法糖（`chain.go:72-82`）：`addNode` 自动把上一个节点的 key 连到新节点，compile 时补 END 边后委托 `gg.compile`。节点 key 自动命名 `node_{idx}` / `node_{idx}_parallel_{i}` / `node_{idx}_branch_{key}`。

**Workflow** 的真正差异在两点：

1. **字段级映射**：`FieldMapping`（`field_mapping.go:31-37`）+ `FromField/ToField/MapFields`（`:65-90`）。路径支持嵌套 struct/map 任意层级（用不可见分隔符 `\x1F` 拼接，`:125-152`）。这让"f1 的输出字段 F1 直接喂给 f2 的输入字段 F3"成为可能，**无需为了对齐类型而改造业务函数签名**。
2. **控制流与数据流分离**：`AddDependency` 只建控制边、`WithNoDirectDependency` 只建数据边、普通 `AddInput` 两者都建（`workflow.go:316-367`）。同一目标字段被两条路径写入会报 `two terminal field paths conflict`（`:369-404`）。

代价：**Workflow 不支持环**（强制 DAG，`graph.go:687-690`），触发模式固定 AllPredecessor。

选型判据很直接：
- 要在节点间精确搬运字段、且是 DAG → **Workflow**
- 需要循环（ReAct、迭代收敛）→ **Graph（Pregel）**
- 只是线性管道 → **Chain**

### 3.9 自省与可视化

`GraphInfo`（`introspect.go:41-52`）导出 CompileOptions / Nodes / Edges / DataEdges / Branches / InputType / OutputType；`GraphNodeInfo`（`:27-36`）含 Component / Instance / InputType / OutputType / GraphInfo（嵌套）。子图通过 `beforeChildGraphsCompile` 把自己的 GraphInfo 回填到父节点（`graph.go:928-946,1001-1003`），从而得到**递归的嵌套拓扑**。

core 仓库**没有** mermaid/graphviz 导出代码；渲染在 `eino-ext/devops`。

---

## 4. ADK：Agent 抽象与运行时

### 4.1 Agent 接口只有三个方法

```go
type TypedAgent[M MessageType] interface {
    Name(ctx context.Context) string
    Description(ctx context.Context) string
    Run(ctx context.Context, input *TypedAgentInput[M],
        opts ...AgentRunOption) *AsyncIterator[*TypedAgentEvent[M]]
}
```
（`adk/interface.go:453-464`）

`GetType()` 只在具体类型上通过 `components.Typer` 反射取值；`OnSetSubAgents / OnSetAsSubAgent / OnDisallowTransferToParent` 是**独立的可选接口**（`:474-479`），由装配层探测调用。

**事件流模型**：`Run` 返回 `AsyncIterator`（无界 channel，`adk/utils.go:31-58`），Agent 在独立 goroutine 推送事件。事件种类由字段组合表达：

| 事件语义 | 字段标志 |
|---|---|
| 模型输出消息 | `Output.MessageOutput`，Role=Assistant |
| 工具结果 | Role=Tool |
| 错误 | `Err != nil` |
| 中断 | `Action.Interrupted` |
| 移交 | `Action.TransferToAgent` |
| 退出 | `Action.Exit` |
| 跳出循环 | `Action.BreakLoop` |
| 自定义 | `CustomizedOutput` / `CustomizedAction` |

`RunPath []RunStep`（`interface.go:377-379`）构成 agent 调用栈，是定位"这个事件来自哪个 agent"的关键。

### 4.2 ReAct 循环是编译出来的一张图

`TypedChatModelAgent` 构造期用 `sync.Once` 一次性构建（`chatmodel.go:1368`）：
- 无工具 → `buildNoToolsRunFunc`（`:1002`），一条 `compose.NewChain` 挂 Lambda→Model
- 有工具 → `buildReActRunFunc`（`:1097`）→ `buildMessageReActRunFunc`（`:1115`）

后者的真实拓扑在 `adk/react.go:354-562`：

```
START → Init → ChatModel ──Branch(toolCallCheck)──┬→ terminal(END)
                    ↑                              ↓
                    │                          CancelCheck
                    │                              ↓
                    └──── AfterToolCallsCancelCheck ← AfterToolCalls ← ToolNode
```

- 分支用 `NewStreamGraphBranch(toolCallCheck, ...)`（`:499-517`）——**逐 chunk 读流，出现 ToolCalls 立即走工具分支**，不必等流结束。这是流式分支的价值所在。
- **回边**是 `afterToolCallsCancelCheckNode_ → chatModel_`（`:558`）。
- **最大迭代次数**：`typedState.RemainingIterations`，**默认 `maxIter = 20`**（`:345-349`），在 ChatModel 节点的 `StatePreHandler` 里递减，≤0 返回 `ErrExceedMaxIterations`（`:390-397`）。
- `compose.WithMaxRunSteps(math.MaxInt)` 放开了 compose 层的步数限制（`chatmodel.go:1169,1176`）——即**Agent 的循环上限由 ADK 自己管，不由 compose 的超步上限管**。

### 4.3 ToolsConfig

```go
type ToolsConfig struct {
    compose.ToolsNodeConfig          // Tools / UnknownToolsHandler / ExecuteSequentially / ToolArgumentsHandler / ToolCallMiddlewares
    ReturnDirectly     map[string]bool
    EmitInternalEvents bool
}
```
（`adk/chatmodel.go:136-156`）

- **ReturnDirectly**：命中的工具结果直接成为 Agent 终态，不再回灌模型（生效点 `adk/react.go:528-556`）。框架自动把 `transfer_to_agent` 与 exit 工具设为 returnDirectly（`chatmodel.go:891,900`）。
- **EmitInternalEvents**：子 Agent 事件实时透传给终端用户，但**不记入父 runSession/checkpoint**（`chatmodel.go:143-155`）。
- **参数修复链**（`tool_node.go:819-862`）：工具名查不到 → `UnknownToolsHandler` → 别名重映射 `remapArgs` → `ToolArgumentsHandler`。模型写错工具名/参数名时，这里是标准补救位。
- **并行/串行**：默认并行（`parallelRunToolCall`，`tool_node.go:985-1017`），`ExecuteSequentially` 切串行。

### 4.4 Handlers：上下文工程的官方落点

现行接口是 `TypedChatModelAgentMiddleware[M]`（`adk/handler.go:139-255`），共 8 个方法：

| 钩子 | 能改什么 | 典型用途 |
|---|---|---|
| `BeforeAgent`（`:142`） | Instruction / Tools / ReturnDirectly / ToolSearchTool | 动态装配、按用户改系统提示 |
| `AfterAgent`（`:158`） | 只读终态 | 结果审计、记忆写入 |
| **`BeforeModelRewriteState`（`:171`）** | `State.Messages / ToolInfos / DeferredToolInfos` | **历史压缩、裁剪、动态工具过滤** |
| **`AfterModelRewriteState`（`:181`）** | 同上 | 后处理、脱敏 |
| `WrapInvokableToolCall`（`:193`） | 工具调用包装 | 重试、审批、缓存 |
| `WrapStreamableToolCall`（`:205`） | 同上（流） | |
| `WrapEnhancedInvokable/StreamableToolCall`（`:217,:229`） | 多模态工具 | |
| `WrapModel`（`:254`） | 模型实例 | 重试 / failover / 自定义流处理 |

旧的 `AgentMiddleware` struct（`chatmodel.go:239-257`）**已标记 Deprecated**。

**模型侧包装顺序**（`chatmodel.go:323-335`）：BeforeChatModel → BeforeModelRewriteState → failover → retry → eventSender → WrapModel → callback → Model。
**工具侧顺序**（`:356-363,533-554`）：eventSender → ToolCallMiddlewares → AgentMiddleware.WrapToolCall → Handlers → callback 注入 → cancel 监控。

> 这一节是"上下文工程"在 Eino 里的正确答案：不要在外面拼 prompt，用 `BeforeModelRewriteState`。

### 4.5 Runner

```go
runner := adk.NewRunner(ctx, adk.RunnerConfig{
    Agent: agent, EnableStreaming: true, CheckPointStore: store,
})
iter := runner.Run(ctx, msgs, adk.WithCheckPointID("id"))
iter2, err := runner.ResumeWithParams(ctx, "id", &adk.ResumeParams{
    Targets: map[string]any{...},
})
```

- `Run` / `Query`（包一层 UserMessage）/ `Resume` / `ResumeWithParams`（`runner.go:102-149`）
- `typedRunnerHandleIterImpl`（`:271-342`）：逐事件转发；遇 `CancelError` → 标记 + 落 checkpoint + 终止；遇 `Action.internalInterrupted` → 转成公开 `InterruptInfo`（带 `InterruptContexts`）+ **落 checkpoint**（`:310-338`）
- `EnableStreaming` 决定走 `runnable.Stream` 还是 `Invoke`；**恢复时以 checkpoint 中保存的值为准**（`:216-220`）
- `CheckPointStore` 是接口（`core.CheckPointStore`），**框架不提供持久化实现**，业务方自己接 DB / Redis / 文件

### 4.6 多智能体的四条路

| 路线 | 机制 | 官方态度 |
|---|---|---|
| **AgentTool**（`agent_tool.go:93`） | 把 Agent 包成 Tool 给父 Agent 调用 | ✅ 推荐 |
| **Prebuilt**（`prebuilt/deep`、`planexecute`、`supervisor`） | 开箱模式 | deep/planexecute 主流；supervisor 标 NOT RECOMMENDED |
| **flow + transfer_to_agent**（`chatmodel.go:641`） | 模型输出"转交给谁"，框架切 agent | ❌ **NOT RECOMMENDED**（`interface.go:311-318`） |
| **Workflow Agent**（`workflow.go:694/703/712`） | Sequential / Parallel / Loop 确定性编排 | ⚠️ 标 NOT RECOMMENDED（`:630-632`） |

**AgentTool 是"上下文防火墙"**（`agent_tool.go:154-280`）：内部起独立 `TypedRunner` + `bridgeStore` 跑子 Agent，**只取最后一个事件的文本作为工具返回值**（`:268-279`）。子 Agent 的中间过程默认不进父 Agent 历史。可选 `WithFullChatHistoryAsInput()`（`:50-54`）把父的完整历史喂进去。

**flow 的 transfer 是单向串接**：`flowAgent.run`（`flow.go:481-582`）发现 `TransferToAgent != nil` 就直接跑目标 agent 并转发事件（`:560-581`），**不回到原 agent**。历史改写默认把其它 agent 的 assistant/tool 消息重写成 `"For context: [X] said ..."` 的 user 消息（`:188-253`）——这是防止多智能体上下文污染的默认行为。

**deterministic transfer**（`deterministic_transfer.go:43-54`）：不看 LLM 输出，当前 agent 事件流结束后**强制追加** TransferToAgent action 依次转到指定 agent。用于确定性编排。

**三种 Workflow Agent**（`workflow.go`）：
- `NewSequentialAgent`：按序跑，中断保存 `InterruptIndex`，恢复从该 index 续跑（`:104-106,258-277`）
- `NewParallelAgent`：`forkRunCtx` 开独立 lane + WaitGroup 并发，`joinRunCtxs` 按时间戳合并事件（`runctx.go:423-508`）
- `NewLoopAgent`：外层 `maxIterations` 循环（0=无限），子 agent 可发 `BreakLoopAction` 提前跳出

### 4.7 TurnLoop：Loop 工程的正式形态

`TurnLoop[T, M]`（`adk/turn_loop.go:879-931`）是一套 **push-based 事件循环**，与"ReAct for 循环"不同层：

- **推入与抢占**：`Push(item, WithPreempt(SafePoint) / WithPreemptTimeout / WithPreemptDelay / WithPushStrategy)`（`:1381,1225,1243,1262`）
- **安全点**：`SafePoint = AfterChatModel | AfterToolCalls | AnySafePoint`（`:1056-1067`）——即"什么时候允许被打断"
- **停止策略**：`Stop(WithGraceful / WithImmediate / WithGracefulTimeout / UntilIdleFor / WithStopCause / WithSkipCheckpoint)`（`:1097-1188`）
- **主循环**（`:1570-1737`）：`tryLoadCheckpoint` → `buffer.Receive()` → `beginPlanningTurn` → `planTurn` → `PrepareAgent` → `runAgentAndHandleEvents` → 遇 `CancelError`/`InterruptError` 落 checkpoint 退出
- 抢占状态机 `preemptController`（`:197-370`）：idle → planning → active → idle

**这是"长时运行、可被打断、可恢复、可持续接收外部事件"的 Agent 的标准做法**：外部系统往 loop 里 push 事件（用户新消息、定时任务、webhook），loop 在安全点抢占当前 turn，而不是等一轮跑完。

### 4.8 取消与可靠性

**取消三模式**（`cancel.go:54-65`）：`CancelImmediate` / `CancelAfterChatModel` / `CancelAfterToolCalls`。`WithAgentCancelTimeout` 在安全点超时后升级为 immediate 并**仍保存 checkpoint**（`:114-123`）；`WithRecursive()` 向 checkpoint-aware 子边界传播（`:149-153`）。

安全点的物理落点就是 ReAct 图里的 `CancelCheck` 与 `AfterToolCallsCancelCheck` 两个节点（`react.go:402-413,476-485`）——**取消不是"kill goroutine"，而是"在图里埋检查点"**。

**重试**（`retry_chatmodel.go:222-281`）：默认退避 base 100ms、指数增长、上限 10s、加 0-50% 抖动。
**Failover**（`failover_chatmodel.go:128-167`）：可配 `ShouldFailover` + `GetFailoverModel`，优先重试"上次成功模型"。

### 4.9 官方中间件与预置

**`adk/middlewares/` 八个开箱件：**

| 中间件 | 能力 | 关键配置 |
|---|---|---|
| `summarization` | 历史压缩/摘要 | 默认 token 阈值 160k |
| `reduction` | 工具结果裁剪 + 上下文卸载 | Truncation 默认 50000；Clear 默认 160000 |
| `patchtoolcalls` | 修补历史中悬空的 tool call | — |
| `toolsearch` | 动态工具检索（模型先检索再调用） | — |
| `plantask` | 注入 TaskCreate/Get/Update/List 四个任务工具 | — |
| `skill` | 技能加载 | fork / fork_with_context 两种子 agent 上下文模式 |
| `filesystem` | ls/read/write/edit/glob/grep/execute + 大结果自动卸载 | — |
| `agentsmd` | 自动注入 Agents.md 作为系统提示 | — |

**`adk/prebuilt/`：**
- `deep`：DeepAgent——write_todos + task 工具编排子 agent + fs/shell 后端
- `planexecute`：plan → execute → replan，含 `NewPlanner` / `NewExecutor` / `NewReplanner`
- `supervisor`：Supervisor 分发模式（**标 NOT RECOMMENDED**）

`adk/filesystem/` 是**可插拔文件后端协议**（`Backend` = LsInfo/Read/GrepRaw/GlobInfo/Write/Edit + 可选 Shell），内置 `InMemoryBackend`。它给 reduction/filesystem 中间件提供"卸载到磁盘再按需读回"的存储面。

---

## 5. 应用场景地图

| 场景 | 推荐层 | 具体装配 |
|---|---|---|
| **纯对话/问答** | ADK ChatModelAgent（无工具） | 一条 Chain，成本最低 |
| **工具调用助手** | ADK ChatModelAgent + ToolsConfig | ReAct 自动循环，maxIterations 默认 20 |
| **RAG 问答** | compose Graph | Retriever → ChatTemplate → ChatModel；或用 ChatModelAgent + Retriever 工具 |
| **知识入库** | compose Graph/Workflow | Loader → Transformer → Embedder → Indexer，天然 DAG 可并行 |
| **人机审批 / 人工补信息** | Interrupt + Resume | 工具内 `StatefulInterrupt`；`GetInterruptState` 恢复上下文；已成功工具不重跑 |
| **确定性业务流程** | compose Workflow | 字段级映射，不改造业务函数签名 |
| **确定性 + 自主决策混合** | graphtool | 把编译好的 Graph 包成 Tool 给 Agent 调用 |
| **多智能体分工** | AgentTool（父调子） | 子 agent 只回摘要，天然上下文防火墙 |
| **复杂任务分解** | prebuilt/deep 或 planexecute | 带 todos / planner-executor-replanner |
| **长时运行 / 后台 Agent** | TurnLoop + CheckPointStore | push 事件 + 安全点抢占 + graceful stop |
| **批处理 / 数据管道** | compose Graph（DAG） | 并行 fan-out + mergeValues |
| **可观测治理** | callbacks | Cozeloop / APMPlus / Langfuse / Langsmith |
| **平台化（把编排当产品能力）** | GraphInfo 自省 + devops | 导出拓扑做画布/调试器 |

**反模式提醒：**
- ❌ **不要用 Graph 表达 ReAct**——ADK 已经编译好了；自己画图只会失去 ReturnDirectly、cancel 安全点、Handlers 等能力。
- ❌ **不要默认用 LLM 驱动的 transfer**——官方已标 NOT RECOMMENDED，改用 AgentTool（把委派变成一次工具调用，可控、可中断、可观测）。
- ❌ **不要在 Workflow 里试图做循环**——强制 DAG，用 LoopAgent 或 Pregel Graph。
- ❌ **不要为了类型对齐去改业务函数签名**——那是 Workflow 字段映射要解决的问题。

---

## 6. 能力四档成熟度分级（采用地图）

> 说明：以下分级基于源码标注（Deprecated / NOT RECOMMENDED 注释）、能力稳定性与生态成熟度。**同一能力在不同档位意味着不同的引入风险。**

### A 档 · 基础面（稳定，广泛使用）

| 能力 | 坐标 | 备注 |
|---|---|---|
| `schema.Message` / `StreamReader` | `schema/message.go` | 事实标准 |
| `ChatModel` / `BaseChatModel` | `components/model` | eino-ext 有 OpenAI/Claude/Gemini/Ark/Ollama 等 16 个实现 |
| `tool.BaseTool` + `utils.NewTool` | `components/tool` | 反射推导 JSON Schema |
| MCP 工具接入 | `eino-ext/components/tool/mcp` | 接任意 MCP Server |
| `adk.NewChatModelAgent` | `adk/chatmodel.go:503` | ReAct 自动循环 |
| `adk.NewAgentTool` | `adk/agent_tool.go:93` | 子 agent 作为工具 |
| `adk.NewRunner` | `adk/runner.go:89` | Run/Query/Resume |
| Interrupt / Resume | `compose/interrupt.go` | 人机协同 |
| compose Chain / Graph | `compose` | 顺序与 DAG |

### B 档 · 已具备但常被忽略（低风险、高收益）

| 能力 | 坐标 | 为什么值得补 |
|---|---|---|
| **`BeforeModelRewriteState`** | `adk/handler.go:171` | 上下文压缩/裁剪的标准位置 |
| **`middlewares/summarization`** | `adk/middlewares/summarization` | 开箱历史摘要，阈值可配 |
| **`middlewares/reduction`** | `adk/middlewares/reduction` | 工具结果裁剪 + 卸载磁盘 |
| **`middlewares/patchtoolcalls`** | `adk/middlewares/patchtoolcalls` | 修悬空 tool call（历史被裁剪后必现的问题） |
| **compose State / ProcessState** | `compose/state.go:165` | 跨节点旁路数据，含子图作用域 |
| **Callbacks** | `callbacks/` | 五切点，接 Cozeloop/Langfuse/APMPlus |
| **retry / failover** | `adk/retry_chatmodel.go` / `failover_chatmodel.go` | 开箱退避与模型切换 |
| **cancel 安全点** | `adk/cancel.go:54-65` | 优雅取消 + 仍落 checkpoint |
| **graphtool** | `eino-ext` | 图 → 工具，混合确定性与自主性 |
| **GraphInfo 自省** | `compose/introspect.go:41` | 做画布/调试器的原料 |
| `WrapToolWithErrorHandler` | `components/tool/utils/error_handler.go:42` | 工具错误转成模型可理解的重试信息 |

### C 档 · 新近 / 演进中（需评估后引入）

| 能力 | 坐标 | 注意事项 |
|---|---|---|
| **AgenticMessage 双轨** | `schema/agentic_message.go` | 多智能体语义清晰，但**不支持 transfer、cancel/retry 未接入** |
| **TurnLoop** | `adk/turn_loop.go` | push-based 长时运行循环；API 面大，建议先小范围试点 |
| **deterministic transfer** | `adk/deterministic_transfer.go:43` | 确定性移交，比 LLM transfer 可控 |
| **prebuilt/deep（DeepAgent）** | `adk/prebuilt/deep` | 官方文档主推，但能力面重，定制成本高 |
| **prebuilt/planexecute** | `adk/prebuilt/planexecute` | 适合长链路任务 |
| **`middlewares/toolsearch`** | `adk/middlewares/dynamictool` | 工具数量爆炸时的解法 |
| **`middlewares/skill`** | `adk/middlewares/skill` | 技能加载，含 fork/fork_with_context |
| **`middlewares/filesystem` + `adk/filesystem`** | — | 需要真实文件后端时（默认只有 InMemory） |
| `EnhancedTool`（多模态工具结果） | `components/tool/interface.go:67` | 工具要返回图片/文件时 |

### D 档 · 官方标注 NOT RECOMMENDED（谨慎）

| 能力 | 坐标 | 官方替代 |
|---|---|---|
| `transfer_to_agent` 工具 | `adk/chatmodel.go:641`，标注见 `interface.go:311-318` | ChatModelAgent + AgentTool 或 DeepAgent |
| `workflow.NewSequential/Parallel/LoopAgent` | `adk/workflow.go:630-632` | compose Workflow / Graph |
| `prebuilt/supervisor` | `adk/prebuilt/supervisor` | DeepAgent |
| `AgentMiddleware` struct | `adk/chatmodel.go:235-257`（Deprecated） | `TypedChatModelAgentMiddleware` 接口 |
| `WithHistoryModifier` | `adk/chatmodel.go:119`（Deprecated） | `ResumeWithData` + `ChatModelAgentResumeData` |

---

## 7. 风险与注意事项

1. **版本仍在 v0.x，API 有迁移**。已有两处 Deprecated 迁移（`AgentMiddleware` struct → Handlers 接口；`WithHistoryModifier` → `ResumeWithData`）。升级前必看 release note。
2. **`CheckPointStore` 需自行实现持久化**。框架只给接口 + gob 序列化。跨版本 state 结构变更要用 `MigrateCheckpointState`（`compose/checkpoint.go:285-298`）。
3. **两个"上限"要心里有数**：Agent 的 `maxIterations` 默认 20（`adk/react.go:345-349`）；Pregel 图的 `maxRunSteps` = 节点数 + 10（`compose/graph.go:884`）——但 ADK 内部把它放开到 `math.MaxInt`，所以 Agent 循环不受此限。
4. **流式中断会"压成值再恢复"**。中断点正在流的输出会被 concat 成完整值后落盘，恢复时重新包成流。对超长工具输出，这是内存与延迟的成本。
5. **并行节点的错误不会互相取消**（`graph_run.go:516`），只作为该节点错误返回。需要"任一失败即整体失败"要自己在 fan-in 处处理。
6. **State 是全局一把锁**。`ProcessState` 的 Mutex 覆盖整个用户闭包，闭包里做重活会成为并行瓶颈。
7. **可视化不在 core**。要画布/调试器需接 `eino-ext/devops` 或基于 `GraphInfo` 自研。
8. **AgenticMessage 路径能力不全**（cancel/retry/transfer），选型时要确认自己需要哪些。
9. **`ErrorExceedMaxIterations` / `ErrExceedMaxSteps` 是兜底不是策略**。生产环境应配合 middleware 做真正的成本控制（token 预算、调用次数预算）。

---

## 8. 与知识密集型系统（RAG / 本体 / 图谱）的结合点

Eino 本身不管知识，但留了几个标准化的插槽：

- **`Retriever` 接口**（`components/retriever/interface.go:48`）：`Retrieve(ctx, query) ([]*Document, error)`。任何检索后端——向量库、ES、**图数据库 / SPARQL / 本体推理**——只要实现这一个方法就能进编排，与 ChatModel 平级成为一等节点。
- **`Indexer`**：写入侧对偶接口。
- **`document.Loader / Transformer`**：文档加载与切分，可承载结构化（RDF/OWL/JSON-LD）到 `Document` 的归一化。
- **`graphtool`**：把"确定性检索管线"包成工具交给 Agent——**这是把本体查询这种强约束流程交给 LLM 调度的正确姿势**，而不是让 LLM 自己写 SPARQL。
- **`DeferredToolInfos`**（`adk/chatmodel.go:216-230` 的 State 字段之一）：配合 `toolsearch` 中间件做"工具太多时先检索再加载"，同样适用于"本体属性/关系太多时的按需暴露"。
- **checkpoint 与审计**：Eino 的 checkpoint 是运行态快照（通道值 + 节点输入 + state），**它不是审计日志**。要溯源需要自行在 callbacks 里外挂。

---

## 9. 一手证据索引（源码坐标）

| 主题 | 坐标 |
|---|---|
| 图结构 | `compose/graph.go:57-89` |
| START/END | `compose/graph.go:37,40` |
| Pregel/DAG 判定 | `compose/graph.go:43-50,680-690` |
| 类型校验三态 | `compose/utils.go:233-253`、`compose/graph.go:592-602` |
| 延迟校验不动点 | `compose/graph.go:561-637` |
| 编译主流程 | `compose/graph.go:674-892` |
| 执行主循环 | `compose/graph_run.go:108-381` |
| 任务管理器 | `compose/graph_manager.go:257-302,352` |
| 超步 / Pregel channel | `compose/pregel.go:55-60`、`graph_manager.go:361-387` |
| DAG 通道 | `compose/dag.go:128-157` |
| State | `compose/state.go:34-38,165-196` |
| 流 concat | `compose/stream_concat.go:50-95`、`internal/concat.go` |
| 流 copy | `graph_run.go:1141-1161`、`schema/stream.go:792-821` |
| 中断三类源 | `compose/interrupt.go:31-42,110-137`、`graph_call_options.go:78-103` |
| checkpoint 结构 | `compose/checkpoint.go:108-119` |
| 流↔值落盘转换 | `compose/checkpoint.go:326-347,369-385` |
| 定向恢复 | `compose/resume.go:94-121,150-184` |
| ToolsNode 中断协作 | `compose/tool_node.go:819-862,1061-1069,1140` |
| Workflow 字段映射 | `compose/field_mapping.go:31-90,125-152` |
| 控制流/数据流分离 | `compose/workflow.go:316-367` |
| 拓扑自省 | `compose/introspect.go:27-52`、`graph.go:948-1009` |
| Agent 接口 | `adk/interface.go:453-464` |
| AgenticMessage | `schema/agentic_message.go:52-53,63-83` |
| ReAct 图拓扑 | `adk/react.go:354-562` |
| 回边 | `adk/react.go:558` |
| 最大迭代 20 | `adk/react.go:345-349,390-397` |
| ToolsConfig | `adk/chatmodel.go:136-156` |
| Handlers 八钩子 | `adk/handler.go:139-255` |
| Runner | `adk/runner.go:89-149,271-342` |
| AgentTool（上下文防火墙） | `adk/agent_tool.go:93-104,154-280,268-279` |
| flow transfer（NOT RECOMMENDED） | `adk/flow.go:481-582`、`interface.go:311-318` |
| 确定性转移 | `adk/deterministic_transfer.go:43-54` |
| Workflow Agent | `adk/workflow.go:694,703,712` |
| TurnLoop | `adk/turn_loop.go:879-931,1056-1067,1570-1737` |
| 取消三模式 | `adk/cancel.go:54-65,114-123` |
| retry / failover | `adk/retry_chatmodel.go:222-281`、`failover_chatmodel.go:128-167` |
| 官方中间件 | `adk/middlewares/{summarization,reduction,patchtoolcalls,plantask,skill,filesystem,dynamictool,agentsmd}` |
| 预置 agent | `adk/prebuilt/{deep,planexecute,supervisor}` |
| 可视化归属 | `README.zh_CN.md` DevOps 段、`llms.txt:95-99` |

---

## 10. 一句话总结

Eino 的价值不在"又一个 Agent 框架"，而在于**它把 LLM 应用里最容易写脏的三件事——流式、类型、中断恢复——做成了内核语义**。Agent 只是它上面一层很薄的封装。真正决定上限的是：有没有用 `BeforeModelRewriteState` 做上下文工程、有没有用 `CheckPointStore` 做跨会话恢复、有没有用 `AgentTool` 而不是 `transfer` 做多智能体、有没有把确定性流程交给 `compose` 而把自主判断留给 `Agent`。
