---
module: 本体
topic: 自进化本体深度研究
desc: EvoOntology（人大，arXiv:2609.15779）与 BI-Agent / BI-Bench（微软研究院 + UIUC，arXiv:2609.20886）双论文实现拆解与对本项目借鉴点；与 40 号 EvoOntology 分析互补（本篇含 BI-Agent）
synced: 2026-09-30
---

# 自进化本体（Self-Evolving Ontology）深度研究：EvoOntology 与 BI-Agent

> **✅ 立项注记（2026-09-30）**：本档 §5.1 可采用设计与 46 号 §四 OaK 质量闭环已合并立项 **REQ-207 本体自进化受控生长闭环**（03 号 §2.15，挂 M43 待排期）——一期人触发最小闭环（诊断→归因→补丁→配对门控 + Evidence 锚定 + 候选版本状态机），无人值守自动循环不做；门控机制与伴生本体（REQ-194）的共享边界待两线一期落地后评估。
>
> **研究目的**：为「智能体 Agent + 本体 Ontology 平台」项目提供设计借鉴。
>
> **研究对象**：微信文章《Graph的尽头是自进化Ontology~》（PaperAgent，2026-09-26）及文中两篇论文的完整实现——**EvoOntology**（人大，arXiv:2609.15779）与 **BI-Agent / BI-Bench**（微软研究院 + UIUC，arXiv:2609.20886）。
>
> **研究日期**：2026 年 9 月 28 日
>
> **资料来源**：论文原文（arXiv 摘要页 + HTML 正文）、两个 GitHub 仓库的 README / USAGE / 架构文档、微信拆解文章。

---

## 0. 一句话结论

这两项工作是**同一个问题的两半**：BI-Agent 证明「给 Agent 挂静态人工图」有真实红利也有硬天花板——它只给了**结构**（join 关系），给不了**理解**（哪个字段是「收入」、过滤按什么业务规则），而且 Agent 每个任务都要从零再猜一遍、猜完就扔；EvoOntology 补的正是这另一半——**一张从数据里长出来、会自己改自己的本体图**，把「重复的模式发现」在任务之间摊销掉。

对本项目最有价值的三个可迁移机制：

1. **本体即「可训练状态」而非模型权重**（EvoOntology 的核心范式，借鉴自 SkillOpt）——用「轮次预算 + 验证集 + Accept/Reject 门控」约束每一次本体改动；
2. **归因引导的类型化编辑 + 配对门控**——四步闭环里**门控贡献最大（去掉掉 11.2 分）、归因第二（去掉掉 6.3 分）**，这是全系统最该抄的两个承重件；
3. **MCP 工具化按需访问 + 极简 manifest**——整张图只有一份极简 manifest 进 prompt，其余按需取用。论文实测：**把同一份语义画成静态图塞进 prompt，在 Claude-Sonnet-5 上反而把准确率拉低 15 个百分点**。

---

## 1. 背景：为什么"挂图"这件事需要升级

### 1.1 行业现状：Graph 已成 Agent 标配，但挂的都是静态图

文章开篇的判断很准：给 Agent「挂 Graph」快成了行业信仰——text-to-SQL 挂 schema graph、BI 手工建 join 关系图、RAG 把知识图谱当外挂记忆、dbt 给数据团队推语义层。仿佛只要塞给模型一张图，它就突然看懂了数据。

但**图确实有用，天花板也是真的**。微软用 BI-Bench 把这件事测明白了（见 §2）。

### 1.2 问题的本质命名：agent–data gap

EvoOntology 把这个缺口命名为 **agent–data gap**（智能体–数据鸿沟）：

> 数据住在 Agent 外面（关系数据库、半结构化文件、非结构化文档），Agent 只能通过 SQL 接口、文件读取器这类**通用工具**摸到列名和路径。数据的结构和语义它**事先一概不知**，只能靠反复发探测查询（probing queries）「盲探」，猜概念在哪、检查可能无关的内容。

跨越这个鸿沟需要一个**中间本体层**：显式表示领域概念、把概念锚定到底层数据、让 Agent 在**语义层**而非**物理层**与数据交互。

### 1.3 两条旧路的失效（都有实测数据支撑）

| 路线 | 做法 | 失效证据 |
|---|---|---|
| **Raw querying**（原始查询） | 让 Agent 自己探索原始数据 | 小数据还行；数据源一宽一杂就陷进**重复低效的探测循环**。GPT-OSS-120B 无工具时几乎不与数据交互——SQL 下**中位 3 次**模型调用、Python 下**中位 2 次**就终止，不检查值编码、join 键或聚合列分布 |
| **Semantic Layer**（静态语义层） | 人工维护的语义层，全量注入 prompt | ①专家维护成本高、数据变了就过时；②全量塞进 prompt 不现实；③**实测反效果**：同一份语义画成死图，在 Claude-Sonnet-5 上把 Traj-Wise 准确率**拉低 15.0 个百分点**（65.6 vs 74.3 Msg-Wise / 57.5 vs 72.5 Traj-Wise） |

> **关键洞察**：**同一份语义，画成死图就是负资产。** 差别不在"内容对不对"，而在"怎么给"——静态提示片段与 Agent 其他指令竞争注意力且无法逐轮剪枝；而按需查询只取当前步骤相关的那几个节点。

---

## 2. BI-Agent / BI-Bench：静态图红利的实测与天花板

### 2.1 基准构建：第一个端到端 BI 基准

