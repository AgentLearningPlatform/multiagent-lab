import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Spin,
  Steps,
  Table,
  Tabs,
  Tag,
  Timeline,
  Tooltip,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  BranchesOutlined,
  ExportOutlined,
  ReloadOutlined,
  SaveOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import { api, ApiError } from '../../api/client'
import type {
  KGClaim,
  KGEntity,
  KGReadResult,
  KGRelationship,
  OntoBuildSelectableKB,
  OntoDecision,
} from '../../api/types'
import { useUI } from '../../store/ui'

// ---------------------------------------------------------------------------
// 消费与审计（第五栏，D-O15/REQ-110：去-semantica 化改造复用）
// 原 Semantica 独立栏骨架保留（页头 + 状态条 + 页签），数据源切换为自研后端：
//   KG 图谱 = GET /api/kg/{kbID}（SQLite 自存三表）
//   GraphRAG 试查 = POST /api/kb/{id}/graphrag-search（向量命中 → KG 一跳扩展）
//   决策审计 = /api/audit/decisions*（SQLite 决策表 + derived_from 溯源链）
//   PROV-O 导出 = GET /api/audit/prov-export（Go 原生 Turtle 模板，零 Python）
// ---------------------------------------------------------------------------

/** 跨栏跳转到本栏（与 BuildPage/RuntimePage 的 onto-sidebar-change 机制一致） */
export function gotoAuditPane() {
  localStorage.setItem('eino.onto.sidebar', 'audit')
  window.dispatchEvent(new CustomEvent('onto-sidebar-change'))
}

/** ① 功能：它是什么 / 解决什么 / 不做什么（去-semantica 化后口径） */
const FEATURES: { key: string; title: string; tag: string; color: string; body: string }[] = [
  {
    key: 'what',
    title: '它是什么',
    tag: '定位',
    color: 'blue',
    body: '本体叙事「构建 → 运行 → 消费 → 审计」中的消费与审计环节：把知识库语料抽取为自存 KG（SQLite 实体/关系/claim 三表），并以决策留痕 + PROV-O 溯源审计「数据从哪来、结论由何推导」。',
  },
  {
    key: 'why',
    title: '解决什么',
    tag: '痛点',
    color: 'geekblue',
    body: '黑盒决策不可审计：KG 怎么抽出来的、本体为何这样建、依据哪些原文片段，往往散落在日志里。本栏把「chunk → claim → KG → 本体 → 决策」固化为可查询的溯源链。',
  },
  {
    key: 'not',
    title: '不做什么',
    tag: '边界',
    color: 'default',
    body: '不引入外部语义层运行时（D-O15 反转 D-O10：semantica worker 归档休眠）；不替代 SPARQL 精确查询运行平面；KG 抽取用 REQ-98 LLM 能力代理 + 规则回退，前期够教学，多跳推理留待扩展。',
  },
]

/** ② 原理：GraphRAG 检索三步 + 溯源链 */
const RETRIEVE_STEPS = [
  { title: '向量命中', description: 'query embed → chunk 相似检索（M6 建制）' },
  { title: 'KG 一跳扩展', description: '命中 chunk 的 claim → seed 实体 → 邻接关系' },
  { title: '拼上下文', description: '实体 claim 按分合并 → 命中列表（degraded 回退向量）' },
]

/** ② 原理：数据链路（抽取 → 消费 → 审计回流） */
const PLANE_STEPS = [
  { title: '抽取（graphrag 导入）', description: 'chunks → REQ-98 LLM 抽实体/关系/claim → SQLite 自存表' },
  { title: '消费（本栏图谱/试查）', description: 'KG 浏览 + GraphRAG 试查 + 策略 B/C 构建本体' },
  { title: '审计（决策留痕）', description: '每次抽取/构建落 onto_decision，derived_from 串成溯源链 → PROV-O 导出' },
]

