-- 025_assistant_unify.sql REQ-192/M32：平台助手配置单源归一（读写穿透 agent 内置行）。
-- builtin 行 instruction 置空（原为迁移 023 seed 的旧角色文案）——空=回落内置默认提示词
-- （启动时 EnsureBuiltinAssistantInstruction 写入当前 AssistantDefaultPrompt，并一次性移植
-- assistant_config 表存量微调）；此后用户微调为全量编辑语义（保存非空即全量快照）。
-- assistant_config 表自此退役不再读写（表保留不删，无破坏性迁移）。
UPDATE agent SET instruction = '' WHERE id = 'builtin-assistant';
