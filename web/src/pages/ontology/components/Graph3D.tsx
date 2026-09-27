import { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Card, Empty, Input, Select, Space, Tag, Typography } from 'antd'
import * as THREE from 'three'
import ForceGraph3D from 'react-force-graph-3d'
// M21/VIZ-2 R0（15 号 v2.15）：react-force-graph-3d ESM 直装——消三 hack：
//   ①UMD vendor 分发（prepare-vendor 补给链退役）②window.THREE 预挂（ESM 直接共享 three 实例）
//   ③StrictMode 容器 DOM 搬移（React 组件生命周期自管）
import type { Spec } from '../../../api/types'

// ---------------------------------------------------------------------------
// M21/VIZ-1+VIZ-2 三维浏览（REQ-154/D-O18）：spec_json → {nodes, links}——
// 概念球体（按顶层根着色）/ 实例八面体（继承概念色）/ 关系边（label 悬浮）。
// VIZ-2 R1 渲染管线（D-O18 v0.46 排期注）：
//   ①几何共享池化——单位球/八面体各一份，mesh.scale 按节点缩放（不再每节点 new Geometry）；
//   ②材质按 kind+color 池化（MeshLambertMaterial 复用）；
//   ③linkWidth 0 走 LineSegments 快路径——箭头/粒子仅在边数低于阈值时启用（万级边第一瓶颈）；
//   ④聚焦高亮增量刷新——直接改写 __threeObj.scale（非邻居收缩 0.25x），不再全图 opacity accessor 重刷；
//   ⑤ResizeObserver 容器尺寸跟随 + onEngineStop 首挂尺寸校正；
//   ⑥buildGraphData 清理（根色 DFS 记忆化、删占位自查找）。
// R2 布局切换：力导向（默认）/ 根向分簇 / 层次分层（d3-force-3d fz 定轴）。
// 只读边界：编辑永远回 React Flow GraphEditor（REQ-71）。
// ---------------------------------------------------------------------------

const PALETTE = ['#4f46e5', '#0891b2', '#ca8a04', '#dc2626', '#16a34a', '#9333ea', '#ea580c', '#0d9488']
/** R1③：边装饰（箭头/粒子）启用的边数阈值——超过走纯 LineSegments 快路径 */
const LINK_DECOR_THRESHOLD = 800

interface GNode {
  id: string
  kind: 'concept' | 'instance'
  name: string
  label: string
  color: string
  concept?: string
  definition?: string
  attributes?: Record<string, unknown>
  /** R1①：渲染半径 */
  radius: number
}
interface GLink {
  source: string
  target: string
  kind: 'parent' | 'rel' | 'instance' | 'instrel'
  label: string
}

/** spec → 三维图数据（R1⑥：根色/颜色 DFS 记忆化；删占位自查找） */
function buildGraphData(spec: Spec) {
  const concepts = spec.concepts ?? []
  const names = new Set(concepts.map((c) => c.name))
  const conceptBy = new Map(concepts.map((c) => [c.name, c]))
  const nodes: GNode[] = []
  const links: GLink[] = []

  const rootColor = new Map<string, string>()
  const colorMemo = new Map<string, string>()
  for (const c of concepts) {
    if (!(c.parents ?? []).some((p) => names.has(p))) rootColor.set(c.name, PALETTE[rootColor.size % PALETTE.length])
  }
  const colorOf = (name: string): string => {
    const hit = colorMemo.get(name)
    if (hit) return hit
    let color = '#6b7280'
    const c = conceptBy.get(name)
    for (const p of c?.parents ?? []) {
      if (names.has(p)) {
        color = colorOf(p)
        break
      }
    }
    colorMemo.set(name, color)
    return color
  }

  for (const c of concepts) {
    nodes.push({ id: `c:${c.name}`, kind: 'concept', name: c.name, label: c.label || c.name, color: colorOf(c.name), definition: c.definition, radius: 5 })
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
      concept: inst.concept, attributes: inst.attributes, radius: 3.2,
    })
    if (names.has(inst.concept)) links.push({ source: `i:${inst.name}`, target: `c:${inst.concept}`, kind: 'instance', label: '属于' })
    for (const rel of inst.relations ?? []) {
      if (spec.instances.some((x) => x.name === rel.target)) {
        links.push({ source: `i:${inst.name}`, target: `i:${rel.target}`, kind: 'instrel', label: rel.rel })
      }
    }
  }
  return { nodes, links }
}

/** R1②：材质池（kind+color 复用；透明材质） */
const materialPool = new Map<string, THREE.MeshLambertMaterial>()
function materialOf(color: string, kind: string): THREE.MeshLambertMaterial {
  const key = `${kind}:${color}`
  let m = materialPool.get(key)
  if (!m) {
    m = new THREE.MeshLambertMaterial({ color, transparent: true, opacity: 0.92 })
    materialPool.set(key, m)
  }
  return m
}
/** R1①：几何池——单位球/单位八面体各一份，mesh.scale 承载尺寸 */
const sphereGeo = new THREE.SphereGeometry(1, 16, 12)
const octaGeo = new THREE.OctahedronGeometry(1)

