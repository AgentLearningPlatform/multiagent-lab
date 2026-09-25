import { useEffect, useState } from 'react'
import { Alert, Button, Card, Drawer, Empty, Form, Input, Select, Skeleton, Space, Table, Tag, Timeline, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { BranchesOutlined, ExportOutlined, ReloadOutlined, SaveOutlined } from '@ant-design/icons'
import { api, ApiError } from '../../../../api/client'
import type { OntoDecision } from '../../../../api/types'
import { useUI } from '../../../../store/ui'

// ---------------------------------------------------------------------------
// 消费与审计 · 决策审计页签（SQLite 决策表 + derived_from 溯源链 + PROV-O 导出）。
// A4（REQ-145/M22）：决策表分页统一 pageSize 10；溯源链抽屉拆入本文件。
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

export default function AuditDecisionTab({ kbId }: { kbId?: string }) {
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
      fixed: 'right' as const,
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
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
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
          <Skeleton active title={false} paragraph={{ rows: 5 }} />
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
