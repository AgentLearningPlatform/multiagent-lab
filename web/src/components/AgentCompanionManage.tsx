import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Empty, Segmented, Space, Spin, Tag, Tooltip, Typography } from 'antd'
import { CheckOutlined, CloseOutlined, ReloadOutlined } from '@ant-design/icons'
import { api } from '../api/client'
import type { Agent, Conversation } from '../api/types'
import { companionApi } from '../api/companion'
import type { CandidateGroup, CompanionCandidate, CompanionStatus } from '../api/companion'
import LoadErrorAlert from './LoadErrorAlert'

// ---------------------------------------------------------------------------
// REQ-193/M33：伴生管理铺平视图（智能体侧板「伴生本体」视图 · 伴生管理页）。
// 定案（开发者拍板「去会话筛选直接铺平」）：去会话下拉——本 agent 跨会话候选单列表
// 时间倒序 + 行内来源会话标注；三桶 Segmented 保留；状态卡 agent 视角（引擎 + 跨会话
// 待确认总数）。会话隔离全在存储/召回层（confirm 入图写候选来源会话 named graph），
// 管理浏览维度与其正交。边界：整体摘除与成长图 3D 留本体模块伴生子模块全量管理面
// （D-O19/D-O21 展示位不动）；会话级筛选后续有清查诉求再加。
//
// REQ-194/M34 增量：①抽取对齐徽标（对齐已有实体/新造）+⑤矛盾审计注记（疑似矛盾待人工）
// 呈现；⑥确认桶聚类——「时间倒序 / 按实体」双视图，按实体视图组卡（代表候选=组内置信
// 最高 + 组内计数）+ 按组批量入图/拒绝（循环单候选端点，结果如实计数，不做硬事务）。
// ---------------------------------------------------------------------------

const KIND_TEXT: Record<CompanionCandidate['kind'], string> = { concept: '概念', relation: '关系', event: '事件' }

/** REQ-194①：对齐徽标（绿=沿用已有实体，default=新造；空=存量未标不呈现） */
function AlignedBadge({ aligned }: { aligned?: CompanionCandidate['aligned'] }) {
  if (aligned === 'aligned') {
    return (
      <Tooltip title="抽取时已对齐会话伴生图已有实体（沿用原名）">
        <Tag color="cyan" style={{ margin: 0 }}>
          对齐
        </Tag>
      </Tooltip>
    )
  }
  if (aligned === 'new') {
    return (
      <Tooltip title="新造实体（不与既有清单重合）">
        <Tag style={{ margin: 0 }}>新造</Tag>
      </Tooltip>
    )
  }
  return null
}

/** REQ-194⑤：矛盾审计注记（疑似矛盾待人工等，橙徽标 + 悬浮全文） */
function NoteBadge({ note }: { note?: string }) {
  if (!note) return null
  return (
    <Tooltip title={note}>
      <Tag color="orange" style={{ margin: 0 }}>
        {note.includes('疑似矛盾') ? '疑似矛盾待人工' : '注记'}
      </Tag>
    </Tooltip>
  )
}

