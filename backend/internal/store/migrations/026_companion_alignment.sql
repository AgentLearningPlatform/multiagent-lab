-- 026 REQ-194/M34：伴生召回与抽取增强——候选表加列
-- aligned = ''(存量未标) | 'aligned'(对齐已有实体) | 'new'(新造实体)——抽取时实体对齐标记
ALTER TABLE companion_candidate ADD COLUMN aligned TEXT NOT NULL DEFAULT '';
-- note = 审计注记（⑤语义矛盾检测「疑似矛盾待人工」等裁决辅助信息）
ALTER TABLE companion_candidate ADD COLUMN note TEXT NOT NULL DEFAULT '';
