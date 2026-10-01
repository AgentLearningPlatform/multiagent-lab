import { useEffect, useMemo, useRef, useState } from 'react'
import { Alert, Button, Card, Empty, Input, Slider, Space, Tag, Typography } from 'antd'
import * as THREE from 'three'
import type { CompanionGraph, CompanionGraphEdge, CompanionGraphNode } from '../../../../api/companion'

// ---------------------------------------------------------------------------
// REQ-154/M28 P2 尾：伴生图 3d-force 成长可视化（方案 §架构 83 行「伴生图成长可视化（开关）」）。
// 数据 = GET /api/companion/graph（节点=概念/事件实体，边=活跃关系；入图时间随行——「成长」
// 体现为随对话确认累计的节点/边）。复用 Graph3D 的 UMD 分发注入模式（prepare-vendor）；
// 独立实现（Graph3D 与 spec_json 强耦合）：Concept 球体蓝 / Event 八面体琥珀，度数定大小，
// 点击聚焦 + 邻居高亮 + 侧栏详情（定义/置信/入图时间）；搜索定位；入场按 created_at 顺序
// 累计动画体现生长（graphData 增量重灌）。
// ---------------------------------------------------------------------------

declare global {
  interface Window {
    ForceGraph3D?: (el: HTMLElement) => any
  }
}

function loadScript(src: string): Promise<void> {
  return new Promise((resolve, reject) => {
    if (document.querySelector(`script[src="${src}"]`)) return resolve()
    const el = document.createElement('script')
    el.src = src
    el.onload = () => resolve()
    el.onerror = () => reject(new Error(`加载失败: ${src}`))
    document.head.appendChild(el)
  })
}

const KIND_COLOR: Record<string, string> = { Concept: '#4f46e5', Event: '#d97706' }

