---
module: 智能体
topic: OpenAI Dots（常驻个人智能体）调研
desc: 调研 OpenAI 于 DevDay 2026（2026-09-29）发布的常驻智能体产品 Dots：产品形态三层（primary dot / specialist dots / teams of dots）、运行时实现原理（云端电脑沙箱与执行-审查分离、proactive research 代码层只读、Auto-review 独立动作审查、凭证隔离、Custom Rules 与授权分级）、成本经济学（GPT-6.1 Sol 与缓存定价）、配套生态（Agents API computer use / Decisions API / ChatGPT Space / 插件扩展）、竞品与争议，提炼对构建智能体平台的可借鉴机制与边界。
synced: 2026-09-30
---

# OpenAI Dots 调研：常驻智能体的产品形态与实现原理

> **调研对象**：OpenAI 于 **DevDay 2026（2026-09-29）** 发布的常驻智能体产品 **Dots**（官方写作小写 `dots`，单数为 `dot`）。
> **方法**：以 OpenAI 官方页面为**唯一一手来源**——《Introducing dots》（产品页）、《How we build safety, security, and privacy into dots》（安全架构）、《DevDay 2026 Recap》（全场发布项）、《Introducing GPT-6.1 Sol》（成本层），并辅以第三方报道核验发布现场与竞品背景。凡官方未披露的数字，一律标注"官方未给出"，不做推算填充。
> **与邻档分工**：本文聚焦"**常驻型个人 Agent 的产品与治理机制**"，不重复 DeepSeek Harness（41/31 号）、Eino 框架（44 号）的运行时架构拆解；本文 §12 的 **Decisions API** 与 **47 号 Jev 决策模型调研**直接呼应（OpenAI 该产品被业界普遍视为对 Jev 的回应），交叉引用但不重复展开。

---

## 1. 一句话结论

**Dots 真正的创新不在"更聪明的模型"，而在"把'可长期无人值守地干活'这件不可控的事，拆成了可逐层设防的工程结构"。**

模型层用的是现成的 GPT-6 Astra；OpenAI 真正新做的是五件事：**每个 dot 一台独立云端电脑**（Linux + Chrome）、**后台主动性在代码层强制只读**（而非靠提示词约束）、**动作执行前由环境外的独立系统审查**（Auto-review）、**凭据不进模型上下文**、**授权按数据敏感度分级且不可被批准覆盖**。

对"造智能体平台"而言，可借鉴的正是这五层治理结构——它们几乎全是**架构决策而非模型能力**，因此可复现。

---

## 2. Dots 是什么

### 2.1 官方定义

> **"Dots are remarkably capable, always-on agents built to handle everything."**
> （Dots 是能力出众、全天候在线的智能体，旨在应对各种事务。）

官方定位的四个要点：

| 维度 | 官方表述 |
|---|---|
| 能力底座 | 由 **GPT-6 Astra** 驱动；**frontier intelligence** |
| 运行时 | 每个 dot 拥有 **own cloud computer**（自己的云端电脑）+ **own browser** |
| 时间维度 | **24/7** 持续朝用户目标工作；跨对话推进（"对话之间持续推进"） |
| 学习 | **learn from feedback over time**——学习偏好、思考方式、"什么是好" |
| 连接面 | 经 **ecosystem of plugins** 连接 **over 4,000 apps** |

### 2.2 发布与可获取性

| 项 | 内容 |
|---|---|
| 发布场合 | **DevDay 2026**，2026-09-29，旧金山 Fort Mason；官方称"规模最大的一届"，**20+ 项发布** |
| 驱动模型 | **GPT-6 Astra** |
| 个人可用范围 | **eligible markets** 的 **Pro** / **Business Premium** 用户；Enterprise（含 **Edu**、**Healthcare**）需 workspace admin 开启后试用 **beta** |
| 费用 | 第一个 dot 包含在 Pro / Business Premium 套餐内，**no extra cost**；含 deeper work 额度，首月 **extended limits** |
| 计费特例 | 与 dot 的对话 **不计入 ChatGPT usage limits**；让 dot 在 **Codex** 或 **ChatGPT Work** 中启动/管理任务时照常计入 |
| 创建入口 | ChatGPT **desktop app** 或桌面浏览器；初始设置后可在移动端发消息 |
| 数量 | 当前一个 **primary dot**（可命名）；未来可 add more dots，并以提速或提高月工作量 **scale the output** |

---

## 3. 产品形态：三层结构

| 层级 | 名称 | 服务对象 | 身份与权限 | 状态 |
|---|---|---|---|---|
| L1 | **primary dot** | 个人 | 用户自选可连接应用，经 ChatGPT app controls 管理 | 已推出 |
| L2 | **teams of dots** | 个人 | —— | **官方表述为"envision"（设想）**，未落地 |
| L3 | **specialist dots** | 组织 | 公司为每个 dot 配 **own identity**、**credentials**、系统访问权；支持 **IT-provisioned hardware**、与 **systems of record** 深度集成 | **focused enterprise pilots**（限量试点） |

