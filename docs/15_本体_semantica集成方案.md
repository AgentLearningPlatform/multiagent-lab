# eino-multiagent-lab · Semantica 独立集成方案（消费/审计环节）

> 状态：v0.1 · 2026-09-22
> 负责人：董奎
> 文档性质：**活文档**，Semantica 接入的**唯一设计事实源**（对标 11/12 知识库拆分模式）；需求事实源见《03_本体_需求文档.md》REQ-99~101（D-O10）。每次变更在文末「迭代记录」追加留痕。
> 范围声明：只写 Semantica 接入。本体两平面（构建/运行）见《04_本体_方案设计.md》；知识库模块见 11/12；部署运维见《16_部署与运行.md》。

---

## 0. 定位与范围

**一句话**：Semantica（github.com/semantica-agi/semantica，MIT，Python，13.3K stars）作为**独立集成路径**承担本体叙事"构建 → 运行 → **消费 → 审计**"四环节中的后两环节学习载体——**零侵入两平面/facade/翻译层**（对标 §4.7 OpenOntologies 双轨模式，可整体摘除）。

| 环节 | 载体 | 状态 |
| --- | --- | --- |
| 构建 | 本体两平面（04 文档） | ✅ 与本方案无关 |
| 运行 | runtime-manager + Oxigraph（04 文档） | ✅ 与本方案无关 |
| **消费** | semantica：TTL→KG 建库 + GraphRAG 语义问答 | ✅ REQ-100 已实现 |
| **审计** | semantica：record_decision + PROV-O 溯源 + Explorer | ✅ REQ-101 前端已实现（P2 深度集成见 §7） |

- 需求：REQ-99（独立栏+三段式引导+MCP 挂载）、REQ-100（消费链路）、REQ-101（审计视图，P2）——见《03_本体_需求文档.md》v0.8。
- 决策：D-O10（semantica 独立集成与消费/审计闭环，2026-09-21 主人拍板）。
- 边界不变：GraphRAG/embeddings 归 semantica 独立栏与知识库模块演示，**不并入本体构建平面**。

---

## 1. 现状盘点：已实现接入架构（v0.1 基线）

```
┌──────────────────────── Browser ─────────────────────────┐
│  导航「Semantica」独立栏（SemanticaPage）                  │
│  首页三段式引导 │ 知识图谱（消费链路） │ 决策审计（REQ-101）│
└──────┬──────────────────────────┬────────────────┬──────┘
       │ /api/semantica/*         │ /semantica/explorer/*（iframe）
       ▼                          ▼
┌────────────────── backend :8080 ─────────────────────────┐
│  SemanticaProxy（剥离 /api/semantica 前缀）               │
│  SemanticaExplorerProxy（重写 /explorer + 剥 X-Frame-Options）│
│  withStatic 放行 /semantica/（生产模式静态托管不拦截）      │
└──────┬──────────────────────────┬────────────────┘
       ▼ REST                     ▼ ASGI
┌──────────── semantica-worker :8093 ──────────────┐
│  FastAPI 薄壳（自研，<400 行）                     │
│  REST: /health /ingest-ttl /query /decision      │
│        /decisions /stats                          │
│  MCP:  /mcp（FastMCP Streamable HTTP，12 工具）   │
│  审计:  /decision-chain /lineage /prov-export     │
│         /causal                                   │
│  Explorer: /explorer（semantica[explorer] 惰性挂载）│
│  状态: ContextGraph 内存图 + graph.json 持久化     │
│         ProvenanceManager(audit.db)               │
└───────────────────────────────────────────────────┘
```

- 前端：`web/src/pages/SemanticaPage.tsx`（导航「Semantica」独立栏，PageKey 已扩展）。
- 反代：`backend/internal/ontology/semantica.go`（SemanticaProxy + SemanticaExplorerProxy）+ `server.go` 路由 + `cmd/backend/main.go` withStatic 放行 `/semantica/`。
- worker：`tools/semantica-worker/`（worker.py + requirements.txt + setup.sh/run.sh + Dockerfile）。

