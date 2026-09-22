-- 004 工具链配置（REQ-75：路径级工具链配置 pipeline_profile；REQ-76：guided 打卡 checklist 双 key 空间）
CREATE TABLE IF NOT EXISTS pipeline_profile (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL,
    ontology_id       TEXT,                      -- 可选：预绑定本体（也可后选）
    runtime_profile_id TEXT,                     -- S5 段引用已有运行方案（managed）
    stages            TEXT NOT NULL DEFAULT '{}',  -- {"s1":{"tool":"builtin_import","mode":"builtin","params":{}} ...}
    checklist         TEXT NOT NULL DEFAULT '{}',  -- 打卡状态 {"tool:s3:jena_rule":{"done_at":...}, "task:s1_first_ontology":{...}}
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_pipeline_updated ON pipeline_profile(updated_at);
