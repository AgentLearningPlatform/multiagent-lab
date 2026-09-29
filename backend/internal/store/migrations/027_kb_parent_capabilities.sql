-- 027: KB-10② 父子分块（knowledge_chunk.parent_content 子块冗余存父块内容，检索子块、召回父块上下文）
--     + KB-11 能力开关（kb_vector/kb_graph，解除 kb.mode 库级二选一互斥；mode 保留为展示页签默认）。
-- 存量迁移口径（45 号 KB-11/D2）：按原 mode 单能力启用——rag=仅向量、graphrag=仅图谱；
-- 图谱臂自身的「无命中降级向量」语义保留（degraded 如实标注），能力开关只约束主路由。

ALTER TABLE knowledge_chunk ADD COLUMN parent_content TEXT NOT NULL DEFAULT '';

ALTER TABLE knowledge_base ADD COLUMN kb_vector INTEGER NOT NULL DEFAULT 1;
ALTER TABLE knowledge_base ADD COLUMN kb_graph INTEGER NOT NULL DEFAULT 1;

UPDATE knowledge_base SET kb_vector = CASE WHEN mode = 'graphrag' THEN 0 ELSE 1 END,
                           kb_graph  = CASE WHEN mode = 'graphrag' THEN 1 ELSE 0 END;
