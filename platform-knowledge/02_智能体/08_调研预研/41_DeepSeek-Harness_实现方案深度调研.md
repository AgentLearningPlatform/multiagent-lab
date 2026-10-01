---
module: 智能体
topic: DeepSeek Harness 实现方案深度调研
desc: 从源码与包级文档拆解 dsh 的循环/会话/上下文/工具/多智能体/沙箱实现，以及它的工程质量体系（100% 覆盖率门禁、快照回放、防御式模式）
synced: 2026-09-30
---

# DeepSeek Harness（dsh）实现方案深度调研

> **✅ 立项注记（2026-09-30）**：本档唯一独立残留建议已立项——**REQ-210 中断恢复失败步骤合成工具结果**（§4.8/§14 TOOL_OUTCOME_UNKNOWN 实践，挂 M42，优先随 REQ-201/M37 同轮领取）；§11/§12/§14 工程质量三条流程性建议归 20 号冒烟清单迭代吸收，不占 REQ。
>
> **调研对象**：`deepseek-ai/deepseek-harness@master`（MIT，TypeScript，Cordis 插件内核）。
> **方法**：逐篇读官方 `docs/` 子系统与 `packages/*/README.zh.md`（包级实现文档含"源码地图"章节），而非只读 README 概述。
> **与邻档分工**：
> - [`31_智能体对接DeepSeek-Harness_可行性及方案分析`](../31_智能体对接DeepSeek-Harness_可行性及方案分析.md) —— 对接/嵌入可行性判定与方案选型（平台侧如何接、走哪条通道）。
> - [`36_智能体开发架构深度对比`](../36_智能体开发架构深度对比_本地实现与DeepSeekHarness.md) —— 本地实现 vs dsh 的十层对照与差距优先级（差什么、先做哪个）。
> - **本档** —— 实现层：代码怎么组织、关键机制怎么落地、工程质量怎么保证、哪些纪律可迁移。
>
> 凡官方文档未给出的数值，一律标注「文档未给出」，不臆测。

---

## 0. 一页纸结论

1. **产品是一棵配置树，不是一个程序。** `dsh --profile web --dump-config` 能打印出真实生效的插件树；任何一行都能被 patch 整体替换。运行形态由 profile + bundle + 三层 patch 决定。
2. **循环是插件，不是内核。** `ctx.agentLoop` 只是配置树里的一行；`dsh-agent-loop` 是公开 `Agent` 约定的**唯一**具象实现，而消费方只依赖 `dsh-agent`。
3. **会话日志是唯一真源，模型历史是投影。** 每个派生的消息都能从日志重建；运行时有不变式在检查这一点。
4. **失败被设计成"可检测的状态"，不是"静默的降级"。** 压缩用 `compaction/start|end` 事件对当锁；沙箱 fail-closed；工具结果缺失会补合成结果并写明"结果未知，只可重试只读/幂等操作"。
5. **上下文工程是一等子系统**：一个测量服务（token meter）为所有决策定价；先剪枝、再摘要；摘要复用热前缀以省 KV cache。
6. **工具执行是固定六段流水线**，策略全部挂在事件上，`pre-execute` 禁止改写参数（否则日志与执行会失同步）。
7. **多智能体三粒度**（一次性/可继续/工作流脚本）共用同一个 subagent 注册表，深度与容量有硬上限。
8. **沙箱只管文件效果**，且明确区分 `full` / `partial` 强制执行——不夸大安全承诺。
9. **工程质量是本档最贵的一段**：`packages/*/*/src` 按文件 **100% 行覆盖门禁**、快照回放、真实 API 冒烟、构建产物冒烟、代码生成新鲜度门禁、防御式模式清单。
10. **最可迁移的不是某个工具，而是"把影响写进文档"的纪律**：每个包 README 必须写「模型看到什么 / Token 影响 / KV Cache 影响」三段。

---

## 1. 技术栈与工程骨架

| 项 | 实现 |
| --- | --- |
| 语言 | TypeScript（运行时 Node 22.19+ / 24 / 26，CI 三版本矩阵） |
| 包管理 | pnpm（仓库在 `package.json` 固定 `pnpm@11.7.0`，经 Corepack 解析）+ workspace |
| 插件框架 | **Cordis**（以 vendor 方式引入，`vendor/README.md` 管同步） |
| 构建 | `tsc -b` 先发射 `lib/types` → `tsdown` 分 **Host / Client 两阶段**打包（`DSH_BUILD_FACE`）→ Desktop bundle → Web 构建 |
| 类型反射 | **Typert**：以 `tsconfig.host.json` 为种子生成 Host 反射产物与 Host-for-Client Remote 投影；Client 阶段不启动 |
| 客户端 RPC | `api/remotes` + `ctx.remote` / `agentCtx.remote`（`@Remote` / `@RemoteScope` 装饰） |
| 产物形态 | CLI(`apps/cli`)、Desktop(Electron，`apps/desktop`)、Web(`apps/web`)、SDK(JSON-RPC)、ACP server |
| 代码生成 | `gen-cordis-catalog`（服务/事件目录）、`gen-module-graph`（依赖图）、`gen-doc-graphs`（时序图）、i18n 双语配对 |

**Host / Client 必须拆成两个 tsconfig aggregate 的原因**：两侧在相同键下对 cordis `Context` 接口做声明合并，单 program 同时看到两份合并会冲突（冲突只存在于 `ts.Program` 内部，模块解析不会触发）。因此：`tsconfig.base.json` 永不加 `include`/`files`；构造全仓 program 的脚本必须以某个 aggregate 为种子；新包只登记进一个 aggregate。

---

## 2. 组装范式：配置树即产品

```
profile（web / headless / sdk / sdk-minimal / acp）
  └─ 按序应用：bundle₁ … bundleₙ → profile 的 cordis.patch.yml → home 级 patch → --patch overlay
```

