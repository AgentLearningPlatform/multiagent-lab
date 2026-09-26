-- 018 内置系统级 Agent「平台助手」配置（M27/REQ-166）：
-- 单行配置（id 固定 'default'）：系统提示词微调（追加到内置提示词后）、模型连接覆盖（空=默认 chat）、温度。
-- 平台助手不落 agent 表（不占用户 Agent 列表、不可删除——REQ-166 硬性要求）。
CREATE TABLE IF NOT EXISTS assistant_config (
  id TEXT PRIMARY KEY,
  system_prompt TEXT NOT NULL DEFAULT '',
  model_conn_id TEXT NOT NULL DEFAULT '',
  temperature REAL,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT OR IGNORE INTO assistant_config (id) VALUES ('default');
