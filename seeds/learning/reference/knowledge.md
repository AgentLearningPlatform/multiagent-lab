---
module: 知识库
req: [REQ-50~59, REQ-107, REQ-127, REQ-128, REQ-129, REQ-130]
docs: ["11 全文", "12 全文", "12 §3.5"]
decisions: [D-KB1, D-KB2, D-KB3, D-KB4]
synced: 2026-09-24
---

# 知识库（Knowledge Base）

## 产品定位

知识库是主平台内的全局资源：文档入库 → 切分 → 向量化 → 检索 → 对话注入的完整 RAG 闭环，并拆出 **RAG 检索**与 **GraphRAG 检索**两个子模块（同一模块页内 Tab 切换，共享 chunk 池，按 `kb.mode` 区分）。

GraphRAG 子模块的差异化价值：向量检索擅长"语义相似的片段"，知识图谱擅长"**多跳关联与溯源**"——谁依赖谁、为什么、依据在哪段原文。两类检索并存且可对照，正是检索策略教学的核心场景（SC-K5）。

## 设计原理

### RAG 管道

txt/md/粘贴文本 → 固定长度+重叠切分（可配置）→ embedding 向量化 → 存入向量库；提问时检索 TopK 片段注入上下文，`retrieval` 事件把来源与得分打在对话时间线上。

### 向量存储可插拔

`KB_VECTOR_BACKEND=qdrant|sqlite` 一键切换——Qdrant（单容器 REST）为主，SQLite 余弦检索为无外部依赖 fallback（学习对照）；同一库在两种后端下检索口径一致。

### GraphRAG：导入即建 KG（已交付）

- 文档索引后自动把 chunks 抽取为**自存知识图谱**：LLM 抽取为主路径（复用平台模型代理），失败回退规则抽取，再失败标记 degraded——不阻断索引与检索。
- SQLite 三表：`kg_entity`（实体）/ `kg_relation`（关系）/ `kg_claim`（论断，带 chunk_id 溯源到原文）。
- **三步检索（local）**：向量命中片段 → 经 KG 一跳扩展关联实体 → 汇出关系与 claims；KG 空或无命中回退纯向量标 degraded。
- `POST /api/kg/{id}/rebuild` 可整库重建 KG。

### KG 增强（REQ-127~130，M16 排期中）

- **REQ-127 图谱浏览与统计（P1）**：graphrag 库新增「图谱」视图——实体搜索、React Flow 邻域展开、claims 溯源原文、统计卡（实体/关系/claims/方法分布）。
- **REQ-128 实体聚焦与多跳（P1）**：从指定实体出发遍历、跳数 1~2 可配置（SQLite 递归 CTE）、关系类型过滤、claims 原文高亮。
- **REQ-129 抽取治理（P2）**：库级抽取模型/提示词覆写、实体消歧合并（仅人工触发）、关系/claims 审核队列（rejected 不参与检索）、质量面板。
- **REQ-130 社区摘要与全局问答（P2）**：label propagation 社区检测 + LLM 摘要按需缓存，回答"这个库整体在讲什么"类全局问题（对标微软 GraphRAG global search）。

### 使用场景（怎么选检索）

| 场景 | 用什么 | 原因 |
| --- | --- | --- |
| 语义相似片段（"讲讲 X 的原理"） | RAG | 片段级语义召回 |
| 依赖/关系遍历（"A 依赖哪些组件"，SC-K1） | GraphRAG 多跳 | 关系链路比片段拼接完整 |
| 结论溯源（"为什么出现 X"，SC-K2） | GraphRAG claims | 每条论断可点回原文 |
| 关系网络浏览（会议纪要人物网络，SC-K3） | REQ-127 图谱视图 | 图比列表直观 |
| 全局主题（"这个库在讲什么"，SC-K4） | REQ-130 社区摘要 | 片段检索回答不了全局问题 |
| 检索策略对照学习（SC-K5） | 同问 RAG vs GraphRAG | 复用对话对比模式 |

## 相关资料

- `docs/11_知识库_需求文档.md` —— 需求事实源（REQ-50~59/107/127~130、场景 SC-K1~K5、开放问题 KB-O1~O4）
- `docs/12_知识库_方案设计.md` —— 设计事实源（§3 双子模块管道、§3.5 KG 增强设计、§5 开源选型对比）
- [Qdrant](https://qdrant.tech/documentation/) —— 向量数据库（P1 首选，单容器）
- [eino-ext 组件库](https://github.com/cloudwego/eino-ext) —— Eino 生态模型/检索组件
- [微软 GraphRAG 论文](https://arxiv.org/abs/2404.16130) —— From Local to Global：社区摘要与全局问答的原始设计（REQ-130 对照）
- [WeKnora](https://github.com/Tencent/WeKnora) / [RAGFlow](https://github.com/infiniflow/ragflow) —— 平台级 RAG 成品（双轨备选，D-KB2）