**BI-Bench** 的构建方式决定了它的说服力——全部来自**真实用户仪表盘**，不是合成查询：

| 维度 | 数据 |
|---|---|
| 数据来源 | 爬取 **3000+ 个真实 Power BI 项目**（.pbix 公开文件） |
| 提取手段 | DAX-studio + RPA 程序化提取：转换步骤、人工 join 关系、最终 dashboard |
| 人工投入 | **400+ 人时**（每条查询 >4 人时） |
| 最终规模 | **100 对**（分析查询 q，标准答案表 R） |
| 平均规模 | 每项目 **10.6 张表**（最多 52 张）、**80,464 行**（最多 7M+）、每表 10.7 列（最多 325 列）、**12.75 个 join 关系**（最多 94 个） |
| 最难题 | 单条查询需在 **38 张表**上同时推理 |

**质量保障流程**（值得本项目借鉴的基准建设方法论）：
1. 三条筛选条件：可视化意图语义清晰可改写 / 改写准确反映分析意图 / 数据与可视化均为英文；
2. **LLM 辅助验证触发人工复核**：给两个独立前沿 LLM 提供 q + 全部 hints（必要转换、ground-truth joins、相关表子集），若**10 次尝试都无法复现**期望结果表（23.1% 案例）→ 触发人工复核 → 修复 14.4% / 丢弃 5.2%；
3. **关键原则：模型能否解出不是入选标准** ——两个模型都解不出但人工确认正确的案例**原样保留**（3.5%），避免基准被模型能力过滤；
4. **标准答案增强**：为每用例准备多个语义有效的答案表（仅基于查询语义与计算逻辑，**不参考任何模型输出**），避免因 schema 轻微偏差误判。

**实测成绩（说明难度）**：

| 方法 | BI-Bench 表现 | 其原生基准表现 |
|---|---|---|
| o4-mini（SQL，最好） | **48.2%** | — |
| BIRD 榜前四 NL2SQL 模型 | **6.0% ~ 17.3%** | >70% |
| Spider 2.0-lite 榜前二 agent | **23.8% ~ 26.3%** | >70% |

### 2.2 BI-Agent 架构：三层 + 四个数据管理工具

```
顶层：Agent 框架（LLM 作为 orchestrator，可 vanilla 或后训练）
中层：工具层 = 通用编码工具 + 专用数据管理工具（search / transform / join）
底层：与数据库或 CSV 文件交互
机制：LLM 在 reasoning-and-tool-call loop 中迭代推理和调用工具
```

**四个工具的设计逻辑（每个都针对 LLM 的一个具体失效模式）**：

| 工具 | 针对的 LLM 失效 | 实现方式 |
|---|---|---|
| **Coding tool** | 基础执行能力 | 执行 LLM 生成的 Python/SQL，返回结果或错误；**跨轮次缓存程序变量**使 LLM 能迭代推进 |
| **Transform tool** | LLM 缺乏对表结构的整体理解，识别不出需要"关系化"；即使识别出也常无法正确合成重塑步骤（SQL 缺原生 reshape 算子，模拟 unpivot 需穷举 UNION ALL，极易出错） | 用数据管理文献中专用于 table-reshaping 的**预测算法**（Li et al. 2023）：检查所有输入表 → 判断是否需要 transpose/pivot/unpivot → 应用 → **同时返回原始表与转换后表** |
| **Join tool** | LLM 在复杂 schema 上频繁出错（cryptic ID 值、代理键列取值相似且范围重叠 → 假阳性）；大表迫使 prompt 只能含小部分行，**难以检测可靠的值重叠** | 用 join 预测算法（Lin et al. 2023），针对 **snowflake-like schema** 优化：在**完整表**上计算统计特征（如 **value containment 值包含度**），在**所有表上做全局推理**。输出如 `(Sales.C1, Budget.C2)` |
| **Search tool** | 在大量表上操作时注意力被分散，易被语义相关但无关的表混淆 | 聚焦"为给定 q 选择相关表"，采用**保守策略**：保留任何看似相关的表，剪除明显无关的 |

> **设计哲学**：**把"LLM 猜不准"的部分交给传统的、有统计保证的数据管理算法，把 LLM 用在它擅长的编排与代码生成上。** 这是本项目架构层最应吸收的一条。

### 2.3 工具红利：全模型普涨，弱模型涨得最猛

| 模型 | SQL 无工具 | SQL 有工具 | 增益 | Python 无工具 | Python 有工具 | 增益 |
|---|---|---|---|---|---|---|
| o4-mini | 48.2 | 61.9 | +13.7*** | 57.4 | 66.3 | +8.9* |
| GPT-5.5 | 46.7 | 60.2 | +13.5*** | 59.5 | 65.4 | +5.9* |
| GPT-4o | 27.1 | 37.7 | +10.6*** | 30.8 | 44.7 | +13.9*** |
| Kimi-K2.6 | 20.0 | 36.7 | +16.7*** | 55.6 | 62.7 | +7.1** |
| **GPT-OSS-120B** | **5.3** | **45.3** | **+40.0\*\*\*** | **3.1** | **27.3** | **+24.2\*\*\*** |
| Qwen3-8B | 5.2 | 19.8 | +14.6*** | 5.4 | 13.1 | +7.7* |

