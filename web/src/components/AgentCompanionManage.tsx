import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Empty, Segmented, Select, Space, Spin, Tag, Tooltip, Typography } from 'antd'
import { CheckOutlined, CloseOutlined, ReloadOutlined } from '@ant-design/icons'
import { api } from '../api/client'
import type { Agent, Conversation } from '../api/types'
import { companionApi } from '../api/companion'
import type { CompanionCandidate, CompanionStatus } from '../api/companion'
import LoadErrorAlert from './LoadErrorAlert'

// ---------------------------------------------------------------------------
// REQ-189：伴生本体「智能体侧」紧凑管理视图（AgentSidePanel 伴生页签内嵌）。
// 定位：单 agent 聚焦视图——配置（开关/REQ-187 三字段在宿主 Form）+ 本 agent 会话的
// 候选确认/拒绝轻操作。边界：整体摘除（破坏性）与成长图 3D 保留在本体模块伴生子模块
// 全量管理面（D-O19/D-O21 展示位不动；AgentModal 不挂本组件——新建场景无对话无候选）。
// 后端零改动：伴生五端点按 conversation 维度，本组件按 conversation.agent_id 前端过滤。
// ---------------------------------------------------------------------------

const KIND_TEXT: Record<CompanionCandidate['kind'], string> = { concept: '概念', relation: '关系', event: '事件' }

