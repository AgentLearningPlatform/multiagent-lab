# 本体学习外部资源导航

> 本文件由 eino-multiagent-lab 仓库随版本维护（REQ-109），学习中心「外部资源」Tab 前端构建期内联渲染（Vite `?raw` 单源导入，无后端端点、无拷贝）——编辑本文件并 commit 后重新构建（dev 模式热更新）即生效，无需改代码。
> 收录原则：可直接在线访问的平台 / 值得精读的典型开源项目 / 官方规范与教程；均须与本项目两大重点（**本体构建 / 智能体工程落地**）直接相关。全局台账与登记状态见 `docs/15_开源项目及论文登记簿.md`。

## 一、可直接访问的在线平台

| 平台 | 链接 | 一句话说明 |
| --- | --- | --- |
| **Ontology Playground**（微软官方，Preview） | <https://microsoft.github.io/Ontology-Playground/> | 免费开源本体学习 Playground（MIT）：6 领域本体目录、可视化设计器、RDF/XML 导入导出、Ontology School 9 门课（渐进路径 + 交互测验 + 演示模式）、Quest 任务系统。与本项目定位同构，推荐整体体验对照学习（[仓库](https://github.com/microsoft/Ontology-Playground)） |
| **WebProtégé**（Stanford，在线版） | <https://webprotege.stanford.edu/> | 在线协作本体建模器，完整 OWL 编辑能力；本项目「引导执行」路线（S1 来源 / S2 编辑）的主用外部平台 |
| **Fabric IQ Ontology 文档**（微软） | <https://learn.microsoft.com/en-us/fabric/iq/ontology/overview> | Ontology Playground 背后的 Microsoft Fabric IQ 本体体系官方文档，可了解工业级本体的业务落地形态 |

## 二、复用词表与顶层本体（方法论「复用既有词表」配套）

| 资源 | 链接 | 一句话说明 |
| --- | --- | --- |
| **FOAF** 词汇表 | <https://xmlns.com/foaf/spec/> | 人物/组织/关系事实标准；本项目示例本体「组织与人员」即对照 FOAF 术语 |
| **SKOS**（W3C） | <https://www.w3.org/TR/skos-primer/> | 受控词表/分类体系标准；「设备故障知识」示例的分类层即 SKOS 思路 |
| **schema.org** | <https://schema.org/> | Google/微软等共推的通用实体词表，覆盖电商/事件/组织等高频概念 |
| **BFO** 顶层本体 | <https://github.com/BFO-ontology/BFO> | Basic Formal Ontology，跨领域顶层本体代表，学习"上层如何约束下层" |

## 三、官方规范与教程文档

| 文档 | 链接 | 一句话说明 |
| --- | --- | --- |
| **OWL 2 Primer**（W3C） | <https://www.w3.org/TR/owl2-primer/> | OWL 2 官方入门教程，S2/S3 校验环节的理论底座 |
| **RDF 1.1 Primer**（W3C） | <https://www.w3.org/TR/rdf11-primer/> | RDF 三元组模型官方入门 |
| **SPARQL 1.1 Query Language**（W3C） | <https://www.w3.org/TR/sparql11-query/> | SPARQL 官方规范；SPARQL 工作台（REQ-92）动手前建议速读 |

## 四、典型开源项目（本项目集成或对照参考）

| 项目 | 链接 | 一句话说明 |
| --- | --- | --- |
| **Protégé 桌面版** | <https://github.com/protegeproject/protege> | 业界标准本体建模器，引导执行路线主力（含推理器 ELK/HermiT） |
| **WebProtégé**（仓库） | <https://github.com/protegeproject/webprotege> | 上表在线版的开源仓库，可自托管 |
| **ROBOT** | <https://github.com/ontodev/robot> | 命令行本体处理（template/merge/subset），进阶批量场景 |
| **rdflib**（Python） | <https://github.com/RDFLib/rdflib> | RDF 全能库；本项目 sidecar 的 OWL/RDF 解析 + TTL 导出 + 校验三职责基座 |
| **pySHACL** | <https://github.com/RDFLib/pySHACL> | SHACL 约束校验，sidecar 业务约束层 |
| **Oxigraph** | <https://github.com/oxigraph/oxigraph> | Rust SPARQL 1.1 单二进制引擎，本项目 P1 默认运行引擎 |
| **Apache Jena / Fuseki** | <https://github.com/apache/jena> | SPARQL + OWL 推理开关服务，推理对照（O6）主引擎 |
| **Yasgui** | <https://github.com/TriplyDB/Yasgui> | SPARQL 查询工作台组件，REQ-92 前端嵌入 |
| **Cayley**（aperturerobotics fork） | <https://github.com/aperturerobotics/cayley> | Go 嵌入式图库，P2 memory_graph 规划集成项 |
| **OOPS!**（陷阱扫描） | <https://oops.linkeddata.es/> | 本体建模 34 类陷阱在线扫描，零代码质量检查入口 |
| **semantica** | <https://github.com/semantica-agi/semantica> | Graph-Native 上下文/决策智能平台，本项目独立栏集成（锁 v0.6.8+） |
| **OntoChat / OntoExtend / OpenBKN** 等 LLM 本体工程对照项目 | 见 [docs/15_开源项目及论文登记簿.md](../../docs/15_开源项目及论文登记簿.md) 第二/六/七类 | 方法论与 prompt 素材对照参考，不引入代码 |

