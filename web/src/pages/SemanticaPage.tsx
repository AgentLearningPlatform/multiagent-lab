import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Result,
  Select,
  Space,
  Spin,
  Steps,
  Table,
  Tabs,
  Tag,
  Timeline,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  BranchesOutlined,
  ExportOutlined,
  HistoryOutlined,
  LinkOutlined,
  ReloadOutlined,
  SaveOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import { api, ApiError } from '../api/client'
import type {
  Ontology,
  SemanticaCausalType,
  SemanticaChainNode,
  SemanticaClaim,
  SemanticaDecision,
  SemanticaDecisionInput,
  SemanticaHealth,
  SemanticaIngestResult,
  SemanticaProvNode,
} from '../api/types'
import { useUI } from '../store/ui'

// ---------------------------------------------------------------------------
// 首页文案（三段式内容资产，本期内嵌；seeds/learning/semantica/ 抽取为后续项）
// ---------------------------------------------------------------------------

/** ① 功能：它是什么 / 解决什么 / 不做什么 */
const FEATURES: { key: string; title: string; tag: string; color: string; body: string }[] = [
  {
    key: 'what',
    title: '它是什么',
    tag: '定位',
    color: 'blue',
    body: 'AI Agent 的语义层与决策智能层：把碎片数据整理成受本体治理（OWL / SHACL / SKOS）的 Context Graph，并为决策提供可追溯的上下文。开源（MIT），GitHub 约 13.3K stars。',
  },
  {
    key: 'why',
    title: '解决什么',
    tag: '痛点',
    color: 'geekblue',
    body: '黑盒决策不可审计：Agent 为什么给出这个结论、依据哪些事实与来源，往往散落在提示词与日志里。semantica 把「事实 → 来源 → 推理路径 → 结论」固化为可查询的记录。',
  },
  {
    key: 'not',
    title: '不做什么',
    tag: '边界',
    color: 'default',
    body: '不解释 LLM 内部的思维过程（不做 chain-of-thought 可视化），只审计系统级决策链；不替代本体构建与运行平面（不并入 facade，不接管 SPARQL 引擎）。',
  },
]

/** ② 原理：三层架构 */
const ARCH_STEPS = [
  { title: 'Input', description: 'ingest / parse / split / normalize' },
  { title: 'Semantic', description: 'semantic_extract / kg / ontology / reasoning' },
  { title: 'Storage', description: 'graph_store / vector_store / triplet_store' },
]

/** ② 原理：与两平面的关系（构建 → 消费 → 审计回流） */
const PLANE_STEPS = [
  { title: '构建平面（本体）', description: 'Spec 保存 → 导出 TTL（GET /export?format=turtle）' },
  { title: 'semantica（消费）', description: 'ingest-ttl → ContextGraph 建库（实体 / 关系）' },
  { title: '审计链路（回流）', description: 'record_decision → PROV-O 溯源展示（REQ-101，P2）' },
]

/** ③ 使用说明：五步演练 */
const DRILL_STEPS = [
  { title: '选本体', description: '在「知识图谱」页从本体列表选择要消费的本体。' },
  { title: '导出 TTL', description: '调用构建平面 GET /api/ontologies/{id}/export?format=turtle 取 Turtle 原文。' },
  { title: 'semantica ingest 建库', description: 'POST /api/semantica/ingest-ttl → ContextGraph（返回实体 / 关系数与警告）。' },
  { title: '图谱 / GraphRAG 查询', description: 'POST /api/semantica/query：向量 + 图混合检索，偏语义近似与多跳。' },
  { title: 'record_decision + 溯源查看', description: '落决策记录并查看 PROV-O 审计链（完整链可视化属 REQ-101 / P2）。' },
]

// ---------------------------------------------------------------------------
// 主页面
// ---------------------------------------------------------------------------

/**
 * Semantica 独立栏（docs/04 §4.9 D-O10 / REQ-99~101）：
 * 本体叙事「构建 → 运行 → 消费 → 审计」中的消费 + 审计学习载体，独立路由、零侵入两平面/facade。
 * 布局：无左树，采用「页头 + Worker 状态条 + 三页签（首页 / 知识图谱 / 决策审计）」单主区。
 */
