import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Button, Card, Checkbox, Empty, Input, Select, Space, Tag, Typography } from 'antd'
import * as THREE from 'three'
import ForceGraph3D from 'react-force-graph-3d'
// M21/VIZ-2 R0（15 号 v2.15）：react-force-graph-3d ESM 直装——消三 hack：
//   ①UMD vendor 分发（prepare-vendor 补给链退役）②window.THREE 预挂（ESM 直接共享 three 实例）
//   ③StrictMode 容器 DOM 搬移（React 组件生命周期自管）
import { ControlOutlined, RightOutlined } from '@ant-design/icons'
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
/** REQ-185③：图例面板宽度三常量与 localStorage 键 */
const LEGEND_WIDTH_KEY = 'eino.viz.legend.width'
const LEGEND_WIDTH_DEFAULT = 280

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
    // 顶层根自身无父级，着色必须先查 rootColor（否则全图回落灰色——VIZ-2 重写回归，2026-09-28 修复）
    const rootHit = rootColor.get(name)
    if (rootHit) {
      colorMemo.set(name, rootHit)
      return rootHit
    }
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
  // 根概念清单（顶层根依出现序着色——与节点 colorOf 同源，图例列表数据源）
  const roots = [...rootColor.keys()].map((name) => ({ name, color: rootColor.get(name)! }))
  return { nodes, links, roots }
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
  const [legendQuery, setLegendQuery] = useState('')
  const [kindFilter, setKindFilter] = useState<'all' | 'concept' | 'instance'>('all')
  /** R2：布局模式（力导向=默认 / 根向分簇 / 层次分层） */
  const [layout, setLayout] = useState<'force' | 'cluster' | 'layer'>('force')
  /** R3：关系类型过滤（可见边 kind 集合） */
  const [linkFilter, setLinkFilter] = useState<Set<string>>(new Set(['parent', 'rel', 'instance', 'instrel']))
  /** R4：雾效开关 */
  const [fog3d, setFog3d] = useState(false)
  // REQ-185①：图例显隐联动——隐藏的根概念集合（其子孙概念+挂载实例+关联边数据级过滤）
  const [hiddenRoots, setHiddenRoots] = useState<Set<string>>(new Set())
  // REQ-185③：图例面板宽度（240~420 拖拽，localStorage 记忆，双击复位 280）与折叠
  const [legendWidth, setLegendWidth] = useState(() => {
    const saved = Number(localStorage.getItem(LEGEND_WIDTH_KEY))
    return saved >= 240 && saved <= 420 ? saved : 280
  })
  const [legendOpen, setLegendOpen] = useState(true)
  const data = useMemo(() => (spec ? buildGraphData(spec) : { nodes: [], links: [], roots: [] }), [spec])
  /** 图例根概念统计（子孙概念数 / 挂载实例数），随 legendQuery 过滤 */
  const rootStats = useMemo(() => {
    const m = new Map<string, { concepts: number; instances: number }>()
    if (!spec) return m
    const names = new Set((spec.concepts ?? []).map((c) => c.name))
    const conceptBy = new Map((spec.concepts ?? []).map((c) => [c.name, c]))
    const rootOf = (name: string): string => {
      let cur = name
      for (let i = 0; i < 32; i++) {
        const c = conceptBy.get(cur)
        const parent = (c?.parents ?? []).find((pp) => names.has(pp))
        if (!parent) return cur
        cur = parent
      }
      return cur
    }
    for (const c of spec.concepts ?? []) {
      const r = rootOf(c.name)
      const e = m.get(r) ?? { concepts: 0, instances: 0 }
      e.concepts++
      m.set(r, e)
    }
    for (const inst of spec.instances ?? []) {
      const e = m.get(rootOf(inst.concept))
      if (e) e.instances++
    }
    return m
  }, [spec])
  const rootsFiltered = useMemo(() => {
    const q = legendQuery.trim().toLowerCase()
    return (data.roots ?? []).filter((r) => !q || r.name.toLowerCase().includes(q))
  }, [data.roots, legendQuery])
  const counts = useMemo(() => {
    const m = new Map<string, number>()
    for (const i of spec?.instances ?? []) m.set(i.concept, (m.get(i.concept) ?? 0) + 1)
    return m
  }, [spec])

  // REQ-185①：name → 顶层根（概念沿 parents 上溯；实例经所属概念）——显隐子树归簇依据
  const rootOfName = useMemo(() => {
    const m = new Map<string, string>()
    if (!spec) return m
    const names = new Set((spec.concepts ?? []).map((c) => c.name))
    const conceptBy = new Map((spec.concepts ?? []).map((c) => [c.name, c]))
    const rootOf = (name: string): string => {
      const hit = m.get(name)
      if (hit) return hit
      let cur = name
      for (let i = 0; i < 32; i++) {
        const c = conceptBy.get(cur)
        const parent = (c?.parents ?? []).find((pp) => names.has(pp))
        if (!parent) break
        cur = parent
      }
      m.set(name, cur)
      return cur
    }
    for (const c of spec.concepts ?? []) rootOf(c.name)
    for (const inst of spec.instances ?? []) if (names.has(inst.concept)) m.set(inst.name, rootOf(inst.concept))
    return m
  }, [spec])

  // REQ-185①：数据级过滤重建 graphData（非视觉遮挡）——隐藏根的子孙概念+挂载实例剔除，边双端可见才保留
  const visibleData = useMemo(() => {
    const nodes = data.nodes.filter((n) => {
      if (kindFilter === 'concept' && n.kind === 'instance') return false
      if (kindFilter === 'instance' && n.kind === 'concept') return false
      const root = rootOfName.get(n.kind === 'concept' ? n.name : (n.concept ?? ''))
      if (root && hiddenRoots.has(root)) return false
      return true
    })
    const ids = new Set(nodes.map((n) => n.id))
    const links = data.links.filter((l) => ids.has(l.source) && ids.has(l.target))
    return { nodes, links, roots: data.roots }
  }, [data, kindFilter, hiddenRoots, rootOfName])

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

  /** R3 路径高亮：概念节点→根的完整继承链（parent 边）+ 自身。返回节点 id 集合 */
  const ancestorPath = useMemo(() => {
    const m = new Map<string, Set<string>>()
    if (!spec) return m
    const conceptBy = new Map((spec.concepts ?? []).map((c) => [c.name, c]))
    const names = new Set((spec.concepts ?? []).map((c) => c.name))
    for (const c of spec.concepts ?? []) {
      const chain = new Set<string>([`c:${c.name}`])
      let cur = c.name
      for (let i = 0; i < 32; i++) {
        const cc = conceptBy.get(cur)
        const parent = (cc?.parents ?? []).find((pp) => names.has(pp))
        if (!parent) break
        chain.add(`c:${parent}`)
        cur = parent
      }
      m.set(`c:${c.name}`, chain)
    }
    return m
  }, [spec])

  const hasConcepts = !!spec && (spec.concepts?.length ?? 0) > 0

  // R1①②：自定义节点对象（几何/材质池化；scale 承载半径）
  const nodeThreeObject = (n: any) => {
    const geo = n.kind === 'instance' ? octaGeo : sphereGeo
    const mesh = new THREE.Mesh(geo, materialOf(n.color, n.kind))
    mesh.scale.setScalar(n.radius)
    return mesh
  }

  // 力布局收敛后的首次全景适配标记（数据/布局变化时重置；修复节点飘出画布视野）
  const firstFitRef = useRef(true)
  // 画布初始像素尺寸（容器测量；ResizeObserver 初始回调早于 graph 内部初始化会被 ref 守卫跳过，
  // 故首挂尺寸经 props 下发——否则 canvas 以窗口尺寸渲染溢出容器）
  const [initSize, setInitSize] = useState({ w: 0, h: 0 })
  useLayoutEffect(() => {
    const el = containerRef.current
    if (el && el.clientWidth > 0 && el.clientHeight > 0) setInitSize({ w: el.clientWidth, h: el.clientHeight })
  }, [])
  // R1④+R3：聚焦高亮增量刷新——非邻居收缩 0.25x；R3 路径高亮模式=点击概念时保留到根的完整继承链
  const highlightRef = useRef<any | null>(null)
  const applyHighlight = () => {
    const g = fgRef.current as any
    if (!g) return
    const cur = highlightRef.current
    let keep: Set<string> | null = null
    if (cur) {
      if (cur.kind === 'concept' && ancestorPath.has(String(cur.id))) {
        // R3 路径高亮：保留到根的完整继承链 + 直接邻居（实例/关系）
        keep = new Set(ancestorPath.get(String(cur.id)) ?? [])
        for (const nb of neighbors.get(String(cur.id)) ?? []) keep.add(nb)
      } else {
        keep = new Set(neighbors.get(String(cur.id)) ?? [])
      }
    }
    // 数据侧遍历：force-graph 就地扩展传入的节点对象（__threeObj 同引用可达）；
    // ref 转发面无 graphData（v1.2x 拆分后数据只走 props），不得经 ref 取图数据
    for (const n of data.nodes as any[]) {
      const obj = n.__threeObj
      if (!obj) continue
      obj.scale.setScalar(cur && n.id !== cur.id && !keep?.has(n.id) ? n.radius * 0.25 : n.radius)
    }
    if (typeof g.linkOpacity === 'function') g.linkOpacity(cur ? 0.85 : 0.32)
  }

  // R4 选中环：TorusGeometry 环绕选中节点
  const ringRef = useRef<THREE.Mesh | null>(null)
  const updateRing = (n: any | null) => {
    const g = fgRef.current as any
    if (!g || typeof g.scene !== 'function') return
    if (ringRef.current) {
      g.scene().remove(ringRef.current)
      ringRef.current = null
    }
    if (!n || !n.__threeObj) return
    const ring = new THREE.Mesh(
      new THREE.TorusGeometry(1.6, 0.15, 8, 32),
      new THREE.MeshBasicMaterial({ color: '#f59e0b', transparent: true, opacity: 0.8 }),
    )
    ring.position.copy(n.__threeObj.position)
    ring.scale.setScalar(n.radius * 1.1)
    ring.lookAt(g.cameraPosition())
    g.scene().add(ring)
    ringRef.current = ring
  }

  // 灯光保障（幂等）：自定义节点为 Mesh + Lambert 材质（受光照驱动），场景无灯或灯弱时全部节点呈灰色。
  // 诊断输出场景灯光与材质样例，用于验证渲染链路。
  useEffect(() => {
    const g = fgRef.current as any
    if (!g || typeof g.scene !== 'function') return
    const scn = g.scene()
    if (!scn) return
    const lights = scn.children.filter((c: any) => c.isLight)
    if (!lights.some((l: any) => l.type === 'AmbientLight')) {
      // 强度按 three r155+ 物理光照单位制取值（×π 等效旧制 1.0；低强度下 Lambert 节点整体发灰），
      // 与 3d-force-graph 库默认灯光同量级（Ambient π / Directional 0.6π）
      scn.add(new THREE.AmbientLight(0xffffff, Math.PI))
      const dir = new THREE.DirectionalLight(0xffffff, 0.6 * Math.PI)
      dir.position.set(300, 500, 200)
      scn.add(dir)
    }
  }, [])

  // ref 方法守卫（react-force-graph-3d ref 转发面随版本差异，缺失方法静默跳过）
  const callFg = (method: string, ...args: any[]) => {
    const g = fgRef.current as any
    if (g && typeof g[method] === 'function') {
      return g[method](...args)
    }
  }

  // REQ-185③：图例面板左缘拖拽（240~420；localStorage 记忆；双击复位）
  const startLegendResize = (e: React.MouseEvent) => {
    e.preventDefault()
    const startX = e.clientX
    const startW = legendWidth
    const onMove = (ev: MouseEvent) => {
      setLegendWidth(Math.min(420, Math.max(240, startW + (startX - ev.clientX))))
    }
    const onUp = () => {
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
      setLegendWidth((w) => {
        localStorage.setItem(LEGEND_WIDTH_KEY, String(w))
        return w
      })
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }
  const resetLegendWidth = () => {
    setLegendWidth(LEGEND_WIDTH_DEFAULT)
    localStorage.setItem(LEGEND_WIDTH_KEY, String(LEGEND_WIDTH_DEFAULT))
  }
  const toggleRoot = (name: string, visible: boolean) => {
    setHiddenRoots((cur) => {
      const next = new Set(cur)
      if (visible) next.delete(name)
      else next.add(name)
      return next
    })
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
    firstFitRef.current = false // 布局切换改变空间分布，收敛后重新全景适配
  }, [layout, data, spec])

  // R1⑤：ResizeObserver 容器尺寸跟随（props 驱动——ref 转发面无 width/height setter，
  // 仅经 props 下发才会触发内部 renderer resize）
  useEffect(() => {
    const el = containerRef.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => {
      if (el.clientWidth > 0 && el.clientHeight > 0) {
        setInitSize((cur) => (cur.w === el.clientWidth && cur.h === el.clientHeight ? cur : { w: el.clientWidth, h: el.clientHeight }))
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

  /** 相机聚焦到指定节点（搜索定位与图例点击共用） */
  const focusNodeById = (id: string) => {
    const g = fgRef.current as any
    const target = g && typeof g.graphData === 'function' ? (g.graphData().nodes as any[]).find((x) => x.id === id) : null
    if (g && target) {
      g.cameraPosition({ x: target.x + 90, y: target.y + 45, z: target.z + 90 }, target, 900)
      setSelected({ kind: target.kind, name: target.name, label: target.label, color: target.color, definition: target.definition, concept: target.concept, attributes: target.attributes })
    }
  }
  const locate = () => {
    const q = query.trim().toLowerCase()
    if (!q) return
    const n = data.nodes.find((x) => x.name.toLowerCase() === q) ?? data.nodes.find((x) => x.name.toLowerCase().startsWith(q)) ?? data.nodes.find((x) => x.label.toLowerCase().includes(q))
    if (n) focusNodeById(n.id)
  }

  // 画布高度随视口自适应（201~? 由 clamp 约束；360 = 资产页头部/页签 chrome 估高，全屏态由 CSS 覆盖）
  const canvasH = 'clamp(480px, calc(100vh - 380px), 1200px)'

  return (
    <div style={{ display: 'flex', gap: 0, alignItems: 'stretch' }}>
      <div style={{ flex: 1, minWidth: 0, position: 'relative' }}>
        <div ref={containerRef} className="viz-3d-box" style={{ width: '100%', height: canvasH, borderRadius: 8, background: 'linear-gradient(180deg,#f2f4fb 0%,#e8ebf5 100%)' }}>
          {hasConcepts && (
            <ForceGraph3D
              ref={fgRef}
              width={initSize.w || undefined}
              height={initSize.h || undefined}
              graphData={visibleData as any}
              backgroundColor="rgba(0,0,0,0)"
              showNavInfo={false}
              nodeLabel={(n: any) => n.label}
              nodeThreeObject={nodeThreeObject}
              nodeVal={(n: any) => n.radius}
              linkWidth={0}
              linkLabel={(l: any) => l.label}
              linkColor={(l: any) => (linkFilter.has(l.kind) ? (l.kind === 'parent' ? 'rgba(120,128,160,0.5)' : 'rgba(150,158,190,0.32)') : 'rgba(0,0,0,0)')}
              linkDirectionalArrowLength={(l: any) => ((visibleData.links.length < LINK_DECOR_THRESHOLD && l.kind !== 'parent') ? 3 : 0)}
              linkDirectionalParticles={(l: any) => ((visibleData.links.length < LINK_DECOR_THRESHOLD && l.kind === 'rel') ? 2 : 0)}
              linkOpacity={0.32}
              onNodeClick={(n: any) => {
                setSelected({ kind: n.kind, name: n.name, label: n.label, color: n.color, definition: n.definition, concept: n.concept, attributes: n.attributes })
                highlightRef.current = n
                const dist = 90
                callFg('cameraPosition', { x: n.x + dist, y: n.y + dist / 2, z: n.z + dist }, n, 900)
                applyHighlight()
                updateRing(n)
              }}
              onBackgroundClick={() => {
                setSelected(null)
                highlightRef.current = null
                applyHighlight()
                updateRing(null)
              }}
              onEngineStop={() => {
                const g = fgRef.current as any
                if (!firstFitRef.current) {
                  firstFitRef.current = true
                  if (typeof g.zoomToFit === 'function') g.zoomToFit(0, 60) // 力布局收敛后全景适配（无边界力模型节点会飘出初始视野）
                }
                if (fog3d && typeof g.scene === 'function') {
                  const scn = g.scene()
                  if (scn && !scn.fog) scn.fog = new THREE.Fog(0xe8ebf5, 200, 900)
                }
              }}
            />
          )}
          {visibleData.nodes.length === 0 && (
            <div className="work-empty" style={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前过滤条件下无可见节点——在图例面板调整类型开关或根概念显隐" />
            </div>
          )}
        </div>
        {!legendOpen && (
          <Button
            size="small"
            className="viz-legend-toggle"
            icon={<ControlOutlined />}
            onClick={() => setLegendOpen(true)}
            aria-label="展开图例面板"
          >
            图例
          </Button>
        )}
        <div style={{ position: 'absolute', zIndex: 5, bottom: 8, left: 10, fontSize: 11, color: 'var(--ant-color-text-tertiary, #888)' }}>
          拖拽旋转 · 滚轮缩放 · 点击节点聚焦飞入（邻居保持、其余收缩）· 标签悬停可见
        </div>
      </div>
      {legendOpen && (
        <>
          <div
            className="viz-legend-resizer"
            role="separator"
            aria-label="拖拽调整图例面板宽度"
            aria-orientation="vertical"
            onMouseDown={startLegendResize}
            onDoubleClick={resetLegendWidth}
          />
          <Card
            size="small"
            className="viz-legend-panel"
            style={{ width: legendWidth, flexShrink: 0, overflowY: 'auto', height: canvasH }}
            title={
              <span style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <span>图例与视图控制</span>
                <Button type="text" size="small" icon={<RightOutlined />} onClick={() => setLegendOpen(false)} aria-label="折叠图例面板" />
              </span>
            }
          >
            {/* REQ-185②：控制条自画布迁入图例区（画布区纯净化）——视图/边/观感三分区 */}
            <div className="onto-flow-info-title">视图</div>
            <Space.Compact style={{ width: '100%', marginBottom: 6 }}>
              <Input
                size="small"
                placeholder="搜索概念/实例定位…"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onPressEnter={locate}
                allowClear
              />
              <Button size="small" onClick={locate}>定位</Button>
            </Space.Compact>
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginBottom: 6 }}>
              <Select
                size="small"
                style={{ flex: '1 1 96px', minWidth: 96 }}
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
                style={{ flex: '1 1 104px', minWidth: 104 }}
                value={layout}
                onChange={setLayout}
                options={[
                  { value: 'force', label: '力导向布局' },
                  { value: 'cluster', label: '根向分簇' },
                  { value: 'layer', label: '层次分层' },
                ]}
              />
            </div>
            <div className="onto-flow-info-title spaced">边</div>
            <Select
              size="small"
              style={{ width: '100%', marginBottom: 6 }}
              mode="multiple"
              allowClear={false}
              maxTagCount={2}
              value={[...linkFilter]}
              onChange={(vs) => setLinkFilter(new Set(vs.length ? vs : ['parent', 'rel', 'instance', 'instrel']))}
              options={[
                { value: 'parent', label: '继承' },
                { value: 'rel', label: '关系' },
                { value: 'instance', label: '属于' },
                { value: 'instrel', label: '实例关系' },
              ]}
            />
            <div className="onto-flow-info-title spaced">观感</div>
            <Space size={6} wrap style={{ marginBottom: 4 }}>
              <Button
                size="small"
                type={fog3d ? 'primary' : 'default'}
                onClick={() => {
                  setFog3d((v) => !v)
                  const g = fgRef.current as any
                  if (g && typeof g.scene === 'function') {
                    const scn = g.scene()
                    if (scn) scn.fog = (!fog3d ? new THREE.Fog(0xe8ebf5, 200, 900) : null)
                  }
                }}
              >
                {fog3d ? '雾效开' : '雾效关'}
              </Button>
              <Button
                size="small"
                onClick={() => {
                  setSelected(null)
                  highlightRef.current = null
                  callFg('zoomToFit', 600, 60)
                  updateRing(null)
                }}
              >
                复位全景
              </Button>
            </Space>

            <div className="onto-flow-info-title spaced">图例</div>
            <div className="onto-flow-legend"><span className="onto-flow-legend-badge" style={{ borderRadius: '50%', background: '#4f46e5' }} />概念（球体，按顶层根着色）</div>
            <div className="onto-flow-legend"><span className="onto-flow-legend-badge" style={{ transform: 'rotate(45deg)', background: '#9333ea' }} />实例（八面体，继承概念色）</div>
            <div className="onto-flow-legend"><span className="onto-flow-legend-line rel" />实线 = 关系</div>
            <div className="onto-flow-legend"><span className="onto-flow-legend-line parent" />暗线 = 继承 / 属于</div>

            <div className="onto-flow-info-title spaced">
              根概念显隐（勾选=显示 · 点名定位）
              <Space size={2} style={{ marginLeft: 'auto' }}>
                <Button size="small" type="text" style={{ fontSize: 11, height: 20, padding: '0 4px' }} onClick={() => setHiddenRoots(new Set())}>全显</Button>
                <Button size="small" type="text" style={{ fontSize: 11, height: 20, padding: '0 4px' }} onClick={() => setHiddenRoots(new Set((data.roots ?? []).map((r) => r.name)))}>全隐</Button>
              </Space>
            </div>
            <Input
              size="small"
              placeholder="搜索根概念…"
              value={legendQuery}
              onChange={(e) => setLegendQuery(e.target.value)}
              allowClear
            />
            <div style={{ marginTop: 6, maxHeight: 220, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 2 }}>
              {rootsFiltered.map((r) => {
                const st = rootStats.get(r.name)
                const visible = !hiddenRoots.has(r.name)
                return (
                  <div key={r.name} className="onto-legend-root" style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <Checkbox
                      checked={visible}
                      onChange={(e) => toggleRoot(r.name, e.target.checked)}
                      aria-label={`显隐 ${r.name}`}
                    />
                    <button
                      type="button"
                      className="onto-legend-root-btn"
                      title={`定位 ${r.name}`}
                      onClick={() => focusNodeById(`c:${r.name}`)}
                      style={{ flex: 1, minWidth: 0, display: 'flex', alignItems: 'center', gap: 6, background: 'none', border: 0, padding: 0, cursor: 'pointer', textAlign: 'left', opacity: visible ? 1 : 0.45 }}
                    >
                      <span className="onto-legend-root-dot" style={{ background: r.color }} />
                      <span className="onto-legend-root-name">{r.name}</span>
                      <span className="onto-legend-root-meta">{st ? `${st.concepts} 概念 · ${st.instances} 实例` : '1 概念'}</span>
                    </button>
                  </div>
                )
              })}
              {rootsFiltered.length === 0 && <p className="onto-flow-hint">无匹配根概念</p>}
            </div>

            <div className="onto-flow-info-title spaced">统计</div>
            <div className="onto-flow-stats">
              <span>概念 <b>{spec.concepts.length}</b></span>
              <span>实例 <b>{spec.instances?.length ?? 0}</b></span>
              <span>关系 <b>{spec.relations?.length ?? 0}</b></span>
            </div>
            <div className="onto-flow-hint" style={{ marginTop: 2 }}>
              当前可见 <b>{visibleData.nodes.length}</b> 节点 / <b>{visibleData.links.length}</b> 边（显隐过滤后子图计数即 REQ-175 渐进装载阈值输入）
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
        </>
      )}
    </div>
  )
}
