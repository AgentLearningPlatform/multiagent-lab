-- 016 对话级工具审批覆盖（REQ-135②：合并顺序 = 对话级 > Agent 级）：
-- '' = 跟随 Agent 级配置（tool_approval）；'on' = 本对话强制开启审批；'off' = 本对话强制关闭。
ALTER TABLE conversation ADD COLUMN tool_approval TEXT NOT NULL DEFAULT '';
