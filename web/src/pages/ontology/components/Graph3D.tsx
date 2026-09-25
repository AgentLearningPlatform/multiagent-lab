import { useEffect, useMemo, useRef, useState } from 'react'
import { Alert, Button, Card, Empty, Input, Select, Space, Tag, Typography } from 'antd'
// M21/VIZ-1：经本地 UMD 分发注入（web/public/vendor/，随镜像分发）——Vite ESM 打包链下
// Kapsule 工厂与 React 18 StrictMode 组合曾出现静默不注入；UMD + window 全局实测稳定
import type { Spec } from '../../../api/types'

// ---------------------------------------------------------------------------
// M21/VIZ-1 三维浏览（REQ-154，3d-force-graph WebGL 只读沉浸视图；调研 §2 基线）：
//   spec_json → {nodes, links}——Concept 球体（按顶层根着色）/ Instance 八面体（继承概念色）/
//   关系边（label 悬浮）。点击聚焦飞入 + 邻居高亮其余淡出 + 侧栏属性；搜索定位；双击空白/复位回全景。
//   只读边界：编辑永远回 React Flow GraphEditor（REQ-71）；两视图数据同源 spec_json 零同步问题。
// ---------------------------------------------------------------------------

/** 调色板：顶层根概念（无父或父不在集内）依次取色；子概念/实例继承根色 */
const PALETTE = ['#4f46e5', '#0891b2', '#ca8a04', '#dc2626', '#16a34a', '#9333ea', '#ea580c', '#0d9488']

/** spec → 三维图数据（节点：概念+实例；边：继承/关系/实例归属/实例关系） */
function buildGraphData(spec: Spec) {
  const concepts = spec.concepts ?? []
  const names = new Set(concepts.map((c) => c.name))
  const nodes: { id: string; kind: 'concept' | 'instance'; name: string; label: string; color: string; concept?: string; definition?: string; attributes?: Record<string, unknown> }[] = []
  const links: { source: string; target: string; kind: 'parent' | 'rel' | 'instance' | 'instrel'; label: string }[] = []

  // 顶层根 → 颜色映射（子概念继承根色）
  const roots = concepts.filter((c) => !(c.parents ?? []).some((p) => names.has(p)))
  const rootColor = new Map<string, string>()
  roots.forEach((c, i) => rootColor.set(c.name, PALETTE[i % PALETTE.length]))
  const colorOf = (name: string): string => {
    if (rootColor.has(name)) return rootColor.get(name)!
    const c = concepts.find((x) => x.name === name)
    if (!c) return '#6b7280'
    for (const p of c.parents ?? []) {
      if (names.has(p)) return colorOf(p)
    }
    return '#6b7280'
  }

  for (const c of concepts) {
    nodes.push({ id: `c:${c.name}`, kind: 'concept', name: c.name, label: c.label || c.name, color: colorOf(c.name), definition: c.definition })
  }
  for (const c of concepts) {
    for (const p of c.parents ?? []) {
      if (names.has(p)) links.push({ source: `c:${c.name}`, target: `c:${p}`, kind: 'parent', label: '继承' })
    }
  }
  for (const r of spec.relations ?? []) {
    if (names.has(r.from) && names.has(r.to)) {
      links.push({ source: `c:${r.from}`, target: `c:${r.to}`, kind: 'rel', label: r.label || r.name })
    }
  }
  for (const inst of spec.instances ?? []) {
    nodes.push({
      id: `i:${inst.name}`, kind: 'instance', name: inst.name, label: inst.name,
      color: names.has(inst.concept) ? colorOf(inst.concept) : '#6b7280',
      concept: inst.concept, attributes: inst.attributes,
    })
    if (names.has(inst.concept)) links.push({ source: `i:${inst.name}`, target: `c:${inst.concept}`, kind: 'instance', label: '属于' })
    for (const rel of inst.relations ?? []) {
      const t = spec.instances?.find((x) => x.name === inst.name) // 占位：target 为实例名
      if (t && spec.instances.some((x) => x.name === rel.target)) {
        links.push({ source: `i:${inst.name}`, target: `i:${rel.target}`, kind: 'instrel', label: rel.rel })
      }
    }
  }
  return { nodes, links }
}

