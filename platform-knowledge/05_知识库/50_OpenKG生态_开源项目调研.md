---
module: 知识库
topic: OpenKG 生态开源项目调研
desc: OneGraph / OpenSPG / KAG / SkillNet / OneEval 四层闭环的一手核验：使用场景 + 实现方案 + 借鉴三档（含对 31 号二手描述的勘误）
synced: 2026-09-30
---

# OpenKG 生态开源项目调研

> **调研日期**：2026-09-30
> **信息来源**：GitHub 仓库一手 README / arXiv 论文页 / openkg.cn 官方数据集与评测规则页 / OpenKG 年度回顾（2026-02）
> **核验原则**：本项目 31 号档《知识图谱相关开源方案梳理》已收录 OpenKG 四层摘要，但其来源为二手文章合集。**本档对每个项目的仓库位置、规模数字、许可与能力边界逐一做一手核验**，凡与二手描述冲突者在 §1.2 出勘误表
> **体例**：每个项目给「定位 / 使用场景 / 实现方案 / 关键数字 / 边界」五段；文末两张汇总表

---

## 一、摘要与勘误

### 1.1 生态全景

OpenKG（开放知识图谱社区）在 2025-2026 年形成了**四层递进的闭环**，四层分别由不同 SIG 兴趣小组承建：

```
数据层   OneGraph           ── 千万级双语开放知识图谱 + E/R/G/T 服务矩阵        （SIGData）
语义底座 OpenSPG            ── SPG 语义增强可编程图引擎 + 动态本体             （SIGSPG）
推理层   KAG / KAG-Thinker  ── 逻辑形式引导的混合推理求解 + 推理能力内化       （SIGSPG）
Agent 层 SkillNet           ── 50 万级技能库 + 三层技能本体 + 五维评估         （SIGAgent/SIGTool）
评测层   OneEval            ── 5 类知识库 × 5 领域的知识密集型推理评测         （SIGEval）
配套     OneKE / OpenKG-ToolAgent ── schema 引导抽取 / KG 工具 MCP 封装
```

**一句话判断**：这套生态的真正价值**不在任何单点技术**（图数据库、RAG、技能库单看都有成熟平替），而在**四层之间形成了"数据→语义→推理→能力→评测"的可闭环验证链路**——尤其 OneEval 提供了把"知识增强到底有没有用"变成可度量问题的工具，这在业界是稀缺的。

### 1.2 勘误表（一手核验 vs 二手描述）

31 号档与 network 流传的二手描述存在以下**必须纠正**的事实偏差：

| # | 二手说法 | 一手核验结果 | 影响 |
|---|---|---|---|
| E1 | OneGraph 是 GitHub 开源项目 | **不是仓库，是 openkg.cn 上的数据集发布**（`openkg.cn/dataset/onegraph`），以 CSV/ZIP 下载 + 官网服务形式提供。OpenKG-ORG 组织下**无**此仓库 | 别去 GitHub 找代码；它是**数据资产**不是软件资产 |
| E2 | SkillNet 在 OpenKG-ORG 组织下 | **真实仓库是 `github.com/zjunlp/SkillNet`**（浙大 Ningyu Zhang 组主导，联合阿里/蚂蚁/腾讯/OPPO 等 19 家机构）。`OpenKG-ORG/SkillNet` 为 404 | 引用时必须给 zjunlp 路径，否则读者找不到 |
| E3 | SkillNet 规模 20 万+ 技能 | 论文口径 200,000+ 总储备 / 150,000+ 精选；平台页 139,685+ 精选；**仓库 News 2026-07-11 已更新为索引 500K+ GitHub 技能** | 二手数字已过期约 2.5 倍，引用需标时间点 |
| E4 | SkillNet 五维评估含"沙箱实跑" | **Python SDK 的 `evaluate` 明确不执行脚本**（README 原文：*reviews the skill's instructions and supporting files without executing its scripts*）。论文/平台的 Executability 才走自动化执行框架 | 这是**静态审查与动态测试的口径差**，直接决定能否把它当安全闸门 |
| E5 | OneEval 覆盖"5 种知识库 × 5 个领域" | 5 类知识库（文本/表格/知识图谱/代码/逻辑）成立；但**领域集合随版本变过**——V1.0 为通用/医疗/政务/科学/法律/编程**六**领域，V1.3 收敛为通用/税务/经济/法律/学术**五**领域 | 引用领域清单时必须带版本号 |
| E6 | OpenKG-ORG 是 OpenKG 主组织 | 该组织下现存 15 个仓库**多为 2021-2024 年的老项目**（DeepKE、gStore、OpenEA、NeuralKG、EasyEdit 等）。**2025-2026 的新主力（KAG/OpenSPG 在 `OpenSPG` 组织，SkillNet 在 `zjunlp`）都不在此** | 按组织名找新项目会漏掉主力 |

---

## 二、OneGraph（数据层）

### 2.1 定位

OneGraph 是 OpenKG **SIGData** 兴趣组发起的开放知识图谱，**不是软件项目而是数据资产 + 服务框架**。其设计目标明确写为"利用大模型构建的 **LLM 需要的**开放知识图谱"——注意这个定语：它服务的消费者是 LLM，不是人类读者或传统 KG 应用。

### 2.2 关键数字（一手，来自 openkg.cn 数据集页）