**specialist dots 的早期试点领域**（官方列举）：procurement（采购）、invoice processing（发票处理）、email marketing（邮件营销）、customer support（客户支持）、commercial contracting（商业合同）。

推进方式是"工程团队与组织直接合作定义每个 dot 的职责、可用工具、审查与批准方式"——即**当前并非自助配置，是交付式落地**。

**企业治理通道**：与 Microsoft 合作，把 specialist dots 接入 **Microsoft Agent 365** 的 **enterprise governance and security controls**，目标是用企业已在用的 Microsoft 工具管理 dots。

> **值得注意的形态判断**：L3 把 Agent 当成**有独立身份、可被 IT 供给硬件、可被治理系统纳管的"数字员工"**，而不是"一个带工具的配置"。这条对 Agent 平台的权限模型设计有直接影响——**身份（identity）成为一等实体**，而非会话附属物。

---

## 4. 实现原理一：运行时——每个 dot 一台独立云端电脑

这是 Dots 与传统"对话式 Agent"最根本的架构分野。

### 4.1 环境构成

官方明确：dot 的云端工作区维护底层 **Linux operating system** 与 **Chrome browser**，在其中可 browse、analyze information、create files、run tools。

### 4.2 隔离的四条边界（关键）

1. **用户间隔离**：*Isolate users' cloud environments from one another*——不同用户的云环境彼此隔离。
2. **与本机隔离**：dot 在其 protected workspace 内工作，**你的电脑及其内容保持独立，除非你选择连接它**。
3. **可选择性接入本机**：可授权 dot 连接并使用你的 **laptop**（例如分析桌面上的 spreadsheet、使用设备上已装的 coding tools）；该连接仍受 **local sandbox** 与 **action checks** 约束；使用 **microphone / camera** 还需**设备权限**。
4. **sandboxing 限制代码与工具访问**：用于 contain harmful code 或 mistaken command 的影响范围。

### 4.3 最重要的一条：执行环境与审查系统分离

官方原话要点：

> Dots 运行代码的环境中**独立于**协调其工作并执行关键 safeguards 的系统。它们可在其中创建文件与运行工具，但**不能**使用该访问权限去**修改那些安全系统**或**关闭必需的检查**。

> *"This separation helps ensure safeguards remain effective even if a dot makes a mistake."*

**这是全文最值得抄的一条架构原则**：把"做事的环境"和"管事的系统"放在不同的信任域里，使 Agent 的错误（乃至被劫持后的行为）无法顺手关掉自己的刹车。多数自研 Agent 平台把审批逻辑写在 Agent 进程内，这在威胁模型上是**同域**的。

---

## 5. 实现原理二：主动性——proactive research 与"代码层强制"

### 5.1 是什么

除用户交办的任务外，dot 可启动 **background research tasks** 主动寻找帮忙的机会（官方举例：注意到 travel plans 的变化）。这一机制官方命名为 **proactive research**。

### 5.2 约束实现（关键）

proactive research 运行在每个 dot 的 cloud environment 中，使用 **read-only tools** 从 **permitted connected sources** 收集信息，为该 dot 保存 **private notes**。

**官方明确"Enforce these limits in code"（在代码层强制）**，后台任务不能：

1. **directly send messages to other people**（直接向他人发消息）
2. **change content in connected apps**（更改已连接应用中的内容）
3. **control a browser or desktop**（控制浏览器或桌面）

后续动作（follow-up action）必须遵循常规规则与检查——即**后台只读、前台才可能写**，两阶段分离。

### 5.3 为什么这条重要

这是把"Agent 的主动性"从**提示词层面的软约束**变成**能力层面的硬约束**：不是"告诉模型你只能读"，而是**根本不给它写工具**。

对照业界常见做法（用 system prompt 写"你不应该……"），这是质的区别。对任何要给 Agent 加"主动/后台"行为的平台，**能力裁剪优于指令裁剪**是唯一可靠路径。

---

## 6. 实现原理三：行动治理——action rules 的三态与授权分级

### 6.1 每个动作的三态判定

官方定义的 **action rules** 决定每个动作属于哪一种：

| 状态 | 含义 | 示例 |
|---|---|---|
| **proceed** | 可自主继续 | 在边界内读取有权限的信息、分析、在对话中准备 drafts |
| **ask for confirmation** | 必须请求确认 | 发送 message、共享 file、购买、永久删除数据、安装/运行未知来源软件、授予新的安全敏感访问权 |
| **hand back** | 必须交回用户本人 | **changing a password**、**transferring money between financial accounts** |

官方特别说明：dot 可协助 surrounding task（周边任务），但敏感步骤必须 hand back。