export default function SemanticaPage() {
  const { setPage } = useUI()
  const [health, setHealth] = useState<SemanticaHealth | null>(null)
  const [healthErr, setHealthErr] = useState<string | null>(null)
  const [healthLoading, setHealthLoading] = useState(false)
  const [tab, setTab] = useState('home')

  const loadHealth = () => {
    setHealthLoading(true)
    api
      .semanticaHealth()
      .then((h) => {
        setHealth(h)
        setHealthErr(null)
      })
      .catch((e: any) => {
        setHealth(null)
        setHealthErr(e?.message ?? 'semantica worker 不可达')
      })
      .finally(() => setHealthLoading(false))
  }

  useEffect(() => {
    loadHealth()
  }, [])

  const down = !healthLoading && !!healthErr

  return (
    <div className="main">
      <div className="work-main sema-main">
        <div className="work-head">
          <div className="work-head-text">
            <div className="work-head-title">
              <Typography.Title level={4} style={{ margin: 0 }}>
                Semantica
              </Typography.Title>
              <Tag color="purple" style={{ margin: 0 }}>
                消费 + 审计
              </Tag>
              <Tag style={{ margin: 0 }}>MIT · 独立集成</Tag>
            </div>
            <p className="work-head-desc">
              本体叙事的「构建 → 运行 → <strong>消费</strong> → <strong>审计</strong>」学习载体：把本体 TTL 消费为受治理的
              Context Graph，并以 PROV-O 记录决策链。独立栏、零侵入构建 / 运行两平面与 facade。
            </p>
          </div>
          <Button icon={<ReloadOutlined />} loading={healthLoading} onClick={loadHealth}>
            刷新状态
          </Button>
        </div>

        {/* Worker 状态条：置顶；未启动时降级 Result + 启动指引 */}
        <div className="sema-status">
          {healthLoading && !health ? (
            <Space size={8}>
              <Spin size="small" />
              <Typography.Text type="secondary">正在探测 semantica worker（:8093）…</Typography.Text>
            </Space>
          ) : down ? (
            <Result
              status="warning"
              title="semantica worker 未启动"
              subTitle={
                <>
                  先安装并拉起 worker（首次安装依赖体积较大）：
                  <br />
                  <Typography.Text code>bash tools/semantica-worker/setup.sh &amp;&amp; tools/semantica-worker/run.sh</Typography.Text>
                  <br />
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    （约定端口 :8093；主平台反代 /api/semantica/* → worker）· {healthErr}
                  </Typography.Text>
                </>
              }
              extra={<Button onClick={loadHealth}>重试</Button>}
            />
          ) : health ? (
            <Alert
              type="success"
              showIcon
              message={
                <Space size={8} wrap>
                  <span>semantica worker 运行中</span>
                  <Tag color="green" style={{ margin: 0 }}>
                    v{health.version}
                  </Tag>
                  <Tag color={health.graph_loaded ? 'blue' : 'default'} style={{ margin: 0 }}>
                    {health.graph_loaded ? '图已加载' : '图未加载'}
                  </Tag>
                </Space>
              }
              description={
                <Space size={16} wrap>
                  <span>实体 <b>{health.entities}</b></span>
                  <span>关系 <b>{health.relationships}</b></span>
                  <span>决策 <b>{health.decisions}</b></span>
                </Space>
              }
            />
          ) : null}
        </div>

        <Tabs
          activeKey={tab}
          onChange={setTab}
          items={[
            { key: 'home', label: '首页', children: <HomeTab /> },
            { key: 'graph', label: '知识图谱', children: <GraphTab onMutated={loadHealth} onGoOntology={() => setPage('ontology')} /> },
            {
              key: 'audit',
              label: '决策审计',
              children: <AuditTab onMutated={loadHealth} workerDown={down} onGoOntology={() => setPage('ontology')} />,
            },
          ]}
        />

        <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 4 }}>
          三段式文案本期内嵌于页面组件；内容资产抽取（seeds/learning/semantica/）列为后续项。GraphRAG / embeddings 不并入构建平面，知识库模块边界不变。
        </Typography.Text>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// 首页（三段式学习引导）
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
            <span>原理 · 三层架构、PROV-O 溯源、决策生命周期与两平面关系</span>
          </Space>
        }
      >
        <div className="onto-sec" style={{ marginTop: 0 }}>
          <span className="onto-sec-title">三层架构</span>
        </div>
        <Steps size="small" orientation="horizontal" titlePlacement="vertical" responsive={false} items={ARCH_STEPS} />

        <div className="onto-sec">
          <span className="onto-sec-title">PROV-O 溯源机制</span>
        </div>
        <p className="sema-p">
          决策链按 W3C PROV-O 记录：<Typography.Text code>Entity</Typography.Text>（事实 / 节点）、
          <Typography.Text code>Activity</Typography.Text>（推理 / 检索过程）、
          <Typography.Text code>Agent</Typography.Text>（发起者）三者以 <Typography.Text code>wasDerivedFrom</Typography.Text> /
          <Typography.Text code>wasGeneratedBy</Typography.Text> 关联，形成「事实 → 来源 → 推理路径 → 结论」可查询链路。
        </p>

        <div className="onto-sec">
          <span className="onto-sec-title">record_decision 决策生命周期与因果链</span>
        </div>
        <p className="sema-p">
          一次决策 = <Typography.Text code>category</Typography.Text>（类别）+
          <Typography.Text code>scenario</Typography.Text>（情境）+
          <Typography.Text code>reasoning</Typography.Text>（依据 / 推理）+
          <Typography.Text code>outcome</Typography.Text>（结论）+
          可选 <Typography.Text code>confidence</Typography.Text>（置信度）。记录后即可由因果链回溯到支撑它的事实与来源。
        </p>

        <div className="onto-sec">
          <span className="onto-sec-title">与两平面的关系</span>
        </div>
        <Steps size="small" orientation="horizontal" titlePlacement="vertical" responsive={false} items={PLANE_STEPS} />
        <p className="sema-p sema-p-muted">
          构建平面只负责供给本体 TTL；semantica 独立消费建 KG；审计链路仅回流展示，不改动两平面与 facade。
        </p>
      </Card>

      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">③</span>
            <span>使用说明 · 五步演练</span>
          </Space>
        }
      >
        <Steps size="small" orientation="vertical" items={DRILL_STEPS} />
      </Card>
    </div>
  )
}

// ---------------------------------------------------------------------------
// 知识图谱（消费链路，REQ-100）
// ---------------------------------------------------------------------------

function GraphTab({ onMutated, onGoOntology }: { onMutated: () => void; onGoOntology: () => void }) {
  const { showToast } = useUI()
  const [ontos, setOntos] = useState<Ontology[]>([])
  const [ontosErr, setOntosErr] = useState<string | null>(null)
  const [ontoId, setOntoId] = useState<string | undefined>(undefined)

  const [ingesting, setIngesting] = useState(false)
  const [ingest, setIngest] = useState<SemanticaIngestResult | null>(null)
  const [ingestErr, setIngestErr] = useState<string | null>(null)

  const [q, setQ] = useState('')
  const [maxResults, setMaxResults] = useState<number>(5)
  const [querying, setQuerying] = useState(false)
  const [claims, setClaims] = useState<SemanticaClaim[] | null>(null)
  const [queryErr, setQueryErr] = useState<string | null>(null)

  useEffect(() => {
    api
      .listOntologies()
      .then((ls) => {
        setOntos(ls)
        setOntosErr(null)
        setOntoId((cur) => cur ?? ls[0]?.id)
      })
      .catch((e: any) => {
        setOntos([])
        setOntosErr(e?.message ?? '本体列表加载失败')
      })
  }, [])

  const doIngest = async () => {
    if (!ontoId) {
      showToast('请先选择本体', 'err')
      return
    }
    setIngesting(true)
    setIngest(null)
    setIngestErr(null)
    try {
      const ttl = await api.exportOntologyTurtle(ontoId)
      const r = await api.semanticaIngestTtl(ontoId, ttl)
      setIngest(r)
      showToast(`已建库：实体 ${r.entities} · 关系 ${r.relationships}`)
      onMutated()
    } catch (e: any) {
      const msg = e instanceof ApiError ? e.message : e?.message ?? '建库失败'
      setIngestErr(msg)
      showToast(msg, 'err')
    } finally {
      setIngesting(false)
    }
  }

  const doQuery = async () => {
    if (!q.trim()) {
      showToast('请输入查询内容', 'err')
      return
    }
    setQuerying(true)
    setQueryErr(null)
    setClaims(null)
    try {
      const r = await api.semanticaQuery(q.trim(), maxResults)
      setClaims(r.claims ?? [])
    } catch (e: any) {
      setQueryErr(e?.message ?? '查询失败')
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
            <span>TTL → KG 建库（消费链路）</span>
          </Space>
        }
      >
        <Space size={8} wrap style={{ marginBottom: 10 }}>
          <Select
            style={{ width: 300 }}
            value={ontoId}
            onChange={setOntoId}
            placeholder={ontosErr ? '本体平面不可达' : '选择要消费的本体'}
            options={ontos.map((o) => ({ value: o.id, label: `${o.name}（v${o.version ?? '—'}）` }))}
            notFoundContent={ontosErr ? '本体平面未就绪' : '暂无本体'}
          />
          <Button type="primary" icon={<ExportOutlined />} loading={ingesting} disabled={!ontoId} onClick={doIngest}>
            导出 TTL 并建库
          </Button>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            导出 <Typography.Text code style={{ fontSize: 12 }}>GET /api/ontologies/{'{id}'}/export?format=turtle</Typography.Text>
            {' '}→ <Typography.Text code style={{ fontSize: 12 }}>POST /api/semantica/ingest-ttl</Typography.Text>
          </Typography.Text>
        </Space>

        {ingestErr && <Alert type="error" showIcon message="建库失败" description={ingestErr} />}
        {ingest && (
          <Alert
            type={ingest.warnings && ingest.warnings.length > 0 ? 'warning' : 'success'}
            showIcon
            message={`已建库：实体 ${ingest.entities} · 关系 ${ingest.relationships}`}
            description={
              ingest.warnings && ingest.warnings.length > 0 ? (
                <ul className="sema-warn-list">
                  {ingest.warnings.map((w, i) => (
                    <li key={i}>{w}</li>
                  ))}
                </ul>
              ) : (
                '无警告，实体与关系已写入 ContextGraph。'
              )
            }
          />
        )}
        {ontosErr && <Alert type="warning" showIcon style={{ marginTop: 10 }} message="本体平面暂不可达" description={ontosErr} />}
      </Card>

      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">2</span>
            <span>GraphRAG 语义问答（向量 + 图混合检索）</span>
          </Space>
        }
      >
        <Input.TextArea
          value={q}
          onChange={(e) => setQ(e.target.value)}
          autoSize={{ minRows: 2, maxRows: 5 }}
          placeholder="用自然语言提问，如：哪些实例与某个概念相关？"
        />
        <Space size={10} wrap style={{ marginTop: 10 }}>
          <Button type="primary" icon={<SearchOutlined />} loading={querying} onClick={doQuery}>
            语义查询
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
            命中含 score / source_node，可回溯到图节点。
          </Typography.Text>
        </Space>

        {queryErr && <Alert type="error" showIcon style={{ marginTop: 10 }} message="查询失败" description={queryErr} />}
        {claims && claims.length === 0 && (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} style={{ margin: '16px 0' }} description="无命中（可先建库或换个问法）" />
        )}
        {claims && claims.length > 0 && (
          <div className="sema-claims">
            {claims.map((c, i) => {
              const score = typeof c.score === 'number' ? c.score : typeof c.score === 'string' && c.score !== '' ? Number(c.score) : NaN
              return (
                <div className="sema-claim" key={i}>
                  <p className="sema-claim-text">{c.text}</p>
                  <div className="sema-claim-meta">
                    {Number.isFinite(score) && <Tag color="blue" style={{ margin: 0 }}>score {score.toFixed(3)}</Tag>}
                    {c.source_node ? (
                      <Tag style={{ margin: 0 }}>来源 {c.source_node}</Tag>
                    ) : (
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        无来源节点
                      </Typography.Text>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </Card>

      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">3</span>
            <span>教学对照 · 精确查询 vs 语义问答</span>
          </Space>
        }
      >
        <div className="sema-compare">
          <div className="sema-compare-col">
            <div className="sema-compare-head">
              <span className="sema-compare-title">运行平面 facade · 精确查询（SPARQL）</span>
              <Tag color="geekblue" style={{ margin: 0 }}>
                结构精确匹配
              </Tag>
            </div>
            <ul className="sema-compare-list">
              <li>面向已知结构：类 / 属性 / 实例的精确三元组匹配。</li>
              <li>结果可复现、可解释，适合校验与断言。</li>
              <li>入口：本体页 S5 运行方式 →「SPARQL」工作台（REQ-92）。</li>
            </ul>
          </div>
          <div className="sema-compare-col">
            <div className="sema-compare-head">
              <span className="sema-compare-title">semantica · GraphRAG 语义问答</span>
              <Tag color="purple" style={{ margin: 0 }}>
                语义近似 + 多跳
              </Tag>
            </div>
            <ul className="sema-compare-list">
              <li>面向模糊意图：向量 + 图混合检索，容忍同义 / 近义表述。</li>
              <li>可沿关系多跳聚合上下文，适合探索与问答。</li>
              <li>入口：本页「知识图谱」建库后语义查询（REQ-100）。</li>
            </ul>
          </div>
        </div>
        <div style={{ marginTop: 10 }}>
          <Button size="small" onClick={onGoOntology}>
            前往本体页 · S5 SPARQL 工作台
          </Button>
        </div>
      </Card>
    </div>
  )
}

// ---------------------------------------------------------------------------
// 决策审计（REQ-101 完整审计/溯源学习视图，§4.9.4）
// ---------------------------------------------------------------------------

/** 置信度 → 文本（防御式：worker 可能返回字符串 / null） */
function confidenceText(v: unknown): string {
  const n = typeof v === 'number' ? v : typeof v === 'string' && v !== '' ? Number(v) : NaN
  return Number.isFinite(n) ? n.toFixed(2) : '—'
}

/** 任意值 → 可读文本（对象 JSON 化；PROV-O source/metadata 形状不定） */
function fmtAny(v: unknown): string {
  if (v === null || v === undefined || v === '') return '—'
  if (typeof v === 'string') return v
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

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

const CAUSAL_OPTIONS: { value: SemanticaCausalType; label: string }[] = [
  { value: 'CAUSED', label: 'CAUSED（导致）' },
  { value: 'INFLUENCED', label: 'INFLUENCED（影响）' },
  { value: 'PRECEDENT_FOR', label: 'PRECEDENT_FOR（先例）' },
]

const DECISION_COLUMNS: ColumnsType<SemanticaDecision> = [
  { title: '时间', dataIndex: 'ts', width: 175, render: (v) => v || '—' },
  { title: '类别', dataIndex: 'category', width: 130, render: (v) => v || '—' },
  { title: '情境', dataIndex: 'scenario', ellipsis: true, render: (v) => v || '—' },
  { title: '结论', dataIndex: 'outcome', ellipsis: true, render: (v) => v || '—' },
  { title: '置信度', dataIndex: 'confidence', width: 100, render: (v) => confidenceText(v) },
  {
    title: 'ID',
    dataIndex: 'id',
    width: 160,
    render: (v) => (
      <Typography.Text code style={{ fontSize: 12 }}>
        {v || '—'}
      </Typography.Text>
    ),
  },
]

function AuditTab({
  onMutated,
  workerDown,
  onGoOntology,
}: {
  onMutated: () => void
  workerDown: boolean
  onGoOntology: () => void
}) {
  const { showToast } = useUI()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)
  const [decisions, setDecisions] = useState<SemanticaDecision[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [lastId, setLastId] = useState<string | null>(null)

  // 审计抽屉：决策链 / PROV-O 溯源
  const [chainTarget, setChainTarget] = useState<SemanticaDecision | null>(null)
  const [lineageTarget, setLineageTarget] = useState<string | null>(null)

  const load = () => {
    setLoading(true)
    api
      .semanticaDecisions(20)
      .then((r) => {
        setDecisions(r.decisions ?? [])
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
  }, [])

  const sorted = useMemo(() => {
    const arr = [...decisions]
    arr.sort((a, b) => String(b.ts ?? '').localeCompare(String(a.ts ?? '')))
    return arr
  }, [decisions])

  const submit = async () => {
    let v: any
    try {
      v = await form.validateFields()
    } catch {
      return
    }
    const payload: SemanticaDecisionInput = {
      category: v.category,
      scenario: v.scenario,
      reasoning: v.reasoning,
      outcome: v.outcome,
      ...(typeof v.confidence === 'number' ? { confidence: v.confidence } : {}),
    }
    setBusy(true)
    try {
      const r = await api.semanticaRecordDecision(payload)
      setLastId(r.decision_id ?? null)
      showToast('决策已记录')
      form.resetFields()
      load()
      onMutated()
    } catch (e: any) {
      showToast(e?.message ?? '记录失败', 'err')
    } finally {
      setBusy(false)
    }
  }

  // 决策列表列：基础列 + 行内审计动作（决策链 / 溯源）
  const columns: ColumnsType<SemanticaDecision> = [
    ...DECISION_COLUMNS,
    {
      title: '操作',
      width: 170,
      render: (_, r) => (
        <Space size={2}>
          <Button type="link" size="small" icon={<BranchesOutlined />} disabled={!r.id} onClick={() => setChainTarget(r)}>
            决策链
          </Button>
          <Button type="link" size="small" icon={<HistoryOutlined />} onClick={() => setLineageTarget(r.id || '')}>
            溯源
          </Button>
        </Space>
      ),
    },
  ]

  return (
    <div className="sema-home">
      <TeachingStoryCard onGoOntology={onGoOntology} />

      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">1</span>
            <span>record_decision · 录入一次决策</span>
          </Space>
        }
      >
        <Form form={form} layout="vertical" requiredMark={false}>
          <div className="sema-form-grid">
            <Form.Item name="category" label="类别 category" rules={[{ required: true, message: '类别必填' }]}>
              <Input placeholder="如：故障处置 / 需求取舍" />
            </Form.Item>
            <Form.Item name="confidence" label="置信度 confidence（0~1，可选）">
              <InputNumber min={0} max={1} step={0.05} style={{ width: '100%' }} placeholder="如 0.8" />
            </Form.Item>
          </div>
          <Form.Item name="scenario" label="情境 scenario" rules={[{ required: true, message: '情境必填' }]}>
            <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} placeholder="决策发生在什么情境下" />
          </Form.Item>
          <Form.Item name="reasoning" label="依据 / 推理 reasoning" rules={[{ required: true, message: '推理必填' }]}>
            <Input.TextArea autoSize={{ minRows: 3, maxRows: 6 }} placeholder="依据哪些事实与来源、经何推理路径" />
          </Form.Item>
          <Form.Item name="outcome" label="结论 outcome" rules={[{ required: true, message: '结论必填' }]}>
            <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} placeholder="最终结论 / 动作" />
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

      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">2</span>
            <span>决策记录（最近 20 条，时间倒序）</span>
          </Space>
        }
        extra={
          <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={load}>
            刷新
          </Button>
        }
      >
        {err && <Alert type="warning" showIcon style={{ marginBottom: 10 }} message="决策列表获取失败" description={err} />}
        <Table<SemanticaDecision>
          rowKey={(r) => r.id || `${r.ts}-${r.category}-${r.outcome}`}
          columns={columns}
          dataSource={sorted}
          loading={loading}
          pagination={false}
          size="small"
          locale={{ emptyText: '暂无决策记录（在上方录入一次，或经挂载 semantica MCP 的对话产生）' }}
          scroll={{ x: 'max-content' }}
        />
      </Card>

      <ExplorerCard workerDown={workerDown} />

      {chainTarget && (
        <DecisionChainDrawer
          decision={chainTarget}
          decisions={sorted}
          onClose={() => setChainTarget(null)}
          onChanged={() => {
            load()
            onMutated()
          }}
        />
      )}
      {lineageTarget !== null && <LineageDrawer initialEntityId={lineageTarget} onClose={() => setLineageTarget(null)} />}
    </div>
  )
}

// ---------------------------------------------------------------------------
// 审计辅助组件：教学故事卡 / Explorer 嵌入 / 决策链抽屉 / PROV-O 溯源抽屉
// ---------------------------------------------------------------------------

/** 教学故事卡（§4.9.4）：推理可解释（REQ-94）与决策可审计（REQ-101）两段互链 */
function TeachingStoryCard({ onGoOntology }: { onGoOntology: () => void }) {
  return (
    <Card
      size="small"
      className="work-card sema-card"
      title={
        <Space size={8}>
          <span className="sema-card-no">0</span>
          <span>教学故事 · 推理可解释 + 决策可审计（REQ-94 + REQ-101）</span>
        </Space>
      }
    >
      <div className="sema-compare">
        <div className="sema-compare-col">
          <div className="sema-compare-head">
            <span className="sema-compare-title">推理可解释</span>
            <Tag color="geekblue" style={{ margin: 0 }}>
              REQ-94
            </Tag>
          </div>
          <ul className="sema-compare-list">
            <li>本体页 S5 运行方式 →「透视」页签：onto_* 每次翻译为 SPARQL 的调用留痕（原文 / 耗时 / 结果数）。</li>
            <li>回答「引擎为何给出这个结果」。</li>
          </ul>
        </div>
        <div className="sema-compare-col">
          <div className="sema-compare-head">
            <span className="sema-compare-title">决策可审计</span>
            <Tag color="purple" style={{ margin: 0 }}>
              REQ-101
            </Tag>
          </div>
          <ul className="sema-compare-list">
            <li>本页：record_decision 落决策记录 → 决策链（因果 / 先例）→ PROV-O 溯源（事实 → 来源 → 推理路径）。</li>
            <li>回答「系统为何做出这个决策、依据哪些事实与来源」。</li>
          </ul>
        </div>
      </div>
      <div style={{ marginTop: 10 }}>
        <Button size="small" onClick={onGoOntology}>
          前往本体页 · S5「透视」页签（推理对照）
        </Button>
      </div>
    </Card>
  )
}

/** Explorer 嵌入：semantica 原生 Explorer（React 19 + Sigma.js）经 /semantica/explorer/ 反代 iframe */
function ExplorerCard({ workerDown }: { workerDown: boolean }) {
  const [loading, setLoading] = useState(true)
  return (
    <Card
      size="small"
      className="work-card sema-card"
      title={
        <Space size={8}>
          <span className="sema-card-no">3</span>
          <span>Explorer 嵌入（semantica 原生图谱审计视图）</span>
        </Space>
      }
      extra={
        <Button size="small" type="link" icon={<LinkOutlined />} href="/semantica/explorer/" target="_blank" rel="noreferrer">
          新窗口打开
        </Button>
      }
    >
      {workerDown ? (
        <Alert
          type="warning"
          showIcon
          message="semantica worker 未启动，Explorer 不可用"
          description="启动 worker（:8093）后此区域将嵌入 semantica 原生 Explorer；若未安装 semantica[explorer] 扩展，Explorer 会返回 503。"
        />
      ) : (
        <div className="sema-explorer">
          {loading && (
            <div className="sema-explorer-skeleton">
              <Spin size="small" />
              <Typography.Text type="secondary">正在加载 Explorer（React 19 + Sigma.js）…</Typography.Text>
            </div>
          )}
          <iframe
            title="semantica-explorer"
            src="/semantica/explorer/"
            className="sema-explorer-iframe"
            onLoad={() => setLoading(false)}
          />
        </div>
      )}
    </Card>
  )
}

/** 决策链抽屉：Timeline 渲染链节点 + 因果/先例关系写入（REQ-101） */
function DecisionChainDrawer({
  decision,
  decisions,
  onClose,
  onChanged,
}: {
  decision: SemanticaDecision
  decisions: SemanticaDecision[]
  onClose: () => void
  onChanged: () => void
}) {
  const { showToast } = useUI()
  const [chain, setChain] = useState<SemanticaChainNode[]>([])
  const [warnings, setWarnings] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const [fromId, setFromId] = useState(decision.id)
  const [toId, setToId] = useState<string | undefined>()
  const [ctype, setCtype] = useState<SemanticaCausalType>('CAUSED')
  const [adding, setAdding] = useState(false)

  const load = () => {
    setLoading(true)
    setErr(null)
    api
      .semanticaDecisionChain(decision.id)
      .then((r) => {
        setChain(r.chain ?? [])
        setWarnings(r.warnings ?? [])
      })
      .catch((e: any) => {
        setChain([])
        setWarnings([])
        setErr(e?.message ?? '决策链获取失败')
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [decision.id])

  const addCausal = async () => {
    if (!fromId.trim() || !toId) {
      showToast('请填写 from_id 并选择 to_id', 'err')
      return
    }
    setAdding(true)
    try {
      await api.semanticaAddCausal(fromId.trim(), toId, ctype)
      showToast('因果关系已写入')
      setToId(undefined)
      load()
      onChanged()
    } catch (e: any) {
      showToast(e?.message ?? '写入失败', 'err')
    } finally {
      setAdding(false)
    }
  }

  const toOptions = decisions
    .filter((d) => d.id && d.id !== fromId.trim())
    .map((d) => ({ value: d.id, label: `${d.category || '决策'} · ${d.id}` }))

  return (
    <Drawer
      open
      width={640}
      title={
        <Space size={8}>
          <BranchesOutlined />
          <span>决策链 · {decision.category || decision.id || '（无 id）'}</span>
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
        决策 id：<Typography.Text code style={{ fontSize: 12 }}>{decision.id || '（无 id）'}</Typography.Text>
      </Typography.Text>

      {err && <Alert type="error" showIcon style={{ marginTop: 10 }} message="决策链获取失败" description={err} />}
      {warnings.length > 0 && (
        <Alert
          type="warning"
          showIcon
          style={{ marginTop: 10 }}
          message="后端提示"
          description={
            <ul className="sema-warn-list">
              {warnings.map((w, i) => (
                <li key={i}>{w}</li>
              ))}
            </ul>
          }
        />
      )}

      <div className="sema-audit-chain">
        {loading && chain.length === 0 ? (
          <Spin size="small" />
        ) : chain.length === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            style={{ margin: '16px 0' }}
            description="暂无决策链（该决策尚无因果 / 溯源记录，可在下方添加）"
          />
        ) : (
          <Timeline
            items={chain.map((n, i) => ({
              key: `${n.id}-${i}`,
              color: i === 0 ? 'green' : 'blue',
              title: (
                <Space size={6} wrap>
                  <Typography.Text code style={{ fontSize: 12 }}>
                    {n.id || '（无 id）'}
                  </Typography.Text>
                  {n.relation && (
                    <Tag color="purple" style={{ margin: 0 }}>
                      {n.relation}
                    </Tag>
                  )}
                  {i === 0 && (
                    <Tag color="green" style={{ margin: 0 }}>
                      起点
                    </Tag>
                  )}
                </Space>
              ),
              content: (
                <div className="sema-audit-node">
                  {n.category && (
                    <div>
                      <span className="sema-audit-k">类别</span>
                      {n.category}
                    </div>
                  )}
                  {n.scenario && (
                    <div>
                      <span className="sema-audit-k">情境</span>
                      {n.scenario}
                    </div>
                  )}
                  {n.outcome && (
                    <div>
                      <span className="sema-audit-k">结论</span>
                      {n.outcome}
                    </div>
                  )}
                  <div>
                    <span className="sema-audit-k">置信度</span>
                    {confidenceText(n.confidence)}
                  </div>
                  {n.ts && (
                    <div>
                      <span className="sema-audit-k">时间</span>
                      {n.ts}
                    </div>
                  )}
                </div>
              ),
            }))}
          />
        )}
      </div>

      <div className="onto-sec">
        <span className="onto-sec-title">添加因果关系（因果链 / 先例）</span>
      </div>
      <div className="sema-causal-form">
        <div className="sema-causal-field">
          <span className="cfg-label">from_id</span>
          <Input value={fromId} onChange={(e) => setFromId(e.target.value)} placeholder="起点 id" />
        </div>
        <div className="sema-causal-field">
          <span className="cfg-label">to_id</span>
          <Select
            style={{ width: '100%' }}
            value={toId}
            onChange={setToId}
            placeholder="选择另一条决策"
            options={toOptions}
            showSearch
            optionFilterProp="label"
            notFoundContent="无其他决策"
          />
        </div>
        <div className="sema-causal-field">
          <span className="cfg-label">type</span>
          <Select style={{ width: '100%' }} value={ctype} onChange={setCtype} options={CAUSAL_OPTIONS} />
        </div>
        <Button type="primary" icon={<SaveOutlined />} loading={adding} onClick={addCausal}>
          添加因果关系
        </Button>
      </div>
    </Drawer>
  )
}

/** PROV-O 溯源抽屉：entity_id 查询 lineage（Descriptions 渲染）+ 导出 Turtle */
function LineageDrawer({ initialEntityId, onClose }: { initialEntityId: string; onClose: () => void }) {
  const { showToast } = useUI()
  const [entityId, setEntityId] = useState(initialEntityId)
  const [lineage, setLineage] = useState<SemanticaProvNode[]>([])
  const [warnings, setWarnings] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [exporting, setExporting] = useState(false)

  const run = (id: string) => {
    setLoading(true)
    setErr(null)
    api
      .semanticaLineage(id)
      .then((r) => {
        setLineage(r.lineage ?? [])
        setWarnings(r.warnings ?? [])
      })
      .catch((e: any) => {
        setLineage([])
        setWarnings([])
        setErr(e?.message ?? '溯源查询失败')
      })
      .finally(() => setLoading(false))
  }

  const query = () => {
    if (!entityId.trim()) {
      showToast('请输入 entity_id', 'err')
      return
    }
    run(entityId.trim())
  }

  // 打开时若带初始 id（行内「溯源」），自动查询
  useEffect(() => {
    if (initialEntityId.trim()) run(initialEntityId.trim())
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const doExport = async () => {
    setExporting(true)
    try {
      const text = await api.semanticaProvExport('turtle')
      downloadText('audit.ttl', text, 'text/turtle;charset=utf-8')
      showToast('已导出 PROV-O（audit.ttl）')
    } catch (e: any) {
      showToast(e?.message ?? '导出失败', 'err')
    } finally {
      setExporting(false)
    }
  }

  return (
    <Drawer
      open
      width={640}
      title={
        <Space size={8}>
          <HistoryOutlined />
          <span>PROV-O 溯源</span>
        </Space>
      }
      onClose={onClose}
      extra={
        <Button size="small" icon={<ExportOutlined />} loading={exporting} onClick={doExport}>
          导出 PROV-O (Turtle)
        </Button>
      }
    >
      <Space.Compact style={{ width: '100%' }}>
        <Input
          value={entityId}
          onChange={(e) => setEntityId(e.target.value)}
          onPressEnter={query}
          placeholder="entity_id（决策 / 实体 id）"
        />
        <Button type="primary" icon={<SearchOutlined />} loading={loading} onClick={query}>
          查询溯源
        </Button>
      </Space.Compact>
      <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 6 }}>
        输入决策 id 或实体 id，查看 PROV-O 溯源（事实 → 来源 → 推理路径）；导出为整图 PROV-O Turtle。
      </Typography.Text>

      {err && <Alert type="error" showIcon style={{ marginTop: 10 }} message="溯源查询失败" description={err} />}
      {warnings.length > 0 && (
        <Alert
          type="warning"
          showIcon
          style={{ marginTop: 10 }}
          message="后端提示"
          description={
            <ul className="sema-warn-list">
              {warnings.map((w, i) => (
                <li key={i}>{w}</li>
              ))}
            </ul>
          }
        />
      )}

      <div className="sema-lineage">
        {loading && lineage.length === 0 ? (
          <Spin size="small" />
        ) : lineage.length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} style={{ margin: '16px 0' }} description="暂无溯源记录" />
        ) : (
          lineage.map((n, i) => (
            <Descriptions
              key={`${n.id}-${i}`}
              className="sema-lineage-node"
              bordered
              size="small"
              column={1}
              title={
                <Typography.Text code style={{ fontSize: 12 }}>
                  {n.id || `节点 ${i + 1}`}
                </Typography.Text>
              }
              items={[
                { key: 'type', label: '类型', children: fmtAny(n.type) },
                { key: 'source', label: '来源', children: fmtAny(n.source) },
                {
                  key: 'metadata',
                  label: '元数据',
                  children: <span className="sema-lineage-meta">{fmtAny(n.metadata)}</span>,
                },
              ]}
            />
          ))
        )}
      </div>
    </Drawer>
  )
}
