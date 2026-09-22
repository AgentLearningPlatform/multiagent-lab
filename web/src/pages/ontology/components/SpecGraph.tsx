import { useEffect, useMemo, useState } from 'react'
import { Empty, Splitter, Space, Tag, Typography } from 'antd'
import { Background, BackgroundVariant, Controls, Handle, MarkerType, MiniMap, Position, ReactFlow, useEdgesState, useNodesState } from '@xyflow/react'
import type { Edge, Node, NodeProps, NodeTypes } from '@xyflow/react'
import type { Spec, SpecConcept } from '../../../api/types'

// ---------------------------------------------------------------------------
// S4 可视化（React Flow 交互式图谱，@xyflow/react v12；D-O12 收口：内置默认定案）
// ---------------------------------------------------------------------------

const NODE_W = 168
const COL_GAP = 46
const ROW_STEP = 112
const PAD = 32

function computeDepths(concepts: SpecConcept[]): Map<string, number> {
  const byName = new Map(concepts.map((c) => [c.name, c]))
  const depth = new Map<string, number>()
  const visiting = new Set<string>()
  const calc = (name: string): number => {
    if (depth.has(name)) return depth.get(name)!
    if (visiting.has(name)) return 0 // 环：降级
    visiting.add(name)
    const c = byName.get(name)
    const parents = (c?.parents ?? []).filter((p) => byName.has(p))
    const d = parents.length ? 1 + Math.max(...parents.map(calc)) : 0
    visiting.delete(name)
    depth.set(name, d)
    return d
  }
  for (const c of concepts) calc(c.name)
  return depth
}

interface NodePos {
  c: SpecConcept
  x: number
  y: number
}

function layoutGraph(spec: Spec): NodePos[] {
  const concepts = spec.concepts ?? []
  const depth = computeDepths(concepts)
  const levels = new Map<number, SpecConcept[]>()
  for (const c of concepts) {
    const d = depth.get(c.name) ?? 0
    if (!levels.has(d)) levels.set(d, [])
    levels.get(d)!.push(c)
  }
  const rows = [...levels.entries()].sort((a, b) => a[0] - b[0])
  const maxCols = Math.max(1, ...rows.map(([, cs]) => cs.length))
  const width = PAD * 2 + maxCols * NODE_W + Math.max(0, maxCols - 1) * COL_GAP
  const nodes: NodePos[] = []
  for (const [d, cs] of rows) {
    cs.sort((a, b) => a.name.localeCompare(b.name))
    const rowW = cs.length * NODE_W + Math.max(0, cs.length - 1) * COL_GAP
    const startX = (width - rowW) / 2
    cs.forEach((c, i) => nodes.push({ c, x: startX + i * (NODE_W + COL_GAP), y: PAD + d * ROW_STEP }))
  }
  return nodes
}

/** 节点数据（React Flow node.data）：显示名 + 原名 + 实例数 + 定义（悬停/详情） */
interface ConceptData extends Record<string, unknown> {
  label: string
  name: string
  count: number
  definition?: string
}
type ConceptFlowNode = Node<ConceptData, 'concept'>

/** 每个概念的实例数（节点徽标 / 详情） */
export function instanceCounts(spec: Spec): Map<string, number> {
  const m = new Map<string, number>()
  for (const inst of spec.instances ?? []) m.set(inst.concept, (m.get(inst.concept) ?? 0) + 1)
  return m
}

/** Spec → React Flow 节点：按父深度分层给初始坐标，之后由 React Flow 接管拖拽/缩放 */
function buildNodes(spec: Spec): ConceptFlowNode[] {
  const counts = instanceCounts(spec)
  return layoutGraph(spec).map(({ c, x, y }) => ({
    id: c.name,
    type: 'concept' as const,
    position: { x, y },
    style: { width: NODE_W },
    data: { label: c.label || c.name, name: c.name, count: counts.get(c.name) ?? 0, definition: c.definition },
  }))
}

/**
 * Spec → React Flow 边：
 *  - 关系：实线 + 标签 + 箭头（source=from → target=to）；
 *  - 父子：虚线（source=父 → target=子，保证自上而下走向）。
 */
function buildEdges(spec: Spec): Edge[] {
  const names = new Set((spec.concepts ?? []).map((c) => c.name))
  const edges: Edge[] = []
  for (const r of spec.relations ?? []) {
    if (!names.has(r.from) || !names.has(r.to)) continue
    edges.push({
      id: `rel:${r.name}:${r.from}:${r.to}`,
      source: r.from,
      target: r.to,
      label: r.label || r.name,
      type: 'smoothstep',
      className: 'onto-flow-edge-rel',
      markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 },
    })
  }
  for (const c of spec.concepts ?? []) {
    for (const p of c.parents ?? []) {
      if (!names.has(p)) continue
      edges.push({
        id: `parent:${p}:${c.name}`,
        source: p,
        target: c.name,
        type: 'smoothstep',
        className: 'onto-flow-edge-parent',
        style: { strokeDasharray: '5 4' },
      })
    }
  }
  return edges
}

/** 自定义概念节点：名称 + 实例数徽标；title 承载定义 */
function ConceptNode({ data, selected }: NodeProps<ConceptFlowNode>) {
  return (
    <div className={`onto-flow-node${selected ? ' selected' : ''}`} title={data.definition || data.label}>
      <Handle type="target" position={Position.Top} className="onto-flow-handle" />
      <span className="onto-flow-node-label">{data.label}</span>
      {data.count > 0 && <span className="onto-flow-node-badge">{data.count}</span>}
      <Handle type="source" position={Position.Bottom} className="onto-flow-handle" />
    </div>
  )
}