| 版本 | 发布时间 | 三元组 | 实体 | 关系 | 准确率 | LLM 生成占比 |
|---|---|---|---|---|---|---|
| V1 | 2024-10-23 | 25,407,912 | 12,051,753 | 15,410 | 0.80 | 32.28% |
| V2 | 2025-12-01 | 37,664,025 | 13,569,677 | 42,591 | 0.86（节点稠密度 2.78） | 59.70% |

覆盖六大学科领域：自然科学、工程技术、医药卫生、农学、社会科学、人文学科（V2 中人文学科约 2500 万、工程技术约 450 万）。

### 2.3 实现方案：四层数据设计

自顶向下数量递增，四类图**互相连接**构成完整 OneGraph：

1. **cnSchema** —— 中文 schema 参考标准（OpenKG 独立项目，187 forks）
2. **概念图** —— 建模概念知识之间的关系（V1 主要发布的就是概念图）
3. **实体图** —— 建模不同领域中实体之间的复杂关系
4. **文本图** —— 建模文档中章节、段落、句子之间的复杂关系

**构建方式的关键选择**：OneGraph **没有采用人工标注或自动抽取**，而是用 LLM 的参数化知识生成，且**用多个大模型交叉验证**（官方称交叉验证后准确率显著提升）。这解释了 V1→V2 的 LLM 生成占比从 32.28% 升到 59.70%、而准确率反而从 0.80 升到 0.86——这是"合成数据结构溢价"论点的核心证据。

### 2.4 服务矩阵 OneGraph-E/R/G/T

四类图谱增强服务，官方称已开放 **R（检索）与 G（生成）** 的工具：

- **OneGraph-E（抽取）**：根据输入文本进行三元组抽取
- **OneGraph-R（检索）**：按文本相似度检索最相似 n 个实体，抽取 k 跳子图返回
- **OneGraph-G（生成增强）**：Enrich-on-Graph（EoG）机制
- **OneGraph-T（思考）**：图谱增强的思考

### 2.5 实测成效与边界

- **CEval**：OneGraph V2 将 DeepSeek 等主流模型准确率从 **80.7% 提升至 86.1%**
- **结构稀疏性对参数不确定性的对冲**：经图谱指令微调的 **Llama3 8B 在规划能力上超过 GPT-3.5（175B）**
- **边界**：准确率 0.86 意味着**约 14% 的三元组是错的**；且 59.70% 由 LLM 生成——这是一个"用模型纠错模型"的自举循环，误差是否相关（类似 47 号档 Rubric Judges 发现的 96% 相关性）**官方未讨论**

---

## 三、OpenSPG（语义底座 / 动态本体）

### 3.1 定位

`github.com/OpenSPG/openspg`，**Java**，Apache-2.0，2.3k stars。蚂蚁集团与 OpenKG 联合开发，是蚂蚁在金融场景构建多元化领域知识图谱经验的沉淀。**在生态中承担类似 Palantir Dynamic Ontology（动态本体）的角色**——这是官方年度回顾的原话定位。

### 3.2 核心设计：SPG 为什么不是 RDF 也不是 LPG

SPG（Semantic-enhanced Programmable Graph，语义增强可编程图）的核心主张是**折中**：

> 创造性地融合 **LPG 结构性** 与 **RDF 语义性**，克服 RDF/OWL 语义复杂度无法工业化落地的难题，同时完整继承 LPG 结构简单、兼容大数据体系的优势。

这句话是理解整个 OpenSPG 的钥匙。它承认了语义网在工业界的失败（OWL 太重），但拒绝放弃语义表达——于是选择了"**够用的语义 + 工业可落地的结构**"。

框架从三方面定义知识语义：**主体模型、演化模型、谓词模型**。

### 3.3 四大核心能力（一手 README）

| 能力 | 内容 |
|---|---|
| **SPG-Schema** 语义建模 | 对属性图做语义增强的 schema 框架：主体模型、演化模型、谓词模型 |
| **SPG-Builder** 知识构建 | 结构化 + 非结构化双路构建；兼容大数据架构；提供实体链接、概念标准化、实体归一化算子 SDK，结合 NLP 与深度学习提升同类型内实例的唯一性水平；支持领域图谱**持续迭代演化** |
| **SPG-Reasoner** 逻辑规则推理 | 抽象 **KGDSL**（Knowledge Graph Domain Specific Language）提供逻辑规则的可编程符号表示；支持规则推理、神经/符号融合学习、**KG2Prompt** 链接 LLM 的知识抽取/推理；通过谓词语义与逻辑规则定义知识间的依赖与传递 |
| **KNext** 可编程框架 | 可扩展、过程化、用户友好的组件集；把 KG 核心能力凝固为组件化与引擎内置能力；**实现引擎与业务逻辑、领域模型的隔离** |

另有 **Cloudext 云适配层**：业务系统对接开放 SDK 自建前端；可扩展/适配定制化图存储与图计算引擎；可适配自有机器学习框架。

### 3.4 使用场景

官方示例三个：**企业供应链知识图谱、风险挖掘知识图谱、医疗知识图谱**。工程定位上它适合"有明确领域模型、需要逻辑规则推理、数据量大到要接 Spark/Hadoop"的场景。

### 3.5 边界（诚实标注）

- 依赖 Java 运行时 + 大数据生态（Spark/Hadoop），**小规模团队部署复杂度高**
- 学习曲线来自 SPG 自身的建模概念（主体/演化/谓词三模型是特有词汇）
- 开源侧文档与案例仍以金融为主，金融外场景案例有限
- **注意**：OpenSPG 提供了 schema 与逻辑规则，但**没有本体层的自动推理**（如由"区属于市、市属于省"推"区属于省"）——这一点与 KAG 共通，是刻意的工程取舍

