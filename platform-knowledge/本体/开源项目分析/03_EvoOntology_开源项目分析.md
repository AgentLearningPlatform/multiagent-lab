# 开源项目 EvoOntology 分析：场景与实现方案

> **分析日期**：2026-09-28（第二版，已整合 DataFun 详解文）
> **信息来源**：
> - ① DataFun《重磅开源EvoOntology：打掉本体"建设、维护、更新"难题》（2026-09-28）— **一手详解文，提供精确实验数据、成本三阶段、收益分布、跨模型迁移实验**
> - ② 微信公众号《Graph的尽头是自进化Ontology~》（PaperAgent，2026-09-26）— 提供背景动机与 BI-Bench 佐证
> - ③ GitHub 官方仓库 README `ruc-datalab/EvoOntology` — 提供架构定义、工具名、生命周期
> - ④ arXiv 2609.15779 论文摘要 — 方法学表述
> **交叉验证说明**：①②③ 相互印证的部分直接采用；此前记录的 backbone 口径差异，在新资料下已获得合理解释（见第八节）。搜索中出现的一条二手站点（aisignal.dev）内容与官方 README 矛盾且无出处，**不予采信**。

---

## 一、项目概览

| 项 | 内容 |
|---|---|
| 项目名 | **EvoOntology** — A Self-Evolving Ontology Layer for Data Agents |
| 定位 | **首个面向 Data Agent 的"自进化本体层"** |
| 团队 | 中国人民大学（RUC DataLab） |
| 论文作者 | **张绍磊**（中国人民大学信息学院助理教授） |
| 论文发布 | **2026 年 9 月 14 日** |
| 论文 | https://arxiv.org/abs/2609.15779 |
| 代码 | https://github.com/ruc-datalab/EvoOntology |
| 关联工作 | **BI-Agent / BI-Bench**（微软研究院 & UIUC），arXiv 2609.20886，GitHub Hu-Chuxuan/bi-agent |

一句话概括：**把本体从"人工维护的静态说明书"变成"从数据里长出来、由 Agent 执行轨迹驱动、带安全回滚的运行时服务"。**

> **延伸信息**：论文作者张绍磊将在 **DACon 2026 北京站（10 月 23–24 日，北京希尔顿逸林酒店）** 做主题分享《自进化数据智能体：迈向 Data–Ontology–Agent 协同进化》，现场拆解三层 Ontology 与"轨迹归因、知识修正、成对评测与安全回滚"机制。关注该方向者值得留意。

---

## 二、要解决的核心问题：agent–data gap

### 2.1 问题定义

论文把缺口命名为 **agent–data gap**：

> 数据住在 Agent 外面，Agent 只能通过 SQL 接口、文件读取器这类通用工具摸到**列名和路径**；数据的**结构和语义它事先一概不知**，只能靠反复发探测查询"盲探"。

**落到企业里，它是这些具体问题**（DataFun 文开篇给出的例子，很接地气）：

- 同一个「**客户**」在不同系统里到底是不是同一个对象？
- 「**收入**」该取订单金额、确认收入，还是回款？
- 一个**风险事件**该关联哪些账户和合同？

这些业务含义，模型**不会天然知道**。

### 2.2 真正麻烦的地方：谁来持续维护？

Ontology 真正的难题会很快暴露：**业务一直在变，数据表越来越多，字段含义会调整，Agent 使用数据的方式也在变。一套 Ontology 建好之后，谁来持续维护？**

EvoOntology 的答案：不把它当成靠数据团队长期人工维护的静态语义层，而是**让 Agent 根据自己的任务和执行轨迹，持续修改它**。

### 2.3 为什么现有两条路都走不远

| 路线 | 做法 | 失败原因 |
|---|---|---|
| **Raw querying（裸查）** | 让 Agent 自己探索数据 | 小数据还行；数据源一宽一杂就陷进**重复低效的探测循环** |
| **静态 Semantic Layer** | 人工维护语义层，塞进上下文/prompt | 既占 Context，又带入大量与当前任务无关的内容；**实证反而拉低准确率**（见下） |

#### 实证：静态语义层是"负资产"

DataFun 文给出了 DDR-Bench 上的三组精确对照：

| 模型 | 无 Ontology | 静态 Semantic Layer | EvoOntology |
|---|---|---|---|
| **Claude-Sonnet-5** | 72.5% | **57.5%**（↓15.0） | **81.3%** |
| **GPT-5.6-sol** | 68.5% | **65.5%**（↓3.0） | **93.5%** |

