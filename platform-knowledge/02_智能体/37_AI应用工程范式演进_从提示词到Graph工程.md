---
module: 智能体
topic: AI 应用工程范式演进——提示词工程 / 上下文工程 / Harness 工程 / Loop 工程 / Graph 工程
desc: 五阶段方法论综述——各阶段定义与核心判词（Harness 控制循环、Context 控制每次迭代输入）及代表性开源项目；前两层从简，Harness/Loop/Graph 三层深入（六个可复制模式、缺步即反模式、过度结构化风险）
docs: ["docs/01_智能体_需求文档_PRD.md", "docs/02_智能体_技术方案设计.md"]
synced: 2026-09-29
---

# AI 应用工程范式演进：从提示词工程到 Graph 工程

> 任务来源：开发者指示「总结分析 AI 应用从提示词工程、上下文工程、Harness 工程、Loop 工程、Graph 工程的发展过程，梳理各阶段主要理论与代表性开源项目；提示词与上下文工程简单些，深入分析后三个」。
> 性质：**方法论综述 + 开源项目全景**（纯资料类）。与本篇互补的既有档位：`36_智能体开发架构深度对比`（本地实现 vs DeepSeek Harness 的十层架构对照）——那一份是**单一项目的纵向剖析**，本档是**五阶段的横向坐标系**。第二份落点文档见 `08_调研预研/38_智能体演进路线建议_HarnessLoopGraph.md`。
> 证据来源标注：Anthropic 官方工程博客（2025-09《Effective context engineering for AI agents》、2025-11《Effective harnesses for long-running agents》、2026-01《Demystifying evals for AI agents》、2026-03《Harness design for long-running application development》、2026-04《Scaling Managed Agents》）与各开源项目仓库/文档为一手；业界转述与二手综述在文中以「据业界转述」标注，请自行核验后再作决策依据。

---

## 0. 一句话结论与阅读地图

**这五层不是替代关系，而是「受控单元」不断外扩的关系**——工程师负责设计的对象，从**一句话**，扩到**一窗上下文**，扩到**一次运行的系统**，扩到**跨会话的自运转闭环**，最后扩到**可持久化、可恢复、可治理的执行结构本身**。

一句判词（Hugging Face 口径，最锋利的区分测试）：

> **Harness 控制循环，Context 控制每次循环迭代的输入。**

一个推论（本文的核心主张）：**越往后的层，越不靠模型能力取胜，越靠"外部状态的质量"取胜。** Anthropic 在长时程 Agent 上的全部工作，本质上都在回答同一个问题——「下一个上下文窗口如何接着上一个上下文窗口干活」。

| 层 | 设计对象 | 关键提问 | 典型产物 | 单独解决不了什么 |
| --- | --- | --- | --- | --- |
| 提示词工程 | 单条指令 | 这句话怎么说 | System Prompt、few-shot、JSON Schema | 私有知识、跨会话记忆、权限、错误恢复 |
| 上下文工程 | 一窗上下文 | 这一轮该给它看什么 | RAG、记忆、压缩、工具描述、JIT 检索 | 动作是否允许、做错了怎么办、如何验证 |
| Harness 工程 | 一次运行的系统 | 它在什么壳里跑 | 工具集、沙箱、权限、hooks、技能、subagent、验证 | 跨会话连贯、无人值守的调度 |
| Loop 工程 | 跨会话的自运转闭环 | 谁来替人给下一条指令 | 触发器、工作树、生成器/评估器分离、进度产物、闸门 | 长时间运行的崩溃恢复、可审计性、并行治理 |
| Graph 工程 | 执行结构本身 | 结构如何被显式声明与持久化 | 状态图、检查点、时间旅行、HITL 中断、fan-out/fan-in | 模型不确定性（结构不能替代判断力） |

**阅读建议**：第 1~3 节速读（约 15 分钟建立坐标系），第 4~6 节精读（Harness / Loop / Graph 三层），第 7 节是给本项目的收敛表。

---

## 1. 演进脉络：为什么会一层层外扩

### 1.1 时间线

| 时期 | 主流范式 | 触发原因 | 代表形态 |
| --- | --- | --- | --- |
| 2022~2023 | **提示词工程** | GPT-3.5 时代，模型无工具、无记忆，唯一的调节旋钮就是输入文本 | ChatGPT、Jasper、Copy.ai |
| 2023~2024 | **上下文工程（萌芽）** | 上下文窗口有限而知识无限 → RAG 出现；多轮对话暴露历史管理问题 | LangChain、LlamaIndex |
| 2024~2025 | **上下文工程（成型）** | Agent 多步执行，模型每步看到的东西决定成败；MCP 让工具描述也变成 token | Cursor、Devin v0、Dify |
| 2025~2026 | **Harness 工程** | 同一个模型，不同 harness 在 Terminal-Bench 上差出十几个百分点 | Claude Code、Codex CLI、OpenHands、OpenCode |
| 2026 | **Loop 工程** | 单次运行已能做对事，但没人下指令时它就停了；需要系统持续驱动 | Anthropic 长时程 harness、Stripe Minions、Archon |
| 2026（并行） | **Graph 工程** | Loop 跑久了要崩溃、要审计、要并行、要治理 → 执行结构必须显式化、持久化 | LangGraph 1.0、Temporal/Inngest、Orkes Conductor |