配套论文：**《SPG 白皮书》（蚂蚁 + OpenKG）**、**KGFabric: A Scalable Knowledge Graph Warehouse for Enterprise Data Interconnection**

---

## 四、KAG（推理层）

### 4.1 定位与规模

`github.com/OpenSPG/KAG`，**Python**，Apache-2.0，**9.1k stars / 720 forks / 1,261 commits**，最新提交 2026-01-28。论文 **arXiv:2409.13731**（2024-09-10 提交，v3 2024-09-26，33 页）。

一句话定位（官方）：*a logical form-guided reasoning and retrieval framework based on OpenSPG engine and LLMs*，用于构建**专业领域知识库**的逻辑推理与事实问答。

### 4.2 它要解决的短板（值得单独记）

RAG 在专业场景的两个短板，KAG 各给一剂药：

1. **检索环节**：向量"相似"代替不了逻辑"相关"。两个表现——① 把不该混的混在一起（政策修订前后条款在向量空间挨得近，被当同义一起喂给模型）；② **硬条件表达不了**（数值比较、时间先后、用药禁忌这类 expert rules 在向量计算里没有对应物）
2. **构建环节**：GraphRAG 式的 OpenIE 自动抽取引入噪声，抽错的实体与张冠李戴的关系一旦进图，会在回答中被放大

### 4.3 实现方案：五大增强（论文原文五点）

1. **LLM-friendly knowledge representation** —— LLM 友好的知识表示
2. **Mutual-indexing between KG and original chunks** —— 图谱与原文块**互索引**：每个节点可指回原始文档段落，回答时同时利用图结构与原文上下文，人工核查点节点即跳回原文
3. **Logical-form-guided hybrid reasoning engine** —— 逻辑形式引导的混合推理引擎
4. **Knowledge alignment with semantic reasoning** —— 语义推理对齐（对应"归类检查"纠错：抽出的"阿莫西林"应归"抗生素"，若错挂"解热镇痛药"则按概念语义纠正）
5. **Model capability enhancement for KAG** —— KAG 模型能力增强

### 4.4 三段式架构与求解闭环

- **kg-builder**（构建）/ **kg-solver**（求解）/ **kag-model**（模型）——**注意：kag-model 未开源**，开源的只有 builder 与 solver
- **KAG-Solver 闭环**：**Planner（规划）→ Executor（执行）→ Generator（生成）**。Planner 把自然语言问题拆成符号化步骤单（Logical Form）；Executor 动态调用**逻辑演绎器、代码计算器、或基于 OpenSPG 的检索器**；最后才由 LLM 组织自然语言答案并附依据
- **KAG Index Diffusion**：把本体知识、实体、关系、时空知识与原文段落**映射到统一语义空间**，官方称解决传统向量检索在**指代 / 时空 / 数值 / 逻辑**四个维度上的错位
- 存储形态：图谱主体存图数据库（TuGraph / Neo4j），原文文本块另存向量索引（Elasticsearch）辅助语义召回——**KAG 的"知识库"= 图库里的图谱 + 向量库里的文本索引 + 两者之间的互索引**

### 4.5 版本演进与关键数字

| 版本 | 时间 | 关键变化 |
|---|---|---|
| v0.6 | 2025-01 | 大重构：schema_free_extractor / schema_constraint_extractor、ZODB checkpointer |
| v0.7 | 2025-04 | **轻量级构建模式，官方称知识构建 token 成本降低 89%** |
| v0.8 | 2025-06-30 | **MCP server（PR #594）+ solver 侧 MCP 支持（#444）**，可把网络搜索、LBS 等公网数据源接入推理；支持**自由定制 Builder 流水线**；迭代式求解或 DAG 引导求解 |

- **基准**：2WikiMultihopQA **F1 相对提升 19.6%**、HotpotQA **F1 相对提升 33.5%**（论文口径）
- **落地**：蚂蚁集团内部 **E-Government Q&A（政务）** 与 **E-Health Q&A（医疗）** 两个场景
- **安装**：产品模式 docker-compose 起服务，浏览器 **127.0.0.1:8887**（默认账号 openspg）；开发者模式 pip 安装工具包

### 4.6 什么时候该用（官方与业界的三条判据）

1. **领域规定是否成文** —— 政策/医疗/金融合规等白纸黑字的领域，schema 有依据；规定模糊或快速变化的领域，定义成本会吃掉收益
2. **问题是否需要多步推理** —— "A 政策与 B 政策对同一情形规定是否冲突"这类是步骤单的用武之地；单点事实查询普通 RAG 就够
3. **答错的代价** —— 政务答复、用药建议错一次的代价足以覆盖 schema 设计投入

### 4.7 KAG-Thinker：把推理从框架层内化到模型层

`github.com/OpenSPG/KAG-Thinker`（Python，90 stars，末次更新 2025-11）。核心思路：**KAG-Solver 提供可解释、可调试的框架化推理，KAG-Thinker 则把这种推理能力蒸馏进模型参数**——官方称为"外部脚手架训练 → 内化为原生能力 → 反哺框架优化"的螺旋上升。

实现上：融合自然语言推理与符号表达式（Logical Form）推理，通过引入**变量、运算器与运算规则**赋予模型简明逻辑约束；用**交互式迭代合成高质量 SFT 语料**让模型学习思考范式。推理侧用 FlashRAG 建检索服务 + **vLLM** 部署模型（README 指定 `vllm==0.6.6`、`faiss-gpu=1.8.0`）。

