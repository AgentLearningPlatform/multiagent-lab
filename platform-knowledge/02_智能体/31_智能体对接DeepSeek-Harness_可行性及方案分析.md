---
module: 智能体
topic: 平台 Agent 对接 DeepSeek Harness 可行性
desc: 回答「能不能接、以什么形态接、每种形态代价、推荐哪条」——平台侧对接面 × dsh 侧可对接面 × 五条路径的成本/风险/能力得失（REQ-160 立项依据；与 36 号架构对比、41 号源码拆解三档分工不重复）
req: [REQ-160]
docs: ["02 §6.16", "01 REQ-160"]
decisions: [D-O13, D-O15, D-O5]
synced: 2026-09-29
---

# 平台 Agent 对接 / 嵌入 DeepSeek Harness（dsh）——可行性及方案分析

> **本档唯一主旨**：回答「本平台的 Agent 能不能对接 dsh、以什么形态对接、每种形态要付什么代价、推荐哪一条」。
>
> **三档分工，不重复**：
> - **本档（31）**＝对接/嵌入的**可行性判定与方案选型**（平台侧对接面 × dsh 侧可对接面 × 五条路径的成本/风险/能力得失）。
> - [`36_智能体开发架构深度对比`](36_智能体开发架构深度对比_本地实现与DeepSeekHarness.md)＝两套架构的十层差距与借鉴优先级（不谈怎么接）。
> - [`41_DeepSeek-Harness_实现方案深度调研`](99_开源项目分析/41_DeepSeek-Harness_实现方案深度调研.md)＝dsh 源码级实现拆解（循环状态机、会话持久化、上下文工程、工具流水线、工程质量体系）。
>
> **资料基准**：dsh 侧结论来自 `deepseek-ai/deepseek-harness@master` 一手文档（2026-09-29 复核），凡文档未给出的一律标注「文档未给出」；平台侧结论来自本仓库代码实测。

---

## 0. 结论速览

1. **"能不能接"早已不是问题**——M24/REQ-160 阶段一/二已交付，dsh 现已是第 4 个外部推理后端。真正没回答的是「**接得有多深**」。
2. **现有接入是最浅的一档**：`dsh --profile headless <task>` 一次性子进程，纯文本 stdout。它把 dsh 当成"另一个会说话的模型"用，**dsh 作为 harness 的全部价值（工具执行、会话持久化、事件流、权限回调、MCP）一个都没接到**。能力矩阵里 `AgentAsTool / Workflow / Resume` 三项全 false、`Skills / MCP` 降级为提示词注入，正是这个形态的直接后果。
3. **dsh 官方提供了两条真正的"进程外驱动"通道**，且 dsh 自己就用它们做进程外 subagent 委派（`dsh-subagent-acp`、`dsh-subagent-dsh-sdk`）——这意味着「嵌入」是官方认可的正道，不是硬掏内部 API：
   - **ACP**（`dsh --profile acp`，标准 ACP v1 over stdio）：完整会话生命周期（new/list/resume/close）、**取消**、**权限回调**、**MCP 挂载**。
   - **SDK**（`dsh --profile sdk`，换行分帧 JSON-RPC 2.0 over stdio）：`session.event` 通知**不过滤地投递运行时内每个会话的事件**——事件富度最高。
4. **协议面极小，自研 Go 客户端成本可控**：SDK 仅 3 个请求方法 + 4 个通知；ACP 13 个调用。且两者都是 **stdio 行分帧**，与现有 `cliAdapter` 的"PATH 探测 + 子进程 + 读 stdout"心智模型同源——**不需要引入 Node/Python 运行时到平台侧**（D-O15 不受影响）。
5. **但现有 `cliAdapter` 骨架接不住这两条通道**：它是一次性 spawn + 只读 stdout + 无 stdin 写入 + 无进程复用 + 无会话概念。上 ACP/SDK 需要**新的适配器形态**（长驻 stdio 客户端 + 会话映射 + 事件转译层），不是"再加一个 15 行的 cliAdapter"。这是本次分析最重要的落地结论。
6. **推荐路径**：**ACP 为主通道**（唯一能翻正 `Resume` 与 `MCP=tools` 的通道），**SDK 为事件富度补充**（需要完整事件审计/回放时），**保留 headless 现状**作为教学 A/B 与降级兜底。**不建议深集成**（把 dsh seam 与平台工具/本体/知识库打通）——预发布无兼容承诺且需 TS 插件开发，与 D-O5/D-O15 冲突。

