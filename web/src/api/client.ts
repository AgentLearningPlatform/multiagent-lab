import type {
  Agent,
  AiDraftResult,
  ArtifactMeta,
  Conversation,
  CsvIngestApplyResult,
  IngestMapping,
  CsvIngestPreview,
  DiffResult,
  DirValidation,
  ForkOntologyInput,
  GuideResponse,
  ImportReport,
  ChunksToKGResult,
  KBDoc,
  KBSearchResult,
  KGToSpecResult,
  KnowledgeBase,
  LearningExample,
  OntoBuildResult,
  OntoBuildSelectableKB,
  PipelineCatalogResponse,
  PipelineDetail,
  PipelineProfile,
  PipelineStageSelection,
  Message,
  ModelConnection,
  Ontology,
  OntoChatSession,
  OntoChatTurnResult,
  Project,
  ProjectDirListing,
  ProviderModelList,
  RunEventDTO,
  RuntimeProfile,
  SemanticaCausalResult,
  SemanticaCausalType,
  SemanticaDecisionChain,
  SemanticaDecisionInput,
  SemanticaDecisionsResponse,
  SemanticaHealth,
  SemanticaIngestResult,
  SemanticaLineage,
  SemanticaQueryResult,
  SemanticaStats,
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
  /** REQ-101：检测本地目录（存在/目录/Git 状态） */
  validateProjectDir: (dir: string) =>
    req<DirValidation>('/api/projects/validate-dir', { method: 'POST', body: JSON.stringify({ dir }) }),
  /** REQ-102：列出绑定目录下的条目（未绑定 → 400）；path 为相对子路径 */
  listProjectDirFiles: (id: string, path?: string) =>
    req<ProjectDirListing>(`/api/projects/${id}/dir-files${path ? `?path=${encodeURIComponent(path)}` : ''}`),
  /** REQ-102：读取目录内文件文本内容（≤1MB；超限 → 400） */
  getProjectDirFile: (id: string, path: string) =>
    reqText(`/api/projects/${id}/dir-file?path=${encodeURIComponent(path)}`),

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
    req<KBSearchResult>(`/api/kb/${kbId}/search-preview`, { method: 'POST', body: JSON.stringify({ query: q, top_k: topK, min_score: minScore }) }),
  // M14 D-KB4：GraphRAG 子模块直查（worker 不可达返回 degraded:true，不抛错）
  graphragSearchKB: (kbId: string, q: string, maxResults?: number) =>
    req<KBSearchResult>(`/api/kb/${kbId}/graphrag-search`, { method: 'POST', body: JSON.stringify({ query: q, max_results: maxResults }) }),

  // ---- O13 由知识库构建本体（REQ-108；精确路由压过本体/semantica 反代前缀） ----
  selectableKBsForBuild: () => req<OntoBuildSelectableKB[]>('/api/kbs/selectable-for-ontology-build'),
  buildFromKB: (input: {
    kb_id: string
    strategy: 'chunk-llm' | 'kg-direct' | 'hybrid'
    cq_mode: 'auto' | 'custom' | 'skip'
    custom_cqs?: string[]
  }) => req<OntoBuildResult>('/api/ontologies/build-from-kb', { method: 'POST', body: JSON.stringify(input) }),
  chunksToKG: (kbId: string) =>
    req<ChunksToKGResult>('/api/semantica/chunks-to-kg', { method: 'POST', body: JSON.stringify({ kb_id: kbId }) }),
  kgToSpecJSON: (kbId: string) =>
    req<KGToSpecResult>('/api/ontologies/kg-to-spec-json', { method: 'POST', body: JSON.stringify({ kb_id: kbId }) }),

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
  createRuntimeProfile: (p: { name: string; engine?: string; ontology_ids: string[]; config?: string | Record<string, unknown>; port?: number }) =>
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
  /** 工具链候选清单（REQ-75/77，七阶段分组，tools.json 数据驱动） */
  listPipelineCatalog: () => req<PipelineCatalogResponse>('/api/pipelines/catalog'),
  /** 工具链配置列表 */
  listPipelines: () => req<PipelineProfile[]>('/api/pipelines'),
  /** 创建工具链配置（默认模板：每阶段预置 builtin 项） */
  createPipeline: (input: { name: string; ontology_id?: string }) =>
    req<PipelineProfile>('/api/pipelines', { method: 'POST', body: JSON.stringify(input) }),
  /** 配置详情（附 checklist 引导清单视图） */
  getPipeline: (id: string) => req<PipelineDetail>(`/api/pipelines/${id}`),
  /** 更新配置（stages 全量覆盖 / meta 局部） */
  updatePipeline: (id: string, input: Partial<{ name: string; ontology_id: string; runtime_profile_id: string; stages: Record<string, PipelineStageSelection> }>) =>
    req<PipelineProfile>(`/api/pipelines/${id}`, { method: 'PUT', body: JSON.stringify(input) }),
  /** 复制为新工具链（checklist 清零） */
  clonePipeline: (id: string, input: { name?: string } = {}) =>
    req<PipelineProfile>(`/api/pipelines/${id}/clone`, { method: 'POST', body: JSON.stringify(input) }),
  /** 删除配置 */
  deletePipeline: (id: string) => req<{ deleted: boolean }>(`/api/pipelines/${id}`, { method: 'DELETE' }),
  /** guided 打卡（tool:<stage>:<tool_id>；toggle 由 done 控制） */
  checkPipeline: (id: string, key: string, done: boolean) =>
    req<{ key: string; done: boolean; checklist: Record<string, unknown> }>(`/api/pipelines/${id}/check`, {
      method: 'POST',
      body: JSON.stringify({ key, done }),
    }),
  /** 翻译透视（最近 N 条，含失败留痕；limit≤200） */
  listTraces: (profileId: string, limit = 50) => req<TraceResponse>(`/api/runtime-profiles/${profileId}/trace?limit=${limit}`),
  /** SPARQL 工作台：POST application/sparql-query；非 running → 409；状态码/错误透传引擎 */
  runSparql: (profileId: string, query: string) => reqSparql(`/api/runtime-profiles/${profileId}/sparql`, query),
  /**
   * SPARQL 工作台端点 URL（REQ-92，Yasgui 自行发起请求，不经 req 封装）。
   * 运行平面反代支持 GET ?query= 与 POST application/sparql-query；非 running → 409。
   */
  sparqlEndpointUrl: (profileId: string) => `/api/runtime-profiles/${profileId}/sparql`,
  /** 某版本原始源文件下载地址（REQ-93；>1MB 时前端提示下载查看而非渲染） */
  versionOriginalUrl: (ontologyId: string, version: number) =>
    `/api/ontologies/${ontologyId}/versions/${version}/original`,

  // ---- Semantica 独立栏（§4.9 D-O10；REQ-99~101；反代 /api/semantica/* → worker :8093）----
  /** worker 健康/图规模；未启动 → 反代 502（UI 降级） */
  semanticaHealth: () => req<SemanticaHealth>('/api/semantica/health'),
  /** 本体 TTL → KG：worker ingest + GraphBuilder（返回实体/关系数与警告） */
  semanticaIngestTtl: (ontologyId: string, ttl: string) =>
    req<SemanticaIngestResult>('/api/semantica/ingest-ttl', {
      method: 'POST',
      body: JSON.stringify({ ontology_id: ontologyId, ttl }),
    }),
  /** GraphRAG 语义问答（向量 + 图混合检索） */
  semanticaQuery: (q: string, maxResults?: number) =>
    req<SemanticaQueryResult>('/api/semantica/query', {
      method: 'POST',
      body: JSON.stringify({ q, max_results: maxResults }),
    }),
  /** record_decision 落决策记录（PROV-O 审计链） */
  semanticaRecordDecision: (d: SemanticaDecisionInput) =>
    req<{ decision_id: string }>('/api/semantica/decision', { method: 'POST', body: JSON.stringify(d) }),
  /** 决策列表（worker 侧已按 limit 截断） */
  semanticaDecisions: (limit = 20) => req<SemanticaDecisionsResponse>(`/api/semantica/decisions?limit=${limit}`),
  /** 图规模统计 */
  semanticaStats: () => req<SemanticaStats>('/api/semantica/stats'),
  /** 导出本体 Turtle 原文（ingest 数据源；复用既有构建平面导出端点，text/turtle） */
  exportOntologyTurtle: (ontologyId: string) => reqText(`/api/ontologies/${ontologyId}/export?format=turtle`),
  /** 决策因果链（GET /api/semantica/decision-chain/{id}；PROV-O 溯源，REQ-101） */
  semanticaDecisionChain: (decisionId: string) =>
    req<SemanticaDecisionChain>(`/api/semantica/decision-chain/${encodeURIComponent(decisionId)}`),
  /** 实体 PROV-O 溯源（GET /api/semantica/lineage/{entity_id}；REQ-101） */
  semanticaLineage: (entityId: string) =>
    req<SemanticaLineage>(`/api/semantica/lineage/${encodeURIComponent(entityId)}`),
  /** PROV-O 导出（GET /api/semantica/prov-export?format=turtle）→ text/turtle 原文（错误为 JSON {error}） */
  semanticaProvExport: (format = 'turtle') => reqText(`/api/semantica/prov-export?format=${encodeURIComponent(format)}`),
  /** 写入因果/先例关系（POST /api/semantica/causal；type ∈ CAUSED|INFLUENCED|PRECEDENT_FOR） */
  semanticaAddCausal: (fromId: string, toId: string, type: SemanticaCausalType = 'CAUSED') =>
    req<SemanticaCausalResult>('/api/semantica/causal', {
      method: 'POST',
      body: JSON.stringify({ from_id: fromId, to_id: toId, type }),
    }),

  // ---- P2 本体增量（REQ-95 diff / REQ-96 CSV 灌装 / REQ-83 fork）----
  /** 版本 diff：GET /api/ontologies/{id}/diff?from&to；400 版本无快照 / 404 → ApiError（UI 内联 Alert） */
  diffOntologyVersions: (id: string, fromV: number, toV: number) =>
    req<DiffResult>(`/api/ontologies/${id}/diff?from=${fromV}&to=${toV}`),
  /** CSV 灌装预览：multipart（csv + concept/key_column/relation_columns/attribute_columns/skip_rows/mode=preview） */
  ingestCsvPreview: (id: string, form: FormData) => reqMultipart<CsvIngestPreview>(`/api/ontologies/${id}/ingest-csv`, form),
  /** CSV 灌装确认入库：同 multipart，mode=apply；400 校验失败带 validation_errors */
  ingestCsvApply: (id: string, form: FormData) => reqMultipart<CsvIngestApplyResult>(`/api/ontologies/${id}/ingest-csv`, form),
  /** 映射配置读取（REQ-96 P2b）：GET /api/ontologies/{id}/ingest-mapping；未保存过 → 404 */
  getIngestMapping: (id: string) => req<IngestMapping>(`/api/ontologies/${id}/ingest-mapping`),
  /** 映射配置保存（P2b）：PUT 同路径；结构即灌装配置（concept/key_column/列绑定/类型规则/分隔符/跳行） */
  putIngestMapping: (id: string, mapping: IngestMapping) =>
    req<{ saved: boolean }>(`/api/ontologies/${id}/ingest-mapping`, { method: 'PUT', body: JSON.stringify(mapping) }),
  /** fork 本体：POST /api/ontologies/{id}/fork → 201 新本体（forked_from=源 id，version 重置 1） */
  forkOntology: (id: string, input: ForkOntologyInput = {}) =>
    req<Ontology>(`/api/ontologies/${id}/fork`, { method: 'POST', body: JSON.stringify(input) }),

  // ---- OntoChat 多轮引导（REQ-103 模式 A；构建平面 /api/ontochat/*）----
  listOntoChatSessions: () => req<OntoChatSession[]>('/api/ontochat/sessions'),
  createOntoChatSession: (title?: string) =>
    req<OntoChatSession>('/api/ontochat/sessions', { method: 'POST', body: JSON.stringify({ title }) }),
  getOntoChatSession: (id: string) => req<OntoChatSession>(`/api/ontochat/sessions/${id}`),
  deleteOntoChatSession: (id: string) => req<{ deleted: string }>(`/api/ontochat/sessions/${id}`, { method: 'DELETE' }),
  /** 一轮交互：text 用户输入；feedback 非空 = refine 修正轮（意见回喂重新生成） */
  ontoChatTurn: (id: string, text: string, feedback?: string) =>
    req<OntoChatTurnResult>(`/api/ontochat/sessions/${id}/turn`, {
      method: 'POST',
      body: JSON.stringify({ text, feedback }),
    }),
  /** 草稿入库（预览确认门控，REQ-82）：201 {ontology, session} */
  ontoChatSave: (id: string, name: string) =>
    req<{ ontology: Ontology; session: OntoChatSession }>(`/api/ontochat/sessions/${id}/save`, {
      method: 'POST',
      body: JSON.stringify({ name }),
    }),
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
