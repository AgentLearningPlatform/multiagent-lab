-- 021 REQ-170/M28：智能体运行时动态薄本体 P1（伴生管线，低侵入旁路）
-- ① Agent 伴生开关（低侵入三原则②：默认关，开启才触发抽取与伴生图写入）
ALTER TABLE agent ADD COLUMN companion_ontology INTEGER NOT NULL DEFAULT 0;
-- ② 候选表（M2 抽取产物 pending 待人工确认；confirm 后入 Oxigraph 会话图）
-- kind = concept|relation|event（种子 5 骨架类的实例维度薄版子集）
CREATE TABLE IF NOT EXISTS companion_candidate (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  rel_name TEXT NOT NULL DEFAULT '',
  rel_target TEXT NOT NULL DEFAULT '',
  definition TEXT NOT NULL DEFAULT '',
  confidence REAL NOT NULL DEFAULT 0,
  source_message_id TEXT NOT NULL DEFAULT '',
  source_excerpt TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  decided_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_companion_candidate_conv ON companion_candidate(conversation_id, status);
-- ③ 会话游标（L1 事件层只读续抽：记录每会话已抽取到的最后一条 message id）
CREATE TABLE IF NOT EXISTS companion_cursor (
  conversation_id TEXT PRIMARY KEY,
  last_message_id TEXT NOT NULL DEFAULT '',
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
