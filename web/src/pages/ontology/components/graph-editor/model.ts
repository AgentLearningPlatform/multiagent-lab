import { MarkerType } from '@xyflow/react'
import type { Edge, Node } from '@xyflow/react'
import type { Spec, SpecConcept } from '../../../../api/types'

// ---------------------------------------------------------------------------
// GraphEditor 纯逻辑层（B1 拆分，REQ-145）：画布节点/边由 Spec 派生的全部规则。
// 布局坐标仅会话内有效，不做坐标持久化（04 决策 O-3：整体排 O8）。
// ---------------------------------------------------------------------------

export const NODE_W = 168
export const COL_GAP = 46
export const ROW_STEP = 112
export const PAD = 32

export const INST_ID = (name: string) => `inst:${name}`
export const instNameOf = (id: string) => id.slice('inst:'.length)

export interface EditorNodeData extends Record<string, unknown> {
  label: string
  name: string
  count: number
  definition?: string
  kind: 'concept' | 'instance'
  conceptName?: string
}
export type EditorFlowNode = Node<EditorNodeData, 'concept' | 'instance'>

/** 按父深度分层（与 SpecGraph 同口径） */
function computeDepths(concepts: SpecConcept[]): Map<string, number> {
  const byName = new Map(concepts.map((c) => [c.name, c]))
  const depth = new Map<string, number>()
  const visiting = new Set<string>()
  const calc = (name: string): number => {
    if (depth.has(name)) return depth.get(name)!
    if (visiting.has(name)) return 0
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

/** Spec + 上一轮节点坐标 → 画布节点（保留已拖拽位置，新节点走分层布局；实例挂概念下方） */
export function deriveNodes(spec: Spec, prev: EditorFlowNode[], showInstances: boolean): EditorFlowNode[] {
  const concepts = spec.concepts ?? []
  const prevPos = new Map(prev.map((n) => [n.id, n.position]))
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
  const counts = new Map<string, number>()
  for (const inst of spec.instances ?? []) counts.set(inst.concept, (counts.get(inst.concept) ?? 0) + 1)
  // 同概念的多个实例横向排开，避免叠在一起
  const instSlot = new Map<string, number>()

  const nodes: EditorFlowNode[] = []
  for (const [d, cs] of rows) {
    cs.sort((a, b) => a.name.localeCompare(b.name))
    const rowW = cs.length * NODE_W + Math.max(0, cs.length - 1) * COL_GAP
    const startX = (width - rowW) / 2
    cs.forEach((c, i) => {
      const fallback = { x: startX + i * (NODE_W + COL_GAP), y: PAD + d * ROW_STEP }
      const pos = prevPos.get(c.name) ?? fallback
      nodes.push({
        id: c.name,
        type: 'concept' as const,
        position: pos,
        style: { width: NODE_W },
        data: { label: c.label || c.name, name: c.name, count: counts.get(c.name) ?? 0, definition: c.definition, kind: 'concept' },
      })
      if (showInstances) {
        for (const inst of (spec.instances ?? []).filter((x) => x.concept === c.name)) {
          const slot = instSlot.get(c.name) ?? 0
          instSlot.set(c.name, slot + 1)
          nodes.push({
            id: INST_ID(inst.name),
            type: 'instance' as const,
            position: prevPos.get(INST_ID(inst.name)) ?? { x: pos.x + slot * (NODE_W + COL_GAP), y: pos.y + ROW_STEP * 0.72 },
            style: { width: NODE_W },
            data: { label: inst.name, name: inst.name, count: 0, kind: 'instance', conceptName: c.name },
          })
        }
      }
    })
  }
  return nodes
}

/** Spec → 画布边：关系实线带标签，继承虚线（父 → 子），实例归属点线，实例关系细实线 */
export function deriveEdges(spec: Spec, showInstances: boolean): Edge[] {
  const names = new Set((spec.concepts ?? []).map((c) => c.name))
  const instNames = new Set((spec.instances ?? []).map((i) => i.name))
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
  if (showInstances) {
    for (const inst of spec.instances ?? []) {
      if (!names.has(inst.concept)) continue
      edges.push({
        id: `belongs:${INST_ID(inst.name)}`,
        source: INST_ID(inst.name),
        target: inst.concept,
        type: 'smoothstep',
        className: 'onto-flow-edge-belongs',
        style: { strokeDasharray: '2 4' },
      })
      for (const ir of inst.relations ?? []) {
        if (!instNames.has(ir.target)) continue
        edges.push({
          id: `instrel:${ir.rel}:${inst.name}:${ir.target}`,
          source: INST_ID(inst.name),
          target: INST_ID(ir.target),
          label: ir.rel,
          type: 'smoothstep',
          className: 'onto-flow-edge-instrel',
        })
      }
    }
  }
  return edges
}