- **组合包（bundle）** 只是一份静态 patch 文档——`dsh-base` 的实体就是 `cordis.patch.yml` 里一个 `insert` 列表：不挂载服务、不发事件、不持有可变状态，每条插入行所属的包负责自己的不变式。
- **patch 语义**：按 `id` 定位条目并**替换其整个 config**，不做合并。所以覆盖时必须重述每个想保留的设置。
- **平台门控**用 `!!js` 表达式节点：`bash-sandbox` / `tool-bash` 带 `disabled: !!js process.platform === 'win32'`，`pwsh-sandbox` / `tool-pwsh` 取反——每台机器恰好一套 shell 栈。
- **`dsh-base` 提供什么**：DeepSeek 模型连接、完整工具集（文件编辑/shell/web 搜索/公开 HTTP(S) 抓取/subagent/任务与目标）、跨重启存活的持久会话、默认权限策略（写限工作区 + 危险操作征询）。MCP 资源服务统一挂载一次，但没有配置 MCP 客户端的作用域拿不到 MCP 工具。
- **默认文件编辑工具**是 `read`/`write`/`edit`；`str_replace_editor` 需显式 insert 启用。
- **遥测默认**：OTel 会话上传对所有用户默认 `FEEDBACK_ONLY`（提交文本反馈时释放截至该事件的标准会话日志前缀），可设 `DISABLED`。
- **硬约束**：在沙箱化 `ctx.fs` 提供方之上再挂普通 `fs-local` 会重复注册同一服务，profile **拒绝加载**——二选一。
- **HMR**：base 启用仅监视配置的 `dsh-hmr`；headless/SDK/ACP 禁用；`sdk-minimal` 不含。
- `sdk-minimal` 是刻意的例外：自带完整显式 SDK 配置树，**不应用** `dsh-base`。

---

## 3. 内核实现：Cordis 五个原语

| 原语 | 实现要点 |
| --- | --- |
| 插件 = 服务对象 | 函数插件具名导出 `name` / `inject` / `Config` / `apply`、**无 default 导出**；服务包 default 导出服务类。**混用两种形式会让 Loader 丢弃函数插件的命名空间**（有 postmortem 事故记录） |
| 上下文 = 服务容器 | 稳定 `ctx.<key>`；可选服务用 `ctx.get(name)`，属性代理对拓扑敏感 |
| `inject` 声明依赖 | 加载顺序由依赖表达；注册是 `ctx.effect()` 的可逆副作用 |
| 类型化事件 | 声明合并注册事件名；五种分发模式 `emit` / `waterfall` / `parallel` / `serial` / `bail` |
| 可逆注册 | 每个注册都要有 disposer；有 teardown 顺序要求的放进同一个 effect |

**包级硬约定（packages/AGENTS.md，节选）**：

- `src/types.ts` **只放类型**，不放运行时代码；测试放包级 `tests/`，不是 `src/__tests__/`。
- 服务定义要为**所有当前消费方**设计；反模式是"只有一个内部调用方的公开方法"（应改为私有能力闭包）。
- **公开选择要有证据**：可配置性本身不能证明一个不受支持的默认值/公开操作集/外部概念合理。
- **面向模型的契约要从模型视角写**：提示词、schema、结果、诊断里只出现任务相关概念，不出现 UI/传输/实现词汇。
- **决策要在做出它的操作里强制执行**：schema 省略、提示词过滤、facade、wrapper、监听器顺序都不是强制——必须通过执行器测试拒绝路径。
- **边界要应用到完整结果**：字节/token/条数/时间上限要在"完整发出或保留的值（含 wrapper 与元数据）已知"处施加，并测试极小值、精确值、超大单块、多字节字节上限。
- **只在提交点发布状态**：通知与派生状态在操作成功后才发出；缓存、提示词、UI 回声、回放、查询视图都从同一个权威源派生。
- **注册表贡献必须证明可撤销**：每个注册表都有 HMR 安全测试——dispose 掉 fiber 并断言移除。
- **spec 并发跑在 fork 出的 worker 里**：端口/路径/子进程都不隔离，只有进程隔离；"只有单独跑才通过"的 spec 视为 spec 的缺陷。

---

## 4. 主循环实现（`packages/core/agent-loop`）

### 4.1 源码地图

| 文件 | 职责 |
| --- | --- |
| `src/index.ts` | 插件入口：`AgentLoop` 服务、配置 schema、声明式 agent 启动、工厂注册 |
| `src/agent.ts` | 具体 `ReactLoopAgent` 驱动器：收件箱、轮次/步骤状态机、取消 |
| `src/inbox.ts` | `ReactLoopInbox`：持久投影、结构化命令、仅供循环使用的领取状态 |
| `src/tool-calls.ts` | 工具调度：独占屏障与有界并行池 |
| `src/runtime-context.ts` | 逐步骤运行时上下文快照 |
| `src/constants.ts` | `DEFAULT_MAX_PARALLEL_TOOL_CALLS` |
| `src/invariant.ts` | 不变式配套：**从会话日志重建请求** |

### 4.2 关键配置

| 字段 | 默认 | 含义 |
| --- | --- | --- |
| `maxParallelToolCalls` | `10` | 每步骤同时在途的并行安全调用；`1` = 串行。在**下一个工具组开始时**读取（volatile 字段） |
| `agents[].id` | 必填 | 未设 `sessionId` 时生成 `${id}-session-<uuid>` |
| `agents[].sessionId` | — | 确切身份：首次创建，重挂载恢复 |
| `agents[].resumeSessionId` | — | 加载已有持久会话；与 `sessionId` 互斥 |

声明式 agent 在插件加载时自动启动；配置标签没有逐 agent persona 字段或 setup 钩子（只有编程式 `ctx.agents.create()` 支持带作用域 persona）。

### 4.3 创建与拆除是一个可回滚事务

