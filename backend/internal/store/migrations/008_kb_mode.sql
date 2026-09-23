-- 008 知识库双子模块（M14，D-KB4）：kb.mode 区分 RAG / GraphRAG 子模块；老数据无此列 = 'rag'（默认值即兼容）
ALTER TABLE knowledge_base ADD COLUMN mode TEXT NOT NULL DEFAULT 'rag';