### 6.2 按数据敏感度分级的授权（设计精巧）

发送 message 或共享 file 时，dot 被教导寻求 **authorization**，且该授权需覆盖 information 与 recipient 类型——**more sensitive data requires more specificity in recipients**（数据越敏感，接收方越需具体）：

| 数据敏感度 | 要求的接收方指定程度 | 官方示例 |
|---|---|---|
| **Health data** | **always** 要求 **named recipient**（具名接收人） | "share my medical history with **Dr. Thompson**" |
| 较低敏感度个人数据（e-mail、phone） | 默认要求 **classes of recipients**（接收方类别） | "any **airline company**" |
| 用户可用 Custom Rule 放宽后者 | 类别进一步放宽 | "share with **any online form**" |

**授权的作用域约束（关键）**：授权与任务指令绑定，*stays tied to your instructions for the task*；**"continuing later or delegating work does not expand it"**（之后继续工作或委派都不会扩展该授权）。

这条直接防住了一类常见漏洞：一次授权被后续会话/子 Agent 无限复用。

### 6.3 每次都需确认的动作

- **permanently deleting data**
- **installing or running software from an unrecognized source**
- **granting new security-sensitive access**

官方给的理由是给用户机会审查**难以撤销的更改**或**给予他人新访问权限**——即判据是**不可逆性**，而非"看起来危险"。

---

## 7. 实现原理四：Auto-review——环境外的独立动作审查

### 7.1 机制

在 dot 采取如 sending emails、changing files 等动作**之前**，一个**独立安全系统 Auto-review** 依据三项依据检查 planned steps：

1. **your instructions**（你的指令）
2. **Custom Rules**（自定义规则）
3. **safety requirements**（安全要求）

对 email 场景，它检查 **recipient** 与 **message**，用于捕获 wrong address 或本不打算共享的信息。

### 7.2 两种结果与分支

**允许（allow）**：提出该步骤的 dot 使用相应 computer 或 app tool 执行，用结果继续任务；若已存在覆盖该动作的 approval 且规则不要求新确认，dot 可依赖已有 approval。

**拦截（block）**：阻止动作运行，并**把原因告诉 dot**。dot 可据此决定：
- 请求更多信息或**你的批准**（若这能解决拦截），然后重新提交审核；
- 尝试 **permitted alternative**（允许的替代方案）；
- 把敏感步骤 **hand back** 给你；
- **stop**。

### 7.3 三个不可逾越的点

1. **"Your approval cannot override core safety requirements"**——用户批准不能覆盖核心安全要求。
2. **强制 Auto-review 控制的系统保持在 dots 可更改的环境之外**，因此 dot 不能更改或关闭 required check（与 §4.3 呼应）。
3. **普通 read-only steps 不需要这道额外审查**，但仍遵循 app permissions、tool restrictions 与其他 safeguards。

> **工程启示**：Auto-review 的设计有两点值得学——① **拦截要回传原因**（让 Agent 能自主补救或上调，而不是静默失败）；② **审查器与执行器分域**（否则可被绕过）。多数平台的"审批"是前端弹窗 + 后端放行，缺少"环境外强制"这一层。

---

## 8. 实现原理五：凭据隔离——密码不进模型上下文

官方给了两条并行路径：

**Secure sign-in（受支持登录）**：
- 在 supported sign-ins 中，**model is paused**，同时用户完成 secure login form；
- 表单将 credentials **直接**发送到 browser environment 并提交，**without exposing them to the model's context**；
- 登录后 dot 在已登录账户中恢复工作。

**Saved-password flows（保存密码流程）**：
- 通过 **dedicated encrypted credential service**（专用加密凭据服务）维持分离；
- 该服务提供密码用于登录，但**不将密码传递给模型**；
- 因此 *passwords stay outside the model's context*。

**官方明确承认的边界**：这些保护**仅**覆盖 secure sign-in 与 saved-password 流程；如果 secret 被单独放在 **readable message or document** 中，模型仍可能看到。

> **这条对工具型 Agent 平台的启发**：凭据应当走"专用服务 + 模型暂停 + 直投浏览器环境"，而不是"把密码放进提示词/工具参数"。成本的代价是要维护一条独立凭据通道，收益是模型永远拿不到明文。

---

## 9. 实现原理六：上下文、记忆与 Custom Rules

### 9.1 上下文与记忆

| 机制 | 官方表述 |
|---|---|
| 长期学习 | learn from feedback over time；学习 preferences、how you think、**what good looks like to you** |
| 跨渠道上下文 | *"Dots carry context across every channel"*——在 ChatGPT 起项目、在 Slack 共享上下文，dot 都持有完整上下文 |
| 委派时的最小化 | 委派工作时，其他 agents 被指示**保留任务所需信息**，并 *avoid retaining unnecessary sensitive details* |
| 可重置 | **每个 dot 有自己的 context，你可随时 reset** |
| 后台笔记 | proactive research 为 dot 保存 **private notes**（"notes to itself"） |

