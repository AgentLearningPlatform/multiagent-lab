-- 003: 知识库三表（方案 §5.2 v0.6：kb/doc/chunk + 状态字段）
-- knowledge_chunk：Qdrant 路径下正文仍存 SQLite（vector_ref=point id），
--                  SQLite fallback 路径下向量 BLOB 直存（float32 小端）。
CREATE TABLE IF NOT EXISTS knowledge_base (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  top_k       INTEGER NOT NULL DEFAULT 4,
  min_score   REAL    NOT NULL DEFAULT 0.0,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS knowledge_doc (
  id          TEXT PRIMARY KEY,
  kb_id       TEXT NOT NULL,
  title       TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'pending', -- pending|indexing|success|failed
  chunk_count INTEGER NOT NULL DEFAULT 0,
  error       TEXT NOT NULL DEFAULT '',
  source      TEXT NOT NULL DEFAULT 'paste',   -- paste（P1 文件上传另扩展）
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kdoc_kb ON knowledge_doc(kb_id);

CREATE TABLE IF NOT EXISTS knowledge_chunk (
  id            TEXT PRIMARY KEY,
  kb_id         TEXT NOT NULL,
  doc_id        TEXT NOT NULL,
  seq           INTEGER NOT NULL,
  content       TEXT NOT NULL,
  vector        BLOB,                          -- SQLite fallback 路径
  vector_ref    TEXT,                          -- Qdrant point id（UUID 格式）
  store_backend TEXT NOT NULL DEFAULT 'sqlite', -- sqlite|qdrant
  created_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kchunk_doc ON knowledge_chunk(kb_id, doc_id);
