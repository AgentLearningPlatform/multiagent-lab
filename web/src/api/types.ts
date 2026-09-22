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
  created_at: string
  updated_at: string
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

/** 目录检测结果（POST /api/projects/validate-dir） */
export interface DirValidation {
  exists: boolean
  is_dir: boolean
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
  /** @deprecated 后端已移除该字段，仅新建表单兼容保留 */
  store_backend?: 'qdrant' | 'sqlite'
  top_k: number
  min_score: number
  doc_count?: number
  chunk_count?: number
  created_at: string
  updated_at: string
}

export interface KBDoc {
  id: string
  kb_id: string
  title: string
  chunk_count?: number
  status: 'pending' | 'indexing' | 'success' | 'failed'
  error?: string
  created_at: string
}

/** 检索试运行命中（§6.9 retrieval 事件 hits 结构） */
export interface KBHit {
  doc: string
  seq: number
  score: number
  excerpt: string
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

// ---- Semantica 独立栏（docs/04 §4.9 D-O10；REQ-99~101；主平台反代 /api/semantica/* → worker :8093）----

/** worker 健康与图规模（GET /api/semantica/health；worker 未启动时反代 502） */
export interface SemanticaHealth {
  ok: boolean
  version: string
  graph_loaded: boolean
  entities: number
  relationships: number
  decisions: number
}

/** TTL 摄入结果（POST /api/semantica/ingest-ttl） */
export interface SemanticaIngestResult {
  ontology_id: string
  entities: number
  relationships: number
  warnings: string[] | null
}

/** GraphRAG 命中片段（POST /api/semantica/query） */
export interface SemanticaClaim {
  text: string
  source_node?: string | null
  score?: number | null
}

export interface SemanticaQueryResult {
  claims: SemanticaClaim[] | null
  query: string
}

/** 决策录入入参（POST /api/semantica/decision） */
export interface SemanticaDecisionInput {
  category: string
  scenario: string
  reasoning: string
  outcome: string
  confidence?: number
}

/** 决策记录（GET /api/semantica/decisions；worker 防御式归一化，字段可能缺省） */
export interface SemanticaDecision {
  id: string
  category?: string | null
  scenario?: string | null
  outcome?: string | null
  confidence?: number | null
  ts?: string | null
}

export interface SemanticaDecisionsResponse {
  decisions: SemanticaDecision[] | null
}

/** 图规模统计（GET /api/semantica/stats） */
export interface SemanticaStats {
  entities: number
  relationships: number
  decisions: number
}

// ---- Semantica 审计/溯源（REQ-101，§4.9.4）----

/** 因果/先例关系类型（POST /api/semantica/causal） */
export type SemanticaCausalType = 'CAUSED' | 'INFLUENCED' | 'PRECEDENT_FOR'

/** 决策链节点（GET /api/semantica/decision-chain/{id}；worker 防御式归一化） */
export interface SemanticaChainNode {
  id: string
  category?: string | null
  scenario?: string | null
  outcome?: string | null
  confidence?: number | string | null
  relation?: string | null
  ts?: string | null
}

export interface SemanticaDecisionChain {
  decision_id: string
  chain: SemanticaChainNode[] | null
  warnings?: string[] | null
}

/** PROV-O 溯源条目（GET /api/semantica/lineage/{entity_id}；source/metadata/type 形状不定） */
export interface SemanticaProvNode {
  id: string
  source?: unknown
  metadata?: unknown
  type?: unknown
}

export interface SemanticaLineage {
  entity_id: string
  lineage: SemanticaProvNode[] | null
  warnings?: string[] | null
}

export interface SemanticaCausalResult {
  ok: boolean
  from_id: string
  to_id: string
  type: string
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

/** fork 入参（POST /api/ontologies/{id}/fork；REQ-83） */
export interface ForkOntologyInput {
  name?: string
  description?: string
}
