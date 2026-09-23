-- 010: KG 自存 + 审计决策表（D-O15/REQ-110：去-semantica 化，反转 D-O10）
--
-- 此前 KG 抽取结果只落 semantica worker 侧（kb_kg.json），主平台经 /graphrag/kg 回读；
-- D-O15 反转后 worker 退出运行链路，KG 作为一等数据自存主平台库：
--   kg_entity / kg_relationship / kg_claim 三表 = 教学口径「实体 / 关系 / claim」；
--   claim 保留 chunk_id 溯源（一跳扩展拼上下文与 PROV-O 导出的材料）。
-- 审计（REQ-101 学习视图，P2）：onto_decision 决策留痕 + derived_from 溯源链。

CREATE TABLE IF NOT EXISTS kg_entity (
  id         TEXT PRIMARY KEY,
  kb_id      TEXT NOT NULL,
  doc_id     TEXT NOT NULL DEFAULT '',
  name        TEXT NOT NULL,
  type        TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kg_entity_kb ON kg_entity(kb_id);

CREATE TABLE IF NOT EXISTS kg_relationship (
  id         TEXT PRIMARY KEY,
  kb_id      TEXT NOT NULL,
  doc_id     TEXT NOT NULL DEFAULT '',
  source     TEXT NOT NULL,
  target     TEXT NOT NULL,
  rel_type   TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kg_rel_kb ON kg_relationship(kb_id);

CREATE TABLE IF NOT EXISTS kg_claim (
  id         TEXT PRIMARY KEY,
  kb_id      TEXT NOT NULL,
  doc_id     TEXT NOT NULL DEFAULT '',
  chunk_id   TEXT NOT NULL DEFAULT '',
  subject    TEXT NOT NULL,
  text       TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kg_claim_kb_subject ON kg_claim(kb_id, subject);
CREATE INDEX IF NOT EXISTS idx_kg_claim_chunk ON kg_claim(chunk_id);

CREATE TABLE IF NOT EXISTS onto_decision (
  id           TEXT PRIMARY KEY,
  subject_kind TEXT NOT NULL DEFAULT 'kg',  -- kg | ontology | kb | manual
  subject_id   TEXT NOT NULL DEFAULT '',    -- kb_id / ontology_id 等
  title        TEXT NOT NULL,
  rationale    TEXT NOT NULL DEFAULT '',
  derived_from TEXT NOT NULL DEFAULT '',    -- 前置决策 id（溯源链）
  meta_json    TEXT NOT NULL DEFAULT '{}',
  created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_onto_decision_subject ON onto_decision(subject_kind, subject_id);
