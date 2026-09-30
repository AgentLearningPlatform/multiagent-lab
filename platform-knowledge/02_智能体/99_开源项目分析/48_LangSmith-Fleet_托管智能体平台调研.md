---
module: 智能体
topic: LangSmith Fleet（托管无代码智能体平台）调研
desc: 调研 LangSmith Fleet（原 Agent Builder）的实现架构与原理：底层 Deep Agents harness 的四层栈（虚拟文件系统 / 上下文管理 / 委派 / 控制）、记忆系统（COALA 映射到 AGENTS.md·tools.json·skills·subagents，Postgres 之上的虚拟文件系统、默认人在环）、上下文工程四段（输入·压缩·隔离·长期记忆）、技能的渐进式披露、身份双形态（Claws/Assistants）与 Agent Auth 的 OAuth 中断、Channels/Schedules 触发、审批门控与可见性规则、LangSmith Tool Server 的 MCP 网关，并提炼可借鉴亮点（通用行业调研，不绑定任何具体平台的现状）。
synced: 2026-09-30
---

# LangSmith Fleet 调研：托管智能体平台的实现架构与原理

> **品牌说明（易踩坑，请先读）**：Fleet **不是**新产品，而是 **LangSmith Agent Builder 的更名**。官方文档原话："*Agent Builder is now LangSmith Fleet. All existing agents, configurations, and integrations continue to work. No action is required.*" 检索时若只按旧名会漏掉大量现行文档——官方文档路径已统一为 `docs.langchain.com/langsmith/fleet/*`（旧路径 `agent-builder-essentials` 仍可访问但已改标题）。
> **与 Open Agent Platform 的关系**：`langchain-ai/open-agent-platform`（OAP）**已于 2026-02-25 被官方 deprecated**，官方推荐的继任方案就是 Fleet。本档因此把主题从 OAP 切换到 Fleet——**OAP 是"被放弃的形态"，Fleet 是"官方押注的形态"**，两者的设计重心差异本身就是一个重要结论（见 §11）。
> **调研方法**：以 LangChain 官方文档为**一手来源**——`fleet/{index,essentials,channels,schedules,agent-identity,manage-agent-settings,tools,skills,mcp-framework}`、`agent-auth`、`oss/python/deepagents/overview`，辅以官方工程博客《How we built Agent Builder's memory system》。凡官方文档未给出的一律标注"官方未给出"。

---

## 1. Fleet 是什么

| 项 | 内容 |
|---|---|
| 全称 | **LangSmith Fleet**（原名 LangSmith Agent Builder） |
| 出品 | LangChain |
| 定位 | **No-code 智能体平台**——创建、托管、运行"替你把日常活干了"的 Agent |
| 底层 harness | **Deep Agents**（LangChain 的开源 agent harness，构建于 LangChain + LangGraph 之上） |
| 部署形态 | 默认 **SaaS**（数据 residency 跟随 LangSmith 配置）；**自托管处于 beta** |
| 计费 | **LCU（LangChain Compute Units）**——Free 5 LCU/组织/月，Plus 25 LCU/月（超额计费）；新计费 2026-07-15 起适用，存量组织 2026-10-01 过渡 |

官方一句话定位：*"Create helpful AI agents without code. Start from a template, connect your accounts, and let the agent handle routine work while you stay in control."*

官方列的四类用途：自动化日常任务（起草邮件、总结更新、整理信息）→ 连接应用带入上下文 → **在聊天或你工作的地方（如 Slack）使用** → **用简单的审批保持控制**。

> **注意最后两条**：Fleet 的卖点不是"搭 Agent 的画布"，而是"**Agent 在哪儿被用起来**"和"**怎么被管住**"。这是它与 OAP 最根本的分野。

---

## 2. 技术栈分层（关键：Fleet 是第四层，不是第一层）

理解 Fleet 的架构，必须先看它站在谁肩上。官方对三层关系的表述很清楚：

| 层 | 角色 | 提供什么 |
|---|---|---|
| **LangChain** | 框架（core building blocks） | Agent 的核心构件 |
| **LangGraph** | **runtime** | **durable execution、streaming、human-in-the-loop、store** |
| **Deep Agents** | **agent harness**（独立库 `deepagents`） | 文件系统、子代理生成、长期记忆 + 可选的规划与技能 |
| **LangSmith Fleet** | 产品层 | 无代码配置 UI、Channels、凭据、审批、调度、治理 |

官方对 Deep Agents 的定义值得原样记住：

> *"Deep Agents is an **'agent harness'**. It is the same core tool calling loop as other agent frameworks, but with built-in capabilities that make agents reliable for real tasks."*

即：**循环本身没变，变的是循环之外被内置的那一圈"让它能干真活"的能力**。这正是"harness"一词的含义，也是近年 Agent 工程的重心所在。

---

## 3. 底层架构：Deep Agents 的四层栈

Deep Agents 的能力可以拆成四组，外加一个执行环境：