构造私有会话 → 具象 agent → 带作用域上下文 → 等待可选 setup → 进入两个注册表 → 宣告 `session/created` → 等待串行 `agent/created` 监听器 → 释放已排队输入。
**setup / commit / 监听器失败 / 所有者 dispose 都会回滚**。
Teardown 顺序：停止并排空驱动器 → 撤销作用域 → 关闭会话写路径 → detach agent → detach 会话。每次 detach 绑定到确切进入的对象，陈旧 disposer 无法移除后来的同 id 替代项。

### 4.4 持久化集成点

循环是**会话写句柄在生产环境的获取点**：

- `create` → `persistence.create(header)`（发布前存储持久身份并取得写所有权）；
- `resume` → `persistence.open(id, 'write')`（排除并发恢复）→ 读物理日志 → 若轮次中途崩溃，**把 `interruptedTurnClosers` 作为普通批次追加**（崩溃修复是 agent 层职责，不是存储层）；
- 发布前最后一刻 `appendUnstoredSuffix` 存储 setup 窗口期间追加的事件（seed 标记、委派策略记录），它们**绝不**经 `session/event` 重新发出；
- 没有后端时会话只存在于内存，其余一切不变。

### 4.5 请求构建：header / freeze / context

- `agent/request` 返回后 `ctx.llm.prepareCall()` 校验适配器持有字段并解析推理强度与输出 token 默认值；循环在解析、记录 header、分派期间**保留同一个适配器**。
- 写完整 header 的时机：首次请求、envelope 变化（配置或工具——**提示词不属于 header**）、显式消息序列起点、surface 替换或图片省略后的请求、恢复。同一序列内的步骤/重试/普通后续轮次**继承**最新 header。
- **请求冻结**：每个派生消息对象首次进入请求时深冻结，仅在同一 agent 内复用该证明；恢复的消息保留对象身份；构造请求不冻结事件包装对象（保留取消信号可变性）。
- `request/context`（provider / model / contextWindow / `systemPromptUpdate` 模式）**仅在其中任何一项变化时**记录，且在提示词与用户消息准入**之后**记录实际已准备调用的模式——它不参与准入决策。

### 4.6 提示词 surface 准入（最微妙的一段）

渲染后的提示词是 `system/message` surface 节点，**不是 header 状态**。因此提示词变更 = 替换/追加一个系统节点：

- 首个追加是 **surface 第 0 号节点**（即使提示词为空也追加，预留但不产生协议消息）；
- **不具备能力的路由 / 新请求序列**：非空文本归并到首个系统节点，每个非空后续节点记录一次"空内容替换"；
- **延续中的 `in-history` 序列**：非空变更**追加到已缓存历史之后**（保住 KV cache 前缀）；
- **空渲染文本**：逐个节点记录空内容替换，清除所有生效系统节点——模型不会继续看到旧指令；
- surface 折叠**拒绝**任何覆盖第 0 号节点的替换，除非替换事件本身就是恰好覆盖该节点的 `system/message`；后续系统节点不受保护，可被压缩遮蔽。
- 开启新请求序列的条件：pre-step 声明 `startsRequestSeries`，或 `surface.contentGeneration` 变化（替换/图片省略）；仅当路由未声明 `toolUpdate` 时，工具 schema 变化才强制归并提示词。

### 4.7 取消

- `agent.cancel(cause, { keepInbox? })`：中止当前活动，未设 `keepInbox` 时清除待处理工作。
- **取消提交规则**：`agent/request` waterfall 期间或 `prepareCall()` resolve 期间取消 ⇒ **system 与 users 都不提交**。
- 实时 `AbortSignal.reason` 是调用方对象；`turn/end` 里记录的是一份**拷贝**（保留 `kind` 与 hook 的 `reason` 文本），既避免把调用栈写进日志，也让结束事件保持可追加。
- 取消后未分发的工具调用：补一条合成 `tool/call` + `ABORTED_BEFORE_DISPATCH` 结果对。

### 4.8 失败兜底：失败步骤的工具结果

关闭失败步骤**之前**，驱动器为每个尚无结果的 assistant 工具调用补结果（共享 `ToolCallRecovery`）：

| 情况 | 结果 | 给模型的文本 |
| --- | --- | --- |
| 无 `tool/call` 记录 | `TOOL_NOT_STARTED` | `The tool call was interrupted before the Harness recorded it as started. Retry it if it is still needed.` |
| 有 `tool/call` 无结果 | `TOOL_OUTCOME_UNKNOWN` | `…Its outcome is unknown. Decide whether to retry from the tool semantics: retry only if the operation is read-only or idempotent; if it may have side effects, first verify external state or ask the user. Do not retry blindly.` |

目的：让后续请求拿到**配对完整**的工具历史，而不是自动重试结果不明的操作。

### 4.9 已知限制（官方自陈）

- 并行分类是**一元的**：安全性取决于比较同级调用或资源的调用必须保持独占。
- **没有内置轮次预算**——限制失控轮次必须从 `agent/turn-stopping` 等扩展点自行取消。（即：主循环 max steps **文档未给出**，因为它根本不存在。）
- 配置标签默认对应新会话；恢复必须显式给稳定 `sessionId`。

---

## 5. 会话与持久化实现

### 5.1 `dsh-session` 源码地图

| 文件 | 职责 |
| --- | --- |
| `src/index.ts` | `SessionStore` 服务、存储生命周期、`fork`、`flush` |
| `src/types.ts` | `SessionEventMap` / `SessionEvent` / `UserMessage` / `SessionHeader` / `TurnEndReasonMap` |
| `src/surface.ts` | 有序 surface 投影、替换校验、`deriveEventMessage` |
| `src/request-header.ts` | header 折叠与重建 |
| `src/repair.ts` | 失败步骤、中断日志、fork 种子共享的工具结果恢复 |
| `src/invariant.ts` | 不变式：序号、轮次/步骤闭合、工具调用/结果配对 |

### 5.2 追加校验（防 TOCTOU）