---

## 1. 问题界定：什么算"对接"，什么算"嵌入"

先把词定义清楚，否则可行性无从判断。

| 层次 | 定义 | 判据 | 本平台现状 |
| --- | --- | --- | --- |
| **L0 人工对照** | 不集成，人在两个 UI 里各跑一遍做对比 | 无代码耦合 | 已具备（教学用） |
| **L1 一次性调用（对接）** | 平台把一次运行交给 dsh，拿回文本 | 提示进 / 文本出，无会话、无事件 | ✅ M24 已交付 |
| **L2 协议驱动（嵌入）** | 平台长驻驱动 dsh 进程，双向 stdio，拿回**事件流** | 有会话标识、能收增量事件 | ❌ 未做 |
| **L3 生命周期托管（深度嵌入）** | 平台能创建/恢复/取消/关闭 dsh 会话，能挂载 MCP、能回答权限请求 | 会话跨进程可恢复、可取消、权限可控 | ❌ 未做 |
| **L4 seam 级融合** | 把 dsh 的 Cordis seam（fs/shell/tools/subagent）与平台工具系统、本体、知识库打通 | 平台能力成为 dsh 的工具提供方 | ❌ 不建议 |

**本档覆盖 L1~L4 四档的可行性**，给出 L2/L3 的具体方案与改动面，论证 L4 不可取。

---

## 2. 平台侧对接面盘点（我们能接住什么）

> 代码事实，路径 `backend/internal/inference/`。

### 2.1 后端契约（`inference.go`）

```go
type Backend interface {
    Name() string
    Probe(ctx) ProbeResult                                  // PATH + --version，5s 超时
    Capabilities() Capabilities
    Run(ctx, req *RunRequest, emit func(Event)) error       // 阻塞执行，增量经 emit 流出
}
```

- `RunRequest{Prompt, UserInput, Cwd, Env, Debug}`——**`Prompt` 是 chat 层预先组装好的完整提示**（指令 + 技能/MCP 降级注入 + 知识库上下文 + 历史 + 用户输入）。
  - ⚠️ 这条决定了 L1 的能力天花板：平台侧的**一切增强只能以文本形式注入**，dsh 侧拿不到结构化工具定义。这是 `Skills/MCP → instruction` 降级的根因，不是实现偷懒。
- `Capabilities`：`Chat` `Stream` `SkillsMode(tools|instruction|none)` `MCPMode` `AgentAsTool` `Workflow` `Resume`。
- `Registry` 探测结果 **10 分钟 TTL 缓存**；`einoADK.Run` 直接返回错误（默认后端不经适配器，走平台内进程管线）。

### 2.2 `cliAdapter` 骨架（`adapters.go:42`）

```go
type cliAdapter struct {
    name        string
    bin         []string                        // 候选可执行名，依次 LookPath
    versionArgs []string
    buildArgs   func(req *RunRequest) []string
    parseLine   func(line string, emit func(Event))  // nil = 整段输出一条 delta
    linePrefix  string
}
```

`Run` 的实际形状（`:89`）：`LookPath` → `buildArgs` → `exec.CommandContext` → `StdoutPipe` → `bufio.Scanner`（64KB 初始 / 4MB 上限）逐行 → `parseLine`；stderr 缓存用于错误信息；`WaitDelay = 3s`。

**四个已注册适配器**：`claude-code`（`--output-format stream-json`，`parseClaudeCodeLine` 能产出 **tool.call / tool.result**）、`opencode`、`deepseek-harness`（`--profile headless`，`parsePlainLine`）、`aider`。

### 2.3 骨架的三条硬约束（决定 L2/L3 必须新形态）

| 约束 | 表现 | 对 L2/L3 的影响 |
| --- | --- | --- |
| **无 stdin 写入** | 只 `StdoutPipe`，从不写子进程输入 | ACP/SDK 都是 **stdio 双向**协议 ⇒ 必须新增写入通道 |
| **无进程复用** | 每次 `Run` 现 spawn、跑完即弃 | ACP/SDK 要求**长驻进程 + 多次往返** ⇒ 必须引入进程/会话持有与回收 |
| **无会话概念** | `RunRequest` 无 sessionId 字段 | ACP 的 `session/resume` 需要有地方持久化 dsh 侧会话标识 ⇒ 平台侧要新增映射 |

