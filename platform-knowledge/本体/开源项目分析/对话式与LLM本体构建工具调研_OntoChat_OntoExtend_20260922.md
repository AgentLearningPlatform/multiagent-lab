# 对话式/LLM 辅助本体构建工具调研：OntoChat、OntoExtend 及同类工具

> 状态：v1.0 ｜ 日期：2026-09-22 ｜ 调研范围：OntoChat、OntoExtend 及同类 LLM 驱动本体构建/扩展工具
> 关联：eino-multiagent-lab REQ-80~82（LLM 辅助创建）、REQ-83（fork 扩展）、REQ-90/91（学习内容包）、07 S3（OOPS! 登记）、D-O7（引导执行）

## TL;DR

- **OntoChat**（ESWC 2024）：对话式**本体需求工程**框架——用户故事共创 → 能力问题（CQ）抽取 → CQ 聚类分析 → 本体测试，四步对话工作流；Python/MIT，GitHub 31★，活跃至今（2026-03 还发表了 ACM 期刊演进版）。**解决"本体建之前"的需求 elicitation 痛点**。
- **OntoExtend**（arXiv 2026-07）：需求驱动的**本体扩展**框架——RAG 检索已有本体中与新 CQ 相关的类/属性/公理片段 → LLM 生成扩展片段 → 自动集成；在 EU 项目本体与 Bosch 工业本体 39 个 CQ 上验证，是**"本体建之后"的增量维护**方案，评估体系最完整（结构+功能+用户三重）。
- 两者恰好构成 LLM 本体工程的一对互补：**OntoChat 管"从需求到本体"，OntoExtend 管"从本体到新需求"**。对本项目：前者可借鉴进构建平面 LLM 辅助创建（REQ-80~82）与引导执行（D-O7），后者的 Retriever→Extender→Integrator 三段式是 fork 扩展（REQ-83）的直接参考架构。

---

## 一、OntoChat：对话式本体工程框架

### 1.1 基本信息

