import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import {
  Alert,
  Badge,
  Button,
  Card,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Result,
  Select,
  Space,
  Splitter,
  Table,
  Tabs,
  Tag,
  Tooltip,
  Typography,
  Upload,
} from 'antd'
import type { BadgeProps } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PlusOutlined, SearchOutlined, UploadOutlined } from '@ant-design/icons'
import { api } from '../api/client'
import type { KBDoc, KBHit, KnowledgeBase } from '../api/types'
import { useUI } from '../store/ui'

/** 文档索引状态 → antd Badge 状态（后端未知状态优雅回退） */
const DOC_STATUS: Record<string, { status: BadgeProps['status']; text: string }> = {
  pending: { status: 'default', text: '待索引' },
  indexing: { status: 'processing', text: '索引中' },
  success: { status: 'success', text: '成功' },
  failed: { status: 'error', text: '失败' },
}
function docStatusOf(s?: string): { status: BadgeProps['status']; text: string } {
  return DOC_STATUS[s ?? ''] ?? { status: 'default', text: s || '未知' }
}

/** 相似度格式化（容忍字符串 / 缺失） */
function fmtScore(v: unknown): string {
  const n = Number(v)
  return Number.isFinite(n) ? n.toFixed(3) : '—'
}

/** KB 双子模块（M14 D-KB4）：rag | graphrag（老数据缺省 = rag） */
type KBMode = 'rag' | 'graphrag'
const modeOf = (k?: KnowledgeBase | null): KBMode => (k?.mode === 'graphrag' ? 'graphrag' : 'rag')
const MODE_TAG: Record<KBMode, { color: string; text: string }> = {
  rag: { color: 'blue', text: 'RAG' },
  graphrag: { color: 'purple', text: 'GraphRAG' },
}

/**
 * 知识库视图（原型 06 §3.4 / 02 文档 §10）：
 * 左栏库列表（名称 / 文档·chunk 计数）→ 右栏库详情
 * （文档表 + 上传入口 + 检索试运行（索引未就绪禁用）+ TopK / min_score 配置）。
 */
