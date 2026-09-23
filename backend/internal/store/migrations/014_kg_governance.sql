-- 014 KG 抽取治理与人工反馈（M16 阶段二 / REQ-129）：
-- ①关系/claims 审核状态（默认 approved，存量数据迁移后检索口径不变；rejected 不参与检索）；
-- ②库级抽取配置：抽取模型连接（空 = 默认 chat）与提示词模板覆写（空 = 默认模板）。
ALTER TABLE kg_relationship ADD COLUMN status TEXT NOT NULL DEFAULT 'approved';
ALTER TABLE kg_claim ADD COLUMN status TEXT NOT NULL DEFAULT 'approved';
ALTER TABLE knowledge_base ADD COLUMN kg_conn_id TEXT NOT NULL DEFAULT '';
ALTER TABLE knowledge_base ADD COLUMN kg_prompt TEXT NOT NULL DEFAULT '';
