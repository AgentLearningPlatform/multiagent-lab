---
module: 本体
req: [REQ-60~73, REQ-74, REQ-78, REQ-80~88, REQ-90~96, REQ-104, REQ-107, REQ-108, REQ-109, REQ-110]
docs: ["03 全文", "04 §3/§4", "12 §3.5", "14 前端改造", "17 §2"]
decisions: [D-O1, D-O3, D-O5, D-O8, D-O11, D-O12, D-O14, D-O15, D-O16]
synced: 2026-09-24
---

# 本体（Ontology）

## 产品定位

本体主线是平台的 **B 线北极星：本体的构建 → 运行 → 使用 → 审计** 全流程学习。定位澄清（D-O11）：本体模块 = **学习各种本体构建方式、运行方式的模块**——不是单一工具，而是把业界多种路线做成可上手对照的"路径陈列馆"。

模块左侧边栏**五栏**（每栏的定位与关键能力）：

| 栏 | 定位 | 你能做什么 |
| --- | --- | --- |
| **学习中心**（默认页） | 学习动线主入口 | 七阶段学习路径条（S1→S7 理论骨架）、建模方法论卡片、任务卡打卡、构建方式六路径对照、运行方式引擎对照、外部资源导航、工具链配置 |
| **本体构建** | 六条路径生成本体 | 按来源分类的构建入口（详见设计原理），未工程化的路径显示引导卡而非空壳 |
| **本体资产** | 全部已构建本体的统一管理 | 列表 + 详情工作区：Spec 编辑 / 校验 / 版本（源码视图、diff）/ 产物 / 可视化（React Flow 画布）/ TTL 导出 |
| **本体运行** | 按运行方式分组的方案管理 | 引擎分组（Oxigraph / Fuseki 推理对照 / Open Ontologies 引导 / Cayley 占位）、方案向导、启停、端点查看 |
| **消费与审计**（第五栏） | 本体被消费的观测台 | KG 增强检索展示、决策审计表与溯源链、PROV-O 导出、知识图谱可视化 |

## 设计原理

### 构建与运行解耦（D-O1，架构级原则）

本体仓库是**唯一事实源**；运行平面同时启动多套"运行方案"（引擎 + 本体集合 + 配置），方案停止/删除不影响仓库资产。反过来说：资产保存新版本后，运行方案需要在方案页**显式重载**才生效（P1 统一语义，页面有固定提示）——这是刻意保留的教学点：让你看见"资产"与"运行态"的区别。

### spec_json 多形态资产

- **主形态 spec_json**：概念（Concept）/关系（Relation）/实例（Instance）/属性 + 能力问题（CQ），人类可读、可 diff、可校验、可画图。
- **导入格式分层**：spec 无损 / CSV 实例灌装 / GraphML / OWL-RDF 有损导入——真实世界资产多为 OWL/TTL，"导入即保留原始形态"是硬要求（原始形态不被破坏，SPARQL 可直接命中）。
- **TTL 导出**：spec_json 经转换器导出 Turtle，使自建本体可以装载进 RDF 引擎（导入/导出经系统级 Python sidecar rdflib，无需 venv）。
- **双形态编辑**：JSON 编辑器与图形编辑器（React Flow 画布 + RJSF 属性表单）并存切换，画布数据经适配器转回 spec_json，**校验管线只有一条**；图形编辑支持概念/实例双类节点与归属关系（v1.5）。
- **版本能力**：保存即新版本；版本 diff（概念/关系/实例增删改统计）、TTL 源码只读视图（按格式选渲染）。

### 六条构建路径（来源 × 交互形态，殊途同归）

| # | 路径 | 来源 | 形态 | 状态 |
| --- | --- | --- | --- | --- |
| 1 | 自定义构建 | 人类手工 | 表单向导（S1~S4 四步） | ✅ 可用（教学基准） |
| 2 | OntoChat 流程 | AI 对话生成 | 多轮 CQ 引导 → 草稿 → 校验循环 | ◐ 部分可用 |
| 3 | 由知识库构建 | 知识资产转化 | 选库 → 选策略 → 草稿 → 校验 | ◐ 部分可用 |
| 4 | Open Ontologies 流程 | 外部本体引入 | 引导卡 + 独立工作台 + 产物回流 | ◐ 引导 + 手工导入 |
| 5 | OntoExtend 流程 | 方法论参照 | 引导卡（不产本体） | ○ 引导先行 |
| 6 | ~~semantica 流程~~ | ~~KG 消费~~ | — | ✕ 已退役（D-O15） |

- **三策略（路径 3）**：A = KB chunk 池 → LLM 抽取 spec_json（复用平台模型代理，CQ 引导约束粒度）；B = GraphRAG 的 KG → 直转映射（entity→Concept / relation→Relation / claim→Attribute 薄层）；C = 混合（KG 作初稿 + LLM 补全）。生成草稿后必须走统一校验循环，预览由用户确认才入库。
- **学习价值设计**：路径间按"来源"正交，同一示例本体可以走任意路径生成再交叉对照——学习中心"同一示例 × 六路径"对照卡就是核心教具。所有路径生成之后**收敛到同一段资产能力**（校验→可视化→版本→运行→消费），后半程技能完全迁移。

### 学习中心（七阶段 + 方法论 + 任务卡）

