-- REQ-204/M39 C1：中断检查点持久化（替换进程内 memCheckPointStore——重启不再丢挂起中断）
CREATE TABLE IF NOT EXISTS checkpoint (
  id TEXT PRIMARY KEY,
  blob BLOB NOT NULL,
  created_at DATETIME
);