```
Deep Agents (agent harness)
├── 执行环境 Execution environment
│   ├── Tools（自定义函数 / API / 数据库 / MCP）
│   ├── Virtual filesystem（可插拔后端）
│   ├── Filesystem permissions（声明式访问控制）
│   ├── Code execution（沙箱 shell / JavaScript interpreter）
│   └── Streaming（typed event streams，含 stream.subagents）
├── 上下文管理 Context management
│   ├── Skills（SKILL.md，渐进式披露）
│   ├── Memory（AGENTS.md，持久，always loaded）
│   ├── Summarization and context offloading（自动压缩历史与大型结果）
│   └── Prompt caching（静态 prompt 段缓存）
├── 委派 Delegation
│   ├── Task planning（opt-in 的 write_todos 工具）
│   └── Subagents（内置 task 工具，ephemeral 子代理）
└── 控制 Steering
    └── Human-in-the-loop（interrupt_on + LangGraph interrupts）
```

### 3.1 中间件栈与"不可移除"的脚手架

| 中间件 | 状态 | 说明 |
|---|---|---|
| `FilesystemMiddleware` | **required scaffolding** | **不能**通过 `excluded_middleware` 移除，尝试移除会被有意拒绝 |
| `SubAgentMiddleware` | **不能移除** | 要禁用子代理应通过 **harness profile** 关闭 auto-added subagent + `subagents=` 不传同步子代理 |
| `TodoListMiddleware` | **opt-in**（v0.7 起） | 启用后提供 `write_todos` 工具；早期版本默认包含 |

> **可借鉴点**：把文件系统与子代理定义为**不可移除的脚手架**，把规划定义为可选插件——这是一条明确的能力分层判断：**文件系统与委派是 Agent 的地基，规划是策略**。

### 3.2 虚拟文件系统（VFS）

Deep Agents 提供**可插拔后端**的虚拟文件系统：in-memory state / local disk / LangGraph store / composite routing / 自定义后端。

默认暴露的文件系统工具：`ls`、`read_file`、`write_file`、`edit_file`、`glob`、`grep`、`delete`（需 `deepagents>=0.7`）、`execute`（**仅 sandbox 后端**）。

`read_file` 支持 offset/limit 分页，且对非文本文件返回 multimodal content blocks（支持 `.png/.jpg/.mp4/.wav/.mp3/.pdf/.pptx` 等扩展名）。

### 3.3 文件系统权限（声明式，值得单独看）

通过 `permissions=` 传入规则列表，每条规则含：

- `operations`：`"read"` 和/或 `"write"`
- `paths`：Glob 模式（如 `/workspace/`、`**/*.py`）
- `mode`：`"allow"` 或 `"deny"`

求值规则：**按声明顺序自上而下，first-match-wins；若无规则匹配，则默认允许**。

官方给的三个用途：把 Agent 限制到特定目录、保护敏感文件（`.env`、credentials）、**给子代理比父代理更窄的访问权**。

**边界**：权限机制**不适用于 sandbox backends**（后者通过 `execute` 支持任意命令执行）。

### 3.4 工具子集的裁剪（两种方式）

1. **隐藏**：`register_harness_profile` + `HarnessProfile(excluded_tools=frozenset({...}))`——中间件保留，但模型看不到工具。
2. **限制**：传入自定义 `FilesystemMiddleware(backend=..., tools=[...])`——**`read_file` 必须包含，遗漏会抛 `ValueError`**。

传播规则很细：主 Agent 的自定义 `FilesystemMiddleware` 会**被 `general-purpose` 子代理继承**，但**declarative subagents 不继承**，需各自单独配置。

### 3.5 子代理（`task` 工具）

内置 `task` 工具，主 Agent 用它创建 **ephemeral subagents**：

| 特性 | 行为 |
|---|---|
| **Fresh context** | 每次调用创建拥有自身上下文的新实例 |
| **Autonomous execution** | 独立运行至完成 |
| **Single handoff** | 只返回一份最终报告 |
| **Stateless** | 无状态，不能返回多条消息 |
| 默认策略 | `general-purpose` subagent 默认启用 |

流式上，Deep Agents 在 LangGraph 事件流之上增加了 **`stream.subagents`**——每个委派任务有自己的 handle 与独立的 message / tool-call / 嵌套子代理流。

---

## 4. 记忆系统：Fleet 最有价值的一节

官方工程博客《How we built Agent Builder's memory system》**完整公开了设计取舍与踩过的坑**，是整个 Fleet 体系中最有参考价值的文档。

### 4.1 为什么优先做记忆

官方给的理由很反直觉但很有说服力：大多数 AI 产品初期**不做**记忆。他们优先做的理由是**用户的使用模式**——Fleet 的 Agent 不是通用助手，而是**为特定任务定制**的，"*it is doing the same task over and over again*"。通用助手里跨会话的经验复用率低；任务专用 Agent 里，上周的教训这周直接可用，"*it would be a bad user experience if memory is not present*"。

> **这是一条可直接引用的判据**：**记忆的投入产出比，取决于任务重复度而非产品成熟度。** 通用型产品做记忆 ROI 低，任务专用 Agent 做记忆 ROI 高。