模型会**根据自己掌握知识的程度决定是否调用外部检索**——这是把"要不要检索"这个决策也内化了。

### 4.8 边界

- **kag-model 未开源**，开源部分是 builder 与 solver，模型能力增强这一环实际拿不到
- 系统复杂度显著上升：**步骤单翻译错了整链条都会偏**
- 每个领域都要配 schema 与推理规则，**通用性是用前期投入换的**
- 仓库已近 8 个月无实质更新（末次提交 2026-01-28，且是 MCP 的 bugfix），**活跃度是风险项**

---

## 五、SkillNet（Agent 层）

### 5.1 定位与归属（勘误重点）

`github.com/zjunlp/SkillNet`，**MIT**，Python 3.10+，`pip install skillnet-ai`。论文 **arXiv:2603.04448**（2026-02-26），浙大 + 同济 + 东南 + 阿里 + 蚂蚁 + 腾讯 + OPPO 等 19 家机构联合。

一句话定位（README 原文）：*Open infrastructure for discovering, evaluating, analyzing, and routing reusable AI agent skills.*

### 5.2 核心论点：从 Know-What 到 Know-How

论文的框架很有说服力——知识表示的三个阶段都只描述了"世界是什么"，**缺少对"怎么做"（Know-How，过程性知识）的有效表示**：

| 阶段 | 形态 | 优点 | 缺陷 |
|---|---|---|---|
| 文本知识 | 非结构化文本 | 覆盖面广 | 语义模糊，语义鸿沟 |
| 符号知识 | 知识图谱 / 描述逻辑 | 语义精确，支持严谨推理 | **获取成本高，难以规模化** |
| 向量化知识 | LLM 高维空间 | 泛化强、调用方便 | **缺乏可解释性、过程不可控、事实一致性脆弱** |

Skill 则被定义为"**过程性知识单元**：封装特定意图、可参数化调用、产生确定性输出"，包含四要素：**适用场景**（前置条件/约束/失败场景）、**工具与接口**、**推理结构**（已验证的决策链与操作序列）、**元认知信息**（成功率、资源消耗、执行时延）。

### 5.3 实现方案：三层技能本体

| 层 | 名称 | 内容 |
|---|---|---|
| L1 | **Skill Taxonomy** 分类层 | 通过 `category` 与 `tag` 两类关系组织为多层级结构：宏观领域（Development / AIGC / Science / Research / Business / Productivity / Security / Testing / Lifestyle）→ 具体标签（frontend / llm / physics） |
| L2 | **Skill Relation Graph** 关系层 | 把抽象标签实例化为具体技能实体（如 Matplotlib、Playwright），用四种关系边定义交互逻辑：**similar_to / compose_with / belong_to / depend_on** |
| L3 | **Skill Package Library** 包层 | 单个技能通过 `packaged_in` 封装为技能包（如 data-science-visualization），支持模块化发布、复用与部署 |

关键设计：**Skill Ontology 是动态演化的**——新标签可从分类体系不断扩展，LLM 基于标签推断潜在关系，逐步实例化并完善技能关系图。

### 5.4 五维评估与 CLI/API

**五维**：Safety（安全）/ Completeness（完备）/ Executability（可执行）/ Maintainability（可维护）/ Cost-awareness（成本意识）。

每维返回 `{level: Good|Average|Poor, reason}`。⚠️ **重要口径差（E4）**：Python SDK 的 `evaluate` **不执行技能脚本**，只审查指令与支撑文件；而论文与官方平台描述的 Executability 是通过**自动化执行框架实跑**检测运行成功率与输出正确性。引用时必须区分。

**CLI 七个命令**：`search` / `download` / `create` / `evaluate` / `analyze` / `route` / `ui`（外加 `validate` 做本地结构检查、不调模型）。

**REST API 公开免鉴权**（这是很友好的一点）：

```
GET http://api-skillnet.openkg.cn/v1/search?q=pdf&sort_by=stars&limit=5
GET http://api-skillnet.openkg.cn/v1/search?q=reading%20charts&mode=vector&threshold=0.8
```

参数：`q`（必填）、`mode`（keyword|vector，默认 keyword）、`category`、`limit`（默认 10，最大 50）、`page`、`min_stars`、`sort_by`（stars|recent）、`sort_order`、`threshold`（vector 模式，默认 0.8）。

**技能来源四路**：execution traces（执行轨迹）/ GitHub 项目 / Office 文档（PDF、PPT、Word）/ 自然语言 prompt。

### 5.5 规模演进与效果数字

| 时间点 | 规模 |
|---|---|
| 论文（2026-02） | 200,000+ 总储备 / 150,000+ curated |
| 平台页（2026-02） | 139,685+ 精选高可靠技能 |
| **仓库 News（2026-07-11）** | **500K+ GitHub 技能索引**，改进去重 + 扩展科研与数据分析覆盖，新增本地场景图与编排 |

**效果**：ALFWorld / WebShop / ScienceWorld 三个基准上，跨 o4-mini 到 Gemini 2.5 Pro 多个骨干模型，**平均奖励 +40%、执行步数 −30%**；HF 侧表述为 10-30 个百分点提升。

**生态集成**：OpenClaw 内置（2026-02-23）、JiuwenClaw 技能市场（2026-03-26）、**MCP server（2026-03-12，由 CycleChain 维护）**、skillnet-ai 0.1.2 本地浏览器 UI（2026-09-24，`skillnet ui`）、SkillNet-Gym（可执行基准）与 SkillNet-Fabric（Wiki 路由，2026-08-20）。

