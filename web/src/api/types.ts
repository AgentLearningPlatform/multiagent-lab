// 与后端 §5 模型对齐的前端类型

export interface Agent {
  id: string
  name: string
  description: string
  instruction: string
  model_conn_id: string | null
  temperature: number | null
  max_tokens: number | null
  max_iteration: number
  tools: string[]
  skills: string[]
  mcp_servers: { name: string; url: string }[]
  runtime_backend: string
  /** M13 §6.16：推理后端（eino-adk 自研默认；claude-code/opencode/aider 外部 CLI） */
  inference_backend: string
  // REQ-137：非内置后端登记的原 logo 图标 URL（未配置回退默认图标）
  logo_url?: string
  /** REQ-131/M18：对外 MCP 服务化（enabled/token/tool_name；token 为 Agent 级 Bearer） */
  mcp_serve?: { enabled: boolean; token?: string; tool_name?: string }
  created_at: string
  updated_at: string
}

/** REQ-131/M18：mcp-serve 管理端点返回（Token 掩码 + 调用示例） */
export interface McpServeInfo {
  enabled: boolean
  tool_name: string
  endpoint: string
  token_mask?: string
  curl?: string
  configured: boolean
}

/** M13 §6.16：推理后端探测状态（GET /api/inference-backends） */
export interface InferenceBackendStatus {
  name: string
  available: boolean
  version?: string
  path?: string
  reason?: string
  default: boolean
  capabilities: {
    chat: boolean
    stream: boolean
    skills_mode: 'tools' | 'instruction' | 'none'
    mcp_mode: 'tools' | 'instruction' | 'none'
    agent_as_tool: boolean
    workflow: boolean
    resume: boolean
  }
}

export interface Project {
  id: string
  name: string
  description: string
  collab_mode: string
  workflow_mode: string
  constraints: string
  agent_ids: string[]
  coordinator: string
  /** REQ-101：绑定的本地目录（绝对路径；空串 = 未绑定） */
  local_dir: string
  created_at: string
  updated_at: string
}

// ---- M12 项目本地目录（REQ-101/102/103）----

/**
 * 目录检测结果（POST /api/projects/validate-dir；REQ-133 分字段直连）。
 * 前端逐字段渲染，不按本机 OS 规则推断路径形态：
 * - format_ok：路径为绝对形态（跨运行时口径）；
 * - reachable：路径形态与部署主机一致——false（远程部署，如 Linux 后端 + Windows 本机目录）时
 *   存在性未校验，exists/is_dir 为 null，不得显示「不存在/非目录」。
 */
export interface DirValidation {
  format_ok: boolean
  reachable: boolean
  exists: boolean | null
  is_dir: boolean | null
  is_git: boolean
  git_branch?: string | null
  git_commit?: string | null
  git_dirty?: boolean | null
  error?: string | null
}

/** 目录条目（GET /api/projects/{id}/dir-files） */
export interface ProjectDirEntry {
  name: string
  is_dir: boolean
  size: number
  mod_time: string
  /** git porcelain 状态（M/A/??/D 或语义词）；非 git 或未跟踪为空 */
  git_status?: string | null
}

export interface ProjectDirListing {
  path: string
  entries: ProjectDirEntry[] | null
}

// ---- M12 REQ-102 深度版：Git 视图（提交历史 / 分支 / 变更明细）----

/** 提交历史条目（GET /api/projects/{id}/git-log） */
export interface GitCommit {
  hash: string
  short: string
  author: string
  date: string
  subject: string
  /** decorations：HEAD -> main / origin/main / tag: x（去掉 HEAD -> 前缀） */
  refs?: string[]
  /** 父提交 ≥2 即合并提交 */
  merge?: boolean
}

/** 分支条目（GET /api/projects/{id}/git-branches） */
export interface GitBranch {
  name: string
  current: boolean
  is_remote: boolean
  short_commit: string
  date: string
}

/** 变更文件（numstat；add/del = -1 表示二进制或合并提交无统计） */
export interface GitFileChange {
  path: string
  add: number
  del: number
}

/** 工作区未提交变更（porcelain 状态码 + numstat） */
export interface GitWorkingFile {
  path: string
  code: string
  staged?: boolean
  add?: number | null
  del?: number | null
}