### 1.2 三条底层驱动力

1. **模型能力的提升，把瓶颈往上推。** 模型越强，分配给它的任务越难，同样的失败模式依旧出现——HumanLayer 团队观察编码 Agent 失败一年多后的结论是「这不是模型问题，是配置问题」。更聪明的模型只是被分配更难的任务。
2. **上下文窗口变大，反而放大了上下文工程的必要性。** Chroma 的 context rot 研究（跨模型族可复现）：性能随上下文长度增长而下降，且当问题与相关上下文语义相似度低时下降更陡。窗口大 ≠ 塞满还能用。
3. **非确定性系统的失败模式是基本属性，只能靠外部约束消化。** 「意外的失败模式是非确定性系统的基本属性」——Harness 的整套方法论，本质上就是把这个命题工程化。

---

## 2. 阶段一：提示词工程（Prompt Engineering）

> 本节简要。它没有被淘汰，而是被**降维收编**为上层的一个组件——每一个压缩提示词、每一条工具描述、每一份 subagent 指令，都仍是提示词。

**定义**：设计单次给模型的指令（特别是 System Prompt），使输出稳定落在期望分布上。

**主要理论与实践**：

| 技术 | 要点 |
| --- | --- |
| 角色 + 任务 + 约束 + 示例 | 四要素；System Prompt 控风格与边界，User Prompt 表达意图 |
| Chain-of-Thought / Zero-shot CoT | 「Let's think step by step」激活推理链 |
| Few-shot | 放**典型**示例而非边角案例；示例是最强的格式约束器 |
| 结构化输出 | JSON Schema / XML 标签约束输出，降低解析脆弱性 |
| 迭代式措辞优化 | 反复测试打磨直到输出稳定 |

**Anthropic 补的关键一条**：System Prompt 要找「正确的抽象高度」（right altitude）——介于「脆弱的过度具体逻辑」与「含糊的欠指定指引」之间。过高则模型自由发挥不一致，过低则编码了 brittle 的 if-else。

**代表性开源项目 / 资料**：

- **LangChain Prompt Templates / Hub** —— 提示词模板化与版本化的最早工程化尝试。
- **OpenAI / Anthropic 官方 Prompt 工程指南与 Prompt Library** —— 事实标准的手册。
- **DSPy**（Stanford）—— 把提示词从手写字串升级为**可编译、可优化的程序**：Signature + Module + Optimizer（BootstrapFewShot / MIPROv2），用度量驱动自动搜索提示词。这是提示词工程向「自动化」方向的最高形态。
- **LLMLingua** —— 提示词压缩，把长提示压成高信号短提示（后可视为上下文工程的一件工具）。

**为什么不够用**：提示词无法注入私有知识库、无法告知上周二代码库发生了什么、无法处理跨会话记忆、无法取代权限系统与错误恢复逻辑。**它按请求生效、无状态**，优化的是单次输入-输出对。一旦要求模型调用工具、追踪状态、跨步骤协作，单靠提示词就撞上硬天花板——即使提示词写得再好，模型读不到 `orders.py`，也没工具跑测试。

---

## 3. 阶段二：上下文工程（Context Engineering）

> 本节简要。它是**提示词工程的父集**，也是 Harness 在「单次推理」这个时间切片上的快照。

**定义**（Anthropic, 2025-09）：在 LLM 推理过程中**策展并维护最优 token 集合**的策略集，包含所有可能落入窗口的非提示词信息。

**七大组件**：System Prompt / User Prompt / State & History / Long-Term Memory / Retrieved Information (RAG) / Available Tools / Structured Output。

**四大操作**（LangChain 口径）：**Write（写入外部记忆）→ Select（按需选取）→ Compress（压缩）→ Isolate（隔离到子上下文）**。

**核心原则**：寻找「最小的高信号 token 集，最大化期望结果的概率」。被刻意排除的东西与放入的东西同样重要。

**Anthropic 列的七项技巧**：System Prompt 精校（每一行都问「删掉它模型会不会犯错」）/ token-efficient 的工具设计 / 策展示例（2~3 个典型代替长篇规则）/ Just-in-time 检索（运行时按需取，不预加载）/ Compaction / 结构化笔记（写 memory 文件而非塞回窗口）/ Sub-agent 架构（子 Agent 独立上下文，只回传结论）。

**三个核心子问题**：

1. **检索** —— 语义相似度在调试类任务上明显退化：调试的信号是调用链、git blame、三个文件外的符号定义，更接近 grep/glob 结构化搜索而非向量相似度。
2. **工具** —— 暴露哪些、如何描述、防过载。业界共识（HumanLayer）：**一组小而可组合的工具**（Read/Write/Grep/Glob/Bash）胜过不断膨胀的专用工具清单；接太多 MCP server 会把窗口塞满、更快进入降智区。若某 MCP server 只是复制了训练数据里表征良好的 CLI 能力，直接让 Agent 调 CLI 效果更好。
3. **记忆** —— 短期记忆=对话缓冲；长期记忆=结构化存储（用户偏好、项目规则、跨会话决策）。

**代表性开源项目**：

