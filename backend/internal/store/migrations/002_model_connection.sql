-- 002 模型连接（chat / embedding），Key 密文存储
CREATE TABLE IF NOT EXISTS model_connection (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  conn_type TEXT NOT NULL CHECK(conn_type IN ('chat','embedding')),
  protocol TEXT NOT NULL DEFAULT 'openai_compat',
  base_url TEXT NOT NULL DEFAULT '',
  model_name TEXT NOT NULL DEFAULT '',
  api_key_enc BLOB,
  api_key_hint TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- REQ-41：首次启动预置一条 DeepSeek 对话连接（BaseURL/model 预填，key 留空待填）
INSERT OR IGNORE INTO model_connection
  (id, name, conn_type, protocol, base_url, model_name, api_key_enc, api_key_hint, enabled, is_default)
VALUES
  ('conn_deepseek_chat', 'DeepSeek（预置）', 'chat', 'openai_compat',
   'https://api.deepseek.com/v1', 'deepseek-chat', NULL, '', 1, 0);
