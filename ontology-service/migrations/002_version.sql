-- 002 本体版本历史（REQ-93 源码视图按版本读原文 + REQ-95 版本 diff 的数据基础）
CREATE TABLE IF NOT EXISTS ontology_version (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    ontology_id      TEXT NOT NULL,
    version          INTEGER NOT NULL,
    spec_json        TEXT,
    original_format  TEXT,
    original_content BLOB,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(ontology_id, version)
);
CREATE INDEX IF NOT EXISTS idx_version_ontology ON ontology_version(ontology_id);