| 项目 | 落点 |
| --- | --- |
| **LlamaIndex** / **LangChain** | RAG 全链路（切分/索引/检索/rerank）、记忆抽象 |
| **MemGPT / Letta** | 把上下文当**分层内存**（main context / archival / recall），OS 式换页 |
| **Mem0** / **Zep** / **Graphiti** | 长期记忆服务化；Graphiti 做时序知识图谱记忆 |
| **Chroma** | 开源向量库 + context rot 等上下文质量实证研究 |
| **MCP（Model Context Protocol）** | 工具与数据的标准化接入协议；跨 Context/Harness 两层 |

**为什么不够用**：上下文工程管「模型看到什么」，不管**模型出错后怎么办**、**不能越过什么边界**、**系统如何验证输出正确**。它也不解决跨会话的连贯性——下一轮上下文窗口打开时，它对上一轮一无所知。

---

## 4. 阶段三：Harness 工程（深入）

### 4.1 定义与来源

**工作定义**：**Agent = Model + Harness**。Harness 是模型之外的全部——系统提示词与 AGENTS.md、工具与 MCP server、代码运行的沙箱、拦截危险命令的 hooks、压缩策略、审查子 Agent、决定何时重试/何时交接/何时停止的编排代码。

**Mitchell Hashimoto 的操作性定义**（2026-02，术语被广泛采用的起点）：

> 每当发现 Agent 犯了一个错误，就投入时间设计一个方案，**让它不再犯同样的错**。

**Addy Osmani 的实践推论**：**一个中等模型配好 harness，胜过一个强模型配坏 harness。**

术语脉络：在 Anthropic 的 SWE-bench 论文里，Agent 被直接定义为「模型 + 其周围的软件 scaffolding」，并指出**即使底层模型完全相同，scaffolding 质量不同会让 benchmark 得分差距巨大**。2026-02 Mitchell Hashimoto 使用后，OpenAI 一周后以「harness」为题发 Codex 案例研究，Anthropic 三月发 harness 设计深潜，到四月多数做编码 Agent 的团队已采用该词。

### 4.2 决定性数据点

| 证据 | 数字 | 含义 |
| --- | --- | --- |
| LangChain 换 harness 不改模型 | Terminal-Bench 2.0 得分 **52.8% → 66.5%**，排名从 30 名外进前 5（据业界转述，Benchmark 版本请以官方为准） | 模型是常量时，harness 是主变量 |
| OpenAI Codex 团队 | 3 人（后增至 7 人）5 个月约 **100 万行代码、1500 个 PR，零行人工手写代码** | 工程师的工作从「写代码」变成「设计环境」 |
| 同一模型不同 harness | Terminal-Bench 上分差显著 | 你体验到的行为，主要由 harness 决定 |

Codex 团队自己的瓶颈总结（值得原文引用）：

> 我们最困难的挑战，现在集中在**设计环境、反馈回路与控制系统**上，以帮助 Agent 达成目标。

### 4.3 Harness 的解剖：八层组件

| 层 | 组件 | 典型实例 |
| --- | --- | --- |
| 1 | **采样循环** | `while(not done): call_model → parse → execute → append_result` |
| 2 | **工具系统** | Read / Edit / Write / Bash / WebFetch / Grep / Glob / MCP |
| 3 | **权限层** | Auto mode / Allowlist / Sandboxing 三档可调 |
| 4 | **上下文管理器** | `/clear`、`/compact`、checkpointing、rewind |
| 5 | **记忆系统** | CLAUDE.md（项目级）+ 用户级 memory 文件 |
| 6 | **子 Agent 编排** | Task 工具派生子上下文 |
| 7 | **Hooks** | 工具调用前后自动触发 lint / 测试 / 危险命令拦截 |
| 8 | **错误恢复** | 工具失败重试、上下文超限自动压缩 |

更抽象地看，可以把 harness 理解为一个**控制面（Control Surface）**——模型提出意图，harness 在运行时边界内执行，环境产出观测，系统按契约验证，然后四选一：**继续 / 修复 / 停止 / 上报**。映射到七个可调面：Intent（成功的定义与显式排除项）/ Context / Action / State / Verification / Policy。

诊断口诀：**模型缺信息 → 查 Context；模型不能行动 → 查 Tools；行动不安全 → 查 Policy 与隔离；行动看似合理但错误 → 查 Verification 与任务设计。**「模型不行」从来不是一个诊断结论。

### 4.4 六个可复制的 Harness 模式

**(1) AGENTS.md / CLAUDE.md：确定性注入的项目记忆**
Harness 把这些 markdown 文件**确定性**地注入系统提示词。**关键反直觉结论**：苏黎世联邦理工（ETH）一项研究测试了 138 个此类文件，发现 **LLM 自动生成版本反而损害性能**——成本增加 20%+，多消耗 14~22% 推理 token，解决率没提升。HumanLayer 的 CLAUDE.md **不到 60 行**，只写普遍适用的简练指令（项目约定、工作流、验证命令），**不写目录清单、不写代码库概述**——让 Agent 自己去发现。少即是多。

**(2) Skills：渐进式披露**
不把全部指令预加载进系统提示，而是**触发时才加载**。每个 Skill 独立目录 + SKILL.md，激活时作为用户消息注入。调试技能不会污染重构任务的上下文。