### 5.6 边界

- **Python 生态**（Python 3.10+，pip 包），对非 Python 运行时不友好
- `create` / `evaluate` / `analyze` 需 OpenAI 兼容端点（默认 `gpt-4o`），`route` 需 Claude 或 Codex Agent SDK——**有外部模型依赖**
- 技能来自外部仓库，**各自保留自身许可**（MIT 是工具许可，不等于技能许可）
- 规模数字随时间快速膨胀，**引用务必带时间点**

---

## 六、OneEval（评测层）

### 6.1 定位

`oneeval.openkg.cn`，2025-04 发布 V1.0，经 4 次迭代至 **V1.3**。评测对象是 **LLM 在知识密集型任务上的推理与应用能力**。

### 6.2 评测矩阵（注意版本号差异，E5）

**5 类知识库**（V1.0 与 V1.3 一致）：

| 类型 | 考查点 |
|---|---|
| 文本 | 非结构化文本知识的理解与推理 |
| 表格 | 结构化表格的数值、分类与层级信息处理、比较与逻辑计算 |
| 知识图谱 | 实体-关系三元组上的多跳推理、实体对齐、关系识别 |
| 代码 | 函数文档、源代码、API 说明；代码补全与 NL→Code 生成 |
| 逻辑 | 逻辑推理能力 |

**领域集合随版本变过**：V1.0 六大领域（通用 / 医疗 / 政务 / 科学 / 法律 / 编程），V1.3 收敛为五大领域（**通用 / 税务 / 经济 / 法律 / 学术**）。编程来源为 GitHub 海量开源代码库，跨 **300+ 依赖库与 2000+ API 版本**。

### 6.3 指标

- **Accuracy** —— 分类任务
- **F1** —— 抽取与生成任务（平衡精确率与召回率）
- **ISM@1**（Identifier Sequence Match）—— 代码生成任务
- **Overall Score = 该模型在每个评测数据集得分的平均值**（官方明文定义，便于均衡衡量综合表现）

### 6.4 榜单揭示的两件事（最有价值的部分）

1. **第一梯队集体未达及格线**：排名第一的 **Claude 4.5-sonnet-thinking 仅 37.65 分**，Gemini 3-pro 37.02、DeepSeek-V3.2 32.60。**知识密集型推理对当前最强模型仍是未解问题**——这条对"要不要上知识增强"是最有力的论据
2. **模型偏科显著**：DeepSeek-V3.2-thinking 擅长代码与 KG 推理但表格推理弱；Gemini 3-pro 在逻辑推理与税务领域领先；GPT-5.2-Thinking 综合排名跌出前五

### 6.5 边界

- 榜单数字引自 OpenKG 年度回顾的转述，**具体榜单页为动态渲染，需以 oneeval.openkg.cn 实时榜单为准**
- 未找到独立开源仓库（评测集与榜单以网站形式发布），**复现性受限**

---

## 七、配套工具：抽取与 MCP 封装

### 7.1 OneKE：schema 引导的知识抽取

`github.com/OpenSPG/OneKE`，**MIT**，被 **WWW 2025 Demonstration Track** 收录。Docker 化的 schema-guided 抽取系统，**多智能体协作**架构。

**任务覆盖**：NER / RE / EE / Triple（三元组，可快速构建知识图谱）/ Open Domain IE（Web 新闻、书籍知识、社交媒体、研究论文）。

**三种 Agent 可自由组合**（这是它最有工程参考价值的设计）：

| Agent | 可选模式 |
|---|---|
| **Schema Agent** | `default`（默认 JSON 格式）/ `predefined`（从知识库检索预定义 schema）/ `self-deduced`（从任务描述与源文本推断生成 schema） |
| **Extraction Agent** | `direct`（直接抽取）/ `case retrieval`（从知识库检索相似优秀案例辅助） |
| **Reflection Agent** | `no reflection`（直接返回）/ `case reflection`（自一致性检查，不一致时检索相似失败案例反思） |

通过 `src/config.yaml` 的 `customized` 段配置三元组，外部 YAML 设 `mode: customized` 即生效。

**模型支持**：API（OpenAI、DeepSeek）+ 本地（LLaMA3-Instruct、Qwen2.5-Instruct、ChatGLM4-9B、MiniCPM3-4B、OneKE 自研、DeepSeek-R1 系列——推荐 vLLM 部署）。

**工程提示**（README 原话）：长文本推荐 `direct mode` 避免注意力分散与处理时间增加；短任务求高精度可用 `standard mode`。

### 7.2 OpenKG-ToolAgent：KG 工具的 MCP 化

把 KG 工具封装为 MCP 服务，让 Agent 调度。已封装三类：

- **DeepKE**（知识抽取）：NER / RE / AE / EE，支持预测阶段独立调用
- **muKG**（知识表示学习）：实体对齐 EA / 链路预测 LP / 实体类型识别 ET，支持训练与预测
- **Medical_Guideline_Extract**：医疗指南文本结构化抽取（垂直领域扩展示例）

其倡导的转变是**从"工具调用"到"任务自动化"**：用户只提目标级需求（"从这些文本中抽取实体关系"），由 Agent 把目标视为多阶段、带依赖的任务自动规划与执行，而不是让用户显式指定 NER → RE → 格式转换。