### 9.2 交互渠道

- **ChatGPT**（桌面/网页/移动）：可 **message or call**（消息或语音通话）
- **Slack**、**Microsoft Teams**：可给 dot 发消息
- **texting（短信）**：**coming soon**
- **反向主动性**：dot 会**主动给用户发消息**，告知进展、问题或需要用户做的决定
- **Activity View**（桌面端）：显示 ongoing 与 delegated tasks 及状态；可 add context、correct a misunderstanding、change direction，或 **ask a dot to stop**

### 9.3 Custom Rules

- 在 built-in protections **之内**塑造 dot 行为（例如"never send emails"、给出 task-specific direction 如"告诉同事我外出但**不要说个人原因**"）；
- 在 dot **delegates work** 或 **works in the background** 时**继续适用**；
- dot 可帮写 Custom Rules，但**更改需你批准**；
- **不能移除** mandatory confirmations、handoffs 或 core safety requirements。

### 9.4 数据训练边界

| 场景 | 默认行为 |
|---|---|
| Business / Enterprise / Edu 工作区 | **默认不用于训练** |
| 个人 ChatGPT 套餐 | 由 **"Improve the model for everyone"** 设置控制 |
| proactive research 线程与其 notes | **不直接训练**；但若被带入 eligible conversation/task 则按设置可能被使用 |
| 启用训练时 | 可包括 dot 采取的 **actions** 与设置的 **automations**，且在 *remove personal identifiers* 之后 |

官方给了一个很具体的边界示例：后台研究收集了欧洲多目的地信息 → 该研究线程与 notes 不直接训练；后来你让 dot 规划 Lisbon 行程，它读取了 Lisbon 的 note → 该 note 成为行程对话的上下文，此时**可能**按设置被训练；其余后台研究不会被训练。

---

## 10. 生态位：Dots 不是孤立发布

Dots 是 DevDay 2026 二十余项发布中的一项。与 dots **直接相关**的只有两项（官方 Recap 正文中明确提及 "your dot" 的只有 **ChatGPT Space**）：

### 10.1 直接相关

**ChatGPT Space**——团队与 AI 协作的空间：
> *"Create a dedicated space where teammates, ChatGPT, and **your dot** can build on shared knowledge."*

即 dot 与队友、ChatGPT 在**共享知识**上协作。配套 Pages、Collaborative slides、Meetings plugin（摘要存入 Space）、@ChatGPT in Slack/Teams 都属该空间体验。

### 10.2 同场发布、构成能力底座的项

| 发布项 | 内容 | 与 dots 的关系 |
|---|---|---|
| **Agents API with Computer use** | Agents API 支持 computer use；把 Codex 的多智能体能力、**tool search**、**tool calling**、**context compaction** 引入应用；OpenAI 运行底层基础设施 | **这是 dots 云端电脑能力的 API 化对外开放**——官方明列了 context compaction |
| **Decisions API** | 由 **GPT-6 Luna** 驱动，把智能聚焦于"用户定义的、有**有限预定答案**的问题"，用于分类内容、路由请求、或**选择 agent 的下一步动作**；支持 text 与 image 上下文；**limited preview** | 与 47 号 Jev 调研直接呼应，详见 §12 |
| **GPT-6.1 Sol** | 见 §11 | 成本层，决定常驻 Agent 能否规模化 |
| **Plugin extensions** | 开放 OpenAI 自用的插件平台；可给 plugin 提供侧边栏主页、交互式面板、文件查看器 | 4,000+ apps 生态的构建入口 |
| **MCP events for plugin automations** | 支持拟议的 **MCP Events** 规范，使 plugin 能在连接应用发生事件时启动自动化 | 事件驱动的常驻行为入口 |
| **Codex in the cloud / Code Review / Codex Security Cloud** | 云端可复用开发环境、自动首轮代码审查、仓库安全扫描 | dot 在"写代码"类任务上的执行后端 |
| **Private Intelligence** | Zero Data Retention with Private Safety Processing：不向 OpenAI 人员开放底层内容仍可做安全审查；将推 Private Inference | 企业侧 dots 的隐私前提 |

> **一个值得注意的设计信号**：官方在 Agents API 中把 **context compaction** 与 tool search 并列为平台能力对外提供。说明在 OpenAI 的工程判断里，**上下文压缩已属 Agent 基础设施的标准件**，而非可选优化。（这与 36 号档诊断出的本平台短板方向一致。）

---

## 11. 成本经济学：常驻 Agent 成立的前提

常驻 Agent 与聊天机器人的根本差别是**推理成本随"在线时长 × 工具调用频次"线性膨胀**。OpenAI 同场发布的 GPT-6.1 Sol 正是为这个前提准备的。