---

## 2. 自研与开源边界（核心章节）

**总原则（承接 04 §0）**：优先使用现成开源方案，自研仅保留"胶水"——**不重实现 semantica 的任何能力**。

### 2.1 自研清单（全部为薄胶水层）

| 自研件 | 位置 | 行数级 | 职责 | 不做的事 |
| --- | --- | --- | --- | --- |
| worker.py 薄服务 | tools/semantica-worker/ | ~330 行 | REST 端点（6 个）+ MCP HTTP 桥（FastMCP 12 工具）+ 审计端点（4 个）+ Explorer 惰性挂载 + 文件持久化 + **防御式形状归一**（_pick/_as_list/_method） | 不实现 KG 构建/GraphRAG/溯源算法（全部调 semantica API） |
| SemanticaProxy / SemanticaExplorerProxy | backend/internal/ontology/semantica.go | ~100 行 | 反代路由（前缀剥离/重写）+ X-Frame-Options 剥离 + 502 语义 | 不缓存、不改写 semantica 响应体 |
| withStatic 放行 | backend/cmd/backend/main.go | +1 行 | /semantica/ 前缀直通（生产模式静态托管不拦截 Explorer） | — |
| SemanticaPage 独立栏 | web/src/pages/SemanticaPage.tsx | ~940 行 | 三段式学习引导（内容资产）+ 消费链路 UI + 决策审计 UI（链/溯源/因果/Explorer iframe） | 不自研图谱可视化（Explorer 复用；页内仅轻量 Timeline/Descriptions） |
| 部署定义 | Dockerfile + compose + helm values | 配置 | 容器化与编排 | — |

### 2.2 开源接入清单（如何接入开源方案）

| 开源件 | 版本 | 接入方式 | 接入点 |
| --- | --- | --- | --- |
| **semantica**（本体消费/审计主体） | 0.6.8（PyPI，钉版；≥0.6.8 含 RCE/SSRF 等安全修复；0.7.0 瘦身版未上 PyPI） | **Python 库直调**——worker.py 进程内 import：`OntologyIngestor().ingest_ontology()` → `GraphBuilder(merge_entities=True).build()` → `ContextGraph`（内存图+save_to_file 持久化）→ `AgentContext.retrieve(use_graph=True)`（GraphRAG）→ `graph.record_decision()`（决策）→ `ProvenanceManager`（PROV-O） | worker.py 进程内 |
| **mcp**（官方 Python SDK） | 1.30.0（**钉 <2**：v2 改名 MCPServer 破坏性） | `FastMCP("semantica")` + `@mcp.tool()` 12 工具 + `streamable_http_app()` 挂载于 /mcp——**HTTP MCP 桥**（semantica 原生 MCP 为 stdio，主平台 HTTP 客户端无法直连，此桥即等价实现） | worker.py /mcp |
| **semantica[explorer]**（Explorer UI） | 同 semantica | `create_app(session=GraphSession(graph))` ASGI 惰性挂载于 /explorer + 反代 + iframe 嵌入（X-Frame-Options 后端剥离） | worker.py /explorer |
| fastapi / uvicorn | 0.115.6 / 0.34.0 | worker REST 框架 | worker.py |

### 2.3 防御式归一（自研胶水的核心逻辑，版本漂移保险）

semantica 各版本方法名/返回形状存在差异（lib-3 研究证实：文档曾引用不存在的 API、Entity.confidence 将变 Optional 等）。worker 内统一容错：

- `_pick(obj, *keys)` / `_as_list(v)` / `_method(obj, *names)`：dict/对象双形态取值、列表归一、多候选方法名解析。
- `AgentContext.retrieve` TypeError 降级（去 use_graph 重试）；`record_decision` 位置参数重试；`ContextGraph` 变更方法多候选（add_entity/add_node）。
- 后果：semantica 小版本漂移不会导致 worker 整体不可用（降级 + warnings 透出）。