**(3) Sub-agents：上下文的防火墙**
每个子 Agent 拿一个全新的、小的、高相关性的上下文窗口，父 Agent 只收压缩后的结果——中间的工具调用、grep 输出、文件读取全部隔离在子 Agent 内部。同时可**调节成本结构**：父会话用强推理模型做编排规划，子 Agent 用更便宜更快的模型执行。

**(4) Hooks：确定性反馈回路**
在生命周期特定事件触发（文件写入后、Agent 停止前、工具调用匹配某模式）。最有效的模式是 **Agent 每次停止后跑类型检查与 linter，只把错误回传；成功时静默**。逻辑极简：错误是反馈信号，通过的测试输出不该淹没上下文。

**(5) 背压机制（Backpressure）：验证层**
覆盖率下降 → 停止前提示补齐；TypeScript 报错 → 任务在错误解决前不得标记完成。这是填平「大概能用」与「可证明能用」之间鸿沟的手段，也是 HumanLayer 实践中**回报最高的投入**。

**(6) 环境即传感器**
Codex 团队把 **Chrome DevTools Protocol 暴露给 Agent**（可在自己的 worktree 里驱动运行中的应用复现 bug、点 UI 验证修复），以及**每个 worktree 一套可查询的本地可观测栈**（logs/metrics/traces，用 LogQL/PromQL 查）。于是「确保服务启动在 800ms 内完成」这类提示词变得可处理——因为测量启动速度的传感器被接进了 harness。

### 4.5 代表性开源项目

| 项目 | 许可 | 定位 | Harness 上的突出点 |
| --- | --- | --- | --- |
| **Claude Code**（Anthropic） | 源码可用，商业条款 | terminal / IDE / web 全能，Claude 深度集成 | 最深的可定制面：skills、subagents、hooks、权限分层、自动上下文压缩、后台 Agent；**Claude Agent SDK** = 去掉 CLI 的 harness 库 |
| **Codex CLI**（OpenAI） | **Apache-2.0**（2026-08 开源，Rust 实现） | 本地 CLI + 云端沙箱 | 「若想读一个生产级 harness 而非空谈，这是目前最好的一手来源」；审批策略与平台级沙箱分离；可脚本化 SDK、AGENTS.md、非交互运行 |
| **OpenCode** | **MIT**（约 162k stars，量级最高） | 终端原生，75+ 模型提供方 | 厂商中立首选；**双 Agent 架构**（只读规划 Agent + 执行 Agent），强制先 scoping 再改文件；server/API 形态明确 |
| **OpenHands**（原 OpenDevin） | **MIT** | 自治 Agent 平台，Docker 沙箱默认 | 每次会话跑在容器内，文件系统/网络/shell 全隔离；headless API 模式可接 CI；**明确标注 process mode 为 unsafe**（诚实的边界声明） |
| **Aider** | **Apache-2.0** | Git 原生终端配对编程 | 对「往上下文窗口里放了什么」异常透明（repo-map 可审计）；architect/editor 双模型模式（一个设计、一个实现）——Generator/Evaluator 分离的雏形 |
| **Goose**（Block → 2026-04 捐赠 Linux Foundation Agentic AI 基金会） | **Apache-2.0** | 编辑器无关的自治 Agent，MCP 驱动扩展 | 基金会治理=厂商中立；插件化扩展 |
| **Gemini CLI**（Google） | **Apache-2.0** | 终端 / headless / IDE | 审批模式 + Seatbelt(macOS)/容器沙箱；扩展/hooks/skills/实验性 subagents ⚠️ 2026-06-18 宣布退役，后继为闭源 Antigravity CLI（**选型时务必核查项目活跃度**） |
| **Cline** | **Apache-2.0** | VS Code 扩展，Plan/Act 监督 | 每个动作一个审批步骤，其**权限模型本身具有教学价值** |
| **Archon**（MIT） | MIT | **harness 之上的编排器** | 不替换你的 harness，而是驱动它：YAML 定义流程（plan→implement→review→开 PR）+ 人工审批闸门 + 隔离 git worktree 并行跑多个；自称「第一个开源 harness builder」 |

**选型提醒（诚实标注）**：star 数反映社区热度而非生产就绪度。Plandex（15k stars）已进入维护模式，Roo Code（24k）2026-05 归档，Gemini CLI 已宣布退役。**选型前请核查最近提交日期与 release cadence**，并区分「source-available」与「OSI 开源」（BSL/SSPL 过不了企业法务）。

### 4.6 Harness 工程的三条纪律

1. **失败即投资**：每次发现 Agent 犯错，就设计一个让它永不再犯的环境修复（lint 规则、测试、一行指南、更紧的权限）。**记录失败，不记录成功**，并观察同一类别是否二次出现。
2. **提示词修复会随换模型而蒸发，Harness 修复不会。**
3. **会漂移的事情交给确定性机制**：类型检查、linter、测试、结构化门禁——凡是能用命令验证的，都不要指望模型自觉。

### 4.7 局限

Harness 优化的是**一次运行**的质量。它不改变一个事实：**没人下指令，它就停。** 跨会话进度、无人值守的持续推进、并行任务之间的冲突，都不在 Harness 的职责范围内——这正是 Loop 工程出现的原因。

---

## 5. 阶段四：Loop 工程（深入）

### 5.1 定义：把「人」从循环里换掉

