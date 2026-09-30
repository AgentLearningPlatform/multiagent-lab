-- REQ-211/M44 伴生图作用域 agent 化：游标复合键 (conversation_id, agent_id)——
-- 多 agent 共用项目会话时各自追踪抽取位（此前单键会互相覆盖）。
-- 存量行回填：agent 会话取 conversation.agent_id；项目会话取该会话最近一条候选的
-- agent_id（历史上实际执行抽取的运行 agent）；均未知落 ''（下次抽取视为首抽）。
CREATE TABLE companion_cursor_new (
  conversation_id TEXT NOT NULL,
  agent_id        TEXT NOT NULL DEFAULT '',
  last_message_id TEXT,
  updated_at      TEXT,
  PRIMARY KEY (conversation_id, agent_id)
);
INSERT INTO companion_cursor_new (conversation_id, agent_id, last_message_id, updated_at)
SELECT c.conversation_id,
       COALESCE(
         (SELECT agent_id FROM conversation WHERE id = c.conversation_id),
         (SELECT agent_id FROM companion_candidate WHERE conversation_id = c.conversation_id ORDER BY created_at DESC LIMIT 1),
         ''),
       c.last_message_id, c.updated_at
FROM companion_cursor c;
DROP TABLE companion_cursor;
ALTER TABLE companion_cursor_new RENAME TO companion_cursor;

-- 伴生模块元数据（图迁移完成标记等）
CREATE TABLE IF NOT EXISTS companion_meta (
  key   TEXT PRIMARY KEY,
  value TEXT
);
