# OpenBKN（github.com/openbkn-ai）实现分析与借鉴价值评估

> 调研时间：2026-09-22 ｜ 数据获取方式：GitHub API / raw 文件直读（沙箱网络无法 clone，故以文件级精读为主，未做全量代码统计）
> 说明：本文所有结论优先基于仓库内的**一手文件**（AGENTS.md、rules/、.bkn 样例、*.bkn 规范 README、LICENSE 等）；涉及 fork 来源、社区规模、star 数等为 GitHub 页面数据，活跃度相关的二手评价已单独标注。

---

## 一、项目全貌

OpenBKN 自我定位是**面向企业 AI Agent 的开源本体平台**（Open-Source Ontology Platform），目标是做 Palantir Foundry Ontology 的开源对标物。它不是单一仓库，而是一个 4 仓 composition：

| 仓库 | 语言 | Star | 职责 |
|---|---|---|---|
| [bkn-foundry](https://github.com/openbkn-ai/bkn-foundry) | Go 为主 | 521 | 后端基座：本体引擎 BKN、上下文装载 Context Loader、数据虚拟化 VEGA、执行工厂 Execution Factory、治理 BKN Safe、证据链 BKN Trace |
| [bkn-studio](https://github.com/openbkn-ai/bkn-studio) | TypeScript / React | 204 | Web 控制台，独立于 Foundry（Foundry **严禁**含 UI） |
| [bkn-sdk](https://github.com/openbkn-ai/bkn-sdk) | TypeScript + Python | 16 | 统一 SDK + `openbkn` CLI + 3 个 Agent Skill 包 |
| [bkn-samples](https://github.com/openbkn-ai/bkn-samples) | — | 14 | 可导入的知识网络样例与数据 |

**关键背景（二手信息，需自行核验）**：公开资料显示该项目于 2026-05 前后从 `kweaver-ai/kweaver-core` fork 并重品牌化而来，仓库中仍能看到 `docs: repoint or drop dead kweaver-ai references` 这类清理提交。这不影响其设计参考价值，但在采纳许可证和判断工程成熟度时要算进去。

活跃度：bkn-foundry 提交密集到 2026-09-21（本次调研前一天），issue/PR 编号已到 #1722 量级，`claude` 与人类账号混合提交——**Agent 深度参与生产级开发**已经是它的默认工作方式，这点后面单列。

---

## 二、核心实现拆解

### 2.1 建模范式：Object / Relation / Action / Risk + ConceptGroup + Metric

BKN 网络的一个完整"切片"是一个**目录**，而不是一张 ER 图或一个 JSON。以官方样例 `exchange-recovery`（Exchange 邮件灾备恢复）为例：

```text
exchange-recovery/
├── network.bkn            # 清单：把下面所有类型登记成一张总表
├── SKILL.md               # 由本体"编译"出的 Agent 操作剧本
├── CHECKSUM               # 全目录校验和，防篡改 + 支持 Diff
├── object_types/    8个   # 对象类（含 Data Properties / Logic Properties / Keys）
├── relation_types/ 11个   # 关系类（含依赖/流程/包含三类语义）
├── action_types/    7个   # 行动类（绑定工具、触发条件、参数绑定、影响契约）
├── risk_types/      2个   # 风险类（控制范围、策略、前置校验、缓解、回滚、审计）
├── concept_groups/  1个   # 概念分组（限定召回范围用）
├── metrics/         8个   # 指标（声明式聚合公式）
└── data/            *.csv # 样例事实数据
```

**值得注意的设计取舍**：它没有采用 OWL / RDFS / SHACL 这套 W3C 语义网栈，而是自建了一套够用即止的模型。理由是清楚的——企业场景要的是**可执行（executable）**而不是**可推理（reasonable）**：没有描述逻辑推理机，换来的是「一张 Markdown 表 = 一个可映射到真实 API 的对象视图」。这是对的方向，也是我认为最值得吸取的判断。

### 2.2 BKN Lang：Markdown + YAML frontmatter 作为本体 DSL

这是整个项目最原创、也最值得借鉴的一点。**每个 `.bkn` 文件 = YAML frontmatter（元信息）+ Markdown 多级标题（语义骨架）+ Markdown 表格（结构化字段）+ 内嵌 YAML 代码块（复杂逻辑）**。

对象类型样例（`object_types/backup_timepoint.bkn`）：

```markdown
---
type: object_type
id: backup_timepoint
name: 备份时间点副本
tags: [备份, 时间点, 灾备]
---

### Data Properties

| Name | Display Name | Type | Description | Mapped Field |
|------|--------------|------|-------------|--------------|
| snapshot_time | 备份时间 | datetime | 备份时间点 | snapshot_time |
| is_verified | 是否验证 | boolean | 是否经过可恢复验证 | is_verified |

### Keys
Primary Keys: id
Display Key: snapshot_name
```

行动类型里则用内嵌 YAML 表达**触发条件**和**影响契约**（`action_types/execute_recovery_task.bkn`）：

```yaml
# Trigger Condition —— 声明式状态机守卫
object_type_id: recovery_task
field: task_status
operation: ==
value: Pending

# Impact Contracts —— 事前声明"这次行动会新增什么对象"
impact_contracts:
  - object_type_id: recovery_server
    expected_operation: add
```

指标则是完整声明式聚合（`metrics/recovery_job_count.bkn`）：

```yaml
kind: atomic
atomic:
  condition: { field: recovery_result, operation: ==, value: 成功 }
  aggregation: { property: id, aggr: count }
  group_by:   [{ property: user_id, description: 按执行恢复操作的用户聚合 }]
  order_by:   [{ property: __value, direction: desc }]
  having:     { field: __value, operation: '>=', value: 2 }
```

**为什么这个设计聪明：**

- **人机双写、LLM 双读**。业务专家能直接改 Markdown，LLM 生成/修改 Markdown 的成功率远高于生成嵌套 JSON Schema——这直接决定了"用 AI 辅助建模"这件事能不能跑通。
- **天然 Git 友好**。diff 是行级的；对比 RDF/OWL 那种图序列化，这是数量级的可读性优势。
- **存得进数据库的一个字段**。README 提到"定义以全文形式存放在特定数据库字段，无复杂底层表耦合"——规避了本体变更带来的迁移地狱。
- **弱 Schema 配强工具链**。表格本身没有 schema 强约束，所以他们在外围补了 `parser / validator / serialize / checksum / differ` 五件套（`bkn-specification/bkn/` 下的 Go 包），把"格式松散"的代价转移到了 SDK 层。

**代价也要看清楚**：Markdown 表格对列数变化、单元格内含 `|`、多表同义等情况非常脆弱，类型演进要比 Protobuf/Avro 痛苦得多。他们用 CHECKSUM + validator 兜底，但这是补丁不是根治。

### 2.3 把知识网络当"包"管理：tar + CHECKSUM + Diff

这是我觉得最被低估的一处工程实现。`bkn` Go 包提供了：

| 能力 | 函数 | 意义 |
|---|---|---|
| 目录 ⇄ Network | `LoadNetwork` / `LoadNetworkWithFS` | 支持 `MemoryFileSystem`，测试与 tar 解包统一 |
| Network ⇄ tar | `WriteNetworkToTar` / `PackDirToTar` | 知识网络**可分发、可版本化、可归档** |
| 校验和 | `GenerateChecksumFile` / `VerifyChecksumFromTar` | 分发完整性校验，等价于容器镜像 digest |
| 差异比对 | `DiffNetworksFromTar(old, new)` → `Creates() / Updates() / Deletes()` | 本体升级前先看"这次改动会增删什么对象/关系" |

**这是在拿容器镜像/包管理的心智模型治理知识资产**。本体从来不是一次性建模，而是持续演化；没有 diff 就无法评审、无法灰度、无法回滚。大多数本体项目做到"能建模"就停了，很少有人把"本体的变更管理"当成一等工程问题。这一点强烈建议直接抄。

### 2.4 Risk Type 是一等公民（这是它比多数同类项目更成熟的地方）

`risk_types/production_data_overwrite.bkn` 的完整结构：

- **Control Scope** — 适用边界
- **Control Policy** — 按条件分级（无 / 中 / 高，高风险"必须用户确认"）
- **Pre-checks** — 声明式校验表：`Object | Check | Condition | Message`
  - `recovery_task / property:recovery_destination / == production / 必须用户确认`
- **Risk Mitigation** — 缓解动作（如"执行前自动创建当前环境快照"）
- **Rollback Plan** — 回滚步骤
- **Audit Requirements** — 审计要求（含"日志至少保留 180 天"这种可落地条款）

把"风险"做成和对象、关系同级的建模原语，而不是散落在代码里的 if-else，意味着**权限策略、人工审批、审计留痕可以被统一推导**，而不是每个行动各自为政。给企业的 Agent 系统做设计时，这是必选项。

### 2.5 从本体到 Agent：SKILL.md 是关键闭环

`examples/exchange-recovery/SKILL.md` 不是人写的产品文档，而是**本体 + 行动 + 风险编译后的 Agent 操作剧本**，包含：

- 工作流状态机：`Accident Request → Think → Risk Evaluate → Plan → Act → Report`
- 每个阶段的**决策规则**（如"邮件被删除 → 邮件级恢复；服务器数据丢失 → 服务器级恢复"）
- 引用具体 `action_type` 的执行步骤和参数
- **明确的行为约束**，例如反复强调：

  > 每次恢复请求都是独立的恢复操作，不应该查询或依赖历史恢复记录。

  这类"反 LLM 惯性"的负向约束写进 Skill，是很务实的做法。
- 风险等级矩阵，并规定"不确认则终止"

这一层的价值在于：**本体不再是给数据库用的 schema，而是给 LLM 用的 operating procedure**。绝大多数本体项目缺的正是这一跳。

### 2.6 运行时：Context Loader / BKN Trace / BKN Safe

**Context Loader** — 解决 Agent 侧的上下文问题，提供两级接口：
- `search_schema`：按**概念分组**限定范围召回本体**结构**（cache ← `concept_groups/*.bkn` 在这里起作用）
- `kn_search`：召回**实例**

这个「先 schema 后 instance」的两段式，本质是把 Text-to-SQL 里"先给 LLM 表结构再让它写查询"的经验，提升成了受本体约束的正式协议。配套能力带有 Retrieved recall → Ranker（粗排/精排）。

**BKN Trace** — 证据链做得相当扎实（`bkn-sdk/TRACE.md`）：
- 三级实体：**Conversation → Interaction → Operation(attempt)**
- `operation_id + attempt` 粒度记录调用事实，重试会申请新 attempt
- **PayloadEnvelope 固定 1 MiB**：阈值内 `inline`，超限 `omitted/payload_too_large`，序列化失败 `omitted/serialization_failed` —— 明确不因为记录而放大存储
- **Receipt（收据）机制**：`receipt_status` 区分 `pending` / `completed`，并强调"要看 `receipt_status`，而不是看 `value` 是否为 null"
- CLI 侧 `openbkn context tool-call --receipt` 返回 `{ value, bkn_receipt }`
- **兜底原则写死在契约里**："Trace 终态写入失败不能覆盖已经产生的业务结果或异常"

这几条规定非常成熟，属于可以直接借鉴到自有 Agent 系统的部分，尤其是"追溯失败不得影响业务结果"这条——很多可观测方案在这里会翻车。

**BKN Safe** — 统一身份 / 权限 / 策略入口，权限下沉到具体对象和行动粒度，支持审批与撤销。

**BKN-lang ↔ VEGA ↔ Execution Factory**：VEGA 做多源数据虚拟化（`refactor(vega): rename built-in catalog and resource marker`），屏蔽 ERP/MES/CRM/SQLServer 等异构源；Execution Factory 编排 tool / MCP / Skill。注意：早期 README 里的 `vega/`、`web/`、`sql/` 目录在当前 `adp/` 下已不存在，说明 close-source/server consolidation 正在发生，README 有滞后。

### 2.7 前端：未必好看，但章程值得读

`bkn-studio/AGENT_COMPOSITION_CHARTER.md`（《智能体整合章程》）是少见的高质量前端模块治理文档，核心约束：

- **页面不是复用单元，`scene` 才是**。路由页只做壳，业务逻辑收敛到 `scenes/`
- **契约先于实现**：`contracts/` 先定义输入输出，再写 scene/service
- **`module.manifest.ts` 是能力清单**：含可暴露场景、输入摘要、权限依赖、后端依赖、是否要求壳层、是否支持嵌入/只读 —— 显式给**人和 Agent**读
- 标准结构：`components/ contracts/ locales/ pages/ scenes/ services/ types/ index.ts module.manifest.ts navigation.tsx routes.tsx`
- **禁止深层 import** 其他模块私有文件；变更 `index.ts / contracts / manifest` 视为对外能力变更，必须人工 review
- 场景必须声明壳层依赖，且**跳转、消息、确认框、数据刷新**四种行为要允许宿主替换

这份章程的潜台词是：**未来的消费方一半是 Agent，模块边界必须机器可读**。如果你在做任何"会被 AI 组装"的前端平台，这份文档几乎是模板级的。

---

## 三、工程治理：真正拉开差距的地方

这部分我认为比架构本身更值得学。

### 3.1 AGENTS.md 分层规则系统

根目录 `AGENTS.md` 是一个**入口而非全集**，它要求 Agent「进入任何子目录前，逐级读取从根到目标目录的每一个 AGENTS.md，深者优先」，并把规则外挂到 `rules/`：

| 文件 | 内容 |
|---|---|
| `rules/WORKFLOW.md` | 人机如何协作 |
| `rules/CONTRIBUTING.md` | 分支、提交、风格 |
| `rules/ARCHITECTURE.md` | 架构与模块边界 |
| `rules/DEVELOPMENT.md` | API / HTTP / 错误约定 |
| `rules/TESTING.md` | 测试约定 |
| `.github/CODEOWNERS` | review 路由 |

好处：规则可以按模块就近扩建，根目录不会被撑爆，Agent 又总能获得上下文作用域内的全部约束。**这是 multi-agent 协作下比单文件 AGENTS.md 更可扩展的形态。**

### 3.2 Agent 的"硬规矩"写得极其具体

`AGENTS.md` 里的 Hard Rules 不是口号，是可执行约定：

- **先 diff 后外部写**：改完本地验证后，**先呈现 diff**，不得 commit / push / 建 PR / 发评论
- **review 反馈需重新授权**：修 review 意见时，早前的授权不自动延续
- **Issue 准入制**：只能领 `agent-ready` 标签的 Issue（验收标准完整 + 可独立完成 + 未分配），且验收标准需人类 `ac-approved`
- **风险操作熔断**：部署、删改数据、schema 迁移、生产配置、密钥权限、大版本依赖升级、跨服务破坏性变更 —— 一律不执行，改为提交「做什么 / 影响半径 / 回滚方案」三段式确认，打 `awaiting-confirmation` 等 Owner 打 `owner-confirmed`
- **红线**：不得 approve/merge PR、不得跳过 CI
- **卡住就交还**：explicitly 要求 comment blocker → 归还 triage → 清空 assignee → 打 `needs-human`

标签体系 `agent-ready / ac-approved / awaiting-confirmation / owner-confirmed / by-agent / needs-human` 构成了一条完整的**人机权限分层流水线**。这是目前我见过的、把 Agent 自治边界定义得最清晰的一套。

### 3.3 架构规则可执行化

`rules/ARCHITECTURE.md` 的规定都是**可用 CI 检验**的：

- **Foundry 严禁含 UI**（无 React/Vue/静态资源/路由）
- **依赖单向**：产品 → Foundry，禁止反向依赖
- **禁止页面级 BFF**：新增后端前必须回答 4 个问题（是否有持久化领域数据？是否需服务端权限合规逻辑？是否有长期演化的领域模型？是否被多产品复用？）全"否"则不许新建
- **服务预算硬约束**：Foundry 后端微服务 **< 5 个**，CI 要自动计数，超限需显式豁免 + 合并计划
- **可选组件必须可关**：禁用某些组件后系统仍能启动，消费方优雅降级
- **API 三级制**：Public / Internal / Experimental，跨组件只能依赖 Public
- **兼容性定义到字段级**：请求侧不得把 optional 改 required；响应侧只增不删不改；行为语义不得"同名异义"
- **Skill 也算契约**：`name` 一经发布不得改；`input_schema` 只增可选字段，`output_schema` 只增字段；破坏性变更只能靠新 version + 废弃窗口

「**组件可选 + 优雅降级 + 服务预算上限**」这三件事组合起来，实际上是在对抗企业级平台的熵增。很少有团队愿意写进规则，更少愿意写进 CI。

### 3.4 CI 里有几个"非典型"检查很见功力

从 `.github/workflows/` 看：

- `ci-i18n-hardcoded.yml` + `ci-i18n-resources.yml`：扫描硬编码中文，并且用**棘轮（ratchet）机制**管理存量 —— 提交信息里能看到 `Regenerated with check_hardcoded_chinese.py --update-baseline: 2605 -> 2585, removals only, no new violations`。**允许存量、绝不允许新增**，这是还技术债唯一现实的做法（顺带一提：基线 2605 条也说明国际化债务确实很重）。
- `lint-no-ee-import.yml`：阻断开源仓引入商业版代码
- `lint-migrations.yml`、`lint-branch-name.yml`、`lint-commit.yml`、`lint-workflow-files.yml`：迁移/命名/提交/gitops 全链路卡点
- `automation-claude-review.yml`：AI review 进流水线任命环节
- `automation-route-issue.yml`：Issue 自动路由

### 3.5 文档与交付一致性

- 每个中英文档成对（`README.md` + `README.zh.md`）
- 每仓都有 `VERSION`、`NOTICE`、`LICENSE` 三件套，`NOTICE` 做**逐组件授权明细**
- 部署链路完整：`preflight.sh`（检查/修复/预览，退出码 0/1/2 语义明确）→ `deploy.sh openbkn install` → `onboard.sh`（注册 LLM + embedding + 建用户），且 onboard 幂等可重入
- OpenAPI 3.1 契约 + redocly 配置 + 版本化文档站
- per-service CHANGELOG

---

## 四、「值得借鉴」分级清单

### A. 可以直接抄（低风险、高回报）

| # | 做法 | 为什么值 |
|---|---|---|
| 1 | **Markdown + YAML frontmatter 作为领域 DSL** | LLM 可读可写、人类可审、Git 友好。做配置化业务模型 / 规则 / 指标定义时优先选这条路，别上来就 JSON Schema |
| 2 | **知识资产包化：目录 → CHECKSUM → tar → Diff** | 让模型资产可评审、可灰度、可回滚。任何"配置即资产"的系统都需要这一步 |
| 3 | **Risk 提升为一等建模原语**（含 Pre-checks / 缓解 / 回滚 / 审计要求四段） | 把散落的 if-else 变成可推导的治理策略，是 Agent 上生产的前提 |
| 4 | **AGENTS.md 分层 + rules/ 外挂 + 深者优先** | 比单文件 AGENTS.md 可扩展得多 |
| 5 | **Agent Issue 标签流水线**（agent-ready / awaiting-confirmation / owner-confirmed / by-agent / needs-human） | 人机权限边界机器可读，可直接复制这套标签与语义 |
| 6 | **i18n 棘轮机制**（baseline 只降不升） | 唯一现实的技术债偿还法 |
| 7 | **Trace 铁律**：追踪失败不得覆盖业务结果；`PayloadEnvelope` 定长阈值 + `omitted` 显式标记；`receipt_status` 而非返回值判成功 | 可观测性不反客为主的三条具体做法 |
| 8 | **场景 `"_____"` 的行为约束写进 Skill**（"不依赖历史记录"这类负向指令） | LLM 最容易犯的错要显式写成 Skill 条款 |
| 9 | `preflight.sh` 三态退出码 + `--list-fixes` 预览 | 安装脚本的工程化标杆 |

### B. 借鉴思路（要按自身场景改造）

| # | 做法 | 注意 |
|---|---|---|
| 10 | **先 schema 后 instance 的两段式召回** | 需要 concept_group 这类"召回作用域"机制配合，否则 schema 本身也会撑爆上下文 |
| 11 | **本体 → SKILL.md 编译** | 这是"让本体被运行"的闭环关键；但样例里的 SKILL.md 目前看仍是人工维护痕迹很重，自动编译程度需自行验证 |
| 12 | **服务预算（< 5）+ 组件可选 + 优雅降级** | 约束要配合 CI 自动计数才有意义；数字本身要按团队规模调 |
| 13 | **Skill 也纳入向后兼容契约** | 很多团队管了 REST 忘了 MCP/Tool schema，这是前沿但必要的 |
| 14 | **module.manifest.ts 机器可读能力清单** | 若你的前端不会被 Agent 组装，可简化 |
| 15 | **弱 Schema + 强 SDK 工具链**（Markdown 表格 + parser/validator/differ 兜底） | 换取了 LLM 友好性，但工具链必须做扎实，否则半年后不可维护 |

### C. 需要谨慎评估，别盲目引入

| # | 事项 | 理由 |
|---|---|---|
| 16 | **许可证** | 见下节。SDK 是 Apache 2.0，但 BKN Safe / BKN Studio 等走 OpenBKN License（modified Apache 2.0），**禁止未授权的多租户/托管/白标使用** |
| 17 | **Markdown 表格承载强类型 Schema** | 演进成本高，务必先做 validator + migration 工具，否则半年后不可维护 |
| 18 | 宣称的性能数字（准确率 93%、Token 省 50%、TCO 降 70%） | README 未给出可复现的基准条件与数据集，当作营销口径，勿直接引用 |
| 19 | 从 KWeaver fork 而来的历史包袱 | 中文硬编码基线 2605 条、README 与目录不一致等都是证据；采纳前先看目标模块的新旧程度 |

---

## 五、许可证必须单独说

`LICENSE-OPENBKN.txt` 是一份 **modified Apache 2.0 + Additional Conditions**，且**只适用于源码头注释中明确引用它的模块**（包括但不限于 BKN Safe、BKN Studio）；源自 upstream `kweaver-ai/kweaver-core` 的组件仍是纯 Apache 2.0，`NOTICE` 有逐组件明细。`bkn-sdk` 明确是 Apache 2.0。

对使用方的实质限制：

1. **允许**：为单个最终客户做实施/集成/部署/培训/运维服务（客户自控环境），**不需要**商业订阅；二次开发扩展、连接器、Skill 也 OK（前提是别抄商业专有代码、别绕过控制）
2. **禁止（需书面授权）**：**共享/多租户服务**、把 Licensed Modules 的实质功能做成**托管平台**、**白标 / OEM** 竞品、用一个客户的订阅给另一个客户提供服务
3. **Commercial Entitlements**（Pro / Enterprise Standard / 行业方案）必须订阅才可用，且不得绕过 License 检查
4. **前端/控制台的 Logo 与版权声明不得去除**（除非订阅含产品定制权）
5. **贡献条款**：你提交的代码，OpenBKN 可以任意商用、任意再许可
6. **条款可调整**：对未来版本生效，不溯及既往

**结论**：拿它学习和借鉴架构思想、在自己的项目里重实现这套理念，完全没问题；但如果打算 fork BKN Studio / BKN Safe 做成自己的产品对外提供服务，**不行**，得走商业授权。

---

## 六、给"自己构建本体"的最小落地路径

如果目标是借鉴它做一个自己的轻量本体/语义层，我建议按这个顺序，而不是照搬全部：

1. **先定义 DSL**：一个实体类型 = 一个 `.md` 文件，frontmatter 放 `id/name/tags`，正文用 `Data Properties`（含 `Mapped Field` 物理映射）、`Keys` 两张表起步。**不要一开始就上 Relation/Action/Risk 全套。**
2. **同步做 parser + validator + checksum + differ**（Go 或 Python 均可）。这一步没做就先别写编辑器，否则模型资产很快失去可控性。
3. **加 `concept_groups`** 控制召回作用域——这是控制 LLM 上下文成本最便宜的手段。
4. **选一个窄业务切片**（参考 exchange-recovery：8 个对象 + 7 个行动 + 2 个风险就够跑完整闭环），**坚持"从业务切片渐进"，别做大而全本体**——这也是官方方法论里明确反对"大本体陷阱"的点。
5. **产出 SKILL.md**：把工作流、决策规则、负向约束、风险矩阵写进去。这一步做完，本体才真正"能跑"。
6. **最后才做 Trace/Safe**：Conversation → Interaction → Operation(attempt) 三级 + `PayloadEnvelope` + 追踪失败不覆盖业务结果。Permissions 下沉到对象/行动粒度。
7. **配套治理**：AGENTS.md 分层、Issue 标签流水线、i18n 棘轮 —— 这些零成本、早引入收益最大。

---

## 七、一句话总结

OpenBKN 的真正价值不在于它是"开源版 Palantir"（connector 广度、社区规模、生产验证都还差数量级），而在于它把**本体从"建模产物"推进到了"可版本、可diff、可执行、可治理、可追溯的工程资产"**——并且把这套工程纪律（包化的知识资产、Risk 一等公民、manifest 化能力、Agent 权限标签流水线、i18n 棘轮）写进了规则和 CI。这些是任何团队做 Agent-native 系统都能直接拿走的部分。

---

## 附：关键文件索引

| 主题 | 路径 |
|---|---|
| Agent 协作总则 | `bkn-foundry/AGENTS.md` |
| 架构硬约束 | `bkn-foundry/rules/ARCHITECTURE.md` |
| BKN Go SDK 说明 | `bkn-foundry/adp/bkn/bkn-backend/server/bkn-specification/README.md` |
| 网络清单样例 | `.../examples/exchange-recovery/network.bkn` |
| 对象/行动/风险/指标样例 | `.../examples/exchange-recovery/{object_types,action_types,risk_types,metrics}/*.bkn` |
| Agent 剧本 | `.../examples/exchange-recovery/SKILL.md` |
| Trace 接入规范 | `bkn-sdk/TRACE.md` |
| 前端整合章程 | `bkn-studio/AGENT_COMPOSITION_CHARTER.md` |
| 许可证 | `bkn-foundry/LICENSE-OPENBKN.txt`、`bkn-foundry/NOTICE` |
