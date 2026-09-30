import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Card, Empty, Popconfirm, Segmented, Space, Spin, Table, Tag, Tooltip, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { CheckOutlined, CloseOutlined, DeleteOutlined, ReloadOutlined } from '@ant-design/icons'
import { api } from '../../../../api/client'
import { companionApi } from '../../../../api/companion'
import type { CandidateGroup, CompanionCandidate } from '../../../../api/companion'
import type { Agent } from '../../../../api/types'
import { useUI } from '../../../../store/ui'
import LoadErrorAlert from '../../../../components/LoadErrorAlert'
import OntologyCompanionGraph from './OntologyCompanionGraph'

// ---------------------------------------------------------------------------
// REQ-216⑥：本体视角候选集中整理（资产详情页「伴生候选」页签）——绑定该本体的全部
// agent 候选跨 agent 铺平（与侧板「伴生管理」高频动线双入口同 API，数据一致零成本）。
// 「确认入图」发生在产物本体上，消除「管理匿名图」的脱节感；页签内含成长图（页首，
// confirm/reject 后联动刷新）与本体级「清空伴生图」（DROP 子图+清全部绑定 agent 候选游标）。
// ---------------------------------------------------------------------------

const KIND_META: Record<CompanionCandidate['kind'], { color: string; text: string }> = {
  concept: { color: 'blue', text: '概念' },
  relation: { color: 'purple', text: '关系' },
  event: { color: 'geekblue', text: '事件' },
}

