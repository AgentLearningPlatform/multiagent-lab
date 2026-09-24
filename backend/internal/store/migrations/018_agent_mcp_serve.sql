-- 018 Agent 对外 MCP 服务化（REQ-131/M18）：mcp_serve 存 {enabled, token, tool_name}。
-- 默认 '{}'（未开启）；开启时由 API 层生成 token（Agent 级 Bearer）。
ALTER TABLE agent ADD COLUMN mcp_serve TEXT NOT NULL DEFAULT '{}';