每次追加用共享的迭代式 `snapshotJsonValue()`：对每个嵌套值**只读取、校验、复制一次**，因此有状态的 getter 无法给校验一个值、给存储另一个值。非无损 JSON（BigInt、循环、稀疏数组、`-0`、特殊原型）在追加位置即被拒绝，**先于任何后端刷新**。被拒绝的追加不改变日志、派生状态或事件流。

### 5.3 surface 折叠与派生

- surface 是**增量投影**：通过 `replaceGeneration` 跟踪位置替换，`contentGeneration` 跟踪位置替换 + 插件消息变更。
- 空内容的 system/developer 节点**不派生消息**；插件投影修改派生内容，**不修改记录的消息**。
- `deriveMessages()` 缓存深度冻结消息、每次返回新数组；替换与投影决策使缓存失效。
- 三层数字类型：`SessionSeq`（已有事件/含端点水位）、`SessionLogOffset`（间隙/前缀长度/读取边界，可等于事件数）、`SessionSeqCursor`（加 `-1`）、`OptionalSessionSeq`（用 `null` 表示"缺失本身是数据"）。

### 5.4 JSONL 后端实现（`@deepseek-ai/dsh-session-persistence-jsonl`）

| 文件 | 职责 |
| --- | --- |
| `src/index.ts` | Config schema、后端服务类、文件存储原语 |
| `src/storage.ts` | JSONL 句柄、已路由实时事件缓冲（固定批处理窗口 + single-flight 排空）、进程内单写者记账、teardown |
| `src/format.ts` | 日志路径派生、header 编码、当前记录扫描 |
| `src/generation.ts` | 单遍历史还原、有界 stage 编码、源 revision 检查、**排他后继发布** |
| `src/migration-verifier.ts` | stage 与竞争 generation 校验的 Worker 生命周期 |
| `src/zstd.ts` | Zstandard 帧压缩/解码/帧扫描 |
| `src/win32.ts` | Windows write-through 发布与目录创建 |

**物理编码**：独立 Zstandard 帧的标准拼接——一个仅含 header 行的带校验和帧 + 每个 append 批次一个带校验和帧；Node 内置 Zstd **默认压缩级别（无级别开关）**。`compression: 'none'` 则为换行分隔 UTF-8。一个根只属于一种编码（混合根回退/双写/压缩转换不受支持）。

**无损来源序列压缩**：`sourceEventSeqs` 中**至少 3 个连续**序号的段编码为 `[start, end]` 区间对，读取时展开。

**单写者 + 排他发布**：

- 内核锁：POSIX 用 `session.lock` 上的非阻塞 `flock(2)`；Windows 用由该路径派生的**命名内核信号量**（零文件系统足迹）。
- POSIX 首次 append 用 `link()`，使同 id 竞态失败而不覆盖已提交日志；Windows 用无替换 write-through rename。
- **延迟实体化**：`create(header)` 不写任何东西；首次 `append` 才发布并 `fsync`。崩溃前从未实体化的会话等于从未存在。
- 写入/同步失败 → 文件**回滚到之前的字节长度**；已提交事件绝不重写。

**崩溃与撕裂尾部**：

- 不完整的最终原始行丢弃；
- 撕裂的 Zstd 帧只贡献**完整解码出来**的记录，写句柄截掉撕裂字节并在首次新批次前持久重写这些记录；
- 完整帧内的校验和/解压/结构失败 = **损坏拒绝**（不降级）；
- 修复是读方职责：`interruptedTurnClosers` 补缺失工具错误、未闭合 `step/end`、合成 `turn/end{interrupted}`。

**文件布局**：`<root>/-<normalized-cwd>-/<encoded-id>/session[.vN].jsonl[.zstd]`，当前 v3（文档另处提及 v4 已在实现路径上）。

**已知限制**：不删除会话文件；每会话一个活动写入方；NFSv3 上 `flock` 不可靠；Windows 信号量按登录会话隔离；POSIX 实体化需硬链接支持；POSIX 写入需匹配的预编译 addon（`@deepseek-ai/node-addon-system` 提供异步 flock）。

---

## 6. 上下文工程实现

### 6.1 一个测量服务为所有决策定价

单例 `ctx.tokenMeter` 在同一个已消费日志 revision 上测量最新规范 envelope 与当前 surface。压力判定、尾部保留、范围选择、缩减验证**共用同一套路由定价节点数值**；已记录的替换影子价则使用与路由无关的启发式，保证纯投影 fold 一致。
（维护者自陈：**token meter 每 token 四字符的启发式对 CJK 与 JSON Schema 定价偏低**，精确 token 化仍是开放方向。）

### 6.2 触发公式与默认值

阈值 = `floor(min(W × thresholdRatio, W − O − headroomTokens))`；保留预算 = `(W − O) × retainRatio`。

| 字段 | 默认 |
| --- | --- |
| `thresholdRatio` | `0.8` |
| `headroomTokens` | `65536` |
| `retainRatio` | `0.16`（与 `retainTokens` 互斥） |
| `maxTokens` | = 解析后的 `headroomTokens`（含推理 token） |
| `compactionRetries` | `1` |
| `maxOverflowRetries` | `1` |
| `modelPolicies` | `[]`（按 provider+model 覆盖） |
| `auto` | `true` |

配置错误**加载时快速失败**：未知设置、重复按模型覆盖、无效 token 数、两种保留形式并存、保留比例 ≥ 阈值比例。

### 6.3 区域事务（先记录标记 = 事务）

1. 验证范围与活动锁 → 同步追加 `compaction/start`；
2. 可选剪枝 → 重测 → 可能**不生成摘要就推进 surface**；
3. `summarize()`（唯一的子类钩子）→ `compaction/summary` → 替换 `user/message`；
4. 最后恰好一次 `compaction/end` 尝试。

**锁的语义**：中途崩溃 = 可检测的遗留锁，而不是一个撒谎的 `compaction/end`。位于较新 `session/end-seed` 之前的未匹配 start 是陈旧证据（不阻塞），之后的报告 `busy`。

