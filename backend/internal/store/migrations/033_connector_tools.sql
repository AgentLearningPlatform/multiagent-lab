-- 033_connector_tools.sql REQ-214 P2 批次（交互完善七项）：
-- tools_json：连接测试成功时落库的工具名清单（mcp=握手取回；k8s/ssh=kind 静态清单，read_only 时无 apply）
-- ——「授权前知道将得到什么工具」；tested_at：最近一次连接测试时间（状态时效性）。
ALTER TABLE connector ADD COLUMN tools_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE connector ADD COLUMN tested_at TEXT NOT NULL DEFAULT '';