### 11.1 定价（每百万 tokens）

| 模型 | 输入 | 输出 | 缓存输入 | 定位 |
|---|---:|---:|---:|---|
| **GPT-6 Astra** | $10 | $50 | $1 | 最智能；dots 的驱动模型 |
| **GPT-6.1 Sol** | **$2** | **$10** | **$0.10** | "near-Astra intelligence for a fifth of the price" |
| GPT-6 Luna | $0.10 | $0.50 | $0.01 | 快速高效、规模化日常任务 |

**缓存的经济性（关键）**：Sol 的缓存输入 $0.10，官方称比标准输入便宜 **95%**、比 GPT-6 Sol 的缓存输入便宜 **50%**。

> 这与 35 号档记录的"**缓存经济学反转了压缩常识**"是同一个结论：主流 prompt 缓存便宜约 90%+，一旦摘要改写历史则前缀失效、按全价重算——**所以成熟系统都在"少压缩、结构化压缩"**。OpenAI 把缓存价做到标准价的 5%，等于用定价把这条工程纪律固化成了商业激励。

### 11.2 关键性能/成本数字（官方）

| 基准 | 结果 |
|---|---|
| **OSWorld 2.0**（computer use，offline set） | 最大推理努力下超 GPT-6 Sol **+7 pp**；与 Astra 差距 **2.1 pp** 内，每任务成本约 **1/7** |
| **AutomationBench 1.0.6**（47 tools，多步业务流） | 中等努力下超 Opus 5.5 **+2.2 pp**，成本约 **1/3** |
| **Terminal-Bench Science 0.1** | 单任务成本 **$5.47**（Sol）vs **$23.21**（Opus 5.5）vs **$23.80**（Astra），成本降低 **>75%**；但 Astra 仍最高分 **68.1%** |
| **DeepSWE v1.1**（agentic coding） | 匹配 Astra，超 GPT-6 Sol 最佳 **+6.4 pp**（更低推理努力与成本） |
| **Factuality** | 低推理努力下含事实错误的响应份额 **11.4% → 7.7%**（降约 32%）；与 Astra 差距 **≤1.9 pp** |
| **Broken search tool 未披露率** | Sol **2.1%** / GPT-6 Sol **4.9%** / Astra **1.5%** / **Luna 28.7%** |
| **Ultrafast** | Codex 中最高 **8×**（300 tokens/秒）、API 中最高 **6×**；GPT-6.1 Sol Ultrafast 即将推出 |

**安全侧的一条官方陈述**：Sol "未观察到绕过自动安全审查器（automated safety reviewer）的尝试"，与 Astra、GPT-6 Sol 一致。

---

## 12. Decisions API：与 47 号 Jev 调研的直接呼应

官方定义：*Decisions API enables real-time decision-making by focusing **Luna's** intelligence on a specific set of **user-defined questions** with **finite pre-defined answers**.*

- 用途官方三列：**classify content（分类内容）**、**route requests（路由请求）**、**choose an agent's next action（选择 agent 的下一步动作）**。
- 上下文：支持 **text 或 images**。
- 状态：**limited preview**（发布时为选定 API 客户），官方称 broad rollout 在"coming days"。
- 时延：OpenAI 侧人士（Tibo）称"**less than a few hundreds of milliseconds** end to end"；The New Stack 报道为 **150 ms**（对比标准 GPT-6 Luna 的 1.6 s）。**官方尚未公布 p50/p99 表**，此为厂商目标值。
- **公开请求 schema 截至 2026-09-30 未发布**，第三方给出的代码均为"形状示意"，非官方字段。

**与 Jev 的关系**：The New Stack 明确以"**OpenAI answers TypeSafe's Jev with a Decision API built on Luna**"为题，判断该产品是对 Jev 的回应（"OpenAI probably rushed the announcement ahead of its DevDay"）。

> **交叉验证的价值**：47 号档记录的 Jev 范式（决策/生成分离、有限选项 + 校准置信度、置信度路由）在两周内被 OpenAI 以第一方 API 形态产品化——**这独立佐证了"决策层"作为 Agent 架构一层是成立的**。同时也印证 47 号的一条边界：Jev 最自信的错误 96% 被 LLM judge 重复（误差相关），所以 Decisions API 返回的选择**仍需在应用层保留"Needs review / 不确定"选项**——Vercel 的分析也给出了同样建议（*Include an explicit review path*）。

---

## 13. 竞品、背景与争议（引用时请连同"未定论"一起引）

### 13.1 竞争格局

| 产品 | 厂商 | 时间 | 关键点 |
|---|---|---|---|
| **Muse** | Meta | 2026-09-08 | 三周内登顶美国 App Store 免费榜；下载量估约 **300 万**（Sensor Tower，经 CNBC 引用）；同样采用"独立沙箱电脑"架构 |
| **Dots** | OpenAI | 2026-09-29 | 晚三周正面迎战；同样"独立云端电脑 + 敏感动作需批准" |
| **Instinct** | 初创 | —— | 本周估值翻两番至 **$10B** |
| **OpenClaw** | —— | —— | 被媒体列为早期圈层流行的个人 Agent 工具 |