### 4.2 记忆 = 文件（但不是真文件系统）

核心设计：**把记忆表示为一组文件**，理由是"模型很擅长使用文件系统"——于是无需给 Agent 专门工具，只要给它文件系统访问权即可。

但关键实现细节是：

> *"We actually **do not use a real filesystem** to store these files. Rather, we store them in **Postgres** and expose them to the agent in the shape of a filesystem."*

即 **Postgres 存储 + 文件系统形态暴露**（虚拟文件系统）。官方给的理由：LLM 擅长用文件系统的心智模型，但从基础设施角度数据库更简单高效。该 VFS 由 DeepAgents 原生支持且**完全可插拔**（可换 S3、MySQL 等）。

### 4.3 四类文件与 COALA 映射

官方采用第三方（COALA 论文）的三类记忆定义，并映射到文件：

| COALA 类别 | Fleet 载体 | 加载方式 |
|---|---|---|
| **Procedural**（决定行为的规则） | **`AGENTS.md`** + **`tools.json`** | `AGENTS.md` **自动插入系统提示** |
| **Semantic**（关于世界的事实） | **skills** + 其他知识文件 | **不自动注入**，需 Agent 按需 `read_file` |
| **Episodic**（过去行为的序列） | **故意未做** | 官方判断"对这类 Agent 不如前两类重要"，列入未来工作 |

文件布局（官方以内部的 LinkedIn recruiter agent 为例）：

```
AGENTS.md          核心指令
subagents/         子代理定义（linkedin_search_worker）
tools.json         MCP server 与工具子集
<其他知识文件>      如各候选人的 JD，Agent 边工作边维护
```

**为什么自定义 `tools.json` 而不用标准 `mcp.json`**：官方明说——为了**只暴露 MCP server 中的工具子集，避免 context overflow**。

> 这条很值得抄：MCP 标准配置粒度是"整个 server"，而上下文预算要求的是"工具子集"。**标准与工程约束冲突时，他们选择自定格式而非硬扛标准。**

### 4.4 记忆写入与人在环

- Agent 通过 **`write_file` / `edit_file`** 把记忆写进 **memories folder**，"in the hot path"（边干活边写）。
- **默认所有记忆编辑需人工批准**，官方明说目的是"*minimize the potential attack vector of prompt injection*"——把记忆写入当作**可被提示注入污染的写操作**来防。
- 提供关闭开关（官方原话 **"yolo mode"**）；且明确建议：**跑在定时调度上的 Agent 应关闭该批准**，否则每次涉及记忆更新都会无限期等待人工。

### 4.5 自述的四条工程教训（本节最值得记）

1. **最难的是 prompting。** 官方原话："*The hardest part of building an agent that could remember things is prompting.*" 几乎所有表现不佳的情况，解法都是改提示词——包括"该记时没记""不该记时记了""往 `AGENTS.md` 写太多而该写进 skills""不知道 skills 文件的正确格式"。**他们有一个人全职做记忆相关的 prompting**（占团队相当大比例）。
2. **必须校验文件 schema。** 某些文件有特定 schema（`tools.json` 需有效 MCP server、skills 需合法 frontmatter），Agent 有时会生成非法文件。解法：增加显式校验步骤，**校验失败把错误抛回 LLM 而不是提交文件**。
3. **Agent 擅长"添加"，不擅长"压缩"。** 官方实例：邮件助手开始**逐个列举**所有要忽略的 cold outreach 发件方，而不是泛化成"忽略所有 cold outreach"。解法仍是显式提示（让用户提示 Agent 去 compact、或在结束时反思并更新记忆）。
4. **显式提示仍有价值。** 即便 Agent 能自更新记忆，官方仍发现两类场景值得用户显式提示：工作结束后反思并补记遗漏；主动要求压缩记忆以解决"只记具体案例不泛化"。

**未来工作（官方列）**：episodic memory（把对话历史暴露为文件）、每日运行的后台记忆进程、显式 `/remember` 命令、超越 grep 的语义搜索、user 级与 org 级记忆层级。

**可移植性收益**：官方指出把 Agent 表示成 markdown + json 文件后，Agent Builder 建的 Agent 可以**近乎无摩擦地在 Deep Agents CLI、Claude Code、OpenCode 上运行**——这是锁定型 DSL 做不到的。

---

## 5. 上下文工程：官方的四段式

Deep Agents 把长上下文管理拆成四段，这个分解方式本身就很清晰：

| 段 | 内容 |
|---|---|
| **1. Input context** | System prompt、memory、skills、tool prompts 定义起始上下文 |
| **2. Compression** | 内置 offloading 与 summarization 压缩对话历史与大型中间结果 |
| **3. Isolation** | 子代理隔离重型子任务，**只返回最终结果** |
| **4. Long-term memory** | 虚拟文件系统中的持久存储，跨 thread 传递信息 |

即：**先控制输入 → 再压缩过程 → 再用隔离避免膨胀 → 最后跨会话沉淀**。