// nodeTypes 必须定义在组件外，避免每次渲染重建导致 React Flow 重挂载
const nodeTypes: NodeTypes = { concept: ConceptNode }

export default function SpecGraph({ spec }: { spec: Spec | null }) {
  const hasConcepts = !!spec && (spec.concepts?.length ?? 0) > 0
  const initialNodes = useMemo(() => (spec ? buildNodes(spec) : []), [spec])
  const initialEdges = useMemo(() => (spec ? buildEdges(spec) : []), [spec])
  const [nodes, setNodes, onNodesChange] = useNodesState<ConceptFlowNode>(initialNodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>(initialEdges)
  const [selectedId, setSelectedId] = useState<string | null>(null)

  // Spec 变化时重置图谱（拖拽后的坐标不跨 Spec 版本保留）
  useEffect(() => {
    setNodes(initialNodes)
    setEdges(initialEdges)
    setSelectedId(null)
  }, [initialNodes, initialEdges, setNodes, setEdges])

  const counts = useMemo(() => (spec ? instanceCounts(spec) : new Map<string, number>()), [spec])
  const selected = useMemo(
    () => (spec && selectedId ? spec.concepts.find((c) => c.name === selectedId) ?? null : null),
    [spec, selectedId],
  )
  const selectedInstances = useMemo(
    () => (spec && selected ? (spec.instances ?? []).filter((i) => i.concept === selected.name) : []),
    [spec, selected],
  )

  if (!hasConcepts || !spec) {
    return (
      <div className="work-empty" style={{ minHeight: 220 }}>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无概念可可视化；请先在构建流程保存含概念的 Spec" />
      </div>
    )
  }

  return (
    <Splitter className="onto-flow-split" orientation="horizontal">
      <Splitter.Panel defaultSize="68%" min="40%">
        <div className="onto-flow-pane">
          <ReactFlow
            key={initialNodes.map((n) => n.id).join('|')}
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            nodeTypes={nodeTypes}
            fitView
            fitViewOptions={{ padding: 0.2 }}
            minZoom={0.2}
            maxZoom={2}
            nodesConnectable={false}
            onNodeClick={(_, n) => setSelectedId(n.id)}
            onPaneClick={() => setSelectedId(null)}
            className="onto-flow"
          >
            <Background variant={BackgroundVariant.Dots} gap={18} size={1.5} color="#c9cee0" />
            <MiniMap position="top-right" pannable zoomable nodeColor="#c9cef3" maskColor="rgba(246, 247, 251, 0.72)" />
            <Controls showInteractive={false} position="bottom-left" />
          </ReactFlow>
        </div>
      </Splitter.Panel>
      <Splitter.Panel min="22%">
        <div className="onto-flow-info">
          <div className="onto-flow-info-title">图例</div>
          <div className="onto-flow-legend">
            <span className="onto-flow-legend-line rel" />
            <span>实线 = 关系（from → to）</span>
          </div>
          <div className="onto-flow-legend">
            <span className="onto-flow-legend-line parent" />
            <span>虚线 = 继承（父 → 子）</span>
          </div>
          <div className="onto-flow-legend">
            <span className="onto-flow-legend-badge">n</span>
            <span>节点徽标 = 实例数</span>
          </div>

          <div className="onto-flow-info-title spaced">统计</div>
          <div className="onto-flow-stats">
            <span>概念 <b>{spec.concepts.length}</b></span>
            <span>关系 <b>{spec.relations?.length ?? 0}</b></span>
            <span>实例 <b>{spec.instances?.length ?? 0}</b></span>
          </div>

          <div className="onto-flow-info-title spaced">选中节点</div>
          {selected ? (
            <div className="onto-flow-detail">
              <div className="onto-flow-detail-name">{selected.label || selected.name}</div>
              <div className="onto-flow-detail-key">{selected.name}</div>
              <p className={`onto-flow-detail-def${selected.definition ? '' : ' muted'}`}>
                {selected.definition || '未填写定义'}
              </p>
              <div className="onto-flow-detail-row">
                <span className="onto-flow-detail-label">父概念</span>
                {selected.parents && selected.parents.length > 0 ? (
                  <Space size={4} wrap>
                    {selected.parents.map((p) => (
                      <Tag key={p} style={{ margin: 0 }}>{p}</Tag>
                    ))}
                  </Space>
                ) : (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>—</Typography.Text>
                )}
              </div>
              <div className="onto-flow-detail-row">
                <span className="onto-flow-detail-label">实例</span>
                <Typography.Text style={{ fontSize: 12 }}>{counts.get(selected.name) ?? 0} 个</Typography.Text>
              </div>
              {selectedInstances.length > 0 && (
                <Space size={4} wrap style={{ marginTop: 6 }}>
                  {selectedInstances.slice(0, 12).map((i) => (
                    <Tag key={i.name} color="purple" style={{ margin: 0 }}>{i.name}</Tag>
                  ))}
                  {selectedInstances.length > 12 && (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>等 {selectedInstances.length} 个</Typography.Text>
                  )}
                </Space>
              )}
            </div>
          ) : (
            <p className="onto-flow-hint">点击图中节点查看定义、父概念与实例；滚轮缩放、拖拽平移 / 节点。</p>
          )}
        </div>
      </Splitter.Panel>
    </Splitter>
  )
}
