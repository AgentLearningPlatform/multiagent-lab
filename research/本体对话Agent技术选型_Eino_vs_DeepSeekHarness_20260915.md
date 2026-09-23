# 本体对话验证 Agent 技术选型：Eino vs DeepSeek Harness

> 状态：Review（供评审/讨论）
> 日期：2026-09-15
> 面向：Kubernetes 智能运维本体研究 —— 对话式本体功能验证 &「有本体 / 无本体」对照实验
> 置信度：medium-high（框架事实来自官方仓库/文档；工作量为本研究规模下的工程估算，未实测）

---

## 1. 结论先行（TL;DR）

1. **两者不在同一抽象层，直接对比有误导性。**
   - **Eino**（CloudWeGo/字节，Go）= **LLM 应用"框架"**：提供组件（ChatModel/Tool/Retriever）、图编排、ADK Agent、回调/Trace/Eval 工具链，**循环和应用要你自己组装**；已在豆包/TikTok/Coze 数百个生产服务落地，稳定、强类型。
   - **DeepSeek Harness（dsh）**（深度求索，TypeScript，2026-08-13 开源）= **Agent "Harness"（成品运行外壳）**：核心理念 `Agent = Model + Harness`，Cordis 微内核"一切皆插件"，**主循环、Web UI、沙箱、会话都已内置**，你装插件/配置即可；但它是**面向编码任务**（读写文件、跑命令、改代码）的 harness，且官方明确处于 **developer preview，会有破坏性变更**（当前 0.1.5-rc.2）。

2. **对你这个研究目标（验证本体功能 + A/B 对照），真正关键的不是选哪个外壳，而是把"本体能力"做成一个可插拔、可开关的工具/检索层**——这样 Eino 或 Harness 都能挂，A/B 只切换"本体链路 on/off"，模型/提示词/题库/温度全部锁死，才能干净地测出本体的增益。

3. **推荐（分情形）：**
   - **若目标是 1–2 周内快速跑通实验、拿到"本体是否有效"的结论** → **DeepSeek Harness 更快**：自带 Web UI（`npx @deepseek-ai/dsh web`，127.0.0.1:3080）、自带 Trajectory 审计日志和 benchmarks 目录、TS 生态有成熟 RDF/SPARQL 库，本体工具写成插件即可。**代价**：版本不稳定（需锁版本+容器化）、它的默认工具集/提示词偏编码场景（需裁剪）、用它做"领域问答"是在用非设计目标的场景。
   - **若要长期沉淀、稳定复现实验、并与既有 Coze/Go 技术栈或未来生产系统打通** → **Eino 更稳**：生产级、强类型、图编排可精确控制"有/无本体"两条链路、回调/Trace/Eval 工具链完整。**代价**：Go 的 RDF/OWL 语义工具链弱于 Python/TS（OWL 推理、SHACL 基本要以独立服务形式接入）、Web UI 要自己写（可参考官方 ChatWithEino + A2UI 的 Web 示例）。
   - **诚实的第三选项（研究场景常被忽略）：Python 轻量 harness**（FastAPI + 一个极简工具调用循环，或 LangChain/LlamaIndex）。语义网工具链最完整：`rdflib`/`owlready2`（含 Pellet/HermiT 推理机封装）/`pyshacl`/`RDFLib SPARQL`，且你已在用 rdflib。**若只为本体验证实验，这是工程性价比最高的路径**；本文聚焦你点名的两者，但建议评审时一并考虑。

> 一句话：**比"快和现成"，Harness 赢；比"稳、可控、长期"，Eino 赢；比"本体语义工具链贴合度"，Python 赢。** 下文给证据和量化矩阵。

---

## 2. 背景与实验目标

| 项 | 内容 |
| --- | --- |
| 业务背景 | 正在构建 K8s 智能运维本体（资源层复用 FLUIDOS/HOCC，运维语义层自建；技术路线 OWL 2 RL + SHACL + Neo4j），需要一个对话式 Agent 让研究者/评审通过自然语言验证本体能力 |
| 核心目标 | ①对话中调用本体做检索/推理/约束校验来回答运维问题；②严格对照"挂本体 vs 不挂本体"的对话效果差异，量化本体贡献 |
| 用户 | 研究者本人、可能的论文评审/团队演示 |
| 成功标准 | 同一批"能力问题（CQ）"在两链路下的正确率/可解释性/幻觉率可量化对比；实验可复现；对话过程（工具调用、SPARQL、命中三元组）全程可审计 |
| 非目标（首期） | 高并发、多租户、公网发布、真实生产集群自动处置（只做只读验证，不做写操作） |

