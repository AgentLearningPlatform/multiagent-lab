# 27_外部MCP客户端直连验证包

| 项 | 内容 |
| --- | --- |
| 版本 / 日期 | v1.0 / 2026-09-27 |
| 需求 | REQ-152（M20/P2，D-O17 ②；18 号登记） |
| 性质 | **交付资产**：外部 MCP 客户端（Claude Desktop / Cursor / 任意 MCP 客户端）直连本平台 facade 的配置文档 + 协议级实测记录。目标场景：**外部 IDE 查平台本体**——"使用本体的最直观外部证明"（03 §5 D-O17 ②） |
| 状态 | ✅ 文档 + 协议级实测已交付（2026-09-27）；GUI 客户端（Claude Desktop / Cursor）实测留主人侧（需 GUI 环境，见 §6 实测记录表） |

---

## 1. 原理速览

平台自 REQ-131/M18 起把**任意开启「MCP 服务」开关的 Agent** 暴露为标准 MCP Server：

- **端点**：`http://localhost:8080/mcp`（传输 = **Streamable HTTP**，协议版本 `2025-03-26`）；
- **鉴权**：`Authorization: Bearer <token>`——Token 为 **Agent 级凭据**（开启开关时自动生成，可经管理端点轮换；list/call 均需有效 Token，call 再校验 Token 精确属于该 Agent）；
- **会话**：`initialize` 响应头返回 `Mcp-Session-Id`，后续请求必须携带（标准 Streamable HTTP 语义，客户端自动处理）；
- **工具面**：对外暴露为**单工具 `agent_{id}`**（"调用一个智能体并返回其最终回复"，入参 `input`）——外部客户端调用它 = 与该 Agent 完整对话一次（内部经装配链路走模型 + 工具；Agent 挂载本体运行方案时，内部即 facade `onto_*` 五工具：`get_concept` / `get_instance` / `list_instances` / `neighbors` / `sparql_query`，见 §4）。

> 澄清（对齐 D-O17 ②的演进）：facade 原生 MCP 工具面由**平台内 Agent 对话**消费（S4.7 已覆盖）；外部客户端直连的是 REQ-131 的 `/mcp` Agent 服务化端点——外部 IDE 用自然语言提问，Agent 在平台侧查本体后回答。这也是对外最稳的形态：Agent 级 Token 隔离 + 工具审批/能力降级等平台语义全量生效。

## 2. 前置条件

1. 平台栈已启动（`./run-dev.sh`，backend :8080）；
2. 在「智能体」页创建/选择 Agent → 开启「MCP 服务」开关（AgentModal/AgentSidePanel，REQ-131）→ 保存后获得 Token（界面可见；或 `GET /api/agents/{id}/mcp-serve` 查询、`POST /api/agents/{id}/mcp-serve/reset` 轮换）；
3. （可选，推荐）给该 Agent 挂载本体运行方案并启动（本体模块「运行」页；方案 running 后 Agent 对话即带 `onto_*` 五工具，外部提问才能查到本体内容——未挂载时 Agent 以纯对话回答，见 §6 实测⑤注记）。

## 3. 客户端配置

### 3.1 Cursor（`~/.cursor/mcp.json`）

```json
{
  "mcpServers": {
    "agentlab": {
      "url": "http://localhost:8080/mcp",
      "headers": { "Authorization": "Bearer <你的AgentToken>" }
    }
  }
}
```

保存后 Cursor → MCP 面板应出现 `agentlab`，工具 `agent_{id}` 可用。Agent Toolbox 里直接对话即可触发。

### 3.2 Claude Desktop

Claude Desktop 对远程 MCP 的支持形态随版本演进，两条路径任选：

- **本地代理（稳态推荐）**：`claude_desktop_config.json` 经 `mcp-remote` 桥接（Node ≥18）：

```json
{
  "mcpServers": {
    "agentlab": {
      "command": "npx",
      "args": ["-y", "mcp-remote@latest", "http://localhost:8080/mcp",
               "--header", "Authorization: Bearer <你的AgentToken>"]
    }
  }
}
```

- **远程连接器（Settings → Connectors → Add custom connector）**：URL 填 `http://localhost:8080/mcp`；若客户端版本要求 OAuth 而不支持自定义 Header，则退回上一条 mcp-remote 路径。

### 3.3 任意 MCP 客户端 / curl（协议级）

Streamable HTTP 四步：`initialize`（取 `Mcp-Session-Id`）→ `notifications/initialized` → `tools/list` → `tools/call`。完整序列见 §5。

## 4. 工具说明

