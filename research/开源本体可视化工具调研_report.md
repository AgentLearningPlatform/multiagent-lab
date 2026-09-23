# 开源本体可视化工具选型调研报告

> 调研日期：2026-09-18 | 调研范围：截至2026年9月全球开源项目 | 目标场景：FLUIDOS/Kubernetes/HOCC 三类 OWL/RDF 本体（45–108 类，含继承与实例）的零开发可视化

## 核心结论

**有可用方案，但需区分"开箱即用"的程度。** 对于"打开网页、拖入本地 .ttl/.owl 即看图、零编码"这一最严格定义，截至 2026 年 9 月，**最匹配的方案是 WebVOWL（在线版或 Docker 自托管）**——它实现了 VOWL 标准视觉表示法，能同时展示类层次(subClassOf)、对象属性(domain→range)、数据类型属性，且已有意大利 ISTAT 维护的集成 fork 将 OWL2VOWL 转换器与前端合一。若接受"需要 Java 环境但桌面端一键启动"，**Protégé Desktop + OntoGraf 插件**是最成熟的全功能方案，支持推理、类层次、属性关系、个体，且许可证宽松 (BSD)。2026 年新出现的 **Microsoft Ontology Playground**（MIT，纯静态站）虽然零后端、可直接部署 GitHub Pages，但其定位是 Fabric IQ 学习工具，对复杂 OWL 构造（匿名类、推理、ABox）的支持深度有限，更适合作为轻量展示而非研究级分析。

**不存在完美方案**——所有工具在本场景（中等规模 OWL/RDF 本体、需同时看 TBox 与 ABox）下都有短板：WebVOWL 不展示个体(ABox)且需 Java 预处理；Protégé 是桌面应用非纯 Web；OntView(2025)功能强大但 ABox 可视化尚未实现。若追求"同时展示类层次+对象属性+个体+可离线+零开发+纯 Web"，**目前无单一工具完全满足，仍需自建方案或对现有工具做轻量定制**。

---

## 1. 工具全景与分类

本报告将候选工具按**产品形态**分为四类，并明确标注每类与本场景的距离：

| 类别 | 代表工具 | 与本场景距离 | 本报告处理 |
|------|----------|-------------|-----------|
| **A. 纯本体检视器** — 核心功能就是可视化本体结构 | WebVOWL, OntView, LD-VOWL | 最契合 | 重点评估 |
| **B. 本体编辑器(含可视化)** — 编辑为主，可视化是辅助 | Protégé(Desktop+插件), WebProtégé, OrionBelt, VocBench3 | 可行但非纯视图 | 重点评估 |
| **C. 通用 RDF/KG 可视化器** — 面向 SPARQL 端点或任意 RDF 图 | LodLive, LodView, Ontodia, KinGVisher, KGviz | 需 SPARQL 端点或非 OWL 专用 | 简要评估 |
| **D. 文档生成/嵌入工具** — 生成静态文档或嵌入 MkDocs | ontoink, Ontospy, WIDOCO/LODE | 非交互式或需文档框架 | 简要评估 |
| **E. 教育/展示平台** — 面向学习场景 | Ontology Playground | 轻量展示可行 | 重点评估 |
| **F. 排除项** — 商业/不活跃/不适用 | TopBraid(商业), Knotation(未找到), LD-VOWL(停更), Fuseki(非可视化器), OLS(生物医学专用) | 不符合约束 | 明确排除 |

---

## 2. 重点候选工具详细规格

### 2.1 WebVOWL + OWL2VOWL

