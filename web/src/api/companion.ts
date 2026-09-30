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
  /** REQ-216：图=绑定本体伴生子图（ont-{ontologyId}；未绑定为空串） */
  graph: string
  /** REQ-216：绑定的伴生本体 id（空=未开启） */
  ontology_id?: string
  pending_count: number
  cursor_count: number
  /** REQ-216：宿主方案运行态（伴生引擎=运行平面方案引擎；读侧兜底拉起后为 true） */
  engine_running: boolean
  engine_endpoint?: string
  /** REQ-216：宿主方案可观测（id+基址；运维入口=本体运行页方案管理） */
  plan?: { id: string; endpoint: string }
  /** REQ-216：宿主方案确保失败原因（诚实呈现；如运行平面不可达） */
  plan_error?: string
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
  /** REQ-216：绑定本体 id（未绑定为空） */
  ontology_id?: string
  engine_running: boolean
  engine_endpoint?: string
  /** REQ-216：宿主方案不可达原因（诚实降级空图时透出） */
  plan_error?: string
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
  // REQ-216⑥：本体维度候选（绑定该本体的全部 agent 跨 agent 铺平——资产详情页「伴生候选」页签）
  listCandidatesByOntology: (ontologyId: string, status = '') =>
    req<CompanionCandidate[]>(`/api/companion/ontologies/${encodeURIComponent(ontologyId)}/candidates${status ? `?status=${encodeURIComponent(status)}` : ''}`),
  // REQ-216：绑定该本体的 agent 清单（本体伴生子图共享者）
  listOntologyAgents: (ontologyId: string) =>
    req<import('./types').Agent[]>(`/api/companion/ontologies/${encodeURIComponent(ontologyId)}/agents`),
  confirmCandidate: (id: string) =>
    req<{ candidate: CompanionCandidate; graph: string }>(`/api/companion/candidates/${id}/confirm`, { method: 'POST', body: '{}' }),
  rejectCandidate: (id: string) => req<CompanionCandidate>(`/api/companion/candidates/${id}/reject`, { method: 'POST', body: '{}' }),
  status: (agentId: string) => req<CompanionStatus>(`/api/companion/status?agent_id=${encodeURIComponent(agentId)}`),
  // REQ-216：agent 级解绑（清该 agent 候选游标+断开绑定；本体伴生子图数据保留）
  resetAgent: (agentId: string) =>
    req<{ reset: boolean }>(`/api/companion/agents/${agentId}/reset`, { method: 'POST', body: '{}' }),
  // REQ-216②：绑定伴生本体（选择既有 / 一键创建空本体；绑定即确保宿主方案 running）
  bindAgent: (agentId: string, payload: { ontology_id?: string; create?: { name?: string } }) =>
    req<{ agent: import('./types').Agent; ontology_id: string; graph: string; plan_error?: string }>(
      `/api/companion/agents/${agentId}/bind`,
      { method: 'POST', body: JSON.stringify(payload) },
    ),
  // REQ-216：本体级伴生图清空（DROP 子图+清全部绑定 agent 候选游标；本体资产不受影响）
  resetOntology: (ontologyId: string) =>
    req<{ reset: boolean }>(`/api/companion/ontologies/${encodeURIComponent(ontologyId)}/reset`, { method: 'POST', body: '{}' }),
  // REQ-216⑦：伴生绑定本体 id 清单（资产列表「对话生长」徽标数据源）
  boundOntologies: () => req<{ ontology_ids: string[] }>(`/api/companion/bound-ontologies`),
  graph: (agentId: string) => req<CompanionGraph>(`/api/companion/graph?agent_id=${encodeURIComponent(agentId)}`),
}
