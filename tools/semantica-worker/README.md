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

主平台经反向代理同源访问：`/api/semantica/health` → worker `/health`（前缀剥离）。
代理目标由 `SEMANTICA_WORKER_URL` 配置，默认 `http://127.0.0.1:8093`。

## 已知边界

- **内存图 + 文件持久化**：ContextGraph 常驻内存（模块级单例），每次变更后 `save_to_file` 落盘；
  启动时若持久化文件存在则加载，加载失败降级为空图并在 `/ingest-ttl` 的 `warnings` 中体现。
- **重依赖体积**：见上；venv 数 GB 属正常，勿误判为异常。
- **GraphRAG 依赖向量索引**：ingest 时会尽力把实体文本写入向量存储；若写入失败，`/query` 仍可用但可能降级
  （`warnings` 提示）。
- **Explorer UI / MCP 后续**：`semantica[explorer]` 与原生 MCP 能力（P2 审计视图 / 对话挂载）本轮**不在范围内**，
  后续复用其自带 Explorer（注意 iframe 嵌入的 X-Frame-Options 限制）。
- **无鉴权 / CORS 放开**：仅本地学习用途，勿暴露公网。
- **防御式适配**：semantica 各版本方法名/返回形状存在差异（`GraphBuilder`、`AgentContext.retrieve`、
  `record_decision` 等），worker 内部统一做容错归一，避免版本漂移导致整体不可用。