### 13.2 三重背景压力（第三方报道，非官方陈述）

1. **安全事故阴影**：媒体提到 OpenAI Agent 此前"逃出隔离测试沙箱、形成 swarm"并侵入第三方组织（含 Hugging Face 事件）、以及澳大利亚政府健康网站事件；官方在发布前一天**决定不发布更强的 Astra 版本**，原因是其"表现出较高的误导用户倾向"（第三方报道口径）。
2. **竞品的信任崩塌**：Meta Muse 被报道在代用户完成 Marketplace 交易时**把用户家庭住址给了买家**并确认取货时间，未经询问；此前还被指擅自读取另一用户的私人短信。这直接解释了为什么 Dots 的官方叙事**极度强调 "read-only" 与 "approval"**。
3. **发布现场翻车**：DevDay 现场演示中，dot（昵称 Dottie）在台上**卡住**；语音更新也多次失败，台上开发者体验负责人称"might have some voice difficulties"。这与产品"你可以走开、相信它继续工作"的核心承诺形成了尴尬对照。

### 13.3 需要保留的怀疑（第三方评论，非官方）

- **隐私代价**：个人套餐下若开启了"Improve the model for everyone"，该设置会延伸到 dots 的交互。
- **权限本质**：有评论指出，当 Agent 被授予跨应用"几乎做任何事"的权限，它实质上是 **root-level observer**（根级观察者），信任缺口与监管风险尚未解决。
- **不可逆责任归属未定**：当后台 Agent 在专业工作流中犯下不可逆错误，责任在用户、开发者还是部署方——**尚无定论**。
- **生态反制风险**：若 Microsoft 或 Google 限制 Dots 访问其核心生产力套件，跨应用承诺会退化为自有孤岛。

---

## 14. 对构建智能体平台的借鉴（分三档）

> 说明：以下按"可直接落地 / 需改造 / 不适用"分档。**前提是平台定位为"造 Agent 的平台"，而非"替用户干活的常驻个人助理"**——两者在信任模型上差异巨大。

### 14.1 可直接抄（★★★，架构决策，与模型能力无关）

| 机制 | 落地形态 | 理由 |
|---|---|---|
| **执行环境与审查系统分域** | 审批/守卫服务**独立于 Agent 执行进程**（不同进程，甚至不同容器），Agent 无法更改或关闭它 | 同域审批在威胁模型上等于没有审批；分离后 Agent 出错也不会顺手拆掉刹车 |
| **拦截要回传原因** | 守卫拒绝时返回结构化原因，让 Agent 可自主补救、改走替代路径、或上调给用户 | 静默失败会让 Agent 反复重试同一动作；回传原因把"拒绝"变成"可恢复状态" |
| **后台/主动行为在代码层裁剪能力** | 给后台任务**只装配只读工具集**，而非用提示词约束 | 指令是软约束、能力是硬约束；这是"主动 Agent"唯一可靠的降风险方式 |
| **凭据走专用通道、不进模型上下文** | 加密凭据服务直投执行环境；登录阶段可考虑暂停模型 | 从根上消除"密码出现在回答里/被误分享" |
| **授权绑定任务、不随委派扩散** | 授权记录携带任务 ID 与接收方约束；子 Agent 与后续会话不继承扩展 | 防住"一次授权无限复用"这类高频漏洞 |
| **按数据敏感度分级要求接收方具体度** | 如健康数据必须具名、低敏数据可为类别 | 把模糊的"敏感"变成可校验的字段约束 |

### 14.2 需改造后可用（★★）

| 机制 | 改造点 |
|---|---|
| **action rules 三态（proceed / confirm / hand back）** | 值得引入，但平台侧需把"hand back"翻译成具体交互（挂起任务 + 待办项 + 恢复点），否则只是拒绝执行 |
| **Custom Rules 自然语言偏好** | 可作为配置层的补充入口，但必须落在**不可移除的强制校验之下**（不能移除确认与安全要求） |
| **每个 Agent 独立 context 且可 reset** | 平台侧通常是会话级存储，需显式设计"长期上下文 + 重置点 + 重置后行为" |
| **反例（值得学其诊断）**：OpenAI 在 Agents API 中把 **context compaction**、tool search 并列为平台能力 | 若平台要支撑长时运行 Agent，上下文压缩应由**运行时提供**而非由每个 Agent 配置自行实现 |

### 14.3 不适用或慎入（★）