配套机制：
- **Prompt caching**：使用 Anthropic 模型或 Bedrock（Claude / Nova）模型时，`create_deep_agent` **默认自动**对 system prompt 的静态部分应用 prompt caching，无需配置。
- **Human-in-the-loop**：通过 `interrupt_on={"edit_file": True}` 这类映射在敏感工具调用前暂停（基于 LangGraph interrupts），可批准、加指导、或修改工具输入。

---

## 6. 技能机制：渐进式披露

Fleet 的 skills 构建于 Deep Agents，遵循 **Agent Skills specification**（`agentskills.io`），核心文件是 `SKILL.md`。

### 6.1 加载策略（省 token 的关键）

> **启动时只加载 skill 的 name 与 description**；Agent 依此判定是否相关，**完整 skill 文件仅在判定相关时才读取**。

官方给的两条收益：节省 token；**避免系统提示塞太多内容导致幻觉与错误回答**。

### 6.2 描述怎么写（官方给的是判据，不是建议）

> *Write the description as **instructions for when to use the skill**, not as a label for what it does.*

- 反例："Helps with email."
- 正例："Use when drafting, replying to, or summarizing emails. Covers tone adjustments, follow-up scheduling, and inbox triage."

官方同时给了两类失败模式：**描述太宽泛 → Agent 即使该用也不用**；**描述与其他 skill 重叠 → 路由错或选不出**。并建议随技能库增长定期审查重叠。

### 6.3 私有 vs 共享

| | 私有 skill | 共享 skill |
|---|---|---|
| 存储 | Agent 自己的长期记忆 | 工作区 Skills 页面 |
| 可见性 | 仅该 Agent | 工作区所有 Agent 可见 |
| 编辑 | — | **仅创建者可编辑/删除** |
| 同步 | — | 加到任意 Agent 后**随更新保持同步**；general-purpose chat 自动获取 |

### 6.4 "把修复固化成技能"（我认为是最实用的一条）

官方的用法说明：

> 当 Agent 某类任务做错了，纠正它，然后说 **"Capture this fix as a skill."** ——Agent 会创建一个 `SKILL.md` 编码正确行为；**后续会话在处理该类任务前会先读这个技能，而不是从零推理**。

这是把"一次性纠错"转化成"永久约束"的机制，等价于给 Agent 加了一条回归测试用例。

### 6.5 技能可下放到本地编码 Agent

```
langsmith fleet skills pull web-research --format pretty
# Installed skill "web-research" to ~/.agents/skills/web-research
#   Linked: ~/.claude/skills/web-research
```

默认装到 `~/.agents/skills/[skill-name]/` 并 symlink 到 `~/.claude/skills/`；支持 `--global=false`（项目级）、`--agent claude|cursor|codex`、`--copy`。

> **含义**：同一份技能资产可以跨"Fleet 托管 Agent"与"本地编码 Agent"复用——技能是**独立于运行时的资产**。

---

## 7. 身份与凭证：两种形态，且不可逆

Agent identity 决定 **Agent 用谁的凭证**去访问应用和服务。官方给了两个具名形态：

| | **Fixed credentials（"Claws"）** | **User credentials（"Assistants"）** |
|---|---|---|
| 行为 | 始终用同一套 API key / OAuth token | 用**交互用户**的 token，代表该用户行事 |
| 适用场景 | 共享服务（team Slack bot、每日简报）；需要单套已认证账户；**跑在 Channels 或 Schedules 上（官方明说此类必须使用固定凭证）** | 每用户应通过自己的账户行事；需要 per-user 访问控制；**审计需要反映是哪个用户执行了动作** |
| 审计 | 动作归属于 Agent 所有者连接的账户 | 审计体现具体用户 |

**关键约束：一旦设定，身份不可更改。**

### 7.1 Agent Auth：OAuth 以"中断"形态呈现

`langchain-auth` / `@langchain/auth`（**beta**）：

- 先在 workspace 配置 OAuth provider（唯一 `provider_id`，callback 形如 `https://smith.langchain.com/host-oauth-callback/{provider_id}`）。
- 运行时调用 `client.authenticate(provider=..., scopes=[...], user_id=...)`；**token 默认 scoped 到调用方 Agent（Assistant ID）**，也可显式传 `agent_id`。
- **若需要认证，SDK 抛出一个 interrupt**——Agent 执行暂停，向用户展示 OAuth URL；用户完成认证并收到回调后，**Agent 从中断点继续执行**。
- Token **被存储并自动刷新**，后续用户或 Agent 使用该服务无需重走 OAuth 流程。

> **可借鉴点**：把"缺凭证"建模成**可恢复的中断**而不是失败——与工具审批中断同构。这样"首次授权"不需要重试整条链路。

---

## 8. 触发与运行：Channels 与 Schedules

Fleet 把"Agent 何时开始跑"明确分成两套机制，**二者与身份形态强耦合**（Channels/Schedules 只能用固定凭证）：