export default function OntologyCompanionPane({ ontologyId }: { ontologyId: string }) {
  const { showToast } = useUI()

  const [agents, setAgents] = useState<Agent[]>([])
  const [convTitles, setConvTitles] = useState<Map<string, string>>(new Map())
  const [metaLoading, setMetaLoading] = useState(false)

  const [cands, setCands] = useState<CompanionCandidate[] | null>(null)
  const [candsLoading, setCandsLoading] = useState(false)
  const [candsErr, setCandsErr] = useState<string | null>(null)
  const [bucket, setBucket] = useState<'pending' | 'confirmed' | 'rejected'>('pending')
  const [deciding, setDeciding] = useState<string | null>(null)
  const [actionErr, setActionErr] = useState<string | null>(null)
  const [resetting, setResetting] = useState(false)
  const [graphTick, setGraphTick] = useState(0) // confirm/reject/reset 后刷新页首成长图
  // REQ-216 增量③：时间倒序 / 按实体双视图（本体视角批量审阅——REQ-216⑥「+批量」范围补齐）
  const [view, setView] = useState<'time' | 'entity'>('time')
  const [groups, setGroups] = useState<CandidateGroup[] | null>(null)
  const [batchBusy, setBatchBusy] = useState<string | null>(null)
  const [batchResult, setBatchResult] = useState<string | null>(null)

  const loadMeta = useCallback(() => {
    setMetaLoading(true)
    Promise.all([
      companionApi.listOntologyAgents(ontologyId).catch(() => [] as Agent[]),
      api.listConversations({ scope: 'agent' }).catch(() => []),
      api.listConversations({ scope: 'project' }).catch(() => []),
    ])
      .then(([ags, agentConvs, projectConvs]) => {
        setAgents(ags)
        const m = new Map<string, string>()
        for (const c of [...agentConvs, ...projectConvs]) {
          m.set(c.id, c.scope === 'project' ? `${c.title || c.id}（项目）` : c.title || c.id)
        }
        setConvTitles(m)
      })
      .finally(() => setMetaLoading(false))
  }, [ontologyId])

  const loadCands = useCallback(() => {
    setCandsLoading(true)
    companionApi
      .listCandidatesByOntology(ontologyId)
      .then((ls) => {
        setCands(ls)
        setCandsErr(null)
      })
      .catch((e: any) => {
        setCands(null)
        setCandsErr(e?.message ?? '候选加载失败')
      })
      .finally(() => setCandsLoading(false))
  }, [ontologyId])

  useEffect(() => {
    loadMeta()
    loadCands()
  }, [loadMeta, loadCands])

  // REQ-216 增量③：按实体视图数据（桶切换/视图切换即重取；切换即清批量结果消息）
  useEffect(() => {
    setBatchResult(null)
    if (view !== 'entity') return
    setCandsLoading(true)
    companionApi
      .listCandidatesGroupedByOntology(ontologyId, bucket)
      .then((r) => {
        setGroups(r.groups ?? [])
        setCandsErr(null)
      })
      .catch((e: any) => {
        setGroups(null)
        setCandsErr(e?.message ?? '候选加载失败')
      })
      .finally(() => setCandsLoading(false))
  }, [view, bucket, ontologyId])

  // 批量裁决：循环单候选端点，结果如实计数（不做硬事务，诚实原则；同侧板 agent 视角口径）
  const decideGroup = async (g: CandidateGroup, action: 'confirm' | 'reject') => {
    setBatchBusy(g.key)
    setActionErr(null)
    setBatchResult(null)
    let ok = 0
    let fail = 0
    const lastErr: string[] = []
    for (const m of g.members.filter((x) => x.status === 'pending')) {
      try {
        await (action === 'confirm' ? companionApi.confirmCandidate(m.id) : companionApi.rejectCandidate(m.id))
        ok++
      } catch (e: any) {
        fail++
        if (lastErr.length < 2) lastErr.push(e?.message ?? '失败')
      }
    }
    setBatchBusy(null)
    if (fail > 0) setActionErr(`批量${action === 'confirm' ? '入图' : '拒绝'}部分失败：成功 ${ok} · 失败 ${fail}${lastErr[0] ? `（${lastErr[0]}）` : ''}`)
    else setBatchResult(`已${action === 'confirm' ? '入图' : '拒绝'} ${ok} 条（${g.entity}）`)
    if (ok + fail > 0) {
      loadCands()
      setGraphTick((t) => t + 1)
    }
  }

  const decide = async (id: string, action: 'confirm' | 'reject') => {
    setDeciding(id)
    setActionErr(null)
    try {
      await (action === 'confirm' ? companionApi.confirmCandidate(id) : companionApi.rejectCandidate(id))
      loadCands()
      setGraphTick((t) => t + 1)
    } catch (e: any) {
      setActionErr(e?.message ?? '操作失败')
    } finally {
      setDeciding(null)
    }
  }

  const doResetGraph = async () => {
    setResetting(true)
    setActionErr(null)
    try {
      await companionApi.resetOntology(ontologyId)
      showToast('已清空该本体的伴生图（绑定智能体候选与游标同步清空；本体资产不受影响）')
      loadCands()
      setGraphTick((t) => t + 1)
    } catch (e: any) {
      setActionErr(e?.message ?? '清空失败')
    } finally {
      setResetting(false)
    }
  }

  const agentName = useMemo(() => new Map(agents.map((a) => [a.id, a.name])), [agents])
  const rows = useMemo(
    () =>
      (cands ?? [])
        .filter((c) => c.status === bucket)
        .slice()
        .sort((a, b) => (a.created_at < b.created_at ? 1 : a.created_at > b.created_at ? -1 : 0)),
    [cands, bucket],
  )
  const pendingTotal = useMemo(() => (cands ?? []).filter((c) => c.status === 'pending').length, [cands])

  const columns: ColumnsType<CompanionCandidate> = [
    {
      title: '来源智能体',
      dataIndex: 'agent_id',
      width: 130,
      ellipsis: true,
      render: (v: string) => <Tag color="cyan" style={{ margin: 0 }}>{agentName.get(v) || v.slice(0, 12) + '…'}</Tag>,
    },
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
      title: '来源会话',
      dataIndex: 'conversation_id',
      width: 130,
      ellipsis: true,
      render: (v: string) => (
        <Tooltip title={v}>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>{convTitles.get(v) || v.slice(0, 10) + '…'}</Typography.Text>
        </Tooltip>
      ),
    },
    {
      title: '原文锚点',
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
      {agents.length === 0 && !metaLoading ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={
            <span style={{ fontSize: 12 }}>
              暂无智能体绑定该本体——在「智能体」侧板「伴生本体」配置中选择/创建绑定后，对话候选在此跨智能体确认入图
            </span>
          }
        />
      ) : (
        <>
          <div style={{ marginBottom: 8 }}>
            <Space size={6} wrap>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>绑定智能体：</Typography.Text>
              {agents.map((a) => (
                <Tag key={a.id} color="cyan" style={{ margin: 0 }}>{a.name}</Tag>
              ))}
            </Space>
          </div>

          {/* 成长图（REQ-216⑧：伴生图=该本体可视化形态之一，页签内页首联动刷新） */}
          <Card size="small" className="work-card" style={{ marginBottom: 12 }} styles={{ body: { paddingTop: 8 } }}>
            <OntologyCompanionGraph key={graphTick} ontologyId={ontologyId} />
          </Card>

          <div className="onto-sec">
            <span className="onto-sec-title">候选（跨智能体 · 人工确认 = 入图门控，REQ-82 草稿必审）</span>
            <span className="hit-spacer" />
            <Segmented
              size="small"
              value={bucket}
              onChange={(v) => setBucket(v as 'pending' | 'confirmed' | 'rejected')}
              options={[
                { value: 'pending', label: `待确认${pendingTotal > 0 ? ` ${pendingTotal}` : ''}` },
                { value: 'confirmed', label: '已入图' },
                { value: 'rejected', label: '已拒绝' },
              ]}
            />
            <Segmented
              size="small"
              value={view}
              onChange={(v) => setView(v as 'time' | 'entity')}
              options={[
                { value: 'time', label: '时间倒序' },
                { value: 'entity', label: '按实体' },
              ]}
            />
            <Popconfirm
              title="清空该本体的伴生图？"
              description="DROP 本体伴生子图 + 清空全部绑定智能体的候选与游标（REQ-216 本体级摘除；各智能体伴生随之解绑，需重新绑定）；本体资产本身不受影响。"
              okText="清空"
              okButtonProps={{ danger: true }}
              cancelText="取消"
              onConfirm={doResetGraph}
            >
              <Button size="small" danger icon={<DeleteOutlined />} loading={resetting}>
                清空伴生图
              </Button>
            </Popconfirm>
            <Button size="small" icon={<ReloadOutlined />} onClick={() => { loadMeta(); loadCands() }} aria-label="刷新伴生候选" />
          </div>

          {actionErr && <LoadErrorAlert title="伴生操作失败" message={actionErr} onRetry={() => setActionErr(null)} style={{ marginBottom: 12 }} />}
          {batchResult && (
            <div style={{ fontSize: 12, color: 'var(--ant-color-success, #389e0d)', marginBottom: 8 }} role="status">
              {batchResult}
            </div>
          )}

          {candsErr ? (
            <LoadErrorAlert title="候选列表加载失败" message={candsErr} onRetry={() => { setView('time'); loadCands() }} />
          ) : candsLoading || metaLoading ? (
            <div style={{ padding: '16px 0' }}>
              <Spin />
            </div>
          ) : view === 'entity' ? (
            // REQ-216 增量③：按实体归组组卡（代表候选组内计数 + 批量入图/拒绝）
            (groups ?? []).length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={<span style={{ fontSize: 12 }}>该分组暂无候选</span>} />
            ) : (
              <div className="agent-companion-cands">
                {(groups ?? []).map((g) => (
                  <div key={g.key} className="agent-companion-cand" data-entity-group={g.key}>
                    <div className="agent-companion-cand-head">
                      <span className="agent-companion-cand-name" title={g.entity}>{g.entity}</span>
                      <Tag color={g.pending_count > 0 ? 'blue' : 'default'} style={{ margin: 0 }}>
                        {g.count} 条{g.pending_count > 0 ? ` · 待确认 ${g.pending_count}` : ''}
                      </Tag>
                      {bucket === 'pending' && g.pending_count > 0 && (
                        <Space size={4} className="agent-companion-cand-actions">
                          <Button size="small" type="primary" ghost icon={<CheckOutlined />} loading={batchBusy === g.key} onClick={() => decideGroup(g, 'confirm')} aria-label={`批量入图 ${g.entity}`}>
                            全部入图
                          </Button>
                          <Button size="small" danger icon={<CloseOutlined />} loading={batchBusy === g.key} onClick={() => decideGroup(g, 'reject')} aria-label={`批量拒绝 ${g.entity}`}>
                            全部拒绝
                          </Button>
                        </Space>
                      )}
                    </div>
                    {g.members.map((m) => (
                      <div key={m.id} style={{ fontSize: 12, color: 'var(--ant-color-text-tertiary, #999)', padding: '2px 0 2px 12px' }}>
                        <Tag color={KIND_META[m.kind]?.color ?? 'default'} style={{ margin: 0 }}>{KIND_META[m.kind]?.text ?? m.kind}</Tag>
                        {m.kind === 'relation' ? `${m.name} →${m.rel_target}` : m.name}
                        <Tag color="cyan" style={{ margin: '0 0 0 8px' }}>{agentName.get(m.agent_id) || m.agent_id.slice(0, 10) + '…'}</Tag>
                      </div>
                    ))}
                  </div>
                ))}
              </div>
            )
          ) : rows.length === 0 ? (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={
                bucket === 'pending'
                  ? '暂无待确认候选——与绑定智能体对话一轮后，收尾自动抽取'
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
