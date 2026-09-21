import type {
  Agent,
  AiDraftResult,
  ArtifactMeta,
  Conversation,
  GuideResponse,
  ImportReport,
  KBHit,
  KBDoc,
  KnowledgeBase,
  LearningExample,
  Message,
  ModelConnection,
  Ontology,
  Project,
  ProviderModelList,
  RunEventDTO,
  RuntimeProfile,
  Skill,
  Spec,
  ToolInfo,
  TraceResponse,
  UsageGroupBy,
  UsageStats,
  ValidationError,
  VersionsResponse,
} from './types'

/** 携带 HTTP 状态与校验错误的接口错误（供 UI 区分 404 / 400 validation_errors / 502 不可达） */
export class ApiError extends Error {
  status: number
  validationErrors?: ValidationError[]
  constructor(message: string, status: number, validationErrors?: ValidationError[]) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.validationErrors = validationErrors
  }
}

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) {
    const msg = (data && data.error) || `HTTP ${res.status}`
    throw new ApiError(msg, res.status, data?.validation_errors)
  }
  return data as T
}

/** multipart 上传：不设置 Content-Type（交由浏览器补 boundary） */
async function reqMultipart<T>(url: string, form: FormData): Promise<T> {
  const res = await fetch(url, { method: 'POST', body: form })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) {
    const msg = (data && data.error) || `HTTP ${res.status}`
    throw new ApiError(msg, res.status, data?.validation_errors)
  }
  return data as T
}

/** 文本响应请求（版本源码视图等非 JSON 端点；错误仍按 JSON {error} 解析） */
async function reqText(url: string): Promise<string> {
  const res = await fetch(url)
  const text = await res.text()
  if (!res.ok) {
    let msg = `HTTP ${res.status}`
    try {
      const j = JSON.parse(text)
      if (j && j.error) msg = j.error
    } catch { /* 非 JSON 错误体，保留状态码消息 */ }
    throw new ApiError(msg, res.status)
  }
  return text
}

