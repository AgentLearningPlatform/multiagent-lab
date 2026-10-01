-- 039_agent_config_version.sql REQ-226/M54（配置治理：版本/回滚，39 号 P-2/E5）
-- 保存即快照（PUT agent 成功后自动落一行全量配置 JSON）；显式还原=读历史版本整包写回。
-- retention：保留最近 50 版（写入时惰性裁剪，单机教学尺度无需后台任务）。
CREATE TABLE IF NOT EXISTS agent_config_version (
  id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  version INTEGER NOT NULL,              -- 每 agent 单调递增（从 1 起）
  config_json TEXT NOT NULL,             -- 全量配置快照（与 GET /api/agents/{id} 同形）
  note TEXT NOT NULL DEFAULT '',         -- 来源注记（「保存前自动快照」/「回滚前快照」）
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_agent_config_version ON agent_config_version(agent_id, version DESC);