> 反过来看好消息：`cliAdapter` 的"PATH 探测 + 未安装即降级 + 能力矩阵声明"这套**治理心智完全可复用**，长驻适配器只需替换传输层，探测/降级/前端呈现零改动。

---

## 3. dsh 侧可对接面盘点（它暴露了什么）

> 一手来源：`packages/sdk/README.zh.md`、`packages/sdk/protocol/README.zh.md`、`packages/sdk/client/README.zh.md`、`packages/acp/acp/README.zh.md`、`docs/api-gateway.zh.md`、`packages/bundle/sdk-app/README.zh.md`、`python/README.zh.md`。

### 3.1 四条通道总览

| 通道 | 启动形态 | 传输 | 面向 | 官方定位 |
| --- | --- | --- | --- | --- |
| **C1 headless CLI** | `dsh --profile headless <task>` | 一次性 stdout 纯文本 | 脚本 | answer-once-and-exit |
| **C2 SDK** | `dsh --profile sdk` + `dsh-sdk-jsonrpc-server` | stdio，**换行分帧 JSON-RPC 2.0** | 进程外 SDK 客户端 | "从另一进程驱动完整运行时" |
| **C3 ACP** | `dsh --profile acp` | stdio，标准 **ACP v1** | 脚本 / 测试运行器 / **另一个 harness** | "仅自动化的 ACP 服务器" |
| **C4 Typert API Gateway** | Host/Client RPC | `POST /api/<ns>/<method>` + `/api/remote.mux` WebSocket | Web 前后端 | 内部 RPC 面 |

**C4 先排除**：官方明确写"会话事件流、分页、增量 reduce、projection 与实体子流……不属于本文范围；即使复用 Connection，也不应伪装成 Remote 方法"。也就是说 **C4 不提供会话事件流**——它适合做平台→dsh 的控制面，不适合做 dsh→平台的事件通道。本档不将其列为候选。

### 3.2 C2 SDK 通道细节

**协议面（小得惊人）**：

| 方向 | 方法 | 载荷 |
| --- | --- | --- |
| client→server | `initialize` | `InitializeParams` → `InitializeResult`（profile / provider / model / reasoningEffort / maxTokens / cwd） |
| client→server | `session/prompt` | `SessionPromptParams` → `SessionPromptResult{messageId}`（**持久入队回执**，不是结果） |
| client→server | `shutdown` | → `{}` |
| server→client | `session.event` | `SessionEventNotification`——**运行时内每个会话，不过滤** |
| server→client | `session.status` | 整个 agent 的 `running`/`idle` 转换 |
| server→client | `subagent.started` | 血缘边 |
| server→client | `subagent.finished` | 仅进程内运行；带 `lastAssistantMessage` |

- **关键能力**：`session.event` 不过滤 ⇒ 平台能拿到 dsh 的完整会话事件流（`turn/*`、`step/*`、`tool/call`、`tool/result`、`assistant/message` 等），这是**事件富度最高**的通道，可做平台的工具事件映射与审计。
- **关键默认值**：`initializeTimeoutMs = 10000`、`shutdownTimeoutMs = 1000`；关闭阶梯 `stdin EOF → SIGTERM → SIGKILL`（幂等）；`serverInfo.name = "deepseek-harness-sdk-runtime"`、`version = "0.0.1"`。
- **工具面**：SDK profile 基于 `dsh-base`，默认提供 `read` / `write` / `edit`（**不含 `str_replace_editor`**，需显式 patch 插入）；persona 为 "You are a coding agent powered by {{model}}"。
- **已关闭的能力**（官方列为限制，务必诚实引用）：
  - **无取消方法**——放弃轮次 = 关闭运行时进程。
  - **无会话关闭方法**——只能 `shutdown` 整个运行时。
  - **无协议版本协商**——握手只带 `version 0.0.1`、客户端不校验；处于**预发布阶段，无兼容承诺**。
  - **server→client 请求未实现**——传输层支持但服务器从不发送（为未来审批流程预留）⇒ **权限回调在 SDK 通道上不存在**。
  - 禁用 HMR；禁用模型生成的会话标题（确定性 fallback 仍持久化）。

### 3.3 C3 ACP 通道细节

**协议面（13 个调用）**：