**KAG 侧也有 MCP 实现**：`feat(mcp): implement mcp server for kag`（PR #594）与 solver 侧 `feat(solver): add mcp support`（#444，支持从 pipeline 到 planner、executor，JSON 传入多个 `mcp_server`，已调通 `baidu_map_mcp`）。

---

## 八、使用场景矩阵

| 场景 | 首选 | 组合建议 | 关键判据 |
|---|---|---|---|
| 领域规定成文、答错代价高的专业问答（政策/医疗/合规） | **KAG + OpenSPG** | schema 先行 → builder 建库 → solver 求解 | 三条判据全中才值；只中一条先做轻量方案 |
| 开放域事实补全、给 LLM 补背景知识 | **OneGraph-R/G** | 直接调已开放的检索与生成增强服务 | 需要中文开放域知识；注意 14% 错误率 |
| 大规模领域图谱 + 逻辑规则推理（风控、供应链） | **OpenSPG** | 接 Spark/Hadoop，KGDSL 写规则 | 已有大数据基础设施才划算 |
| Agent 能力复用与积累 | **SkillNet** | 免鉴权 REST API 直接检索；本地用 CLI `analyze`/`route` | 需要过程性知识（Know-How）而非陈述性知识 |
| 知识增强到底有没有用的量化验证 | **OneEval** | 自建业务问答集 + 对照 OneEval 口径 | **先用它建立基线，再上重方案** |
| 从文档/网页/书籍批量抽取结构化知识 | **OneKE** | 长文本 direct mode；短文本高精度 standard mode | schema 可预定义则优先 predefined |
| 让 Agent 自己编排 KG 流水线 | **OpenKG-ToolAgent / KAG MCP** | 把能力封成 MCP 工具供 Agent 调度 | 面向"目标级需求"而非"步骤级指令" |

---

## 九、对智能体 + 本体平台的借鉴（三档）

以下按「可直接抄 / 需改造 / 不适用」分档，均为**纯架构决策**，与模型能力无关。

### 9.1 可直接抄（★）

- **互索引（mutual-indexing）作为强制约束**：任何进图的三元组/节点**必须挂回原文出处**。成本极低（一个字段），收益是"答案可溯、错误可定位、人工核查不用翻库"。这是 KAG 五项增强里唯一几乎没有代价的一项。
- **免鉴权公开检索 API 的产品形态**：SkillNet 的 `/v1/search` 免鉴权、keyword + vector 双模式、`limit` 上限 50——这是一份可以照抄的接口设计（含 `sort_by` / `min_stars` / `threshold` 的默认值约定）。
- **评测先行的优先级**：OneEval 榜单显示最强模型在知识密集型推理上仅 37.65 分。这提示**先建业务侧评测集与基线，再决定要不要上重方案**——与本项目 35 号档"要不要上图先测再上"的结论同向，且 OneEval 提供了现成口径可对齐。
- **技能/知识条目自带元认知信息**：SkillNet 的 skill 四要素里"元认知信息（成功率、资源消耗、执行时延）"值得迁移到任何可复用资产上——**让资产自带"上次用得怎么样"的记录**，比事后统计有用得多。

### 9.2 需改造（★★）

- **分类体系 + 关系图 + 包三层结构**：SkillNet 的 Taxonomy / Relation Graph / Package Library 三层，以及 `similar_to / compose_with / belong_to / depend_on` 四种关系边，是"可复用资产库"的通用骨架。**但要注意它的关系是靠 LLM 从标签推断的**——若资产库规模小或标签质量差，这层会退化成噪音。改造建议：先建 Taxonomy 与 Package 两层（人工可控），关系层等资产量上来再引入自动推断。
- **schema 约束的知识构建 + 归类纠错**：KAG 的"先定结构再入库 + 语义推理对齐纠错"是压住 OpenIE 噪声的有效组合。但**完整照搬需要领域专家先定 schema**——对通用平台不现实。改造方向：把 schema 从"前置必需"改成"**后置可选约束**"（先抽，再用已有本体做归类校验与冲突标记），这与本项目 35 号档 KB-6③「本体约束抽取」方向一致。
- **逻辑形式求解器的定位**：KAG-Solver 把自然语言转成符号步骤单再分步执行，收益是**每一步可核验可回溯**。但对通用平台代价太大（步骤单翻译错了整链条都偏）。改造建议：**只在"多跳 + 成文规则"的窄场景启用**，不作为默认检索路径。
- **KG 能力 MCP 化**：OpenKG-ToolAgent 与 KAG MCP 都指向同一方向——把 KG 能力封成 MCP 工具，让 Agent 以目标级需求驱动。这是本项目已定的 KB 能力分发方向可对齐的形态。

### 9.3 不适用（✕）

- **OpenSPG 的 Java + Spark/Hadoop 技术栈**：与"单机可部署、零额外运行时"的定位直接冲突。可借鉴其 **schema 三模型（主体/演化/谓词）与 KGDSL 的设计思路**，但引擎本身不宜引入。
- **KAG / OneKE / SkillNet 全系**：三者均为 **Python 生态**（KAG pip、OneKE 本地模型推理、SkillNet `pip install skillnet-ai` + Python 3.10+）。若平台对额外语言运行时有硬约束，**只能借鉴设计、以独立进程或 API 形式对接，不能直接入依赖**。
- **OneGraph 数据本体**：37.66M 三元组、准确率 0.86、59.70% 由 LLM 生成——**作为训练/增强语料可考虑，作为事实底座不可**（14% 的错误率在专业场景不可接受，且误差可能相关）。
- **KAG-Thinker 的模型内化路线**：依赖 vLLM + FlashRAG + 特定 SFT 语料合成，属于模型层工作，与平台层无关。