**Loop Engineering 的核心定义**：**用设计的系统，取代那个给 Agent 下指令的人。**

沿用 Anthropic 工程师 Boris Cherny 的表述：不是给它更好的提示词，而是「**把它放进一个循环里，它自己会去干活**」。

四层栈的第四层：

| 层 | 设计对象 |
| --- | --- |
| Prompt Engineering | 写好一条提示词 |
| Context Engineering | 选什么检索、怎么压缩 |
| Harness Engineering | 单次运行配好武器（工具、沙箱、验证） |
| **Loop Engineering** | **把 harness 调度起来反复运行** |

一个操作性类比：**Loop Engineering 就是「让 cron 定时唤醒一个配好武器、并知道先前发生了什么的 Claude」**。

### 5.2 五步循环与六大组件

**五步**：`Discovery（发现该做什么）→ Handoff（交接隔离环境）→ Verification（验证）→ Persistence（持久化）→ Scheduling（调度）`

| 步骤 | 承载组件 |
| --- | --- |
| **Scheduling** | Automations（定时器/触发器） |
| **Handoff** | Worktrees（隔离目录，供并行 Agent 使用） |
| **Discovery** | Skills（SKILL.md 常驻项目知识）、Connectors（MCP hooks 接外部系统） |
| **Verification** | Sub-agents（一个生成、另一个 review） |
| **Persistence** | Memory（磁盘持久化状态）、Connectors |

**缺一步就产生一个可预测的反模式**（这张表是 Loop 工程最实用的诊断工具）：

| 缺失 | 反模式 | 症状 |
| --- | --- | --- |
| **Verification** | **Nodding Loop（点头循环）** | 从不说「不」，坏代码照批 |
| **Persistence** | **Amnesiac Loop（失忆循环）** | 每天从头开始，状态日抛 |
| **Scheduling** | **Manual Loop（手动循环）** | 只在有人演示时才跑 |
| **Discovery** | **Blind Loop（盲目循环）** | 还是人在决定做什么 |
| **Handoff** | **Tangled Loop（缠结循环）** | 并行 Agent 在同一个目录上互相踩 |

### 5.3 理论根基：从 ReAct 到长时程 Harness

Loop 工程的学术血统可以清晰追溯：

| 工作 | 贡献 |
| --- | --- |
| **ReAct**（Yao et al., 2022） | Reason-Act-Observe 交替，奠定单循环形态 |
| **Reflexion**（Shinn et al., 2023） | 语言强化：失败后自我反思写入记忆，下一轮更好 |
| **Voyager**（2023） | 技能库沉淀——可复用行为成为持久资产 |
| **CodeAct / OpenHands**（2024） | 用**可执行代码**替代 JSON 工具调用作为动作空间，动作表达力跃升 |
| **SWE-agent / mini-swe-agent** | 证明「小而精的 Agent-Computer Interface」极其有效 |
| **Anthropic《Effective harnesses for long-running agents》**（2025-11） | **长时程 harness v1**：initializer agent + coding agent 角色切分 |

**长时程 harness v1 的机制**（这是 Loop 工程最值得抄的一份公开配方）：

```
第 1 次会话（initializer）：
  - 把任务拆成更小的 feature list（JSON）
  - 写 init.sh（一键把环境起回可用状态）
  - 建 git repo 并做初始 commit
  - 写 claude-progress.txt 进度文件

后续每一次会话（coding agent）：
  1. 读进度文件 + 查 feature_list.json + 看 git log
  2. 用 init.sh 起服务，确认环境干净
  3. **只推进一个有界增量**（实现一个 feature / 修一个具体问题）
  4. 测试 → 更新记录 → commit → 让仓库回到 clean state
```

关键点：**这不是提示词技巧，是操作脚手架。** 其本质是 Anthropic 用与人类班组交接完全相同的方式解决问题——可靠的入口路径 + 简短的工作日志 + 让下一个工人无需重建整个系统就能接着干的近期历史。

**Anthropic 的原话**（一句话道破重心的转移）：长时程 Agent 跨离散会话工作，**每个新会话开始时对前事一无所知**；解法是**结构化产物，用来跨越上下文窗口搭桥**。

> **核心主张重述**：不是更多的潜在聪明，而是**更好的外部状态**。

### 5.4 Generator / Evaluator 分离

这是 Loop 工程中最反直觉、也最重要的一条。

**观察**：让同一个 Agent 既生成又评判自己的产出，会产生**自证偏见（self-approval bias）**——Agent 会高估自己的结果，在主观任务（如设计）上尤甚。

**Anthropic 的解法**：设立**独立的 Evaluator Agent**，用 few-shot 示例与评分标准**校准**，并**默认怀疑**。Prithvi Rajasekaran（Anthropic Labs 工程负责人）：

> 把干活的 Agent 与评判的 Agent 分开，是解决这个问题的一个强力杠杆。

**具体形态**（2026-03《Harness design for long-running application development》，harness v2）：

- 角色从 `initializer + coding agent` 扩为 **planner + generator + evaluator**。
- Evaluator 用 **Playwright MCP** 像真实用户一样操作应用，测试 UI/API/数据库状态，不达标则打回。
- 前端设计任务定**四条评分标准**：设计质量、原创性、工艺、功能性；每轮 5~15 次迭代，最长可达 4 小时。
- **上下文重置（context reset）** 而非压缩：压缩虽保留上下文，但会让模型对接近上限变得谨慎，反而影响长任务表现。