| 维度 | 详情 |
|------|------|
| **官方主页** | https://service.tib.eu/webvowl/ (在线 demo) |
| **仓库** | https://github.com/VisualDataWeb/WebVOWL |
| **许可证** | MIT [(VisualDataWeb)](https://github.com/VisualDataWeb/WebVOWL) |
| **最新版本** | v1.1.6 (2019-10-08)，仓库活跃更新至 2025-01 |
| **GitHub 指标** | ~817 stars, 242 forks, 62 open issues [(repositorystats)](https://repositorystats.com/visualdataweb/webvowl) |
| **技术栈** | 前端 JavaScript (D3.js 力导向图)；后端转换需 OWL2VOWL (Java, Spring Boot, OWL API) |
| **输入方式** | ①在线版：上传 .owl/.ttl → 自动调用 OWL2VOWL 转换 → 展示 ②自托管：需先运行 OWL2VOWL 生成 JSON → 前端加载 JSON |
| **支持格式** | OWL/RDF (通过 OWL2VOWL 转换)，支持 Turtle, RDF/XML, OWL/XML |
| **可视化能力** | ✅ 类层次(subClassOf) ✅ 对象属性 domain→range ✅ 数据类型属性 ✅ 集合运算(union/intersection/complement) ⚠️ 推理(仅 OWL2VOWL 提取的隐式公理) ❌ 个体(ABox)展示有限 |
| **交互能力** | 点击节点看详情、过滤、搜索、缩放、多种布局切换 |
| **是否需写代码** | 在线版：零代码；自托管：需 npm build + Java 环境 |
| **离线部署** | Docker: `docker build . -t webvowl:v1 && docker-compose up -d` → http://localhost:8080 [(GitHub README)](https://gitmemories.com/index.php/VisualDataWeb/WebVOWL) |
| **意大利 ISTAT fork** | 集成 OWL2VOWL + WebVOWL 为一体，活力指数 85% [(EU OSS Catalogue)](https://interoperable-europe.ec.europa.eu/eu-oss-catalogue/solutions/webvowl) |

**优势**：VOWL 是 W3C 社区认可的本体视觉表示标准，图形语义丰富（不同形状区分 class/property/datatype）；在线 demo 可零配置试用；MIT 商用友好；学术引用广泛（2014 年至今持续被引用）。

**劣势**：最终发布版本 2019 年，核心代码更新缓慢；OWL2VOWL 依赖 Java 预处理步骤（非纯前端"拖文件即看"）；对大规模本体性能未经验证（100+ 类可能布局拥挤）；个体展示能力弱。

**本场景契合度**：⭐⭐⭐⭐ (4/5) — TBox 可视化最佳选择，但 ABox 展示不足。

---

### 2.2 Protégé Desktop + OntoGraf/OWLViz 插件

| 维度 | 详情 |
|------|------|
| **官方主页** | https://protege.stanford.edu/software/ |
| **仓库** | https://github.com/protegeproject (桌面); OntoGraf: https://github.com/protegeproject/ontograf |
| **许可证** | BSD-2-Clause (Protégé), BSD (OntoGraf) [(protege.stanford.edu)](https://protege.stanford.edu/software/) |
| **最新版本** | Protégé Desktop v5.6.9 (社区维护) |
| **技术栈** | Java (Swing/OSGi), 跨平台 (Windows/Mac/Linux) |
| **输入方式** | 打开本地 .owl/.ttl/.rdf 文件 |
| **支持格式** | RDF/XML, Turtle, OWL/XML, OBO, Manchester Syntax 等 |
| **可视化能力** | ✅ 类层次(subClassOf) ✅ 对象属性 domain→range ✅ 个体(individuals) ✅ 推理(HermiT/Pellet) ✅ 断言 vs 推断区分 |
| **插件** | OntoGraf: cube/tree/vertical tree 布局；OWLViz: 类似 WebVOWL 的 VOWL 视图 |
| **是否需写代码** | 零代码（安装后直接打开文件） |
| **离线部署** | 下载 installer → 安装 → 需要 JRE |
| **推理支持** | ✅ HermiT, Openllet(Pellet fork) — 直接内存连接 |

**优势**：功能最全面的开源本体工具；推理器直接集成；插件生态成熟；社区活跃（邮件列表持续更新）；能同时看 TBox + ABox + 推理结果。

**劣势**：桌面应用（非 Web）；Java GUI 界面相对老旧；需要安装 JRE；不适合"打开浏览器即用"的轻量展示场景；OntoGraf 布局对 100+ 类的本体可能拥挤。

**本场景契合度**：⭐⭐⭐⭐⭐ (5/5) — 功能最全面，但非 Web 形态。

---

### 2.3 Microsoft Ontology Playground

| 维度 | 详情 |
|------|------|
| **官方主页** | https://microsoft.github.io/Ontology-Playground |
| **仓库** | https://github.com/microsoft/Ontology-Playground |
| **许可证** | MIT [(everydev.ai)](https://www.everydev.ai/tools/ontology-playground) |
| **最新版本** | Preview（创建于 2026-02，最后更新 2026-07） |
| **GitHub 指标** | 1,209 stars, 193 forks |
| **技术栈** | React 19 + TypeScript 5 + Cytoscape.js (fcose 布局) + Vite |
| **输入方式** | 导入 .rdf / .owl 文件（RDF/XML 格式） |
| **支持格式** | RDF/XML (.rdf, .owl)；导出 RDF/XML 或 JSON |
| **可视化能力** | ✅ 类节点 ✅ 对象属性边 ✅ 交互式图探索 ⚠️ 层次布局（fcose 力导向） ⚠️ Turtle 格式需先转换 |
| **是否需写代码** | 零代码（静态站点，打开即用） |
| **离线部署** | 下载构建产物 → 任意静态文件服务器 / 本地打开 index.html |

**优势**：纯静态零后端；MIT 许可证；Cytoscape.js 渲染质量高；2026 年新项目，活跃度高；可嵌入 widget 模式。

**劣势**：Preview 状态，成熟度存疑；主要面向 Microsoft Fabric IQ 学习；**RDF/XML 是主要输入格式，Turtle(.ttl) 支持未明确**；不展示 OWL 推理结果；无 ABox 个体专门视图；VOWL 标准表示法不支持。

**本场景契合度**：⭐⭐⭐ (3/5) — 零部署、零代码最友好，但功能深度不足，TTL 支持存疑。

---

### 2.4 OntView (2025 新工具)

| 维度 | 详情 |
|------|------|
| **官方主页** | https://sid.cps.unizar.es/projects/OntView/ |
| **仓库** | https://github.com/cbobed/OntView |
| **许可证** | 开源（具体许可证类型未确认） [(arxiv)](https://arxiv.org/html/2507.13759) |
| **发表日期** | 2025-07 (论文) |
| **技术栈** | Java 17 + JavaFX + OWLAPI 5.1.* + Openllet 推理器 |
| **输入方式** | 加载 OWL 文件（通过 OWLAPI） |
| **可视化能力** | ✅ 类层次 ✅ 属性层次 ✅ 匿名类/复杂表达式 ✅ GCI (General Concept Inclusions) ✅ 推理结果 ✅ 摘要算法 |
| **推理支持** | ✅ Openllet (Pellet fork) |
| **个体支持** | ❌ 尚未实现（计划中） |
| **离线部署** | Java 应用，直接运行 |
| **性能** | DBpedia 本体 <10 秒加载 [(arxiv)](https://arxiv.org/html/2507.13759) |

**优势**：学术最新（2025），功能创新（GCI 可视化是首创）；推理集成；大本体性能好；摘要算法解决信息过载。

**劣势**：新项目成熟度低；JavaFX 桌面应用（非 Web）；个体可视化尚未实现；社区规模极小。

**本场景契合度**：⭐⭐⭐ (3/5) — TBox 可视化功能最强，但缺 ABox 且非 Web。

---

### 2.5 ontoink (MkDocs 插件)

| 维度 | 详情 |
|------|------|
| **仓库** | https://github.com/ISE-FIZKarlsruhe/ontoink |
| **许可证** | MIT [(pypi)](https://pypi.org/project/ontoink/0.7.2/) |
| **最新版本** | v0.7.2 (2026-07-15) |
| **技术栈** | Python (rdflib, pySHACL) + JavaScript (Cytoscape.js) + MkDocs |
| **输入方式** | TTL 文件（MkDocs 构建时解析） |
| **可视化能力** | ✅ 类层次(subClassOf) ✅ 对象属性 domain→range ✅ 个体(rdf:type) ✅ SHACL 约束 ✅ OWL 推理 (HermiT/Konclude WASM) |
| **特色** | 多种布局(dagre力导向/层次/树/圆)、发布级 PNG/SVG 导出、实时 TTL 编辑器、SPARQL 查询 |
| **离线部署** | Docker 镜像(MkDocs+Java+Node.js) 或 `pip install ontoink && mkdocs serve` |
| **局限** | 需要 MkDocs 框架；非独立"上传看图"工具 |

**本场景契合度**：⭐⭐⭐ (3/5) — 功能匹配度极高（同时看类/属性/个体/推理），但需要 MkDocs 文档框架。有 embeddable widget 可独立使用。

---

### 2.6 OrionBelt Ontology Builder

| 维度 | 详情 |
|------|------|
| **PyPI** | https://pypi.org/project/orionbelt-ontology-builder/ |
| **输入方式** | .ttl, .owl/.rdf, .nt, .n3, .jsonld |
| **可视化** | vis-network 图 + 层次树视图 + 统计图表 |
| **部署** | `pip install orionbelt-ontology-builder` → `open http://localhost:8501` |
| **局限** | 本体编辑器为主，可视化是辅助功能 |

**本场景契合度**：⭐⭐⭐ (3/5) — 格式支持全面，但可视化能力不如专业工具。

---

## 3. 简要评估的工具

### 3.1 LodLive / LodView

| 工具 | 定位 | 与本场景契合度 | 关键限制 |
|------|------|--------------|---------|
| **LodLive** | SPARQL 端点的 RDF 图浏览器，增量探索 | ⭐⭐ (2/5) | **需要 SPARQL 端点**，不支持直接上传本地文件；不区分 OWL 语义层次 |
| **LodView** | URI 解引用浏览器，HTML 表格展示 | ⭐ (1/5) | **非图形化可视化**，是表格页面；需 SPARQL 端点 |

**结论**：两者都需要先搭建 SPARQL 端点（如 Fuseki），将 .ttl 加载进 triplestore，才能查看。对"零开发加载本地文件"场景不适用。LodView 甚至不做图形化展示。[(arxiv)](https://arxiv.org/pdf/2208.13295v3) [(dbpedia.org)](https://www.dbpedia.org/community/lodlive/)

### 3.2 Ontodia

| 维度 | 详情 |
|------|------|
| **许可证** | LGPL-2.1 (开源库) |
| **在线服务** | ontodia.org（商业版有更多功能） |
| **定位** | JavaScript 可视化库，需实现 data provider 接口 |

**结论**：ontodia.org 可上传 RDF 文件试用，但作为 JS 库，"开箱即用"程度取决于数据源适配。LGPL 许可证对商用有限制。**不符合"不用自己开发"的约束**，除非使用其在线服务。[(git.durrantlab)](https://git.durrantlab.pitt.edu/culturecreates/ontodia)

### 3.3 Apache Jena Fuseki

**结论**：Fuseki 是 SPARQL 服务器/ triplestore，**不提供内置的本体关系图可视化**。它可作为数据后端配合 WebVOWL/LodLive 使用，但本身不是可视化工具。排除。

### 3.4 EMBL-EBI OLS (Ontology Lookup Service)

**结论**：OLS4 是生物医学术语库注册和查询服务，2026-09 含 285 个本体/1080 万类。它提供层次浏览和 API，**但不是通用本体的关系图可视化器**。可自托管 (EBISPOT/OLS4) 但部署复杂且定位不匹配。排除。[(ebi.ac.uk)](https://www.ebi.ac.uk/ols4/)

### 3.5 TopBraid / Spectra

**结论**：TopBraid Composer 已归档(~2021)，EDG 7.1 有可视化面板但为**商业产品**。不满足开源要求。排除。

### 3.6 Knotation

**结论**：搜索未找到名为 Knotation 的独立开源本体可视化工具。该项目可能不存在、已更名或已不可访问。标记为 **[INFO_GAP]**。

### 3.7 LD-VOWL

**结论**：WebVOWL 的 SPARQL 端点扩展，MIT 许可证。GitHub 36 stars，**最后更新于 2020-03-18，已停止维护超过 5 年**。不推荐。[(GitHub)](https://github.org/VisualDataWeb/LD-VOWL)

### 3.8 VocBench3

**结论**：EU 资助的 Web 协作编辑平台 (BSD-3)，功能全面但**以编辑为主，可视化非核心功能**。部署较重（Java 后端 + triplestore）。对本场景"看效果"需求过度。

### 3.9 KinGVisher

**结论**：2024 ESWC 发表，MIT 许可证，Docker 可用。但**面向 SPARQL 端点的大规模 KG 可视化**，需先加载数据到 triplestore。不直接支持上传本地 .ttl/.owl 文件查看本体结构。

---

## 4. 横向对比表

下表聚焦"直接加载本地 .ttl/.owl、零开发、看类层次+对象属性+个体"这一核心需求：

| 工具 | 类型 | 许可证 | 需写代码 | 直接上传文件 | TTL 支持 | 类层次 | 对象属性 | 个体 | 推理 | 离线部署 | 活跃度 | 契合度 |
|------|------|--------|---------|------------|---------|--------|---------|------|------|---------|--------|-------|
| **WebVOWL** | Web 检视器 | MIT | 否(在线) | ✅(在线)/需转换(自托管) | ✅→JSON | ✅ | ✅ | ⚠️弱 | ⚠️仅提取 | Docker | 🟡中(2025活跃) | ⭐⭐⭐⭐ |
| **Protégé+OntoGraf** | 桌面编辑器 | BSD | 否 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅HermiT | 下载即用 | 🟢高(社区) | ⭐⭐⭐⭐⭐ |
| **Ontology Playground** | 静态 Web | MIT | 否 | ✅ | ⚠️仅RDF/XML | ✅ | ✅ | ⚠️ | ❌ | 静态文件 | 🟢高(2026) | ⭐⭐⭐ |
| **OntView** | 桌面检视器 | 开源(未确认) | 否 | ✅ | ✅ | ✅ | ✅ | ❌未实现 | ✅Openllet | Java运行 | 🟡新(2025) | ⭐⭐⭐ |
| **ontoink** | MkDocs插件 | MIT | 否(但需框架) | ✅(构建时) | ✅ | ✅ | ✅ | ✅ | ✅HermiT | Docker | 🟢高(2026) | ⭐⭐⭐ |
| **OrionBelt** | Streamlit编辑 | 待确认 | 否 | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ | pip+运行 | 🟡中 | ⭐⭐⭐ |
| **LodLive** | SPARQL浏览器 | 开源 | 需端点 | ❌需端点 | ❌ | ⚠️ | ⚠️ | ⚠️ | ❌ | 需端点 | 🔴低 | ⭐⭐ |
| **LodView** | URI浏览器 | 开源 | 需端点 | ❌需端点 | ❌ | ❌ | ❌ | ⚠️ | ❌ | 需端点 | 🔴低 | ⭐ |
| **Ontodia** | JS库/服务 | LGPL | 需集成 | ⚠️在线可试 | ⚠️ | ⚠️ | ⚠️ | ⚠️ | ❌ | N/A | 🔴低 | ⭐⭐ |
| **LD-VOWL** | SPARQL可视化 | MIT | 需端点 | ❌需端点 | ❌ | ✅ | ✅ | ⚠️ | ❌ | N/A | 🔴停更 | ⭐ |

---

## 5. 推荐排序与上手路径

### 🥇 第一推荐：WebVOWL（快速看 TBox 结构）

**适用场景**：想看 FLUIDOS/K8s/HOCC 本体的类层次和对象属性关系图

**最短路径**：
1. 打开在线版：https://service.tib.eu/webvowl/
2. 点击 "Ontology" → 选择 "Upload ontology" → 拖入 .ttl 或 .owl 文件
3. 等待 OWL2VOWL 转换完成 → 自动渲染 VOWL 图

**离线部署**：
```bash
git clone https://github.com/VisualDataWeb/WebVOWL.git
cd WebVOWL
docker build . -t webvowl:v1
docker-compose up -d
# 访问 http://localhost:8080
```
注意：自托管版本需要先将本体文件通过 OWL2VOWL 转为 JSON。

**局限**：不展示个体；需 Java 环境做预处理；100+ 类布局可能拥挤。

---

### 🥈 第二推荐：Protégé Desktop + OntoGraf（功能最全）

**适用场景**：需要同时看类层次、对象属性、个体、推理结果

**最短路径**：
1. 下载安装 Protégé Desktop：https://protege.stanford.edu/software/ (需要 JRE)
2. File → Open → 选择 .ttl 或 .owl 文件
3. 安装 OntoGraf 插件：File → Preferences → Plugins → 搜索 OntoGraf → Install
4. 切换到 OntoGraf 标签页 → 查看类关系图

**查看类层次**：OntoGraf → 选择根类 → 展开子树
**查看对象属性**：OntoGraf → Express View → "Domain-Range"
**查看个体**：Instances 标签页 → 选择类 → 查看该类的个体列表
**运行推理**：Reasoner → 选择 HermiT → Start

---

### 🥉 第三推荐：Microsoft Ontology Playground（最轻量 Web 展示）

**适用场景**：需要在内网/会议中快速展示本体结构图，零安装

**最短路径**：
1. 访问：https://microsoft.github.io/Ontology-Playground
2. 或使用离线版：clone 仓库 → npm install → npm run build → 部署 dist/ 目录

**局限**：主要支持 RDF/XML 格式；.ttl 文件可能需要先转换；功能深度有限。

---

### ⚠️ 特殊推荐：ontoink（文档+可视化一体化）

**适用场景**：需要为本体生成可发布的文档（论文/报告），同时含交互式图

**路径**：
```bash
pip install ontoink
# 在 mkdocs.yml 中配置
# 在 markdown 中写 ontoink 代码块指向 .ttl 文件
mkdocs serve
```

或使用 embeddable widget 模式嵌入任意 HTML 页面。

---

## 6. 排除项与理由

| 工具 | 排除理由 |
|------|---------|
| **TopBraid Composer/EDG** | 商业产品，非开源 |
| **LD-VOWL** | 停止维护 5+ 年 (2020) |
| **Knotation** | 搜索未找到，项目不存在或不可访问 [INFO_GAP] |
| **Apache Jena Fuseki** | SPARQL 服务器，无内置可视化 |
| **EMBL-EBI OLS** | 生物医学专用注册服务，非通用可视化器 |
| **LodLive/LodView** | 需要 SPARQL 端点，不支持直接上传本地文件 |
| **vis-network / d3 / cytoscape.js (裸库)** | 需要大量编码开发，不符合"开箱即用"约束 |
| **Ontopia** | Topic Maps 工具，非 OWL/RDF；最后发布 2026-06 但面向旧技术栈 |

---

## 7. 与本场景的差距分析

对于 FLUIDOS/K8s/HOCC 三类本体的具体需求：

| 需求 | WebVOWL | Protégé+OntoGraf | 差距 |
|------|---------|-----------------|------|
| ①subClassOf 类层次 | ✅ VOWL 标准展示 | ✅ OntoGraf 树布局 | 均可满足 |
| ②对象属性 domain→range 边 | ✅ VOWL 属性节点+角色 | ✅ OntoGraf domain-range 视图 | 均可满足 |
| ③个体 rdf:type | ❌ 几乎不展示 | ✅ Instances 面板 | WebVOWL 有明确短板 |
| ④离线/内网 | ✅ Docker | ✅ 下载即用 | 均可满足 |
| ⑤零开发 | ✅ 在线版 | ✅ 安装即用 | 均可满足 |
| ⑥Turtle (.ttl) 直接加载 | ✅(在线) | ✅ | 均可满足 |
| ⑦OWL/XML (.owl) 直接加载 | ✅ | ✅ | 均可满足 |

**核心差距**：没有单一工具同时满足"纯 Web + 零开发 + 类层次 + 对象属性 + 个体 + 离线"全部约束。如需同时展示 TBox 和 ABox 且要求 Web 形态，建议：
- 方案 A：WebVOWL 看 TBox + Protégé 看 ABox/推理（组合使用）
- 方案 B：继续使用自建单文件 HTML 浏览器（已有方案覆盖了三类边的展示）
- 方案 C：基于 ontoink 的 embeddable widget 模式做轻量定制（仍需少量配置）

---

## 8. 许可证与商用友好性

| 工具 | 许可证 | 商用友好度 |
|------|--------|-----------|
| WebVOWL / OWL2VOWL | MIT | ✅ 完全自由 |
| Protégé Desktop | BSD-2-Clause | ✅ 完全自由 |
| Ontology Playground | MIT | ✅ 完全自由 |
| ontoink | MIT | ✅ 完全自由 |
| Ontodia | LGPL-2.1 | ⚠️ 修改需开源衍生 |
| OrionBelt | 待确认 | ❓ |
| OntView | 开源(具体未确认) | ❓ |

---

## 9. 风险提示

1. **WebVOWL 核心版本老旧**：最终发布 v1.1.6 于 2019 年，虽然 2025 年仍有少量提交，但 OWL2VOWL 的依赖库（Spring Boot 1.5.6, OWL API 5.1.1）存在已知安全漏洞 [(mvnrepository)](https://mvnrepository.com/artifact/com.github.VisualDataWeb/OWL2VOWL/master-0.3.5-g788adbb-6)。用于内网研究可接受，生产部署需评估安全风险。

2. **OntView 过于年轻**：2025-07 发表，GitHub 社区极小，长期维护存疑。

3. **Ontology Playground 处于 Preview**：功能可能变化，对复杂 OWL 构造的支持未经广泛验证。

4. **LodLive/LodView/LD-VOWL 停更风险**：多年无维护，不应作为选型依赖。

---

## 10. 总结

对于"零开发加载本地 OWL/RDF 本体看可视化效果"这一需求：

- **如果只需要看类结构和属性关系（TBox）**→ WebVOWL 在线版是最快的零配置选择
- **如果需要完整功能（TBox + ABox + 推理）**→ Protégé Desktop + OntoGraf 是最成熟的方案
- **如果需要纯 Web + 零后端 + 快速分享**→ Ontology Playground 是最轻量的选择
- **如果已有自建方案且覆盖了三类语义边**→ 现有自建方案可能已经是最优解，开源工具各有短板

最终建议：**Protégé Desktop 作为日常研究分析的主力工具**（功能全、推理强），**WebVOWL 作为快速分享/演示的 Web 方案**（零配置、视觉标准），两者组合可覆盖绝大多数场景。自建 HTML 浏览器在"同时看 TBox+ABox+自定义三类边"方面仍有独特价值，建议保留作为补充。
