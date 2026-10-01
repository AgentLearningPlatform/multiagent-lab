-- 003_evolution_candidate.sql REQ-207/M43（本体自进化受控生长闭环）
-- 候选补丁 vN-cK 状态机：proposed（running）→ accepted（采纳升正式 vN+1）/ rejected（留档冻结）。
-- 永不覆盖正式版本；门控证据（对照评测结果）随候选留档可追溯。
CREATE TABLE IF NOT EXISTS evolution_candidate (
  id TEXT PRIMARY KEY,
  ontology_id TEXT NOT NULL,
  label TEXT NOT NULL,                 -- v{N}-c{K} 展示标签
  base_version INTEGER NOT NULL,       -- 基于正式版本 N
  layer TEXT NOT NULL,                 -- content | tool | schema（归因三层，每次只动一层）
  summary TEXT NOT NULL,               -- 补丁摘要（LLM 归因产出）
  evidence TEXT NOT NULL DEFAULT '',   -- Evidence 锚定（物理来源/探测实证，JSON 或文本）
  patch_json TEXT NOT NULL,            -- 类型化编辑（add/remove/update concepts/relations）
  gate_report TEXT NOT NULL DEFAULT '',-- 配对门控对照评测结果 JSON（双信号）
  status TEXT NOT NULL DEFAULT 'proposed', -- proposed | accepted | rejected
  round INTEGER NOT NULL DEFAULT 1,        -- 进化轮次（预算控制）
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  decided_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_evo_cand ON evolution_candidate(ontology_id, status);
-- 轮次预算（每本体每轮一条活跃候选；轮次上限读取侧控制）
