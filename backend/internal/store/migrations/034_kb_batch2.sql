-- M36 知识库批次二（KB-6/KB-7/B1/KB-13；37 号正式方案）
-- KB-6③ 本体约束抽取挂载（I3 裁定：抽取配置扩 ontology_id，本体侧只读词表，运行期单向 本体→KB）
ALTER TABLE knowledge_base ADD COLUMN kg_ontology_id TEXT DEFAULT '';
-- KB-6① 库级抽取语料预算（0 = 默认 200 chunks，解除原 60 硬截断）
ALTER TABLE knowledge_base ADD COLUMN kg_max_chunks INTEGER DEFAULT 0;
-- KB-7② 实体别名（检索/搜索命中别名；重建时同 名 实体别名保留）
ALTER TABLE kg_entity ADD COLUMN alias TEXT DEFAULT '';