### 6.4 摘要请求怎么发

直接 `ctx.llm.stream()`（**不走** `agent/request` 扩展点），回放：

1. surface 节点 0 的 `system/message`（系统提示词）+ 上次已路由请求的工具 → **复用提供方热前缀**；
2. 已遮蔽区域消息；
3. 最后一条 user 消息 = 压缩指令（冻结的 `RequestUserInput`，不含持久身份）。

`GenerateOptions.purpose = 'compaction'`；**只有返回文本**进入检查点（推理与工具调用排除）；图片输出以 `UNSUPPORTED_CONTENT` 失败而不是消失。替换消息用 `<compacted-summary>` 标签框定，原文保留在 `compaction/summary` 事件上。

压缩指令要求输出固定八段 Markdown：`Primary Request and Intent / Key Technical Concepts / Files and Code / Errors and Fixes / Pending Jobs / Current Work / Next Step / Critical Context`，并显式要求"保留精确文件路径、命令、错误串、标识符、数值、函数签名"，"不得提及本次压缩"。

### 6.5 剪枝 / spill / 图片省略

- 剪枝 `ctx.toolResultPruner`：`thresholdChars=8192`、`headChars=4096`、`tailChars=1024`（**Unicode 码点**）；非文本块计 0；每次替换前写 `compaction/prune` 计价事件；**低于压力的对话绝不被触碰**。
- spill：`ctx.spillStore.saveText()` → 不透明 locator + `retrievalHint`；本地后端私有 0700 + 排他 `open('wx', 0o600)`；`cleanupPeriodDays=30`；best-effort（保存失败保留内联结果，绝不把成功的调用变成 `isError`）。
- 图片省略：`compaction-image-offload` 拥有 `image/offload` 事件 + 纯消息投影，`ImageOffloadTarget { seq, imageIndexes }`。

---

## 7. 工具系统实现

### 7.1 `defineTool` 与 schema DSL

```ts
ctx.tools.register(defineTool({
  name: 'read_file',
  description: 'Read a file from disk.',
  parameters: { path: { type: 'string', required: true }, offset: { type: 'number' } },
  output: { schema: { type: 'string' }, render: (_args, value) => [{ type: 'text', text: value }] },
  async execute(args, exec) { return readFile(args.path, { encoding: 'utf8', signal: exec.signal }) },
}))
```

统一 DSL 支持 `string/number/integer/boolean/null/array/object/json/oneOf`；`InferValue` 在 **16 层**容器内保留精确类型，之后加宽为 `JsonValue`。原始 JSON Schema（`JsonSchemaNode`）是与 subagent / workflow / MCP 共享的协议级类型。

### 7.2 六段固定流水线

`tools/pre-execute`（allow / deny / cancel / ask）→ **已注册单调 guard** → `tools/execute`（环绕分发包装层，唯一可替换 `exec.signal`）→ `projectContent` → `tools/post-execute`（accept / 替换 / block / 附加上下文）→ `finalizeContent` → `tools/result`（仅观测）。

- **参数不可改写**（`pre-execute` 有意禁止改写 `exec.arguments`）——否则日志、UI、执行会失同步。
- `ToolGuard` 返回类型**故意不含 allow**，因此监听器顺序无法把 deny 翻回 allow。
- 审批 `ctx.approval` 是 one-shot，位于 pre-execute 之后、guard 之前；无回答方即 deny。
- 错误归一化：`UNKNOWN_TOOL` / `INVALID_ARGS` / `INVALID_TOOL_OUTPUT` / `TOOL_TIMEOUT` / `ABORTED_BEFORE_DISPATCH` / `ABORTED`——**调用失败不结束轮次**。
- `schemas()` 只投影 `name`/`description`/`parameters`，`output`/`execute`/`timeoutMs`/呈现回调**绝不泄漏到协议**。
- **`timeoutMs` 只是声明**，注册表不强制执行；要强制必须挂 `@deepseek-ai/dsh-tool-call-timeout-policy` 包装层。

### 7.3 并发调度

`executionMode` 分类 → 独占调用形成**屏障**，并行安全调用进**有界滚动池**（`maxParallelToolCalls`，默认 10），启动前重新分类。分类是**一元的**：安全性取决于比较同级调用或资源的调用必须保持独占。

### 7.4 PTC mode（工具即代码）

`mode: native | ptc | both`（默认 `native`）。PTC 下只暴露保留传输 `run_code` + 生成的类型化 SDK（TS/Python 两版渲染器）：

- 程序内 `await tools.name(args)`；失败抛 `ToolCallError`；
- 独立只读调用可用 `Promise.all` 重叠，变更调用按提交顺序串行；
- 纯 `ptc` 下模型直呼其他工具 = `UNKNOWN_TOOL`（通告面与可调用面一致）；
- 新子调用 id 格式 `<parent>:ptc:<n>`；
- `run_code` **Node 默认 timeout 120000 ms，上限 600000 ms**（含嵌套工具与审批等待）；更宽的 `sandbox_permissions` 需要非空 `justification` 且事前获批；
- **中间绑定值只存在于执行局部、无字节上限**（无法从会话回放重建）；只有外层 `run_code` 输出受硬上限；
- 每次运行获得全新状态（不做持久 REPL，因为跨调用状态不在日志里）。

---

## 8. 多智能体实现

### 8.1 subagent：一个服务 + 具名提供方注册表

`dsh-subagent` 源码地图：`index.ts`（注册表/启动/生命周期事件）、`continuation.ts`（身份预留/冷恢复/授权路由）、`continuation-activation.ts`（Activation 图/准入/结算）、`continuation-messages.ts`（相邻消息与结算通知）、`descriptor.ts`（`subagent/descriptor` 事件词汇）、`catalog.ts`（父级 `subagent/catalog` + 分块投影）、`child-agent.ts`（子级组装/委派策略/深度）、`list-children.ts`、`control.ts`、`archive-admission.ts`。