- **平均提升：SQL +14pp、Python +11pp；20 个案例中 19 个统计显著**（paired t-test, n=100, 10 次重复）
- 弱模型受益最大：GPT-OSS-120B 提升**超过 8 倍**，因为工具把它的调用行为从"中位 3 次就放弃"变成"中位 9 次，每次都以观察替代猜测"
- 统计严谨性：项目级重复验证 + **Benjamini–Hochberg 控制 FDR 至 q=0.05**

### 2.4 后训练：8B 小模型打赢大模型，且便宜 54 倍

训练数据合成（Figure 6 五步）：从真实 BI 项目构造 join graph → 采样**连通子 schema** → 物化成单个 **"wide table" W** → 让 LLM 在 W 上合成查询 q 与代码 c → 执行得 R（R≠∅ 才保留，产出 **7,985 条**）→ 混合无关表 + 逆 table-reshaping 算子制造复杂案例。

> **巧思**：通过物化 wide table，**把"搜索相关表"和"join 预测"这两个最难的问题从训练任务中消掉**，让训练聚焦于分析能力；再在推理阶段靠工具把这两个能力补回来。

防泄漏做得很扎实：项目级 + 至少 5 行表内容重叠即排除；**查询级泄漏分析 ρ=99.72%**（仅 0.28% 训练查询比测试查询的最近邻更相似）；**转换级 0.73%**。

| 模型 | SQL 准确率 | SQL 成本 |
|---|---|---|
| Qwen3-8B（基座） | 5.2 | $0.0017 |
| Qwen3-8B-SFT-Tool | 27.2（+22.0***） | $0.0031 |
| **Qwen3-8B-RL-Tool** | **35.0（+29.8\*\*\*）** | **$0.0031** |
| GPT-4o 无工具 | 27.1 | $0.0897 |
| ktx w/ Codex (GPT-5.5) | 26.3 | $0.2215 |

- **RL 相比 SFT 额外贡献显著**：SQL 下 ↑Tool +11.5**、↑Train +15.2***（组合增益，非简单相加）
- 运行整个 BI-Bench 仅需 **$0.19**，大型模型可超 $10 → **成本效益 54 倍**
- 总训练成本 **< $200**（2×H100，SFT 9h + RL 22h @ $2.89/GPU-h）
- **RL 设计**：RLVR + GRPO；奖励函数 `+1−0.1n`（正确）/ `−0.5−0.1n`（返回了结果表但错，**部分信用，提供稠密信号**）/ `−1−0.1n`（无结果）；**策略梯度仅优化工具调用块**（coding/transform/join），不模仿自然语言推理——让 RL 聚焦核心技能

### 2.5 天花板在哪：为什么静态图走不远

文章一句话点透：BI-Agent **吃到了图的红利**（join 工具针对雪花/星座结构做全局推理，比 LLM 自己猜 join 准得多），**但天花板也恰恰在这张图上**：

> 它只给了**结构**，给不了**理解**——「收入」对应哪张表哪一列、过滤该按什么业务规则，Agent 每个任务都要**从零猜起，而且猜完就扔**。图是静态的、人工的。

这正是 EvoOntology 要补的另一半。

---

## 3. EvoOntology：一张会自己改自己的本体图

### 3.1 核心范式：本体是「可训练状态」，不是模型权重

> **EvoOntology treats the Ontology Layer as trainable agent state—not model weights.**

架构文档明确写了方法论来源：**借鉴 SkillOpt**——SkillOpt 训练的是 skill 文档，EvoOntology 演化的是本体层记录。共同的做法是：**用轮次预算、验证集和 Accept/Reject 门控约束每一次改动**。

这是一个非常值得本项目直接采用的心智模型：**不训练模型，训练"知识资产"**。

### 3.2 三层结构：Content / Schema / Tool

本体状态记作 $\mathcal{L}_t = (\mathcal{S}_t, \Gamma_t, \mathcal{R}_t)$：

| 层 | 角色 | 内容 |
|---|---|---|
| **Content Layer** $\mathcal{S}_t$ | 知识本体 | 类型化语义图 |
| **Schema Layer** $\Gamma_t$ | 表示规则 | 四类节点的字段定义、允许的关系类型、允许的引用模式——**设定本体的表示边界** |
| **Tool Layer** $\mathcal{R}_t$ | 运行时访问 | 通过 MCP 暴露：`browse_semantics`、`resolve_semantics` + 会话 manifest |

**关键设计的用意**（论文原话）：三者分离了语义知识、其对象模型、运行时暴露，使**部署的 Agent 只检索当前步骤相关语义**，**进化 Agent 只更新本体状态的有界部分**。这是"局部化干预"能成立的结构前提。

#### Content Layer：4 类节点 + 2 类边

**节点家族：**

| 类型 | 含义 | 消融贡献（去掉后 Traj-Wise 下降） |
|---|---|---|
| **Terms** | 领域概念 | 无法单独屏蔽（其他家族都引用它） |
| **Mappings** | 把概念**锚定**到具体字段和 join 路径 | **−13.4（最大）** |
| **Constraints** | 约束概念的合法使用 | −3.5 |
| **Evidence** | 留存探查证据、支持语义声明 | **−8.7** |

**边家族：**

| 类型 | 含义 |
|---|---|
| **Semantic Relations** | 连接 Terms，5 种：*association*（关联）、*hierarchy*（层次）、*composition*（组合）、*equivalence*（等价）、*derivation*（派生） |
| **Structural References** | 把 Terms 链到 Mappings，把 Constraints / Evidence 附加到其约束或支持的对象上 |

