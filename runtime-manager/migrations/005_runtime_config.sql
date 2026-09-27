-- 005 REQ-179/M-O16：全局运行配置（执行方式为系统级配置；默认 k8s——2026-09-27 主人指示）
CREATE TABLE IF NOT EXISTS runtime_config (
  id INTEGER PRIMARY KEY CHECK(id = 1),
  execution_method TEXT NOT NULL DEFAULT 'k8s',
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