**两种子级形态**：

| | 一次性 | 可继续 |
| --- | --- | --- |
| 运行 | 发布即转移所有权，单次结果结算 | 保留持久 Session，至多一个进程内 Activation |
| 后续消息 | 不支持 | `sendMessage()`（running→steer 最近 step；waiting→唤醒 steer；无 Activation→冷恢复后 steer） |
| 中断 | — | `interrupt()`：fire-and-return，`Agent.cancel(cause, { keepInbox: true })` |

**"兑现即发布"**：提供方的 `start()` 只有在真实子 agent 存在后才兑现——调用方要么拥有一段在线运行，要么一无所有；失败时回滚每个未发布的资源。

**硬上限**：`maxActiveSubagents=8`（可继续父子链共享的存活子级数；耗尽以 `ACTIVATION_LIMIT_REACHED` 拒绝，**不排队**）、`maxDepth=1`（`0` 禁止委派）。池只存在于当前进程。

**权限继承**：创建子级时在第一次 await 前**快照**委派权限状态——Auto/Full access 父级让子级获得相同 `permission/preset` 身份；Auto 会独立审查子级的每个受支持调用（低风险项目内工作直接允许；中风险需明确授权；高风险始终拒绝）。进程内 DSH 子级继承 Auto；ACP / Codex / Claude Code 保留各自权限系统。

**每个子 agent 的运行时上下文带一段固定声明**（且生命周期内不变，因此只写一次、不破坏 KV cache 前缀）：

> You are a delegated subagent: your permission scope was fixed when you were started and cannot be widened from inside this session — operations that require approval are rejected automatically…

**投影**：父级 Session 追加 `subagent/catalog` 事实（一次性在创建后记录，可继续在初始 inbox 准入后、返回子 id 前记录）；`subagentTiming` 投影累加耗时与最近轮次是否 `completed`；目录视图对 D 条事实以 **O(D)** 保留父目录事件顺序。

**官方自陈限制**：无持久 parent mailbox（child→parent 需 parent 在线）；驻留仅限进程内（跨进程需持久化邮箱与租约协议）；崩溃可能丢失"已接受但未写入子日志"的提示词且不回放；ACP 子级仍为一次性。

### 8.2 workflow：模型写脚本，PTC 进程执行

`workflow-ptc` 源码地图：`index.ts`（配置/校验/运行创建）、`host.ts`（PTC 执行/子 agent 归属/结算/释放）、`guest.ts`（guest 适配器）、`guest-source.ts`（自包含 guest 程序源码）、`runtime.ts`（VM 求值/辅助函数/组合器）、`realm.ts`（跨 realm 无损 JSON 物化）、`meta.ts`。

- 钩子：`agent()` / `parallel()` / `pipeline()` / `phase()` / `log()`；
- 默认 `provider=spawn`、`maxConcurrentAgents=0`（→auto `min(16, max(1, cores−2))`）、`maxTotalAgents=1000`、`maxItemsPerCall=4096`、`syncTimeoutMs=5000`；
- **无整体经过时间截止**；取消立即中止 PTC 进程；清理不另设定时器；
- 值跨 realm 物化为无损 JSON（特殊原型/函数/symbol/循环/稀疏数组/非有限数/嵌套 undefined 被拒绝）；跨 realm 错误**不能用 `instanceof Error`**，要按 `name`/`code` 分支；
- **VM 不是安全边界**——OS 文件策略（沙箱）才是；程序可见环境为空；文件策略不限制网络。

### 8.3 ralph（需显式启用）

每 Round 一个**全新子会话**（不看父对话/前序子会话），**共享工作区当长期记忆**，Round 间只传有界交接报告：`maxRounds=256`（默认兼上限）、`maxHandoffChars=16384`、`maxResultChars=16384`。

---

## 9. 执行环境与权限实现

### 9.1 沙箱（只管文件效果）

| 平台 | runner | 形态 |
| --- | --- | --- |
| Linux | `bwrap` → Landlock（链序探测） | 只读宿主根 + 全新 `/dev` + **私有 PID 命名空间**的 `/proc`（防 procfs 魔法链接绕过挂载）；`workspace-write` 加临时 `/tmp` 与可写工作区绑定 |
| macOS | Seatbelt（`sandbox-exec`） | 默认允许 + `(deny file-write*)` + 规范化后的写入 allow-list（`Seatbelt` 匹配解析后路径，`/tmp` 即 `/private/tmp`） |
| Windows | ACL 受限令牌 | 每工作区一个确定性写入 SID + 常驻 ACE；**每活跃会话/工作区对一个随机私有临时目录**（`<temp>\dsh-<hash>`）与可撤销 ACE |

- `enforcement: 'full' | 'partial'` 是**后端报告的事实**：Windows（硬链接别名、读取不受限、AppContainer ACL 边界）与旧 Landlock ABI 均报 `partial`。
- **fail-closed**：无可用 runner → `confine()` 以 `SANDBOX_UNAVAILABLE` 拒绝，"静默无隔离透传永远不合法"。
- 两类 stderr 分类器：`denialSignatures`（沙箱正常工作、命令被拦）与 `runnerFailureRules`（runner 在命令执行**前**失败 = 基础设施故障，必须先判）。分类不会重写 stderr。
- `probeTimeoutMs=5000`。runner 选择在提供方生命周期内缓存（装/修 runner 后需重载插件）。

### 9.2 审批与权限预设

`ctx.approval` one-shot → 无回答方/无 agent ⇒ deny。权限预设把 `sandbox/mode` + `approval/policy` 两个 knob 捆成具名预设：`workspace-write`（+ask）、`danger-full-access`（+never）；`custom` 是派生值（可显示、不可切换），`auto` 由 Auto review 以 effect 生命周期注册。

---

## 10. 可观测与治理实现