**一个必须记住的告诫**：Evaluator 该不该存在，**取决于任务是否超出当前模型的可靠单干能力**。在 Opus 4.5 上它明显有用；到 Opus 4.6 模型本身变强，部分脚手架可以移除或减弱。

> **Harness 不是固定结构，而是与模型能力共同演化的。** 这是 Anthropic《Scaling Managed Agents》（2026-04）的核心命题——早先 Sonnet 4.5/Opus 4.5 有「上下文焦虑」、会主动缩小范围，所以需要更多脚手架；模型变强后脚手架应当被拆掉。**脚手架不手下线，就会变成新的技术债。**

### 5.5 双轨产物：人类可读 + 机器可读

成熟的 Loop 系统会把状态拆成互补的两种形态，缺一不可：

| 形态 | 实例 | 作用 |
| --- | --- | --- |
| **人类可读** | `CLAUDE.md` / `AGENTS.md`、`CHANGELOG.md`、`NOW.md`、`MEMORY.md`、`decisions.md`、`feature_list.json`（半可读）、git history | 保存** rationale、任务、负面知识**（哪些路走不通及原因），供运维者审阅 |
| **机器可读** | checkpoint 表、trace 事件、run-state 快照、验收门禁结果 | 供运行时 resume / diff / gate / 分析 |

Anthropic《Long-running Claude for scientific computing》规定了进度文件里该写什么：**当前状态、已完成任务、失败的尝试及失败原因、关键检查点的准确率表、已知局限**。最后一项是"运营黄金"——**负面知识与已知局限，比成功记录更值钱**。

> 只有散文的系统难以自动化；只有机器状态的系统难以审计。**2026 年的持久栈两者都要。**

### 5.6 代表案例与项目

| 案例 | 形态 |
| --- | --- |
| **Anthropic 长时程 harness**（2025-11 v1 / 2026-03 v2 / 2026-04 Scaling Managed Agents） | 参考架构本身；核心是「结构化产物 + 上下文重置 + 角色切分 + 与模型能力共演化」 |
| **Stripe Minions** | 每周合并 **1300+ 个机器写的 PR**，零人工代码。**确定性编排**（硬编码上下文采集、lint 闸门）与 **LLM 生成代码**交错，最后才由人 review。核心主张：**可靠性来自约束质量，而非模型规模** |
| **OpenAI Codex 后台 Agent** | doc-gardening agent 定时扫描陈旧文档开修复 PR；「垃圾回收」pass 扫描偏离 golden principles 之处、更新质量分级、开可在一分钟内 review 完并自动合并的重构 PR |
| **Addy Osmani 的 Morning Loop** | 读昨天的 CI 失败 → 开隔离 worktree → 子 Agent 起草修复 → 另一个 Agent review → 自动提 PR → 记录状态供明天继续 |
| **Archon**（MIT） | 开源可版本化的 Loop：YAML 流程 + 审批闸门 + worktree 隔离并行 |
| **OpenHands** | headless 模式把 Loop 接进 CI |
| **Conductor（OSS）/ OpenAI Agents SDK** | 治理层：审批闸门、预算、停止条件，防止跨真实世界边界的静默升级 |
| **Claude Code `/loop`** | 本地会话级循环（分钟级，带本地文件访问） |

调度三选一：Cloud（小时级，无本地访问）/ Desktop（分钟级，有本地文件）/ `/loop`（本地会话，分钟级，有本地文件）。

### 5.7 四类隐性成本与三条安全纪律

**隐性成本**（会静默累积，必须在设计期就预算）：

| 成本 | 含义 |
| --- | --- |
| **Verification Debt（验证债）** | 未经检验的产出不断堆积，直到发布日集中爆炸 |
| **Comprehension Rot（理解腐化）** | 开发者越来越看不懂循环写出的代码 |
| **Cognitive Surrender（认知缴械）** | 因为循环看起来可靠，人类停止 review |
| **Token Blowout（预算爆掉）** | 派生的 helper、重试、死循环可以烧光整个预算 |

**三条安全纪律**（第一个 Loop 上线前必做）：

1. **Read a Sample, Always** —— 每天读一份代表性产出，并能解释它的行为。
2. **Cap Before You Ship** —— 设定每次运行、每日、重试次数的上限，把风险关进笼子。
3. **Keep One Door Open** —— 永远保留一个人工介入的检查点。

### 5.8 局限

Loop 工程解决了「持续推进」，但没有解决**结构性问题**：一次跑几小时的东西崩溃了怎么办？多个 Loop 并行时状态如何不打架？审计与回滚怎么做？**Loop 的执行流是隐式的（藏在代码里），而隐式结构无法被检查点、回放、时间旅行和治理。** 这就是 Graph 工程的位置。

---

## 6. 阶段五：Graph 工程（深入）

### 6.1 定义

**Graph 工程**：把 Agent 的执行结构**显式声明为一等公民的有向图**——节点是工作单元（LLM 调用、工具、人工输入、子图），边是转换与条件路由，状态在图中流动并被持久化。

它不只是「多 Agent 编排」，而是三件事的合流：

