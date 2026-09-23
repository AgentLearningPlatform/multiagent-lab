-- 015 KG 社区摘要与全局问答（M16 阶段二 / REQ-130）：
-- 每社区一行（label=代表实体名；members_json=成员实体名数组；summary=LLM 摘要，重建时全量替换=缓存失效语义）。
CREATE TABLE IF NOT EXISTS kg_community (
  id TEXT PRIMARY KEY,
  kb_id TEXT NOT NULL,
  label TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  method TEXT NOT NULL DEFAULT '',
  members_json TEXT NOT NULL DEFAULT '[]',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_kg_community_kb ON kg_community(kb_id);
