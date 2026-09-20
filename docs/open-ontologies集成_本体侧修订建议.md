# open-ontologies 独立集成 · 本体侧（03/04）修订建议

> 来源：《open-ontologies借鉴与引入分析.md》v0.2 §8（2026-09-20 主人拍板：独立集成路径）。
> 智能体侧文档（01 PRD / 02 方案）已同步升 v0.8；以下为**本体侧文档（03/04，本体专属会话维护）的建议条目**，供该会话取用执行。
> 核心口径：open-ontologies 为**独立集成路径**（oo-worker :8092 托管其单二进制 + 工作台 + MCP 暴露），**不进入两平面/facade/翻译层**，与主线双轨并行、可整体摘除。

## 04_本体_方案设计.md 建议修订

1. **§6 开源组件清单**：登记条目——
   `open-ontologies（Rust 单二进制，MIT，Oxigraph 0.5 后端，pin 版本）｜接入位：独立集成（oo-worker 托管，不进两平面）｜能力：RDFS/OWL-RL 物化推理、SHACL 校验、不一致检查、变更影响分析（plan/blast radius）、数据装载（CSV/XLSX 等→RDF）、MCP server（39 个 onto_* 工具）`
2. **新增一节（建议 §4.7 或 §6 后）「外部独立集成：OpenOntologies 路径」**，要点：
   - 与 D-O5 的关系：P1 唯一引擎仍为 Oxigraph（不变）；open-ontologies 不作为两平面引擎适配器（融入式方案搁置），而是经主平台 oo-worker 独立托管。
   - 数据边界：该路径本体为 TTL 文件集（worker data-dir 自管），不进本体仓库 spec_json 体系；两轨数据 P1 不互通（互通见 01 需求池"P2 TTL 导出互通"）。
   - 与七阶段工具链（D-O7）的关系：可在 §4.6 `tools.json` 候选清单登记两条目——S3 段（校验/推理工具，guided 模式，交接契约 Eats=[turtle] Gives=[report]）与 S5 段（运行工具，managed 候选，交接契约 Gives=[endpoint]）——仅为候选登记，不改变 D-O7 执行模式定义。
   - 待验证项：open-ontologies `serve` 的 MCP 传输形态（README 中 stdin/stdout 与 streamable HTTP 表述不一致；若 stdio 由 oo-worker 转 HTTP）；物化推理与重载成本实测。
3. **§8 风险**（可选）：补一行——"外部工具版本演进快：open-ontologies 定位近期大改（变更管理/证书为主打），统一收在 oo-worker 适配层后并 pin 版本。"
4. **§11 迭代记录**：追加一行（建议引用智能体侧 01/02 v0.8 与分析文档 v0.2）。

## 03_本体_需求文档.md 建议修订

1. **需求池**：补两条——
   - `两轨数据互通：spec_json 仓库 ↔ OpenOntologies data-dir 的 TTL 导出互通（P2）`
   - `本体对齐/合并（alignment）：多本体 fork/合并场景的对齐能力参考 open-ontologies stable matching 方案（P2 远期）`
2. **工具链候选（REQ-74~77 口径）**：S1 段可增候选"实例数据快速灌装"（open-ontologies `load`：CSV/XLSX/JSON→RDF，guided）；S3 段增"语义校验/推理"候选（`validate`/`reason`）。两条均为 tools.json 数据条目，不新增执行模式。
3. **§11 迭代记录**：追加一行。

## 联调依据

- 主平台侧对接点（oo-worker :8092、`/api/oo/*` 反代、OO_MCP_URL、M8.5 里程碑、验收 15a）以《02_智能体_技术方案设计.md》v0.8 §6.15 为准。
- 本路径与本体两平面**无接口依赖**；两轨联调互不阻塞。