| 调用 | 得到什么 |
| --- | --- |
| `initialize` | 稳定 ACP v1 + `session/list`/`resume`/`close` + Streamable HTTP MCP 支持 |
| `authenticate` | **立即成功，服务器不需要身份验证** |
| `session/new` | 全新持久 agent；工作区与 MCP 服务器在发布前校验 |
| `session/list` | 分页列出可恢复的根会话（`sessionListPageSize` 默认 **100**） |
| `session/resume` | 恢复已持久化且非活跃的会话；**恢复日志但不回放旧更新** |
| `session/close` | 停稳式取消 + 更新 drain + 后代释放 + 持久化 flush |
| `session/set_config_option` | 串行更新 `model` / `reasoning_effort` |
| `session/prompt` | 每会话一次一个提示词；Agent 空闲且有序更新交付后才结算 |
| `session/cancel` / `$/cancel_request` | **提示词级取消**；无进行中提示词时取消自主工作 |
| `session/update` | 已提交 assistant 消息与 thought、通用工具生命周期、配置变化、**上下文用量** |
| `session/request_permission` | 带一次性允许/拒绝的权限提示，**客户端可自动回答** |

- **独有优势**：①**一个连接可同时运行多个会话且彼此独立**；②**MCP 挂载**（stdio 与 Streamable HTTP 两种；客户端授权其绝对命令/环境或绝对 URL/header）；③**权限回调**；④**跨进程会话持久化与恢复**。
- **诚实边界**（官方 Known Limitations，引用时别说满）：
  - **只发标准语义更新**——原始提供方增量、重试尝试、DSH 专有呈现数据不进协议。
  - 仅**一个主 workspace**，附加目录不支持；仅**光栅提示词图片**（PNG/JPEG/WebP/GIF，需持久附件存储 + 确切图片路由）。
  - **仅 MCP 工具**——MCP resource 与 prompt 没有 DSH 消费方。
  - **无** transcript 回放、会话删除、fork、`session/load`、SSE、mode、命令、计划、终端、客户端 FS 操作、elicitation。
- **信任模型**：`authenticate` 立即成功 ⇒ **安全边界完全由"本机 stdio + 受信控制器"提供**，绝不可把这条通道暴露到网络。

### 3.4 Python SDK 的形态（关系到 D-O15 判定）

`python/README.zh.md`：Python SDK 由两个包组成——`deepseek-harness-sdk`（高层轮次 API + 低层 JSON-RPC 客户端）与 **`deepseek-harness-runtime-bin`（内置 dsh CLI 可执行程序与原生伴随文件）**。

> 即：**客户端是 Python，运行时仍是打包进去的 dsh 二进制**。平台是 Go，既不能也不该依赖 Python SDK；但只要 PATH 里有 `dsh`，**用 Go 直接实现同一个行分帧 JSON-RPC 协议即可**——协议仅 3 方法 4 通知，且官方明说 Python SDK 是"复现这些结构而非导入"。

**这对 D-O15 的判定**：dsh 仍按"外部 CLI、PATH 探测、未装即降级"处理，**不进 run-dev 主链**，平台侧不引入 Node/Python 运行时依赖——与现有四个适配器同待遇，**合规**。

---

## 4. 五条集成路径的可行性逐项分析

> 成本单位：人日（后端 + 前端 + 冒烟，不含 dsh 侧学习成本）。

### P1 · headless CLI 适配器（**现状，已交付**）

- **形态**：`dsh --profile headless <task>`，一次性子进程，`parsePlainLine` 整段转一条 `message.delta`。
- **能力**：Chat ✅ / Stream ✅（整段降级）/ Skills·MCP = instruction / AgentAsTool ❌ / Workflow ❌ / Resume ❌。
- **成本**：已完成（约 1~2 人日）。
- **优点**：零风险、零新增形态、A/B 教学演示现成。
- **根本缺陷**：**拿不到任何事件**——工具调用、步骤边界、用量全部不可见，平台上"运行过程"是黑盒。
- **结论**：**保留**，定位为降级兜底与教学对照，不作为深化方向。

### P2 · SDK stdio 长驻客户端（事件富度路径）