> **Mappings 和 Evidence 是两个承重家族**——这验证了"要求每个提交条目锚定在**探测查询**而非仅自然语言描述"的决定。**光有概念不够，必须钉到物理字段（Mapping）+ 有实证（Evidence）。**

#### Tool Layer：只有两个工具 + 一份极简 manifest

- $f_{\mathrm{browse}}(q, k, n)$：检索查询 $q$ 和种类 $k$ 的 top-$n$ 语义匹配
- $f_{\mathrm{resolve}}(\mathcal{I}, c)$：返回请求的记录及其链接对象
- **Manifest**：会话初始化时提供紧凑的源和使用信息——**它是唯一被放入提示的本体内容**，详细记录全部按需检索

> 这就是"从**被动看图**变成**主动查图**"的实现。也是静态语义层反效果（−15pp）的解药。

### 3.3 自进化闭环：五步生命周期 + 四步循环

**生命周期（README 版）：**
```
Build  → 从 workload 推导候选概念，对原始源验证，发布 ontology_v0
Use    → Data Agent 按需查询本体，记录工具交互与结果
Evolve → 诊断反复出现的行为，归因到 Content/Tool/Schema，产出局部化 Candidate patch
Evaluate → 用相同数据、Agent、解码设置、交互预算对比 Parent 与 Candidate
Publish or reject → 通过则发布为 ontology_vN+1；否则保留 Parent，结果用于下一轮
```

**进化循环四步（核心）：**

**① 诊断（Diagnose）**：从历史轨迹 $\mathcal{T}_t$ 与当前本体 $\mathcal{L}_t$ 提取反复出现的失败签名 $\Sigma_t = \mathrm{analyze}(\mathcal{T}_t, \mathcal{L}_t)$。

**② 归因（Attribute）**：把每个签名归因到三层之一 $\alpha: \Sigma_t \rightarrow \{\mathsf{C}, \mathsf{T}, \mathsf{S}\}$，并**陈述更新的预期行为效果**。
> 消融去掉归因掉 **−6.3**：不分层归因，系统就会在 manifest 出问题时长节点、内容出问题时改图式。

**③ 局部化补丁（Patch）**：$\mathcal{L}'_t = \mathrm{patch}(\mathcal{L}_t, \sigma, \alpha(\sigma))$，**每个候选仅修改一个层级**：
- Content 干预：增/删/改实例化语义对象
- Tool 干预：修改已有工具或按观察到的行为增删工具
- Schema 干预：修订对象模型

**④ 配对门控（Gate）**：候选与父本在**同一验证集、同一解码与交互预算**下评估，改进达阈值 $\tau$ 才保留：

$$\mathcal{L}_{t+1} = \begin{cases} \mathcal{L}'_t, & \phi(\mathcal{L}'_t, \mathcal{V}; m) - \phi(\mathcal{L}_t, \mathcal{V}; m) \geq \tau \\ \mathcal{L}_t, & \text{otherwise} \end{cases}$$

> **消融去掉门控掉 11.2 分——最大承重墙**。原因：未过滤候选引入的回归下一轮无法总被撤销。
> 被拒绝的候选**不部署，但其签名、干预和评估结果被记录以防止重复无效更新**。

### 3.4 消融实验：哪些部件是承重的

**进化循环四步消融（DDR-Bench，4 backbone 平均，Full loop = 89.5）：**

| 变体 | Traj-Wise | Δ | 解读 |
|---|---|---|---|
| **Full loop** | **89.5** | — | |
| w/o Gate | 78.3 | **−11.2** | 承重墙 #1 |
| w/o Attribution | 83.2 | −6.3 | 承重墙 #2 |
| w/o Diagnose | 84.7 | −4.8 | 显著 |
| w/o Patch（自由形式重写） | 87.8 | −1.7 | 类型化编辑有价值 |

> 论文结论原话：**"更选择性而非更迭代"（more selective rather than more iterative）** 的进化循环设计。这是本项目实施时最容易做错的地方——直觉会让人不断加大迭代轮次，而正确做法是**加大门控严格度**。

**三层可编辑性消融（Baseline = 69.5）：**

| 变体 | Traj-Wise | Δ |
|---|---|---|
| Content-only evolution | 78.2 | +8.7 |
| **Tool-only evolution** | **82.7** | **+13.2（单层最大）** |
| Schema-only evolution | 73.1 | +3.6 |
| **Full three-level evolution** | **89.5** | **+20.0** |

- Tool-only 单层增益最大，与"**manifest 重塑是归因分析揭示的主导杠杆**"一致
- 三层**互补且不可替代**，没有一层能达到完整循环的增益
- Appendix C 的增益占比：**Tool 57% / Content 34% / Schema 9%**（Tool 编辑仅 6 个接受轮次却贡献 57% 增益）

**内容层对象家族消融（Full = 89.5）**：`w/o Mappings −13.4` > `w/o Evidence −8.7` > `w/o Constraints −3.5` > `w/o Relations −2.1`

### 3.5 主结果：全面涨分

**DDR-Bench（多源数据研究，10-K 场景，Traj-Wise %）：**

| Method | 4-backbone 平均 |
|---|---|
| Baseline (ReAct 无本体) | 69.5 |
| ReAct + Memory | 75.8（+6.3） |
| Baseline + SL（静态语义层） | 最差，Claude-Sonnet-5 上 **−15.0** |
| **EvoOntology** | **89.5（+20.0）** |