## 五、智能体工程与知识增强（RAG / GraphRAG，对照参考）

| 资源 | 链接 | 一句话说明 |
| --- | --- | --- |
| **ReAct**（论文，2022） | <https://arxiv.org/abs/2210.03629> | 推理+行动交替协同的智能体奠基范式，「边想边做」；理解本项目 Agent 运行循环（计划→工具→观察）的理论起点 |
| **A Survey on LLM based Autonomous Agents**（论文，人大） | <https://arxiv.org/abs/2308.11432> | 自主智能体全景综述：架构/记忆/规划/工具使用四模块分类法，建立智能体能力地图的首选读物 |
| **RAG for LLMs: A Survey**（论文） | <https://arxiv.org/abs/2312.10997> | 检索增强生成综述：Naive→Advanced→Modular RAG 演进脉络，本项目知识库检索链路的对标框架 |
| **GraphRAG**（论文，微软） | <https://arxiv.org/abs/2404.16130> | 图谱增强检索：LLM 抽实体图谱+社区摘要回答全局性问题——知识库与知识图谱（REQ-141 调研方向）联动的关键参考 |
| **Eino**（CloudWeGo） | <https://github.com/cloudwego/eino> | Go 语言 LLM 应用开发框架（编排/组件/流式），本项目智能体运行时基座 |
| **GraphRAG 官方实现**（微软） | <https://github.com/microsoft/graphrag> | 论文的官方开源实现，可对照体验图谱构建与全局问答全流程 |

---

## 六、本体开源实现方案借鉴（技术研究专栏，REQ-155~158 配套）

> 来源：[docs/23《本体开源实现方案借鉴研究》](../../platform-knowledge/本体/23_本体_开源实现方案借鉴研究.md)（2026-09-25，7 方向全量评估）。原则：选型全都要（多方案并存）/ Go 优先排期 / 结构化分组呈现。「落地栏」= 建议并入的本体模块页面；工作量 S/M/L 为初估。