/** ③ 使用说明：四步演练 */
const DRILL_STEPS = [
  { title: '选库', description: '顶部选择知识库（graphrag 模式导入文档时自动抽取 KG）。' },
  { title: '看图谱 / 试查', description: '「KG 图谱」页浏览实体/关系/claim；「GraphRAG 试查」体验向量 + 图混合检索。' },
  { title: '重建 KG', description: 'rag 模式库或抽取失败时，点「重建 KG」显式重抽（LLM 主路径，失败自动回退规则抽取）。' },
  { title: '审计与导出', description: '「决策审计」页看留痕与溯源链，导出 PROV-O Turtle 供外部工具检查。' },
]

export default function AuditPage() {
  const [kbs, setKbs] = useState<OntoBuildSelectableKB[]>([])
  const [kbsErr, setKbsErr] = useState<string | null>(null)
  const [kbId, setKbId] = useState<string | undefined>(undefined)
  const [tab, setTab] = useState('graph')

  const loadKbs = () => {
    api
      .selectableKBsForBuild()
      .then((ls) => {
        setKbs(ls)
        setKbsErr(null)
        setKbId((cur) => cur ?? ls.find((k) => k.kg_ready)?.id ?? ls[0]?.id)
      })
      .catch((e: any) => {
        setKbs([])
        setKbsErr(e?.message ?? '知识库列表加载失败')
      })
  }

  useEffect(() => {
    loadKbs()
  }, [])

  const cur = kbs.find((k) => k.id === kbId)

  return (
    <div className="main">
      <div className="work-main sema-main">
        <div className="work-head">
          <div className="work-head-text">
            <div className="work-head-title">
              <Typography.Title level={4} style={{ margin: 0 }}>
                消费与审计
              </Typography.Title>
              <Tag color="purple" style={{ margin: 0 }}>
                消费 + 审计
              </Tag>
              <Tag style={{ margin: 0 }}>自研 KG · 零外部进程</Tag>
            </div>
            <p className="work-head-desc">
              本体叙事的「构建 → 运行 → <strong>消费</strong> → <strong>审计</strong>」环节载体（D-O15 改造自原
              Semantica 独立栏）：知识库语料抽取为自存 KG 供图谱浏览与 GraphRAG 检索，构建/抽取决策全程留痕可溯源。
            </p>
          </div>
          <Space size={8} wrap>
            <Select
              style={{ width: 300 }}
              value={kbId}
              onChange={setKbId}
              placeholder={kbsErr ? '知识库不可达' : '选择知识库'}
              options={kbs.map((k) => ({
                value: k.id,
                label: `${k.name}（KG ${k.kg_entities}/${k.kg_relationships}${k.kg_ready ? '' : ' · 未建'}）`,
              }))}
              notFoundContent={kbsErr ? '知识库接口未就绪' : '暂无知识库'}
            />
            <Button icon={<ReloadOutlined />} onClick={loadKbs}>
              刷新
            </Button>
          </Space>
        </div>

        {/* 状态条：当前库 KG 规模 + 重建入口 */}
        <div className="sema-status">
          {kbsErr ? (
            <Alert type="warning" showIcon message="知识库列表不可用" description={kbsErr} />
          ) : !cur ? (
            <Alert type="info" showIcon message="先选择一个知识库" description="没有合适的库？先到「知识库」页创建并导入文档。" />
          ) : (
            <Alert
              type={cur.kg_ready ? 'success' : 'warning'}
              showIcon
              message={
                <Space size={8} wrap>
                  <span>{cur.name}</span>
                  <Tag color="blue" style={{ margin: 0 }}>
                    {cur.mode === 'graphrag' ? 'graphrag 模式' : 'rag 模式'}
                  </Tag>
                  <Tag color={cur.kg_ready ? 'green' : 'default'} style={{ margin: 0 }}>
                    {cur.kg_ready ? 'KG 已建' : 'KG 未建'}
                  </Tag>
                </Space>
              }
              description={
                <Space size={16} wrap>
                  <span>文档 <b>{cur.doc_count}</b></span>
                  <span>chunk <b>{cur.chunk_count}</b></span>
                  <span>实体 <b>{cur.kg_entities}</b></span>
                  <span>关系 <b>{cur.kg_relationships}</b></span>
                  <RebuildButton kbId={cur.id} onDone={loadKbs} />
                </Space>
              }
            />
          )}
        </div>

        <Tabs
          activeKey={tab}
          onChange={setTab}
          items={[
            { key: 'graph', label: 'KG 图谱', children: <GraphTab kbId={kbId} /> },
            { key: 'query', label: 'GraphRAG 试查', children: <QueryTab kbId={kbId} /> },
            { key: 'audit', label: '决策审计', children: <AuditTab kbId={kbId} /> },
            { key: 'home', label: '学习引导', children: <HomeTab /> },
          ]}
        />

        <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 4 }}>
          D-O15/REQ-110：KG 与审计数据全部落主平台 SQLite（010 迁移），semantica worker 已归档休眠（tools/semantica-worker/ 代码保留、不进启动链路）。
        </Typography.Text>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// 重建 KG（显式重抽；LLM 主路径失败自动回退规则抽取）