单 backbone 最佳：**GPT-5.6-sol 达 93.5（+25.0）**，GPT-5.5 达 **90.9（+26.7）**。

> **最有力的一组对比**：ReAct + Memory（把历史轨迹存为可检索 episodes）只拿到 +6.3，而 EvoOntology 是 +20.0。
> 论文原话：**"情景记忆只会重播做过的事，图才是可组合的结构化沉淀"**（episode memory only replays what has been done; the graph is a composable, structured accumulation）。

**BIRD（text-to-SQL，Oracle Knowledge）**：EX 平均 **+7.4**、VES 平均 **+8.6**。Claude-Opus-4.8 达 **78.3 EX / 80.5 VES**——**超过 CHESS（65.0）等专用 text-to-SQL 系统**。
> 值得注意：Baseline + SL 在 BIRD 上呈现**混合模式**——EX 最多掉 −5.6 而 VES 全涨，说明静态语义层"改善 SQL 良构性但分散了生成正确查询的注意力"。

**InsightBench（商业洞察）**：Overall 平均 **+1.9**（最大 +6.1，DeepSeek-V4-Flash）。增益小是因为 **Insight 按短参考式发现评分，一旦对齐就饱和**。

### 3.6 成本账：每轮更贵，但总账更便宜

| 指标 | Baseline | Initial | Evolved |
|---|---|---|---|
| 输入 token/轮 (K) | 3.2 | 4.1 | 4.6 |
| 输出 token/轮 (K) | 0.4 | 0.4 | 0.4 |
| **轮次/任务** | **14.6** | 11.2 | **8.4** |
| **总 token/任务 (K)** | 52.6 | 50.4 | **42.0** |
| Traj-Wise (%) | 69.5 | 81.8 | **89.5** |

> **准确率涨 20 分的同时总成本反降约 20%**。机制：本体层把**重复的模式发现摊销**掉了，Agent 不再每个任务都从零盲探。这个"每轮更贵、总账更便宜"的曲线，是本项目做 ROI 论证时最有说服力的一张图。

### 3.7 两个必须知道的"约束"

**① 跨骨干不可迁移（本体的"个性"）**

- 各 backbone 接受的 Term 集合成对 **Jaccard 重叠没有任何一对超过 0.62**；两个 Claude 之间（0.55）比两个 GPT 之间（0.61）共享更少
- 把某 backbone 进化后的本体应用到其他 backbone：**对角线始终是该列最高项，每个非对角项至少下降 6.6 分**（平均列下降 −6.6 到 −10.9）
- 结论：**骨干特定的进化是有益的**，但也意味着每个骨干需独立承担进化成本
- 有趣细节：Claude-Opus-4.8 保留了比 Sonnet-5 更详细的 manifest 变体；GPT-5.5 在 Evidence 下引入了 Opus 本体中不出现的短 SQL 片段库

**② 收敛性（何时该停）**

- 所有 backbone 沿**接受的轮次单调爬升**；GPT-5.6-sol **5 轮**达 93.5，Claude-Opus-4.8 **4 轮**达 92.3
- 末两轮趋于平坦，与"失败签名变稀有"一致——本体覆盖反复出现的跨文件概念后就稳了
- Appendix A：Terms 从 Initial 的 61 增至 5 轮后 80；**第 3 轮后每元素逐轮增长降至 5% 以下**

---

## 4. 工程实现细节：EvoOntology 的产品形态

这一节对平台落地最有直接参考价值。

### 4.1 产品形态：无 CLI，两个 skill 命令 + 一个 MCP 服务

> **产品最终形态 = 一个核心包（含 validate 门禁）+ 两个 skill 命令，无 CLI。智能分析全在 skill，Python 只做「运行时 + 最小确定性校验 + 进化生命周期状态机」。**

这是很克制的架构分工：**确定性的事交给 Python，判断性的事交给 skill（即 LLM）**。

**安装即用**（零 clone、零 venv、零 pip install）：
```bash
claude plugin marketplace add MeiduoChong/EvoOntology
claude plugin install evoontology@evoontology
# 新会话后：
/evo-build  /evo-evolve  /evo-visualize
```

### 4.2 Workspace 结构

```
.evoontology/
├── project.json         # mode / data source / workload / evaluator / boundary
├── active.json          # {"active_version": "ontology_v0"}
├── versions/            # 正式 ontology_vN + 候选 vN-cK，每版本 5 个 JSON
│                        # （对应 Term / Mapping / Relation / Constraint / Evidence）
├── trajectories/        # 每个任务一条 JSON trajectory（Tool Call 粒度，不存思维链）
├── evolution/run_N/
│   ├── run.json                 # 状态 / Parent / 当前 Candidate / 轮次 / 冻结预算
│   ├── trajectory-sources.json  # 用户确认的轨迹来源记录
│   ├── rounds.jsonl             # 每轮一行摘要
│   └── evaluations/             # 正式 Parent/Candidate 评估摘要
└── state.json           # Trigger checkpoint 与阈值
```

**版本命名约定**：正式 `ontology_vN`、候选 `vN-cK`；Accept 时映射 `vN-cK` → `ontology_vN+1`。**不覆盖已有正式版本。**

### 4.3 进化状态机（进化闭环的工程化约束）

```
running ──Reject──▶ running（同一 run 内设计下一个 Candidate）
running ──Accept──▶ accepted（发布新版本、切 active、推进 checkpoint）
running ──预算耗尽/外部阻断──▶ incomplete（不发布、不推进）
```

