-- 013 工具调用人工审批（REQ-14 恢复语义② / LG-8 危险操作审批，M11 收尾）：
-- tool_approval = ''（默认，不审批）| 'all'（所有工具调用前挂起等待批准）。
-- 复用 REQ-14 恢复机制（StatefulInterrupt + checkpoint + 定向恢复），批准/拒绝作为恢复数据投递。
ALTER TABLE agent ADD COLUMN tool_approval TEXT NOT NULL DEFAULT '';