在论文测试的六种模型上，EvoOntology 相比无 Ontology 的 Baseline，Trajectory-Wise **平均提升 17.8 个百分点**。

> 注：静态 Semantic Layer 并非无价值，关键是**怎么用**。整套业务知识提前塞进 Prompt，它更像"一本很厚的说明书"——Agent 每次都要从中重新筛选，**Ontology 越复杂，这种做法越难扩展**。
> PaperAgent 那句总结很锋利：**"同一份语义，画成死图就是负资产。"**

### 2.4 佐证：静态 Graph 的"红利与天花板"（BI-Bench）

微软 & UIUC 的 BI-Bench 用实证给出了"图有用但不够"的边界：

- 从 **3000+ 个真实 Power BI 项目**中，花 **400+ 人小时**校验出 **100 对「业务问题 + 标准答案」**，建成首个端到端 BI 基准。
- **实测成绩很差**：最强模型 o4-mini 仅 **48.2%**；BIRD 榜单前四的 NL2SQL 模型集体跳水到 **6.0%~17.3%**（一道题最多同时推理 **38 张表**）。
- **BI-Agent 吃到图的红利**：把 search / transform / join 封装为 Agent 工具，join 工具针对雪花、星座结构做**全局推理**，比 LLM 自己猜 join 准得多。
- **但天花板也在这张图上**：雪花模型的 join 边全靠人工画死，**只给了结构，给不了理解**——每个任务仍要从零猜，且猜完就扔。

---

## 三、场景分析

### 3.1 适用场景

- **异构数据源上的 Data Agent**：表格（tables）、文件（files）、数据库（databases）混杂，Agent 需跨源理解。
- **端到端 BI / 数据分析**：从几十张表选表、变形、定义 join、再到分析（BI-Bench 场景）。
- **多源数据研究（DDR-Bench）** / **商业洞察（InsightBench）** / **Text-to-SQL（BIRD）**。
- **"重复探索成本"高的场景**：Agent 反复对同一批数据做探测时，自进化本体的摊销收益最明显（见 5.2）。
- **维护人力跟不上的语义层**：业务、数据、Agent 都在变，却只能靠数据团队逐条维护 Ontology 的组织。

### 3.2 不适用场景 / 前提依赖

- **没有 workload 可供 grounding**：初始图由 Builder Agent 读取真实任务、对数据源发 probe 查询验证后生成；无可观察执行轨迹，自进化循环无从启动。
- **需要严格形式化推理**：它是**类型化语义图**，不是 OWL/RDF 逻辑体系；需形式化推理的场合仍走 Protégé/Jena/HermiT。
- **一次性、低重复度的临时查询**：轮数/成本摊销收益体现不出来（但也无害）。
- **多模型混跑且不希望维护多份本体**：见 5.4——不同模型进化出的本体差异显著，跨模型复用会掉分。

### 3.3 与邻近方案的分工

| 方案 | 解决的痛点 | 与 EvoOntology 的关系 |
|---|---|---|
| **BI-Agent** | 端到端 BI 工具化编排（join 等数据管理算法） | **互补**：BI-Agent 的工具操作数据，EvoOntology 的工具查询"理解" |
| **ReAct + Memory（记忆回放）** | 记住做过的轨迹 | **被超越**：Memory 68.5→75.8（+6.3），EvoOntology 达 **89.5**（+20.0）。记忆是历史记录，本体是**可组合的结构化规则** |
| **静态语义层** | 提供业务语义 | **被替代**：人工维护、易过期、全量注入反噬准确率 |
| **OpenSPG 动态本体** | 严谨 Schema 约束 + 图谱推理 | **参考**：都主张本体需演化；OpenSPG 面向 KG 问答，EvoOntology 面向数据 Agent 执行轨迹 |
| **OntoFlow** | 企业本体建模 + 可执行本体 | **参考**：OntoFlow 演化靠人/多 Agent 驱动；EvoOntology 由**配对评估门控**自动驱动 |

---

## 四、实现方案（核心）

### 4.1 总体思路

EvoOntology 把 **Ontology Layer 当作"可训练的 Agent 状态"，而不是模型权重**（"trainable agent state—not model weights"）。本体被封装为一个 **MCP Server**，包含三层：

```
┌─ Schema Layer ─────── 定义"图式"：节点类型字段、允许的边类型、引用规则（也可被受控修改）
├─ Content Layer ────── 业务语义本体：Terms / Mappings / Constraints / Evidence
└─ Tool Layer ───────── 运行时访问：browse / resolve + 极简 manifest
```

