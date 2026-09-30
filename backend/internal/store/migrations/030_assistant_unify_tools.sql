-- 030_assistant_unify_tools.sql REQ-213：平台助手工具面归一——内置行 tools 补齐 L0+L1 八工具。
-- 历史缺口：迁移 023 seed 仅列 L0 五工具，REQ-186 阶段一/三新增的 L1 三工具
-- （sync_platform_kb/search_platform_kb/propose_assistant_config）只注册进 registry 未入行，
-- 提示词引导与装配面不一致。本迁移一次性并入（启动引导 EnsureAssistantTools 幂等兜底自愈）。
UPDATE agent SET tools = '["doc_read","list_model_connections","list_agents","list_kbs","get_assistant_config","sync_platform_kb","search_platform_kb","propose_assistant_config"]'
WHERE id = 'builtin-assistant' AND is_builtin = 1 AND tools = '["doc_read","list_model_connections","list_agents","list_kbs","get_assistant_config"]';
