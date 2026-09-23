-- 012 会话中断恢复（M11 收尾 REQ-95 收尾段 / §6.16.4 可恢复能力）：
-- interrupt_state 存中断挂起信息 JSON（checkpoint_id / target_id / question / choices / run_id），
-- 空串 = 无挂起中断。checkpoint 本体在进程内 memCheckPointStore（学习尺度单机；重启后中断失效，
-- 会话可重新提问，Run 时自动清理挂起状态）。
ALTER TABLE conversation ADD COLUMN interrupt_state TEXT NOT NULL DEFAULT '';
