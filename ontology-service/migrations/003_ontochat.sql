-- 003 OntoChat 多轮引导会话（REQ-103 模式 A：对话式 CQ 引导 → 逐轮补全 → 草稿 → 校验回喂 → 入库）
CREATE TABLE IF NOT EXISTS ontochat_session (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL DEFAULT '',
    stage        TEXT NOT NULL DEFAULT 'cq',   -- cq | domain | draft | refine | done
    round        INTEGER NOT NULL DEFAULT 0,   -- 已完成轮数（domain 补全轮计数）
    messages     TEXT NOT NULL DEFAULT '[]',   -- [{role, content, ts}] 全量对话留痕
    context_json TEXT NOT NULL DEFAULT '{}',   -- {description, cqs[], hints[], draft_spec?} 累积上下文
    ontology_id  TEXT,                          -- 入库后回填（done 阶段）
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_ontochat_updated ON ontochat_session(updated_at);