配两类 Agent：
- **Builder Agent**：自主构建初始本体
- **Evolution Agent**：分析轨迹、持续演化本体

### 4.2 Content Layer：四类节点 + 两类边

**四类节点（node families）**：

| 节点族 | 职责 | 举例 |
|---|---|---|
| **Term** | 业务概念 | Revenue、Customer |
| **Mapping** | 把概念对应到**底层数据表、字段和 join 路径** | Revenue → 具体字段与关联方式 |
| **Constraint** | 数据使用时的限制 | 「统计收入时必须过滤退款订单」 |
| **Evidence** | 支撑判断的**数据证据** | 当初 probe 验证时的依据 |

> **关键理解**：Ontology 里不会只写一句"Revenue 代表收入"，还要说明**对应哪个字段、怎样关联、什么条件下才能用、当初根据什么数据确定下来**。

**两类边**：

| 边类型 | 职责 |
|---|---|
| **Semantic Relations** | 连接 Term 之间，表达派生与关联 |
| **Structural References** | 把 Term 与 Mapping / Constraint / Evidence 挂在一起 |

> **设计要点**：把「概念」「落地」「约束」「证据」拆成四类**可独立演化**的对象，是后续能做**局部定向修补**的前提。

### 4.3 Schema Layer：给进化划边界

定义四类节点族各自的**字段**、允许的 **Semantic Relation 类型**、允许的 **Structural Reference 模式**，设定本体的**表示边界**。

> **注意它的开放性**：Schema Layer 管的正是 Ontology 自己的结构——**如果以后出现新的业务关系、现有结构描述不了，它允许继续修改**。这正是"自进化"不止于内容补充的体现。

### 4.4 Tool Layer：从"被动看图"到"主动查图"

通过 **MCP** 暴露为两个工具 + 一份清单：

| 工具/构件 | 作用 |
|---|---|
| **browse_semantics** | 按查询**检索相关节点**（找路） |
| **resolve_semantics** | 取回**完整语义邻域**（展开） |
| **compact session manifest** | **极简清单**初始化会话；详细记录与关联对象**按需取用** |

**典型交互**（DataFun 文给的例子）：Agent 要研究某公司的 Revenue，先找到相关业务概念，再往下解析具体字段、映射关系和约束——**不需要提前读完整个企业数据字典**。

> **关键转变**：Ontology 不再是 Prompt 前附带的一大段背景，而是变成 **Agent Harness 里一个独立的运行时服务**。

### 4.5 Builder Agent：初始本体怎么来

不要求人工建设。流程：

1. 读取**一批真实任务**，从中寻找经常出现的**实体、指标、操作和分析条件**；
2. **主动检查底层数据**：判断某字段可能对应 Revenue 后，还要看**字段类型、真实数据值和关联关系**；
3. **确认映射合理才写入 Ontology**，同时**保存相关 Evidence**；
4. 发布 **ontology_v0**。

> 这与我们资料库中 LLMs4OL 的共识完全一致（LLM 擅长早期抽取、不擅长形式化验证）——EvoOntology 把"必须验证"做成了硬流程。

### 4.6 Lifecycle：五阶段闭环

```
Build   → Builder 从 workload 提取候选概念，probe 验证后才提交，发布 ontology_v0
Use     → Data Agent 按需查询 Ontology Layer；记录工具交互与结果
Evolve  → 诊断重复出现的问题，归因到 Content / Tool / Schema 之一，产出局部 Candidate patch
Evaluate→ Candidate 与 Parent 在相同数据、相同 Agent、相同解码参数、相同执行预算下配对比较
Publish or reject → 通过则发布 ontology_vN+1；否则回滚（保留旧版本）
```

对应原则：**Grounded construction**、**Targeted evolution**、**Gated versioning**。

### 4.7 自进化循环：诊断 → 归因 → 修补 → 门控

1. **诊断（Diagnose）**：Evolution Agent 分析历史执行轨迹，寻找**重复出现的问题**。
2. **归因（Attribute）**：判断应修改哪一层——
   - Agent 经常把业务概念**映射到错误字段** → 改 **Content Layer**
   - 正确信息已存在，但 Agent **总是检索不到** → 调整 **Tool Layer**
   - 新的业务关系**没有合适结构表示** → 改 **Schema Layer**
