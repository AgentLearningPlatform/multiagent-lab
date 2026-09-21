-- 本体仓库：多形态资产（REQ-80/87，方案 §3.1）
CREATE TABLE IF NOT EXISTS ontology (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL DEFAULT 1,
  forked_from TEXT,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ontology_artifact (
  ontology_id TEXT NOT NULL REFERENCES ontology(id) ON DELETE CASCADE,
  format TEXT NOT NULL,             -- spec_json | owl_rdfxml | turtle | csv | graphml
  content TEXT NOT NULL,            -- 文件内容（大文件可存盘+路径，P1 内存库/小文件直接存）
  is_normalized INTEGER NOT NULL DEFAULT 0,
  imported_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (ontology_id, format)
);

CREATE INDEX IF NOT EXISTS idx_artifact_ontology ON ontology_artifact(ontology_id);
