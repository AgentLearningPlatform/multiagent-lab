-- 032_connector.sql REQ-214/M46 批次一（外部连接器模块）：
-- 产品层只呈现「连接器」，MCP 自 agent 配置的挂载协议降为连接器的交付驱动之一
-- （REQ-177「协议降为模型连接属性」同构）。两轴 = 连接对象（mcp/kubernetes/ssh）× 交付驱动
-- （MCP 直通 / 平台托管自研 Go MCP 插件服务）。
-- connector 表：凭据服务端绑定（credentials_encrypted AES-256-GCM，不进 LLM 上下文不进工具参数）；
-- agent.connectors：连接器实例 id 数组（agent 级连接白名单 = 最小权限第一层）。
-- 存量 agent.mcp_servers → connector 行（kind=mcp）+ 引用迁移由启动迁移
-- MigrateAgentMCPToConnectors 幂等补齐（mcp_servers 置空即完成，零破坏）。
CREATE TABLE IF NOT EXISTS connector (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,                              -- mcp | kubernetes | ssh
  name TEXT NOT NULL UNIQUE,                       -- 实例名（装配前缀槽位 {name}__{tool}）
  description TEXT NOT NULL DEFAULT '',
  config_json TEXT NOT NULL DEFAULT '{}',          -- kind 专属非敏感配置（mcp: url；k8s: kubeconfig 路径/上下文；ssh: host/port/user）
  credentials_encrypted BLOB,                      -- AES-256-GCM 密文（secrets.Box；k8s/ssh 凭据）
  status TEXT NOT NULL DEFAULT 'unknown',          -- unknown | ok | error（连接测试回写）
  status_detail TEXT NOT NULL DEFAULT '',
  is_builtin INTEGER NOT NULL DEFAULT 0,           -- 内置实例（open-ontologies 预设迁移）不可删除
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
ALTER TABLE agent ADD COLUMN connectors TEXT NOT NULL DEFAULT '[]';