/** 节点几何：概念=球体 / 实例=八面体（调研 §2 数据映射规范） */
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


export default function Graph3D({ spec }: { spec: Spec | null }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const graphRef = useRef<any>(null)
  const [selected, setSelected] = useState<{ kind: 'concept' | 'instance'; name: string; label: string; color: string; definition?: string; concept?: string; attributes?: Record<string, unknown> } | null>(null)
  const [query, setQuery] = useState('')
  const [kindFilter, setKindFilter] = useState<'all' | 'concept' | 'instance'>('all')
  const [initErr, setInitErr] = useState<string | null>(null)

  const data = useMemo(() => (spec ? buildGraphData(spec) : { nodes: [], links: [] }), [spec])
  const counts = useMemo(() => {
    const m = new Map<string, number>()
    for (const i of spec?.instances ?? []) m.set(i.concept, (m.get(i.concept) ?? 0) + 1)
    return m
  }, [spec])

  // 邻居集（聚焦高亮用）
  const neighbors = useMemo(() => {
    const m = new Map<string, Set<string>>()
    for (const l of data.links) {
      if (!m.has(l.source)) m.set(l.source, new Set())
      if (!m.has(l.target)) m.set(l.target, new Set())
      m.get(l.source)!.add(l.target)
      m.get(l.target)!.add(l.source)
    }
    return m
  }, [data])

  const hasConcepts = !!spec && (spec.concepts?.length ?? 0) > 0

  // 初始化 ForceGraph3D（一次）；spec 变化经 graphData 重灌。
  // init 错误显性化（10a 教训：静默失败只剩空白容器无从排查）。
  // 注意：React 18 StrictMode 开发态双挂载——cleanup 不调用 _destructor（它会清空容器 DOM，
  // 二次挂载时 ForceGraph3D 注入的 DOM 已被抽走导致空白画布）；卸载清理由父容器替换 DOM 承担。
  useEffect(() => {
    let cancelled = false
    ;(async () => {
    // graphRef 有实例：StrictMode 二次挂载（容器被重建）——把画布 DOM 搬回新容器
    if (graphRef.current) {
      if (containerRef.current) {
        const dom = graphRef.current.renderer?.().domElement
        if (dom && dom.parentElement !== containerRef.current) containerRef.current.appendChild(dom)
      }
      return
    }
    try {
      await loadScript('/vendor/3d-force-graph.min.js')
      if (!window.ForceGraph3D) throw new Error('3d-force-graph 分发加载失败（/vendor/3d-force-graph.min.js）')
      if (cancelled || !containerRef.current) return
      // 容器在 Tabs 切换瞬间可能尺寸塌陷（0x0 导致 renderer 初始化无效且无报错）——延后一帧等布局
      const w = containerRef.current.clientWidth
      const h = containerRef.current.clientHeight
      // 1.80 起为 class 模式：必须 new（普通调用 classMode=false 不执行 initStatic，静默空容器）
      const graph: any = new (window.ForceGraph3D as any)(containerRef.current)
        .width(w > 0 ? w : undefined)
        .height(h > 0 ? h : undefined)
      graph
        .backgroundColor('rgba(0,0,0,0)')
        .showNavInfo(false)
        .nodeLabel((n: any) => n.label)
        .nodeThreeObjectExtend(true)
        .nodeThreeObject((n: any) => {
          const T = (window as any).THREE
          if (!T) return undefined
          const color = new T.Color(n.color)
          const geo = n.kind === 'concept' ? new T.SphereGeometry(5, 16, 12) : new T.OctahedronGeometry(5)
          return new T.Mesh(geo, new T.MeshLambertMaterial({ color, transparent: true, opacity: 0.92 }))
        })
        .nodeColor((n: any) => n.color)
        .nodeVal((n: any) => (n.kind === 'concept' ? 6 : 2.5))
        .linkColor((l: any) => (l.kind === 'parent' ? 'rgba(120,128,160,0.5)' : 'rgba(150,158,190,0.32)'))
        .linkWidth(0.6)
        .linkDirectionalArrowLength(3)
        .linkLabel((l: any) => l.label)
        .linkDirectionalParticles((l: any) => (l.kind === 'rel' ? 2 : 0))
        .linkDirectionalParticleWidth(1.4)
        .onNodeClick((n: any) => {
          setSelected({ kind: n.kind, name: n.name, label: n.label, color: n.color, definition: n.definition, concept: n.concept, attributes: n.attributes })
          // 聚焦飞入（相机距离拉近）
          const dist = 90
          graphRef.current?.cameraPosition({ x: n.x + dist, y: n.y + dist / 2, z: n.z + dist }, n, 900)
          highlight(n)
        })
        .onBackgroundClick(() => {
          setSelected(null)
          highlight(null)
        })
      if (cancelled) return
      graphRef.current = graph
      graph.graphData(data as any)
    } catch (e: any) {
      if (!cancelled) setInitErr(e?.message ?? String(e))
    }
  })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // spec 变化重灌数据
  useEffect(() => {
    graphRef.current?.graphData(data as any)
    setSelected(null)
  }, [data])

  // 邻居高亮/淡出（透明度梯度）
  const highlight = (n: { id?: string } | null) => {
    const g = graphRef.current as any
    if (!g) return
    if (!n) {
      g.nodeOpacity(0.92)
      g.linkOpacity(0.32)
      return
    }
    const keep = neighbors.get(String(n.id)) ?? new Set()
    g.nodeOpacity((x: any) => (x.id === n.id || keep.has(x.id) ? 0.98 : 0.12))
    g.linkOpacity((l: any) => (l.source.id === n.id || l.target.id === n.id ? 0.85 : 0.04))
  }

  // 过滤器：按节点类型显隐
  useEffect(() => {
    const g = graphRef.current as any
    if (!g) return
    g.nodeVisibility((n: any) => kindFilter === 'all' || n.kind === kindFilter)
    g.linkVisibility((l: any) => {
      if (kindFilter === 'all') return true
      const sn = (l.source as any)?.kind ?? (l.source as string)
      const tn = (l.target as any)?.kind ?? (l.target as string)
      return kindFilter === 'concept' ? sn === 'concept' && tn === 'concept' : true
    })
  }, [kindFilter, data])

  console.log('[Graph3D] render: initErr=', initErr, 'hasConcepts=', hasConcepts, 'nodes=', data.nodes.length)
  if (initErr) {
    return (
      <Alert type="warning" showIcon message="三维视图初始化失败" description={initErr + '（WebGL 不可用或驱动限制时可回退 2D 结构视图）'} />
    )
  }
  if (!hasConcepts || !spec) {
    return (
      <div className="work-empty" style={{ minHeight: 220 }}>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无概念可三维可视化；请先保存含概念的 Spec" />
      </div>
    )
  }

  // 搜索定位：名称精确/前缀/包含命中第一个，聚焦飞入
  const locate = () => {
    const q = query.trim().toLowerCase()
    if (!q) return
    const n = data.nodes.find((x) => x.name.toLowerCase() === q) ?? data.nodes.find((x) => x.name.toLowerCase().startsWith(q)) ?? data.nodes.find((x) => x.label.toLowerCase().includes(q))
    if (!n) {
      return
    }
    const g = graphRef.current
    if (g) {
      const target = (g.graphData().nodes as any[]).find((x) => x.id === n.id)
      if (target) {
        g.cameraPosition({ x: target.x + 90, y: target.y + 45, z: target.z + 90 }, target, 900)
        setSelected({ kind: target.kind, name: target.name, label: target.label, color: target.color, definition: target.definition, concept: target.concept, attributes: target.attributes })
        highlight(target)
      }
    }
  }

  const conceptOfSelected = selected?.concept ? spec.concepts.find((c) => c.name === selected.concept) ?? null : null
  const attrs = selected?.attributes ? Object.entries(selected.attributes) : []

  return (
    <div style={{ display: 'flex', gap: 12, minHeight: 480 }}>
      <div style={{ flex: 1, minWidth: 0, position: 'relative' }}>
        <div style={{ position: 'absolute', zIndex: 5, top: 8, left: 8, right: 8, display: 'flex', gap: 6 }}>
          <Space.Compact style={{ flex: 1, maxWidth: 320 }}>
            <Input
              size="small"
              placeholder="搜索概念/实例定位并聚焦…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onPressEnter={locate}
              allowClear
            />
            <Button size="small" onClick={locate}>定位</Button>
          </Space.Compact>
          <Select
            size="small"
            style={{ width: 130 }}
            value={kindFilter}
            onChange={(v) => setKindFilter(v)}
            options={[
              { value: 'all', label: '全部节点' },
              { value: 'concept', label: '仅概念' },
              { value: 'instance', label: '仅实例' },
            ]}
          />
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
        <div ref={containerRef} style={{ width: '100%', height: 520, borderRadius: 8, background: 'linear-gradient(180deg,#f2f4fb 0%,#e8ebf5 100%)' }} />
        <div style={{ position: 'absolute', zIndex: 5, bottom: 8, left: 10, fontSize: 11, color: 'var(--ant-color-text-tertiary, #888)' }}>
          拖拽旋转 · 滚轮缩放 · 点击节点聚焦飞入（邻居高亮）· 双击空白复位 · 标签悬停可见
        </div>
      </div>
      <Card size="small" style={{ width: 280, flexShrink: 0, overflowY: 'auto', maxHeight: 560 }}>
        <div className="onto-flow-info-title">图例</div>
        <div className="onto-flow-legend"><span className="onto-flow-legend-badge" style={{ borderRadius: '50%', background: '#4f46e5' }} />概念（球体，按顶层根着色）</div>
        <div className="onto-flow-legend"><span className="onto-flow-legend-badge" style={{ transform: 'rotate(45deg)', background: '#9333ea' }} />实例（八面体，继承概念色）</div>
        <div className="onto-flow-legend"><span className="onto-flow-legend-line rel" />实线 = 关系（粒子流向）</div>
        <div className="onto-flow-legend"><span className="onto-flow-legend-line parent" />暗线 = 继承 / 属于</div>

        <div className="onto-flow-info-title spaced">统计</div>
        <div className="onto-flow-stats">
          <span>概念 <b>{spec.concepts.length}</b></span>
          <span>实例 <b>{spec.instances?.length ?? 0}</b></span>
          <span>关系 <b>{spec.relations?.length ?? 0}</b></span>
        </div>

        <div className="onto-flow-info-title spaced">选中节点</div>
        {selected ? (
          <div className="onto-flow-detail">
            <div className="onto-flow-detail-name">
              <Tag color={selected.kind === 'concept' ? 'geekblue' : 'purple'} style={{ marginInlineEnd: 6 }}>{selected.kind === 'concept' ? '概念' : '实例'}</Tag>
              {selected.label}
            </div>
            <div className="onto-flow-detail-key">{selected.name}</div>
            {selected.definition && <p className="onto-flow-detail-def">{selected.definition}</p>}
            {selected.kind === 'instance' && (
              <>
                <div className="onto-flow-detail-row">
                  <span className="onto-flow-detail-label">所属概念</span>
                  <Tag style={{ margin: 0 }} color="geekblue">{conceptOfSelected?.label || selected.concept}</Tag>
                </div>
                {attrs.length > 0 && (
                  <div className="onto-flow-detail-row">
                    <span className="onto-flow-detail-label">属性</span>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                      {attrs.slice(0, 10).map(([k, v]) => (
                        <Typography.Text key={k} style={{ fontSize: 12 }}>{k}: {String(v)}</Typography.Text>
                      ))}
                    </div>
                  </div>
                )}
              </>
            )}
            {selected.kind === 'concept' && (
              <div className="onto-flow-detail-row">
                <span className="onto-flow-detail-label">实例</span>
                <Typography.Text style={{ fontSize: 12 }}>{counts.get(selected.name) ?? 0} 个</Typography.Text>
              </div>
            )}
          </div>
        ) : (
          <p className="onto-flow-hint">点击节点聚焦飞入并查看属性；搜索框可定位实体；「复位全景」回到整体视野。</p>
        )}
      </Card>
    </div>
  )
}
