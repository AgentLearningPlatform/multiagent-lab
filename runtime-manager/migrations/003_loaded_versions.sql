-- 003 REQ-155/M-O15 阶段二：方案加载版本快照（生命周期 drift 检测数据源）
ALTER TABLE runtime_profile ADD COLUMN loaded_versions TEXT NOT NULL DEFAULT '';