export default function Graph3D({ spec }: { spec: Spec | null }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const fgRef = useRef<any>(null)
  const [selected, setSelected] = useState<{ kind: 'concept' | 'instance'; name: string; label: string; color: string; definition?: string; concept?: string; attributes?: Record<string, unknown> } | null>(null)
  const [query, setQuery] = useState('')
  const [kindFilter, setKindFilter] = useState<'all' | 'concept' | 'instance'>('all')
  /** R2：布局模式（力导向=默认 / 根向分簇 / 层次分层） */
  const [layout, setLayout] = useState<'force' | 'cluster' | 'layer'>('force')
  const data = useMemo(() => (spec ? buildGraphData(spec) : { nodes: [], links: [] }), [spec])
  const counts = useMemo(() => {
    const m = new Map<string, number>()
    for (const i of spec?.instances ?? []) m.set(i.concept, (m.get(i.concept) ?? 0) + 1)
    return m
  }, [spec])

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

  // R1①②：自定义节点对象（几何/材质池化；scale 承载半径）
  const nodeThreeObject = (n: any) => {
    const geo = n.kind === 'instance' ? octaGeo : sphereGeo
    const mesh = new THREE.Mesh(geo, materialOf(n.color, n.kind))
    mesh.scale.setScalar(n.radius)
    return mesh
  }

  // R1④：聚焦高亮增量刷新——直接改写 __threeObj.scale（非邻居收缩 0.25x）
  const highlightRef = useRef<any | null>(null)
  const applyHighlight = () => {
    const g = fgRef.current as any
    if (!g || typeof g.graphData !== 'function') return
    const cur = highlightRef.current
    const keep = cur ? (neighbors.get(String(cur.id)) ?? new Set()) : null
    for (const n of g.graphData().nodes as any[]) {
      const obj = n.__threeObj
      if (!obj) continue
      obj.scale.setScalar(cur && n.id !== cur.id && !keep?.has(n.id) ? n.radius * 0.25 : n.radius)
    }
    g.linkOpacity(cur ? 0.85 : 0.32)
  }

  // ref 方法守卫（react-force-graph-3d ref 转发面随版本差异，缺失方法静默跳过）
  const callFg = (method: string, ...args: any[]) => {
    const g = fgRef.current as any
    if (g && typeof g[method] === 'function') {
      return g[method](...args)
    }
  }

  // R2：布局切换——fz 定轴（力导向=自由 z；仅非 force 布局执行）
  useEffect(() => {
    const g = fgRef.current as any
    if (!g || !spec || typeof g.graphData !== 'function') return
    const names = new Set((spec.concepts ?? []).map((c) => c.name))
    const conceptBy = new Map((spec.concepts ?? []).map((c) => [c.name, c]))
    const depthMemo = new Map<string, number>()
    const depthOf = (name: string): number => {
      const hit = depthMemo.get(name)
      if (hit !== undefined) return hit
      const c = conceptBy.get(name)
      let d = 0
      for (const p of c?.parents ?? []) {
        if (names.has(p)) d = Math.max(d, depthOf(p) + 1)
      }
      depthMemo.set(name, d)
      return d
    }
    const roots: string[] = []
    for (const c of spec.concepts ?? []) {
      if (!(c.parents ?? []).some((pp) => names.has(pp))) roots.push(c.name)
    }
    for (const n of g.graphData().nodes as any[]) {
      if (n.kind !== 'concept') {
        n.fz = undefined
        continue
      }
      if (layout === 'layer') {
        n.fz = -depthOf(n.name) * 90 // 层次分层：继承深度定 z 轴
      } else if (layout === 'cluster') {
        let cur: string = n.name
        for (let i = 0; i < 32; i++) {
          const c = conceptBy.get(cur)
          const parent = (c?.parents ?? []).find((pp) => names.has(pp))
          if (!parent) break
          cur = parent
        }
        const ri = roots.indexOf(cur)
        n.fz = ((ri < 0 ? 0 : ri % 8) - 4) * 110 // 根向分簇：8 簇 z 轴分布
      } else {
        n.fz = undefined
      }
    }
    if (typeof g.d3ReheatSimulation === 'function') g.d3ReheatSimulation()
  }, [layout, data, spec])

  // R1⑤：ResizeObserver 容器尺寸跟随
  useEffect(() => {
    const el = containerRef.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => {
      const g = fgRef.current as any
      if (!g || typeof g.width !== 'function') return
      if (el.clientWidth > 0 && el.clientHeight > 0) {
        g.width(el.clientWidth)
        g.height(el.clientHeight)
      }
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  useEffect(() => {
    setSelected(null)
  }, [data])

  const conceptOfSelected = selected?.concept ? spec?.concepts.find((c) => c.name === selected.concept) ?? null : null
  const attrs = selected?.attributes ? Object.entries(selected.attributes) : []

  if (!hasConcepts || !spec) {
    return (
      <div className="work-empty" style={{ minHeight: 220 }}>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无概念可三维可视化；请先保存含概念的 Spec" />
      </div>
    )
  }

  const locate = () => {
    const q = query.trim().toLowerCase()
    if (!q) return
    const n = data.nodes.find((x) => x.name.toLowerCase() === q) ?? data.nodes.find((x) => x.name.toLowerCase().startsWith(q)) ?? data.nodes.find((x) => x.label.toLowerCase().includes(q))
    if (!n) return
    const g = fgRef.current as any
    const target = g && typeof g.graphData === 'function' ? (g.graphData().nodes as any[]).find((x) => x.id === n.id) : null
    if (g && target) {
      g.cameraPosition({ x: target.x + 90, y: target.y + 45, z: target.z + 90 }, target, 900)
      setSelected({ kind: target.kind, name: target.name, label: target.label, color: target.color, definition: target.definition, concept: target.concept, attributes: target.attributes })
    }
  }

  return (
    <div style={{ display: 'flex', gap: 12, minHeight: 480 }}>
      <div style={{ flex: 1, minWidth: 0, position: 'relative' }}>
        <div style={{ position: 'absolute', zIndex: 5, top: 8, left: 8, right: 8, display: 'flex', gap: 6, flexWrap: 'wrap' }}>
          <Space.Compact style={{ flex: 1, maxWidth: 300 }}>
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
            style={{ width: 118 }}
            value={kindFilter}
            onChange={setKindFilter}
            options={[
              { value: 'all', label: '全部节点' },
              { value: 'concept', label: '仅概念' },
              { value: 'instance', label: '仅实例' },
            ]}
          />
          <Select
            size="small"
            style={{ width: 128 }}
            value={layout}
            onChange={setLayout}
            options={[
              { value: 'force', label: '力导向布局' },
              { value: 'cluster', label: '根向分簇' },
              { value: 'layer', label: '层次分层' },
            ]}
          />
          <Button
            size="small"
            onClick={() => {
              setSelected(null)
              highlightRef.current = null
              callFg('zoomToFit', 600, 60)
            }}
          >
            复位全景
          </Button>
        </div>
        <div ref={containerRef} style={{ width: '100%', height: 520, borderRadius: 8, background: 'linear-gradient(180deg,#f2f4fb 0%,#e8ebf5 100%)' }}>
          {hasConcepts && (
            <ForceGraph3D
              ref={fgRef}
              graphData={data as any}
              backgroundColor="rgba(0,0,0,0)"
              showNavInfo={false}
              nodeLabel={(n: any) => n.label}
              nodeThreeObject={nodeThreeObject}
              nodeVal={(n: any) => n.radius}
              linkColor={(l: any) => (l.kind === 'parent' ? 'rgba(120,128,160,0.5)' : 'rgba(150,158,190,0.32)')}
              linkWidth={0}
              linkLabel={(l: any) => l.label}
              linkDirectionalArrowLength={(l: any) => ((data.links.length < LINK_DECOR_THRESHOLD && l.kind !== 'parent') ? 3 : 0)}
              linkDirectionalParticles={(l: any) => ((data.links.length < LINK_DECOR_THRESHOLD && l.kind === 'rel') ? 2 : 0)}
              linkDirectionalParticleWidth={1.4}
              linkOpacity={0.32}
              onNodeClick={(n: any) => {
                setSelected({ kind: n.kind, name: n.name, label: n.label, color: n.color, definition: n.definition, concept: n.concept, attributes: n.attributes })
                highlightRef.current = n
                const dist = 90
                callFg('cameraPosition', { x: n.x + dist, y: n.y + dist / 2, z: n.z + dist }, n, 900)
                applyHighlight()
              }}
              onBackgroundClick={() => {
                setSelected(null)
                highlightRef.current = null
                applyHighlight()
              }}
              onEngineStop={() => {
                const el = containerRef.current
                const g = fgRef.current as any
                if (el && g && typeof g.width === 'function' && el.clientWidth > 0) {
                  g.width(el.clientWidth)
                  g.height(el.clientHeight)
                }
              }}
            />
          )}
        </div>
        <div style={{ position: 'absolute', zIndex: 5, bottom: 8, left: 10, fontSize: 11, color: 'var(--ant-color-text-tertiary, #888)' }}>
          拖拽旋转 · 滚轮缩放 · 点击节点聚焦飞入（邻居保持、其余收缩）· 标签悬停可见
        </div>
      </div>
      <Card size="small" style={{ width: 280, flexShrink: 0, overflowY: 'auto', maxHeight: 560 }}>
        <div className="onto-flow-info-title">图例</div>
        <div className="onto-flow-legend"><span className="onto-flow-legend-badge" style={{ borderRadius: '50%', background: '#4f46e5' }} />概念（球体，按顶层根着色）</div>
        <div className="onto-flow-legend"><span className="onto-flow-legend-badge" style={{ transform: 'rotate(45deg)', background: '#9333ea' }} />实例（八面体，继承概念色）</div>
        <div className="onto-flow-legend"><span className="onto-flow-legend-line rel" />实线 = 关系</div>
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
