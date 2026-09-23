# 知识库（Knowledge Base）

## 产品定位

知识库是主平台内的全局资源：文档入库 → 切分 → 向量化 → 检索 → 对话注入的完整 RAG 闭环，并拆出 **RAG 检索**与 **GraphRAG 检索**两个子模块（同一模块页内 Tab 切换，共享 chunk 池，按 `kb.mode` 区分）。

## 设计原理

- **RAG 管道**：txt/md/粘贴文本 → 固定长度+重叠切分（可配置）→ embedding 向量化 → 存入向量库；提问时检索 TopK 片段注入上下文，`retrieval` 事件把来源与得分打在对话时间线上。
- **向量存储可插拔**：`KB_VECTOR_BACKEND=qdrant|sqlite` 一键切换——Qdrant（单容器 REST）为主，SQLite 余弦检索为无外部依赖 fallback（学习对照）；同一库在两种后端下检索口径一致。
- **GraphRAG 三步检索**：向量命中 → 命中片段经 KG 一跳扩展关联实体 → 汇出关系与 claims；KG 为空或无命中时回退纯向量并标记 `degraded`，不阻断。
- **KG 自研内置**：导入即触发 KG 建立——LLM 抽取（复用平台模型代理）失败则规则回退，零外部依赖（此前 semantica worker 路线已退役）。
- **索引状态可见**：向量化进行中/成功/失败（含原因）可重试；"试运行检索"框直接验证召回质量。

## 相关资料

- `docs/11_知识库_需求文档.md` / `docs/12_知识库_方案设计.md` —— 需求与设计事实源（含开源选型对比）
- [Qdrant](https://qdrant.tech/documentation/) —— 向量数据库（P1 首选，单容器）
- [eino-ext 组件库](https://github.com/cloudwego/eino-ext) —— Eino 生态模型/检索组件
- [WeKnora](https://github.com/Tencent/WeKnora) / [RAGFlow](https://github.com/infiniflow/ragflow) —— 平台级 RAG 成品（对照参考，双轨备选）