- **形态**：平台 spawn `dsh --profile sdk`，持有长驻进程，写 stdin JSON-RPC、读 stdout 行分帧；把 `session.event` 流转译为平台事件（`message.delta` / `tool.call` / `tool.result` / `run.error`）。
- **技术前提**：①PATH 有 dsh（现有 Probe 可复用）；②Go 侧实现约 200~300 行的行分帧 JSON-RPC 传输 + 3 个方法编解码；③新增**进程持有与回收**（对齐官方关闭阶梯：stdin EOF → SIGTERM → SIGKILL，且幂等）。
- **能力增益**：`Stream` 升级为**真增量**；**工具事件首次可见**；可借 `session.status` 判定 idle；`subagent.finished` 可见（仅进程内）。
- **能力不变**：`Resume` ❌（无 resume 方法，只有 `session/prompt` 可带 `sessionId`——但无 close/resume 语义，跨进程恢复不成立）；`MCP` 仍 ❌（无挂载面）；**取消 ❌**（官方限制：放弃轮次 = 关运行时）。
- **平台改动面**：新增 `streamingStdioAdapter` 形态（**不动 `cliAdapter`**）+ 事件映射表 + 进程池；`Capabilities` 增加描述维度但字段可沿用。前端零改动。
- **成本**：约 **3~5 人日**。
- **风险**：协议无版本协商（预发布），dsh 升级可能变协议；单次运行只能一个进行中提示词（每会话一次一个提示词）。
- **结论**：**值得做，但作为 P3 的补充**（它的价值在事件审计富度，不在生命周期）。

### P3 · ACP stdio 长驻客户端（**推荐主通道**）

- **形态**：平台 spawn `dsh --profile acp`，长驻 stdio，实现 ACP v1 客户端（13 个调用）。
- **技术前提**：同上（长驻 stdio 传输），另需：①平台侧持久化 `dshSessionId ↔ 平台会话` 映射（启用 `session/resume`）；②权限回调处理器（`session/request_permission` → 平台审批策略）；③MCP 挂载配置面（若要用）。
- **能力增益（关键，三项翻正）**：
  - **`Resume` ❌ → ✅**：`session/resume` 跨进程恢复持久会话（注意：恢复日志但**不回放旧更新**）。
  - **`MCPMode` instruction → tools**：MCP server 真正挂载为工具（stdio + Streamable HTTP）。
  - **`Stream` 整段 → 真增量**：`session/update` 串行交付已提交消息、thought、工具生命周期与**上下文用量**。
  - 附加：**取消能力首次具备**（`session/cancel`）；**权限可控**（自动回答或转平台审批）。
- **能力不变**：`AgentAsTool` ❌（dsh 内部 subagent 不映射到平台 Agent 编排）；`Workflow` ❌；`SkillsMode` 仍为 instruction/none（dsh 用自己的 skill 注册表，平台技能无法注入——诚实标注）。
- **平台改动面**：新增 ACP 客户端（约 400~600 行，含 13 个调用的编解码与状态机）+ 会话映射表 + 权限处理器 + 进程池；`Capabilities` 需新增字段或扩枚举以表达"MCP 可挂载"。前端零改动（探测面板/下拉自动出现）。
- **成本**：约 **6~10 人日**（ACP 面比 SDK 大，但换来生命周期）。
- **风险**：①信任模型要求 **必须本机 stdio**，不可容器间裸奔网络；②预发布协议稳定性；③仅 MCP 工具 ⇒ 若期望平台工具被 dsh 调用，此路不通；④单 workspace 限制。
- **结论**：**推荐**。它是唯一能让外部后端拿到"会话生命周期 + 取消 + 权限 + MCP"的通道，且协议标准化（ACP 是公开协议，非 dsh 私有）。

### P4 · seam 级深集成（**不建议**）

- **形态**：把 dsh 的 Cordis seam（fs/shell/tools/subagent/llm）与平台工具系统、本体、知识库打通——例如让 dsh 调平台的本体 SPARQL 工具、让 dsh 的会话落平台库。
- **为什么不可取**：
  1. **需要 TypeScript 插件开发**——dsh 插件是 TS，平台是 Go，等于在平台仓库里养一条 Node 工具链，与 **D-O15**（零 venv / 一条命令启动）精神冲突（虽禁令字面只限 Python）。
  2. **预发布无兼容承诺**——官方明言 developer preview、迭代极快有破坏性变更，深集成等于把平台的稳定性押在外部预发布件上。
  3. **与 D-O5（开源优先、避免重复造轮子）不冲突但也不获益**——平台已有 eino-adk 主壳做深度能力，dsh 的定位就是**对照位与形态样本**，深集成会让两条主线互相纠缠。
  4. **成本量级跳变**：从人日到人周，且维护面随 dsh 版本漂移。
- **结论**：**不做**。若未来确需"平台能力进 dsh"，正确抓手是 **MCP**——把平台工具包成 MCP server，经 **P3 的 MCP 挂载面**接入，dsh 侧零插件开发。

### P0 · 不集成（人工对照）

