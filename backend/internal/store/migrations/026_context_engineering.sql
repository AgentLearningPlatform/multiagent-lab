-- REQ-201/M37 上下文工程补零
-- agent.context_mode：历史 token 预算档位（'' = 标准档；compact 紧凑 / standard 标准 / full 完整不限量）
ALTER TABLE agent ADD COLUMN context_mode TEXT NOT NULL DEFAULT '';
-- conversation.context_state：压缩状态持久化（JSON：摘要 + 覆盖到的消息 ID；消息只追加故前缀状态长期有效）
ALTER TABLE conversation ADD COLUMN context_state TEXT NOT NULL DEFAULT '';