### 9.4 一条战略提示

OpenKG 这套生态最有说服力的**不是任何一个组件的效果数字，而是它把"知识增强"变成了一个可闭环验证的工程问题**：数据层提供事实锚点、语义层提供结构约束、推理层提供可追溯路径、Agent 层提供能力复用、评测层回答"到底有没有用"。

**对平台的启示**：若要引入知识增强，**评测层的优先级应高于数据层与推理层**——先用 OneEval 式口径建立基线与对照开关（能关掉、能 A/B），再决定投入规模。这与 46 号档"借鉴时要留对照开关"、35 号档"先测再上"是同一条纪律。

---

## 十、风险与边界清单

| # | 风险 | 说明 |
|---|---|---|
| R1 | **活跃度分层严重** | KAG 末次实质提交 2026-01-28（近 8 个月）；KAG-Thinker 2025-11；OpenSPG 主仓 2025-05。而 SkillNet 至 2026-09 仍在高频更新。**生态重心正从"图谱推理"移向"Agent 技能"** |
| R2 | **技术栈与运行时冲突** | 除 OpenSPG（Java）外均为 Python。对有运行时硬约束的平台只能借鉴设计 |
| R3 | **数字时效性** | SkillNet 规模 200K→500K（5 个月内 2.5 倍）；OneEval 领域集合跨版本变动。**所有数字引用必须带时间点** |
| R4 | **自举循环误差相关性** | OneGraph 59.70% 由 LLM 生成、用多模型交叉验证；SkillNet 的评估用 GPT-5o-mini 判定安全与完备。**两处都是"用模型审模型"，误差是否相关官方均未讨论** |
| R5 | **开源不完整** | KAG 的 kag-model 未开源；OneEval 无独立仓库（网站发布，复现性受限）；OneGraph 无仓库 |
| R6 | **schema 前置的人力成本** | KAG 最贵的投入是领域 schema 设计，是人力活、省不掉。官方亦承认"公开基准成绩替代不了业务问答集" |
| R7 | **静态评估 ≠ 安全闸门** | SkillNet SDK 的 evaluate 不执行脚本，不能当作"这个技能安全"的证明 |

---

## 十一、资料与开源项目汇总

### 表 1 · 核心项目（OpenKG 生态）

| 项目 | 层 | 定位 | 语言·技术栈 | 许可 / 成熟度 | 关键数字 | 使用场景 | 借鉴度 |
|---|---|---|---|---|---|---|---|
| **OneGraph** | 数据 | 千万级双语开放知识图谱 + E/R/G/T 服务 | 数据集（CSV/ZIP），非代码仓库 | openkg.cn 发布 / V2 已发布 | V2：37,664,025 三元组 / 13,569,677 实体 / 42,591 关系 / 准确率 0.86 / 稠密度 2.78 / 59.70% LLM 生成；CEval 80.7%→86.1% | 开放域事实补全、LLM 背景知识增强、图谱指令微调语料 | ★★（数据资产，非软件；错误率 14%） |
| **OpenSPG** | 语义底座 | SPG 语义增强可编程图引擎（动态本体） | Java，接 Spark/Hadoop | Apache-2.0 / 2.3k stars，末更 2025-05 | 三能力 SPG-Schema/Builder/Reasoner + KNext + Cloudext；KGDSL 逻辑规则 | 大规模领域图谱、风控规则推理、供应链/医疗图谱 | ★★★（**仅借鉴 schema 三模型与 KGDSL 设计**） |
| **KAG** | 推理 | 逻辑形式引导的混合推理与检索框架 | Python，图库(TuGraph/Neo4j)+向量(ES) | Apache-2.0 / 9.1k stars / v0.8.0，末更 2026-01 | 2Wiki F1 +19.6%、HotpotQA F1 +33.5%；v0.7 轻量构建 token −89%；v0.8 支持 MCP | 规定成文领域的多跳专业问答（政务/医疗/合规） | ★★★（互索引★；求解器需窄场景改造） |
| **KAG-Thinker** | 推理模型 | 交互式思考与深度推理模型，推理能力内化 | Python + vLLM 0.6.6 + FlashRAG + faiss-gpu | Apache-2.0 / 90 stars，末更 2025-11 | 变量+运算器+运算规则；交互式迭代合成 SFT 语料 | 复杂多跳问题的认知推理范式 | ★（模型层，平台无关） |
| **SkillNet** | Agent | 技能发现/安装/创建/评估/分析/路由基础设施 | Python 3.10+，pip `skillnet-ai` | **MIT** / 高频更新至 2026-09 | 论文 200K+ 总 / 150K+ curated；**2026-07 索引 500K+**；ALFWorld/WebShop/ScienceWorld 奖励 +40%、步数 −30% | Agent 过程性知识复用、技能库治理与路由 | ★★★（三层本体 + 免鉴权 API 形态可抄） |
| **OneEval** | 评测 | 知识密集型推理评测框架与榜单 | 网站发布（无独立仓库） | oneeval.openkg.cn / V1.0→V1.3 | 5 类知识库 × 5 领域；Accuracy/F1/ISM@1；Overall=均值；榜首 Claude 4.5-sonnet-thinking **37.65** | 知识增强效果量化、模型选型对照 | ★★★（**评测口径可对齐，优先级最高**） |
| **OneKE** | 抽取 | schema 引导的多智能体知识抽取 | Python，Docker 化，多模型 | **MIT** / WWW 2025 Demo | NER/RE/EE/Triple/OpenIE；Schema+Extraction+Reflection 三 Agent 可组合 | 从文档/网页/书籍批量结构化抽取 | ★★（三 Agent 组合设计可抄；Python 栈受限） |
| **OpenKG-ToolAgent** | 编排 | KG 工具 MCP 封装 + Agent 任务自动化 | MCP 服务 | 公开服务 | 封装 DeepKE(NER/RE/AE/EE)、muKG(EA/LP/ET)、医疗指南抽取 | 目标级需求驱动的 KG 流水线自动编排 | ★★★（MCP 化形态可对齐） |

