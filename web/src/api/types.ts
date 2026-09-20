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