---

## 3. 证据台账（事实 / 来源 / 置信度）

| ID | 事实 | 来源 | 置信度 |
| --- | --- | --- | --- |
| E1 | Eino 是 CloudWeGo 开源的 **Go** LLM 应用框架，抽象 ChatModel/Tool/Retriever/PromptTemplate 等组件，提供图/链/Workflow 编排、ADK（ChatModelAgent/多 Agent/Interrupt-Resume/预置 Deep/Supervisor/Plan-Execute 模式）、回调（OnStart/End/Error/Stream）、流式处理、可视化调试、Trace（集成 APMPlus/Langfuse）与 Eval | [Eino 官方文档 Overview](https://www.cloudwego.io/docs/eino/overview/)；[pkg.go.dev/eino](https://pkg.go.dev/github.com/cloudwego/eino) | high |
| E2 | Eino 已在字节内部豆包、TikTok、Coze 等数百个服务集成，是内部 LLM 应用首选全代码框架；Coze Studio 的 Agent/工作流运行时即由 Eino 团队支撑 | [Eino 开源公告](https://www.cloudwego.io/docs/eino/overview/eino_open_source/)；Coze Studio 官方 README | high |
| E3 | Eino 官方提供从 Console 渐进到 **Web（A2UI 协议 + SSE 推送，localhost:8080）** 的完整 Agent 示例 ChatWithEino，含 Memory、Tools、Middleware、Callback、Interrupt/Resume、Graph Tool、Skill 共 11 章 | [Eino Quick Start / eino-examples](https://www.cloudwego.io/docs/eino/quick_start/) | high |
| E4 | Eino 组件实现独立成 module（eino / eino-ext），官方模型实现覆盖 OpenAI、Ollama、Ark（豆包）等；编排图编译期做类型检查 | Eino 文档/Overview | high |
| H1 | DeepSeek Harness（`dsh`）是深度求索官方开源的 **agent harness**，TypeScript 占比 96.9%，基于 **Cordis** 微内核、"everything-is-a-plugin"，理念 `Agent = Model + Harness` | [deepseek-ai/deepseek-harness 官方仓库](https://github.com/deepseek-ai/deepseek-harness) | high |
| H2 | 协议 **MIT**；2026-08-13 开源（开发者预览 v0.1）；README 明确标注 **"developer preview，迭代极快，将有兼容性破坏性变更"**；当前发布线 0.1.5-rc.2（2026-09 仍在频繁合入，1.6 万+ commits） | 官方仓库 README / Releases / 提交记录 | high |
| H3 | 自带 **Web UI**：`npx @deepseek-ai/dsh web`，默认 http://127.0.0.1:3080；需 Node.js（建议 18+/22+）；含 `apps/`、`website/`、`benchmarks/`、`snapshots/`、Trajectory 审计、Docker 沙箱等目录结构 | 官方仓库 README + 目录 | high |
| H4 | 模型不绑定 DeepSeek，支持多家厂商（近 40 家，二手资料口径）；预设 Standard/PTC/Minimal/Creator 等模式；内置 DSBench/LM-Eval 风格评测与 pytest 自纠错（部分为二手教程描述，需以官方文档复核） | [freeCodeCamp 综述](https://www.freecodecamp.org/news/what-is-an-agent-harness/)；CSDN 教程 | medium（模式/评测细节） |
| H5 | Harness 定位是**编码型 agent harness**：默认工具为 read_file/write_file/edit/bash/grep/glob/git/web_search/subagent，工作单元是"代码 diff + 跑测试"，与 Claude Code/Cline 同类 | 官方仓库工具契约文档；freeCodeCamp 行业综述 | high（定位）/ medium（工具清单细节） |
| X1 | Go 的 RDF/OWL 生态弱于 Python/JS：无对标 owlready2 的成熟 OWL-DL 推理封装，SHACL 引擎稀缺；SPARQL 客户端可用 | 通用工程认知，**建议在选型 PoC 中实测确认** | medium |
| X2 | TS 生态可用 `rdf-ext`/`sparqljs`/Comunica（SPARQL 查询）做本体接入；Python 生态有 rdflib/owlready2/pySHACL，最完整 | 各库官方文档 | high（存在性）/ medium（成熟度需实测） |
| X3 | 已有可直接复用的数据资产：三个本体文件（FLUIDOS k8s/fluidos/HOCC）+ 已抽取的 `ontology_data.json`（类/属性/个体/继承/domain/range）+ 单文件本体浏览器 HTML | 本项目 `ontologies/` 目录 | high |

---

## 4. 两个候选方案画像

### 方案 A：基于 Eino（Go）自建 Web Agent

- **形态**：Go 后端服务，用 ADK 的 `ChatModelAgent`（ReAct 式，自动处理工具调用/对话状态/推理循环）挂自定义工具；或用 Graph 编排精确画出两条链。前端可用官方 ChatWithEino 的 A2UI/SSE 方案或自写轻前端。
- **本体接入**：自定义 `Tool` / `Retriever` 组件，调用一个独立的"本体服务"（SPARQL endpoint，如 Apache Jena Fuseki / rdflib 服务 / Neo4j-n10s）；SHACL 校验、OWL 推理建议放在 Python 侧独立微服务，Go 通过 HTTP/gRPC 调用（规避 Go 语义工具链短板）。
- **A/B 实现**：Graph 里用一个 `Branch` 节点按实验标志位走"本体工具链"或"直答"；或同构两个 agent 配置，仅工具集不同。回调天然记录每轮工具调用，接 Langfuse 做轨迹对比。
- **模型**：OpenAI / 豆包 Ark / Ollama（本地 DeepSeek 也可经 OpenAI 兼容接口）均可换。

### 方案 B：基于 DeepSeek Harness（TypeScript，dsh）

- **形态**：直接用内置 Web UI 和主循环，本体能力封装成一个 **Cordis/dsh 插件**（SPARQL 查询、类层次检索、SHACL 校验）；用 Minimal/自定义模式**裁剪掉 bash/edit 等编码工具**，避免 Agent 跑偏去操作文件系统。
- **本体接入**：插件内用 Comunica/sparqljs 调 SPARQL endpoint；或让插件调 Python 本体服务。Trajectory 仅追加日志直接产出可审计轨迹。
- **A/B 实现**：通过"插件装配配置"做两套 profile（带本体插件 / 不带），同一题集回放；复用其 benchmarks/评测目录做 Pass@1 式对比。
- **模型**：默认 DeepSeek，可换近 40 家；但框架 API 仍在破坏性变动期。

### （参照）方案 C：Python 轻量自研 harness

- FastAPI + 前端单页，一个 ~150 行的工具调用循环；本体侧直接 `rdflib + owlready2(推理) + pyshacl + SPARQL`，全部在同一语言内完成，无需跨服务。最贴合研究、可控性最高，但 Web UI/会话/审计都要自己写（量不大）。**非你点名，仅作评审对照。**

---

## 5. 加权选型矩阵

评分 1–5（5 最好），权重按"本体研究 + A/B 实验"目标设定。

| 评估标准 | 权重 | A. Eino (Go) | B. DeepSeek Harness (TS) | C. Python 自研（参照） | 说明/证据 |
| --- | ---: | ---: | ---: | ---: --- |
| 本体/语义工具链贴合度 | 22 | 3（需外部服务） | 4（Comunica 可用） | **5**（owlready2/pySHACL） | X1/X2 |
| A/B 链路可控与可复现 | 18 | **5**（Graph 编译期确定 + 回调） | 3（插件可配，但主循环黑盒、版本漂移） | 4（全自控，但要自建） | E1/H2 |
| Web UI 交付速度 | 14 | 3（有 A2UI 示例，仍需写） | **5**（内置 Web UI，开箱） | 2（自写） | E3/H3 |
| 实验审计/评测配套 | 12 | 4（Trace/Eval/Langfuse） | 4（Trajectory+benchmarks，偏编码指标） | 3（自埋点） | E1/H3/H4 |
| 稳定性与长期维护 | 14 | **5**（生产验证、API 稳） | 2（developer preview，破坏性变更） | 4（自控，依赖少） | E2/H2 |
| 团队语言/学习成本 | 8 | 3（需 Go + 图编排学习） | 4（TS，npx 即跑，插件门槛） | 4（Python，已在用 rdflib） | — |
| 与既有资产/栈协同 | 7 | **5**（与 Coze 同源；本体资产语言无关） | 3 | 3 | E2/X3 |
| 场景匹配（领域问答 vs 编码） | 5 | 4（通用，需自建） | 2（编码 harness，需裁剪纠偏） | **5**（为研究定制） | H5 |
| **加权总分（满分 5）** | 100 | **3.95** | **3.51** | **3.80** | 见下注 |

> 计算（Σ 权重×评分 ÷ 100）：
> - **A = 395/100 = 3.95**：22×3+18×5+14×3+12×4+14×5+8×3+7×5+5×4 = 66+90+42+48+70+24+35+20
> - **B = 351/100 = 3.51**：22×4+18×3+14×5+12×4+14×2+8×4+7×3+5×2 = 88+54+70+48+28+32+21+10
> - **C = 380/100 = 3.80**：22×5+18×4+14×2+12×3+14×4+8×4+7×3+5×5 = 110+72+28+36+56+32+21+25
>
> 说明：权重显著影响结论。"本体语义贴合度 + A/B 可控 + 稳定性"权重高 → **Eino 与 Python 接近、且都高于 Harness**；若把"最快出 Web Demo"权重从 14 提到 25 以上，Harness 会反超。**这正是分情形推荐、而非给一个绝对赢家的原因。**

---

## 6. 推荐、放弃理由与反转条件

| 决策点 | 推荐 | 放弃 | 理由 | 反转条件 |
| --- | --- | --- | --- | --- |
| 快速实验（≤2 周拿结论） | **DeepSeek Harness** | Eino / Python 自研 | 内置 Web UI + Trajectory + benchmarks，本体写成插件即可，省时 | 若裁剪编码工具/适配问答场景发现要大量改内核，或升级再次破坏插件 API → 立即转 C |
| 长期/生产/与 Coze 栈融合 | **Eino** | Harness | 生产级、强类型、图编排精确控链、Trace/Eval 完整、与 Coze 同源 | 团队完全不打算维护 Go 服务 → 用 C 或 Harness |
| 纯研究、语义能力深度（推理/SHACL） | **Python 自研（C）** | A/B | owlready2/pySHACL/rdflib 同语言闭环，实验最可控 | 研究成果要进生产 Go/Coze 系统 → 用 Eino 重写外壳，本体服务保留 |
| 本体能力承载方式（三者共通） | **独立"本体服务"+ 薄工具适配** | 在 Agent 框架内硬写推理 | 解耦：同一本体服务可被任一框架/语言调用，A/B 与选型正交 | —— |

**核心工程判断（与选型解耦，强烈建议无论选哪个都这么做）：**
把系统拆成「Agent 外壳（可替换：Eino / dsh / Python）」+「本体能力服务（稳定：SPARQL/推理/SHACL/检索）」+「实验评测层」。这样：
- A/B 对比 = 同一外壳切换"是否调用本体服务"，**唯一变量**；
- 将来换框架不影响本体侧投入；
- 论文/评审时能清楚展示"本体服务被调用了什么、返回了什么"。

---

## 7. 目标架构（三链路共通的参考视图）

```
┌──────────────────────────────────────────────────────────────┐
│                        Web 对话界面                            │
│   （Eino A2UI/SSE 自写  │  dsh 内置 Web UI  │ Python SPA）      │
└───────────────┬──────────────────────────────────────────────┘
                │ 同一问题 + 实验标志位 {arm: ONTO | BASELINE}
┌───────────────▼──────────────────────────────────────────────┐
│                    Agent 外壳（可替换）                         │
│  对话状态/记忆 · 工具调用循环 · 提示词模板(两臂共用) · 轨迹记录    │
│              ┌───────────────┐                                 │
│   BASELINE ──┤ 不挂本体工具   │── 纯模型参数化知识作答            │
│              └───────────────┘                                 │
│   ONTO ──────┐                    ┌── 工具: 类层次/术语检索       │
│              ├─ 本体工具集(Tools) ├── 工具: SPARQL 查询         │
│              │                    ├── 工具: OWL 推理(子类/等价) │
│              │                    └── 工具: SHACL 约束校验       │
└──────────────┴───────────┬────────────────────────────────────┘
                            │ HTTP / SPARQL
┌───────────────────────────▼────────────────────────────────────┐
│                    本体能力服务（稳定内核）                       │
│  rdflib/owlready2(Python 推荐) · Jena Fuseki · SHACL 引擎        │
│  数据源: FLUIDOS k8s.ttl / fluidos.ttl / HOCC.owl / 自建运维层    │
│  (可选) Neo4j + n10s/RDF* 存故障传播图                            │
└────────────────────────────────────────────────────────────────┘
┌────────────────────────────────────────────────────────────────┐
│  实验评测层：固定 CQ 题库 · 两臂回放 · 轨迹/命中三元组落盘 · 打分   │
└────────────────────────────────────────────────────────────────┘
```

**本体接入的 4 种由浅到深方式**（可分级验证，避免一上来就做重推理）：
1. **术语/类层次检索**：输入问题→检索相关 Class/Property/label/comment/父子类（直接复用已抽取的 `ontology_data.json` 就能起步）。
2. **SPARQL 按需查询**：把问题转 SPARQL，查实例/关系（FLUIDOS 类多为平铺骨架，关系主要靠对象属性）。
3. **OWL 推理**：subClassOf 传递、等价类、对象属性传导（OWL 2 RL 可用规则引擎；复杂 DL 推理用 owlready2+HermiT/Pellet）。
4. **SHACL 约束校验**：对 Agent 给出的配置/结论做形状约束校验，体现"本体约束违规检测"这一独有价值。

---

## 8.「有本体 / 无本体」A/B 实验设计（本方案的真正核心）

**控制变量（两臂必须完全一致）：** 同一基座模型与版本、`temperature=0`（或固定 seed）、同一系统提示词模板、同一批问题、同一上下文窗口策略、同一回答长度上限。**唯一差异：ONTO 臂可用本体工具，BASELINE 臂不可用。**

**题库（Competency Questions，建议 30–60 条，分层）：**
| 类别 | 示例（K8s 运维） | 主要考什么 |
| --- | --- | --- |
| 术语/结构 | "Pod 和 Service 在本体里是什么关系？Pod 的 spec 包含哪些对象？" | 类层次/对象属性检索 |
| 关系推理 | "影响一个 Node 故障会波及哪些资源？"（多跳） | 推理/故障传播 |
| 约束校验 | "这个 Pod 配置缺了哪个必填字段/违反什么约束？" | SHACL（本体独有） |
| 反事实/幻觉 | "K8s 里有没有 XXX 资源？"（故意问不存在的概念） | 幻觉抑制/拒答 |
| 开放运维问答 | "ImagePullBackOff 可能原因？" | 泛化问答（本体帮助可能最弱，用作对照） |

**评测指标：**
| 指标 | 定义 | 数据来源 |
| --- | --- | --- |
| 答案正确率 | 与标准答案/金标 SPARQL 结果比对（精确/部分/错） | 人工 + 规则双评 |
| 接地率（grounding） | 回答中关键结论可追溯到本体三元组/推理路径的比例 | 工具轨迹 |
| 幻觉率 | 断言了本体中不存在的类/属性/关系的比例 | 对照本体校验 |
| 工具有效性 | ONTO 臂调用本体工具后答案被修正/增强的比例 | 轨迹前后对比 |
| 可解释性 | 是否给出类/属性 IRI、SPARQL、推理路径 | 人工量表 1–5 |
| 延迟/Token 成本 | 每问平均时延、额外工具调用成本 | 运行日志 |
| 拒答正确性 | 对超出本体范围问题是否恰当拒答而非编造 | 人工 |

**统计与复现：** 每题两臂各跑 N≥3 次（即使 temp=0 也建议复测稳定性）；用配对比较（同题两臂）报告正确率差与置信区间（McNemar 或配对 bootstrap）；全部提示词、模型版本、本体版本（git commit）、工具轨迹、原始回答落盘归档——这与 dsh 的 Trajectory、Eino 的回调/Trace 都能对接。

**已知参照：** 你此前调研的 OpenRCA（ICLR 2025）显示无图谱接地时纯 LLM 根因定位准确率不足 12%——可作为"BASELINE 臂预期偏弱"的先验，但**不要直接套用其数字作为你的结论**，需用自己的 CQ 题库实测。

---

## 9. 风险登记

| 风险 | 触发 | 影响 | 检测 | 缓解 | 降级/回滚 |
| --- | --- | --- | --- | --- | --- |
| Harness 破坏性变更致插件失效 | `dsh` 升级 | 实验中断、不可复现 | CI 跑题集冒烟 | 锁定固定版本 + 容器镜像 + 离线 vendoring | 回滚到锁定镜像；仍失败则转 Python 自研 |
| 用编码 harness 做问答，Agent 跑偏去读写文件/执行命令 | 默认工具集 | 脏轨迹、安全风险 | 轨迹审计 | Minimal 模式裁剪工具，只留本体插件，只读挂载工作区 | 禁用 bash/edit 插件 |
| Go 侧 OWL/SHACL 能力不足 | 选 Eino 且需复杂推理 | 本体独有价值做不出来 | PoC 阶段验证 | 推理/校验放 Python 微服务，Go 走 HTTP | 关键推理链路整体放 Python |
| A/B 变量污染（提示词/上下文不一致） | 两臂实现差异 | 结论不可信 | 评审 diff 两臂配置 | 共用同一提示词模板与组装代码，仅开关工具 | 重跑 |
| 本体太"骨架"导致 ONTO 增益不明显 | FLUIDOS 无故障语义、零继承边 | 实验得阴性结果 | 分类别看得分 | 先在自建"运维语义层（故障/告警/处置）"上出题；按第 7 节分级接入 | 阴性结果本身也是研究发现，如实报告 |
| SPARQL 由 LLM 生成出错 | 自然语言转查询 | 接地失败 | 校验查询语法/空结果 | 预置查询模板 + 结果为空时回退术语检索 | 回退到方式 1 |

---

## 10. 建议实施路线（轻量、可在本项目落地）

| 阶段 | 周期（人日，估算） | 交付物 | 依赖 |
| --- | --- | --- | --- |
| P0 本体服务最小化 | 2–3 | 一个只读本体服务：加载 3 个 TTL/OWL，暴露 `术语检索` + `SPARQL` 两个 HTTP 接口（Python+rdflib，复用 ontology_data.json） | 已有本体文件 |
| P0 CQ 题库 + 金标 | 2 | 30–60 条分层 CQ、标准答案/金标查询、评分表模板 | 自建运维语义（可选） |
| P1 选定外壳做双链路 | 3–5（Harness 更快/Eino 略慢） | 对话 Web 页 + BASELINE/ONTO 两臂 + 轨迹落盘 | P0 |
| P1 跑首轮 A/B | 2 | 配对结果、指标表、典型好/坏案例 | 题库 |
| P2 加深本体能力 | 3–5 | 加 OWL 推理（owlready2）与 SHACL 校验两类工具，再测 | P1 |
| P2 复现与报告 | 2 | 锁定版本/提示词/本体 commit，产出可复现实验报告 | 全部 |

> 最小可行闭环建议：**先用 Python 把 P0 本体服务 + 题库做出来（框架无关），再决定外壳是 Eino 还是 dsh**——这样选型不阻塞实验本体的核心投入。

---

## 11. 开放问题（需你确认）

| 优先级 | 问题 | 影响 |
| --- | --- | --- |
| P0 | 团队主力语言是 Go 还是 Python/TS？是否需要最终并入 Coze/Go 生产体系？ | 直接决定 Eino vs 其他 |
| P0 | 首要诉求是"最快出可演示 Demo"还是"严谨可复现实验/论文数据"？ | 决定 Harness vs Eino/Python |
| P1 | 模型用哪个？DeepSeek（本地/API）、豆包、OpenAI、还是 Ollama 本地部署？是否有离线/合规要求？ | 两方案的模型适配与部署 |
| P1 | 首期要做到第 7 节的哪几档（仅检索？还是必须含 OWL 推理 / SHACL）？ | 工作量与是否必须 Python 推理服务 |
| P2 | 自建"运维语义层"本体当前进度？是否已有故障/告警类可用于出题？ | 实验能否得到阳性结论 |

---

### 附：主要来源
- Eino 官方文档：https://www.cloudwego.io/docs/eino/overview/ ；快速开始（含 Web/A2UI 示例）：https://www.cloudwego.io/docs/eino/quick_start/ ；开源公告：https://www.cloudwego.io/docs/eino/overview/eino_open_source/ ；包文档：https://pkg.go.dev/github.com/cloudwego/eino
- DeepSeek Harness 官方仓库：https://github.com/deepseek-ai/deepseek-harness （MIT，developer preview）；文档站：https://deepseek-harness.github.io/deepseek-harness/
- 行业综述（harness vs framework 概念区分）：https://www.freecodecamp.org/news/what-is-an-agent-harness/

*本文框架事实据 2026-09 官方仓库/文档；Harness 处快速迭代期，落地前请以当时官方文档与锁版本为准。*
