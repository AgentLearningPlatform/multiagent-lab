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

> **维护说明**：本文件面向学习者（运行时渲染），收录粒度为"一句话 + 链接"；更完整的定位/使用状态/借鉴价值/活跃度评估见 `docs/15_开源项目及论文登记簿.md`（维护者视角全局台账）。两者内容不一致时以登记簿为准并回修本文件。
