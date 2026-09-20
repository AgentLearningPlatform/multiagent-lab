import type {
  Agent,
  Conversation,
  KBHit,
  KBDoc,
  KnowledgeBase,
  Message,
  ModelConnection,
  OntologyDetail,
  OntologySummary,
  Project,
  RunEventDTO,
  RuntimeProfile,
  Skill,
  ToolInfo,
} from './types'

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
  listEvents: (id: string) => req<RunEventDTO[]>(`/api/conversations/${id}/events`),
  stopConversation: (id: string) => req<{ stopped: boolean }>(`/api/conversations/${id}/stop`, { method: 'POST' }),

  // model connections
  listConnections: () => req<ModelConnection[]>('/api/model-connections'),
  createConnection: (c: any) => req<ModelConnection>('/api/model-connections', { method: 'POST', body: JSON.stringify(c) }),
  updateConnection: (id: string, c: any) => req<ModelConnection>(`/api/model-connections/${id}`, { method: 'PUT', body: JSON.stringify(c) }),
  deleteConnection: (id: string) => req<{ deleted: string }>(`/api/model-connections/${id}`, { method: 'DELETE' }),
  setDefaultConnection: (id: string) => req<ModelConnection>(`/api/model-connections/${id}/default`, { method: 'PUT' }),
  testConnection: (input: any) => req<{ ok: boolean; error?: string; elapsed_ms: number }>('/api/model-connections/test', { method: 'POST', body: JSON.stringify(input) }),

  // ---- M6 知识库（§8：/api/kb 系列） ----
  listKBs: () => req<KnowledgeBase[]>('/api/kb'),
  createKB: (k: Partial<KnowledgeBase>) => req<KnowledgeBase>('/api/kb', { method: 'POST', body: JSON.stringify(k) }),
  updateKB: (id: string, k: Partial<KnowledgeBase>) => req<KnowledgeBase>(`/api/kb/${id}`, { method: 'PUT', body: JSON.stringify(k) }),
  deleteKB: (id: string) => req<{ deleted: string }>(`/api/kb/${id}`, { method: 'DELETE' }),
  listKBDocs: (kbId: string) => req<KBDoc[]>(`/api/kb/${kbId}/docs`),
  uploadKBDoc: (kbId: string, doc: { name: string; content: string }) =>
    req<KBDoc>(`/api/kb/${kbId}/docs`, { method: 'POST', body: JSON.stringify(doc) }),
  deleteKBDoc: (kbId: string, docId: string) => req<{ deleted: string }>(`/api/kb/${kbId}/docs/${docId}`, { method: 'DELETE' }),
  reindexKBDoc: (kbId: string, docId: string) => req<KBDoc>(`/api/kb/${kbId}/docs/${docId}/reindex`, { method: 'POST' }),
  searchPreview: (kbId: string, q: string, topK?: number) =>
    req<{ hits: KBHit[] }>(`/api/kb/${kbId}/search-preview`, { method: 'POST', body: JSON.stringify({ q, top_k: topK }) }),

  // ---- M7 技能（§8：/api/skills 系列 + 注入预览） ----
  listSkills: () => req<Skill[]>('/api/skills'),
  createSkill: (s: Partial<Skill>) => req<Skill>('/api/skills', { method: 'POST', body: JSON.stringify(s) }),
  updateSkill: (id: string, s: Partial<Skill>) => req<Skill>(`/api/skills/${id}`, { method: 'PUT', body: JSON.stringify(s) }),
  deleteSkill: (id: string) => req<{ deleted: string }>(`/api/skills/${id}`, { method: 'DELETE' }),
  skillPreview: (id: string) => req<{ instruction: string }>(`/api/skills/${id}/preview`),

  // ---- M5 工具注册表（§6.8：前端勾选落 agent.tools） ----
  listTools: () => req<ToolInfo[]>('/api/tools'),

  // ---- M8 本体对接（§6.10：双反代只读展示） ----
  listRuntimeProfiles: () => req<RuntimeProfile[]>('/api/runtime-profiles'),
  listOntologies: () => req<OntologySummary[]>('/api/ontologies'),
  getOntology: (id: string) => req<OntologyDetail>(`/api/ontologies/${id}`),
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