### 表 2 · 生态配套与相邻项目

| 项目 | 定位 | 语言·技术栈 | 成熟度 | 与本生态关系 | 借鉴点 | 风险与边界 |
|---|---|---|---|---|---|---|
| **cnSchema**（OpenKG-ORG） | 开放中文知识图谱 schema 参考标准 | — | 187 forks / 15 stars，末更 2022 | OneGraph 四层之顶层 | 中文 schema 参考标准可直接复用 | 多年未更新 |
| **DeepKE**（OpenKG-ORG） | 知识图谱抽取与构建工具包 | Python | MIT / EMNLP 2022 Demo / 750 forks | 被 OpenKG-ToolAgent 封装为 MCP 工具 | 经典 IE 任务实现参考 | 组织内老项目，2023 后无更新 |
| **muKG** | 多源知识图谱表示学习 | Python | 开源 | ToolAgent 封装（EA/LP/ET） | 实体对齐与链路预测 | 需训练，成本高 |
| **gStore**（OpenKG-ORG） | 基于图的 RDF 三元组存储 | C++ | BSD-3 / 213 forks | 独立图存储，与 OpenSPG 可插拔层同类 | RDF 存储自研参考 | 2023 后无更新 |
| **OpenEA** | 基于 KG 嵌入的实体融合工具 | Python | GPL-3（**商用需注意**） | OpenKG 老牌工具 | 实体对齐算法参考 | **GPL-3 许可，与商业闭源冲突** |
| **NeuralKG** | KG 表示学习库 | Python | Apache-2.0 | 同上 | 表示学习算法集合 | 2023 后无更新 |
| **EasyEdit / EasyDetect / EasyInstruct** | LLM 知识编辑 / 幻觉检测 / 指令处理 | Python (Jupyter) | MIT / Apache-2.0 | OpenKG 面向 LLM 的工具组 | 幻觉检测与知识编辑思路 | 2024 后停更 |
| **KGFabric**（论文） | 企业级数据互联的可扩展 KG 仓库 | — | OpenSPG 引用论文 | OpenSPG 的企业级形态论文 | 企业 KG 架构参考 | 论文形态，无独立仓库 |
| **《SPG 白皮书》** | SPG 框架官方定义 | — | 蚂蚁 + OpenKG 联合发布 | OpenSPG 的理论基础 | **理解 SPG 必读** | 需先读白皮书再做工程判断 |

---

## 十二、引用注意

1. **仓库路径易错**：SkillNet 在 `zjunlp` 而非 `OpenKG-ORG`；KAG/KAG-Thinker/OneKE 在 `OpenSPG` 组织；OneGraph 与 OneEval 无 GitHub 仓库（分别是 openkg.cn 数据集页与评测网站）。
2. **所有规模数字必须带时间点**——SkillNet 5 个月内从 200K 涨到 500K。
3. **版本号敏感**：OneEval 的领域集合在 V1.0 与 V1.3 之间变化；KAG 的能力（轻量构建、MCP）分别落在 v0.7 / v0.8。
4. **区分静态评估与动态测试**：SkillNet 的 SDK `evaluate` 不执行脚本，与论文描述的沙箱实跑是两个口径。
5. 榜单数字（37.65 / 37.02 / 32.60）引自 OpenKG 年度回顾转述，**以 oneeval.openkg.cn 实时榜单为准**。

---

## 十三、延伸阅读

- OpenKG 年度回顾（2025-2026）：`https://www.53ai.com/news/knowledgegraph/2026022019635.html`
- OneGraph 数据集页：`https://openkg.cn/dataset/onegraph` · 官网 `http://onegraph.openkg.cn/`
- SkillNet 官网 `http://skillnet.openkg.cn/` · 论文 `https://arxiv.org/abs/2603.04448`
- KAG 论文 `https://arxiv.org/abs/2409.13731` · OpenSPG 文档站 `https://openspg.github.io/v2/`
- OneEval 评测规则 `http://oneeval.openkg.cn/?p=1475`

**相关本库文档**：
- [`31_知识图谱相关开源方案梳理`](31_知识图谱相关开源方案梳理.md) —— 二手资料清单视角（本档对其做一手核验，见 §1.2 勘误表）
- [`35_知识库RAG与知识图谱与本体_发展历程与口径辨析`](35_知识库RAG与知识图谱与本体_发展历程与口径辨析.md) —— 三概念口径分层
- [`36_知识库_知识图谱_本体边界分析`](36_知识库_知识图谱_本体边界分析.md) —— 三者为同栈三层次而非并列技术
- [`37_知识库与知识图谱升级方案`](37_知识库与知识图谱升级方案.md) —— 本平台已立项执行清单（KB-4~14）