- 保留为教学与选型留档手段。**不做为交付项**。

---

## 5. 能力映射矩阵（五条路径横向对照）

| 平台能力 | P0 人工 | P1 headless（现状） | P2 SDK | P3 ACP | P4 深集成 |
| --- | --- | --- | --- | --- | --- |
| Chat | — | ✅ | ✅ | ✅ | ✅ |
| Stream | — | ⚠️ 整段一条 delta | ✅ 真增量（`session.event`） | ✅ 真增量（`session/update`） | ✅ |
| 工具事件可见 | — | ❌ 黑盒 | ✅ **全量会话事件** | ⚠️ 标准语义更新（无 DSH 专有呈现） | ✅ |
| Skills | — | ⚠️ instruction 注入 | ⚠️ instruction 注入 | ⚠️ instruction 注入 | ✅ 可融合 |
| MCP | — | ⚠️ instruction 注入 | ❌ 无挂载面 | ✅ **tools（stdio + Streamable HTTP）** | ✅ |
| 权限/审批 | — | ❌ | ❌（server→client 请求未实现） | ✅ **`session/request_permission`** | ✅ |
| 取消 | — | ⚠️ ctx cancel 杀进程 | ⚠️ 只能关运行时 | ✅ **提示词级 `session/cancel`** | ✅ |
| Resume | — | ❌ | ❌（无 resume 方法） | ✅ **`session/resume`** | ✅ |
| AgentAsTool / Workflow | — | ❌ | ❌ | ❌ | ⚠️ 理论可行，代价极高 |
| 平台侧改造量 | 0 | 已完成 | 3~5 人日 | 6~10 人日 | 人周级 |
| 协议稳定性风险 | — | 低（CLI 输出） | **高（无版本协商）** | 中（标准 ACP v1） | 高 |

**读法**：P3 是唯一在"能翻正的能力数 / 改造量"上划算的路径；P2 的价值集中在**事件富度**（审计/回放/可观测），可作为 P3 的并行补充或前置铺垫。

---

## 6. 约束与合规判定

| 约束 | 判定 | 依据 |
| --- | --- | --- |
| **D-O15**（不引入 Python 运行时依赖；`run-dev.sh` 一条命令零 venv） | ✅ **不受影响** | dsh 始终按"外部 CLI、PATH 探测、未装即降级"处理，不进 run-dev 主链；Go 侧自研 stdio 客户端不引入任何 Node/Python 运行时 |
| **D-O5**（开源优先、避免重复造轮子） | ✅ 不冲突 | 协议客户端属"适配"非"自研替代"；平台不重写 harness |
| **凭据** | ✅ 平台不经手 | dsh 自管（`DEEPSEEK_API_KEY` 环境变量或 `dsh web` Models 页写入 DSH_HOME）；缺失时报结构化 `MISSING_CREDENTIAL` 透出 run.error |
| **版本锁定** | ⚠️ 必须 | 平台锁定 `0.1.5-rc.3`（Probe 显示版本）；SDK 通道 `version=0.0.1` 且客户端不校验 ⇒ **锁版本是唯一防线** |
| **网络暴露** | 🚫 禁止 | ACP `authenticate` 立即成功、无鉴权 ⇒ 只能本机 stdio；不得跨容器/跨主机裸连 |
| **工具裁剪抓手**（校正原档"Minimal 预设"说法） | — | 正确抓手是 **agent preset + `tools.restrict` + 沙箱模式 + 审批策略**；`sdk-minimal` 是"不应用 `dsh-base` 的完整显式配置树"，不是"最小工具集" |

### 勘误表（对早期二手结论的校正，沿用）

| 既有说法 | 一手结论 |
| --- | --- |
| "预设 Standard/PTC/Minimal/Creator" | 官方 profile 只有 `web` / `headless` / `sdk` / `sdk-minimal` / `acp`；PTC 是工具呈现模式 `native\|ptc\|both` |
| "自带 ReAct 主循环" | 官方词汇是 **turn / step**；循环本体 `ctx.agentLoop` 只是配置树里可替换的插件 |
| "Trajectory 审计" | 官方术语是 Session 事件日志 + transcript 投影；"trajectory" 非官方术语 |

---

## 7. 决策：推荐路径与分阶段落地

### 7.1 推荐组合

> **P3（ACP）为主通道 + P2（SDK）为事件富度补充 + P1（headless）保留兜底；P4 不做。**