---

## 3. 本地运行痛点分析（为什么不靠谱）

| # | 痛点 | 实测证据 | 影响 |
| --- | --- | --- | --- |
| 1 | **核心依赖重**：0.6.8 核心 = torch/transformers/spacy/faiss-cpu/fastembed+onnxruntime/gensim/librosa/opencv-python/umap-learn/grpcio + pandas/scipy/sklearn/pyarrow | venv 安装下载数 GB（nvidia-cuda 系列 ~3 GB 仅 GPU 需要）、耗时 30 min+ | 安装失败率高、磁盘占用大、CI/分发困难 |
| 2 | **版本漂移风险**：0.7.0（main）核心瘦身 22 包但未上 PyPI；文档与实现漂移（query(mode="graphrag") 不存在等） | lib-3 逐条核实 | 防御式归一是必需品（§2.3），升级需回归 |
| 3 | **启动重量**：import 链拉起 torch 等重模块，worker 冷启动慢 | 首次 import 数秒~数十秒 | 开发体验差 |
| 4 | **venv 分发困难**：多 GB venv 无法入 git，跨机迁移脆弱 | setup.sh 依赖网络 + 清华镜像 | 客户端打包受阻（docs/13 §9 P-4） |
| 5 | **stdio MCP 不兼容**：semantica 原生 MCP 为 stdio，主平台客户端为 Streamable HTTP | lib-3 证实 | 已由 worker /mcp HTTP 桥解决（§2.2） |

---

## 4. 支持方式评估与推荐（更好的支持方式）

| 模式 | 说明 | 优点 | 缺点 | 适用 | 推荐 |
| --- | --- | --- | --- | --- | --- |
| **A. 本地 venv**（现状） | setup.sh 建 venv + run.sh/run-dev 启动 | 零额外设施；worker 冷启动快（semantica 延迟 import） | 安装阶段痛点 1~4 全中（多 GB、易失败、venv 分发难） | 已装好的开发机 | ★★（维持但不推荐新装） |
| **B. 容器化** | `docker build -f tools/semantica-worker/Dockerfile` + compose/helm 已就绪 | **解决"靠谱"**：环境一致（免 PEP 668/Python 版本/venv 路径问题）、requirements 不变时层缓存使重建近零成本、可推 registry 供 `docker pull` 分发 | **不解决"快"**：首次获取仍需搬 6~10 GB（build 或 pull）；运行时 import 重量不变（semantica 延迟 import 链照旧） | **服务器部署 + 本地开发（docker run 单容器替代本地 venv）** | ★★（解决一致性与可复现，非速度） |
| **C. 远连服务端** | `SEMANTICA_WORKER_URL` 指向远端已部署实例（多机/客户端共享） | 本地零安装、零体积 | 依赖网络与远端可用性 | 客户端模式 / 多人团队 | ★★（客户端 v1 首选） |
| **D. 0.7.0 瘦身迁移** | 核心瘦身至 22 包、重依赖转 extras | **治本**：安装与 import 重量同时大降 | **未上 PyPI**（仅 main 分支）；迁移需回归验证（防御式归一覆盖） | 上 PyPI 后切换 | 跟踪中（P-1A，治本首选） |
| **E. PyInstaller 冻结** | worker 冻结为单二进制 → Tauri sidecar | 客户端分发友好 | 冻结 torch 系体积仍大、构建矩阵复杂 | 客户端离线需求出现时 | ★（随需求） |
| **F. 整体摘除** | 零侵入设计保障：删 worker + 反代路由 + 导航项即完全移除 | 主平台不受任何影响；**开发其他模块时可不跑 worker（零成本）** | 消费/审计演示不可用 | 不需要消费/审计演示时 | 保障性能力 |

