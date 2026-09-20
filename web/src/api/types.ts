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
  store_backend: 'qdrant' | 'sqlite'
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
  name: string
  size?: number
  chunk_count?: number
  status: 'pending' | 'indexing' | 'ready' | 'error'
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

// ---- M8 本体对接（02 文档 §6.10/§10：反代只读展示） ----

export interface RuntimeProfile {
  id: string
  name: string
  ontology_id?: string
  status: 'running' | 'stopped' | 'draft' | 'error'
  engine?: string
}

export interface OntologySummary {
  id: string
  name: string
  status: 'running' | 'draft' | 'importing'
  progress: number // 已完成阶段数 n（共 7）
  updated_at?: string
}

export interface OntologyStage {
  key: string // s1..s7
  title: string
  mode?: 'builtin' | 'guided' | 'managed'
  status?: 'done' | 'current' | 'pending'
  detail?: Record<string, unknown>
}

export interface OntologyDetail extends OntologySummary {
  stages: OntologyStage[]
}
