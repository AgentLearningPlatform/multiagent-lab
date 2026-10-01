-- 运行方案（REQ-84/85/86，方案 04 §4.1）
CREATE TABLE IF NOT EXISTS runtime_profile (
  id TEXT PRIMARY KEY,                    -- 如 rt_ox_k8s
  name TEXT NOT NULL,
  engine TEXT NOT NULL,                   -- oxigraph | fuseki（rest.create 限定；memory_graph 已随 D-O5 退役不实装）
  ontology_ids TEXT NOT NULL DEFAULT '[]',-- 加载的本体集合（JSON 数组，构建平面仓库 id）
  config TEXT NOT NULL DEFAULT '{}',      -- 细节配置 JSON（端口/可写/健康间隔…）
  port INTEGER,
  status TEXT NOT NULL DEFAULT 'created', -- created|starting|running|stopped|error
  pid TEXT,
  last_error TEXT,
  created_at DATETIME,
  updated_at DATETIME
);