| 项 | 内容 |
| --- | --- |
| 论文 | [OntoChat: a Framework for Conversational Ontology Engineering using Language Models](https://arxiv.org/abs/2403.05921)（arXiv 2403.05921，2024-03；ESWC 2024 发表） |
| 作者 | Bohui Zhang, Valentina Anita Carriero, Katrin Schreiberhuber, Stefani Tsaneva, Lucía Sánchez González, Jongmo Kim, Jacopo de Berardinis（7 人） |
| 代码 | [github.com/King-s-Knowledge-Graph-Lab/OntoChat](https://github.com/King-s-Knowledge-Graph-Lab/OntoChat)（MIT，Python 3.11，Gradio 界面，Hugging Face Spaces 可在线体验） |
| 仓库现状 | 31★ / 6 forks，2024-01 创建，持续维护（2026-09 仍有提交） |
| 模型依赖 | OpenAI API（论文评估用 gpt-3.5-turbo / gpt-3.5-turbo-16k） |

### 1.2 解决什么问题

大型本体工程中，stakeholders、领域专家、本体设计师多方交互，传统手工需求收集（访谈、讨论页）资源密集且易产生系统性歧义与偏差。作者先做了 **N=23 语义网从业者调查**（Likert 5 分制，≥75% 正向视为强需求），确认最需要计算支持的 OE 任务：

- 本体需求收集：86.4%
- 从文本需求中抽取 CQ：81.8%
- 本体测试：81.8%
- 需求分析：77.3%

### 1.3 核心工作流（四大功能）

基于 IDEA 框架（Infer, DEsign, CreAte，de Berardinis et al. 2023）设计：

1. **用户故事共创**：stakeholder 与对话 agent 交互，agent 以 elicitation 问题引导生成结构化用户故事（Persona/Goal/Scenario/Example Data/Data Resource 模板）；
2. **CQ 抽取**：本体工程师从用户故事中抽取能力问题（Competency Questions），可迭代细化模型建议；
3. **CQ 过滤与聚类分析**：去冗余 + LLM 聚类（无需预设簇数），输出带标签的 CQ 簇（论文用 Music Meta Ontology 的 CQ 做了演示）；
4. **本体测试支持**：对早期版本本体做基于 CQ 的测试。

### 1.4 评估与后续演进

- 论文评估：复刻 Music Meta Ontology（[polifonia-project/music-meta-ontology](https://github.com/polifonia-project/music-meta-ontology)）的工程过程，收集各组件有效性指标；领域专家与本体工程师反馈积极。
- 演进 1：[Improving ORE with OntoChat and Participatory Prompting](https://arxiv.org/pdf/2408.15256v1)（arXiv 2408.15256）——发现用户直接提示效果不佳，引入"参与式提示"协议（研究者按预定义策略协助用户迭代精炼提示），GPT-4o 生成用户故事的相关性/清晰度/有用性三项均 ≥4/5。
- 演进 2：[OntoChat Assistant for User Story Generation in Ontology Engineering](https://dlnext.acm.org/doi/10.1145/3810943)（ACM，2026-03-14 录用）——形成性研究 N=10 + 知识工程师评估 N=24，**首个经设计验证的"提示引导框架"**，帮助用户充分发挥 LLM 能力生成本体需求。

### 1.5 局限（作者自述）

- LLM 在专门领域的知识有限或过时；
- persona 创建存在偏差；
- 尚未提供实施成本与工期的洞察；
- 用户监督与参与度需进一步量化。

---

## 二、OntoExtend：需求驱动的本体扩展框架

### 2.1 基本信息

| 项 | 内容 |
| --- | --- |
| 论文 | [OntoExtend: A Framework for Requirement-driven and Scalable Ontology Extension with LLMs](https://arxiv.org/html/2607.17963v1)（arXiv 2607.17963，2026-07-20，CC BY 4.0） |
| 作者 | Anna Sofia Lippolis*, Mohammad Javad Saeedizade*（共一），Stefan Schmid, Simon Blattner, Robin Keskisärkkä, Aldo Gangemi, Eva Blomqvist, Andrea Giovanni Nuzzolese |
| 机构 | Linköping 大学、博洛尼亚大学、**Bosch**、ISTC-CNR |
| 代码 | [github.com/dersuchendee/OntoExtend](https://github.com/dersuchendee/OntoExtend)（Python，仓库 10★/1 fork/3 issues，2025-08 创建，2026-09 仍更新） |

### 2.2 解决什么问题

本体扩展 = 面向新需求增强已有本体。难点：成熟本体常含数百个类与属性，超出 LLM 上下文；即使塞得下，无关细节也会误导模型产出离题或不一致的片段。**此前没有任何工作把"检索已有本体元素"内建进扩展流程**——这是 OntoExtend 的核心创新点。

### 2.3 三段式架构

```
新 CQ + 输入本体
   │
   ▼
① Ontology Retriever —— 按新 CQ 检索本体中相关的命名类/属性及其公理，压缩为紧凑"片段"（self-contained RDF/Turtle axioms）
   │
   ▼
② Ontology Extender —— CQ + 检索片段 → LLM 生成缺失的本体片段（不超上下文、不过载）
   │
   ▼
③ Ontology Integrator —— 将片段一致地集成回输入本体
```

### 2.4 评估（同类工作里最完整）

- 数据：**39 个 CQ**，两个真实用例——EU 项目本体 **Onto-DESIDE** + **Bosch 工业本体**；
- 结构有效性：OOPS! 陷阱分析、语法良构性、逻辑一致性全部通过或仅少量问题；
- 功能有效性：生成的片段**满足全部功能评估测试**；
- 专家评估：本体工程师评级为**"少量至中等修订即可集成"**；
- 结论：适合作为需求驱动本体扩展的**"起草助手"**（drafting assistant），对 CQ 具体性与建模 profile 敏感；
- 对比表：在 OOPS!/语法/一致性/需求验证/冗余元素/用户评估/专家评估/跨域/真实本体/可扩展性 10 个维度上，OntoExtend 是唯一全 Y 的工作。

### 2.5 相关工作谱系（论文 Table 1 + 正文）

| 工作 | 思路 | 关键局限 |
| --- | --- | --- |
| Soares et al.（APTO 扩展） | ChatGPT-4 交互式生成 OWL 分类公理 | 无结构校验 |
| Matieu & Groza | Protégé 插件，微调 GPT-3 受控自然语言→OWL | 仅 CNL 翻译 |
| **Phrase2Onto** | 短语主题建模做本体扩展原型 | 仅玩具级本体 |
| **Taxoria** | LLM 为已有节点提议子术语+来源追踪 | 幻觉节点、隐含需求捕获难 |
| Wu et al. 2024（[在线聚类框架](https://www.frontiersin.org/journals/big-data/articles/10.3389/fdata.2024.1463543/full)） | 多 LLM agent + 在线/层次聚类，零样本流式扩展医疗本体 | 领域特定（生物医学） |
| Kholmska et al. | 多 LLM 工作流（概念搜索/抽取/对齐/CQ+SPARQL 生成） | 专业领域需人工修复 |
| Joachimiak et al.（AIO） | Ontology Development Kit 工作流 + AI 策展 | 结构校验为主 |
| Garcìa Fernandez et al. | 人工审核的本体扩展 | 发现 LLM 幻觉已有标准、生成浅于人工金标 |

**共性结论**：当前 LLM 本体扩展主流仍是半自动分类式增长；可复现性、复杂公理支持、需求引出与验证的人工介入是持续痛点。

---

## 三、同类工具补充扫描（"等"）

| 工具/框架 | 定位 | 状态 | 备注 |
| --- | --- | --- | --- |
| **IDEA**（2023，[de Berardinis et al.](https://arxiv.org/abs/2403.05921) 引用） | LLM 驱动 ORE 的先导工具集：CQ 抽取/组织/精炼/不一致检测 | OntoChat 的直接前身 | 学术原型 |
| **LLMs4OL**（2024） | 通用本体学习框架：术语分类/分类层次/关系抽取/概念层次发现/属性填充/公理生成 6 任务；大规模语料微调 | 活跃（含 shared task 社区） | 中文技术博客称微调模型多数任务超 GPT-4 15-20pp（[来源](https://blog.csdn.net/wayle123/article/details/159654802)，二手转述，未核对原论文数据） |
| **OntoGenix**（2024） | 数据集驱动构建：schema 分析→语义类型推断→概念映射→关系发现→候选本体（从数据反推本体） | 工程化框架 | 与"先建本体再灌数据"路线相反，适合已有数据资产的场景 |
| **Taxoria** | 分类树富化（LLM 提议子节点+语义校验+来源追踪） | 论文阶段 | 见 §2.5 |
| **OntoCLM**（[arXiv 2604.04450](https://arxiv.org/html/2604.04450)） | 反向思路：用本体约束 LLM 对话生成（受控生成），7 个开源模型验证 | 论文阶段 | 本体作为对话控制手段而非构建对象 |
| OntoChatGPT（knowledge-ukraine） | 本体驱动结构化提示元学习 | GitHub 15★ | **与 OntoChat 不是同一工作**，勿混淆 |
| ONTO（NeuLLabs，产品） | agent 分层类型化记忆本体（UPO 持久人格层 / SMO 会话暂存层 + 四级抽取路由） | 商业产品 | 与 agent 记忆（semantica Memories/P3）同方向，可作设计参考 |

---

## 四、对 eino-multiagent-lab 的借鉴映射

| 发现 | 呼应需求 | 可落地动作 |
| --- | --- | --- |
| OntoChat 四步工作流（用户故事→CQ→聚类→测试） | REQ-80~82 LLM 辅助创建；D-O7 引导执行 | 构建平面"LLM 辅助创建"引导卡按此拆步：先让用户口述场景生成用户故事，再抽 CQ，再生成类/属性骨架——比"直接一键生成本体"更符合学习平台的教学节奏 |
| CQ（能力问题）作为本体需求的锚 | REQ-90/91 学习内容包 | 方法论五模块中补"能力问题 CQ"概念卡（NeOn / Ontology 101 均以 CQ 为核心，OntoChat 论文有现成通俗解释） |
| OntoExtend Retriever→Extender→Integrator 三段式 | REQ-83 fork 扩展（P2） | fork 扩展功能直接参考此架构：按新需求检索已有 K8s 本体相关片段（而非整本体塞 prompt）→ LLM 生成扩展片段 → 校验后集成；检索可用本项目已有向量层 |
| OntoExtend 的三重评估（OOPS!/语法一致性/功能测试） | 07 S3 OOPS! 已登记；semantica 质量门（REQ-102） | 质量门页面的检查维度对齐：结构（OOPS!+语法+一致性）与功能（CQ 覆盖测试）两层，正是其论文验证过的组合 |
| Participatory Prompting（提示引导框架，ACM 2026） | REQ-80~82；REQ-91 任务卡 | 用户与 LLM 协作建本体时预置引导提示模板，降低"不会写提示"门槛——其 GitHub 开源了全部 prompt 策略可白嫖 |
| OntoChat MIT + Gradio + HF Spaces 在线可体验 | — | 学习内容包可挂体验链接，学员 5 分钟内上手感受对话式 OE |
| LLM 本体扩展仍是"起草助手"定位（专家修订不可省） | 全局 | 印证本项目"LLM 辅助 + 人工确认"的交互定位，不做全自动生成幻觉 |

---

## 五、来源清单

1. OntoChat 论文：https://arxiv.org/abs/2403.05921 （HTML 版 https://arxiv.org/html/2403.05921v2）
2. OntoChat Participatory Prompting：https://arxiv.org/pdf/2408.15256v1
3. OntoChat ACM 2026 期刊版：https://dlnext.acm.org/doi/10.1145/3810943
4. OntoChat 代码：https://github.com/King-s-Knowledge-Graph-Lab/OntoChat ；在线体验 https://huggingface.co/spaces/b289zhan/OntoChat
5. OntoExtend 论文：https://arxiv.org/html/2607.17963v1
6. OntoExtend 代码：https://github.com/dersuchendee/OntoExtend
7. Wu et al. 在线聚类扩展：https://www.frontiersin.org/journals/big-data/articles/10.3389/fdata.2024.1463543/full
8. OntoCLM（本体约束对话生成）：https://arxiv.org/html/2604.04450
9. LLMs4OL / OntoGenix 中文综述：https://blog.csdn.net/wayle123/article/details/159654802
10. GitHub 仓库元数据（star/fork/活跃度）：GitHub API 检索，2026-09-22 查询

> 局限说明：OntoChat/OntoExtend 论文细节以 arXiv 摘要与 HTML 全文可见部分为准；LLMs4OL 的性能数字来自二手博客转述，引用前建议核对原论文；GitHub star 数为 2026-09-22 快照。