| 机制 | 触发源 | 说明 |
|---|---|---|
| **Channels** | 外部事件 | Gmail（新邮件）、Slack（频道提及或 DM）、Microsoft Teams（会话消息） |
| **Schedules** | 时间 | 循环定时，**UTC**；可带自定义 prompt |

### 8.1 Channels 的实现细节

- **Gmail channel 只监控 primary inbox**——官方明确排除三类：alias 邮件、邮件列表/群组邮件、因过滤器未进收件箱的邮件（含垃圾箱/回收站等）。
- **Slack channel**：一次认证后一键把 Agent 加入 Slack，并**用 Agent 的 name / description / icon 配置一个 Slack app**；在频道中提及或发 DM 即可启动一次 run。
- **可暂停/恢复**：`Pause channels` / `Resume channels`，无需移除渠道。
- **Thread 行为差异**：无 channel 的 chat agent 响应会把 thread 标记为 **unread**；channel-based agent 默认保持 **read**。

### 8.2 Schedules 的一个有意思用例

官方列举的用例中有一条值得单独记：**Memory synthesis**——"*Periodically review and consolidate the agent's memory files to keep context clean and relevant.*"

> 即用**定时任务去补 Agent 不擅长的"压缩/归纳"**（见 §4.5 教训 3）。这是用编排层能力补模型层短板的典型做法。

---

## 9. 治理：审批、可见性与计费

### 9.1 工具级审批门控

每个工具在 Connections drawer 可设 **approval mode**：

| 模式 | 行为 |
|---|---|
| **Auto** | 自动执行，不询问 |
| **Ask** | Agent 暂停并等待批准 |

暂停后用户有两个选项：**Accept**（放行）或 **Reject**（拒绝并告知要改什么）。

**Slack 集成**：当 Agent 由 Slack 触发时，批准请求**直接以 Approve / Deny 按钮形式出现在 Slack 线程里**，用户无需离开 Slack。

> **这条很关键**：审批的**发生地跟随触发渠道**——在哪儿发起就在哪儿批。否则"发到别处去审批"会破坏流式体验。

### 9.2 工作区 Agent vs 私有 Agent 的可见性规则（设计精细）

| 维度 | 规则 |
|---|---|
| **Threads** | **始终 user-scoped**——即使 Agent 是工作区共享的，聊天历史也只对创建者可见 |
| **System prompt / tools / sub-agents** | 工作区 Agent 上**公开**；他人不能改原版，但**克隆后可改** |
| **Channel type** | 公开（例如"收到 Slack 消息"），但**具体连接不共享**（哪个 Slack 频道、哪个 Gmail 地址）——让克隆者知道要用什么渠道，但拿不到原用户的连接 |
| **OAuth** | 私有 Agent 凭证 scoped 到创建者；工作区 Agent **按每用户 scoped**，新用户克隆须重新认证 |
| **Secrets** | 两者都用 workspace-scoped LangSmith secrets |

### 9.3 计费：LCU

- **Free**：5 LCU / 组织 / 月，用尽后**暂停新 run**直到重置或升级。
- **Plus**：25 LCU / 组织 / 月，超额计费。
- 额度**跨组织共享、每月重置**。
- 官方说明：一次 run 可能包含多次模型调用，**更长的任务、更大的上下文、更高的档位消耗更多 LCU**。

---

## 10. 工具层：LangSmith Tool Server 与 MCP 网关

`langsmith-tool-server`（PyPI）+ `langchain-cli-v2`，用于两类场景：构建与 Fleet **Agent Auth** 集成的自定义工具；或作为**独立 MCP 网关**给自建 Agent 用（**Fleet 用户无需直接接触它**）。

### 10.1 自定义工具

```python
from langsmith_tool_server import tool

@tool
def add(x: int, y: int) -> int:
    """Add two numbers."""
    return x + y
```

### 10.2 MCP 网关（聚合多 server 到单一端点）

`toolkit.toml` 配置：

```toml
[[mcp_servers]]
name = "weather"
transport = "streamable_http"
url = "http://localhost:8001/mcp/"

[[mcp_servers]]
name = "math"
transport = "stdio"
command = "python"
args = ["-m", "mcp_server_math"]
```

**关键实现细节**：所有 MCP 工具在网关上**以 server 名做前缀**（`weather_get_forecast`、`math_add`）来避免命名冲突。

> **可借鉴点**：多源工具聚合时，**前缀化命名空间**是解决工具名冲突最省事的工程手段，比动态重命名或冲突检测简单得多。

### 10.3 工具级 OAuth 与自定义认证

```python
@tool(auth_provider="google",
      scopes=["https://www.googleapis.com/auth/gmail.readonly"],
      integration="gmail")
async def read_emails(context: Context, max_results: int = 10) -> str:
    credentials = Credentials(token=context.token)
    ...
```

带 `auth_provider` 的工具必须：`context: Context` 作为首参、至少指定一个 scope、通过 `context.token` 发认证请求。

自定义认证（`auth.py`）则在每个请求上运行，须返回含 `identity`（可选 `permissions`）的 dict：