- **不变式服务** `ctx.invariants`（`enabled` 默认 `true`，`package_allowlist`/`package_blocklist` 为大小写敏感正则）：各包通过 `./invariant` 子路径注册本地检查并标明归属包。**只对"会发散的观察"发布**——空配套与被忽略的 reporter 会让 `verify-package-invariants` 失败。
- **从日志重建请求**的不变式：`agent-loop/src/invariant.ts` 与 `session/src/invariant.ts` 各自校验序号、轮次/步骤闭合、工具调用/结果配对。
- **遥测**：`ctx.sessionTelemetry`（脱敏后交后端）+ `ctx.otel` + 产品遥测。
- **循环卫生守卫**：重复调用提醒 `thresholds=[3,5,8]`（非法值在**插件加载时**即抛）、`argumentsPreviewChars=500`；`tools/execute` 截止时间强制执行器（协作式）。
- **凭据卫生**：启动命令前清理名称匹配 `*KEY*/*SECRET*/*TOKEN*/*PASSWORD*` 的环境变量。

---

## 11. 工程质量体系（本档最值得抄的一段）

### 11.1 测试分层

| 层 | 命令 | 做法 |
| --- | --- | --- |
| 单元 | `pnpm run test` | vitest 跑包级 `tests/**`；**每个注册表必须有一个 HMR 安全测试**（dispose fiber 并断言移除） |
| 覆盖率门禁 | `pnpm run test:coverage` | 对 `packages/*/*/src` **按文件 100% 行覆盖**；未覆盖的行通常应删除死代码而非补测试 |
| 真实 API e2e | `pnpm run test:e2e` | 各提供方密钥各自控制，缺密钥自动跳过 |
| 预期输出 | `pnpm run test:expected` | 无录制往返的无密钥 CLI/进程预期，CI 针对**构建产物**跑 |
| 性能基准 | `pnpm run test:bench` | 合成输入跑耗时/堆/缩放预算（Linux PR gate） |
| 快照 | `pnpm run test:snapshot` | 录制会话 + 模型回放，headless/SDK/ACP/Web 四套；`record` vs `refresh` 语义分离 |
| Web 浏览器快照 | `pnpm run test:web` | Chromium 比对全部会话驱动输出；CI 强制只读 `DSH_SNAPSHOT=replay` |

### 11.2 三条硬纪律

1. **优先真实实现而非 mock**：只 mock 高开销/不确定边界（LLM 适配器、网络、时钟）；桥接测试用 `makeBridgeHarness()` 挂真实 agent loop + 会话存储 + 工具注册表 + JSONL 持久化，唯一 mock 是脚本化 `MockAdapter`。
2. **验证外部世界，而非自我报告**：e2e 断言要重跑命令或从外部重读文件，对 agent 自身输出做关键词探测会让作弊的 agent 通过。
3. **测试真实入口路径**：产品的 `bin` 跑的是构建后的 `lib/bin.js`，由普通 `node` 执行——暴露 tsx 会掩盖的失败（结算竞态、模块解析、被吞掉的加载失败）。"手动构建的 `ctx.plugin(...)` 套件不够"——必须通过 Loader 与 app/process 启动仅用于测试的 `cordis.yml`。

> 覆盖率门禁的清醒说明：行覆盖是**必要但不充分**——它证明行被执行过，不证明功能按预期工作。

### 11.3 防御式模式（每条都来自真实事故）

1. **正交结果独立上报**——进程可能已超时却仍以退出码 0 结束；`timedOut`/`signal`/`exitCode` 各自上报，不要嵌套分支。
2. **公共约定两侧都要遵守**——实现收到多种表示时先规范化再返回（如 `LlmRuntime.stream()` 只用终止型 finish 分片暴露失败，middleware 缺陷仍抛异常），并在类型处记录规范化后的约定。
3. **异步状态不是同步状态**——不要把 `agent/status` 或 `whenIdle()` 当成某次 `followup()` 的结果；拥有一次运行的调用方必须显式定义其区间。
4. **dispose 必须达到完全停稳**——只发终止信号就返回会留下孤儿进程；要先关监听器注册表再杀进程，并等待 `done`。
5. **在分发器中隔离回调异常**——一个行为不当的订阅者绝不能破坏核心生命周期。
6. **绝不把环境变量或可预测路径暴露给不可信输出**——清理 `*KEY*/*SECRET*/*TOKEN*/*PASSWORD*`；临时/spill 文件放 0700 私有目录 + 随机名 + `open('wx', 0o600)`。
7. **用 unlink 删除可能是符号链接的路径**——先 `lstatSync().isSymbolicLink()` 判断再 `unlinkSync`（unlink 拒绝真实目录，绝不跟随）；Windows junction 用 `rmSync` 会抛 `ERR_FS_EISDIR`，递归删除可能穿过 junction。

### 11.4 CI 与代码生成门禁

- **代码生成新鲜度**：`gen-cordis-catalog` / `gen-module-graph` / `gen-doc-graphs` 产物由 CI 校验（`verify-cordis-catalog`、`verify-type-equiv`、`verify-translation-pairing`、`verify-package-readme-*`、`verify-subsystem-pages`、`verify-application-entrypoints`、`verify-package-invariants`）。
- **`ts type-equiv` 围栏**：子系统文档里粘贴的类型必须与源码符号 + 原始 JSDoc 一致，在 `scripts/type-equiv.manifest.json` 登记；改了源码即失败。
- **Git 钩子**（Lefthook）：`pre-commit` 校验暂存配对文档 + Oxlint + 空白 + vendor manifest 守卫；`pre-push` 跑 `typecheck`。**钩子有意不跑测试/快照/构建**——贡献者只跑与改动相关的检查，CI 负责全量。
- **依赖治理**：`module-graph` 由 `gen-module-graph` 从 `peerDependencies` 生成；事实层面 `extensions`/`experimental` 都 peer 依赖 `agent`/`tools`/`session`，**不依赖 `agent-loop`**（书面禁令在 `packages/AGENTS.md` 与 capability-seam 文档里：扩展插件依赖 Service Definition，绝不依赖具体提供方）。
- TODO 标签分级：`FIXME`（阻塞发布）/ `TODO`（尽快）/ `XXX`（也许某天）。