> **只有 Accept 或合法的 Incomplete 是终态；Reject 只是下一轮的输入。**

这是把"进化"从"一个可能永不收敛或悄悄跑偏的循环"变成**状态机管理的可审计过程**。工程上值得学的地方：

| 机制 | 设计 | 用意 |
|---|---|---|
| **恢复优先** | 新 run 开始时若已有未结束的 run，**resume 而非新开** | 防止状态分叉 |
| **冻结数据** | 开始时冻结训练/验证子集与预算（默认 8 轮），确认后写入 `run.json` | 防止"边跑边改验证集"这种自欺 |
| **冻结来源** | 向用户说明每条轨迹来源的路径/范围/时间/用途并确认 | 数据血缘可审计 |
| **Reject 留档** | 写 `rounds.jsonl`、更新归因与 problem map | **防止重复无效更新** |
| **判断性停止的门槛** | `missing_data` / `unreliable_evaluation` / `external_block` 需在同一 run 内正式 Reject ≥ 2 个候选（`min_rejects_before_incomplete`）才能停 | 防止过早放弃 |
| **报告基于终态** | 最终报告必须基于 session 终态与落盘记录，**不依赖对话记忆** | 可复现 |

### 4.4 两种运行模式

| Mode | 适用 | 机制 |
|---|---|---|
| **`fixed_split`** | 有固定问题集、GT 和评测边界的 benchmark | Construction Pool 用于 Build/诊断；**Validation Reserve 只用于最终 Gate，不能回流到构建、诊断或补丁生成** |
| **`rolling_trajectory`** | 真实业务或冷启动 | seed workload 初始化 `ontology_v0`；上线后任务持续写入 `trajectories/`，达阈值后**冻结一批**，用独立抽样任务或 LLM Judge 完成 Gate |

> 两种 mode 共用同一套 workspace、版本与 checkpoint 机制，**区别只在 workload 如何进入构建、进化与评估**。这个抽象很干净，本项目可直接复用。

### 4.5 评测协议：互逆双折评估

把本体构建和进化视为**训练期工作负载适应**。每个 benchmark 划分为两个不相交折 A 和 B：
- A→B 运行：**70%** 的 A 用于构建/轨迹分析/候选生成，剩余 **30%** 用于配对验证；选定本体在测试 B 前冻结
- 反向折叠，最终分数取双向平均

**留出折仅在冻结后用于最终评估，其答案和评估反馈从不用于本体构建、进化或候选选择。**

无 GT 时走 **LLM Judge 匿名 A/B**（Candidate 以自己的存储版本参评，比较期间**不修改 `active.json`**）。

### 4.6 触发与配置：默认零配置

- **无 `config.yaml`**。用户调整时直接对话说明（如"以后每 60 个任务提醒我一次"），由 agent 更新 `state.json`
- **进化触发默认**：checkpoint 后新增 ≥ 30 个 task，或距 checkpoint ≥ 7 天
- **只有正式 Gate 的 Accept 推进 checkpoint**（Reject 在同 run 内继续循环，Incomplete 不推进）
- **一期明确不做**：Web UI / SaaS / 多租户 / 消息队列 / 常驻 worker / 多 Candidate 并行 / 自动循环 / 高频改 schema。**无人值守全自动进化需要常驻后台 worker，一期只做「检测 + 提醒」，由人触发。**

> **这个边界划定很清醒**：自动演化本体听起来性感，但没有门控监督的自动演化 = 自动积累错误。先做"人触发"，是对的。

### 4.7 案例研究：Card-Legality 语义的进化

论文 Appendix D 给了一个教科书式的局部化更新示例（text-to-SQL，识别目标赛制中被禁的卡牌）：

| 阶段 | 内容 |
|---|---|
| **初始状态** | 本体含 Terms *Card*、*Legality* 及二者 association。*Card* 锚定 `Cards.uuid`；*Legality* 锚定 `legalities.uuid/format/status`。**但未解释 `legalities.status` 的值应如何解读，也未明确 legality status 是相对特定赛制定义的**——Agent 须在执行中从原始值重新发现 |
| **归因** | 归因到 **Content Layer**：现有 browse/resolve 工具已能检索相关对象，Schema Layer 也能表示所需知识。**缺失的是对 status 字段及其适用条件的可复用语义描述** |
| **局部化干预** | 新增 Term *Legality Status Code* 锚定 `legalities.status`；一个 Evidence 记录观察到的 status 值分布；一个 Constraint 声明识别 banned 卡牌需同时满足 `status='Banned'` 且 `format=target_format`。**现有 *Card* 和 *Legality* 保持不变，不引入任何 Tool- 或 Schema-level 修改** |
| **效果** | browse 可为 banned/legal 相关查询浮现新 Term；Agent 用 resolve 取 Mapping + Evidence + 格式依赖的 Constraint。**原生 SQL 执行仍负责应用过滤并验证返回记录** |

> 这个案例完整展示了**"归因 → 只动一层 → 门控"**的威力：不是重写本体，而是精准补一个缺失的可复用语义对象。也印证了 **Evidence + Mapping 是承重家族**。

---

## 5. 对本项目的借鉴清单

### 5.1 可直接采用的设计（高优先级）