理由：P3 是唯一能同时翻正 `Resume`、`MCP=tools`、取消、权限的通道，且走的是**公开标准协议**（ACP v1），dsh 侧的漂移风险低于私有 SDK 协议；P2 的 `session.event` 是"不过滤"的全量事件流，在需要运行过程审计/回放时无可替代。

### 7.2 分阶段

| 阶段 | 内容 | 门 | 产出 |
| --- | --- | --- | --- |
| **S0 协议 PoC**（0.5~1 人日） | 本机装锁版本 dsh；分别起 `dsh --profile acp` 与 `--profile sdk`，用裸 stdio 手工发 `initialize` / `session/new` / `session/prompt`，验证：①ACP 会话能否 resume；②`session/update` 增量粒度；③SDK `session.event` 实际事件类型分布 | **go/no-go**：任一通道握手不通即止，退回 P1 | PoC 记录 + 事件样本（落本档 §9 或新开小节） |
| **S1 长驻 stdio 适配器基座**（2~3 人日） | 新增 `streamingStdioAdapter` 形态：进程持有 + 关闭阶梯（stdin EOF→SIGTERM→SIGKILL，幂等）+ 行分帧传输 + 探测/降级复用 `cliAdapter` 治理 | — | 复用现有 Registry/Capabilities/前端，零前端改动 |
| **S2 ACP 主通道**（3~5 人日） | 13 个调用客户端 + `dshSessionId ↔ 平台会话` 映射 + 权限处理器（默认 deny/转审批）+ 事件转译（`session/update` → 平台 SSE） | 冒烟：切 dsh 后端跑一次多步工具对话，平台侧可见工具事件；中断后可 resume | `Resume: true` / `MCPMode: tools` 翻正（**需同步更新 `Capabilities` 表达**） |
| **S3 SDK 事件补充**（2~3 人日，可选） | SDK 通道 + 全量 `session.event` 落平台 `run_event`，用于审计/回放 | — | 运行过程可观测 |
| **S4 能力矩阵与文档回写** | 更新 02 §6.16 能力矩阵、01 REQ-160（或新立项 REQ）、15 号登记簿、20 号冒烟清单 | — | 交付闭环 |

> **立项提示**：若 S2/S3 要作为正式交付，须先查 `docs/18_REQ编号注册表.md` 分配 REQ 编号，再落 01/02 需求档（纪律 #1）。本档只出方案，不分配编号。

### 7.3 验收要点（建议）

1. 设置页「推理后端」面板能识别 dsh 并区分通道形态（headless / acp / sdk）；
2. ACP 通道下，一次多步工具对话中平台侧可见工具调用与结果事件（P1 下为黑盒）；
3. ACP 通道下，会话中断后可用 `session/resume` 续跑（P1/P2 均不可）；
4. 权限请求按平台审批策略处理，不静默放行；
5. dsh 未安装时全部通道降级为"不可用"，Agent 保存不受影响、运行时报错可懂；
6. 20 号冒烟清单对应模块全过。

---

## 8. 已交付现状与本次增量

- **已交付（M24/REQ-160 阶段一/二）**：`dsh --profile headless <task>` 契约 PoC ✅；第 4 个 `cliAdapter` ✅（锁版本 0.1.5-rc.3；`parsePlainLine` 降级；结构化 `MISSING_CREDENTIAL` 透出）；阶段三（agentd 镜像内置 Node+dsh）随沙箱线合流，未启动。
- **本次分析给出的增量**：现状是"最浅一档"；真正的嵌入应走 **ACP/SDK 长驻 stdio**，核心新增是**适配器形态**（而非再加一个 cliAdapter）与**事件转译层**。
- **与 REQ-142（多类型智能体研究）的关系**：dsh 是"编码型 harness"的形态样本；走 P3 后，其工具事件与会话语义可被平台观测，研究价值从"看输出"升级为"看过程"。

---

