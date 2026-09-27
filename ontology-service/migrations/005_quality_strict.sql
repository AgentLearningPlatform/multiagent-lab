-- 005 REQ-156/M-O15：本体级质量门禁 strict 开关（默认宽松；零新表，仅一列）
ALTER TABLE ontology ADD COLUMN quality_strict INTEGER NOT NULL DEFAULT 0;
