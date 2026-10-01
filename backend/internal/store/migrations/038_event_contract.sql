-- REQ-224/M52：事件契约治理与会话状态族持久化。
-- ①run_event.data 契约版本化（只增不改）：存量行=1（M0~M51 契约），新写入=2（结构化审计事件
--   approval.granted|denied / hook.denied / verify.completed|failed / connector.degraded +
--   tool.result 截断标记 + message.delta 门控落库）。消费方按版本兼容读。
ALTER TABLE run_event ADD COLUMN schema_version INTEGER NOT NULL DEFAULT 1;

-- ②C5 定时续跑调度持久化（跨重启）：进程内 map 退役为 DB 行 + 内存定时器装配；
--   一会话一条（重设=覆盖），fire 计数落库，cap 达到删行。
CREATE TABLE IF NOT EXISTS conversation_schedule (
  conversation_id  TEXT PRIMARY KEY REFERENCES conversation(id) ON DELETE CASCADE,
  interval_minutes INTEGER NOT NULL,
  max_runs         INTEGER NOT NULL,
  done             INTEGER NOT NULL DEFAULT 0,
  created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