| # | 借鉴点 | 来源 | 说明 |
|---|---|---|---|
| 1 | **本体 = 可训练状态**的心智模型 | EvoOntology | 不训练模型，训练知识资产。用轮次预算+验证集+门控约束改动 |
| 2 | **四步进化循环**：诊断→归因→补丁→门控 | EvoOntology | 门控最承重（−11.2），归因第二（−6.3）。**"更选择性而非更迭代"** |
| 3 | **归因到三层的类型化编辑** | EvoOntology | 每次只动一层，隔离假设；拒绝的候选留档防重复 |
| 4 | **状态机化管理进化过程** | EvoOntology | running/accepted/incomplete 三态；Reject 不是终点；冻结预算与数据 |
| 5 | **MCP 工具化按需访问 + 极简 manifest** | EvoOntology | 唯一进 prompt 的是 manifest；静态全量注入实测 −15pp |
| 6 | **Evidence 锚定**：概念必须钉到物理字段 + 有探测实证 | EvoOntology | Mappings（−13.4）和 Evidence（−8.7）是承重家族 |
| 7 | **传统算法 + LLM 分工** | BI-Agent | join/transform 交给有统计保证的数据管理算法，LLM 只做编排 |
| 8 | **保守策略的表剪枝** | BI-Agent | 保留任何看似相关的表，剪除明显无关的——宁可多留 |
| 9 | **版本化 + 不覆盖正式版本** | EvoOntology | `vN-cK` → `ontology_vN+1`，可追溯可回滚 |
| 10 | **双模式抽象**（fixed_split / rolling_trajectory） | EvoOntology | 评测场景与生产场景共用同一套 workspace/版本/checkpoint 机制 |

### 5.2 需要我们自己决策的点

| 议题 | 观察 | 建议 |
|---|---|---|
| **跨骨干迁移** | Jaccard < 0.62，跨用至少 −6.6 分 | 若要支持多模型，需决定：每模型独立演化（成本高但效果最好）vs 维护共享基线（可迁移但损失 6-10 分）。**建议先支持单模型，把多模型作为后期能力** |
| **本体粒度** | Terms 从 61 涨到 80 后收敛（第 3 轮后 <5%/轮） | 说明本体存在自然规模上限；**不必预设"应该多大"**，靠收敛曲线自然停止 |
| **无 GT 场景的验证** | rolling_trajectory 模式用 LLM Judge 匿名 A/B | 需要评估 judge 的可靠性；留意已知问题（LLM-as-judge 可造成两位数波动） |
| **门控阈值 τ** | 论文未给具体值，只说"达阈值" | 参考 MLSys 2026 本体引导记忆论文的敏感性分析：门限在 **τ∈[0.60,0.80]** 有宽平台区，设计不依赖精调 |
| **冷启动** | EvoOntology 用 builder agent 从 workload 探针建 `ontology_v0` | 可结合 Evontree 式"少样本挖掘 LLM 内隐知识"作为补充 |
| **一期范围** | EvoOntology 明确不做常驻 worker、自动循环、多租户 | **建议采纳同样的克制**：先做「检测 + 提醒 + 人触发」，验证门控有效性后再谈自动化 |

### 5.3 平台架构推测建议

综合两项工作，一个「Agent + Ontology 平台」的最小可行形态可以是：

```
┌──────────────────────────────────────────────────────┐
│  Agent 运行时（订阅 MCP）                              │
│    browse_semantics / resolve_semantics / manifest     │
└────────────────────┬─────────────────────────────────┘
                     │ MCP
┌────────────────────▼─────────────────────────────────┐
│  本体层（版本化：ontology_vN）                          │
│  ├─ Content：Term / Mapping / Relation / Constraint / Evidence │
│  ├─ Schema：对象模型与引用规则                          │
│  └─ Tool：browse / resolve / manifest                  │
├──────────────────────────────────────────────────────┤
│  确定性核心（Python）：store / runtime / trajectory /   │
│  trigger / evaluation / evolution 状态机 / validate 门禁│
├──────────────────────────────────────────────────────┤
│  智能分析（Skill/LLM）：Build 与 Evolve 的判断部分       │
│    Builder：workload 探针 → 证据落地 → ontology_v0      │
│    Evolver：诊断 → 归因 → 补丁 → 配对门控               │
└──────────────────────────────────────────────────────┘
        数据源：数据库 / 文件 / 文档（通过领域工具访问）
```

**给 Builder 与 Evolver 分工的原则**（沿用 EvoOntology 的核心判断）：
- **确定性的事交给代码**：JSON 合法性、引用完整性、可加载性、版本切换、状态流转、预算控制
- **判断性的事交给 LLM**：改什么、为什么改、归因到哪一层、改哪个对象
- **两条硬约束**：① 一个 Candidate 只验证一个主要假设；② 一切改动可溯源到目标维度、可回滚到 Parent

---

## 6. 关键数据速查