**代价三阶段模型**（诚实评估基准）：①安装（一次性，多 GB 下载+venv，30min+）②worker 冷启动（快——semantica 延迟 import，顶层仅 fastapi）③首次功能调用（触发 semantica import 链，数秒~数十秒）。**容器化只改善 ①的可靠性与可分发，对 ②③ 无实质收益**；治本 = D（0.7.0）+ CPU 裁剪验证（P-2）。

**推荐组合（修正）**：
- **本地开发**：开发其他模块时不跑 worker（F 零成本）；需演示消费/审计时——已装 venv 者继续 A，未装者用 B（一次性获取后层缓存生效）。
- **服务器**：Mode B（compose/helm 已就绪）。
- **客户端**：Mode C（远连）起步，离线需求出现后评估 Mode E。
- **长期**：Mode D（0.7.0 上 PyPI 后迁移瘦身核心）。

---

## 5. 部署矩阵

| 形态 | worker 启动方式 | 前端连接 | 文档 |
| --- | --- | --- | --- |
| 本地开发（venv） | run-dev.sh 自动（venv 存在时）或 run.sh | `/api/semantica/*` 反代 → :8093 | §2/§6 |
| 本地开发（容器，推荐） | `docker run -p 8093:8093 -v …/data/semantica:/app/data eino-lab/semantica-worker` | 同上（SEMANTICA_WORKER_URL 默认 :8093 不变） | docs/16 §3 |
| 服务器 Compose | compose 服务 `semantica-worker` | backend 容器 env `SEMANTICA_WORKER_URL=http://semantica-worker:8093` | docs/16 §3 |
| K8s Helm | values 第 4 组件（range 模板自动三件套） | backend env 指向 Service | docs/16 §4 |
| 客户端（Tauri） | Mode C 远连 或 Mode E 冻结 sidecar | SEMANTICA_WORKER_URL 指向服务端/本地 sidecar | docs/16 §6 |

---

## 6. 与主平台集成契约（稳定面）

| 契约 | 内容 |
| --- | --- |
| REST 反代 | `/api/semantica/*` → worker `/*`（前缀剥离；502 `{error}` 语义） |
| Explorer 反代 | `/semantica/explorer/*` → worker `/explorer/*`（重写 + X-Frame-Options 剥离 + withStatic 放行） |
| MCP | worker `:8093/mcp`（Streamable HTTP，FastMCP 12 工具）——agent 级 `mcp_servers` 配 `{name:"semantica", url:"http://127.0.0.1:8093/mcp"}` |
| env | `SEMANTICA_WORKER_URL`（默认 http://127.0.0.1:8093）、`SEMANTICA_ADDR`、`SEMANTICA_DATA_DIR` |
| 前端 | 导航「Semantica」独立栏（PageKey `semantica`）——三页签：首页/知识图谱/决策审计 |
| 降级 | worker 不可达 → 独立栏 Result 降级 + 各端点 inline Alert；主平台其余功能零影响 |

---

## 7. 待办与痛点

- 部署痛点 P-1~P-5：见《16_部署与运行.md》§9（大体积组件部署痛点与待办）。
- REQ-99 ③ 对话挂载：MCP 端点已就绪（§2.2）；**对话配置 UI 的 MCP 目标勾选**随智能体侧 M9 前端编辑器交付（当前 agent 级 mcp_servers 无前端编辑器）。
- REQ-101 深度集成（P2）：Explorer 深度联动（决策审计页直跳 Explorer 对应工作区）、PROV-O 链图形化（当前 Timeline/Descriptions 轻量渲染）。
- 内容资产抽取：三段式引导文案内嵌于 SemanticaPage 组件——抽取至 `seeds/learning/semantica/` 随版本维护（04 §4.9.2 口径）。

---

## 8. 迭代记录

| 版本 | 日期 | 变更 |
| --- | --- | --- |
| v0.1 | 2026-09-22 | 首版：自 04 §4.9 独立成档（设计事实源）——现状盘点/自研与开源边界/本地运行痛点分析/支持方式六模式评估与推荐组合/部署矩阵/集成契约/待办；04 §4.9 收编为对接契约 + 指针 | 董奎 |
