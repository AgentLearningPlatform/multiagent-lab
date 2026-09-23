import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Card, Empty, Input, Select, Space, Spin, Statistic, Tag, Typography } from 'antd'
import { SearchOutlined } from '@ant-design/icons'
import { Background, Controls, ReactFlow } from '@xyflow/react'
import type { Edge, Node } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { api } from '../api/client'
import { useUI } from '../store/ui'

/**
 * GraphRAG 库「图谱」视图（M16 阶段一：REQ-127 图谱浏览与统计 + REQ-128 聚焦检索前端）：
 * - 统计卡：实体/关系/claims 计数、类型分布、文档覆盖（degraded_docs = 无 KG 行的文档）；
 * - 实体搜索 → 邻域展开（hops 1~2，React Flow；节点=实体、边=关系类型）；
 * - 聚焦检索：query/entity + 跳数 + 关系类型 → graphrag-search 增强（结果附实体/关系/claims，
 *   claims 带 chunk 溯源 doc#seq 定位原文）。
 */

interface KGStats {
  entities: number
  relationships: number
  claims: number
  entity_type_dist: Record<string, number>
  rel_type_dist: Record<string, number>
  docs_total: number
  docs_with_kg: number
  degraded_docs: number
}

interface KGEntityLite {
  id: string
  name: string
  type?: string
  description?: string
}

interface KGRelLite {
  id: string
  source: string
  target: string
  type?: string
}

interface KGClaimT {
  id?: string
  status?: string
  subject: string
  text: string
  chunk_id?: string
  trace_doc_id?: string
  trace_seq?: number
}

const TYPE_COLOR: Record<string, string> = {
  concept: '#4f46e5',
  individual: '#0ea5e9',
  event: '#d97706',
  property: '#64748b',
}

