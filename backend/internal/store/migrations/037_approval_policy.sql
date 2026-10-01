-- 037_approval_policy.sql REQ-231/M58（审批面精细化，51 号 W2）：
-- ①approval_exempt 审批豁免清单（JSON 数组；danger/all 档下清单内工具免审——个工具覆盖档位）
-- ②approval_timeout_hours 挂起超时（0=不限；恢复重入时超时自动 deny 附超时语义）
ALTER TABLE agent ADD COLUMN approval_exempt TEXT NOT NULL DEFAULT '[]';
ALTER TABLE agent ADD COLUMN approval_timeout_hours REAL NOT NULL DEFAULT 0;