export interface Conversation {
  id: string
  scope: 'agent' | 'project'
  agent_id: string | null
  project_id: string | null
  title: string
  kb_id: string | null
  enable_kb: boolean
  runtime_profile_id: string | null
  ontology_enabled: boolean
  /** 会话级技能开关（后端列待跟进；前端按 `?? true` 兼容默认开） */
  enable_skills?: boolean
  /** 中断挂起信息 JSON（M11 收尾：ask_human 等待答复；空 = 无挂起） */
  interrupt_state?: string | null
  /** REQ-135②：对话级工具审批覆盖（空=跟随智能体级 | on | off） */
  tool_approval?: string | null
  top_k: number
  min_score: number
  created_at: string
  updated_at: string
}

export interface Message {
  id: string
  conversation_id: string
  role: 'user' | 'assistant' | 'system' | 'tool'
  content: string
  meta?: string
  created_at: string
}

export interface ModelConnection {
  id: string
  name: string
  conn_type: 'chat' | 'embedding'
  protocol: string
  base_url: string
  model_name: string
  api_key_hint: string
  has_key: boolean
  enabled: boolean
  is_default: boolean
  /** REQ-148 供应商分组 ID（分组标识与 BaseURL 解耦，同供应商可多实例） */
  provider_group_id?: string
  /** REQ-148 组别名快照（List/Get 联查返回，展示层用；仅显示不改真名） */
  provider_alias?: string
  created_at: string
  updated_at: string
}

/**
 * 自动发现模型（ASSUMED 后端契约，接口可能尚未就绪）：
 * POST /api/model-connections/{anchorId}/list-models
 * 后端解密锚点连接的 Key，调用其 OpenAI 兼容的 GET {base_url}/models，返回可用模型 id 列表。
 */
export interface ProviderModelList {
  models: string[]
}

/**
 * 使用统计（ASSUMED 后端契约，接口可能尚未就绪）：
 * GET /api/stats/usage?group_by=model|agent|project
 */
export type UsageGroupBy = 'model' | 'agent' | 'project'

export interface UsageRow {
  key: string
  label: string
  calls: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
}

export interface UsageStats {
  rows: UsageRow[]
}

/** SSE 平台事件（方案 §7 统一协议） */
export interface PlatformEvent {
  type: string
  run_id?: string
  ts?: string
  data?: any
}

/** run_event 持久化事件（GET /conversations/{id}/events，历史时间线回放） */
export interface RunEventDTO {
  id: string
  conversation_id: string
  run_id: string
  type: string
  data?: string
  created_at: string
}

// ---- M6 知识库（02 文档 §5.1/§6.9/§8） ----

export interface KnowledgeBase {
  id: string
  name: string
  description?: string
  /** M14 D-KB4 双子模块：rag | graphrag（老数据缺省 = rag） */
  mode?: 'rag' | 'graphrag'
  /** @deprecated 后端已移除该字段，仅新建表单兼容保留 */
  store_backend?: 'qdrant' | 'sqlite'
  top_k: number
  min_score: number
  /** M16/REQ-129①：库级 KG 抽取模型连接（空 = 默认 chat）与提示词覆写 */
  kg_conn_id?: string
  kg_prompt?: string
  doc_count?: number
  chunk_count?: number
  created_at: string
  updated_at: string
}

/** graphrag 文档索引后的 KG 抽取结果（M14 ②⑥ 建制；D-O15 起自研抽取，degraded = 非阻断降级） */
export interface KBGraphragInfo {
  ok?: boolean
  method?: 'llm' | 'lightweight'
  chunks?: number
  entities?: number
  relationships?: number
  degraded?: boolean
  error?: string
  warnings?: string[]
}

export interface KBDoc {
  id: string
  kb_id: string
  title: string
  chunk_count?: number
  status: 'pending' | 'indexing' | 'success' | 'failed'
  error?: string
  graphrag?: KBGraphragInfo
  created_at: string
}

/** 检索试运行命中（§6.9 retrieval 事件 hits 结构） */
export interface KBHit {
  doc: string
  seq: number
  score: number
  excerpt: string
}

/** 检索响应（M14 ③④：mode = 实际生效的检索路径；degraded = KG 无命中/异常回退向量） */
export interface KBSearchResult {
  kb_id: string
  mode?: 'rag' | 'graphrag'
  degraded?: boolean
  error?: string
  hits: KBHit[]
}

// ---- O13 由知识库构建本体（REQ-108，04 §3.7；主平台 4 端点） ----