3. **修补（Patch）**：**不重建整套 Ontology**，只生成局部 Patch。
4. **门控（Gate）**：新 Candidate 与 Parent 在同一组验证任务上重跑，**模型、解码参数、执行预算保持一致**；**只有新版本确实更好才接受**，否则保留旧版本。

> 所以论文里的 "Self-Evolving" **不是让 Agent 自由修改知识库**，而是一条清楚链路：跑任务 → 收集轨迹 → 找失败模式 → 定位问题 → 生成修改 → **验证决定是否接受**。

### 4.8 五大设计原则（README 提炼）

| 原则 | 核心思想 |
|---|---|
| **Active access（主动取用）** | 通过 MCP 只取当前步骤所需语义，而非全量注入 |
| **Grounded construction（落地构建）** | 围绕 workload，且对象仅在对底层数据验证通过后才提交 |
| **Targeted evolution（定向演化）** | 诊断轨迹，对互联的三层施加局部更新 |
| **Gated versioning（门控版本）** | 仅当配对评估显示可复现提升时才发布 Candidate |
| **Agent integration（Agent 集成）** | 通过插件把本体工作区与 MCP 运行时直连到 Agent |

### 4.9 ★ 收益分布：一半以上来自"怎么用"而非"有什么"（重要洞察）

论文对被接受的演化修改做了收益归因：

| 层 | 累计增益占比 |
|---|---|
| **Tool Layer** | **57%** |
| **Content Layer** | **34%** |
| **Schema Layer** | **9%** |

> **超过一半的收益来自"怎么让 Agent 使用 Ontology"，而不是继续往里面补更多知识。**
>
> 这与传统理解不同：过去企业建 Ontology，核心工作是把业务对象和关系**定义准确**；进入 Agent 场景后，业务语义依然重要，但 **Agent 怎么查询、什么时候查询、查询到什么粒度，也开始成为 Ontology 的一部分**。

### 4.10 工程集成形态

- 本体封装为 **MCP Server**，作为**通用 Agent 插件**：官方 README 明确支持 **Claude Code、Codex** 及其他 AI Agent。
- 与本项目此前记录的"本体暴露为 MCP 服务"方向（Palantir Ontology MCP、KAG 0.8 接入 MCP）**高度同构**——EvoOntology 给出了**可直接跑的开源实现**。

---

## 五、实验结果

### 5.1 主实验：三个 benchmark

覆盖 **DDR-Bench（多源数据研究）、InsightBench（商业洞察）、BIRD（Text-to-SQL）**，性能沿 **Baseline → Initial → Evolved** 逐步提升（随进化轮次单调爬升、末两轮趋于平坦——是**收敛的精炼**而非单次走运）。

DDR-Bench 上的关键对照：

| 设置 | Claude-Sonnet-5 | GPT-5.6-sol |
|---|---|---|
| Baseline（无 Ontology） | 72.5% | 68.5% |
| 静态 Semantic Layer | 57.5% | 65.5% |
| **EvoOntology** | **81.3%** | **93.5%** |

四模型（GPT-5.5、GPT-5.6-sol、Claude-Sonnet-5、Claude-Opus-4.8）平均：**Trajectory-Wise 69.5% → 89.5%**。

**对比记忆回放**（DDR-Bench）：ReAct Baseline 69.5% → +Memory **75.8%**（+6.3）→ **EvoOntology 89.5%**（+20.0）。差距的本质：Memory 保存"过去某个任务怎么做过"，Ontology 把经验**重新整理成可查询和可组合的结构**。

其他基准：GPT-5.5 在 DDR-Bench 上 **+26.7**（此前报道口径）；BIRD 上 **EX 与 VES 双升（+7.4 / +8.6）**——既让 SQL 更对，也让 SQL 更快。

### 5.2 成本：涨分的同时反而更便宜

DataFun 文给出了**完整三阶段**数据（比此前只有两端更清晰）：

| 阶段 | 轮均输入 Token | 平均任务轮数 | 单任务总 Token |
|---|---|---|---|
| 无 Ontology | 3.2K | 14.6 | 52.6K |
| **初始 Ontology** | 4.1K | **11.2** | **50.4K** |
| **进化后 Ontology** | 4.6K | **8.4** | **42.0K** |

**总 Token 下降约 20%**，同时 Trajectory-Wise **69.5% → 89.5%**。

> **机制**：没有 Ontology 时，Agent 每次都要重复探索——找表、看字段、检查取值、尝试 Join、判断业务含义。Ontology 把已验证过的结果沉淀下来供后续复用：**每步多读一点语义，减少大量无效探索。**

