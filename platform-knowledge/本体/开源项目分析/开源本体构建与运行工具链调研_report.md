# 开源本体构建与运行工具链调研报告

> 调研日期：2026-09-20 ｜ 调研范围：截至 2026-09 全球开源本体构建（authoring/engineering）与运行时（runtime/serving）项目
> 适用场景：K8s 智能运维本体（OWL/RDF，Turtle 与 OWL/XML，45–108 类、含 subClassOf 与个体），Go/Eino + MCP + GraphRAG 技术栈方向

---

## 一、核心结论

**构建侧**，Protégé Desktop（v5.6.5，BSD-2）仍是功能最全的单一本体编辑工具，WebProtégé 填补了多人协作在线编辑的空白，ROBOT（v1.9.10）是 OBO 生态的 CLI 标准件，OWL API（Java）与 Owlready2/RDFLib（Python）分别主导两个生态的库形态操作；2025–2026 年涌现的 AI 辅助工具中，**OWLAPY**（MIT，50,000+ 下载）和 **AWS Context Ontology Accelerator**（Apache-2.0）是两个真正开源可用的新选项，前者提供 Python 原生的 OWL 操作 + LLM 生成 + 推理集成，后者将本体构建+MCP 服务器一体化但深度绑定 AWS 基础设施。TopBraid Composer 已转为纯商业产品，不再开源。

