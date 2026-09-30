-- 031_companion_ontology_binding.sql REQ-216/M47 批次一①（伴生本体资产化·方案 D）：
-- 伴生开关 bool → 本体绑定可空引用。伴生产物归属容器化：图换绑本体伴生子图 ont-{id}，
-- 多 agent 绑同一本体 = 项目沉淀共享；开关语义退役为派生（companion_ontology_id != ''）。
-- 存量回填（companion_ontology=1 的 agent 绑定哪个本体）不在 SQL 层做——需经构建平面
-- 创建空本体（「{agent 名}的伴生本体」），由启动迁移 MigrateBindingsToOntologies 幂等补齐
-- （companion_meta 标记，构建平面不可达时下次启动重试）。
ALTER TABLE agent ADD COLUMN companion_ontology_id TEXT NOT NULL DEFAULT '';
