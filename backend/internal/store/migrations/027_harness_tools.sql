-- REQ-202/M38 Harness 执行面
-- agent.work_dir：工作目录（读写/grep/glob 工具的安全根，SafeJoin 约束；空=仅项目会话文件工具）
ALTER TABLE agent ADD COLUMN work_dir TEXT NOT NULL DEFAULT '';
-- agent.verify_command：结束前验证命令（verify_on_stop 背压；空=不验证）
ALTER TABLE agent ADD COLUMN verify_command TEXT NOT NULL DEFAULT '';
-- conversation.todo_json：todo_write 任务清单（模型自写进度，机器可读侧）
ALTER TABLE conversation ADD COLUMN todo_json TEXT NOT NULL DEFAULT '';
