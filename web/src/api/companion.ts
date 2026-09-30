import { ApiError } from './client'

// ---------------------------------------------------------------------------
// REQ-170/M28 伴生本体前端 API（独立模块——client.ts 含并行 WIP，按文件隔离避让；
// req 语义与 client.ts 对齐：非 JSON 归一 ApiError、error 字段透出）。
// 后端五端点见 backend/internal/api/handlers_companion.go。
// ---------------------------------------------------------------------------

export interface CompanionCandidate {
  id: string
  conversation_id: string
  agent_id: string
  kind: 'concept' | 'relation' | 'event'
  name: string
  rel_name?: string
  rel_target?: string
  definition?: string
  confidence: number
  source_message_id?: string
  source_excerpt?: string
  status: 'pending' | 'confirmed' | 'rejected'
  created_at: string
  decided_at?: string
  /** REQ-194①：抽取时实体对齐标记（aligned=沿用已有实体 / new=新造；空=存量未标） */
  aligned?: '' | 'aligned' | 'new'
  /** REQ-194⑤：审计注记（语义矛盾「疑似矛盾待人工」等） */
  note?: string
}

/** REQ-194⑥：按实体归组（group_by=entity；代表候选=组内置信最高） */
export interface CandidateGroup {
  key: string
  entity: string
  count: number
  pending_count: number
  representative: CompanionCandidate
  members: CompanionCandidate[]
}

export interface CompanionStatus {
  /** REQ-211：状态按智能体聚合（图/待确认/标签/在抽会话数均为 agent 维度） */
  agent_id: string
  graph: string
  pending_count: number
  cursor_count: number
  engine_running: boolean
  engine_endpoint: string
  /** REQ-195：引擎加载详情（实际二进制/数据目录/端点；后端旧版无此字段=undefined） */
  engine_detail?: { binary: string; data_dir: string; endpoint: string }
  labels?: string[]
}

/** REQ-154 成长可视化图数据（GET /api/companion/graph） */
export interface CompanionGraphNode {
  label: string
  kind: 'Concept' | 'Event'
  definition?: string
  confidence?: number
  created_at?: string
}
export interface CompanionGraphEdge {
  source: string
  target: string
  rel: string
  created_at?: string
}
export interface CompanionGraph {
  agent_id: string
  graph: string
  engine_running: boolean
  nodes: CompanionGraphNode[]
  edges: CompanionGraphEdge[]
}

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, { headers: { 'Content-Type': 'application/json' }, ...init })
  const text = await res.text()
  let data: any = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    throw new ApiError(`HTTP ${res.status}：响应非 JSON — ${text.slice(0, 140) || '(空)'}`, res.status)
  }
  if (!res.ok) {
    throw new ApiError((data && data.error) || `HTTP ${res.status}`, res.status, data?.validation_errors)
  }
  return data as T
}

export const companionApi = {
  // REQ-193/M33：增 agentId 维度（跨会话铺平）；conversationId 与 agentId 可任选/同传
  listCandidates: (conversationId = '', status = '', agentId = '') => {
    const q = new URLSearchParams()
    if (conversationId) q.set('conversation_id', conversationId)
    if (status) q.set('status', status)
    if (agentId) q.set('agent_id', agentId)
    const s = q.toString()
    return req<CompanionCandidate[]>(`/api/companion/candidates${s ? '?' + s : ''}`)
  },
  // REQ-194⑥：按实体归组形态（group_by=entity；桶过滤照常在 status 参数）
  listCandidatesGrouped: (conversationId = '', status = '', agentId = '') => {
    const q = new URLSearchParams()
    if (conversationId) q.set('conversation_id', conversationId)
    if (status) q.set('status', status)
    if (agentId) q.set('agent_id', agentId)
    q.set('group_by', 'entity')
    return req<{ groups: CandidateGroup[] }>(`/api/companion/candidates?${q.toString()}`)
  },
  confirmCandidate: (id: string) =>
    req<{ candidate: CompanionCandidate; graph: string }>(`/api/companion/candidates/${id}/confirm`, { method: 'POST', body: '{}' }),
  rejectCandidate: (id: string) => req<CompanionCandidate>(`/api/companion/candidates/${id}/reject`, { method: 'POST', body: '{}' }),
  status: (agentId: string) => req<CompanionStatus>(`/api/companion/status?agent_id=${encodeURIComponent(agentId)}`),
  resetAgent: (agentId: string) =>
    req<{ reset: boolean }>(`/api/companion/agents/${agentId}/reset`, { method: 'POST', body: '{}' }),
  graph: (agentId: string) => req<CompanionGraph>(`/api/companion/graph?agent_id=${encodeURIComponent(agentId)}`),
}