| 工具 | 入参 | 行为 |
| --- | --- | --- |
| `agent_{id}` | `input`（必填，字符串） | 与该 Agent 对话一次，返回最终回复文本。Agent 内部按其配置走模型与工具（挂载本体方案时即 facade 五工具：`get_concept` 按名称精确查概念 / `get_instance` 精确查实例 / `list_instances` 列某概念全部实例 / `neighbors` 查实例关系邻居 / `sparql_query` 只读 SPARQL SELECT，`graph=companion` 可查会话伴生图） |

多 Agent 各自开服务 = 多个 `agent_{id}` 工具；Token 与 Agent 一一对应（跨 Agent 调用被 403 拒绝）。

## 5. 协议级实测记录（2026-09-27，本机全链 ✅）

```bash
TOKEN=<AgentToken>
# ① initialize（取响应头 Mcp-Session-Id）
curl -sS -D - -o /tmp/init.json http://localhost:8080/mcp \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"verify","version":"0.1"}}}'
SID=<Mcp-Session-Id 响应头值>
# ② initialized 通知（202）
curl -sS -o /dev/null -w "%{http_code}\n" http://localhost:8080/mcp \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -H "Content-Type: application/json" -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}'
# ③ tools/list（应见 agent_{id}）
curl -sS http://localhost:8080/mcp -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -H "Content-Type: application/json" -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
# ④ tools/call（真实对话）
curl -sS http://localhost:8080/mcp -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -H "Content-Type: application/json" -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"agent_<id>","arguments":{"input":"你好"}}}'
```

实测输出摘录（2026-09-27）：

- ① `initialize` → 200，`{"protocolVersion":"2025-03-26","capabilities":{"tools":{"listChanged":true}},"serverInfo":{"name":"eino-multiagent-lab","version":"0.1.0"}}` + `Mcp-Session-Id: mcp-session-…`；
- ② `notifications/initialized` → 202；
- ③ `tools/list` → 200，1 个工具 `agent_{id}`（description「调用一个智能体并返回其最终回复」）；
- ④ `tools/call` 缺 `input` → 200 + 结构化业务错误 `{"content":[{"type":"text","text":"input 必填"}],"isError":true}`；
- ⑤ `tools/call` 带 `input` → 200，真实 LLM 回答文本（默认 GLM 连接；该冒烟 Agent 未挂本体方案，故回答说明自身无本体工具——挂载运行方案后即经内部 `onto_*` 查询，链路见 20 号 S4.7）。

## 6. 实测记录表

| # | 项 | 结果 | 备注 |
| --- | --- | --- | --- |
| 1 | initialize / initialized / tools/list（协议握手） | ✅ 2026-09-27 | 本机 curl（§5），Streamable HTTP 全语义（Bearer + 会话头） |
| 2 | tools/call 缺参 → 结构化 isError | ✅ 2026-09-27 | MCP 错误语义正确（HTTP 200 + isError，非传输层报错） |
| 3 | tools/call 真实对话（LLM 全链） | ✅ 2026-09-27 | 平台内模型连接执行，最终回复经 content[0].text 返回 |
| 4 | tools/call 挂载本体方案的问答（Agent 内查 TTL 本体） | ⏳ 待主人侧复验 | 链路本身由 20 号 S4.7 覆盖（agent 对话内 tool.call→tool.result）；本验证包演示建议挂 med_common/gene_core 种子方案 |
| 5 | Claude Desktop 直连 | ⏳ 待主人 | §3.2 两路径；结果回填本表 |
| 6 | Cursor 直连 | ⏳ 待主人 | §3.1 mcp.json；结果回填本表 |

## 7. 排障

| 现象 | 原因与处置 |
| --- | --- |
| 401 | Token 缺失/错误，或 Agent 的 MCP 服务开关已关——重开开关或 `POST /api/agents/{id}/mcp-serve/reset` 轮换后更新客户端 |
| 400 `Invalid session ID` | 未携带 `Mcp-Session-Id`（每次 initialize 的会话头必须随后续请求回传；GUI 客户端自动处理，自写脚本常漏） |
| 工具回答"没有本体工具/查询失败" | Agent 未挂载运行方案或方案未 running——本体模块「运行」页启动方案后再试（409=方案未运行） |
| tools/call 超时 | Agent 对话含 LLM 与工具调用，耗时正常范围数十秒；`input` 复杂问题建议在平台内先跑通再外部复用 |

## 8. 安全注记

- Token = Agent 级明文凭据：仅 localhost/内网明文使用；对外暴露务必经 TLS 反代；
- Token 泄露即该 Agent 的对话与本体查询能力泄露——`mcp-serve/reset` 一键轮换；
- Agent 级工具审批（REQ-14）与能力降级语义对外部调用同样生效。
