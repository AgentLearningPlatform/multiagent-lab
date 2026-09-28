-- 023_assistant_builtin.sql REQ-186 阶段二：平台助手智能体化（内置行隔离）
-- 方案 A 修正：平台助手以 agent 表内置行承载（is_builtin=1）——「不落表」的动机
-- （不出现在用户智能体配置面）由 is_builtin 标记 + 应用层过滤/禁改达成；对话链路
-- （会话树/SSE/历史还原/工具装配）因此全部复用既有实现，零特判。
ALTER TABLE agent ADD COLUMN is_builtin INTEGER NOT NULL DEFAULT 0;

-- 内置平台助手行（幂等）：模型连接跟随全局默认（model_conn_id 空）、工具面=L0 只读白名单
INSERT OR IGNORE INTO agent (
  id, name, description, instruction, model_conn_id, temperature, max_tokens,
  max_iteration, tools, skills, mcp_servers, runtime_backend, inference_backend,
  logo_url, tool_approval, mcp_serve, sandbox_memory, sandbox_cpus,
  companion_ontology, is_builtin, created_at, updated_at
) VALUES (
  'builtin-assistant',
  '平台助手',
  '平台内置助手（REQ-186）：平台使用答疑 / 系统状态查询 / 平台知识检索。内置徽标、不可编辑删除。',
  '你是「平台助手」——智能体构建平台的内置使用助手。职责（REQ-186 分级工具面 L0）：①平台使用答疑（怎么配置模型/建智能体/建本体/挂知识库——优先用 doc_read 精读 docs/ 或 platform-knowledge/ 下的文档后回答，给出文档路径）；②系统状态查询（用 list_model_connections / list_agents / list_kbs / get_assistant_config 查询后回答，如实引用条数与名称）；③回答中给出依据来源（文档路径或列表条目）。约束：只陈述工具查到的事实，查不到就直说；不做任何配置修改建议的自动执行。',
  NULL,
  0.3,
  2048,
  15,
  '["doc_read","list_model_connections","list_agents","list_kbs","get_assistant_config"]',
  '[]',
  '[]',
  'inprocess',
  '',
  '',
  '',
  '',
  0,
  0,
  0,
  1,
  CURRENT_TIMESTAMP,
  CURRENT_TIMESTAMP
);
