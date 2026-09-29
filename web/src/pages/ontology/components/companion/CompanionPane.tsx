import { useCallback, useEffect, useState } from 'react'
import { Button, Card, Empty, Popconfirm, Segmented, Space, Spin, Table, Tag, Tooltip, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { DeleteOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { companionApi } from '../../../../api/companion'
import type { CompanionCandidate } from '../../../../api/companion'
import LoadErrorAlert from '../../../../components/LoadErrorAlert'

// ---------------------------------------------------------------------------
// REQ-170/M28 P2：伴生候选确认流面板（D-O19 第三来源「对话」边界——不入第五栏 KG 检索区）。
// REQ-195 重构：会话选择与成长图 3D 上提页级（CompanionPage 单源驱动）——本组件收
// convId/convs props，只保留「管线状态卡 + 候选三桶确认流 + 整体摘除」；confirm/reject/
// reset 后经 onGraphChanged 通知页级刷新成长图。此前组件内自带会话下拉与第二个成长图
// 入口，与页首图两套状态互不相通（上下重复/显示对象不可切换/入图后图不更新的根因）。
// ---------------------------------------------------------------------------

const KIND_META: Record<CompanionCandidate['kind'], { color: string; text: string }> = {
  concept: { color: 'blue', text: '概念' },
  relation: { color: 'purple', text: '关系' },
  event: { color: 'geekblue', text: '事件' },
}

export default function CompanionPane({
  convId,
  onChangedGraph,
}: {
  convId: string | undefined
  /** confirm/reject/reset 入图状态变化后通知页级刷新成长图（REQ-195） */
  onChangedGraph?: () => void
}) {
  const [status, setStatus] = useState<Awaited<ReturnType<typeof companionApi.status>> | null>(null)
  const [statusLoading, setStatusLoading] = useState(false)

  const [cands, setCands] = useState<CompanionCandidate[] | null>(null)
  const [candsLoading, setCandsLoading] = useState(false)
  const [candsErr, setCandsErr] = useState<string | null>(null)
  const [bucket, setBucket] = useState<'pending' | 'confirmed' | 'rejected'>('pending')
  const [deciding, setDeciding] = useState<string | null>(null)
  const [resetting, setResetting] = useState(false)
  const [actionErr, setActionErr] = useState<string | null>(null)

  const loadAll = useCallback((cid: string) => {
    setStatusLoading(true)
    setCandsLoading(true)
    companionApi
      .status(cid)
      .then(setStatus)
      .catch(() => setStatus(null))
      .finally(() => setStatusLoading(false))
    companionApi
      .listCandidates(cid)
      .then((ls) => {
        setCands(ls)
        setCandsErr(null)
      })
      .catch((e: any) => {
        setCands(null)
        setCandsErr(e?.message ?? '候选列表加载失败')
      })
      .finally(() => setCandsLoading(false))
  }, [])

  useEffect(() => {
    if (convId) loadAll(convId)
    else {
      setStatus(null)
      setCands(null)
    }
  }, [convId, loadAll])

  const decide = async (id: string, action: 'confirm' | 'reject') => {
    if (!convId) return
    setDeciding(id)
    setActionErr(null)
    try {
      await (action === 'confirm' ? companionApi.confirmCandidate(id) : companionApi.rejectCandidate(id))
      loadAll(convId)
      onChangedGraph?.()
    } catch (e: any) {
      setActionErr(e?.message ?? '操作失败')
    } finally {
      setDeciding(null)
    }
  }

  const doReset = async () => {
    if (!convId) return
    setResetting(true)
    setActionErr(null)
    try {
      await companionApi.resetConversation(convId)
      loadAll(convId)
      onChangedGraph?.()
    } catch (e: any) {
      setActionErr(e?.message ?? '摘除失败')
    } finally {
      setResetting(false)
    }
  }

  const rows = (cands ?? []).filter((c) => c.status === bucket)

  const columns: ColumnsType<CompanionCandidate> = [
    {
      title: '类型',
      dataIndex: 'kind',
      width: 72,
      render: (k: CompanionCandidate['kind']) => <Tag color={KIND_META[k]?.color ?? 'default'} style={{ margin: 0 }}>{KIND_META[k]?.text ?? k}</Tag>,
    },
    {
      title: '内容',
      ellipsis: true,
      render: (_, r) =>
        r.kind === 'relation' ? (
          <Typography.Text>
            {r.name} <Tag style={{ margin: 0 }}>{r.rel_name}</Tag> {r.rel_target}
          </Typography.Text>
        ) : (
          <Typography.Text>{r.name}</Typography.Text>
        ),
    },
    { title: '说明', dataIndex: 'definition', ellipsis: true, render: (v) => v || '—' },
    {
      title: '置信',
      dataIndex: 'confidence',
      width: 76,
      render: (v: number) => <Tag color={v >= 0.7 ? 'green' : v >= 0.4 ? 'orange' : 'default'} style={{ margin: 0 }}>{v ? v.toFixed(2) : '—'}</Tag>,
    },
    {
      title: '来源（原文锚点）',
      dataIndex: 'source_excerpt',
      ellipsis: true,
      render: (v, r) =>
        v ? (
          <Tooltip title={`消息 ${r.source_message_id}`}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>{v}</Typography.Text>
          </Tooltip>
        ) : (
          '—'
        ),
    },
    {
      title: '操作',
      width: 150,
      render: (_, r) =>
        r.status === 'pending' ? (
          <Space size={4}>
            <Button size="small" type="primary" ghost loading={deciding === r.id} onClick={() => decide(r.id, 'confirm')}>
              确认入图
            </Button>
            <Button size="small" danger loading={deciding === r.id} onClick={() => decide(r.id, 'reject')}>
              拒绝
            </Button>
          </Space>
        ) : (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {r.status === 'confirmed' ? '已入图' : '已拒绝'}
          </Typography.Text>
        ),
    },
  ]

  return (
    <div>
      {!convId ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="先在上方选择伴生对象（会话）" />
      ) : (
        <>
          <Card size="small" className="work-card" style={{ marginBottom: 12 }}>
            {statusLoading ? (
              <Spin size="small" />
            ) : status ? (
              <Space size={16} wrap>
                <Tag color={status.engine_running ? 'green' : 'default'} style={{ margin: 0 }}>
                  伴生引擎{status.engine_running ? '运行中' : '未启动'}
                </Tag>
                {status.engine_running && status.engine_endpoint && (
                  <Tooltip title={`端点 ${status.engine_endpoint}${status.engine_detail?.binary ? ` ｜ 二进制 ${status.engine_detail.binary} ｜ 数据 ${status.engine_detail.data_dir}` : ''}`}>
                    <Tag style={{ margin: 0 }}>{status.engine_endpoint.replace(/^https?:\/\/[^/]+/, '')}</Tag>
                  </Tooltip>
                )}
                <Tag color="blue" style={{ margin: 0 }}>待确认 {status.pending_count}</Tag>
                <Tooltip title={`游标位置：消息 ${status.cursor?.last_message_id || '—'}`}>
                  <Tag style={{ margin: 0 }}>已抽取至游标 {status.cursor?.last_message_id ? status.cursor.last_message_id.slice(0, 8) + '…' : '—'}</Tag>
                </Tooltip>
                {(status.labels ?? []).slice(0, 12).map((l) => (
                  <Tag key={l} color="geekblue" style={{ margin: 0 }} icon={<ThunderboltOutlined />}>{l}</Tag>
                ))}
                {(status.labels ?? []).length > 12 && <Typography.Text type="secondary" style={{ fontSize: 12 }}>…共 {(status.labels ?? []).length} 个实体</Typography.Text>}
                {(status.labels ?? []).length === 0 && <Typography.Text type="secondary" style={{ fontSize: 12 }}>图内暂无实体（确认候选后生成）</Typography.Text>}
              </Space>
            ) : (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>伴生管线状态不可用（后端未升级到 M28 或引擎未启动；对话收尾抽取后自动就绪）</Typography.Text>
            )}
          </Card>

          <div className="onto-sec">
            <span className="onto-sec-title">候选（人工确认 = 入图门控，REQ-82 草稿必审）</span>
            <span className="hit-spacer" />
            <Segmented
              size="small"
              value={bucket}
              onChange={(v) => setBucket(v as 'pending' | 'confirmed' | 'rejected')}
              options={[
                { value: 'pending', label: '待确认' },
                { value: 'confirmed', label: '已入图' },
                { value: 'rejected', label: '已拒绝' },
              ]}
            />
            <Popconfirm
              title={`摘除该会话伴生图？`}
              description="DROP 会话图 + 清空候选与游标（产物整体摘除，低侵入三原则③）；对话本身不受影响。"
              okText="摘除"
              okButtonProps={{ danger: true }}
              cancelText="取消"
              onConfirm={doReset}
            >
              <Button size="small" danger icon={<DeleteOutlined />} loading={resetting}>
                整体摘除
              </Button>
            </Popconfirm>
          </div>

          {actionErr && <LoadErrorAlert title="伴生操作失败" message={actionErr} onRetry={() => setActionErr(null)} style={{ marginBottom: 12 }} />}

          {candsErr ? (
            <LoadErrorAlert title="候选列表加载失败" message={candsErr} onRetry={() => convId && loadAll(convId)} />
          ) : candsLoading ? (
            <div style={{ padding: '16px 0' }}>
              <Spin />
            </div>
          ) : rows.length === 0 ? (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={
                bucket === 'pending'
                  ? '暂无待确认候选——在 Agent 配置开启「伴生本体」并对话一轮后，收尾自动抽取'
                  : '该分组暂无候选'
              }
            />
          ) : (
            <Table<CompanionCandidate>
              rowKey="id"
              columns={columns}
              dataSource={rows}
              size="small"
              pagination={{ pageSize: 10, hideOnSinglePage: true, showTotal: (n) => `共 ${n} 条` }}
              scroll={{ x: 'max-content' }}
            />
          )}
        </>
      )}
    </div>
  )
}
