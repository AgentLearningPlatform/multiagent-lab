# 本体（Ontology）

## 产品定位

本体主线是平台的 B 线北极星：**本体的构建 → 运行 → 使用 → 审计**全流程学习。本体模块左侧边栏五栏——学习中心（默认页，方法论/任务卡/路径对照）/ 本体构建（六条路径）/ 本体资产（统一管理）/ 本体运行（多引擎方案）/ 消费与审计（KG 增强检索 + 决策溯源）。

## 设计原理

- **构建与运行解耦**：本体仓库是唯一事实源；运行平面同时跑多套"运行方案"（引擎 + 本体集合 + 端口），方案停止不影响仓库资产。
- **spec_json 多形态资产**：自建本体以 spec_json 为主形态（概念/关系/实例/属性），可校验、可视化（React Flow 画布）、diff，并经 TTL 导出器装载进 RDF 引擎；外部 OWL/TTL 则原样导入直跑（有损/无损分层）。
- **六条构建路径**（来源 × 交互形态正交，殊途同归收敛到资产段）：手工向导 / OntoChat 多轮对话 / 由知识库构建（chunk→LLM 抽取 / KG 直转 / 混合三策略）/ Open Ontologies 引入 / OntoExtend 方法论引导 / ~~semantica~~（已退役）。
- **运行方案与 MCP facade**：Runtime Manager 统一编排引擎（Oxigraph 为主，Fuseki 做推理对照）；对 Agent 暴露稳定的 4 个只读 onto_* 工具（内部翻译为 SPARQL），对话挂载运行方案即可做结构化查询。
- **TTL 即 KG**：本体资产 TTL 直接装载为知识图谱源；知识库侧对任意文本可 LLM 抽取建 KG（SQLite 三表：实体/关系/claim），抽取失败规则回退、再失败标记降级不阻断。
- **消费与审计**：检索命中展示"向量召回 → KG 一跳扩展"；Agent 的本体调用落决策审计表，支持 PROV-O 溯源导出。

## 相关资料

- `docs/03_本体_需求文档.md` / `docs/04_本体_方案设计.md` —— 本体模块需求与方案事实源
- `docs/19_本体_semantica集成方案.md` —— 已退役路线的设计存档（对照学习用）
- [Protégé](https://protege.stanford.edu/) —— 业界标准本体编辑器（七阶段 S2 的参照物）
- [Apache Jena Fuseki](https://jena.apache.org/documentation/fuseki2/) —— SPARQL 服务与 OWL 推理
- [Oxigraph](https://oxigraph.org/) —— 内嵌 SPARQL 引擎（P1 默认引擎）
- [React Flow](https://reactflow.dev/) —— 可视化画布库
- [FOAF 词表](http://xmlns.com/foaf/spec/) —— 复用型词表示例（组织人员本体）
