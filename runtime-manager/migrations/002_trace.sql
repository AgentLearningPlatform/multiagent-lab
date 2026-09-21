-- 翻译透视日志（REQ-94，docs/04 v0.8 §4.8.2）
CREATE TABLE IF NOT EXISTS trace_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  tool TEXT NOT NULL,           -- facade 工具名（get_concept/get_instance/list_instances/neighbors）
  profile_id TEXT NOT NULL,     -- 所属运行方案
  ontology_id TEXT NOT NULL,    -- 查询的本体仓库 id
  sparql TEXT NOT NULL,         -- 翻译出的 SPARQL 原文
  took_ms INTEGER NOT NULL DEFAULT 0,
  result_count INTEGER NOT NULL DEFAULT 0,
  ok INTEGER NOT NULL DEFAULT 1,
  error TEXT
);
CREATE INDEX IF NOT EXISTS idx_trace_profile ON trace_log(profile_id, id);