```python
@auth.authenticate
async def authenticate(authorization: str = None) -> dict:
    ...
    return {"identity": user.id}
```

---

## 11. 从 OAP 到 Fleet：官方为什么换形态

这一节是理解"该抄什么"的前提。

| | Open Agent Platform（已 deprecated） | LangSmith Fleet |
|---|---|---|
| 交付 | 开源仓库，需自托管（还需 Supabase + LangConnect + LangSmith + MCP） | 托管 SaaS（自托管 beta） |
| 核心动作 | **在画布上搭 Agent** | **让 Agent 在你工作的地方干活** |
| 重心能力 | 配置驱动 UI、`x_oap_ui_config`、Supervisor 委派、RAG 服务 | Channels、Approvals、Memory、Schedules、Skills、治理 |
| 配置方式 | 表单 | **表单 + 直接跟 Agent 聊天让它自己改**（"Add the Slack tools so you can respond to messages."） |

**结论**：官方废弃 OAP 的理由不是"功能不够"，而是**形态不对**——无代码构建画布可复制性极高，**托管运维与运行治理才是护城河**。搭 Agent 这一步甚至被"跟 Agent 聊天让它自己改"取代了。

---

## 12. 可借鉴亮点

> 本节面向**智能体构建平台**给出通用借鉴判断，不绑定任何具体平台现状。每条标注迁移难度与前提。

### ★★★ 高价值、与模型能力无关（纯架构决策）

1. **把"记忆"实现为"虚拟文件系统"，而非专用记忆 API。**
   文件心智模型是 LLM 天然擅长的，于是无需为记忆设计专用工具——给文件系统访问权即可；底层却用数据库（Postgres）存储，**可插拔后端**。这同时拿到了"LLM 友好"与"基础设施高效"，还顺带获得**可移植性**（同一份 `AGENTS.md` + `SKILL.md` 可在其他 harness 上运行）。

2. **按 COALA 分类，并明确"哪一类不做"。**
   Procedural（`AGENTS.md` + `tools.json`）自动注入；Semantic（skills + 知识文件）按需读取；**Episodic 明确不做并说明理由**。有分类框架才谈得上取舍——多数系统的记忆是"一堆向量 + 一堆文本"，没有类别就没有加载策略。

3. **记忆写入默认走人工批准，且把它当作提示注入的攻击面。**
   官方明说这是为 *minimize the potential attack vector of prompt injection*。记忆是**会被后续会话自动读取的持久化写入**，其危险等级高于一次性工具调用——把它放进审批通道是正确的威胁建模。

4. **校验失败把错误抛回 LLM，而不是静默提交或静默丢弃。**
   `tools.json` 需有效 MCP server、skills 需合法 frontmatter。官方解法是显式校验 + **把错误回抛给模型让它重试**。这是"让 Agent 能自我纠错"的最小可靠形态。

5. **技能渐进式披露：启动时只加载 name + description，全文按需加载。**
   一举三得：省 token、避免系统提示过载导致幻觉、让技能库可以持续变大。配套的描述写作规范（写"何时用"而非"是什么"）是让路由准确的前提。

6. **"把修复固化成技能"——把一次性纠错变成永久约束。**
   纠正后立即 `Capture this fix as a skill`，后续同类任务先读技能再动手。等价于给 Agent 加回归测试，是**最低成本的持续改进机制**。

7. **文件系统权限用声明式规则 + first-match-wins，且可给子代理更窄权限。**
   `operations / paths / mode` 三元组足够表达绝大多数约束，比命令式 hook 好审计。注意其默认策略是"无匹配则允许"——**收紧型系统应显式加 deny 兜底**。

8. **把"缺凭证"建模成可恢复中断，与工具审批中断同构。**
   认证未完成时抛 interrupt → 展示 OAuth URL → 完成后从中断点继续；token 存储并自动刷新。避免"首次授权要重跑整条链路"。

9. **审批的发生地跟随触发渠道。**
   Slack 触发就在 Slack 线程里给 Approve / Deny 按钮。审批若跳到另一个界面，常驻 Agent 的体验就断了。

10. **上下文工程四段式分解（输入 → 压缩 → 隔离 → 长期记忆）。**
    四段各自独立可优化，也各自可独立验证。比"加个压缩"这种单点做法更成体系。

### ★★ 中价值，需结合自身形态改造

11. **用定时任务补模型短板（memory synthesis）。**
    官方承认 Agent 不擅长归纳压缩，解法之一是用 schedule 定期整合记忆文件。**用编排层补模型层**是这个思路的精髓，可迁移到任何"模型不擅长 X"的场景。

12. **身份双形态（固定凭证 / 用户凭证），且设定后不可更改。**
    这个"不可逆"看似苛刻，实则合理——凭证归属决定了审计语义与数据边界，中途切换会让历史动作的解释权漂移。借鉴时值得保留这个约束，但要提供"克隆一个新 Agent 换身份"的逃生通道（Fleet 正是这么做的）。