1. **状态机语义**（确定性骨架）：分支、循环、并行 fan-out/fan-in、条件路由。
2. **持久化执行**（Durable Execution）：每个节点后写检查点，崩溃后从最后完成的节点恢复，而非从头重放。
3. **治理面**（Governance）：人工审批闸门、时间旅行调试、状态编辑、版本化、审计轨迹。

**一句话区分**：**Loop 是「一直跑」，Graph 是「跑到哪一步了、能回到哪一步、谁能拦它」。**

### 6.2 为什么必须有这一层

| 问题 | Loop 的处境 | Graph 的解法 |
| --- | --- | --- |
| 崩溃恢复 | 长任务跑到第 12 步挂了，只能从头重放 | 每节点检查点，从第 12 步续跑 |
| 调试 | 只能看日志 | **时间旅行**：回滚到任意历史步骤、改状态、用不同输入重放 |
| 人工介入 | 靠 hook 打补丁 | 一等公民的 interrupt（节点前/后暂停、改状态后恢复） |
| 并行 | 靠 worktree 手工隔离 | 状态对象的 merge 语义由框架处理 |
| 审计 | 事件日志散落 | 每次状态转换都有检查点，是最接近确定性审计轨迹的东西 |
| 可靠性 | 会话式框架约 70% 错误恢复率（据 LangGraph 侧 benchmark 引用） | 复杂多步任务约 **96% 错误恢复率**；等效任务 **4.2 次** LLM 调用 vs 会话式的 20+ 次（**确定性路由省掉的是重试轮次，不是聪明度**） |

### 6.3 关键技术要素

**(1) 类型化状态对象（State Schema）**
图的一切围绕一个显式 schema（TypedDict / Pydantic）。这是「可审计性」的物理基础——**没有类型化的状态，就没有可回放的图**。

**(2) Durable Execution / Checkpointing**
每节点后把完整状态写入后端（Postgres / Redis / 内存 dev）。这是把「小时级/天级工作流」从不可行变成可行的那一项。

**(3) Time-Travel Debugging**
因为每次转换都被检查点化，工程师可以回滚到任意前序步骤、修改状态、在不同输入下重放执行。当 Agent 在生产中得出错误结论时，可以重建分歧点的状态、定位致因输入、并**在同一检查点上验证修复**。对受监管行业，这是 Agentic 系统所能提供的最接近确定性审计轨迹的东西。

**(4) Human-in-the-Loop 为一等公民**
- `interrupt before`：在特定节点前暂停，等人工批准；
- `interrupt after`：节点执行后暂停，人工复核再继续；
- **edit state**：人在任意检查点修改状态后恢复。

**(5) 分层记忆**
短期工作记忆（单次 run 内持久）+ 长期持久记忆（跨会话）。

**(6) 子图（Subgraph）组合**
复杂工作流由小的、已测试的子图组合而成——分类子图 / 检索子图 / 生成子图各自独立开发测试再组装。**这是 Graph 工程对应「模块化」的答案。**

**(7) Deep Agents**（LangGraph 较新能力）
把规划、子 Agent 派生、文件系统三件事合成一个编排模式——**这实际上是 Graph 层与 Loop/Harness 层的合流点**：图提供结构，深度 Agent 提供自主性。

### 6.4 代表性开源项目

| 项目 | 许可 | 定位与要点 |
| --- | --- | --- |
| **LangGraph**（LangChain） | **MIT**（LangSmith / Platform 为付费 SaaS） | 事实标准。StateGraph + 条件边 + 循环 + 检查点 + 时间旅行 + HITL；2026 年初随 LangChain 一起到 1.0，获企业认证；Uber / LinkedIn / Klarna / JPMorgan / BlackRock / Cisco 生产使用；LangChain 1.0 底层即用 LangGraph。**代价：陡峭的学习曲线 + 强生态耦合**（同样流程 CrewAI 30 行，LangGraph 常 200 行） |
| **Google ADK**（Agent Development Kit） | Apache-2.0 | `SequentialAgent` / `ParallelAgent` / `LoopAgent` 三个显式工作流 Agent + LLM 驱动的动态路由；与 Eino ADK 血缘相近，**对本项目参照价值最高** |
| **CloudWeGo Eino / Eino ADK** | 开源 | 本项目底座。`compose` 提供 Graph/Chain/Workflow 编排；ADK 提供 ChatModelAgent / AgentTool / SetSubAgents / Workflow（Sequential/Parallel/Loop） |
| **Microsoft Agent Framework** | 开源 | AutoGen + Semantic Kernel 合流；Azure / Entra ID / M365 / Copilot Studio 原生；**企业治理面强**（身份、审计、合规、管理员控制台）——「LangGraph 赢工程白板，Microsoft 赢安全评审」 |
| **CrewAI** | 开源 | 角色模型（Researcher/Writer/Reviewer）+ Flows 图式编排；Crews 自治、Flows 精确控制；**读起来最直观、上手最快**；适合非确定性代价低的流程（内容、销售、研究摘要） |
| **OpenAI Agents SDK** | 开源 | 轻量：Agents / Handoffs / Guardrails / Sessions（自动历史管理）；会话内置近因过滤与上下文管理 |
| **LlamaIndex Workflows** | 开源 | 事件驱动 + 步骤式；与检索/RAG 生态天然咬合 |
| **AutoGen / AG2** | 开源 | 对话式多 Agent，群聊与可定制对话模式；社区治理 |
| **Dify / n8n / LangFlow** | 开源（部分条款需注意） | 低代码可视化编排；**Dify 是本项目产品形态的直接对照物** |
| **Temporal** | MIT / 商业 | **持久化执行的工业级底座**：Workflow 即代码、事件历史、任意时长、崩溃自愈。当 Agent 图需要「跑几周、绝不丢状态」时的答案 |
| **Inngest / Restate / DBOS / Orkes Conductor** | 开源 | 持久化执行 / durable workflow 的当代选项， increasingly 与 Agent 编排合流 |
| **PydanticAI / Mastra / Motia** | 开源 | 类型安全优先的轻量 Agent 框架；TypeScript 侧的对应物 |