| 方向 | 项目/方案 | 链接 | 一句话说明 | 落地栏 | 工作量 |
| --- | --- | --- | --- | --- | --- |
| 建模辅助 | **ROBOT Template** | <https://github.com/ontodev/robot> | 表格驱动批量生成本体，模板→术语批量展开 | 构建·批量生成（sidecar 后置） | M |
| 建模辅助 | **ODP 本体设计模式库** | <https://github.com/kastle-lab/modular-ontology-design-library> | 模块化可复用设计模式库，构建时推荐套用 | 构建·新增路径 | M |
| 建模辅助 | **Domain-OntoGen** | <https://arxiv.org/html/2504.17402> | LLM 需求引出→领域本体自动生成管线 | 构建·ontochat 路径 | S |
| 建模辅助 | **LinkML** | <https://linkml.io> | Schema-as-Code 建模（与 spec_json 定位需评估融合） | 构建·对照评估 | L |
| 校验 | **OLIVAW** | <https://arxiv.org/html/2510.17184v1> | 本体质量 CI 门禁三模式（保存钩子/导入门禁/CLI 批检） | 资产·质量门禁（REQ-156） | M |
| 校验 | **Semantica QualityGate** | <https://docs.getsemantica.ai/reference/ontology/> | 商用质量门禁检查项参考（语法/一致性/覆盖率） | 资产·质量门禁（REQ-156） | M |
| 复用生态 | **LOV** | <https://lov.linkeddata.es> | 词表搜索 API，Go HTTP 客户端直连即可用 | 构建·新增路径 | S |
| 复用生态 | **EBI OLS** | <https://www.ebi.ac.uk/ols4/> | 本体语义地图与术语检索 | 资产·可视化 | M |
| 复用生态 | **BioPortal** | <https://bioportal.bioontology.org> | 最大本体仓库与 Annotator 术语抽取 | 构建·新增路径 | S |
| 可视化 | **WebVOWL** | <https://www.npmjs.com/package/angular-webvowl> | 力导向本体可视化（已随 REQ-154/M21 排期对照激活） | 资产·可视化（已排期） | — |
| 可视化 | **OrionBelt** | <https://pypi.org/project/orionbelt-ontology-builder/1.16.6/> | Streamlit 本体工作台，导入三策略审查参考 | 资产·导入审查（REQ-157） | M |
| 可视化 | **OntoGraf** | <https://protegewiki.stanford.edu/wiki/OntoGraf> | Protégé 交互式图谱导航 | 资产·可视化 | S |
| AI-native | **Open Ontologies** ⚡ | <https://glama.ai/mcp/servers/fabio-rovai/open-ontologies> | Rust/Oxigraph AI-native 本体工程，70+ MCP 工具；F1 实验：工具链 0.717 ≫ 直读 OWL 0.323 | 三栏·工具链（REQ-155，Top1） | L |
| AI-native | **OntoChat** | <https://github.com/King-s-Knowledge-Graph-Lab/OntoChat> | 多智能体对话式本体构建工作流 | 构建·ontochat 路径 | M |
| AI-native | **OntoGenix** | <https://mikelval82.github.io/Portfolio/blog-ontogenix.html> | 本体自修复循环（validate→repair 迭代） | 构建·ontochat 路径 | S |
| AI-native | **LLM4ACOE** | <https://resolve.cambridge.org/core/journals/knowledge-engineering-review/article/automating-agentic-collaborative-ontology-engineering-with-roleplaying-simulation-of-llmpowered-agents-and-rag-technology/C4DFC9BD18020226B4CC763BE7056659> | 角色扮演多智能体协作本体工程框架 | 构建·ontochat 路径 | M |
| 存储 | **Data Pipeline 增强** | [见 docs/23 §7.1](../../platform-knowledge/本体/23_本体_开源实现方案借鉴研究.md) | KG 构建管线增强（D-O14 迭代：混合策略/质量抽检） | 知识库·第六路径 | M |
| 存储 | **语义嵌入双空间搜索** | [见 docs/23 §7.2](../../platform-knowledge/本体/23_本体_开源实现方案借鉴研究.md) | 术语向量与图结构双空间检索 | 资产·检索 | M |
| 协作 | **WebProtégé 协作** | <https://github.com/protegeproject/webprotege> | 自托管协作建模（远期） | 构建·协作 | L |
| 协作 | **语义 Diff** | <https://www.w3.org/2001/sw/wiki/How_to_diff_RDF> | RDF diff 方法集（结构化报告先行） | 资产·版本对比 | M |

> 完整评估（每方案三段式：是什么/亮点/借鉴点）与三阶段路线见 [docs/23_本体_开源实现方案借鉴研究.md §9](../../platform-knowledge/本体/23_本体_开源实现方案借鉴研究.md)；吸收池方案推进时逐一立项。

### 6.A 子课题：智能体运行时动态薄本体（2026-09-25 登记）

> **源文档**（主人引入，仓库副本 `research/01/02-*.md`）：《01-研究报告-智能体运行时动态本体的可行性与方案》——以 MLSys 2026《Ontology-Guided Long-Term Agent Memory for Conversational RAG》为核心的可行性研究：运行时从对话数据自动归纳轻量本体图，破解"隐式召回失败"（Recall@10 0.58→0.70，成本较长上下文降 81%），四层六模块架构 + 轻量/标准/重型三档路线；《02-资料合集-动态本体与智能体记忆》——30+ 篇文献六板块合集（容量记忆线 MemGPT/Mem0/A-MEM、结构检索线 GraphRAG/HippoRAG/LightRAG、动态 schema 归纳线 AutoSchemaKG/EDC/Agentic-KGR、时序图谱线 Zep·Graphiti/Tag2Graph，附 LoCoMo/LongMemEval/BEAM 评测基准与术语表）。
> **落地方案**：[动态薄本体_可行方案](../../platform-knowledge/智能体/动态薄本体_可行方案.md)——取报告**轻量档**（固定种子 schema + 实例填充）映射 D-O15 后自研栈（Oxigraph named graph 会话图 / LLM 抽 KG lightweight / PROV-O / REQ-151 facade），旁路低侵入：对话主链路 0 改动、开关默认关、会话图可整体摘除；展示入口走资产栏独立页签（D-O19 边界外第三来源「对话」）；**REQ-170 已立项**（2026-09-26，方案 only 待排期；子课题资料与方案均入平台知识智能体模块）。

---

> **维护说明**：本文件面向学习者（运行时渲染），收录粒度为"一句话 + 链接"；更完整的定位/使用状态/借鉴价值/活跃度评估见 `docs/15_开源项目及论文登记簿.md`（维护者视角全局台账）。两者内容不一致时以登记簿为准并回修本文件。
