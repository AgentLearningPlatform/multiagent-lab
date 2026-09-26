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
}

export interface CompanionStatus {
  conversation_id: string
  cursor: { conversation_id: string; last_message_id: string; updated_at: string }
  pending_count: number
  graph: string
  engine_running: boolean
  engine_endpoint: string
  labels?: string[]
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
  listCandidates: (conversationId = '', status = '') => {
    const q = new URLSearchParams()
    if (conversationId) q.set('conversation_id', conversationId)
    if (status) q.set('status', status)
    const s = q.toString()
    return req<CompanionCandidate[]>(`/api/companion/candidates${s ? '?' + s : ''}`)
  },
  confirmCandidate: (id: string) =>
    req<{ candidate: CompanionCandidate; graph: string }>(`/api/companion/candidates/${id}/confirm`, { method: 'POST', body: '{}' }),
  rejectCandidate: (id: string) => req<CompanionCandidate>(`/api/companion/candidates/${id}/reject`, { method: 'POST', body: '{}' }),
  status: (conversationId: string) => req<CompanionStatus>(`/api/companion/status?conversation_id=${encodeURIComponent(conversationId)}`),
  resetConversation: (conversationId: string) =>
    req<{ reset: boolean }>(`/api/companion/conversations/${conversationId}/reset`, { method: 'POST', body: '{}' }),
}