13. **工作区共享 Agent 的可见性分层：thread 永远私有、配置公开、连接不共享。**
    三条规则覆盖了"共享一个 Agent"时最容易出事的三个面（对话内容、配置、凭证），可直接照搬为共享策略模板。

14. **多源工具聚合时用具名前缀命名空间。**
    `weather_get_forecast`、`math_add`。简单、可预测、无冲突检测开销。

### ★ 参考即可，慎入

15. **Fleet 的产品形态本身（SaaS + Channels + 4,000 级应用生态）**：是商业生态位，不是可复现的技术机制。
16. **"跟 Agent 聊天改配置"**：依赖强模型与强护栏（记忆写入需审批），护栏不足时风险高于收益。
17. **LCU 抽象计费**：与托管商业模式绑定，自托管平台不适用。

### 📌 一条战略判断

从 OAP 到 Fleet 的转向，与同期 OpenAI Dots（见 49 号）的重心**高度一致**：**都不在"怎么搭 Agent"，而在"怎么让 Agent 被用起来、被管住"**。

对任何构建智能体平台的一方，这意味着**审批门控、记忆与技能沉淀、渠道接入、可观察性与上下文重置**这几项的优先级，很可能高于继续丰富构建画布。无代码画布的可复制性已经被官方用自己的产品迭代证明了。

---

## 13. 边界与风险（引用本文请一并引用）

1. **自托管处于 beta**；默认 SaaS，数据 residency 跟随 LangSmith 配置。
2. **Agent Auth 处于 beta** 且在活跃开发中。
3. **Agent identity 一旦设定不可更改**；Channels/Schedules 强制使用固定凭证。
4. **文件系统权限不适用于 sandbox backends**（后者可经 `execute` 执行任意命令）。
5. **permissions 无匹配时默认允许**——收紧型场景需显式 deny 兜底。
6. **记忆的提示词工程成本被官方明确为"最难的部分"**，且需一人全职投入；不要低估自建记忆系统的调教成本。
7. **Agent 不擅长压缩/归纳**是官方实测结论，不是推测；需靠显式提示或定时任务补偿。
8. **Episodic memory 官方明确未做**，属未来工作；引用时不要说 Fleet 具备完整三类记忆。
9. **§4.3 的 `tools.json` 是自定义格式**（非标准 `mcp.json`），目的是工具子集裁剪；若照搬需接受"偏离标准"的代价。
10. 本档关于 Fleet 的能力陈述均取自官方文档；**LCU 具体费率、Channel 支持列表等会随版本变化**，决策前请回官方页面核对。

---

## 14. 资料索引（一手优先）

**官方产品文档** `docs.langchain.com/langsmith/fleet/*`
- `fleet/index` — 产品定位、隐私与免责声明、自托管 beta 说明
- `fleet/essentials` — **核心**：Agent 身份、侧边栏 drawer 结构、Channels、Human-in-the-loop（Auto/Ask）、Memory 双源、Tools、Models 与 LCU
- `fleet/channels` — Gmail / Slack / Teams 触发与 thread 行为
- `fleet/schedules` — 定时运行与 memory synthesis 用例
- `fleet/agent-identity` — Claws / Assistants 双形态
- `fleet/manage-agent-settings` — 模型、访问与可见性、记忆批准、程序化调用、暂停与删除
- `fleet/tools` — 内置工具清单（Gmail / Slack / GitHub / Linear / BigQuery / Exa / Tavily …）
- `fleet/skills` — 渐进式披露、私有/共享、`langsmith fleet skills pull`
- `fleet/mcp-framework` — LangSmith Tool Server、MCP 网关、`toolkit.toml`、认证
- `langsmith/agent-auth` — OAuth provider 配置、interrupt 式认证、token 存储与刷新

**底层 harness**
- `docs.langchain.com/oss/python/deepagents/overview` — **本档 §3/§5 的一手来源**：四层栈、中间件、VFS、权限、子代理、prompt caching、`interrupt_on`

**官方工程博客**
- 《How we built Agent Builder's memory system》— https://www.langchain.com/blog/how-we-built-agent-builders-memory-system （**本档 §4 的一手来源**，含 COALA 映射、Postgres VFS、四条教训、未来工作）

**背景**
- `langchain-ai/open-agent-platform`（OAP，**2026-02-25 deprecated**）— 见 §11 形态对比

**取数坑记录**：LangChain 产品品牌在本调研期内从 **Agent Builder** 更名为 **Fleet**，官方文档路径随之从 `agent-builder*` 迁到 `fleet*`；旧路径仍可访问但标题已改。后续调研 LangChain 产品请优先用 `fleet` 关键词。另：`/fleet/sub-agents`、`/fleet/instructions` 当前为 404（内容合并进了 `essentials`）。

---

## 15. 汇总表

### 表 1 · 官方文档与资料