/** SPARQL 工作台请求：Accept JSON 结果；失败解析 {error} 或透传引擎原文片段 */
async function reqSparql(url: string, query: string): Promise<{ raw: string; json: any }> {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/sparql-query', Accept: 'application/sparql-results+json' },
    body: query,
  })
  const text = await res.text()
  let json: any = null
  try {
    json = JSON.parse(text)
  } catch { /* 引擎可能返回非 JSON（如 HTML 错误页） */ }
  if (!res.ok) {
    const msg = (json && json.error) || `引擎返回 HTTP ${res.status}：${(text || '').slice(0, 200)}`
    throw new ApiError(msg, res.status)
  }
  return { raw: text, json }
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
  /**
   * 自动获取某提供商（锚点连接）的可用模型列表（ASSUMED 契约，接口可能未就绪 → 抛错由 UI 降级）。
   * POST /api/model-connections/{anchorId}/list-models → { models: string[] }
   */
  listProviderModels: (anchorId: string) => req<ProviderModelList>(`/api/model-connections/${anchorId}/list-models`, { method: 'POST' }),

  /**
   * 使用统计（ASSUMED 契约，接口可能未就绪 → 抛错由 UI 降级）。
   * GET /api/stats/usage?group_by=model|agent|project[&from=YYYY-MM-DD&to=YYYY-MM-DD]
   * from/to 可选且含首尾；空值不拼入查询串。
   */
  usageStats: (groupBy: UsageGroupBy, range?: { from?: string; to?: string }) => {
    const params = new URLSearchParams()
    params.set('group_by', groupBy)
    if (range?.from) params.set('from', range.from)
    if (range?.to) params.set('to', range.to)
    return req<UsageStats>(`/api/stats/usage?${params.toString()}`)
  },

  // ---- M6 知识库（§8：/api/kb 系列） ----
  listKBs: () => req<KnowledgeBase[]>('/api/kb'),
  createKB: (k: Partial<KnowledgeBase>) => req<KnowledgeBase>('/api/kb', { method: 'POST', body: JSON.stringify(k) }),
  updateKB: (id: string, k: Partial<KnowledgeBase>) => req<KnowledgeBase>(`/api/kb/${id}`, { method: 'PUT', body: JSON.stringify(k) }),
  deleteKB: (id: string) => req<{ deleted: string }>(`/api/kb/${id}`, { method: 'DELETE' }),
  listKBDocs: (kbId: string) => req<KBDoc[]>(`/api/kb/${kbId}/docs`),
  uploadKBDoc: (kbId: string, doc: { name: string; content: string }) =>
    req<KBDoc>(`/api/kb/${kbId}/docs`, { method: 'POST', body: JSON.stringify({ title: doc.name, content: doc.content }) }),
  deleteKBDoc: (kbId: string, docId: string) => req<{ deleted: string }>(`/api/kb/${kbId}/docs/${docId}`, { method: 'DELETE' }),
  reindexKBDoc: (kbId: string, docId: string) => req<KBDoc>(`/api/kb/${kbId}/docs/${docId}/reindex`, { method: 'POST' }),
  searchPreview: (kbId: string, q: string, topK?: number, minScore?: number) =>
    req<{ hits: KBHit[] }>(`/api/kb/${kbId}/search-preview`, { method: 'POST', body: JSON.stringify({ query: q, top_k: topK, min_score: minScore }) }),

  // ---- M7 技能（§8：/api/skills 系列 + 注入预览） ----
  listSkills: () => req<Skill[]>('/api/skills'),
  createSkill: (s: Partial<Skill>) => req<Skill>('/api/skills', { method: 'POST', body: JSON.stringify(s) }),
  updateSkill: (id: string, s: Partial<Skill>) => req<Skill>(`/api/skills/${id}`, { method: 'PUT', body: JSON.stringify(s) }),
  deleteSkill: (id: string) => req<{ deleted: string }>(`/api/skills/${id}`, { method: 'DELETE' }),
  skillPreview: (id: string) => req<{ instruction_block: string; enabled?: boolean }>(`/api/skills/${id}/preview`),

  // ---- M5 工具注册表（§6.8：前端勾选落 agent.tools） ----
  listTools: () => req<ToolInfo[]>('/api/tools'),

  // ---- M8 本体对接：构建平面 :8091 /api/ontologies*（同源反代，全路径透传） ----
  listOntologies: () => req<Ontology[]>('/api/ontologies'),
  getOntology: (id: string) => req<Ontology>(`/api/ontologies/${id}`),
  createOntology: (o: { name: string; description?: string }) =>
    req<Ontology>('/api/ontologies', { method: 'POST', body: JSON.stringify(o) }),
  updateOntologyMeta: (id: string, m: { name: string; description?: string }) =>
    req<Ontology>(`/api/ontologies/${id}`, { method: 'PUT', body: JSON.stringify(m) }),
  deleteOntology: (id: string) => req<{ deleted?: string }>(`/api/ontologies/${id}`, { method: 'DELETE' }),
  /** 原始 Spec JSON；从未保存过 → 404（UI 视为空 Spec） */
  getSpec: (id: string) => req<Spec>(`/api/ontologies/${id}/spec`),
  /** 全量保存 Spec（校验门控、递增 version）；400 时错误带 validation_errors */
  saveSpec: (id: string, spec: Spec) =>
    req<{ saved: boolean; version: number }>(`/api/ontologies/${id}/spec`, { method: 'PUT', body: JSON.stringify(spec) }),
  /** 校验：始终 200，返回 ok + 错误列表 */
  validateOntology: (id: string) =>
    req<{ ok: boolean; validation_errors: ValidationError[] }>(`/api/ontologies/${id}/validate`, { method: 'POST' }),
  listArtifacts: (id: string) => req<ArtifactMeta[]>(`/api/ontologies/${id}/artifacts`),
  /** 导入：multipart（file + 可选 name），自动嗅探 ttl/owl/graphml/csv/spec_json */
  importOntologyFile: (file: File, name?: string) => {
    const fd = new FormData()
    fd.append('file', file)
    if (name) fd.append('name', name)
    return reqMultipart<{ ontology: Ontology; report: ImportReport }>('/api/ontologies/import', fd)
  },
  /** 导入：JSON 模式 {filename, content, name?} */
  importOntologyContent: (filename: string, content: string, name?: string) =>
    req<{ ontology: Ontology; report: ImportReport }>('/api/ontologies/import', {
      method: 'POST',
      body: JSON.stringify({ filename, content, name }),
    }),
  /** 内置示例：201 新建（id=onto_k8s_ops）或 200 {id, seeded:false, note} */
  seedSampleOntology: () =>
    req<Ontology & { seeded?: boolean; note?: string }>('/api/ontologies/seed-sample', { method: 'POST' }),
  /** AI 草案：503 表示 LLM 未配置；capabilityQuestions 为能力问题（CQ，REQ §4.8.3） */
  aiDraftOntology: (description: string, extraHint?: string, capabilityQuestions?: string[]) =>
    req<AiDraftResult>('/api/ontologies/ai-draft', {
      method: 'POST',
      body: JSON.stringify({ description, extraHint, capability_questions: capabilityQuestions }),
    }),
  /** 导出下载地址（text/turtle attachment） */
  ontologyExportUrl: (id: string, format: string) => `/api/ontologies/${id}/export?format=${encodeURIComponent(format)}`,
  /** 注入指引（经构建平面路由，稳定可用） */
  getOntologyGuide: (id: string) => req<GuideResponse>(`/api/ontologies/${id}/guide`),

  // ---- M8 运行平面 :8090 /api/runtime-profiles* ----
  listRuntimeProfiles: () => req<RuntimeProfile[]>('/api/runtime-profiles'),
  createRuntimeProfile: (p: { name: string; engine?: string; ontology_ids: string[]; config?: Record<string, unknown>; port?: number }) =>
    req<RuntimeProfile>('/api/runtime-profiles', { method: 'POST', body: JSON.stringify(p) }),
  updateRuntimeProfile: (id: string, p: { name: string; ontology_ids?: string[]; config?: Record<string, unknown>; port?: number }) =>
    req<RuntimeProfile>(`/api/runtime-profiles/${id}`, { method: 'PUT', body: JSON.stringify(p) }),
  deleteRuntimeProfile: (id: string) => req<{ deleted?: string }>(`/api/runtime-profiles/${id}`, { method: 'DELETE' }),
  startRuntimeProfile: (id: string) => req<RuntimeProfile>(`/api/runtime-profiles/${id}/start`, { method: 'POST' }),
  stopRuntimeProfile: (id: string) => req<RuntimeProfile>(`/api/runtime-profiles/${id}/stop`, { method: 'POST' }),
  reloadRuntimeProfile: (id: string) => req<RuntimeProfile>(`/api/runtime-profiles/${id}/reload`, { method: 'POST' }),
  runtimeProfileLogs: (id: string, tail = 200) => req<{ lines: string[] }>(`/api/runtime-profiles/${id}/logs?tail=${tail}`),

  // ---- P1 尾适配：版本历史（REQ-93）/ 学习示例 / 翻译透视（REQ-94）/ SPARQL 工作台（REQ-92） ----
  /** 版本历史列表（每次保存/导入/灌装留快照） */
  listVersions: (id: string) => req<VersionsResponse>(`/api/ontologies/${id}/versions`),
  /** 某版本导入时的原始源文件（Turtle/RDF-XML/JSON/CSV/GraphML 原文；无原始源 → 404） */
  getVersionOriginal: (id: string, version: number) => reqText(`/api/ontologies/${id}/versions/${version}/original`),
  /** 内置学习示例清单（软件缺陷/组织人员/设备故障） */
  listLearningExamples: () => req<LearningExample[]>('/api/ontologies/seed-learning'),
  /** 灌装学习示例（幂等：已存在 → 200 {seeded:false, note}） */
  seedLearningExample: (key: string) =>
    req<Ontology & { seeded?: boolean; note?: string }>('/api/ontologies/seed-learning', {
      method: 'POST',
      body: JSON.stringify({ key }),
    }),
  /** 翻译透视（最近 N 条，含失败留痕；limit≤200） */
  listTraces: (profileId: string, limit = 50) => req<TraceResponse>(`/api/runtime-profiles/${profileId}/trace?limit=${limit}`),
  /** SPARQL 工作台：POST application/sparql-query；非 running → 409；状态码/错误透传引擎 */
  runSparql: (profileId: string, query: string) => reqSparql(`/api/runtime-profiles/${profileId}/sparql`, query),
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
