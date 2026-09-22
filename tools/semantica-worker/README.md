# semantica-worker

Semantica 独立集成薄服务（§4.9 D-O10），导航栏「Semantica」独立一栏的后端载体。
**零侵入两平面 / facade**：不进翻译层、不进引擎清单，只作为「消费（TTL→KG/GraphRAG）+ 审计（决策溯源）」环节的学习演示设施。

> 依赖体积说明：semantica 核心依赖较重（torch / transformers 等），首次安装需下载**数 GB**、耗时较长，
> 生成的 `.venv` 体积同样达数 GB，**属预期**。要求 Python 3.10–3.12。

## 启动

```bash
# 1) 一次性环境准备（多 GB 下载，清华镜像加速）
bash tools/semantica-worker/setup.sh

# 2) 启动（默认 :8093）
bash tools/semantica-worker/run.sh
# 或由一键脚本统一编排：./run-dev.sh
```

环境变量：

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `SEMANTICA_PORT` | `8093` | 监听端口（run.sh） |
| `SEMANTICA_ADDR` | `:8093` | `python worker.py` 直接运行时的绑定地址 |
| `SEMANTICA_DATA_DIR` | `./data/semantica/graph.json` | 图持久化文件路径（run.sh 默认落到仓库 `data/semantica/`） |

## 端点

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/health` | `{ok, version, graph_loaded, entities, relationships, decisions}` |
| POST | `/ingest-ttl` | body `{ontology_id, ttl}` → 摄入 TTL、建 KG、持久化 |
| POST | `/query` | body `{q, max_results=5}` → GraphRAG 检索，返回 `{claims:[{text,source_node,score}], query}` |
| POST | `/decision` | body `{category, scenario, reasoning, outcome, confidence?}` → 记录决策，返回 `{decision_id}` |
| GET | `/decisions?limit=20` | 决策节点列表（id/category/scenario/outcome/confidence/ts） |
| GET | `/stats` | 实体/关系/决策计数 |
| GET | `/decision-chain/{decision_id}` | 决策因果链 `{decision_id, chain:[…]}`（PROV-O 溯源） |
| GET | `/lineage/{entity_id}` | 实体 lineage `{entity_id, lineage:[…]}`（ProvenanceManager） |
| GET | `/prov-export?format=turtle` | 导出 PROV-O（`text/turtle`，含 prov:Entity/wasDerivedFrom 等词汇） |
| POST | `/causal` | body `{from_id, to_id, type?}`（`CAUSED`\|`INFLUENCED`\|`PRECEDENT_FOR`）→ `{ok:true}` |
| GET | `/explorer/` … | semantica 自带 Explorer UI（ASGI 惰性挂载；未安装 `semantica[explorer]` 时 503） |

主平台经反向代理同源访问：`/api/semantica/health` → worker `/health`（前缀剥离）。
代理目标由 `SEMANTICA_WORKER_URL` 配置，默认 `http://127.0.0.1:8093`。

## 审计与 Explorer（REQ-101，§4.9.2/§4.9.4）

审计链路：`POST /decision` 落决策节点 → `POST /causal` 建立因果/先例关系 →
`GET /decision-chain/{id}` 取决策链 → `GET /lineage/{id}` + `GET /prov-export` 查看 PROV-O 溯源。

Explorer UI（semantica 自带，React 19 + Sigma.js，随 wheel 打包）：

- 挂载于 worker `/explorer`（首次访问惰性创建并缓存：启动时图单例可能尚未就绪；未安装 `semantica[explorer]` 返回 503 JSON，worker 其余端点不受影响）。
- 主平台 iframe 入口：`/semantica/explorer/`（后端反代改写前缀 `/semantica/explorer` → `/explorer`）。
- **X-Frame-Options**：semantica 服务默认设置 `X-Frame-Options` 阻止 iframe 嵌入。worker 挂载层与后端反代 `ModifyResponse` **双重剥离**该响应头（存在才剥离，缺失无副作用），使 Explorer 可在主平台内嵌。
- 已知边界：Explorer 静态资源若使用绝对根路径（`/assets/...`）可能受子路径挂载影响；如需彻底规避，可后续评估独立端口直连或前端改用相对路径。

## MCP 端点（REQ-99 ③）

worker 同时暴露 **Streamable HTTP** MCP 端点：`http://127.0.0.1:8093/mcp`（依赖 `mcp>=1.2.0,<2`，已入 requirements）。

- 与 REST 端点**共享同一内存图与文件持久化**——REST 写入对 MCP 工具可见，反之亦然。
- 工具清单（12）：`extract_entities` / `extract_relations` / `record_decision` / `query_decisions` / `find_precedents` / `get_causal_chain` / `add_entity` / `add_relationship` / `run_reasoning`（关键词图检索）/ `get_graph_analytics` / `export_graph` / `get_graph_summary`。
- 主平台挂载：智能体属性 → MCP servers 配 `{name: "semantica", url: "http://127.0.0.1:8093/mcp"}`（Streamable HTTP，与 mark3labs/mcp-go 客户端兼容）。
- 生命周期：FastMCP 的 session_manager 由 worker 的 FastAPI lifespan 驱动（挂载子应用不会自动运行自身 lifespan——已显式处理）。
- 已知边界：semantica 原生 `semantica-mcp` 为 stdio 传输，主平台 HTTP 客户端无法直连——本端点即官方 stdio 之外的 HTTP 等价实现（工具语义对齐其 MCP 工具清单）。

## 已知边界

- **内存图 + 文件持久化**：ContextGraph 常驻内存（模块级单例），每次变更后 `save_to_file` 落盘；
  启动时若持久化文件存在则加载，加载失败降级为空图并在 `/ingest-ttl` 的 `warnings` 中体现。
- **重依赖体积**：见上；venv 数 GB 属正常，勿误判为异常。
- **GraphRAG 依赖向量索引**：ingest 时会尽力把实体文本写入向量存储；若写入失败，`/query` 仍可用但可能降级
  （`warnings` 提示）。
- **Explorer / PROV-O 溯源**：已由 `/explorer`（惰性挂载）与 `/decision-chain`、`/lineage`、`/prov-export`、
  `/causal` 提供（REQ-101）；Explorer 需 `semantica[explorer]` extra（已入 requirements）。
- **无鉴权 / CORS 放开**：仅本地学习用途，勿暴露公网。
- **防御式适配**：semantica 各版本方法名/返回形状存在差异（`GraphBuilder`、`AgentContext.retrieve`、
  `record_decision` 等），worker 内部统一做容错归一，避免版本漂移导致整体不可用。