/** KB 选择器条目（GET /api/kbs/selectable-for-ontology-build） */
export interface OntoBuildSelectableKB {
  id: string
  name: string
  description?: string
  mode?: 'rag' | 'graphrag'
  doc_count: number
  chunk_count: number
  kg_entities: number
  kg_relationships: number
  kg_ready: boolean
  updated_at?: string
}

/** 生成侧结构校验报告（入库时构建平面 PUT spec 做权威校验） */
export interface OntoBuildSpecReport {
  ok: boolean
  errors: ValidationError[]
  warnings?: string[]
}

/** build-from-kb 结果（spec_json 为构建平面 Spec 同形草稿；cqs = REQ-90 能力问题） */
export interface OntoBuildResult {
  kb_id: string
  kb_name: string
  strategy: 'chunk-llm' | 'kg-direct' | 'hybrid'
  method?: string
  cq_mode: 'auto' | 'custom' | 'skip'
  cqs?: string[]
  rounds: number
  chunks_used: number
  truncated?: boolean
  spec_json: Spec
  validation_report: OntoBuildSpecReport
  warnings?: string[]
}

/** chunks-to-kg 结果（D-O15：POST /api/kg/{id}/rebuild 显式重建自存 KG；graphrag 复用 M14 GraphragInfo） */
export interface ChunksToKGResult {
  kb_id: string
  chunks: number
  graphrag: KBGraphragInfo
}

/** kg-to-spec-json 结果（策略 B 独立入口：KG 薄映射） */
export interface KGToSpecResult {
  kb_id: string
  method?: string
  kg_entities: number
  kg_relationships: number
  spec_json: Spec
  validation_report: OntoBuildSpecReport
}

// ---- M7 技能（02 文档 §5.2 skill DDL / §6.12） ----

export interface SkillResource {
  name: string
  content: string
}

export interface Skill {
  id: string
  name: string
  description: string
  instruction: string
  tools: string[]
  resources: SkillResource[]
  builtin: boolean
  enabled: boolean
  created_at: string
  updated_at: string
}

// ---- M5 工具注册表（02 文档 §6.8：id/name/desc，前端勾选落 agent.tools） ----

export interface ToolInfo {
  id: string
  name: string
  description: string
  source?: string
}

// ---- M8 本体对接（构建平面 :8091 / 运行平面 :8090，同源反代；契约以本体平面源码为准） ----

/** 本体元数据（GET /api/ontologies 为裸数组；无 status/progress，前端按阶段派生） */
export interface Ontology {
  id: string
  name: string
  description?: string
  version?: number
  forked_from?: string | null
  created_at?: string
  updated_at?: string
  n_concepts?: number
  n_relations?: number
  n_instances?: number
}

/** Spec 概念（S2 编辑对象） */
export interface SpecConcept {
  name: string
  label?: string
  definition?: string
  parents?: string[]
}

/** Spec 关系（有向 from → to） */
export interface SpecRelation {
  name: string
  label?: string
  definition?: string
  from: string
  to: string
}

/** Spec 实例关系 */
export interface SpecInstanceRelation {
  rel: string
  target: string
}

/** Spec 实例 */
export interface SpecInstance {
  name: string
  concept: string
  attributes?: Record<string, unknown>
  relations?: SpecInstanceRelation[]
}

/** 本体 Spec（GET/PUT /api/ontologies/{id}/spec；PUT 全量、校验门控、递增 version） */
export interface Spec {
  name: string
  description?: string
  concepts: SpecConcept[]
  relations: SpecRelation[]
  instances: SpecInstance[]
}

/** 校验错误（PUT spec 400 / POST validate） */
export interface ValidationError {
  path: string
  message: string
}

/** 构建产物元数据（GET /api/ontologies/{id}/artifacts） */
export interface ArtifactMeta {
  format: string
  size: number
  is_normalized?: boolean
  imported_at?: string
}

/** 导入报告（POST /api/ontologies/import） */
export interface ImportReport {
  format: string
  lossy: boolean
  warnings: string[]
  lossy_note?: string
}

/** AI 草案结果（POST /api/ontologies/ai-draft） */
export interface AiDraftResult {
  spec: Spec
  rounds: number
  warning?: string
}

// ---- OntoChat 多轮引导（REQ-103 模式 A）----