- **七阶段理论骨架**（REQ-74）：S1 来源 → S2 编辑 → S3 校验 → S4 可视化 → S5 运行 → S6 服务化 → S7 对接 Agent——它就是"统一收敛链路"的概念化表达；每阶段登记主流开源工具，按 builtin（内置直接用）/ guided（生成指引+打卡）/ managed（平台自动编排）三种执行模式。
- **方法论卡片 + 任务卡**（REQ-90/91）：五模块方法论（含深度版）配可运行示例（4+1 个示例本体随 seeds 交付，每份附建模说明：背景/CQ/决策/局限）；任务卡以 `task:`/`tool:` 双 key 打卡，"故意制造不一致观察推理机报什么错"是官方玩法（试错安全原则）。
- **外部资源导航**（REQ-109）：在线平台/复用词表/官方规范/开源项目四节，内容来自仓库随版本维护的 md，编辑提交后刷新即生效。

### 运行方案与 MCP facade

- **Runtime Manager（:8090）**统一编排运行方案；引擎清单：**Oxigraph**（P1 默认，内嵌 SPARQL）、**Fuseki**（推理对照：同查询并排 diff 推理结果）、Open Ontologies 引导页、Cayley 占位（P2 可选）。
- **对 Agent 的契约稳定不变**：无论底层哪个引擎，Agent 侧只看到 4 个只读 `onto_*` 工具（get_concept / get_instance / list_instances / neighbors），入参必带 ontology_id，内部翻译为 SPARQL——主平台与对话无需感知引擎差异。
- **对话挂载**：对话输入区「本体」chip 选择 **running** 状态的方案（存 runtime_profile_id）；方案停止 → 明确降级提示且不影响普通对话与知识库。onto_* 调用的翻译后 SPARQL 与耗时可在运行方案调试面板透视（REQ-94 翻译透视）。
- **SPARQL 工作台**（Yasgui）：选中某 running 方案直接执行 SELECT，是学习"本体查询长什么样"的最短路径；配合源码视图（TTL 原文）建立"结构 ↔ 查询"的映射感。

### 知识图谱：TTL 即 KG + LLM 抽取

- **两条 KG 来源**：①本体资产的 TTL 直接装载（"TTL 即 KG"）；②任意文本 chunk 集合用 LLM 抽取建 KG（复用平台模型代理；失败自动回退规则抽取，再失败标记 degraded 不阻断）。
- **自研轻量实现**：SQLite 三表（kg_entity / kg_relation / kg_claim，claim 带 chunk_id 溯源）；API 面 `/api/kg/{id}`、`/api/kg/{id}/rebuild`。
- **GraphRAG 三步检索**：向量命中片段 → 经 KG 一跳扩展关联实体 → 汇出关系与 claims；KG 空或无命中时回退纯向量并标记 degraded。知识库侧（GraphRAG 子模块）与本侧共用这套 KG。

### 消费与审计（第五栏）

- **消费**：检索命中展示"向量召回 → KG 一跳扩展"的全过程，让学习者看见"图谱到底多给了什么"。
- **审计**：Agent 的本体查询落 SQLite 决策审计表，支持决策链 Timeline、溯源 Drawer 与 **PROV-O 导出**（Go 原生 Turtle 模板，零 Python 依赖）——回答"这条知识/这次结论从哪来"。

### 一个重要的架构教训：去 semantica 化（D-O15）

本项目曾规划接入 semantica 平台承担 KG 与审计，主人实测后拍板反转：worker 重依赖（torch 系 venv 数 GB）启动困难，防御式适配成本失控，而核心学习链路其实零依赖它。结论是**用轻量自研等价实现**（上文的 internal/kg + 审计表），semantica 转为对照参考（设计存档见 19 号）。这是"开源优先 ≠ 无条件集成"的活案例：**当集成成本超过自研轻量实现的成本时，换路线正是同一原则的延续**。

## 相关资料

- `docs/03_本体_需求文档.md` —— 需求事实源（REQ 全表 + D-O1~16 决策 + 验收 22 条）
- `docs/04_本体_方案设计.md` —— 方案设计（构建平面/运行平面/前端结构）
- `docs/19_本体_semantica集成方案.md` —— 退役路线设计存档（对照学习：为什么反转）
- `docs/14_本体_前端改造方案.md` —— 本体前端结构与改造史
- `docs/17` §2 / §7 —— 本体模块 IA 与 J2/J3 动线
- `research/` —— OntoChat/OntoExtend/open-ontologies 等路线的立项前调研
- [Protégé](https://protege.stanford.edu/) —— 业界标准本体编辑器（S2 编辑阶段的参照物）
- [W3C OWL](https://www.w3.org/OWL/) / [SPARQL](https://www.w3.org/SPARQL/) —— 本体语言与查询规范
- [Oxigraph](https://oxigraph.org/) —— P1 默认 SPARQL 引擎 ｜ [Apache Jena Fuseki](https://jena.apache.org/documentation/fuseki2/) —— 推理对照引擎
- [React Flow](https://reactflow.dev/) —— 可视化画布库 ｜ [RJSF](https://rjsf-team.github.io/react-jsonschema-form/docs/) —— Schema 驱动属性表单
- [FOAF 词表](http://xmlns.com/foaf/spec/) —— 复用成熟词表的示例（组织人员本体）
- [open-ontologies](https://github.com/fabio-rovai/open-ontologies) —— 双轨路线的宿主项目（Rust 单二进制 + Oxigraph）