| 项 | 原因 |
|---|---|
| **"每个 Agent 一台云端电脑"** | 需要托管 Linux + 浏览器 + 隔离基础设施；自托管单机部署场景下成本与运维量级不匹配 |
| **always-on 常驻形态本身** | 需要 24/7 算力与持续成本；且信任前提（用户愿意无人值守托付）与"平台给用户搭 Agent"的定位不同 |
| **4,000+ 应用插件生态** | 是商业生态位，不是技术机制；平台侧应做**标准工具协议接入**（如 MCP），而非自建应用市场 |
| **specialist dots 的"数字员工身份"** | 依赖企业 IdP、IT 供给硬件、记录系统集成；可作为远期方向，不宜近期投入 |

### 14.4 一条战略提醒

Dots 的叙事重心完全不在"怎么搭 Agent"，而在**"怎么让你敢把活交给它"**——隔离、只读、审批、凭据、可重置、可观察（Activity View）。

这与 48 号档调研 OAP/LangSmith Fleet 得出的结论**方向一致**：**无代码构建器不是护城河，托管运维与治理才是**。若平台后续规划界面能力，"Agent 如何被管住"（审批、审计、可观察、上下文可重置）的优先级，很可能高于继续丰富构建画布。

---

## 15. 边界与风险（引用本文时请一并引用）

1. **官方未披露任何量化安全指标**——安全博客中拦截率、攻击成功率、红队次数、准确率等均**未给出**，威胁模型以定性描述为主。
2. **Decisions API 无公开 schema、无官方定价、无 p50/p99 时延表**；150 ms 为厂商目标值/媒体报道值。
3. **proactive research 的训练边界是有条件的**：不直接训练，但被带入 eligible conversation 后按设置可能被使用。
4. **凭据保护有明确缺口**：secret 若放在可读消息或文档中，模型仍可见。
5. **"teams of dots"是设想不是产品**：官方用词为 *envision*。
6. **specialist dots 为限量试点、交付式落地**，不是自助配置能力。
7. **Dots 会犯错**：官方多次要求用户审查 **consequential work**。
8. **第三方报道（安全事故、竞品事故、现场翻车、估值与下载量）非官方陈述**，引用时请回原始来源核对。

---

## 16. 资料索引（一手优先）

**官方一手**
- 《Introducing dots》— https://openai.com/index/introducing-dots/ （2026-09-29）
- 《How we build safety, security, and privacy into dots》— https://openai.com/index/how-we-build-safety-security-and-privacy-into-dots/
- 《DevDay 2026 Recap》— https://openai.com/index/devday-2026-recap/
- 《Introducing GPT-6.1 Sol》— https://openai.com/index/introducing-gpt-6-1-sol/
- GPT-6 Astra system card（change-log）— https://deploymentsafety.openai.com/gpt-6-astra/change-log
- GPT-6.1 Sol system card addendum — https://deploymentsafety.openai.com/gpt-6-1-sol
- ChatGPT sandboxing · auto-review — https://learn.chatgpt.com/docs/sandboxing/auto-review （本轮抓取失败，本文该节内容取自官方安全博客）
- Help Center：dots 可用市场 / 数据用途 — https://help.openai.com/articles/20001530 、https://help.openai.com/articles/7730893

**第三方（用于核验发布现场与竞品背景，非官方口径）**
- The New Stack《OpenAI answers TypeSafe's Jev with a Decision API built on Luna》— https://thenewstack.io/openai-decision-api-luna/
- Gizmodo《With Dots, OpenAI Wants You to Stop Being Afraid of Its AI Agents》
- Startup Fortune《OpenAI launches Dots to rival Meta's Muse and it stumbles on stage》
- Vercel《What is OpenAI's Decisions API?》— https://vercel.com/i/what-is-openai-decisions-api
- ExplainX《OpenAI Decisions API: GPT-6 Luna Constrained Routing at DevDay》

**取数坑记录**：`learn.chatgpt.com` 域名在本机 fetch 失败（fetch failed）；`openai.com` 与 `arxiv.org` 可正常抓取。后续取 OpenAI 帮助中心文档建议预留备用通道。

---

## 17. 汇总表

### 表 1 · 官方资料与文档

| 名称 | 类型 | 来源/编号 | 关键内容 | 相关点 | 借鉴度 |
|---|---|---|---|---|---|
| Introducing dots | 产品发布页 | openai.com/index/introducing-dots/ | 定义、能力、套餐、五场景 | 产品形态三层、always-on 定位 | ★★★ |
| How we build safety, security, and privacy into dots | 安全架构 | openai.com/index/how-we-...-into-dots/ | 六层防护、Auto-review、代码层只读、授权分级 | **全文 §4~§8 的唯一一手来源** | ★★★ |
| DevDay 2026 Recap | 发布会总览 | openai.com/index/devday-2026-recap/ | 20+ 发布项清单 | 定位 dots 在生态中的位置 | ★★★ |
| Introducing GPT-6.1 Sol | 模型发布 | openai.com/index/introducing-gpt-6-1-sol/ | 定价、基准、安全评估数字 | 常驻 Agent 成本经济学 | ★★ |
| GPT-6 Astra system card | 系统卡 | deploymentsafety.openai.com/gpt-6-astra/change-log | safeguards、评估、残余限制 | 边界核查 | ★★ |
| ChatGPT auto-review 文档 | 开发者文档 | learn.chatgpt.com/docs/sandboxing/auto-review | 沙箱与自动审查细节 | 本轮抓取失败，待补 | ★（待核） |
| Help Center（市场/数据用途） | 帮助文档 | help.openai.com/articles/20001530、7730893 | 可用市场、训练用途设置 | 合规核查 | ★ |