## 9. 风险台账

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| dsh 预发布、破坏性变更（1.6 万+ commits） | 协议/CLI 契约漂移导致适配失效 | 锁版本 + Probe 版本探测 + 适配器注记 pin 范围；升级需手动验证 |
| SDK 协议**无版本协商**（`version 0.0.1` 客户端不校验） | 静默不兼容 | 平台侧自行校验 `serverInfo.version` 并显式拒绝未知值 |
| ACP 无鉴权（`authenticate` 立即成功） | 网络暴露即等价任意代码执行 | 仅本机 stdio；禁止容器间/跨主机直连；纳入安全评审 |
| 长驻进程泄漏 | 僵尸 dsh 进程占用资源 | 复用官方关闭阶梯并幂等；平台侧加空闲超时与上限 |
| 仅 MCP 工具（ACP 限制） | 平台工具无法直接被 dsh 调用 | 走 MCP 包装，而非 seam 深集成 |
| 事件语义不完整（ACP 只给标准语义更新） | 运行过程信息有损 | 需要完整事件时并行 P2（SDK `session.event`） |
| 无取消（SDK 限制） | 放弃轮次只能关运行时 | 优先 P3；P2 仅用于可容忍"整进程丢弃"的场景 |
| Node 运行时与 D-O15 | 平台侧不得引入 Node 工具链 | dsh 恒为外部 CLI；协议客户端用 Go 自研 |

---

## 10. 待决问题（领取时确认）

1. **通道优先级**：是否接受"ACP 主 + SDK 辅"？还是先只做 P2（便宜、先拿到事件）再评估 P3？
2. **权限默认策略**：`session/request_permission` 平台侧默认 deny、转人工审批，还是按 Agent 配置预设放行？（建议默认 deny）
3. **MCP 挂载面**：是否在平台 UI 暴露"为 dsh 后端挂载 MCP server"的配置？还是固定由 profile patch 决定？
4. **`Capabilities` 表达**：新增字段（如 `mcp_mode: mountable`、`cancel: true`）还是扩枚举？需与 02 §6.16 表结构一并定。
5. **会话映射持久化**：`dshSessionId` 存平台哪张表、与平台 `conversation` 如何对应（一对一？一会话多 dsh session？）。
6. **与 REQ-142 合并推进还是独立交付**（建议：独立小交付，研究结论回填 REQ-142）。
7. **阶段三（agentd 镜像内置 dsh）是否仍推进**——若 P3 落地，"容器内跑 dsh"可由 REQ-191 运行环境统一配置承载，二者需排重。

---

## 11. 一手资料索引（对接相关）

| 主题 | 官方路径（`deepseek-ai/deepseek-harness@master`） |
| --- | --- |
| SDK 组总览 | `packages/sdk/README.zh.md` |
| SDK 协议（方法与通知全表） | `packages/sdk/protocol/README.zh.md` |
| SDK TypeScript 客户端 | `packages/sdk/client/README.zh.md` |
| SDK 服务端插件 | `packages/sdk/server/README.zh.md` |
| SDK 应用组合包（`--profile sdk`） | `packages/bundle/sdk-app/README.zh.md` |
| ACP 服务器（`--profile acp`） | `packages/acp/acp/README.zh.md` |
| ACP subagent 客户端（官方进程外委派范例） | `packages/subagent/subagent-acp/README.zh.md` |
| SDK subagent 提供方 | `packages/subagent/subagent-dsh-sdk/README.zh.md` |
| API Gateway（本档判定不适用，留档） | `docs/api-gateway.zh.md` |
| Python SDK（打包运行时形态） | `python/README.zh.md` |
| 基础组合包 `dsh-base` | `packages/bundle/base/README.zh.md` |

> **复核方法注记**：本机直连 `raw.githubusercontent.com` 会 SSL 握手失败（exit 35）；`api.github.com/.../contents/<path>` 可用但未认证限 60 次/小时且会被限流；**WebFetch 通道不受限流影响**，后续取一手文档优先用它。递归 tree API 在 master 上会被截断，需逐目录 `contents` 查询。

---

## 附：立项时点调研结论（2026-09-25，已兑现）

> 原《DeepSeek_Harness接入可行性_20260925》的核心判断保留如下，作为本档的历史依据（其中 dsh 形态陈述已按 §6 勘误表校正）：

1. **架构匹配度极高**：M13/D-O13 的推理后端可插拔体系（`Backend` 接口 + `cliAdapter` 骨架 + PATH 探测 + 能力降级语义）就是为"接入外部执行外壳"设计的——dsh 与 claude-code/opencode/aider 完全同类，新增适配器约 60~100 行、零架构改动。✅ 已兑现为 M24 阶段二。
2. **当时的关键不确定性**（dsh 非交互 CLI 形态未有一手确认）已由 PoC 消除：`dsh --profile headless <task>` 成立。✅
3. **未兑现的部分**：本档 §7 的 P2/P3 长驻通道——当时未识别到 dsh 有 ACP/SDK 两条官方进程外通道，故方案止步于 CLI 口径。**这是本次重新分析的主要增量。**
