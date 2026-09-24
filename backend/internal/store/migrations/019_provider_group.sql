-- 019 REQ-148 供应商分组实体：分组标识与 BaseURL 解耦，同一供应商可创建多个独立实例
--（同 BaseURL 不同账号/Key/用途）；老数据由 Go 侧 BackfillProviderGroups 按 (protocol, base_url) 幂等回填。
CREATE TABLE IF NOT EXISTS provider_group (
  id TEXT PRIMARY KEY,
  alias TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE model_connection ADD COLUMN provider_group_id TEXT NOT NULL DEFAULT '';