export default function AgentCompanionManage({ agent }: { agent: Agent }) {
  const [convs, setConvs] = useState<Conversation[]>([])
  const [convsErr, setConvsErr] = useState<string | null>(null)
  const [convId, setConvId] = useState<string | undefined>(undefined)

  const [status, setStatus] = useState<CompanionStatus | null>(null)
  const [statusLoading, setStatusLoading] = useState(false)

  const [cands, setCands] = useState<CompanionCandidate[] | null>(null)
  const [candsLoading, setCandsLoading] = useState(false)
  const [candsErr, setCandsErr] = useState<string | null>(null)
  const [bucket, setBucket] = useState<'pending' | 'confirmed' | 'rejected'>('pending')
  const [deciding, setDeciding] = useState<string | null>(null)
  const [actionErr, setActionErr] = useState<string | null>(null)

  const loadConvs = useCallback(() => {
    api
      .listConversations({ scope: 'agent', agent_id: agent.id })
      .then((ls) => {
        setConvs(ls)
        setConvsErr(null)
        setConvId((cur) => cur ?? ls[0]?.id)
      })
      .catch((e: any) => setConvsErr(e?.message ?? '会话列表加载失败'))
  }, [agent.id])

  useEffect(() => {
    loadConvs()
  }, [loadConvs])

  const loadAll = useCallback((cid: string) => {
    setStatusLoading(true)
    setCandsLoading(true)
    companionApi.status(cid).then(setStatus).catch(() => setStatus(null)).finally(() => setStatusLoading(false))
    companionApi
      .listCandidates(cid)
      .then((ls) => {
        setCands(ls)
        setCandsErr(null)
      })
      .catch((e: any) => {
        setCands(null)
        setCandsErr(e?.message ?? '候选加载失败')
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
    } catch (e: any) {
      setActionErr(e?.message ?? '操作失败')
    } finally {
      setDeciding(null)
    }
  }

  const rows = useMemo(() => (cands ?? []).filter((c) => c.status === bucket), [cands, bucket])

  if (convsErr) return <LoadErrorAlert title="会话列表加载失败" message={convsErr} onRetry={loadConvs} style={{ marginBottom: 8 }} />

  if (convs.length === 0) {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description={
          <span style={{ fontSize: 12 }}>
            本智能体暂无会话——新建对话并完成一轮后，收尾自动抽取候选在此确认入图
            <br />
            （全量管理面在本体模块「伴生本体」栏）
          </span>
        }
      />
    )
  }

  return (
    <div className="agent-companion-manage">
      <Space size={6} style={{ marginBottom: 8, width: '100%' }} wrap>
        <Select
          size="small"
          showSearch
          optionFilterProp="label"
          style={{ minWidth: 180, flex: 1 }}
          value={convId}
          onChange={setConvId}
          placeholder="选择会话"
          options={convs.map((c) => ({ value: c.id, label: c.title || c.id }))}
        />
        <Button size="small" icon={<ReloadOutlined />} onClick={() => convId && loadAll(convId)} aria-label="刷新伴生数据" />
      </Space>

      <div className="agent-companion-status" style={{ marginBottom: 8 }}>
        {statusLoading ? (
          <Spin size="small" />
        ) : status ? (
          <Space size={6} wrap>
            <Tag color={status.engine_running ? 'green' : 'default'} style={{ margin: 0 }}>
              引擎{status.engine_running ? '运行中' : '未启动'}
            </Tag>
            <Tag color="blue" style={{ margin: 0 }}>
              待确认 {status.pending_count}
            </Tag>
            {(status.labels ?? []).slice(0, 6).map((l) => (
              <Tag key={l} color="geekblue" style={{ margin: 0 }}>
                {l}
              </Tag>
            ))}
            {(status.labels ?? []).length > 6 && (
              <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                …共 {(status.labels ?? []).length} 实体
              </Typography.Text>
            )}
          </Space>
        ) : (
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>
            管线状态不可用（对话收尾抽取后自动就绪）
          </Typography.Text>
        )}
      </div>

      {actionErr && <LoadErrorAlert title="伴生操作失败" message={actionErr} onRetry={() => setActionErr(null)} style={{ marginBottom: 8 }} />}

      <Segmented
        size="small"
        value={bucket}
        onChange={(v) => setBucket(v as typeof bucket)}
        options={[
          { value: 'pending', label: '待确认' },
          { value: 'confirmed', label: '已入图' },
          { value: 'rejected', label: '已拒绝' },
        ]}
        style={{ marginBottom: 8 }}
      />

      {candsErr ? (
        <LoadErrorAlert title="候选加载失败" message={candsErr} onRetry={() => convId && loadAll(convId)} />
      ) : candsLoading ? (
        <Spin size="small" />
      ) : rows.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={<span style={{ fontSize: 12 }}>{bucket === 'pending' ? '暂无待确认候选——与该智能体对话一轮后收尾自动抽取' : '该分组暂无候选'}</span>} />
      ) : (
        <div className="agent-companion-cands">
          {rows.map((c) => (
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
                <Tag color={c.confidence >= 0.7 ? 'green' : c.confidence >= 0.4 ? 'orange' : 'default'} style={{ margin: 0 }}>
                  {(c.confidence ?? 0).toFixed(2)}
                </Tag>
                {c.status === 'pending' ? (
                  <Space size={4} className="agent-companion-cand-actions">
                    <Button size="small" type="primary" ghost icon={<CheckOutlined />} loading={deciding === c.id} onClick={() => decide(c.id, 'confirm')} aria-label={`确认入图 ${c.name}`}>
                      入图
                    </Button>
                    <Button size="small" danger icon={<CloseOutlined />} loading={deciding === c.id} onClick={() => decide(c.id, 'reject')} aria-label={`拒绝 ${c.name}`}>
                      拒绝
                    </Button>
                  </Space>
                ) : (
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    {c.status === 'confirmed' ? '已入图' : '已拒绝'}
                  </Typography.Text>
                )}
              </div>
              {c.definition && (
                <Tooltip title={c.definition}>
                  <div className="agent-companion-cand-def">{c.definition}</div>
                </Tooltip>
              )}
            </div>
          ))}
        </div>
      )}

      <Typography.Paragraph type="secondary" style={{ fontSize: 11, marginTop: 10, marginBottom: 0 }}>
        成长图 3D 与整体摘除在本体模块「伴生本体」栏（全量管理面）。
      </Typography.Paragraph>
    </div>
  )
}