export default function KGGraphView({ kbID }: { kbID: string }) {
  const { showToast } = useUI()
  const [stats, setStats] = useState<KGStats | null>(null)
  const [q, setQ] = useState('')
  const [results, setResults] = useState<KGEntityLite[]>([])
  const [searching, setSearching] = useState(false)
  const [seed, setSeed] = useState<string | null>(null)
  const [hops, setHops] = useState(1)
  const [loading, setLoading] = useState(false)
  const [entities, setEntities] = useState<KGEntityLite[]>([])
  const [rels, setRels] = useState<KGRelLite[]>([])
  const [claims, setClaims] = useState<KGClaimT[]>([])

  const loadStats = useCallback(() => {
    api
      .kgStats(kbID)
      .then(setStats)
      .catch((e) => showToast(e.message, 'err'))
  }, [kbID, showToast])
  useEffect(loadStats, [loadStats])

  const search = async () => {
    if (!q.trim()) return
    setSearching(true)
    try {
      const r = await api.kgEntitySearch(kbID, q.trim())
      setResults(r.entities ?? [])
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setSearching(false)
    }
  }

  const expand = async (entity: string, hop = hops) => {
    setLoading(true)
    setSeed(entity)
    try {
      const r = await api.kgNeighborhood(kbID, entity, hop)
      setEntities(r.entities ?? [])
      setRels(r.relationships ?? [])
      setClaims(r.claims ?? [])
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setLoading(false)
    }
  }

  const nodes: Node[] = useMemo(
    () =>
      entities.map((e, i) => ({
        id: e.name,
        position: {
          x: 120 + (i % 6) * 150 + (seed === e.name ? 0 : 20),
          y: 60 + Math.floor(i / 6) * 90,
        },
        data: { label: `${e.name}${e.type ? `\n${e.type}` : ''}` },
        style: {
          background: seed === e.name ? '#4f46e5' : TYPE_COLOR[e.type ?? ''] ?? '#64748b',
          color: '#fff',
          border: 'none',
          borderRadius: 8,
          fontSize: 11,
          padding: 6,
          width: 120,
        },
      })),
    [entities, seed],
  )
  const edges: Edge[] = useMemo(
    () =>
      rels.map((r, i) => ({
        id: `e${i}`,
        source: r.source,
        target: r.target,
        label: r.type,
        style: { stroke: '#9aa2b8', fontSize: 10 },
        labelStyle: { fill: '#4b5563', fontSize: 10 },
        labelBgStyle: { fill: '#f3f4f6' },
      })),
    [rels],
  )

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <Card size="small" title="图谱统计（REQ-127）" extra={<Button size="small" onClick={loadStats}>刷新</Button>}>
        {stats ? (
          <>
            <Space size={24} wrap>
              <Statistic title="实体" value={stats.entities} />
              <Statistic title="关系" value={stats.relationships} />
              <Statistic title="Claims" value={stats.claims} />
              <Statistic title="文档覆盖" value={`${stats.docs_with_kg}/${stats.docs_total}`} />
              {stats.degraded_docs > 0 && (
                <Statistic title="未覆盖（degraded）" value={stats.degraded_docs} valueStyle={{ color: '#d97706' }} />
              )}
            </Space>
            <div style={{ marginTop: 10 }}>
              {Object.entries(stats.entity_type_dist).map(([t, n]) => (
                <Tag key={'e' + t} style={{ marginInlineEnd: 6 }}>{t || '未分类'} × {n}</Tag>
              ))}
              {Object.entries(stats.rel_type_dist).map(([t, n]) => (
                <Tag key={'r' + t} color="blue" style={{ marginInlineEnd: 6 }}>{t || '未标注'} × {n}</Tag>
              ))}
            </div>
          </>
        ) : (
          <Spin size="small" />
        )}
      </Card>

      <Card size="small" title="实体搜索与邻域展开（hops 1~2）">
        <Space size={8} wrap>
          <Input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onPressEnter={search}
            placeholder="搜索实体名…"
            style={{ width: 260 }}
            suffix={<SearchOutlined onClick={search} style={{ cursor: 'pointer' }} />}
          />
          <Button size="small" loading={searching} onClick={search}>搜索</Button>
          <Select value={hops} onChange={(v) => setHops(v)} style={{ width: 100 }}
            options={[{ value: 1, label: '1 跳' }, { value: 2, label: '2 跳' }]} />
          {seed && <Tag color="purple">种子：{seed}</Tag>}
        </Space>
        {results.length > 0 && (
          <div style={{ marginTop: 8, display: 'flex', flexWrap: 'wrap', gap: 6 }}>
            {results.map((e) => (
              <Button key={e.id} size="small" onClick={() => expand(e.name)}>
                {e.name}
              </Button>
            ))}
          </div>
        )}
        {loading ? (
          <Spin size="small" style={{ marginTop: 10 }} />
        ) : entities.length > 0 ? (
          <div style={{ height: 380, marginTop: 10, border: '1px solid #e3e6f0', borderRadius: 8, overflow: 'hidden' }}>
            <ReactFlow nodes={nodes} edges={edges} fitView proOptions={{ hideAttribution: true }}>
              <Background gap={18} />
              <Controls showInteractive={false} />
            </ReactFlow>
          </div>
        ) : seed ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="该实体无邻域（无关系边）" style={{ marginTop: 10 }} />
        ) : null}
        {claims.length > 0 && (
          <div style={{ marginTop: 10 }}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>Claims（chunk 溯源）：</Typography.Text>
            <ul style={{ margin: '6px 0 0', paddingLeft: 18 }}>
              {claims.slice(0, 12).map((c, i) => (
                <li key={i} style={{ fontSize: 12, marginBottom: 4 }}>
                  <Tag style={{ marginInlineEnd: 6 }}>{c.subject}</Tag>
                  {c.text}
                  {c.trace_doc_id && (
                    <Tag color="cyan" style={{ marginInlineStart: 6 }}>
                      溯源 doc#{c.trace_seq ?? '—'}
                    </Tag>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
      </Card>
    </div>
  )
}

/**
 * 抽取治理面板（M16 阶段二 REQ-129）：质量统计（method 分布/孤儿实体/TopN 关系类型/rejected 计数）
 * + 合并建议（别名消歧，人工确认执行）+ 审核队列（关系/claims approve/reject，rejected 不参与检索）。
 */
export function KGGovernancePanel({ kbID }: { kbID: string }) {
  const { showToast } = useUI()
  const [q, setQ] = useState<Awaited<ReturnType<typeof api.kgQuality>> | null>(null)
  const [sugs, setSugs] = useState<{ keep: string; merge: string; reason: string }[]>([])
  const [rejected, setRejected] = useState<{ rels: KGRelLite[]; claims: KGClaimT[] }>({ rels: [], claims: [] })
  const [approved, setApproved] = useState<{ rels: KGRelLite[]; claims: KGClaimT[] }>({ rels: [], claims: [] })
  const [loading, setLoading] = useState(true)

  const load = useCallback(() => {
    setLoading(true)
    Promise.all([api.kgQuality(kbID), api.kgMergeSuggestions(kbID), api.kgRead(kbID)])
      .then(([quality, sugg, read]) => {
        setQ(quality)
        setSugs(sugg.suggestions ?? [])
        // 审核队列：从全量子图中筛 rejected（kgRead 返回全量含 rejected）
        const allRels = (read.relationships ?? []) as (KGRelLite & { status?: string; id: string })[]
        const allClaims = (read.claims ?? []) as (KGClaimT & { status?: string; id: string })[]
        setApproved({
          rels: allRels.filter((r) => r.status !== 'rejected').slice(0, 20),
          claims: allClaims.filter((c) => c.status !== 'rejected').slice(0, 20),
        })
        setRejected({
          rels: (read.relationships ?? []).filter((r: any) => r.status === 'rejected'),
          claims: (read.claims ?? []).filter((c: any) => c.status === 'rejected'),
        })
      })
      .catch((e) => showToast(e.message, 'err'))
      .finally(() => setLoading(false))
  }, [kbID, showToast])
  useEffect(load, [load])

  const review = async (kind: 'relationship' | 'claim', id: string, status: 'approved' | 'rejected') => {
    try {
      await api.kgReview(kbID, kind, id, status)
      showToast(status === 'approved' ? '已批准' : '已拒绝（不再参与检索）')
      load()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }
  const merge = async (keep: string, m: string) => {
    try {
      const r = await api.kgMerge(kbID, keep, [m])
      showToast(`已合并：关系 ${r.moved_relationships} 条、claims ${r.moved_claims} 条迁移至「${keep}」`)
      load()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  return (
    <Card size="small" title="抽取治理（REQ-129）" style={{ marginTop: 12 }}
      extra={<Button size="small" loading={loading} onClick={load}>刷新</Button>}>
      {q ? (
        <Space size={18} wrap style={{ marginBottom: 8 }}>
          <Statistic title="实体" value={q.entities} />
          <Statistic title="关系" value={q.relationships} />
          <Statistic title="孤儿实体" value={q.orphan_entity} />
          <Statistic title="拒绝（关系）" value={q.rejected_rels} />
          <Statistic title="拒绝（claims）" value={q.rejected_claims} />
          <div>
            <Typography.Text type="secondary" style={{ fontSize: 11 }}>抽取方式分布</Typography.Text>
            <div>
              {Object.entries(q.method_dist || {}).map(([m, n]) => (
                <Tag key={m} color={m === 'llm' ? 'blue' : 'orange'} style={{ margin: 2 }}>{m} × {n}</Tag>
              ))}
              {Object.keys(q.method_dist || {}).length === 0 && <Typography.Text type="secondary">—</Typography.Text>}
            </div>
          </div>
          <div>
            <Typography.Text type="secondary" style={{ fontSize: 11 }}>高频关系类型 TopN</Typography.Text>
            <div>
              {(q.top_rel_types || []).map((t) => (
                <Tag key={t.type} style={{ margin: 2 }}>{t.type || '未标注'} × {t.count}</Tag>
              ))}
            </div>
          </div>
        </Space>
      ) : (
        <Spin size="small" />
      )}

      <Typography.Text strong style={{ fontSize: 12 }}>合并建议（别名消歧，人工确认后执行）</Typography.Text>
      {sugs.length === 0 ? (
        <div style={{ margin: '4px 0 10px' }}><Typography.Text type="secondary" style={{ fontSize: 12 }}>暂无建议</Typography.Text></div>
      ) : (
        <ul style={{ margin: '4px 0 10px', paddingLeft: 18 }}>
          {sugs.map((sg, i) => (
            <li key={i} style={{ fontSize: 12, marginBottom: 4 }}>
              {sg.reason}
              <Button size="small" type="link" onClick={() => merge(sg.keep, sg.merge)}>执行合并</Button>
            </li>
          ))}
        </ul>
      )}

      <Typography.Text strong style={{ fontSize: 12 }}>审核队列（rejected 不参与检索）</Typography.Text>
      {rejected.rels.length === 0 && rejected.claims.length === 0 ? (
        <div style={{ margin: '4px 0' }}><Typography.Text type="secondary" style={{ fontSize: 12 }}>暂无被拒绝的关系/claims（可在下方原始数据中拒绝新条目）</Typography.Text></div>
      ) : (
        <ul style={{ margin: '4px 0', paddingLeft: 18 }}>
          {rejected.rels.map((r: any) => (
            <li key={r.id} style={{ fontSize: 12, marginBottom: 4 }}>
              <Tag color="red" style={{ marginInlineEnd: 6 }}>关系</Tag>
              {r.source} —[{r.type}]→ {r.target}
              <Button size="small" type="link" onClick={() => review('relationship', r.id, 'approved')}>批准恢复</Button>
            </li>
          ))}
          {rejected.claims.map((c: any) => (
            <li key={c.id} style={{ fontSize: 12, marginBottom: 4 }}>
              <Tag color="red" style={{ marginInlineEnd: 6 }}>Claim</Tag>
              [{c.subject}] {c.text}
              <Button size="small" type="link" onClick={() => review('claim', c.id ?? '', 'approved')}>批准恢复</Button>
            </li>
          ))}
        </ul>
      )}
      <Typography.Text strong style={{ fontSize: 12 }}>已批准（可拒绝，拒绝后不参与检索）</Typography.Text>
      {approved.rels.length === 0 && approved.claims.length === 0 ? (
        <div style={{ margin: '4px 0' }}><Typography.Text type="secondary" style={{ fontSize: 12 }}>暂无已批准条目</Typography.Text></div>
      ) : (
        <ul style={{ margin: '4px 0', paddingLeft: 18 }}>
          {approved.rels.map((r) => (
            <li key={r.id} style={{ fontSize: 12, marginBottom: 4 }}>
              <Tag color="green" style={{ marginInlineEnd: 6 }}>关系</Tag>
              {r.source} —[{r.type}]→ {r.target}
              <Button size="small" type="link" danger onClick={() => review('relationship', r.id, 'rejected')}>拒绝</Button>
            </li>
          ))}
          {approved.claims.map((c) => (
            <li key={c.id} style={{ fontSize: 12, marginBottom: 4 }}>
              <Tag color="green" style={{ marginInlineEnd: 6 }}>Claim</Tag>
              [{c.subject}] {c.text}
              <Button size="small" type="link" danger onClick={() => review('claim', c.id ?? '', 'rejected')}>拒绝</Button>
            </li>
          ))}
        </ul>
      )}
      <Typography.Text type="secondary" style={{ fontSize: 11 }}>
        拒绝后的条目进入上方审核队列，可随时批准恢复。
      </Typography.Text>
    </Card>
  )
}
