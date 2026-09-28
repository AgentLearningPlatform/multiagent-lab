-- 022 REQ-187：伴生本体配置增强（领域提示词/抽取模型覆盖/置信度阈值自动入图）
ALTER TABLE agent ADD COLUMN companion_extract_hint TEXT NOT NULL DEFAULT '';
ALTER TABLE agent ADD COLUMN companion_extract_conn_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent ADD COLUMN companion_auto_threshold REAL NOT NULL DEFAULT 0;
