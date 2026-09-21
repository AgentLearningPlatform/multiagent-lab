import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
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
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ExportOutlined, ReloadOutlined, SaveOutlined, SearchOutlined } from '@ant-design/icons'
import { api, ApiError } from '../api/client'
import type {
  Ontology,
  SemanticaClaim,
  SemanticaDecision,
  SemanticaDecisionInput,
  SemanticaHealth,
  SemanticaIngestResult,
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
            { key: 'audit', label: '决策审计', children: <AuditTab onMutated={loadHealth} /> },
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
// 决策审计（轻量版，REQ-101 完整 PROV-O 链为 P2）
// ---------------------------------------------------------------------------

const DECISION_COLUMNS: ColumnsType<SemanticaDecision> = [
  { title: '时间', dataIndex: 'ts', width: 175, render: (v) => v || '—' },
  { title: '类别', dataIndex: 'category', width: 130, render: (v) => v || '—' },
  { title: '情境', dataIndex: 'scenario', ellipsis: true, render: (v) => v || '—' },
  { title: '结论', dataIndex: 'outcome', ellipsis: true, render: (v) => v || '—' },
  {
    title: '置信度',
    dataIndex: 'confidence',
    width: 100,
    render: (v) => {
      const n = typeof v === 'number' ? v : typeof v === 'string' && v !== '' ? Number(v) : NaN
      return Number.isFinite(n) ? n.toFixed(2) : '—'
    },
  },
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

function AuditTab({ onMutated }: { onMutated: () => void }) {
  const { showToast } = useUI()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)
  const [decisions, setDecisions] = useState<SemanticaDecision[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [lastId, setLastId] = useState<string | null>(null)

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

  return (
    <div className="sema-home">
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
          columns={DECISION_COLUMNS}
          dataSource={sorted}
          loading={loading}
          pagination={false}
          size="small"
          locale={{ emptyText: '暂无决策记录（在上方录入一次，或经挂载 semantica MCP 的对话产生）' }}
          scroll={{ x: 'max-content' }}
        />
      </Card>

      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">3</span>
            <span>PROV-O 审计链可视化（REQ-101 · P2）</span>
          </Space>
        }
      >
        <Alert
          type="info"
          showIcon
          message="完整 PROV-O 链可视化属 P2，复用 semantica 自带 Explorer UI，本页不自研替代"
          description={
            <>
              P1 提供决策录入与列表（上方）；P2 将反代 / iframe 嵌入 semantica Explorer，按 <Typography.Text code>decision_id</Typography.Text> 展开
              「事实 → 来源 → 推理路径」链路，并与 REQ-94 推理对照页互链，构成「推理可解释 + 决策可审计」教学闭环。
            </>
          }
        />
      </Card>
    </div>
  )
}