---

## 12. 可迁移的工程纪律（12 条）

| # | 纪律 | dsh 的做法 |
| --- | --- | --- |
| 1 | 唯一真源 | 会话日志仅追加；模型历史 `deriveMessages()` 派生、从不另存；不变式检查"请求可从日志重建" |
| 2 | 崩溃可检测 | `compaction/start\|end` 事件对当锁；失败步骤补合成工具结果并写明"结果未知" |
| 3 | fail-closed | 沙箱无后端即拒绝；剪枝/摘要失败保留原始状态并在日志留痕 |
| 4 | 正交失败 | PTC 8 类失败独立报告；预算耗尽≠异常，中止≠超时，基底崩溃≠二者 |
| 5 | 参数不可改写 | `tools/pre-execute` 禁止改 `exec.arguments`（日志/UI/执行一致性） |
| 6 | 单调守卫 | guard 返回值不含 allow ⇒ 监听器顺序无法翻案 |
| 7 | 边界用于完整结果 | 字节/token/条数上限在"完整值已知"处施加，并测极小/精确/超大/多字节 |
| 8 | 只在提交点发布状态 | 通知与派生状态在操作成功后才发；缓存/提示词/UI/回放同源派生 |
| 9 | 决策在做出它的操作里强制执行 | 不用 schema 省略或提示词过滤当强制；通过执行器测拒绝路径 |
| 10 | 一个决策一个计价服务 | `ctx.tokenMeter` 为压力、保留、范围选择、缩减验证统一定价 |
| 11 | 把影响写进文档 | 每个包 README 必写「模型看到什么 / Token 影响 / KV Cache 影响」三段 + `## Known Limitations and Deferred Work` |
| 12 | 测试真实入口 | 跑构建产物 + 普通 node；只 mock LLM/网络/时钟；e2e 验证外部世界 |

---

## 13. dsh 自己承认的边界（诚实清单）

- **主循环没有轮次预算**（限制失控轮次需自行从扩展点取消）。
- **token 计量是启发式**，对 CJK 与 JSON Schema 定价偏低；精确 token 化未做。
- **溢出恢复只认 `CONTEXT_WINDOW_EXCEEDED`**，其他提供方侧上下文失败不参与分类。
- **压缩不能**缩减系统提示词/工具/会话前缀，不能拆分不可分单元（如一次超大工具调用）。
- **沙箱只管文件效果**，不管网络与进程可见性；Windows/Landlock 只达 `partial`。
- **PTC 中间值无字节上限**且无法从会话回放重建；VM 不是安全边界。
- **workflow 上限是协作式的**，不是 Host 强制的安全配额或后代 token 预算。
- **subagent 驻留仅限进程内**；无持久 parent mailbox；崩溃可能丢失"已接受未落盘"的提示词。
- **会话文件不删除**（seam 无删除接口），需外部清理。
- **`experimental/` 组**（Agent Teams、browser-use、computer-use、PTC Python）**无稳定性与支持承诺**。

---

## 14. 对本地平台的落地映射

> 成本评估与优先级见 36 号；这里只写"照它的实现方式怎么做"。

| 目标 | dsh 的实现要点 | 本地落点（建议） |
| --- | --- | --- |
| 历史不丢 tool 消息 | surface 事件一等化，`deriveMessages()` 派生 | `assembler.go:718 BuildHistoryMessages` 补 tool 消息（一行级） |
| 上下文预算 | 单点 token meter + 阈值公式 + 剪枝优先 | 新增测量服务；先做"历史限量 + 超预算告警"，再做压缩 |
| 崩溃可检测 | 事件对当锁 + 合成结果 | 中断恢复补 `TOOL_OUTCOME_UNKNOWN` 式结果文本 |
| 能力可替换 | seam 三角色 | 沙箱已三后端；把"文件系统/进程"抽成 seam，让 bash/文件工具同时搬迁 |
| 工具策略外置 | 六段流水线 + 单调 guard | 工具执行前后加 `pre/post` 钩子（P1，36 号已列） |
| 工程质量 | 100% 覆盖门禁不现实；**可先抄三条**：HMR/注册表可撤销测试、真实入口冒烟、防御式模式清单 | 加入 `docs/20` 冒烟清单与代码评审清单 |
| 文档纪律 | 每个包写「模型看到什么 / Token / KV cache」 | 平台知识与包 README 可试点 |

---

## 15. 资料索引

| 主题 | 官方路径（`deepseek-ai/deepseek-harness@master`） |
| --- | --- |
| 包实现文档（含源码地图） | `packages/<group>/<pkg>/README.zh.md`（本档引用：`core/agent-loop`、`core/session`、`core/tools`、`session/session-persistence-jsonl`、`compaction/compaction-basic`、`sandbox/sandbox-local`、`subagent/subagent`、`workflow/workflow-ptc`、`bundle/base`） |
| 包约定 | `packages/AGENTS.md` |
| 工程 | `docs/development.zh.md`、`docs/testing.zh.md`、`docs/defensive-patterns.zh.md`、`docs/module-graph.zh.md`、`docs/config-catalog.zh.md` |
| 子系统 | `docs/subsystems/*.zh.md`（59 个事件变体见 `docs/persistence-catalog.zh.md`） |
| 工具总表 | `docs/tool-catalog.zh.md` |
| 取数注记 | 本机直连 `raw.githubusercontent.com` 会 SSL 失败；`api.github.com/.../contents/<path>` 可取（未认证 60 次/小时，已在本轮触发限流）；递归 tree API 会截断，需逐目录查 |

> 本地缓存：`.workbuddy/tmp/dsh2/`（临时，不入库）。