| 维度 | 数字 |
|---|---|
| BI-Bench 来源 | 3000+ 真实 Power BI 项目，400+ 人时 → 100 对问答 |
| BI-Bench 单题规模 | 平均 10.6 表 / 80,464 行 / 12.75 join，最难题需 38 张表 |
| BI-Agent 工具增益 | SQL **+14pp**、Python **+11pp**（20 案例中 19 个显著） |
| 工具对弱模型增益 | GPT-OSS-120B **+40.0pp（超 8 倍）** |
| 后训练增益 | Qwen3-8B-RL-Tool SQL **+29.8pp**；总训练成本 **< $200** |
| 成本效益 | 后训练 8B vs 大模型：**54 倍**（$0.19 vs >$10） |
| EvoOntology DDR 增益 | Traj-Wise **+20.0**（69.5→89.5）；最高 GPT-5.6-sol **93.5** |
| BIRD 增益 | EX **+7.4**、VES **+8.6**；Opus-4.8 达 **78.3/80.5** |
| 静态语义层反效果 | Claude-Sonnet-5 Traj-Wise **−15.0** |
| 记忆回放对比 | ReAct+Memory 仅 **+6.3**，比 EvoOntology 低 13.7 |
| 消融·门控 | w/o Gate **−11.2**（最大承重） |
| 消融·归因 | w/o Attribution **−6.3** |
| 消融·对象家族 | w/o Mappings **−13.4**、w/o Evidence **−8.7** |
| 三层增益占比 | Tool **57%** / Content 34% / Schema 9% |
| 成本反转 | 轮次 14.6→8.4，总 token **52.6K→42.0K（−20%）** |
| 收敛速度 | GPT-5.6-sol **5 轮**、Opus-4.8 **4 轮** |
| 跨骨干迁移损失 | 至少 **−6.6** 分（Jaccard < 0.62） |

---

## 7. 核心文献与资源

| 资源 | 链接 |
|---|---|
| **EvoOntology** 论文 | [arXiv:2609.15779](https://arxiv.org/abs/2609.15779) |
| **EvoOntology** 仓库 | [github.com/ruc-datalab/EvoOntology](https://github.com/ruc-datalab/EvoOntology) |
| EvoOntology 作者 | Meiduo Chong, Shaolei Zhang*, Ju Fan, Xiaoyong Du（中国人民大学） |
| **BI-Agent / BI-Bench** 论文 | [arXiv:2609.20886](https://arxiv.org/abs/2609.20886) |
| **BI-Agent** 仓库 | [github.com/Hu-Chuxuan/bi-agent](https://github.com/Hu-Chuxuan/bi-agent) |
| BI-Agent 作者 | Chuxuan Hu, Yeye He*, Penny Zhou, Wee Hyong Tok, Daniel Kang, Surajit Chaudhuri（微软研究院 + UIUC） |
| 微信拆解文章 | [Graph的尽头是自进化Ontology~](https://mp.weixin.qq.com/s/E0GJfYjzSs8fBIcn8jhHtA) |
| 方法论来源（SkillOpt） | EvoOntology 架构文档明确说明借鉴其"轮次预算 + 验证集 + Accept/Reject 门控"方法论 |
| 前置相关研究 | MLSys 2026《Ontology-Guided Long-Term Agent Memory for Conversational RAG》（见本项目研究报告 01） |

**BI-Agent 仓库结构**：
```
bi-bench/     # BIBench 数据集（{case_id}/ CSV + gt/ + queries.json）
tools/        # BI-Agent 评测框架（run_large_models.py / run_posttrained.py）
nl2sql/       # NL2SQL 系统评测（HF 模型 + databao agent）
post-train/   # 后训练管线（data_gen / sft / rl）
results/      # 论文结果 CSV（proprietary / open_source / posttrained / nl2sql）
```

**EvoOntology 仓库结构**：
```
evoontology/     # 确定性核心：ontology store / runtime(MCP) / trajectory / trigger
                 #             / evaluation / evolution 状态机 / validate 门禁
plugins/         # Claude Code 插件 + Codex 插件（Build/Evolve/Visualize skills + MCP）
benchmarks/      # bird / ddr_10k / insightbench（各实现一个 EvolutionAdapter）
docs/            # architecture.md / guide/new-benchmark.md
scripts/         # sync_plugin_core.py（core 同步到两个插件）
```

---

## 8. 结论：五条判断

1. **静态图的红利与天花板都已量化。** BI-Agent 证明挂图能让所有模型普涨（弱模型 +40pp），但也证明它只给结构不给理解——Agent 每个任务仍要从零猜"收入对应哪列"。**这不是实现问题，是范式问题。**

2. **动态本体的关键突破是"访问方式"和"演化约束"，不是"内容更多"。** 同一份语义画成死图 = −15pp；按需查询 + 门控演化 = +20pp。**"怎么给"比"给什么"更重要。**

3. **进化闭环的两个承重件是门控和归因，且"少而准"优于"多而快"。** 去掉门控 −11.2、去掉归因 −6.3，而去掉类型化编辑只 −1.7。若要砍功能，先砍迭代轮次，绝不砍门控。

4. **证据锚定是防止本体幻觉的结构性保障。** Mappings（−13.4）和 Evidence（−8.7）是最大承重家族——**概念必须钉到物理字段、必须有探测实证**，不能只写自然语言描述。这与 MLSys 2026 本体引导记忆论文的"低置信度三元组只留向量存储不入图"是同一设计哲学。

5. **克制比激进更难，但更对。** EvoOntology 一期明确不做常驻 worker、自动循环、多租户，只做"检测 + 提醒 + 人触发"。同时它承认跨骨干本体不可迁移（至少 −6.6 分）。**一个诚实的平台应该先证明门控有效，再谈无人值守的自治演化。**

---

*本报告基于公开论文与开源仓库整理。所有数据均来自论文正文、README 或架构文档原文；论文正文在 §6.2 主结果后被截断，故 BI-Agent 的敏感性分析、错误分析与局限性章节内容未能获取，相关部分已明确标注为不可得。*