/** 会话阶段：cq 列 CQ → domain 逐轮补全 → draft/refine 草稿与修正 → done 已入库 */
export type OntoChatStage = 'cq' | 'domain' | 'draft' | 'refine' | 'done'

export interface OntoChatMessage {
  role: 'user' | 'assistant' | 'system'
  content: string
  ts: string
}

export interface OntoChatContext {
  description: string
  cqs?: string[]
  hints?: string[]
  draft_spec?: unknown
}

export interface OntoChatSession {
  id: string
  title: string
  stage: OntoChatStage
  round: number
  messages?: OntoChatMessage[]
  context?: OntoChatContext
  ontology_id?: string
  created_at: string
  updated_at: string
}

/** turn 响应：reply 为 assistant 回复；draft 仅生成轮产出 */
export interface OntoChatTurnResult {
  reply: string
  stage: OntoChatStage
  round: number
  draft?: Spec
  warning?: string
  session: OntoChatSession
}

/** 注入指引（GET /api/ontologies/{id}/guide） */
export interface GuideResponse {
  ontology_id: string
  guide: string
}

/** 运行方案（运行平面 :8090；status 状态机 created→starting→running⇄stopped→error） */
export interface RuntimeProfile {
  id: string
  name: string
  engine?: string
  ontology_ids: string[]
  config?: Record<string, unknown>
  port?: number
  status: 'created' | 'starting' | 'running' | 'stopped' | 'error'
  pid?: number
  last_error?: string
  created_at?: string
  updated_at?: string
}

// ---- P1 尾适配（REQ-92/93/94 + 学习示例） ----

/**
 * 版本原始源文件格式（VersionMeta.original_format；REQ-93 源码视图按格式选渲染模式）。
 * 与构建平面 SaveVersion 落库口径一致：turtle / owl_rdfxml / spec_json / csv / graphml。
 */
export type OriginalFormat = 'turtle' | 'owl_rdfxml' | 'spec_json' | 'csv' | 'graphml'

/** 版本历史条目（GET /api/ontologies/{id}/versions，REQ-93） */
export interface VersionMeta {
  version: number
  created_at: string
  has_original: boolean
  original_format?: string
  original_size?: number
}

export interface VersionsResponse {
  ontology_id: string
  versions: VersionMeta[] | null
}

/** 翻译透视条目（GET /api/runtime-profiles/{id}/trace，REQ-94；失败查询也留痕） */
export interface TraceEntry {
  id?: number
  ts: string
  tool: string
  profile_id: string
  ontology_id: string
  sparql: string
  took_ms: number
  result_count: number
  ok: boolean
  error?: string
}

export interface TraceResponse {
  profile_id: string
  traces: TraceEntry[] | null
}

/** 内置学习示例（GET /api/ontologies/seed-learning） */
export interface LearningExample {
  key: string
  name: string
  description: string
}

// ---- KG 自存 + 消费/审计（D-O15/REQ-110：去-semantica 化，主平台自研 /api/kg、/api/audit）----

/** KG 实体（GET /api/kg/{kbID}；name 库内业务键，同 KB 跨 doc 同名归一） */
export interface KGEntity {
  id: string
  kb_id: string
  doc_id?: string
  name: string
  type?: string
  description?: string
  created_at?: string
}

/** KG 关系（source/target 引用实体 name；type 为大写短语，如 IS_A/具有/引发） */
export interface KGRelationship {
  id: string
  kb_id: string
  doc_id?: string
  source: string
  target: string
  type?: string
  created_at?: string
}

/** KG claim：实体的一条可溯源陈述（原文句），chunk_id 指向出处片段 */
export interface KGClaim {
  id: string
  kb_id: string
  doc_id?: string
  chunk_id?: string
  subject: string
  text: string
  created_at?: string
}

/** KG 子图读取结果（GET /api/kg/{kbID}；消费页图谱/审计页数据源） */
export interface KGReadResult {
  kb_id: string
  entities: KGEntity[]
  relationships: KGRelationship[]
  claims: KGClaim[]
  counts: { entities: number; relationships: number; claims: number }
}

/** 审计决策留痕（GET/POST /api/audit/decisions；derived_from 指向前置决策构成溯源链） */
export interface OntoDecision {
  id: string
  subject_kind: 'kg' | 'ontology' | 'kb' | 'manual'
  subject_id: string
  title: string
  rationale?: string
  derived_from?: string
  meta_json?: string
  created_at: string
}

