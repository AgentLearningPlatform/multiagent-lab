-- 034_companion_governance.sql REQ-227/M55（伴生治理与质量，40 号 E1）：
-- ①batch_rank 批内分位（REQ-227② 置信校准——LLM 自评虚高，自动入图门控改「conf≥阈值 且
--   批内分位≥0.5」；存量 0=未校准，门控对存量行为不回溯）。
-- ②time_scope 事件时点（REQ-229② 时效最小版——event 类知识的时间范围提示，落 bot:timeScope）。
ALTER TABLE companion_candidate ADD COLUMN batch_rank REAL NOT NULL DEFAULT 0;
ALTER TABLE companion_candidate ADD COLUMN time_scope TEXT NOT NULL DEFAULT '';
