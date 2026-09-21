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
  created_at: string
  updated_at: string
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