/** 审计决策录入入参（POST /api/audit/decisions；id/subject_kind 缺省由后端补） */
export interface OntoDecisionInput {
  subject_kind?: OntoDecision['subject_kind']
  subject_id?: string
  title: string
  rationale?: string
  derived_from?: string
  meta_json?: string
}

// ---- P2 本体增量（REQ-95 版本 diff / REQ-96 CSV 灌装 / REQ-83 fork） ----

/** diff 字段级变化：from → to（REQ-95） */
export interface DiffFieldChange {
  from: unknown
  to: unknown
}

/** diff 变更条目：元素名 + 字段级变化集合 */
export interface DiffChanged {
  name: string
  fields: Record<string, DiffFieldChange>
}

/** diff 元素（三集合之一：概念 / 关系 / 实例，按 id=name 对齐） */
export type DiffItem = SpecConcept | SpecRelation | SpecInstance

/** diff 单集合（concepts / relations / instances 同构） */
export interface DiffCollection {
  added: DiffItem[]
  removed: DiffItem[]
  changed: DiffChanged[]
}

/** 引用影响统计：变更元素被 relations/instances 引用的次数 */
export interface DiffImpact {
  name: string
  referenced_by: number
}

/** 版本 diff（GET /api/ontologies/{id}/diff?from&to；400 版本无快照 / 404） */
export interface DiffResult {
  from_version: number
  to_version: number
  concepts: DiffCollection
  relations: DiffCollection
  instances: DiffCollection
  impact: DiffImpact[]
}

/** CSV 灌装统计（preview / apply 同口径） */
export interface CsvIngestStats {
  rows_read: number
  instances_generated: number
  skipped_empty_key: number
}

/** CSV 灌装预览（POST /api/ontologies/{id}/ingest-csv，mode=preview） */
export interface CsvIngestPreview {
  stats: CsvIngestStats
  warnings: string[] | null
  draft: SpecInstance[] | null
}

/** CSV 灌装确认入库（mode=apply；400 校验失败带 validation_errors） */
export interface CsvIngestApplyResult {
  saved: boolean
  version: number
  stats: CsvIngestStats
}

/** CSV 灌装映射配置（REQ-96 P2b；GET/PUT /api/ontologies/{id}/ingest-mapping） */
export interface IngestMapping {
  concept: string
  key_column: string
  relation_columns?: string[]
  attribute_columns?: string[]
  skip_rows?: number
  /** 列名 → 转换类型（int/number/date/bool；缺省 string 原样） */
  type_rules?: Record<string, string>
  /** 关系列多值分隔符（空 = 整格单值） */
  multi_value_sep?: string
}

/** fork 入参（POST /api/ontologies/{id}/fork；REQ-83） */
export interface ForkOntologyInput {
  name?: string
  description?: string
}

// ---------------------------------------------------------------------------
// 工具链配置（REQ-75/76，GET/POST /api/pipelines 等；04 §4.6）
// ---------------------------------------------------------------------------

/** 七阶段候选工具（tools.json 数据驱动，REQ-77 开放性） */
export interface PipelineStageTool {
  id: string
  stage: string
  name: string
  license?: string
  mode: 'builtin' | 'guided' | 'managed'
  eats?: string[]
  gives?: string[]
  guide?: { install?: string; handoff?: string; entry?: string }
  learning?: string
}

/** 单阶段选择（pipeline_profile.stages 的值） */
export interface PipelineStageSelection {
  tool: string
  mode: string
  params?: Record<string, unknown>
}

/** 工具链配置（pipeline_profile 行） */
export interface PipelineProfile {
  id: string
  name: string
  ontology_id?: string
  runtime_profile_id?: string
  stages: Record<string, PipelineStageSelection>
  checklist: Record<string, unknown>
  created_at?: string
  updated_at?: string
}

/** 引导清单条目（guided 阶段聚合；key = tool:<stage>:<tool_id>） */
export interface PipelineChecklistItem {
  key: string
  kind: 'tool' | 'task'
  stage: string
  title: string
  detail?: string
  entry_url?: string
}

/** catalog 响应（七阶段固定顺序 + 分组清单） */
export interface PipelineCatalogResponse {
  stages: string[]
  catalog: Record<string, PipelineStageTool[]>
}

/** 配置详情响应（profile + checklist 视图） */
export interface PipelineDetail {
  profile: PipelineProfile
  checklist: PipelineChecklistItem[]
}