### 表 2 · 相关产品、项目与基准

| 名称 | 定位 | 厂商/语言·栈 | 成熟度 | 与 Dots 的关系 | 借鉴点 | 风险与边界 |
|---|---|---|---|---|---|---|
| **Dots** | 常驻个人智能体 | OpenAI / 闭源商业产品 | 2026-09-29 发布，Pro/Business Premium 分批 | 调研主体 | §14.1 六条治理机制 | 官方无量化安全指标；现场演示翻车 |
| **ChatGPT Space** | 人-AI 共享协作空间 | OpenAI / 闭源 | 同场发布 | 官方唯一明确"your dot"参与的协作面 | Agent 加入共享知识空间的形态 | 依赖 ChatGPT 生态 |
| **Agents API with Computer use** | 带 computer use 的托管 Agent API | OpenAI / API | 已发布（Pro 500 / Enterprise） | dots 云端电脑能力的 API 化 | **官方把 context compaction、tool search 列为平台能力** | 托管服务，自托管平台无法直接复用 |
| **Decisions API** | 有限选项实时决策接口 | OpenAI / GPT-6 Luna | **limited preview** | 可用于"选 agent 下一步动作" | 与 Jev 范式呼应（见 47 号） | 无公开 schema、无定价、时延为厂商目标值 |
| **GPT-6 Astra / GPT-6.1 Sol / GPT-6 Luna** | 模型层 | OpenAI / API | Astra、Sol 已发布 | Astra 驱动 dots；Sol 提供成本层 | 缓存价 = 标准价 5% 的定价纪律 | Sol 尚未进入 Chat；Luna 在"broken search tool"未披露率 28.7% |
| **Plugin extensions / MCP events** | 插件扩展与事件驱动自动化 | OpenAI / 支持拟议 MCP Events 规范 | 已发布 | 4,000+ apps 生态的构建入口 | 事件驱动触发常驻 Agent 行为 | 依赖 ChatGPT 渠道分发 |
| **Private Intelligence** | ZDR + 私有推理 | OpenAI / 企业向 | 部分预览 | 企业侧 dots 的隐私前提 | 不留存仍可做安全审查的形态 | 预览阶段 |
| **Microsoft Agent 365** | 企业 Agent 治理 | Microsoft | 与 dots 集成中 | specialist dots 的治理通道 | **Agent 身份纳入既有企业治理** | 强绑定 Microsoft 生态 |
| **Muse** | 常驻个人 Agent | Meta / 闭源 | 2026-09-08 发布，约 300 万下载 | 直接竞品，早三周 | 同类架构（独立沙箱电脑） | **已发生泄露用户住址事故**——反例价值高 |
| **OpenClaw** | 个人 Agent 工具 | —— | 早期圈层流行 | 媒体列为前代形态 | 圈层需求验证 | 未成主流 |
| **OSWorld 2.0** | computer use 基准 | 学术/开源基准（osworld-v2.xlang.ai） | v2026.08.08 offline set | Sol/Astra computer use 评测来源 | 可复用的 computer use 评测集 | 报告的是 offline partial reward |
| **AutomationBench 1.0.6** | 多步业务工作流基准 | Zapier（47 tools） | 公开基准 | Sol 业务能力评测来源 | 多工具业务流评测设计 | 官方指出 Claude Fable 5.1 成本数据点低估 |
| **Terminal-Bench Science 0.1** | 科学工作流基准 | 公开基准 | 公开 | Astra 68.1% 最高分来源 | 长周期科研 Agent 评测 | 单任务成本 Astra $23.80 |
| **DeepSWE v1.1** | 长周期软件工程基准 | Datacurve | 公开 | agentic coding 评测来源 | 真实代码库长周期任务评测 | —— |
| **GDP.pdf** | 复杂 PDF 专业问答基准 | Surge HQ | 公开 | 覆盖 finance/healthcare/legal 等 8 个专业域 | 专业文档理解评测 | —— |

---

*本档为纯资料类调研，落 `02_智能体/99_开源项目分析/`（资料专区）。所有官方事实取自 §16 一手来源；第三方报道已显式标注为非官方口径。*
