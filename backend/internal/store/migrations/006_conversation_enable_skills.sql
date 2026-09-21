-- 006 会话级技能开关（§6.12）：enable_skills=0 时本次运行不注入技能指令、不加载技能工具
ALTER TABLE conversation ADD COLUMN enable_skills INTEGER NOT NULL DEFAULT 1;