### 5.3 消融实验：谁是承重墙

DDR-Bench Trajectory-Wise：

| 配置 | 分数 | 影响 |
|---|---|---|
| **完整 EvoOntology** | **89.5%** | — |
| 取消 Gate（修改直接进下一版） | **78.3%** | **-11.2** ← 最大承重墙 |
| 取消 Attribution（不判断归属哪层） | 下降（约 -6.3） | 不分层归因 → 修错地方 |

> 没有配对评估把关，自进化就退化成无约束的随机修改；没有归因，就会出现"manifest 出问题却去改节点、内容出问题却去改图式"。

### 5.4 ★ 跨模型迁移：Ontology 是模型特异的（重要发现）

论文测试了"同一套 Ontology 能否直接给不同模型共用"：

- 让 **GPT-5.5、GPT-5.6-sol、Claude-Sonnet-5、Claude-Opus-4.8** 从**同一个初始 Ontology** 出发，各自按自己的执行轨迹进化；
- **最终得到的 Ontology 并不一样**——不同模型最终接受的 Term 之间，**两两 Jaccard 重合度最高不超过 0.62**；
- 交叉实验：把一个模型进化出的 Ontology 直接交给另一个模型，**性能都会下降**：**跨模型迁移至少下降 6.6 个百分点**，部分情况平均差距达 **10.9 个百分点**；使用自己进化出的 Ontology 效果最好。

> **含义**：同一份企业业务知识，不代表所有 Agent 都应该用同一种方式访问。
> 客户、合同、订单、收入的定义相对稳定，但**这些内容该怎样组织、哪些字段先暴露、工具怎么返回结果，可能与具体模型有关**。这对平台设计是个重要约束（见第六节）。

---

## 六、对本平台（Agent + Ontology）的可借鉴点 ★

EvoOntology 是目前梳理到的**与本平台目标最契合的单一开源项目**（Agent + 本体 + MCP 三者同时命中）。按优先级：

1. **"分层 + 归因"的演化机制（首要）**
   拆成 **Content / Schema / Tool** 三层，失败时先归因到某一层、只做局部 patch。解决自演化系统最常见的失败模式——**改错地方**。建议直接照搬三层划分与归因流程（尤其是"检索不到 → 改 Tool 层"这条判定，极易被忽略）。

2. **配对评估门控是刚需（-11.2，承重墙）**
   任何"自进化"的本体/技能/提示都必须配：**同数据、同 Agent、同解码参数、同执行预算下的 Parent vs Candidate 配对对比 + 阈值 + 回滚**。没有这道门，自进化就是随机游走。

3. **★ 资源重心要从"补知识"转向"设计访问方式"（Tool Layer 占 57%）**
   这是对传统本体建设直觉的**修正**：平台不应把主要精力全投在"把业务对象和关系定义准"，而应同步甚至优先投入 **browse/resolve 的检索粒度、manifest 的内容裁剪、工具返回结构**。这是本轮新资料带来的最大认知增量。

4. **★ Ontology 是模型特异的——要为它单独设计**
   Jaccard ≤ 0.62、跨模型 -6.6～-10.9。意味着平台若多模型混跑，**不能假定一套本体通吃**。可选策略：按主干模型分别演化并版本化管理，或在切换 backbone 时触发再进化/适配流程。

5. **"主动查图"（Active access）替代全量注入**
   只把极简 manifest 放进 prompt，其余按 browse/resolve 按需取。这是静态语义层从 **-15 个百分点**翻转为正收益的关键工程手段，也是**总成本 -20%** 的直接原因。

6. **四类节点拆分（Term / Mapping / Constraint / Evidence）**
   尤其把 **Evidence**（可追溯）与 **Constraint**（防错）作为一等公民。相比传统本体只存"概念+关系"，这是面向 Agent 的重要扩展。

7. **"Ontology as trainable agent state, not model weights"**
   不微调模型、只演化外部语义状态 → 可审计、可回滚、成本低；天然适配多 backbone（但要接受 5.4 的模型特异性代价）。

8. **MCP 作为标准暴露方式**
   与 Palantir Ontology MCP、KAG 0.8 的 MCP 接入同构；EvoOntology 提供了可直接参考的开源实现。

9. **Grounded construction：先验证再提交**
   Builder 提出的候选须经 probe 对真实数据验证（字段类型、真实数据值、关联关系）才入图——避免 LLM 幻觉进入本体，与 LLMs4OL 共识一致且已工程化。

---

## 七、风险与局限

