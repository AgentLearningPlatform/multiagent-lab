import type { Agent, Conversation, Message, ModelConnection, Project } from './types'

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) {
    const msg = (data && data.error) || `HTTP ${res.status}`
    throw new Error(msg)
  }
  return data as T
}

export const api = {
  // agents
  listAgents: () => req<Agent[]>('/api/agents'),
  createAgent: (a: Partial<Agent>) => req<Agent>('/api/agents', { method: 'POST', body: JSON.stringify(a) }),
  updateAgent: (id: string, a: Partial<Agent>) => req<Agent>(`/api/agents/${id}`, { method: 'PUT', body: JSON.stringify(a) }),
  deleteAgent: (id: string) => req<{ deleted: string }>(`/api/agents/${id}`, { method: 'DELETE' }),

  // projects
  listProjects: () => req<Project[]>('/api/projects'),
  createProject: (p: Partial<Project>) => req<Project>('/api/projects', { method: 'POST', body: JSON.stringify(p) }),
  updateProject: (id: string, p: Partial<Project>) => req<Project>(`/api/projects/${id}`, { method: 'PUT', body: JSON.stringify(p) }),
  setProjectAgents: (id: string, members: { agent_id: string; role: 'coordinator' | 'member' }[]) =>
    req<Project>(`/api/projects/${id}/agents`, { method: 'PUT', body: JSON.stringify(members) }),
  deleteProject: (id: string) => req<{ deleted: string }>(`/api/projects/${id}`, { method: 'DELETE' }),

  // conversations
  listConversations: (q: { scope?: string; agent_id?: string; project_id?: string } = {}) => {
    const params = new URLSearchParams()
    if (q.scope) params.set('scope', q.scope)
    if (q.agent_id) params.set('agent_id', q.agent_id)
    if (q.project_id) params.set('project_id', q.project_id)
    const qs = params.toString()
    return req<Conversation[]>(`/api/conversations${qs ? '?' + qs : ''}`)
  },
  createConversation: (c: Partial<Conversation>) => req<Conversation>('/api/conversations', { method: 'POST', body: JSON.stringify(c) }),
  updateConversation: (id: string, c: Partial<Conversation>) => req<Conversation>(`/api/conversations/${id}`, { method: 'PUT', body: JSON.stringify(c) }),
  deleteConversation: (id: string) => req<{ deleted: string }>(`/api/conversations/${id}`, { method: 'DELETE' }),
  listMessages: (id: string) => req<Message[]>(`/api/conversations/${id}/messages`),

  // model connections
  listConnections: () => req<ModelConnection[]>('/api/model-connections'),
  createConnection: (c: any) => req<ModelConnection>('/api/model-connections', { method: 'POST', body: JSON.stringify(c) }),
  updateConnection: (id: string, c: any) => req<ModelConnection>(`/api/model-connections/${id}`, { method: 'PUT', body: JSON.stringify(c) }),
  deleteConnection: (id: string) => req<{ deleted: string }>(`/api/model-connections/${id}`, { method: 'DELETE' }),
  setDefaultConnection: (id: string) => req<ModelConnection>(`/api/model-connections/${id}/default`, { method: 'PUT' }),
  testConnection: (input: any) => req<{ ok: boolean; error?: string; elapsed_ms: number }>('/api/model-connections/test', { method: 'POST', body: JSON.stringify(input) }),
}

/**
 * 运行对话并逐事件回调（SSE over fetch，POST 请求）。
 * 返回一个可中断的 AbortController。
 */
export function runConversation(
  conversationId: string,
  input: string,
  onEvent: (ev: { event: string; data: any }) => void,
): { abort: () => void; done: Promise<void> } {
  const ctrl = new AbortController()
  const done = (async () => {
    const res = await fetch(`/api/conversations/${conversationId}/runs`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ input }),
      signal: ctrl.signal,
    })
    if (!res.ok || !res.body) {
      let msg = `HTTP ${res.status}`
      try {
        const j = await res.json()
        if (j.error) msg = j.error
      } catch { /* ignore */ }
      onEvent({ event: 'run.error', data: { data: { message: msg } } })
      return
    }
    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    for (;;) {
      const { done: streamDone, value } = await reader.read()
      if (streamDone) break
      buf += decoder.decode(value, { stream: true })
      let idx: number
      // SSE 事件以空行分隔
      while ((idx = buf.indexOf('\n\n')) >= 0) {
        const raw = buf.slice(0, idx)
        buf = buf.slice(idx + 2)
        let event = 'message'
        let data = ''
        for (const line of raw.split('\n')) {
          if (line.startsWith('event:')) event = line.slice(6).trim()
          else if (line.startsWith('data:')) data += line.slice(5).trim()
        }
        if (!data) continue
        try {
          onEvent({ event, data: JSON.parse(data) })
        } catch {
          onEvent({ event, data: { raw: data } })
        }
      }
    }
  })()
  return { abort: () => ctrl.abort(), done }
}