// ---------------------------------------------------------------------------

function RebuildButton({ kbId, onDone }: { kbId: string; onDone?: () => void }) {
  const { showToast } = useUI()
  const [busy, setBusy] = useState(false)
  const doRebuild = async () => {
    setBusy(true)
    try {
      const r = await api.chunksToKG(kbId)
      const g = r.graphrag
      showToast(
        g.degraded
          ? `重建降级：${g.error ?? '未知原因'}`
          : `KG 已重建：实体 ${g.entities ?? 0} · 关系 ${g.relationships ?? 0}（${g.method ?? 'llm'}）`,
        g.degraded ? 'err' : 'ok',
      )
      onDone?.()
    } catch (e: any) {
      showToast(e instanceof ApiError ? e.message : (e?.message ?? '重建失败'), 'err')
    } finally {
      setBusy(false)
    }
  }
  return (
    <Button size="small" icon={<ExportOutlined />} loading={busy} disabled={busy} onClick={doRebuild}>
      重建 KG
    </Button>
  )
}

// ---------------------------------------------------------------------------
// KG 图谱（消费链路：实体 / 关系 / claim 溯源）
// ---------------------------------------------------------------------------

function GraphTab({ kbId }: { kbId?: string }) {
  const [data, setData] = useState<KGReadResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const load = () => {
    if (!kbId) return
    setLoading(true)
    api
      .kgRead(kbId)
      .then((d) => {
        setData(d)
        setErr(null)
      })
      .catch((e: any) => {
        setData(null)
        setErr(e?.message ?? 'KG 读取失败')
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    setData(null)
    setErr(null)
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kbId])

  const entities = data?.entities ?? []
  const rels = data?.relationships ?? []

  // 实体按 type 分组展示（无 type 归「未分类」；hook 须在早退之前调用）
  const byType = useMemo(() => {
    const m = new Map<string, KGEntity[]>()
    for (const e of entities) {
      const k = e.type || '未分类'
      if (!m.has(k)) m.set(k, [])
      m.get(k)!.push(e)
    }
    return [...m.entries()].sort((a, b) => b[1].length - a[1].length)
  }, [entities])

  if (!kbId) return <Empty description="先在顶部选择知识库" style={{ marginTop: 24 }} />
  if (err) return <Alert type="error" showIcon message="KG 读取失败" description={err} />

  const claimColumns: ColumnsType<KGClaim> = [
    { title: '主体', dataIndex: 'subject', width: 140, ellipsis: true },
    { title: '陈述（原文句）', dataIndex: 'text', ellipsis: true },
    {
      title: '出处 chunk',
      dataIndex: 'chunk_id',
      width: 200,
      render: (v) =>
        v ? (
          <Tooltip title={v}>
            <Typography.Text code style={{ fontSize: 12 }}>
              {String(v).slice(0, 12)}…
            </Typography.Text>
          </Tooltip>
        ) : (
          '—'
        ),
    },
  ]

  return (
    <div className="sema-home">
      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">1</span>
            <span>实体（按类型分组）</span>
          </Space>
        }
        extra={
          <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={load}>
            刷新
          </Button>
        }
      >
        {!data && loading ? (
          <Space size={8} style={{ padding: '12px 0' }}>
            <Spin size="small" />
            <Typography.Text type="secondary">读取中…</Typography.Text>
          </Space>
        ) : entities.length === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description="该库暂无 KG（graphrag 模式导入文档自动抽取，或点顶部「重建 KG」）"
          />
        ) : (
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12 }}>
            {byType.map(([t, es]) => (
              <Card
                key={t}
                type="inner"
                size="small"
                title={
                  <Space size={6}>
                    <Tag color="blue" style={{ margin: 0 }}>
                      {t}
                    </Tag>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {es.length}
                    </Typography.Text>
                  </Space>
                }
                style={{ width: 320 }}
              >
                {es.slice(0, 12).map((e) => (
                  <Tooltip key={e.id} title={e.description || e.name}>
                    <Tag style={{ marginBottom: 4 }}>{e.name}</Tag>
                  </Tooltip>
                ))}
                {es.length > 12 && (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    …共 {es.length} 个
                  </Typography.Text>
                )}
              </Card>
            ))}
          </div>
        )}
      </Card>

      {rels.length > 0 && (
        <Card
          size="small"
          className="work-card sema-card"
          title={
            <Space size={8}>
              <span className="sema-card-no">2</span>
              <span>关系（{rels.length}）</span>
            </Space>
          }
        >
          <div className="sema-claims">
            {rels.slice(0, 40).map((r: KGRelationship) => (
              <div className="sema-claim" key={r.id}>
                <p className="sema-claim-text">
                  <Typography.Text strong>{r.source}</Typography.Text>
                  <Tag color="purple" style={{ margin: '0 8px' }}>
                    {r.type || '关联'}
                  </Tag>
                  <Typography.Text strong>{r.target}</Typography.Text>
                </p>
              </div>
            ))}
            {rels.length > 40 && (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                …仅展示前 40 条（共 {rels.length}）
              </Typography.Text>
            )}
          </div>
        </Card>
      )}

      {(data?.claims?.length ?? 0) > 0 && (
        <Card
          size="small"
          className="work-card sema-card"
          title={
            <Space size={8}>
              <span className="sema-card-no">3</span>
              <span>claim 溯源（陈述 → 原文 chunk）</span>
            </Space>
          }
        >
          <Table<KGClaim>
            rowKey="id"
            columns={claimColumns}
            dataSource={data!.claims}
            pagination={{ pageSize: 8, hideOnSinglePage: true }}
            size="small"
            scroll={{ x: 'max-content' }}
          />
        </Card>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// GraphRAG 试查（向量命中 → KG 一跳扩展，教学口径）
// ---------------------------------------------------------------------------

function QueryTab({ kbId }: { kbId?: string }) {
  const { showToast } = useUI()
  const [q, setQ] = useState('')
  const [maxResults, setMaxResults] = useState<number>(5)
  const [querying, setQuerying] = useState(false)
  const [result, setResult] = useState<{ degraded?: boolean; error?: string; hits: import('../../api/types').KBHit[] } | null>(null)

  const doQuery = async () => {
    if (!kbId) {
      showToast('先在顶部选择知识库', 'err')
      return
    }
    if (!q.trim()) {
      showToast('请输入查询内容', 'err')
      return
    }
    setQuerying(true)
    setResult(null)
    try {
      const r = await api.graphragSearchKB(kbId, q.trim(), maxResults)
      setResult(r)
    } catch (e: any) {
      showToast(e?.message ?? '查询失败', 'err')
    } finally {
      setQuerying(false)
    }
  }

  return (
    <div className="sema-home">
      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">1</span>
            <span>GraphRAG 检索（向量 + KG 一跳扩展）</span>
          </Space>
        }
      >
        <Input.TextArea
          value={q}
          onChange={(e) => setQ(e.target.value)}
          autoSize={{ minRows: 2, maxRows: 5 }}
          placeholder="用自然语言提问，命中 chunk 的 claim 实体会沿 KG 关系一跳扩展拼入上下文"
        />
        <Space size={10} wrap style={{ marginTop: 10 }}>
          <Button type="primary" icon={<SearchOutlined />} loading={querying} onClick={doQuery} disabled={!kbId}>
            试查
          </Button>
          <InputNumber
            min={1}
            max={50}
            value={maxResults}
            onChange={(v) => setMaxResults(typeof v === 'number' ? v : 5)}
            addonBefore="max_results"
            style={{ width: 190 }}
          />
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            score=1.0 为 seed 实体命中、0.6 为一跳邻接；KG 无命中自动回退纯向量（degraded 标注）。
          </Typography.Text>
        </Space>

        {result?.degraded && (
          <Alert type="warning" showIcon style={{ marginTop: 10 }} message="已降级为向量检索" description={result.error} />
        )}
        {result && !result.degraded && result.hits.length === 0 && (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} style={{ margin: '16px 0' }} description="无命中（换个问法或先建 KG）" />
        )}
        {result && result.hits.length > 0 && (
          <div className="sema-claims">
            {result.hits.map((h, i) => (
              <div className="sema-claim" key={i}>
                <p className="sema-claim-text">{h.excerpt}</p>
                <div className="sema-claim-meta">
                  <Tag color="blue" style={{ margin: 0 }}>
                    score {h.score.toFixed(3)}
                  </Tag>
                  <Tag style={{ margin: 0 }}>
                    {h.doc} · #{h.seq}
                  </Tag>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">2</span>
            <span>教学对照 · SPARQL 精确查询 vs GraphRAG 语义检索</span>
          </Space>
        }
      >
        <div className="sema-compare">
          <div className="sema-compare-col">
            <div className="sema-compare-head">
              <span className="sema-compare-title">运行平面 · SPARQL 精确查询</span>
              <Tag color="geekblue" style={{ margin: 0 }}>
                结构精确匹配
              </Tag>
            </div>
            <ul className="sema-compare-list">
              <li>面向已知结构：类 / 属性 / 实例的精确三元组匹配。</li>
              <li>结果可复现、可解释，适合校验与断言（Fuseki 可开推理对照）。</li>
              <li>入口：本体运行栏 → SPARQL 工作台（REQ-92）。</li>
            </ul>
          </div>
          <div className="sema-compare-col">
            <div className="sema-compare-head">
              <span className="sema-compare-title">本栏 · GraphRAG 语义检索</span>
              <Tag color="purple" style={{ margin: 0 }}>
                语义近似 + 一跳
              </Tag>
            </div>
            <ul className="sema-compare-list">
              <li>面向模糊意图：向量命中后沿 KG 关系扩展，容忍同义 / 近义表述。</li>
              <li>教学口径三步：向量命中 → KG 一跳 → 拼上下文；多跳推理留待扩展。</li>
              <li>入口：本页试查（D-O15 自研，命中可回溯 chunk）。</li>
            </ul>
          </div>
        </div>
      </Card>
    </div>
  )
}

// ---------------------------------------------------------------------------
// 决策审计（SQLite 决策表 + derived_from 溯源链 + PROV-O 导出）
// ---------------------------------------------------------------------------

/** 客户端下载文本（PROV-O Turtle 导出） */
function downloadText(filename: string, text: string, mime: string) {
  const blob = new Blob([text], { type: mime })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

const KIND_TAG: Record<string, { color: string; text: string }> = {
  kg: { color: 'purple', text: 'kg' },
  ontology: { color: 'geekblue', text: 'ontology' },
  kb: { color: 'blue', text: 'kb' },
  manual: { color: 'default', text: 'manual' },
}

const KIND_OPTIONS = [
  { value: 'kg', label: 'kg（KG 抽取/重建）' },
  { value: 'ontology', label: 'ontology（本体构建/版本）' },
  { value: 'kb', label: 'kb（知识库操作）' },
  { value: 'manual', label: 'manual（手工补录）' },
]

function AuditTab({ kbId }: { kbId?: string }) {
  const { showToast } = useUI()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)
  const [decisions, setDecisions] = useState<OntoDecision[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [kindFilter, setKindFilter] = useState<string | undefined>(undefined)
  const [lastId, setLastId] = useState<string | null>(null)
  const [chainTarget, setChainTarget] = useState<OntoDecision | null>(null)
  const [exporting, setExporting] = useState(false)

  const load = () => {
    setLoading(true)
    api
      .listDecisions({ subject_kind: kindFilter, subject_id: kbId, limit: 50 })
      .then((r) => {
        setDecisions(r)
        setErr(null)
      })
      .catch((e: any) => {
        setDecisions([])
        setErr(e?.message ?? '决策列表获取失败')
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kbId, kindFilter])

  const submit = async () => {
    let v: any
    try {
      v = await form.validateFields()
    } catch {
      return
    }
    setBusy(true)
    try {
      const r = await api.createDecision({
        subject_kind: v.subject_kind,
        subject_id: v.subject_id || undefined,
        title: v.title,
        rationale: v.rationale || undefined,
        derived_from: v.derived_from || undefined,
      })
      setLastId(r.id)
      showToast('决策已留痕')
      form.resetFields()
      load()
    } catch (e: any) {
      showToast(e instanceof ApiError ? e.message : (e?.message ?? '记录失败'), 'err')
    } finally {
      setBusy(false)
    }
  }

  const doExport = async () => {
    setExporting(true)
    try {
      const text = await api.provExport(kbId)
      downloadText(`prov-audit-${kbId ?? 'all'}.ttl`, text, 'text/turtle;charset=utf-8')
      showToast('已导出 PROV-O Turtle（Go 原生模板）')
    } catch (e: any) {
      showToast(e?.message ?? '导出失败', 'err')
    } finally {
      setExporting(false)
    }
  }

  const columns: ColumnsType<OntoDecision> = [
    { title: '时间', dataIndex: 'created_at', width: 165, render: (v) => v || '—' },
    {
      title: '类别',
      dataIndex: 'subject_kind',
      width: 100,
      render: (v) => <Tag color={KIND_TAG[v]?.color ?? 'default'} style={{ margin: 0 }}>{KIND_TAG[v]?.text ?? v}</Tag>,
    },
    { title: '主体', dataIndex: 'subject_id', width: 150, ellipsis: true, render: (v) => v || '—' },
    { title: '决策', dataIndex: 'title', ellipsis: true },
    { title: '依据', dataIndex: 'rationale', ellipsis: true, render: (v) => v || '—' },
    {
      title: '操作',
      width: 110,
      render: (_, r) => (
        <Button type="link" size="small" icon={<BranchesOutlined />} onClick={() => setChainTarget(r)}>
          溯源链
        </Button>
      ),
    },
  ]

  return (
    <div className="sema-home">
      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">1</span>
            <span>决策留痕（抽取/构建自动记录 + 手工补录）</span>
          </Space>
        }
        extra={
          <Space size={8}>
            <Select
              size="small"
              style={{ width: 170 }}
              allowClear
              placeholder="全部类别"
              value={kindFilter}
              onChange={setKindFilter}
              options={KIND_OPTIONS}
            />
            <Button size="small" icon={<ExportOutlined />} loading={exporting} onClick={doExport}>
              导出 PROV-O
            </Button>
            <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={load}>
              刷新
            </Button>
          </Space>
        }
      >
        {err && <Alert type="warning" showIcon style={{ marginBottom: 10 }} message="决策列表获取失败" description={err} />}
        <Table<OntoDecision>
          rowKey="id"
          columns={columns}
          dataSource={decisions}
          loading={loading}
          pagination={{ pageSize: 8, hideOnSinglePage: true }}
          size="small"
          locale={{ emptyText: '暂无决策记录（KG 抽取/重建会自动留痕，也可在下方手工补录）' }}
          scroll={{ x: 'max-content' }}
        />
      </Card>

      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">2</span>
            <span>手工补录决策（PROV-O 溯源链节点）</span>
          </Space>
        }
      >
        <Form form={form} layout="vertical" requiredMark={false}>
          <div className="sema-form-grid">
            <Form.Item name="subject_kind" label="类别 subject_kind" initialValue="manual">
              <Select options={KIND_OPTIONS} />
            </Form.Item>
            <Form.Item name="subject_id" label="主体 subject_id（可选，如 kb_id / 本体 id）">
              <Input placeholder="如当前知识库 id" />
            </Form.Item>
          </div>
          <Form.Item name="title" label="决策 title" rules={[{ required: true, message: '决策标题必填' }]}>
            <Input placeholder="如：采用 kg-direct 策略由 KB 直转本体" />
          </Form.Item>
          <Form.Item name="rationale" label="依据 rationale">
            <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} placeholder="依据哪些事实/来源、经何推理" />
          </Form.Item>
          <Form.Item name="derived_from" label="前置决策 derived_from（可选，串成溯源链）">
            <Input placeholder="另一条决策的 id" />
          </Form.Item>
          <Space size={10} wrap>
            <Button type="primary" icon={<SaveOutlined />} loading={busy} onClick={submit}>
              记录决策
            </Button>
            {lastId && (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                最近记录 ID：<Typography.Text code style={{ fontSize: 12 }}>{lastId}</Typography.Text>
              </Typography.Text>
            )}
          </Space>
        </Form>
      </Card>

      {chainTarget && <ChainDrawer decision={chainTarget} onClose={() => setChainTarget(null)} />}
    </div>
  )
}

/** 溯源链抽屉：沿 derived_from 回溯（后端 32 跳封顶 + 环防御） */
function ChainDrawer({ decision, onClose }: { decision: OntoDecision; onClose: () => void }) {
  const [chain, setChain] = useState<OntoDecision[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const load = () => {
    setLoading(true)
    setErr(null)
    api
      .decisionChain(decision.id)
      .then((r) => {
        setChain(r)
        setErr(null)
      })
      .catch((e: any) => {
        setChain([])
        setErr(e?.message ?? '溯源链获取失败')
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [decision.id])

  return (
    <Drawer
      open
      width={640}
      title={
        <Space size={8}>
          <BranchesOutlined />
          <span>溯源链 · {decision.title}</span>
        </Space>
      }
      onClose={onClose}
      extra={
        <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={load}>
          刷新
        </Button>
      }
    >
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        决策 id：<Typography.Text code style={{ fontSize: 12 }}>{decision.id}</Typography.Text>；链路 = 本条沿
        derived_from 逐级回溯（32 跳封顶，环防御）。
      </Typography.Text>

      {err && <Alert type="error" showIcon style={{ marginTop: 10 }} message="溯源链获取失败" description={err} />}

      <div className="sema-audit-chain" style={{ marginTop: 12 }}>
        {loading && chain.length === 0 ? (
          <Spin size="small" />
        ) : chain.length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无链路" />
        ) : (
          <Timeline
            items={chain.map((d, i) => ({
              key: d.id,
              color: i === 0 ? 'green' : 'blue',
              title: (
                <Space size={6} wrap>
                  <Typography.Text code style={{ fontSize: 12 }}>
                    {d.id}
                  </Typography.Text>
                  <Tag color={KIND_TAG[d.subject_kind]?.color ?? 'default'} style={{ margin: 0 }}>
                    {d.subject_kind}
                  </Tag>
                  {i === 0 && (
                    <Tag color="green" style={{ margin: 0 }}>
                      起点
                    </Tag>
                  )}
                </Space>
              ),
              content: (
                <div className="sema-audit-node">
                  <div>
                    <span className="sema-audit-k">决策</span>
                    {d.title}
                  </div>
                  {d.rationale && (
                    <div>
                      <span className="sema-audit-k">依据</span>
                      {d.rationale}
                    </div>
                  )}
                  {d.derived_from && (
                    <div>
                      <span className="sema-audit-k">前置</span>
                      <Typography.Text code style={{ fontSize: 12 }}>{d.derived_from}</Typography.Text>
                    </div>
                  )}
                  <div>
                    <span className="sema-audit-k">时间</span>
                    {d.created_at}
                  </div>
                </div>
              ),
            }))}
          />
        )}
      </div>
    </Drawer>
  )
}

// ---------------------------------------------------------------------------
// 学习引导（三段式：功能 / 原理 / 使用说明；去-semantica 化后口径）
// ---------------------------------------------------------------------------

function HomeTab() {
  return (
    <div className="sema-home">
      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">①</span>
            <span>功能 · 它是什么、解决什么、不做什么</span>
          </Space>
        }
      >
        <div className="sema-grid-3">
          {FEATURES.map((f) => (
            <div className="sema-feature" key={f.key}>
              <div className="sema-feature-head">
                <span className="sema-feature-title">{f.title}</span>
                <Tag color={f.color} style={{ margin: 0 }}>
                  {f.tag}
                </Tag>
              </div>
              <p className="sema-feature-body">{f.body}</p>
            </div>
          ))}
        </div>
      </Card>

      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">②</span>
            <span>原理 · GraphRAG 检索三步、PROV-O 溯源与数据链路</span>
          </Space>
        }
      >
        <div className="onto-sec" style={{ marginTop: 0 }}>
          <span className="onto-sec-title">GraphRAG 检索三步（教学口径）</span>
        </div>
        <Steps size="small" orientation="horizontal" titlePlacement="vertical" responsive={false} items={RETRIEVE_STEPS} />

        <div className="onto-sec">
          <span className="onto-sec-title">PROV-O 溯源机制</span>
        </div>
        <p className="sema-p">
          决策按 W3C PROV-O 语义留痕：<Typography.Text code>Activity</Typography.Text>（一次抽取/构建/手工决策）、
          <Typography.Text code>Entity</Typography.Text>（作用主体 kb/本体）、
          <Typography.Text code>wasDerivedFrom</Typography.Text>（前置决策）。「导出 PROV-O」生成 Turtle 供外部工具检查
        </p>

        <div className="onto-sec">
          <span className="onto-sec-title">数据链路（抽取 → 消费 → 审计回流）</span>
        </div>
        <Steps size="small" orientation="horizontal" titlePlacement="vertical" responsive={false} items={PLANE_STEPS} />
        <p className="sema-p sema-p-muted">
          KG 抽取用 REQ-98 LLM 能力代理（chat.GenerateStructured），失败自动回退规则抽取（「A 是 B」→ IS_A 等句式）；
          M14 非阻断降级语义保留：抽取失败不影响导入与向量检索。
        </p>
      </Card>

      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">③</span>
            <span>使用说明 · 四步演练</span>
          </Space>
        }
      >
        <Steps size="small" orientation="vertical" items={DRILL_STEPS} />
      </Card>
    </div>
  )
}