**选型判据（务实版）**：

- 分支多、状态转换多、要工具重试、要自定义控制逻辑 → **LangGraph**。
- 企业身份/审计/合规/管理员控制台是硬需求 → **Microsoft Agent Framework**。
- Agent 是**产品功能** → LangGraph；Agent 是**企业内部助手** → Microsoft。
- 简单管道式、快速出原型 → CrewAI / OpenAI Agents SDK。
- 要跑几周且绝不丢状态 → Temporal / Restate 这类 durable execution 引擎。

### 6.5 代价与风险（诚实标注）

1. **学习曲线与代码量**：要显式写状态 schema、逐条连边、配 checkpointer 后端、实现 interrupt handler。一项不复杂，加起来在第一个 Agent 跑起来之前是可观的代码量。
2. **概念负担**：图思维对习惯命令式代码的开发者不友好。
3. **平台锁定**：LangGraph 虽 MIT，但离开 LangChain 生态会失去大部分杠杆；LangSmith 是付费的。
4. **过度结构化的风险**：这是最需要警惕的一条。**能用 Loop 解决的，别上 Graph。** Graph 的价值随「任务时长 × 状态复杂度 × 治理要求」增长；对一次十步以内的对话，图是纯粹的负担。
5. **与模型能力共演化**：Anthropic 的告诫同样适用于此——模型变强后，部分显式结构应当被拆掉。**图僵化不动，就会成为新的技术债。**

---

## 7. 五层收敛表（速查）

| 维度 | Prompt | Context | Harness | Loop | Graph |
| --- | --- | --- | --- | --- | --- |
| 控制粒度 | 一条消息 | 一窗 | 一次运行 | 跨会话闭环 | 执行结构 |
| 时间尺度 | 单次 | 单轮 | 分钟~小时 | 小时~天~常驻 | 分钟~周 |
| 核心资产 | 提示词 | 检索/记忆/压缩 | 工具·沙箱·hooks·技能 | 进度产物·调度·评估器 | 状态图·检查点 |
| 失败后 | 改措辞 | 改检索/压缩 | **加一条环境修复** | 查五步缺哪步 | 回滚到检查点重放 |
| 成功指标 | 输出稳定 | 高信噪比 token | 一次跑对 | 无人值守持续产出 | 可恢复/可审计/可并行 |
| 主要风险 | 表达天花板 | context rot | 覆盖不到跨会话 | 验证债/预算爆掉 | 过度结构化 |

**三条贯穿全程的原则**：

1. **上层不取消下层，而是把下层收编为组件。** 每一层的压缩提示词、工具描述、子 Agent 指令，仍是提示词——**在最高层做提示词工程，收益最大**（Anthropic 明确建议「仔细调优压缩提示词」）。
2. **越往上，「外部状态的质量」越比「模型的聪明度」决定成败。** 从 Harness 的错误永久修复，到 Loop 的进度产物，到 Graph 的检查点——全部都是外部状态。
3. **脚手架必须与模型能力共同演化，否则自己变成技术债。** 每增加一个 Agent 结构件，都要问一句「模型变强后它还需要吗」。

---

## 8. 相关资料

- 一手官方博客（Anthropic Engineering）：《Effective context engineering for AI agents》(2025-09)、《Effective harnesses for long-running agents》(2025-11)、《Demystifying evals for AI agents》(2026-01)、《Harness design for long-running application development》(2026-03)、《Scaling Managed Agents》(2026-04)
- Anthropic《Building effective agents》(2024-12)：workflows 与 agents 的经典分野
- Chroma Research：context rot 实证研究
- LangChain / LangGraph 文档：durable execution、time-travel、Deep Agents
- CloudWeGo Eino 文档：`compose` 编排原语与 Eino ADK
- 本仓库 `36_智能体开发架构深度对比_本地实现与DeepSeekHarness.md` —— 十层架构纵向剖析
- 本仓库 `08_调研预研/38_智能体演进路线建议_HarnessLoopGraph.md` —— 本档在 multiagent-lab 上的落点建议

## 9. 迭代记录

| 版本 | 日期 | 要点 |
| --- | --- | --- |
| v1.0 | 2026-09-29 | 初稿：五阶段定义与边界、演进脉络、Harness/Loop/Graph 三层深入分析、代表开源项目全景、五层收敛表。提示词与上下文两节按指示从简。二手数据点已标注「据业界转述」。 |