export default function AgentCompanionManage({ agent }: { agent: Agent }) {
  const [convs, setConvs] = useState<Conversation[]>([])
  const [convsErr, setConvsErr] = useState<string | null>(null)

  const [engine, setEngine] = useState<CompanionStatus | null>(null)
  const [engineLoading, setEngineLoading] = useState(false)

  const [cands, setCands] = useState<CompanionCandidate[] | null>(null)
  const [candsLoading, setCandsLoading] = useState(false)
  const [candsErr, setCandsErr] = useState<string | null>(null)
  const [bucket, setBucket] = useState<'pending' | 'confirmed' | 'rejected'>('pending')
  const [deciding, setDeciding] = useState<string | null>(null)
  const [actionErr, setActionErr] = useState<string | null>(null)
  // REQ-194⑥：时间倒序 / 按实体双视图
  const [view, setView] = useState<'time' | 'entity'>('time')
  const [groups, setGroups] = useState<CandidateGroup[] | null>(null)
  const [batchBusy, setBatchBusy] = useState<string | null>(null)
  const [batchResult, setBatchResult] = useState<string | null>(null)

  // 会话清单仅用于「来源会话标注」（id → 标题），不再作筛选
  const loadConvs = useCallback(() => {
    api
      .listConversations({ scope: 'agent', agent_id: agent.id })
      .then((ls) => {
        setConvs(ls)
        setConvsErr(null)
      })
      .catch((e: any) => setConvsErr(e?.message ?? '会话列表加载失败'))
  }, [agent.id])

  const loadAll = useCallback(() => {
    setEngineLoading(true)
    setCandsLoading(true)
    // agent 视角状态卡：引擎为全局单例（任一会话 status 即真），取最新会话探测；无会话=未启动
    api
      .listConversations({ scope: 'agent', agent_id: agent.id })
      .then((ls) => (ls[0] ? companionApi.status(ls[0].id) : Promise.resolve(null)))
      .then((st) => setEngine(st))
      .catch(() => setEngine(null))
      .finally(() => setEngineLoading(false))
    // 跨会话铺平：agent 维度一次拉全（量级=单 agent 候选总数），前端分桶+倒序
    companionApi
      .listCandidates('', '', agent.id)
      .then((ls) => {
        setCands(ls)
        setCandsErr(null)
      })
      .catch((e: any) => {
        setCands(null)
        setCandsErr(e?.message ?? '候选加载失败')
      })
      .finally(() => setCandsLoading(false))
  }, [agent.id])

  // REQ-194⑥：按实体视图数据（后端 group_by=entity 归组，桶过滤照常）；桶/视图切换即清批量结果消息
  useEffect(() => {
    setBatchResult(null)
    if (view !== 'entity') return
    setCandsLoading(true)
    companionApi
      .listCandidatesGrouped('', bucket, agent.id)
      .then((r) => {
        setGroups(r.groups ?? [])
        setCandsErr(null)
      })
      .catch((e: any) => {
        setGroups(null)
        setCandsErr(e?.message ?? '候选加载失败')
      })
      .finally(() => setCandsLoading(false))
  }, [view, bucket, agent.id])

  useEffect(() => {
    loadConvs()
    loadAll()
  }, [loadConvs, loadAll])

  const decide = async (id: string, action: 'confirm' | 'reject') => {
    setDeciding(id)
    setActionErr(null)
    try {
      await (action === 'confirm' ? companionApi.confirmCandidate(id) : companionApi.rejectCandidate(id))
      loadAll()
    } catch (e: any) {
      setActionErr(e?.message ?? '操作失败')
    } finally {
      setDeciding(null)
    }
  }

  // REQ-194⑥：按组批量裁决——循环既有单候选端点，结果如实计数（不做硬事务，诚实原则）
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
    if (ok + fail > 0) loadAll()
  }

  const convTitle = useMemo(() => {
    const m = new Map<string, string>()
    for (const c of convs) m.set(c.id, c.title || c.id)
    return m
  }, [convs])

  // 时间倒序（最新在上）；当前会话（对话窗正在用的会话）自然也在其中，行内标注可辨
  const rows = useMemo(
    () =>
      (cands ?? [])
        .filter((c) => c.status === bucket)
        .slice()
        .sort((a, b) => (a.created_at < b.created_at ? 1 : a.created_at > b.created_at ? -1 : 0)),
    [cands, bucket],
  )
  const pendingTotal = useMemo(() => (cands ?? []).filter((c) => c.status === 'pending').length, [cands])

  if (convsErr) return <LoadErrorAlert title="会话列表加载失败" message={convsErr} onRetry={loadConvs} style={{ marginBottom: 8 }} />

  const renderCandRow = (c: CompanionCandidate) => (
    <div key={c.id} className="agent-companion-cand">
      <div className="agent-companion-cand-head">
        <Tag color={c.kind === 'concept' ? 'blue' : c.kind === 'relation' ? 'purple' : 'geekblue'} style={{ margin: 0 }}>
          {KIND_TEXT[c.kind]}
        </Tag>
        <span className="agent-companion-cand-name" title={c.kind === 'relation' ? `${c.name} →${c.rel_target}` : c.name}>
          {c.kind === 'relation' ? (
            <>
              {c.name} <Tag style={{ margin: 0 }}>{c.rel_name}</Tag> {c.rel_target}
            </>
          ) : (
            c.name
          )}
        </span>
        <AlignedBadge aligned={c.aligned} />
        <NoteBadge note={c.note} />
        <Tag color={c.confidence >= 0.7 ? 'green' : c.confidence >= 0.4 ? 'orange' : 'default'} style={{ margin: 0 }}>
          {(c.confidence ?? 0).toFixed(2)}
        </Tag>
        {c.status === 'pending' ? (
          <Space size={4} className="agent-companion-cand-actions">
            <Button size="small" type="primary" ghost icon={<CheckOutlined />} loading={deciding === c.id || batchBusy !== null} onClick={() => decide(c.id, 'confirm')} aria-label={`确认入图 ${c.name}`}>
              入图
            </Button>
            <Button size="small" danger icon={<CloseOutlined />} loading={deciding === c.id || batchBusy !== null} onClick={() => decide(c.id, 'reject')} aria-label={`拒绝 ${c.name}`}>
              拒绝
            </Button>
          </Space>
        ) : (
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>
            {c.status === 'confirmed' ? '已入图' : '已拒绝'}
          </Typography.Text>
        )}
      </div>
      {/* REQ-193：行内来源会话标注（候选自带 conversation_id 归属） */}
      <div style={{ fontSize: 11, color: 'var(--ant-color-text-tertiary, #999)', marginBottom: 2 }}>
        来源会话：{convTitle.get(c.conversation_id) ?? c.conversation_id} · {c.created_at?.slice(5, 16).replace('T', ' ')}
      </div>
      {c.definition && (
        <Tooltip title={c.definition}>
          <div className="agent-companion-cand-def">{c.definition}</div>
        </Tooltip>
      )}
    </div>
  )

  return (
    <div className="agent-companion-manage">
      <div className="agent-companion-status" style={{ marginBottom: 8 }}>
        {engineLoading ? (
          <Spin size="small" />
        ) : engine ? (
          <Space size={6} wrap>
            <Tag color={engine.engine_running ? 'green' : 'default'} style={{ margin: 0 }}>
              引擎{engine.engine_running ? '运行中' : '未启动'}
            </Tag>
            <Tag color={pendingTotal > 0 ? 'blue' : 'default'} style={{ margin: 0 }}>
              跨会话待确认 {pendingTotal}
            </Tag>
          </Space>
        ) : (
          <Space size={6} wrap>
            <Tag style={{ margin: 0 }}>引擎未启动</Tag>
            <Tag color={pendingTotal > 0 ? 'blue' : 'default'} style={{ margin: 0 }}>
              跨会话待确认 {pendingTotal}
            </Tag>
          </Space>
        )}
      </div>

      <Space size={6} style={{ marginBottom: 8, width: '100%' }} wrap>
        <Segmented
          size="small"
          value={bucket}
          onChange={(v) => setBucket(v as typeof bucket)}
          options={[
            { value: 'pending', label: '待确认' },
            { value: 'confirmed', label: '已入图' },
            { value: 'rejected', label: '已拒绝' },
          ]}
        />
        {/* REQ-194⑥：确认桶聚类——时间倒序 / 按实体 */}
        <Segmented
          size="small"
          value={view}
          onChange={(v) => setView(v as typeof view)}
          options={[
            { value: 'time', label: '时间倒序' },
            { value: 'entity', label: '按实体' },
          ]}
        />
        <Button size="small" icon={<ReloadOutlined />} onClick={loadAll} aria-label="刷新伴生数据" />
      </Space>

      {actionErr && <LoadErrorAlert title="伴生操作失败" message={actionErr} onRetry={() => setActionErr(null)} style={{ marginBottom: 8 }} />}
      {batchResult && (
        <div style={{ fontSize: 12, color: 'var(--ant-color-success, #389e0d)', marginBottom: 8 }} role="status">
          {batchResult}
        </div>
      )}

      {candsErr ? (
        <LoadErrorAlert
          title="候选加载失败"
          message={candsErr}
          onRetry={() => {
            setView('time')
            loadAll()
          }}
        />
      ) : candsLoading ? (
        <Spin size="small" />
      ) : view === 'time' ? (
        rows.length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={<span style={{ fontSize: 12 }}>{bucket === 'pending' ? '暂无待确认候选——与该智能体对话一轮后收尾自动抽取' : '该分组暂无候选'}</span>} />
        ) : (
          <div className="agent-companion-cands">{rows.map(renderCandRow)}</div>
        )
      ) : (groups ?? []).length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={<span style={{ fontSize: 12 }}>该分组暂无候选</span>} />
      ) : (
        <div className="agent-companion-cands">
          {(groups ?? []).map((g) => (
            <div key={g.key} className="agent-companion-cand" data-entity-group={g.key}>
              <div className="agent-companion-cand-head">
                <span className="agent-companion-cand-name" title={g.entity}>
                  {g.entity}
                </span>
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
              {g.members.map(renderCandRow)}
            </div>
          ))}
        </div>
      )}

      <Typography.Paragraph type="secondary" style={{ fontSize: 11, marginTop: 10, marginBottom: 0 }}>
        入图写入候选来源会话的伴生图（对话召回按会话隔离）；成长图 3D 与整体摘除在本体模块「伴生本体」栏（全量管理面）。
      </Typography.Paragraph>
    </div>
  )
}
