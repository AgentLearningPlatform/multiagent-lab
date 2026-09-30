import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Empty, Input, Select, Space, Spin, Statistic, Tag, Tooltip, Typography } from 'antd'
import { SearchOutlined } from '@ant-design/icons'
import { Background, Controls, ReactFlow } from '@xyflow/react'
import type { Edge, Node } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { api } from '../api/client'
import { useUI } from '../store/ui'
import HighlightSpans, { locateText } from './HighlightSpans'

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
  /** M36/B1：出处 chunk 原文（句级高亮用；缺失 = 溯源降级，仅 doc#seq 标注） */
  trace_chunk_content?: string
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
  const [rebuilding, setRebuilding] = useState(false)
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

  // M36/KB-13：KG 重建治理入口收敛知识库侧（消费与审计页只保留展示，P2 原则「展示位≠管理入口」）
  const rebuildKG = async () => {
    setRebuilding(true)
    try {
      const r = await api.chunksToKG(kbID)
      const g = r.graphrag
      showToast(
        g.degraded ? `重建降级：${g.error ?? '未知原因'}` : `KG 已重建：实体 ${g.entities ?? 0} · 关系 ${g.relationships ?? 0}（${g.method ?? 'llm'}）`,
        g.degraded ? 'err' : 'ok',
      )
      loadStats()
    } catch (e: any) {
      showToast(e?.message ?? '重建失败', 'err')
    } finally {
      setRebuilding(false)
    }
  }

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
      <Card size="small" title="图谱统计（REQ-127）"
        extra={
          <Space size={6}>
            <Button size="small" loading={rebuilding} onClick={rebuildKG}>重建 KG</Button>
            <Button size="small" onClick={loadStats}>刷新</Button>
          </Space>
        }
      >
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
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>Claims（chunk 溯源；命中原文句级高亮）：</Typography.Text>
            <ul style={{ margin: '6px 0 0', paddingLeft: 18 }}>
              {claims.slice(0, 12).map((c, i) => {
                const spans = c.trace_chunk_content ? locateText(c.trace_chunk_content, c.text) : undefined
                return (
                  <li key={i} style={{ fontSize: 12, marginBottom: 4 }}>
                    <Tag style={{ marginInlineEnd: 6 }}>{c.subject}</Tag>
                    {c.text}
                    {c.trace_chunk_content && (
                      <Tooltip title={<span style={{ fontSize: 12 }}><HighlightSpans text={c.trace_chunk_content} spans={spans} /></span>}>
                        <Tag color="cyan" style={{ marginInlineStart: 6 }}>
                          溯源 doc#{c.trace_seq ?? '—'}{spans ? ' · 句级' : ''}
                        </Tag>
                      </Tooltip>
                    )}
                  </li>
                )
              })}
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
  const [sugs, setSugs] = useState<{ keep: string; merge: string; reason: string; strategy?: string; similarity?: number; type_warning?: string }[]>([])
  const [vecDegraded, setVecDegraded] = useState(false)
  const [rejected, setRejected] = useState<{ rels: KGRelLite[]; claims: KGClaimT[] }>({ rels: [], claims: [] })
  const [approved, setApproved] = useState<{ rels: KGRelLite[]; claims: KGClaimT[] }>({ rels: [], claims: [] })
  const [loading, setLoading] = useState(true)
  // M36/KB-7②：别名人工标注（选择实体 + 别名输入；重建/合并自动保留归并）
  const [aliasEntity, setAliasEntity] = useState<string | undefined>(undefined)
  const [aliasText, setAliasText] = useState('')
  const [entityOpts, setEntityOpts] = useState<{ name: string; alias?: string }[]>([])
  const [savingAlias, setSavingAlias] = useState(false)

  const load = useCallback(() => {
    setLoading(true)
    Promise.all([api.kgQuality(kbID), api.kgMergeSuggestions(kbID), api.kgRead(kbID)])
      .then(([quality, sugg, read]) => {
        setQ(quality)
        setSugs(sugg.suggestions ?? [])
        setVecDegraded(!!sugg.vector_degraded)
        setEntityOpts((read.entities ?? []).map((e: any) => ({ name: e.name, alias: e.alias })))
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

  const saveAlias = async () => {
    if (!aliasEntity) {
      showToast('先选择实体', 'err')
      return
    }
    setSavingAlias(true)
    try {
      await api.kgEntityAlias(kbID, aliasEntity, aliasText.trim())
      showToast(aliasText.trim() ? '别名已保存（检索/搜索命中别名）' : '别名已清除')
      load()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setSavingAlias(false)
    }
  }

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

      <Typography.Text strong style={{ fontSize: 12 }}>合并建议（规则 + 向量双臂，人工确认后执行）</Typography.Text>
      {vecDegraded && (
        <Alert type="warning" showIcon style={{ margin: '4px 0' }}
          message="向量消歧建议降级：embedding 未配置或不可用，当前仅名称包含规则建议（M36/KB-7）" />
      )}
      {sugs.length === 0 ? (
        <div style={{ margin: '4px 0 10px' }}><Typography.Text type="secondary" style={{ fontSize: 12 }}>暂无建议</Typography.Text></div>
      ) : (
        <ul style={{ margin: '4px 0 10px', paddingLeft: 18 }}>
          {sugs.map((sg, i) => (
            <li key={i} style={{ fontSize: 12, marginBottom: 4 }}>
              <Tag color={sg.strategy === 'vector' ? 'geekblue' : 'default'} style={{ marginInlineEnd: 6 }}>
                {sg.strategy === 'vector' ? `向量 ${sg.similarity?.toFixed(3) ?? ''}` : '规则'}
              </Tag>
              {sg.reason}
              {sg.type_warning && (
                <Tag color="orange" style={{ marginInlineStart: 4 }}>{sg.type_warning}</Tag>
              )}
              <Button size="small" type="link" onClick={() => merge(sg.keep, sg.merge)}>执行合并</Button>
            </li>
          ))}
        </ul>
      )}

      <Typography.Text strong style={{ fontSize: 12 }}>别名标注（KB-7②：检索/搜索命中别名；重建与合并自动保留）</Typography.Text>
      <div style={{ display: 'flex', gap: 8, margin: '4px 0 10px', flexWrap: 'wrap' }}>
        <Select
          showSearch
          optionFilterProp="label"
          value={aliasEntity}
          onChange={(v) => {
            setAliasEntity(v)
            setAliasText(entityOpts.find((e) => e.name === v)?.alias ?? '')
          }}
          placeholder="选择实体…"
          style={{ width: 220 }}
          options={entityOpts.map((e) => ({
            value: e.name,
            label: e.alias ? `${e.name}（${e.alias}）` : e.name,
          }))}
        />
        <Input
          value={aliasText}
          onChange={(e) => setAliasText(e.target.value)}
          placeholder="别名（分号分隔多个；留空清除）"
          style={{ width: 240 }}
          onPressEnter={saveAlias}
        />
        <Button size="small" loading={savingAlias} onClick={saveAlias}>保存别名</Button>
      </div>

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

/**
 * 社区摘要与全局问答（M16 阶段二 REQ-130）：社区列表（摘要 + 成员实体）+ 重建 +
 * 全局问答输入（社区摘要 2-gram 评分检索；未建社区时降级引导）。
 */
export function KGGlobalPanel({ kbID }: { kbID: string }) {
  const { showToast } = useUI()
  const [comms, setComms] = useState<{ id: string; label: string; summary: string; method?: string; members: string[] }[]>([])
  const [loading, setLoading] = useState(true)
  const [rebuilding, setRebuilding] = useState(false)
  const [query, setQuery] = useState('')
  const [hits, setHits] = useState<{ label: string; summary: string; members: string[]; score: number }[] | null>(null)
  const [degraded, setDegraded] = useState(false)
  const [degradedMsg, setDegradedMsg] = useState('')

  const load = useCallback(() => {
    setLoading(true)
    api.kgCommunities(kbID)
      .then((r) => setComms(r.communities ?? []))
      .catch((e) => showToast(e.message, 'err'))
      .finally(() => setLoading(false))
  }, [kbID, showToast])
  useEffect(load, [load])

  const rebuild = async () => {
    setRebuilding(true)
    try {
      const r = await api.kgCommunitiesRebuild(kbID)
      showToast(`已重建 ${r.communities} 个社区摘要`)
      load()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setRebuilding(false)
    }
  }

  const ask = async () => {
    const text = query.trim()
    if (!text) return
    try {
      const r = await api.kgGlobalSearch(kbID, text)
      setHits(r.hits ?? [])
      setDegraded(!!r.degraded)
      setDegradedMsg(r.message ?? '')
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  return (
    <Card size="small" title="社区摘要与全局问答（REQ-130）" style={{ marginTop: 12 }}
      extra={<Button size="small" loading={rebuilding} onClick={rebuild}>重建社区摘要</Button>}>
      {loading ? (
        <Spin size="small" />
      ) : comms.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="尚未构建社区摘要；点击右上「重建社区摘要」（label propagation 检测 + LLM 摘要，无模型时回退骨架摘要）。" />
      ) : (
        <ul style={{ margin: '0 0 10px', paddingLeft: 18 }}>
          {comms.map((c) => (
            <li key={c.id} style={{ fontSize: 12, marginBottom: 6 }}>
              <Typography.Text strong style={{ fontSize: 12 }}>「{c.label}」社区</Typography.Text>
              {c.method && <Tag style={{ marginInlineStart: 6 }} color={c.method.startsWith('llm') ? 'blue' : 'orange'}>{c.method}</Tag>}
              <div style={{ color: 'var(--c-ink-2)', margin: '2px 0' }}>{c.summary}</div>
              <Space size={4} wrap>
                {c.members.map((m) => <Tag key={m} style={{ margin: 1, fontSize: 10 }}>{m}</Tag>)}
              </Space>
            </li>
          ))}
        </ul>
      )}

      <div style={{ display: 'flex', gap: 8, marginTop: 6 }}>
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onPressEnter={ask}
          placeholder="全局性问题，如「这个库整体在讲什么」…"
          disabled={comms.length === 0}
        />
        <Button type="primary" onClick={ask} disabled={comms.length === 0}>全局问答</Button>
      </div>
      {degraded && (
        <Alert type="warning" showIcon style={{ marginTop: 8 }} message={degradedMsg || '社区摘要未就绪'} />
      )}
      {hits && (
        hits.length === 0 ? (
          <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 8 }}>未命中社区摘要，请调整问题或先重建。</Typography.Text>
        ) : (
          <ul style={{ margin: '8px 0 0', paddingLeft: 18 }}>
            {hits.map((h, i) => (
              <li key={i} style={{ fontSize: 12, marginBottom: 6 }}>
                <Tag color="purple" style={{ marginInlineEnd: 6 }}>「{h.label}」社区 · 分 {h.score}</Tag>
                {h.summary}
                <div><Typography.Text type="secondary" style={{ fontSize: 11 }}>成员：{h.members.join('、')}</Typography.Text></div>
              </li>
            ))}
          </ul>
        )
      )}
    </Card>
  )
}