| 名称 | 类型 | 来源 | 关键内容 | 相关点 | 借鉴度 |
|---|---|---|---|---|---|
| Fleet / index | 产品页 | docs.langchain.com/langsmith/fleet | 定位、隐私、自托管 beta | 产品定位与边界 | ★★ |
| Fleet / essentials | **核心文档** | .../fleet/essentials | 身份、drawers、Channels、HIL、双源 Memory、LCU | §7~§9 一手来源 | ★★★ |
| Fleet / channels | 功能文档 | .../fleet/channels | Gmail/Slack/Teams 触发、thread 行为 | 渠道接入形态 | ★★ |
| Fleet / schedules | 功能文档 | .../fleet/schedules | UTC 定时、memory synthesis 用例 | 用编排补模型短板 | ★★ |
| Fleet / agent-identity | 功能文档 | .../fleet/agent-identity | Claws / Assistants，不可逆 | 凭证归属与审计 | ★★★ |
| Fleet / manage-agent-settings | 功能文档 | .../fleet/manage-agent-settings | 可见性三规则、记忆批准、导出 ZIP | 共享策略模板 | ★★★ |
| Fleet / skills | 功能文档 | .../fleet/skills | 渐进式披露、私有/共享、CLI pull | §6 一手来源 | ★★★ |
| Fleet / mcp-framework | 功能文档 | .../fleet/mcp-framework | Tool Server、MCP 网关、前缀化 | 工具聚合 | ★★ |
| Agent Auth | 功能文档（beta） | .../langsmith/agent-auth | OAuth provider、interrupt 认证、token 刷新 | 凭证中断建模 | ★★★ |
| Deep Agents / overview | **架构文档** | .../oss/python/deepagents/overview | 四层栈、中间件、VFS、权限、子代理 | §3/§5 一手来源 | ★★★ |
| How we built Agent Builder's memory system | **工程博客** | langchain.com/blog | COALA 映射、Postgres VFS、四条教训 | §4 一手来源 | ★★★ |
| open-agent-platform | 开源仓库（已废弃） | github.com/langchain-ai | 被 Fleet 取代的形态 | §11 反例 | ★ |

### 表 2 · 相关项目、规范与工具

| 名称 | 定位 | 语言·技术栈 | 成熟度 | 相关块 | 借鉴点 | 风险与边界 |
|---|---|---|---|---|---|---|
| **LangSmith Fleet** | 托管 no-code 智能体平台 | 商业 SaaS（自托管 beta） | 在演进 | 调研主体 | §12 十条 ★★★ 亮点 | 默认 SaaS；自托管 beta；LCU 计费 |
| **Deep Agents** | Agent harness（Fleet 底层） | Python（`deepagents`）/ LangGraph runtime | 活跃，v0.7+ | 运行时架构 | 四层栈、VFS、权限、子代理、prompt caching | **Python 栈**；`delete`/tools allowlist 需 ≥0.7 |
| **LangGraph** | runtime | Python/TS | 成熟 | durable execution / HIL / store | 把 HIL 做成 interrupt 原语 | 强绑定可获得持久化，代价是生态封闭 |
| **LangChain** | Agent 构建块框架 | Python/TS | 成熟 | 基础层 | 与 runtime/harness 的分层定义 | —— |
| **LangSmith Tool Server** | MCP 框架 / 网关 | Python（`langsmith-tool-server`，PyPI） | 可用 | 工具层 | MCP 聚合、工具名前缀化、`@tool` + OAuth scope | Python 包；Fleet 用户无需直接接触 |
| **langchain-auth** | Agent OAuth 认证 | Python / JS（`@langchain/auth`） | **beta** | 凭证层 | 中断式 OAuth、token 按 assistant/user scoped | beta，活跃变更中 |
| **Agent Skills specification** | 技能格式开放规范 | 规范（agentskills.io） | 跨产品采用 | 技能层 | `SKILL.md` + frontmatter + 渐进式披露 | 依赖实现方支持程度 |
| **AGENTS.md** | 指令文件开放标准 | 规范（agents.md） | 跨产品采用 | 记忆层 | Procedural 记忆的行业格式 | —— |
| **COALA 记忆分类** | 学术论文（记忆三分） | 学术 | 引用广泛 | 记忆设计 | Procedural / Semantic / Episodic 分类框架 | Fleet 仅实现前两类 |
| **Open Agent Platform** | 开源 no-code 平台 | TS / Next.js，MIT | **2026-02 deprecated** | 形态反例 | 配置驱动 UI（`x_oap_ui_config`）仍可借鉴 | 已停止演进；依赖一整套外部服务 |
| **LangConnect** | RAG 服务（OAP 配套） | Python / FastAPI + pgvector | 可用 | 检索面 | 独立生命周期的检索服务 | 随 OAP 一起被弱化 |
| **Claude Code / Cursor / Codex** | 本地编码 Agent | 商业 | 成熟 | 技能可移植目标 | `langsmith fleet skills pull` 可下发技能到这些工具 | 依赖本地目录约定与 symlink |

---

*本档为纯资料类调研，落 `02_智能体/99_开源项目分析/`（资料专区）。所有官方事实取自 §14 一手来源；未给出项已显式标注。*
