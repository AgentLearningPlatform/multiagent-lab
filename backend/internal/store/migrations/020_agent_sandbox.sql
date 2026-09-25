-- 020 M10/10b：沙箱资源限制参数化（per Agent；空/0 = 默认 512m/1CPU）
ALTER TABLE agent ADD COLUMN sandbox_memory TEXT NOT NULL DEFAULT '';
ALTER TABLE agent ADD COLUMN sandbox_cpus REAL NOT NULL DEFAULT 0;