export default function KnowledgePage() {
  const { showToast, bumpData } = useUI()
  const [kbs, setKbs] = useState<KnowledgeBase[]>([])
  const [loadErr, setLoadErr] = useState<string | null>(null)
  const [activeId, setActiveId] = useState<string | null>(null)

  const [docs, setDocs] = useState<KBDoc[]>([])
  const [docsLoading, setDocsLoading] = useState(false)
  const [reindexing, setReindexing] = useState<string | null>(null)

  const [createOpen, setCreateOpen] = useState(false)
  const [uploadOpen, setUploadOpen] = useState(false)

  const [modeTab, setModeTab] = useState<KBMode>('rag') // M14 ⑤：双子页签

  const [query, setQuery] = useState('')
  const [topK, setTopK] = useState<number | null>(4)
  const [minScore, setMinScore] = useState<number | null>(0)
  const [searching, setSearching] = useState(false)
  const [hits, setHits] = useState<KBHit[] | null>(null)
  const [searchMeta, setSearchMeta] = useState<{ mode?: string; degraded?: boolean; error?: string } | null>(null)
  const [savingCfg, setSavingCfg] = useState(false)

  const active = useMemo(() => kbs.find((k) => k.id === activeId) ?? null, [kbs, activeId])
  const hasReady = docs.some((d) => d.status === 'success')

  const reloadKBs = () => {
    api.listKBs()
      .then((ks) => {
        setKbs(ks)
        setLoadErr(null)
        setActiveId((cur) => (cur && ks.some((k) => k.id === cur) ? cur : (ks[0]?.id ?? null)))
      })
      .catch((e: any) => {
        setKbs([])
        setActiveId(null)
        setLoadErr(e?.message ?? '加载失败')
      })
  }

  const reloadDocs = (kbId: string) => {
    setDocsLoading(true)
    api.listKBDocs(kbId)
      .then(setDocs)
      .catch(() => setDocs([]))
      .finally(() => setDocsLoading(false))
  }

  useEffect(reloadKBs, [])

  // M14 ⑤：双子页签切换 → 选中该模式下的第一个库（当前库不属该模式时）
  useEffect(() => {
    const inTab = kbs.filter((k) => modeOf(k) === modeTab)
    if (active && modeOf(active) !== modeTab) {
      setActiveId(inTab[0]?.id ?? null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modeTab])

  // 切换库：重置检索态，载入文档，并以库当前检索参数预填配置
  useEffect(() => {
    setQuery('')
    setHits(null)
    if (!activeId) {
      setDocs([])
      setTopK(4)
      setMinScore(0)
      return
    }
    const kb = kbs.find((k) => k.id === activeId)
    setTopK(kb?.top_k ?? 4)
    setMinScore(kb?.min_score ?? 0)
    reloadDocs(activeId)
    // 仅在选中库变化时执行；kbs 仅用于取当前库参数
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeId])

  const saveConfig = async () => {
    if (!active) return
    setSavingCfg(true)
    try {
      await api.updateKB(active.id, { ...active, top_k: topK ?? active.top_k, min_score: minScore ?? active.min_score })
      showToast('检索参数已保存')
      bumpData()
      reloadKBs()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setSavingCfg(false)
    }
  }

  const doSearch = async () => {
    if (!active) return
    const q = query.trim()
    if (!q) {
      showToast('请输入检索词', 'err')
      return
    }
    setSearching(true)
    try {
      const r = await api.searchPreview(active.id, q, topK ?? undefined, minScore ?? undefined)
      setSearchMeta({ mode: r.mode, degraded: r.degraded, error: r.error })
      setHits(r.hits ?? [])
    } catch (e: any) {
      showToast(e.message, 'err')
      setHits(null)
    } finally {
      setSearching(false)
    }
  }

  const graphragSearch = async () => {
    if (!active) return
    const q = query.trim()
    if (!q) {
      showToast('请输入检索词', 'err')
      return
    }
    setSearching(true)
    try {
      const r = await api.graphragSearchKB(active.id, q, topK ?? undefined)
      setSearchMeta({ mode: r.mode, degraded: r.degraded, error: r.error })
      setHits(r.hits ?? [])
      if (r.degraded) showToast('semantica worker 不可达，GraphRAG 检索降级', 'err')
    } catch (e: any) {
      showToast(e.message, 'err')
      setHits(null)
    } finally {
      setSearching(false)
    }
  }

  const reindex = async (doc: KBDoc) => {
    if (!active) return
    setReindexing(doc.id)
    try {
      await api.reindexKBDoc(active.id, doc.id)
      showToast('已提交重建索引')
      reloadDocs(active.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setReindexing(null)
    }
  }

  const removeDoc = async (doc: KBDoc) => {
    if (!active) return
    try {
      await api.deleteKBDoc(active.id, doc.id)
      showToast('已删除文档')
      reloadDocs(active.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  const removeKB = async () => {
    if (!active) return
    try {
      await api.deleteKB(active.id)
      showToast('已删除知识库')
      bumpData()
      reloadKBs()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  const columns: ColumnsType<KBDoc> = [
    {
      title: '文档',
      dataIndex: 'title',
      ellipsis: true,
      render: (v: string) => <Typography.Text strong>{v}</Typography.Text>,
    },
    {
      title: 'Chunks',
      dataIndex: 'chunk_count',
      width: 90,
      align: 'right',
      render: (v?: number) => (v ?? '—'),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 120,
      render: (_, d: KBDoc) => {
        const st = docStatusOf(d.status)
        const badge = <Badge status={st.status} text={st.text} />
        const tip = [d.status === 'failed' ? d.error : '', d.graphrag?.degraded ? `KG 抽取降级：${d.graphrag.error ?? 'worker 不可达'}` : ''].filter(Boolean).join('；')
        const withGr = d.graphrag && (
          <span style={{ marginInlineStart: 6 }}>
            {d.graphrag.degraded ? (
              <Tag color="warning" style={{ margin: 0, fontSize: 11 }}>KG 降级</Tag>
            ) : (
              <Tooltip title={`KG 抽取完成（${d.graphrag.method ?? 'semantica'}：实体 ${d.graphrag.entities ?? 0} / 关系 ${d.graphrag.relationships ?? 0}）`}>
                <Tag color="purple" style={{ margin: 0, fontSize: 11 }}>KG ✓</Tag>
              </Tooltip>
            )}
          </span>
        )
        return (
          <span>
            {tip ? <Tooltip title={tip}>{badge}</Tooltip> : badge}
            {withGr}
          </span>
        )
      },
    },
    {
      title: '操作',
      key: 'ops',
      width: 180,
      render: (_, d: KBDoc) => (
        <Space size={0} wrap>
          <Button type="link" size="small" loading={reindexing === d.id} onClick={() => reindex(d)}>
            重建索引
          </Button>
          <Popconfirm
            title={`删除文档「${d.title}」？`}
            description="将级联删除其全部 chunk 与向量。"
            okText="删除"
            okButtonProps={{ danger: true }}
            cancelText="取消"
            onConfirm={() => removeDoc(d)}
          >
            <Button type="link" size="small" danger>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <Splitter
      className="main sidebar-splitter"
      onResizeEnd={(sizes) => localStorage.setItem('eino.sidebar.width', String(Math.round(sizes[0])))}
    >
      <Splitter.Panel defaultSize={Number(localStorage.getItem('eino.sidebar.width')) || 280} min={220} max={480} className="sidebar-panel">
        <aside className="sidebar">
          <div className="side-head">
            <span className="side-title">知识库</span>
            <span className="side-count">{kbs.length}</span>
          </div>
          <Tabs
            size="small"
            activeKey={modeTab}
            onChange={(k) => setModeTab(k as KBMode)}
            style={{ margin: '0 12px' }}
            items={[
              { key: 'rag', label: `RAG ${kbs.filter((k) => modeOf(k) === 'rag').length}` },
              { key: 'graphrag', label: `GraphRAG ${kbs.filter((k) => modeOf(k) === 'graphrag').length}` },
            ]}
          />
          <div className="side-actions">
            <Button type="primary" block icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
              新建库
            </Button>
          </div>
          <div className="side-list">
            {kbs.filter((k) => modeOf(k) === modeTab).map((k) => (
              <div key={k.id} className={`side-item${k.id === activeId ? ' active' : ''}`} onClick={() => setActiveId(k.id)}>
                <div className="side-item-top">
                  <span className="side-item-name" title={k.name}>
                    {k.name}
                  </span>
                </div>
                <div className="side-item-meta">
                  <Tag color={MODE_TAG[modeOf(k)].color} style={{ margin: 0, fontSize: 11, lineHeight: '16px' }}>
                    {MODE_TAG[modeOf(k)].text}
                  </Tag>
                  <span>文档 {k.doc_count ?? '—'}</span>
                  <span className="dot">·</span>
                  <span>chunks {k.chunk_count ?? '—'}</span>
                </div>
              </div>
            ))}
            {kbs.length === 0 && <div className="empty-hint">{loadErr ? '知识库接口未就绪' : '暂无知识库，点击上方新建'}</div>}
          </div>
        </aside>
      </Splitter.Panel>
      <Splitter.Panel className="content-panel">

        <div className="work-main">
          {loadErr ? (
            <div className="work-empty">
              <Result
                status="warning"
                title="知识库后端未就绪"
                subTitle={`${loadErr}（M6 后端另行部署）`}
                extra={<Button onClick={reloadKBs}>重试</Button>}
              />
            </div>
          ) : !active ? (
            <div className="work-empty">
              <Result
                icon={null}
                title="选择左侧知识库查看详情"
                subTitle="或点击「新建库」：上传 txt / md → 切分 → 向量化 → 检索试运行（对齐 02 文档 §6.9）。"
              />
            </div>
          ) : (
            <>
              <div className="work-head">
                <div className="work-head-text">
                  <div className="work-head-title">
                    <Typography.Title level={4} style={{ margin: 0 }}>
                      {active.name}
                      <Tag color={MODE_TAG[modeOf(active)].color} style={{ marginInlineStart: 8, verticalAlign: 'middle' }}>
                        {MODE_TAG[modeOf(active)].text}
                      </Tag>
                    </Typography.Title>
                  </div>
                  <p className="work-head-desc">{active.description || '未填写描述'}</p>
                </div>
                <Popconfirm
                  title={`删除知识库「${active.name}」？`}
                  description="将删除其全部文档、chunk 与向量数据。"
                  okText="删除"
                  okButtonProps={{ danger: true }}
                  cancelText="取消"
                  onConfirm={removeKB}
                >
                  <Button danger>删除库</Button>
                </Popconfirm>
              </div>

              {modeOf(active) === 'graphrag' && (
                <Alert
                  type="info"
                  showIcon
                  style={{ marginBottom: 12 }}
                  message="GraphRAG 子模块（M14）"
                  description="文档索引后自动把 chunks 同步抽取为 KG（semantica worker :8093 /graphrag/ingest）；检索优先 GraphRAG，worker 不可达自动回退向量检索（不阻断）。注意：KG 抽取与 embedding 是两套独立模型，chunk 切分质量直接影响抽取输入。"
                />
              )}
              <div className="stat-strip">
                <StatTile k="文档" v={docs.length} />
                <StatTile k="Chunks" v={docs.reduce((s, d) => s + (d.chunk_count ?? 0), 0)} />
                <StatTile k="TopK" v={active.top_k ?? '—'} />
                <StatTile k="min_score" v={active.min_score ?? '—'} />
              </div>

              <Card
                className="work-card"
                size="small"
                title="检索参数"
                extra={<Typography.Text type="secondary" style={{ fontSize: 12 }}>保存后用于对话召回（§6.9）</Typography.Text>}
              >
                <div className="cfg-row">
                  <label className="cfg-field">
                    <span className="cfg-label">TopK（返回片段数）</span>
                    <InputNumber
                      min={1}
                      max={20}
                      value={topK}
                      onChange={(v) => setTopK(typeof v === 'number' ? v : null)}
                      style={{ width: 140 }}
                    />
                  </label>
                  <label className="cfg-field">
                    <span className="cfg-label">min_score（相似度下限 0~1）</span>
                    <InputNumber
                      min={0}
                      max={1}
                      step={0.05}
                      precision={2}
                      value={minScore}
                      onChange={(v) => setMinScore(typeof v === 'number' ? v : null)}
                      style={{ width: 160 }}
                    />
                  </label>
                  <Button type="primary" loading={savingCfg} onClick={saveConfig}>
                    保存
                  </Button>
                </div>
              </Card>

              <Card
                className="work-card"
                size="small"
                title={`文档（${docs.length}）`}
                extra={
                  <Button type="primary" icon={<PlusOutlined />} onClick={() => setUploadOpen(true)}>
                    上传文档
                  </Button>
                }
              >
                <Table<KBDoc>
                  rowKey="id"
                  columns={columns}
                  dataSource={docs}
                  loading={docsLoading}
                  pagination={false}
                  size="middle"
                  locale={{ emptyText: '暂无文档，点击右上「上传文档」导入 txt / md' }}
                />
              </Card>

              <Card
                className="work-card"
                size="small"
                title="检索试运行"
                extra={
                  hasReady ? (
                    <Space size={6}>
                      {modeOf(active) === 'graphrag' && (
                        <Button size="small" loading={searching} onClick={graphragSearch}>
                          GraphRAG 直查
                        </Button>
                      )}
                      {searchMeta?.mode && <Tag color={searchMeta.mode === 'graphrag' ? 'purple' : 'blue'} style={{ margin: 0 }}>{searchMeta.mode}</Tag>}
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        命中按相似度排序
                      </Typography.Text>
                    </Space>
                  ) : (
                    <Tag color="warning" style={{ margin: 0 }}>
                      索引未就绪，暂不可检索
                    </Tag>
                  )
                }
              >
                <div className="search-row">
                  <Input
                    allowClear
                    value={query}
                    disabled={!hasReady}
                    placeholder={hasReady ? '输入检索词，回车试运行' : '需至少一篇「就绪」文档'}
                    onChange={(e) => setQuery(e.target.value)}
                    onPressEnter={doSearch}
                    style={{ maxWidth: 420 }}
                  />
                  <InputNumber
                    min={1}
                    max={20}
                    value={topK}
                    disabled={!hasReady}
                    onChange={(v) => setTopK(typeof v === 'number' ? v : null)}
                    style={{ width: 128 }}
                    prefix={<Typography.Text type="secondary" style={{ fontSize: 12 }}>TopK</Typography.Text>}
                  />
                  <Button type="primary" icon={<SearchOutlined />} disabled={!hasReady} loading={searching} onClick={doSearch}>
                    检索
                  </Button>
                </div>

                {searchMeta?.degraded && (
                  <Alert
                    type="warning"
                    showIcon
                    style={{ marginTop: 12 }}
                    message="GraphRAG worker 不可达，本次结果来自向量检索回退（降级不阻断）"
                    description={searchMeta.error}
                  />
                )}
                {hits && hits.length === 0 && (
                  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="无命中（可尝试降低 min_score 或补充文档）" style={{ marginTop: 16 }} />
                )}
                {hits && hits.length > 0 && (
                  <div className="hits">
                    {hits.map((h, i) => {
                      const score = Number(h.score)
                      const band = score >= 0.8 ? 'hi' : score >= 0.5 ? 'mid' : 'lo'
                      return (
                        <div className={`hit ${band}`} key={`${h.doc}-${h.seq}-${i}`}>
                          <div className="hit-top">
                            <span className="hit-doc">{h.doc}</span>
                            <span className="hit-seq">#{h.seq}</span>
                            <span className="hit-spacer" />
                            <Tag className="hit-score" color={band === 'hi' ? 'green' : band === 'mid' ? 'blue' : 'gold'} style={{ margin: 0 }}>
                              score {fmtScore(h.score)}
                            </Tag>
                          </div>
                          <div className="hit-excerpt">{h.excerpt}</div>
                        </div>
                      )
                    })}
                  </div>
                )}
              </Card>
            </>
          )}
        </div>

        {createOpen && (
          <CreateKBModal
            onClose={() => setCreateOpen(false)}
            onCreated={(kb) => {
              setCreateOpen(false)
              bumpData()
              setActiveId(kb.id)
              reloadKBs()
            }}
          />
        )}
        {uploadOpen && active && (
          <UploadDocModal
            kb={active}
            onClose={() => setUploadOpen(false)}
            onUploaded={() => {
              setUploadOpen(false)
              reloadDocs(active.id)
              reloadKBs()
            }}
          />
        )}
      </Splitter.Panel>
    </Splitter>
  )
}

function StatTile({ k, v }: { k: string; v: ReactNode }) {
  return (
    <div className="stat-tile">
      <span className="k">{k}</span>
      <span className="v">{v}</span>
    </div>
  )
}

/** 新建知识库：name / description / store_backend（后端无状态字段，索引状态在文档级） */
function CreateKBModal({ onClose, onCreated }: { onClose: () => void; onCreated: (kb: KnowledgeBase) => void }) {
  const { showToast } = useUI()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)

  const mode = Form.useWatch('mode', form) ?? 'rag'
  const save = async () => {
    let v: any
    try {
      v = await form.validateFields()
    } catch {
      return
    }
    setBusy(true)
    try {
      const kb = await api.createKB({ name: v.name, description: v.description ?? '', mode: v.mode, store_backend: v.store_backend })
      showToast('知识库已创建')
      onCreated(kb)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      centered
      title="新建知识库"
      width={520}
      onCancel={onClose}
      footer={
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={busy} onClick={save}>
            创建
          </Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical" requiredMark={false} initialValues={{ store_backend: 'qdrant', description: '', mode: 'rag' }}>
        <Form.Item name="name" label="名称" rules={[{ required: true, message: '名称必填' }]}>
          <Input placeholder="如：K8s 运维手册" maxLength={60} />
        </Form.Item>
        <Form.Item
          name="mode"
          label="子模块模式（M14 D-KB4）"
          extra={
            mode === 'graphrag'
              ? 'GraphRAG：chunks 额外抽取为 KG（依赖 semantica worker :8093）；KG 抽取与 embedding 是两套独立模型，切分质量影响抽取输入；worker 不可达自动回退向量检索。'
              : 'RAG：向量检索（默认）。GraphRAG 模式额外构建 KG，适合关系型问答。'
          }
        >
          <Select
            options={[
              { value: 'rag', label: 'RAG（向量检索）' },
              { value: 'graphrag', label: 'GraphRAG（KG + 向量，需 semantica worker）' },
            ]}
          />
        </Form.Item>
        <Form.Item name="description" label="描述">
          <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} placeholder="用途说明（可选）" />
        </Form.Item>
        <Form.Item
          name="store_backend"
          label="向量后端"
          extra="qdrant：eino-ext 适配 / 直连 REST（P1 首选）；sqlite：无外部依赖对照（fallback）。创建后不可切换。"
        >
          <Select
            options={[
              { value: 'qdrant', label: 'qdrant' },
              { value: 'sqlite', label: 'sqlite' },
            ]}
          />
        </Form.Item>
      </Form>
    </Modal>
  )
}

/** 上传文档：粘贴文本，或选择本地 txt / md 读取文本填入（仅读取文本，不传文件本体） */
function UploadDocModal({ kb, onClose, onUploaded }: { kb: KnowledgeBase; onClose: () => void; onUploaded: () => void }) {
  const { showToast } = useUI()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)

  const save = async () => {
    let v: any
    try {
      v = await form.validateFields()
    } catch {
      return
    }
    if (!String(v.content ?? '').trim()) {
      showToast('文档内容不能为空', 'err')
      return
    }
    setBusy(true)
    try {
      await api.uploadKBDoc(kb.id, { name: v.name, content: v.content })
      showToast('已上传，索引在后台进行')
      onUploaded()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      centered
      title={`上传文档 · ${kb.name}`}
      width={620}
      onCancel={onClose}
      footer={
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={busy} onClick={save}>
            上传
          </Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        <Form.Item label="从文件读取（txt / md）" extra="仅在浏览器本地读取文本填入下方，不上传文件本体。">
          <Upload
            accept=".txt,.md,text/plain,text/markdown"
            maxCount={1}
            showUploadList={false}
            beforeUpload={(file) => {
              file
                .text()
                .then((t) => {
                  form.setFieldsValue({ content: t, name: form.getFieldValue('name') || file.name })
                })
                .catch(() => showToast('读取文件失败', 'err'))
              return false
            }}
          >
            <Button icon={<UploadOutlined />}>选择文件</Button>
          </Upload>
        </Form.Item>
        <Form.Item name="name" label="文档名" rules={[{ required: true, message: '文档名必填' }]}>
          <Input placeholder="如：deployment-guide.md" maxLength={120} />
        </Form.Item>
        <Form.Item name="content" label="内容" rules={[{ required: true, message: '内容必填' }]}>
          <Input.TextArea autoSize={{ minRows: 8, maxRows: 18 }} placeholder="粘贴文本，或从上方选择 txt / md 文件自动填入" />
        </Form.Item>
      </Form>
    </Modal>
  )
}