**运行侧**，Apache Jena Fuseki（Apache-2.0，v5.x）与 Oxigraph（Apache-2.0/MIT，v0.5.11）仍是开源双雄——前者含 RDFS/OWL 推理，后者 Rust 单二进制、运维极简。Eclipse RDF4J（EPL-2.0，v6.1.0）已升级至 RDF 1.2/SPARQL 1.2 基线；QLever（Apache-2.0）在万亿级场景表现突出，正被 Wikimedia 评估为 Blazegraph 替代；GraphDB Free 许可专有、Blazegraph 已归档，结论无变化 [(本体运行时报告, 2026-09-18)](file:///Coze/Drive/本体学习/本体运行时与MCP服务开源方案调研_report.md)。

**全生命周期视角**：对于 K8s 运维本体场景（Turtle/OWL/XML 混合，需团队协作与版本管理），推荐组合为 **Protégé Desktop（核心编辑 + 推理验证）+ WebProtégé（多人协作）+ ROBOT（CI/CD 校验）+ Widoco（文档发布）+ Fuseki Docker（运行服务）+ mcp-go 自建薄层 MCP Server**，AI 辅助构建可试用 OWLAPY 的 LLM 本体生成功能作为初始草案。

---

## 二、A 类｜本体构建（Authoring/Engineering）工具盘点

### 2.1 A 类总览对比表

| 项目 | 官方仓库/主页 | 许可证 | 最新版本/日期 | 部署形态 | 协作能力 | 推理与验证 | 导入导出格式 | 活跃度 | 适用场景 |
|------|-------------|--------|-------------|---------|---------|-----------|------------|--------|---------|
| **Protégé Desktop** | [protegeproject/protege](https://github.com/protegeproject/protege) | BSD-2-Clause | v5.6.5 (2026) | 桌面（Java/跨平台） | 单人 | ✅ HermiT/Openllet/ELK 接入，SHACL 通过插件 | ttl/owl/xml/rdf/obo/manchester | ★★★★★ 斯坦福+社区持续维护 | 全功能本体编辑/推理/可视化 |
| **WebProtégé** | [protegeproject/webprotege](https://github.com/protegeproject/webprotege) | BSD-3-Clause | 持续更新 (2026) | Web（Docker） | ✅ **多人在线协作**，角色权限，修订历史 | 有限（无内置推理） | ttl/owl/xml/rdf/obo | ★★★★ 斯坦福维护 | 团队协作编辑 OWL 本体 |
| **VocBench3** | [art-uniroma2/vocbench3](https://github.com/art-uniroma2/vocbench3) | BSD-3-Clause | 持续更新 (2026) | Web（Java WAR） | ✅ 多人协作，审批流程 | ✅ SHACL 校验，规则引擎 | ttl/xml/rdf | ★★★★ EU 资助维护 | 术语/词表(SKOS/RDFS)管理 |
| **ROBOT** | [INCATools/ontology-development-kit → robot](https://github.com/INCATools/ontology-tools) 及 [obolibrary/robot](https://github.com/obolibrary/robot) | BSD-3-Clause | v1.9.10 (2026-04) | CLI（Java jar） | 单人（CI/CD 集成） | ✅ 通过 OWL API 调用推理器，merge/validate | ttl/owl/xml/obo/json | ★★★★ OBO 标准工具 | 本体 CI/CD 校验/转换/合并 |
| **ODK** | [INCATools/ontology-development-kit](https://github.com/INCATools/ontology-development-kit) | BSD-3-Clause | v1.6.1 (2026) | Docker/CLI | 单人（项目脚手架） | 通过 ROBOT 间接调用 | 项目模板生成 | ★★★ OBO 社区维护 | 本体项目脚手架/标准化开发流程 |
| **Widoco** | [dgarijo/Widoco](https://github.com/dgarijo/Widoco) | MIT | v1.4.25 (2026) | CLI（Java jar） | 单人 | ❌ 文档生成，不做推理 | 输入:ttl/owl → 输出:HTML+RDFa | ★★★ 学术维护 | 本体文档自动生成 |
| **Chowlk** | [ontology-linkeddata/chowlk](https://github.com/ontology-linkeddata/chowlk) | Apache-2.0 | 在线版持续可用 (2025-12 确认) | Web/CLI | 单人 | ❌ 建模→OWL 转换 | draw.io XML → TTL | ★★ 学术项目 | 用 draw.io 可视化建模后导出 OWL |
| **OWL API** | [owlcs/owlapi](https://github.com/owlcs/owlapi) | BSD-2-Clause | v5.x (持续更新) | Java 库 | N/A（嵌入式） | ✅ OWL 2 完整推理接口 | ttl/owl/xml/rdf/manchester | ★★★★★ 本体 Java 生态基础设施 | Java 程序化本体操作 |
| **Apache Jena** | [apache/jena](https://github.com/apache/jena) | Apache-2.0 | v5.x (2026 持续) | Java 库/CLI | N/A（嵌入式） | ✅ RDFS/OWL 推理（TDB+推理器） | ttl/owl/xml/ntriples/jsonld | ★★★★★ Apache TLP | Java RDF/OWL 编程 |
| **FOOPS!** | [ontologyLink/fairOntologyPitfallScanner](https://ontool.linkeddata.es/foops/) | 开源 | 在线可用 | Web 服务 | 单人 | ✅ FAIR 评估 + OOPS! 缺陷检测 | 输入:OWL URI/文件 | ★★★ 学术维护 | 本体质量/FAIR 合规检查 |
| **OOPS!** | [oeg-upm/oops](http://oops.linkeddata.es/) | 开源 | 在线可用 | Web 服务 | 单人 | ✅ 40+ 种本体缺陷检测 | 输入:OWL URI | ★★★ 学术维护 | 本体设计缺陷扫描 |
| **ontoology** | [Ontoology/Ontoology](https://github.com/OnToology/OnToology.github.io) | AGPL-3.0 | 在线可用 | Web 服务 | ✅ 与 GitHub 集成，自动发布 | ✅ 集成 Widoco+LODE+AR2DTool | ttl/owl → HTML 文档 | ★★★ 学术维护 | 本体自动发布到 GitHub Pages |

### 2.2 关键项目详细说明

#### Protégé Desktop

Protégé Desktop 由斯坦福大学开发，是本体检视与编辑的事实标准工具。v5.6.5 可通过 winget 直接安装（`winget install -e --id Stanford.Protege`），也可从 [protege.stanford.edu](https://protege.stanford.edu/software/) 下载。项目基于 Java/OSGi 架构，通过插件系统扩展功能。核心优势在于 **推理器直接集成**（HermiT、Openllet/Pellet、FaCT++）和**丰富的插件生态**，支持 OWL 2 DL 全部构造的编辑、一致性检查、分类推理与 DL Query [(protegeproject/protege)](https://github.com/protegeproject/protege)。

**优点**：功能最全面；社区最大（邮件列表持续活跃）；教程/文档最丰富；直接打开 .ttl/.owl 文件，零配置。
**缺点**：桌面应用非 Web；Java Swing GUI 相对老旧；不支持多人实时协作（需 WebProtégé）。

#### WebProtégé

WebProtégé 是 Protégé 的 Web 版本，由斯坦福同一团队开发，**专为多人协作设计**。部署使用 `docker-compose up -d`（MongoDB + WebProtégé），浏览器访问 `http://localhost:5000` 即可使用。支持 RDF/XML、Turtle、OWL/XML、OBO 等主流格式的导入导出；内置修订历史与版本回滚；权限按角色划分（项目级/角色级）。底层通过 GWT 富客户端 + MongoDB 持久化 + Lucene 索引实现多人同时编辑同一本体的稳定性 [(webprotege guide)](https://gitcode.com/gh_mirrors/we/webprotege)。

**推理支持**：WebProtégé 自身**不内置推理器**，这是与 Protégé Desktop 的关键差异。需要推理的用户应将本体导入 Protégé Desktop 运行推理，或在 Fuseki 加载后通过 SPARQL 查询推理结果。
**优点**：唯一成熟的开源 Web 协作本体编辑器；Docker 一键部署；修订历史完善。
**缺点**：无内置推理；GWT 前端技术栈较老；大型本体性能未充分验证。

#### ROBOT (A ROBOT is an OBO Tool)

ROBOT 是 OBO Foundry 社区的标准 CLI 工具，基于 Java/OWL API。核心功能包括：本体格式转换（`robot convert`）、合并（`robot merge`）、推理（`robot reason`）、验证（`robot validate`）、提取模块（`robot extract`）、生成文档（`robot export`）等。v1.9.10（2026-04）新增了 OBO Graphs 格式更新、前缀注入等功能。在 CI/CD 流水线中可用 ROBOT 自动检查本体一致性、生成发布产物 [(OBO Newsletter Issue 10)](https://obofoundry.org/newsletter/2026/04/13/10th-issue-newsletter.html)。

#### Widoco

Widoco 是本体的 HTML 文档自动生成工具，输入 OWL/TTL 文件，输出包含类/属性描述、交叉引用、 provenance 页面的 HTML 文档（含 RDFa 注解）。v1.4.25 需 JDK 11/14/17。常与 ontoology（GitHub 自动发布）配合使用，实现"推送 GitHub → 自动构建文档 → 发布到 GitHub Pages"的工作流 [(dgarijo/Widoco)](https://github.com/dgarijo/Widoco)。

#### Chowlk

Chowlk 是一个独特的工具：允许用户在 draw.io（diagrams.net）中按本体建模约定绘制概念图，然后导出为 Turtle 格式。在线版 <https://chowlk.linkeddata.es/> 支持上传 draw.io XML 文件直接转换。适合**快速可视化建模阶段**——用 draw.io 画出类/属性/关系图，一键转换为正式 OWL 文件 [(Chowlk)](https://chowlk.linkeddata.es/)。

**优点**：降低建模门槛（draw.io 人人会用）；输出标准 TTL。
**缺点**：仅支持 OWL 的有限构造；不适合复杂公理（匿名类、限制等）。

#### TopBraid Composer 现状

TopBraid Composer 由 TopQuadrant 公司开发，**当前为纯商业产品**，不再开源。W3C Semantic Web Wiki 页面最后编辑日期为 2011 年，描述其基于 Eclipse 平台。早期存在"TopBraid Composer Free Edition"（如 v4.1），但已停止更新。当前产品线为 **TopBraid EDG (Enterprise Data Governance)** 7.1，为订阅制商业软件。在 2026 年多个"Top 10 Ontology Management Tools"对比文章中，TopBraid EDG 被标注为"enterprise subscription"，与开源工具分开列出 [(TopBraid W3C Wiki)](https://www.w3.org/2001/sw/wiki/TopBraid) [(devopsschool.com)](https://www.devopsschool.com/blog/top-10-ontology-management-tools-for-ai-features-pros-cons-comparison/)。

**结论**：TopBraid Composer **已不再开源/免费**，仅作为商业产品存在。本调研将其归入排除项。

### 2.3 推理引擎现状

| 推理引擎 | 许可证 | 支持逻辑 | 集成方式 | 状态 (2026-09) |
|---------|--------|---------|---------|---------------|
| **HermiT** | LGPL v2.1 | OWL 2 DL 完整 | Protégé 内置/OWL API 调用/Owlready2 内置 | ★★★★ 活跃，最广泛使用的开源 OWL 2 DL 推理器 |
| **Openllet** | LGPL v2.1 | OWL 2 DL (Pellet fork) | Protégé 插件/OntView 内置 | ★★★ 活跃，Pellet 的社区维护分支 |
| **ELK** | Apache-2.0 | OWL 2 EL | Protégé 插件/OWL API | ★★★ 活跃，适合 EL profile 的大规模本体 |
| **FaCT++** | LGPL v2.1 | SHIN(D) | Protégé 默认推理器 | ★★ 低活跃，逐渐被 HermiT 取代 |

### 2.4 Protégé 插件生态定位

| 插件 | 角色 | 归属分类 |
|------|------|---------|
| **OntoGraf** | 类/属性/个体的图可视化（cube/tree 布局） | 浏览/可视化 |
| **OWLViz** | VOWL 标准表示法可视化 | 浏览/可视化 |
| **HermiT/Pellet/Openllet** | 推理器（一致性与分类推理） | 构建/验证 |
| **CellFIE** | 基于 Excel 批量编辑本体 | 构建 |
| **DL Query Tab** | 描述逻辑表达式查询测试 | 浏览/查询 |
| **OntoTrace** | 个体追踪与可视化 | 浏览 |
| **Snowowl** | SNOMED CT 编辑 | 构建（生物医学专用） |

### 2.5 AI 辅助本体构建新工具（2023+）

这是 2025–2026 年涌现的新领域，以下项目经搜索核实确实存在且开源：

| 项目 | 仓库/来源 | 许可证 | 核心能力 | 开源可用状态 |
|------|---------|--------|---------|------------|
| **OWLAPY** | [dice-group/owlapy](https://github.com/dice-group/owlapy) | MIT | Python OWL 框架；支持 LLM 从自然语言生成 OWL；集成 HermiT/Pellet + 嵌入推理；50,000+ 下载 | ✅ 可用，MIT |
| **OntoLearner** | [dice-group/Ontolearn](https://github.com/dice-group/Ontolearn) | MIT | 基于 OWLAPY 的 OWL 类表达式学习框架，用于知识图谱上的归纳学习 | ✅ 可用 |
| **IDEA2** | [KE-UniLiv/IDEA2](https://github.com/KE-UniLiv/IDEA2) | MIT | LLM + Expert-in-the-Loop 提取 Competency Question；Gemini 驱动；PROV-O provenance | ✅ 可用 (2026-04 公开) |
| **AWS Context Ontology Accelerator** | [aws/context-ontology-accelerator](https://github.com/aws/context-ontology-accelerator) | Apache-2.0 | AI 辅助本体构建+MCP 服务器；Neptune 图存储；HermiT/ELK 推理容器；v0.2.0 | ✅ 可用，但深度绑定 AWS (2026-07 GA) |
| **ExtensityAI/ontology-hydra (HyDRA)** | [ExtensityAI/ontology-hydra](https://github.com/ExtensityAI/ontology-hydra) | 开源 | LLM 驱动的领域本体+知识图谱生成；基于 symbolicai 框架；CLI + 可视化 | ✅ 可用 (2026-04 更新) |
| **GrOIL** | arXiv:2608.22134 | 学术 | 图约束的 LLM 本体归纳管线（7 阶段），生成完整 OWL TBox | ⚠️ 论文公开，代码可用性未确认 |
| **OntoEKG** | [LiberAI/OntoEKG](https://github.com/LiberAI/OntoEKG) | 开源 | LLM 从企业非结构化数据构建 Ontology + KG | ⚠️ 项目较小，活跃度待确认 |
| **LLM4VKG** | [HomuraT/LLM4VKG](https://github.com/HomuraT/LLM4VKG) | 开源 | LLM + 虚拟知识图谱，时间感知 | ⚠️ 研究项目 |

**OWALPY 详细说明**：OWLAPY 是当前最成熟的 AI 辅助本体构建开源框架。它提供类似 Java OWL API 的 Python 接口，支持从自然语言文本通过 LLM 自动抽取实体关系生成 RDF 三元组，并支持 HermiT/Pellet 推理（通过 JPype 桥接 Java）。15,000 行代码，165 个单元测试，MIT 许可证 [(OWLAPY paper, arXiv:2511.08232v1)](https://arxiv.org/html/2511.08232v1/)。

**AWS COA 详细说明**：AWS Context Ontology Accelerator (v0.2.0) 是一个完整的 AI 辅助本体构建+服务化方案。架构包含：OntologyEngine（HermiT/ELK 推理，Java 容器）、VKG（Ontop SPARQL↔SQL 转换）、MCP Server、Neptune 图存储。Apache 2.0 许可证。但依赖 AWS 生态（Neptune、OpenSearch、Bedrock），不适合非 AWS 用户 [(AWS COA)](https://aws.amazon.com/cn/about-aws/whats-new/2026/07/aws-context--ontology-accelarator-generally-available/) [(qiita.com COA tutorial)](https://qiita.com/mksamba/items/bc18e35b407b15d1f630)。

---

## 三、B 类｜本体运行（Runtime/Serving）工具复核与补充

### 3.1 已有报告结论复核（复核日期：2026-09-20）

> 引用来源：[本体运行时与MCP服务开源方案调研_report.md](file:///Coze/Drive/本体学习/本体运行时与MCP服务开源方案调研_report.md)（2026-09-18）

| 项目 | 上次结论 | 本次复核 | 状态变化 |
|------|---------|---------|---------|
| **Apache Jena Fuseki** | Apache-2.0, v5.x, 持续更新 | 确认：v5.x 持续维护，GitHub apache/jena 活跃 | ✅ 无变化 |
| **Oxigraph** | Apache-2.0/MIT, v0.5.11, 1876★ | 确认：Rust 单二进制，无 OWL 推理 | ✅ 无变化 |
| **GraphDB Free** | 专有免费层，OWL 2 RL 推理 | 确认：仍为商业产品免费版 | ✅ 无变化 |
| **Blazegraph** | 2026-03 归档废弃 | 确认：GitHub 已归档 | ✅ 无变化 |
| **mcp-go** | MIT, v1.1.0, ~9000★ | 确认：Go MCP SDK，需自建 SPARQL 薄层 | ✅ 无变化 |

**MCP/GraphRAG 结论**：直接引用已有报告——Open Ontologies (Rust) 是唯一开箱即用的本体 MCP Server；mcp-go 是 Go 生态推荐路线但需自建工具层；主流 GraphRAG 工具不从 OWL/RDF 原生抽取。

### 3.2 B 类补充项目

| 项目 | 仓库/主页 | 许可证 | 最新版本/日期 | 部署形态 | 推理支持 | 性能量级 | 活跃度 |
|------|---------|--------|-------------|---------|---------|---------|--------|
| **Eclipse RDF4J Server** | [eclipse/rdf4j](https://github.com/eclipse/rdf4j) | EPL-2.0 | v6.1.0 (2026-09-12) | Java 库/Server | ✅ RDFS + 可接入推理器 (Sail) | 取决于后端 | ★★★★★ Java 25 基线，RDF 1.2/SPARQL 1.2 |
| **Virtuoso OSE** | [openlink/virtuoso-opensource](https://github.com/openlink/virtuoso-opensource) | GPL v2 | 持续维护 | 二进制/Docker/源码 | RDFS + 有限 OWL | 10⁹+（DBpedia） | ★★★ 成熟但文档古老 |
| **QLever** | [ad-freiburg/qlever](https://github.com/ad-freiburg/qlever) | Apache-2.0 | 持续更新 (2026) | C++ 二进制/Docker | ❌ 无 OWL 推理 | **万亿级**（单机） | ★★★★ 被 Wikimedia 评估替代 Blazegraph |
| **RDFLib** | [RDFLib/rdflib](https://github.com/RDFLib/rdflib) | BSD-3 | v7.6.0 (2026-02) | Python 库 | ❌ 无内置推理 | 内存型，中小规模 | ★★★★ Python 标准 RDF 工具 |
| **Owlready2** | [pwin/owlready2](https://github.com/pwin/owlready2) | LGPL v3 | v0.51 (2026-06) | Python 库 | ✅ HermiT/Pellet 内置 | SQLite 后端，10⁸ 级 | ★★★★ Python 本体编程首选 |
| **pySHACL** | [RDFLib/pySHACL](https://github.com/RDFLib/pySHACL) | Apache-2.0 | 持续更新 | Python 库 | ✅ SHACL 验证 | N/A（验证工具） | ★★★ 数据合规验证 |
| **Ontop** | [ontop/ontop](https://github.com/ontop/ontop) | Apache-2.0 | v5.5.0 (2026-02) | jar/Docker | OWL 2 QL（查询重写） | 虚拟知识图谱 | ★★★★ 活跃学术项目 |

**Eclipse RDF4J 详细说明**：v6.1.0（2026-09-12）是最新版本，基线已升级至 Java 25，支持 RDF 1.2 和 SPARQL 1.2 标准。RDF4J Server 提供完整的 SPARQL 端点，通过 Sail 接口可接入各种推理器。作为 Eclipse 基金会项目，长期维护有保障。RDF4J 与 Jena 的主要差异在于：RDF4J 更偏"企业级嵌入"，Jena 更偏"完整 triplestore + 推理" [(rdf4j.org)](https://rdf4j.org/news/2026/09/12/rdf4j-6-1-0-released/)。

**QLever 详细说明**：QLever 是德国弗莱堡大学的 C++ RDF 引擎，Apache-2.0 许可证。单机可扩展到万亿级三元组（Wikidata 166 亿三元组秒级查询）。已驱动 DBLP 和 UniProt 的官方 SPARQL 端点。Wikimedia 基金会正评估其作为 Blazegraph 的替代品（用于 Wikidata Query Service）。支持全文检索 + GeoSPARQL 在同一查询中组合。但无 OWL 推理，仅做存储与查询 [(QLever GitHub)](https://github.com/ad-freiburg/qlever) [(qlever.dev)](https://qlever.dev/)。

### 3.3 Fuseki Docker 生态

Fuseki 的部署形态已有成熟的 Docker 生态：

```bash
# 官方推荐 Docker 镜像
docker run -d -p 3030:3030 stain/jena-fuseki

# 或使用 Apache 官方镜像
docker run -d -p 3030:3030 apache/jena-fuseki:latest

# 创建命名数据集 + 加载本体
curl -X POST http://localhost:3030/$/datasets --data 'dbType=tdb2&dbName=k8s_ontology'
curl -X POST -H 'Content-Type: text/turtle' --data-binary @fluidos.ttl \
  http://localhost:3030/k8s_ontology/data
```

社区还有 `stain/jena-fuseki` 镜像（含预配置）和各种 `docker-compose.yml` 模板。Fuseki 本身作为 SPARQL 1.1 完整端点，可直接被 mcp-go 通过 HTTP POST 调用。

---

## 四、全生命周期工具链表

| 环节 | 首选开源项目 | 备选 | 说明 |
|------|------------|------|------|
| **构建/编辑** | Protégé Desktop | WebProtégé（多人协作）、Owlready2（Python 编程）、OWLAPY（Python+LLM） | 核心建模工具 |
| **协作编辑** | WebProtégé | VocBench3（术语管理） | 唯一成熟的 Web 协作编辑器 |
| **CLI/CI 校验** | ROBOT | dosdp-tools | 格式转换、一致性检查、模块提取 |
| **项目脚手架** | ODK | — | OBO 标准项目模板 |
| **推理验证** | HermiT（OWL 2 DL）/ ELK（OWL 2 EL） | Openllet（Pellet fork） | 一致性与分类推理 |
| **质量检查** | FOOPS!（FAIR）+ OOPS!（缺陷） | pySHACL（SHACL 验证） | 本体质量保障 |
| **文档生成** | Widoco | LODE、ontoink（MkDocs） | HTML 文档自动生成 |
| **自动发布** | ontoology（GitHub → Pages） | — | 推送即发布 |
| **可视化建模** | Chowlk（draw.io → TTL） | draw.io 手动绘图 | 降低建模门槛 |
| **可视化浏览** | WebVOWL / OntoGraf（Protégé 插件） | Microsoft Ontology Playground、OntView | 参见 [可视化工具报告] |
| **程序化操作** | OWL API（Java）、Owlready2（Python）、RDFLib（Python） | Apache Jena（Java） | 库形态基础设施 |
| **AI 辅助构建** | OWLAPY（Python+LLM）、IDEA2（CQ 提取） | AWS COA（AWS 绑定）、HyDRA | 2023+ 新工具 |
| **运行/存储** | Apache Jena Fuseki | Oxigraph（轻量）、QLever（万亿级） | SPARQL 端点 + 持久化 |
| **运行时推理** | Fuseki + HermiT | GraphDB Free（OWL 2 RL）、RDF4J Server | 推理 + 查询 |
| **服务化/MCP** | mcp-go 自建薄层 → Fuseki | Open Ontologies（Rust） | Go/Eino 技术栈推荐 mcp-go |

---

## 五、K8s 运维本体场景建议组合

### 5.1 推荐工具链

针对用户场景（K8s/云边运维本体，Turtle/OWL/XML 混合，45–108 类，需要团队协作与版本管理，Go/Eino + MCP 技术栈）：

| 阶段 | 工具 | 理由 |
|------|------|------|
| **日常编辑** | Protégé Desktop | 功能最全，直接打开 .ttl/.owl，HermiT 推理验证 |
| **团队协作** | WebProtégé (Docker) | 多人在线编辑同一本体，修订历史 + 版本回滚 |
| **版本管理** | Git + ROBOT CI | .ttl/.owl 文件用 Git 管理；ROBOT 做 CI 一致性校验 |
| **推理验证** | HermiT (via Protégé) | OWL 2 DL 完整推理，subClassOf 推导 |
| **质量保障** | FOOPS! + pySHACL | FAIR 评估 + SHACL 约束验证 |
| **文档发布** | Widoco + ontoology | 自动生成 HTML 文档，推送 GitHub 自动发布 |
| **运行服务** | Fuseki Docker | SPARQL 端点 + RDFS/OWL 推理 + TDB2 持久化 |
| **MCP 服务化** | mcp-go 自建薄层 | Go/Eino 原生集成，HTTP 调用 Fuseki SPARQL |
| **AI 辅助（可选）** | OWLAPY | Python LLM 辅助生成初始本体草案 |

### 5.2 排除项与理由

| 项目 | 排除理由 |
|------|---------|
| **TopBraid Composer/EDG** | 纯商业产品，不再开源 |
| **Blazegraph** | 2026-03 归档废弃 |
| **Stardog** | 纯商业软件 |
| **RDFox** | 纯商业许可 |
| **GraphDB Free** | 专有许可免费层，有数据规模限制 |
| **AWS COA** | 深度绑定 AWS 生态（Neptune/Bedrock），不适合用户场景 |
| **QLever** | 无 OWL 推理，适合万亿级查询而非运维本体服务化 |
| **Virtuoso OSE** | GPL 许可 + 文档古老 + 运维复杂 |
| **LD-VOWL** | 停更 5+ 年 |
| **knakk/rdf** | Go RDF 库，低活跃/疑似废弃 |

### 5.3 已知差距

1. **本体 → GraphRAG 的链路缺口**：当前主流 GraphRAG 工具不从 OWL/RDF 原生抽取，本体 schema 利用度接近零。参见 [本体运行时报告 §4.2](file:///Coze/Drive/本体学习/本体运行时与MCP服务开源方案调研_report.md)。
2. **WebProtégé 无推理**：WebProtégé 不提供推理功能，需要与 Protégé Desktop 或 Fuseki 配合使用。
3. **Go 生态 RDF 库匮乏**：mcp-go 方案需自建 SPARQL HTTP 客户端，但逻辑简单（SPARQL 协议是标准 HTTP POST）。
4. **AI 辅助工具成熟度**：OWLAPY 的 LLM 生成本体功能仍需后处理验证；IDEA2 仅覆盖 CQ 提取阶段。

---

## 六、数据来源说明

本报告主要信息来源包括：
- **GitHub 仓库**：各项目的 releases、commits、README（查证日期：2026-09-20）
- **官方文档**：protege.stanford.edu、jena.apache.org、rdf4j.org、obofoundry.org
- **PyPI/Maven**：rdflib、owlready2、owlapy、robot 版本信息
- **学术论文**：OWLAPY (arXiv:2511.08232)、IDEA2 (arXiv:2604.01344)、GrOIL (arXiv:2608.22134)
- **AWS 公告**：aws.amazon.com/about-aws/whats-new/2026/07/
- **已有报告**：本体运行时与MCP服务开源方案调研 (2026-09-18)、开源本体可视化工具调研 (2026-09-18)
- **技术博客**：qiita.com、CSDN 上的工具使用教程与评测

---

*报告生成时间：2026-09-20 ｜ 调研范围截至：2026-09-20*