| 项 | 说明 |
|---|---|
| **成熟度** | 2026-09-14 发布，属研究性质早期工作；社区与文档仍在建设，PoC 前需评估代码可用性 |
| **无形式化推理** | 类型化语义图而非 OWL/RDF；需严格逻辑推理的场景要另配推理机 |
| **依赖 workload** | 初始构建与后续演化都依赖可观察执行轨迹；冷启动收益有限 |
| **企业复杂性未覆盖** | 论文实验集中在 DDR-Bench / InsightBench / BIRD 等 benchmark；**真实企业里的权限控制、业务规则冲突、多人修改、审计、长期版本治理要复杂得多** |
| **模型特异性成本** | 多模型环境需维护多份本体或做迁移适配（见 5.4） |
| **Agent 生态适配** | README 主要标注 Claude Code / Codex；自研 Agent 需自行适配 MCP 与插件层 |
| **维护持续性** | 学术团队出品（与 OntoFlow"目前没有团队"风险同类），需评估长期维护意愿 |

---

## 八、信息口径说明（已更新）

1. **backbone 数量差异——现已获得合理解释**（此前标注为"不一致"）：
   - **六种模型**：DDR-Bench 主实验的性能平均口径（平均 +17.8 个百分点）；
   - **四个模型**（GPT-5.5、GPT-5.6-sol、Claude-Sonnet-5、Claude-Opus-4.8）：成本分析与 cross-model 迁移实验的口径（Traj-Wise 69.5%→89.5%）；GitHub README 摘要所说的 "four LLM backbones" 与此吻合。
   - 两者**并非矛盾**，而是不同实验组。引用时应说明具体是哪一组。

2. **静态 Semantic Layer 的降幅有两种口径**：
   - DataFun 文：Claude-Sonnet-5 上 **72.5% → 57.5%（-15.0）**；GPT-5.6-sol 上 68.5% → 65.5%（-3.0）。
   - PaperAgent 文："拉低 15 个百分点"（对应 Sonnet-5 那一组）。
   - 引用时应**写明具体模型**，避免笼统说"-15"。

3. **不予采信的二手源**：搜索中出现的第三方站点（aisignal.dev）声称 EvoOntology 是"pip 可装的 Claude Code 插件、MIT 协议、幻觉率 18.2%、约 2.2k 行代码、14 forks"等——**与官方 README 矛盾且无出处，疑似自动生成**。请以 GitHub 仓库与 arXiv 原文为准。

---

## 九、参考链接

| 资源 | 链接 |
|---|---|
| EvoOntology 论文（2026-09-14） | https://arxiv.org/abs/2609.15779 |
| EvoOntology 代码 | https://github.com/ruc-datalab/EvoOntology |
| **详解文章（本版主要依据）** | 《重磅开源EvoOntology：打掉本体"建设、维护、更新"难题》DataFun，2026-09-28 |
| 背景解读文章 | 《Graph的尽头是自进化Ontology~》PaperAgent，2026-09-26 |
| BI-Agent 论文 / 代码 | https://arxiv.org/abs/2609.20886 ／ https://github.com/Hu-Chuxuan/bi-agent |
| 作者现场分享 | DACon 2026 北京站（10/23–24），张绍磊《自进化数据智能体：迈向 Data–Ontology–Agent 协同进化》 |

---

## 十、后续建议动作

1. **克隆并跑通** `ruc-datalab/EvoOntology`，重点看三处实现：
   - Tool Layer 的 `browse_semantics` / `resolve_semantics` 与 **manifest 的内容裁剪策略**（收益占比最高的部分）；
   - Evolution Agent 的**归因判定逻辑**（如何决定改 Content / Tool / Schema）；
   - Gate 的**配对评估与回滚**代码结构。
2. **对照阅读** OpenSPG 动态本体（见《02_知识图谱相关开源方案梳理.md》3.2 节）：两者都解决"本体需随需求演化"，但 EvoOntology 的**三层归因 + 门控**经消融验证，值得优先吸收。
3. **PoC 设计建议**：选一个"多表 + 重复查询"的子场景（如数据分析助手），照搬 `Build → Use → Evolve → Evaluate → Publish` 五段生命周期；**务必同时监控**准确率、轮数与总 Token 三项指标，验证"成本下降 + 准确率上升"是否可复现。
4. **多模型评估前置**：若平台规划支持多个 backbone，需在 PoC 阶段就测试切换模型后的表现落差，提前设计"按模型分支演化"或"迁移适配"机制。