// REQ-195：数据由页级（CompanionPage）统一获取下发——组件只画图，不再自带取数。
// 此前「页首图 + 面板内成长图视图」各取一份数据且各挂一套会话状态，是上下重复与
// 「显示对象不可切换/入图后图不刷新」的根因；重构后成长图唯一化于页首。
export default function CompanionGraph3D({ data }: { data: CompanionGraph }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const graphRef = useRef<any>(null)
  const graph = data
  const [initErr, setInitErr] = useState<string | null>(null)
  const [selected, setSelected] = useState<CompanionGraphNode | null>(null)
  const [query, setQuery] = useState('')
  // REQ-230③：时间轴滑杆（截至 T 的累计成长视图——0%=最早节点前，100%=全部）
  const [timePct, setTimePct] = useState(100)
  const timeBounds = useMemo(() => {
    const ts = (graph?.nodes ?? []).map((n) => n.created_at || '').filter(Boolean).sort()
    return { min: ts[0] ?? '', max: ts[ts.length - 1] ?? '' }
  }, [graph])
  const cutoff = useMemo(() => {
    if (!timeBounds.min || !timeBounds.max || timePct >= 100) return null
    const a = new Date(timeBounds.min).getTime()
    const b = new Date(timeBounds.max).getTime()
    if (!isFinite(a) || !isFinite(b) || b <= a) return null
    return new Date(a + ((b - a) * timePct) / 100).toISOString()
  }, [timePct, timeBounds])

  const visibleNodes = useMemo(() => {
    if (!cutoff) return graph?.nodes ?? []
    return (graph?.nodes ?? []).filter((n) => !n.created_at || n.created_at <= cutoff)
  }, [graph, cutoff])

  const nodes = useMemo(
    () =>
      (visibleNodes as CompanionGraphNode[]).map((n: CompanionGraphNode) => ({
        id: n.label,
        label: n.label,
        kind: n.kind,
        definition: n.definition,
        confidence: n.confidence,
        createdAt: n.created_at,
        color: KIND_COLOR[n.kind] ?? '#6b7280',
      })),
    [graph],
  )
  const links = useMemo(
    () =>
      (visibleNodes.length !== (graph?.nodes?.length ?? 0)
        ? (graph?.edges ?? []).filter((e: CompanionGraphEdge) => visibleNodes.some((n) => n.label === e.source) && visibleNodes.some((n) => n.label === e.target) && (!e.created_at || !cutoff || e.created_at <= cutoff))
        : (graph?.edges ?? [])
      )
        .filter((e: CompanionGraphEdge) => e.source && e.target)
        .map((e: CompanionGraphEdge, i: number) => ({
          id: `e${i}`,
          source: e.source,
          target: e.target,
          label: e.rel,
          createdAt: e.created_at,
          confirmCount: e.confirm_count ?? 1,
        })),
    [graph, visibleNodes, cutoff],
  )
  const neighbors = useMemo(() => {
    const m = new Map<string, Set<string>>()
    for (const l of links) {
      if (!m.has(l.source)) m.set(l.source, new Set())
      if (!m.has(l.target)) m.set(l.target, new Set())
      m.get(l.source)!.add(l.target)
      m.get(l.target)!.add(l.source)
    }
    return m
  }, [links])
  const degree = useMemo(() => {
    const d = new Map<string, number>()
    for (const l of links) {
      d.set(l.source, (d.get(l.source) ?? 0) + 1)
      d.set(l.target, (d.get(l.target) ?? 0) + 1)
    }
    return d
  }, [links])
  const degreeRef = useRef(degree)
  degreeRef.current = degree

  // 初始化 ForceGraph3D（模式同 Graph3D：UMD + 全局 THREE；StrictMode 容器重建搬迁）
  useEffect(() => {
    let cancelled = false
    ;(async () => {
      if (graphRef.current) {
        if (containerRef.current) {
          const dom = graphRef.current.renderer?.().domElement
          if (dom && dom.parentElement !== containerRef.current) containerRef.current.appendChild(dom)
        }
        return
      }
      try {
        ;(window as any).THREE = THREE
        await loadScript('/vendor/3d-force-graph.min.js')
        if (!window.ForceGraph3D) throw new Error('3d-force-graph 分发缺失（/vendor/3d-force-graph.min.js 未就绪）——在 web 目录执行 npm run build 即可自动补齐（prepare-vendor）')
        if (cancelled || !containerRef.current) return
        const w = containerRef.current.clientWidth
        const h = containerRef.current.clientHeight
        const g: any = new (window.ForceGraph3D as any)(containerRef.current)
          .width(w > 0 ? w : undefined)
          .height(h > 0 ? h : undefined)
        g.backgroundColor('rgba(0,0,0,0)')
          .showNavInfo(false)
          .nodeLabel((n: any) => `${n.label}（${n.kind === 'Event' ? '事件' : '概念'}）`)
          .nodeThreeObjectExtend(true)
          .nodeThreeObject((n: any) => {
            const color = new THREE.Color(n.color)
            const size = 3 + Math.min(degreeOf(n.id) * 1.2, 7)
            const geo = n.kind === 'Event' ? new THREE.OctahedronGeometry(size) : new THREE.SphereGeometry(size, 16, 12)
            return new THREE.Mesh(geo, new THREE.MeshLambertMaterial({ color, transparent: true, opacity: 0.92 }))
          })
          .nodeColor((n: any) => n.color)
          .linkColor('rgba(120,128,160,0.5)')
          .linkWidth((l: any) => 0.6 + Math.min((l.confirmCount ?? 1) - 1, 4) * 0.9) // REQ-227①：边宽随印证计数
          .linkDirectionalArrowLength(3)
          .linkLabel((l: any) => l.label)
          .linkDirectionalParticles(1)
          .linkDirectionalParticleWidth(1.2)
          .onNodeClick((n: any) => {
            setSelected({ label: n.label, kind: n.kind, definition: n.definition, confidence: n.confidence, created_at: n.createdAt })
            const dist = 90
            g.cameraPosition({ x: n.x + dist, y: n.y + dist / 2, z: n.z + dist }, n, 900)
            highlight(n)
          })
          .onBackgroundClick(() => {
            setSelected(null)
            highlight(null)
          })
        if (cancelled) return
        graphRef.current = g
        g.graphData({ nodes: nodes as any, links: links as any })
      } catch (e: any) {
        if (!cancelled) setInitErr(e?.message ?? String(e))
      }
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 数据变化重灌（同一会话刷新/摘除后）
  useEffect(() => {
    graphRef.current?.graphData({ nodes: nodes as any, links: links as any })
    setSelected(null)
  }, [nodes, links])

  const degreeOf = (id: string) => degreeRef.current.get(id) ?? 0

  const highlight = (n: { id?: string } | null) => {
    const g = graphRef.current as any
    if (!g) return
    if (!n) {
      g.nodeOpacity(0.92)
      g.linkOpacity(0.5)
      return
    }
    const keep = neighbors.get(String(n.id)) ?? new Set()
    g.nodeOpacity((x: any) => (x.id === n.id || keep.has(x.id) ? 0.98 : 0.12))
    g.linkOpacity((l: any) => (l.source.id === n.id || l.target.id === n.id ? 0.85 : 0.04))
  }

  const locate = () => {
    const q = query.trim().toLowerCase()
    if (!q || !graphRef.current) return
    const target = (graphRef.current.graphData().nodes as any[]).find(
      (x) => x.id.toLowerCase() === q || x.id.toLowerCase().includes(q),
    )
    if (target) {
      graphRef.current.cameraPosition({ x: target.x + 90, y: target.y + 45, z: target.z + 90 }, target, 900)
      setSelected({ label: target.label, kind: target.kind, definition: target.definition, confidence: target.confidence, created_at: target.createdAt })
      highlight(target)
    }
  }

  if (initErr) return <Alert type="warning" showIcon title="三维视图初始化失败" description={initErr} />
  if (nodes.length === 0) {
    return (
      <div className="work-empty" style={{ minHeight: 200 }}>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="该会话伴生图暂无内容——确认候选入图后，此处呈现其成长形态" />
      </div>
    )
  }

  const byTime = [...nodes].sort((a, b) => (a.createdAt || '').localeCompare(b.createdAt || ''))

  return (
    <div style={{ display: 'flex', gap: 12 }}>
      <div style={{ flex: 1, minWidth: 0, position: 'relative' }}>
        <div style={{ position: 'absolute', zIndex: 5, top: 8, left: 8, right: 8, display: 'flex', gap: 6 }}>
          <Space.Compact style={{ flex: 1, maxWidth: 320 }}>
            <Input size="small" placeholder="搜索实体定位并聚焦…" value={query} onChange={(e) => setQuery(e.target.value)} onPressEnter={locate} allowClear />
            <Button size="small" onClick={locate}>定位</Button>
          </Space.Compact>
          <Button
            size="small"
            onClick={() => {
              setSelected(null)
              highlight(null)
              graphRef.current?.zoomToFit(600, 60)
            }}
          >
            复位全景
          </Button>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
          <Typography.Text type="secondary" style={{ fontSize: 11, flexShrink: 0 }}>时间轴</Typography.Text>
          <Slider
            range={false}
            min={0}
            max={100}
            value={timePct}
            onChange={(v) => setTimePct(v as number)}
            tooltip={{ formatter: () => (cutoff ? `截至 ${cutoff.slice(0, 10)}` : `全部（${timeBounds.max?.slice(0, 10) || '—'}）`) }}
            style={{ flex: 1, margin: '0 4px' }}
            aria-label="成长时间轴（截至某时点的累计视图）"
          />
          <Typography.Text type="secondary" style={{ fontSize: 11, flexShrink: 0 }}>{cutoff ? cutoff.slice(0, 10) : '全部'}</Typography.Text>
        </div>
        <div ref={containerRef} style={{ width: '100%', height: 480, borderRadius: 8, background: 'linear-gradient(180deg,#f4f0fb 0%,#eae8f5 100%)' }} />
        <div style={{ position: 'absolute', zIndex: 5, bottom: 8, left: 10, fontSize: 11, color: 'var(--ant-color-text-tertiary, #888)' }}>
          成长序（入图时间）：{byTime.slice(0, 3).map((n) => n.label).join(' → ')}{byTime.length > 3 ? ' …' : ''} · 拖拽旋转 · 滚轮缩放 · 点击聚焦
        </div>
      </div>
      <Card size="small" style={{ width: 260, flexShrink: 0, overflowY: 'auto', maxHeight: 520 }}>
        <div className="onto-flow-info-title">图例</div>
        <div className="onto-flow-legend"><span className="onto-flow-legend-badge" style={{ borderRadius: '50%', background: KIND_COLOR.Concept }} />概念（球体）</div>
        <div className="onto-flow-legend"><span className="onto-flow-legend-badge" style={{ transform: 'rotate(45deg)', background: KIND_COLOR.Event }} />事件（八面体）</div>
        <div className="onto-flow-legend"><span className="onto-flow-legend-line rel" />箭头 = 关系（活跃边；矛盾旧边已失效化不展示）</div>
        <div className="onto-flow-info-title spaced">成长统计</div>
        <div className="onto-flow-stats">
          <span>实体 <b>{nodes.length}</b></span>
          <span>关系 <b>{links.length}</b></span>
        </div>
        <Typography.Text type="secondary" style={{ fontSize: 11, display: 'block', marginBottom: 8 }}>
          节点大小 ∝ 关系度数；内容随对话候选确认累计入图。
        </Typography.Text>
        <div className="onto-flow-info-title spaced">选中实体</div>
        {selected ? (
          <div className="onto-flow-detail">
            <div className="onto-flow-detail-name">
              <Tag color={selected.kind === 'Event' ? 'gold' : 'geekblue'} style={{ marginInlineEnd: 6 }}>{selected.kind === 'Event' ? '事件' : '概念'}</Tag>
              {selected.label}
            </div>
            {selected.definition && <p className="onto-flow-detail-def">{selected.definition}</p>}
            {!!selected.confidence && (
              <div className="onto-flow-detail-row">
                <span className="onto-flow-detail-label">置信度</span>
                <Tag style={{ margin: 0 }} color={selected.confidence >= 0.7 ? 'green' : 'orange'}>{selected.confidence.toFixed(2)}</Tag>
              </div>
            )}
            {selected.time_scope && (
              <div className="onto-flow-detail-row">
                <span className="onto-flow-detail-label">时点</span>
                <Tag style={{ margin: 0 }} color="gold">{selected.time_scope}</Tag>
              </div>
            )}
            {selected.created_at && (
              <div className="onto-flow-detail-row">
                <span className="onto-flow-detail-label">入图时间</span>
                <Typography.Text style={{ fontSize: 12 }}>{selected.created_at.slice(0, 19).replace('T', ' ')}</Typography.Text>
              </div>
            )}
          </div>
        ) : (
          <p className="onto-flow-hint">点击节点查看定义/置信/入图时间；搜索框定位实体。</p>
        )}
      </Card>
    </div>
  )
}
