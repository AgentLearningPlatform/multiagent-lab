import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Empty,
  Form,
  Input,
  Modal,
  Popconfirm,
  Radio,
  Select,
  Space,
  Splitter,
  Switch,
  Tag,
  Typography,
} from 'antd'
import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  useEdgesState,
  useNodesState,
} from '@xyflow/react'
import type { Connection, Edge, Node, NodeProps, NodeTypes } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { api } from '../../../api/client'
import type { Spec, SpecConcept, SpecInstance, SpecRelation } from '../../../api/types'
import { useUI } from '../../../store/ui'

// ---------------------------------------------------------------------------
// REQ-71 图形化编辑器：React Flow 画布上直接编辑概念/关系/继承/实例，
// 保存时写回 spec_json 走既有 PUT 保存通道（校验门控、递增 version）。
// 与只读「可视化」Tab（SpecGraph）双形态并存；布局坐标仅会话内有效，
// 不做坐标持久化（04 决策 O-3：维持 localStorage 方案，整体排 O8）。
// v1.5：实例节点入画布（归属概念/实例间关系/增删与属性编辑，可开关显示）；
//       实例改名仍不进画布（name 是引用键，改名=级联重建，引导走 Spec 编辑）。
// ---------------------------------------------------------------------------

const NODE_W = 168
const COL_GAP = 46
const ROW_STEP = 112
const PAD = 32

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

interface EditorNodeData extends Record<string, unknown> {
  label: string
  name: string
  count: number
  definition?: string
  kind: 'concept' | 'instance'
  conceptName?: string
}
type EditorFlowNode = Node<EditorNodeData, 'concept' | 'instance'>

const INST_ID = (name: string) => `inst:${name}`
const instNameOf = (id: string) => id.slice('inst:'.length)

/** Spec + 上一轮节点坐标 → 画布节点（保留已拖拽位置，新节点走分层布局；实例挂概念下方） */
function deriveNodes(spec: Spec, prev: EditorFlowNode[], showInstances: boolean): EditorFlowNode[] {
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
function deriveEdges(spec: Spec, showInstances: boolean): Edge[] {
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

function EditorConceptNode({ data, selected }: NodeProps<EditorFlowNode>) {
  return (
    <div className={`onto-flow-node${selected ? ' selected' : ''}`} title={data.definition || data.label}>
      <Handle type="target" position={Position.Top} className="onto-flow-handle" />
      <span className="onto-flow-node-label">{data.label}</span>
      {data.count > 0 && <span className="onto-flow-node-badge">{data.count}</span>}
      <Handle type="source" position={Position.Bottom} className="onto-flow-handle" />
    </div>
  )
}

/** 实例节点：紫色调 + 所属概念脚标，可连线建实例关系 */
function EditorInstanceNode({ data, selected }: NodeProps<EditorFlowNode>) {
  return (
    <div className={`onto-flow-node onto-flow-node-inst${selected ? ' selected' : ''}`} title={`实例 · ${data.conceptName}`}>
      <Handle type="target" position={Position.Top} className="onto-flow-handle" />
      <span className="onto-flow-node-label">{data.label}</span>
      <span className="onto-flow-node-badge inst">{data.conceptName}</span>
      <Handle type="source" position={Position.Bottom} className="onto-flow-handle" />
    </div>
  )
}

const nodeTypes: NodeTypes = { concept: EditorConceptNode, instance: EditorInstanceNode }

type Selection = { kind: 'node'; id: string } | { kind: 'edge'; id: string } | null

interface ConnDraft {
  source: string
  target: string
  kind: 'parent' | 'relation' | 'instance-relation'
}

export default function GraphEditor({
  ontologyId,
  spec,
  onSpecSaved,
}: {
  ontologyId: string
  spec: Spec | null
  onSpecSaved: (version: number) => void
}) {
  const { showToast } = useUI()
  // specDraft 是编辑事实源；画布 nodes/edges 由它派生，拖拽位置保留在 nodes 状态里
  const [draft, setDraft] = useState<Spec | null>(spec)
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveErrors, setSaveErrors] = useState<{ path: string; message: string }[] | null>(null)
  const [selection, setSelection] = useState<Selection>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [addInstOpen, setAddInstOpen] = useState(false)
  const [showInstances, setShowInstances] = useState(true)
  const [connDraft, setConnDraft] = useState<ConnDraft | null>(null)
  const [addForm] = Form.useForm<{ name: string; label?: string; definition?: string; parents?: string[] }>()
  const [addInstForm] = Form.useForm<{ name: string; concept: string; attributes?: string }>()
  const [connForm] = Form.useForm<{ relName: string; relLabel?: string }>()

  // 外部 Spec 变化（切换本体 / 保存后刷新）→ 丢弃本地草稿
  useEffect(() => {
    setDraft(spec)
    setDirty(false)
    setSaveErrors(null)
    setSelection(null)
  }, [spec, ontologyId])

  const initialNodes = useMemo(() => (draft ? deriveNodes(draft, [], showInstances) : []), [draft, showInstances])
  const initialEdges = useMemo(
    () => deriveEdges(draft ?? { name: '', concepts: [], relations: [], instances: [] }, showInstances),
    [draft, showInstances],
  )
  const [nodes, setNodes, onNodesChange] = useNodesState<EditorFlowNode>(initialNodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>(initialEdges)

  // 结构变化或实例显示开关切换时以 draft 重建画布（保留已有节点坐标）
  const rebuild = useCallback(
    (next: Spec, withInstances = showInstances) => {
      setNodes((prev) => deriveNodes(next, prev, withInstances))
      setEdges(deriveEdges(next, withInstances))
    },
    [setNodes, setEdges, showInstances],
  )

  const mutate = useCallback(
    (next: Spec) => {
      setDraft(next)
      setDirty(true)
      setSaveErrors(null)
      rebuild(next)
    },
    [rebuild],
  )

  const onConnect = useCallback(
    (conn: Connection) => {
      if (!draft || !conn.source || !conn.target || conn.source === conn.target) return
      const srcInst = conn.source.startsWith('inst:')
      const tgtInst = conn.target.startsWith('inst:')
      if (srcInst !== tgtInst) {
        showToast('概念与实例之间不连线；归属关系随实例自动生成', 'err')
        return
      }
      if (srcInst && tgtInst) {
        setConnDraft({ source: conn.source, target: conn.target, kind: 'instance-relation' })
        connForm.setFieldsValue({ relName: '', relLabel: '' })
        return
      }
      setConnDraft({ source: conn.source, target: conn.target, kind: 'relation' })
      connForm.setFieldsValue({ relName: `rel_${(draft.relations?.length ?? 0) + 1}`, relLabel: '' })
    },
    [draft, connForm, showToast],
  )

  const applyConnect = useCallback(() => {
    if (!draft || !connDraft) return
    const { source, target } = connDraft
    const v = connForm.getFieldsValue()
    const relName = (v.relName || '').trim()
    if (!relName) {
      showToast('关系名不能为空', 'err')
      return
    }
    if (connDraft.kind === 'instance-relation') {
      const srcName = instNameOf(source)
      const tgtName = instNameOf(target)
      mutate({
        ...draft,
        instances: (draft.instances ?? []).map((i) =>
          i.name === srcName ? { ...i, relations: [...(i.relations ?? []), { rel: relName, target: tgtName }] } : i,
        ),
      })
      setConnDraft(null)
      return
    }
    if (connDraft.kind === 'parent') {
      mutate({
        ...draft,
        concepts: draft.concepts.map((c) =>
          c.name === target && !(c.parents ?? []).includes(source) ? { ...c, parents: [...(c.parents ?? []), source] } : c,
        ),
      })
      setConnDraft(null)
      return
    }
    const dup = (draft.relations ?? []).some((r) => r.name === relName)
    if (dup) {
      showToast(`关系名 ${relName} 已存在`, 'err')
      return
    }
    const rel: SpecRelation = { name: relName, from: source, to: target }
    if (v.relLabel?.trim()) rel.label = v.relLabel.trim()
    mutate({ ...draft, relations: [...(draft.relations ?? []), rel] })
    setConnDraft(null)
  }, [draft, connDraft, mutate, connForm, showToast])

  // ---- 概念增删 / 属性编辑 ----
  const addConcept = useCallback(() => {
    if (!draft) return
    addForm.validateFields().then((v) => {
      const name = v.name.trim()
      if (!name) return
      if (draft.concepts.some((c) => c.name === name)) {
        showToast(`概念 ${name} 已存在`, 'err')
        return
      }
      const c: SpecConcept = { name }
      if (v.label?.trim()) c.label = v.label.trim()
      if (v.definition?.trim()) c.definition = v.definition.trim()
      if (v.parents?.length) c.parents = v.parents
      mutate({ ...draft, concepts: [...draft.concepts, c] })
      setAddOpen(false)
      addForm.resetFields()
      setSelection({ kind: 'node', id: name })
    })
  }, [draft, mutate, addForm, showToast])

  // ---- 实例增删（v1.5）----
  const addInstance = useCallback(() => {
    if (!draft) return
    addInstForm.validateFields().then((v) => {
      const name = v.name.trim()
      if (!name) return
      if (draft.concepts.some((c) => c.name === name)) {
        showToast(`「${name}」已是概念名，实例与概念不可同名`, 'err')
        return
      }
      if ((draft.instances ?? []).some((i) => i.name === name)) {
        showToast(`实例 ${name} 已存在`, 'err')
        return
      }
      let attributes: Record<string, unknown> | undefined
      if (v.attributes?.trim()) {
        try {
          const parsed = JSON.parse(v.attributes)
          if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) throw new Error('需为 JSON 对象')
          attributes = parsed
        } catch (e) {
          showToast(`属性 JSON 不合法：${(e as Error).message}`, 'err')
          return
        }
      }
      const inst: SpecInstance = { name, concept: v.concept }
      if (attributes) inst.attributes = attributes
      mutate({ ...draft, instances: [...(draft.instances ?? []), inst] })
      setAddInstOpen(false)
      addInstForm.resetFields()
      setSelection({ kind: 'node', id: INST_ID(name) })
    })
  }, [draft, mutate, addInstForm, showToast])

  const removeInstance = useCallback(
    (name: string) => {
      if (!draft) return
      mutate({
        ...draft,
        instances: (draft.instances ?? [])
          .filter((i) => i.name !== name)
          .map((i) => ({ ...i, relations: (i.relations ?? []).filter((r) => r.target !== name) })),
      })
      setSelection(null)
    },
    [draft, mutate],
  )

  const removeConcept = useCallback(
    (name: string) => {
      if (!draft) return
      mutate({
        ...draft,
        concepts: draft.concepts.filter((c) => c.name !== name),
        relations: (draft.relations ?? []).filter((r) => r.from !== name && r.to !== name),
        // 悬空实例一并移除，保持 spec 一致性（校验门控也会拦）
        instances: (draft.instances ?? []).filter((i) => i.concept !== name),
      })
      setSelection(null)
    },
    [draft, mutate],
  )

  const removeEdge = useCallback(
    (edgeId: string) => {
      if (!draft) return
      if (edgeId.startsWith('parent:')) {
        const [, parent, child] = edgeId.split(':')
        mutate({
          ...draft,
          concepts: draft.concepts.map((c) =>
            c.name === child ? { ...c, parents: (c.parents ?? []).filter((p) => p !== parent) } : c,
          ),
        })
      } else if (edgeId.startsWith('instrel:')) {
        const [, relName, from, to] = edgeId.split(':')
        mutate({
          ...draft,
          instances: (draft.instances ?? []).map((i) =>
            i.name === from ? { ...i, relations: (i.relations ?? []).filter((r) => !(r.rel === relName && r.target === to)) } : i,
          ),
        })
      } else if (edgeId.startsWith('belongs:')) {
        showToast('归属关系随实例存在；要移除请删除该实例或在属性面板更换所属概念', 'err')
        return
      } else {
        const [, relName, from, to] = edgeId.split(':')
        mutate({
          ...draft,
          relations: (draft.relations ?? []).filter((r) => !(r.name === relName && r.from === from && r.to === to)),
        })
      }
      setSelection(null)
    },
    [draft, mutate, showToast],
  )

  const save = useCallback(async () => {
    if (!draft) return
    setSaving(true)
    setSaveErrors(null)
    try {
      const r = await api.saveSpec(ontologyId, draft)
      showToast(`图形编辑已保存（version ${r.version}）`)
      setDirty(false)
      onSpecSaved(r.version)
    } catch (e) {
      const err = e as Error & { validationErrors?: { path: string; message: string }[] }
      if (err.validationErrors?.length) setSaveErrors(err.validationErrors)
      showToast(err.message || '保存失败', 'err')
    } finally {
      setSaving(false)
    }
  }, [draft, ontologyId, onSpecSaved, showToast])

  // ---- 选中项详情 ----
  const selNode = useMemo(
    () => (draft && selection?.kind === 'node' && !selection.id.startsWith('inst:') ? draft.concepts.find((c) => c.name === selection.id) ?? null : null),
    [draft, selection],
  )
  const selInstance = useMemo(
    () => (draft && selection?.kind === 'node' && selection.id.startsWith('inst:') ? (draft.instances ?? []).find((i) => i.name === instNameOf(selection.id)) ?? null : null),
    [draft, selection],
  )
  const selRelEdge = useMemo(() => {
    if (!draft || selection?.kind !== 'edge' || !selection.id.startsWith('rel:')) return null
    const [, relName, from, to] = selection.id.split(':')
    return (draft.relations ?? []).find((r) => r.name === relName && r.from === from && r.to === to) ?? null
  }, [draft, selection])
  const selInstRel = useMemo(() => {
    if (!draft || selection?.kind !== 'edge' || !selection.id.startsWith('instrel:')) return null
    const [, relName, from, to] = selection.id.split(':')
    return { rel: relName, from, to }
  }, [selection])
  const selParentEdge = useMemo(() => {
    if (selection?.kind !== 'edge' || !selection.id.startsWith('parent:')) return null
    const [, parent, child] = selection.id.split(':')
    return { parent, child }
  }, [selection])

  if (!spec || (spec.concepts?.length ?? 0) === 0) {
    return (
      <div className="work-empty" style={{ minHeight: 220 }}>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无概念；请先在 Spec 编辑中定义概念，再用图形编辑器扩展" />
      </div>
    )
  }
  const d = draft ?? spec

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8, flexWrap: 'wrap', gap: 8 }}>
        <Space size={8} wrap>
          <Button size="small" type="primary" ghost onClick={() => setAddOpen(true)}>
            添加概念
          </Button>
          <Button size="small" type="primary" ghost onClick={() => {
            addInstForm.setFieldsValue({ attributes: '' })
            setAddInstOpen(true)
          }}>
            添加实例
          </Button>
          <Tag>概念 {d.concepts.length}</Tag>
          <Tag>关系 {d.relations?.length ?? 0}</Tag>
          <Tag>实例 {d.instances?.length ?? 0}</Tag>
          <Switch
            size="small"
            checkedChildren="显示实例"
            unCheckedChildren="隐藏实例"
            checked={showInstances}
            onChange={(v) => {
              setShowInstances(v)
              rebuild(d, v)
            }}
          />
          {dirty && <Tag color="orange">未保存修改</Tag>}
        </Space>
        <Space size={8}>
          {dirty && (
            <Button
              size="small"
              onClick={() => {
                setDraft(spec)
                setDirty(false)
                setSaveErrors(null)
                setSelection(null)
                rebuild(spec)
              }}
            >
              放弃修改
            </Button>
          )}
          <Button size="small" type="primary" loading={saving} disabled={!dirty} onClick={save}>
            保存 Spec
          </Button>
        </Space>
      </div>
      {saveErrors && saveErrors.length > 0 && (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 8 }}
          message="校验未通过，未保存"
          description={
            <ul style={{ margin: 0, paddingLeft: 18 }}>
              {saveErrors.map((e, i) => (
                <li key={i}>
                  {e.path}: {e.message}
                </li>
              ))}
            </ul>
          }
        />
      )}
      <Splitter className="onto-flow-split" orientation="horizontal">
        <Splitter.Panel defaultSize="68%" min="40%">
          <div className="onto-flow-pane">
            <ReactFlow
              key={d.concepts.map((c) => c.name).join('|')}
              nodes={nodes}
              edges={edges}
              onNodesChange={onNodesChange}
              onEdgesChange={onEdgesChange}
              onConnect={onConnect}
              nodeTypes={nodeTypes}
              fitView
              fitViewOptions={{ padding: 0.2 }}
              minZoom={0.2}
              maxZoom={2}
              onNodeClick={(_, n) => setSelection({ kind: 'node', id: n.id })}
              onPaneClick={() => setSelection(null)}
              onEdgeClick={(_, e) => setSelection({ kind: 'edge', id: e.id })}
              className="onto-flow"
            >
              <Background variant={BackgroundVariant.Dots} gap={18} size={1.5} color="#c9cee0" />
              <MiniMap position="top-right" pannable zoomable nodeColor="#c9cef3" maskColor="rgba(246, 247, 251, 0.72)" />
              <Controls showInteractive={false} position="bottom-left" />
            </ReactFlow>
          </div>
        </Splitter.Panel>
        <Splitter.Panel min="24%">
          <div className="onto-flow-info">
            <div className="onto-flow-info-title">编辑面板</div>
            {!selection && (
              <p className="onto-flow-hint">
                拖拽节点边缘连线即创建关系/继承（实例间连线=实例关系）；点击节点或连线在右侧编辑；「添加概念/实例」新建节点。保存走统一校验门控并递增版本。
              </p>
            )}
            {selNode && <NodePanel key={selNode.name} concept={selNode} draft={d} onApply={mutate} onRemove={removeConcept} />}
            {selInstance && (
              <InstancePanel
                key={selInstance.name}
                instance={selInstance}
                draft={d}
                onApply={mutate}
                onRemove={removeInstance}
              />
            )}
            {selRelEdge && (
              <div className="onto-flow-detail">
                <div className="onto-flow-detail-name">{selRelEdge.label || selRelEdge.name}</div>
                <div className="onto-flow-detail-key">
                  {selRelEdge.from} → {selRelEdge.to}
                </div>
                {selRelEdge.definition && <p className="onto-flow-detail-def">{selRelEdge.definition}</p>}
                <Popconfirm title={`删除关系「${selRelEdge.name}」？`} onConfirm={() => removeEdge(selection!.id)}>
                  <Button size="small" danger block style={{ marginTop: 8 }}>
                    删除关系
                  </Button>
                </Popconfirm>
              </div>
            )}
            {selParentEdge && (
              <div className="onto-flow-detail">
                <div className="onto-flow-detail-name">继承</div>
                <div className="onto-flow-detail-key">
                  {selParentEdge.parent} → {selParentEdge.child}
                </div>
                <p className="onto-flow-detail-def muted">子概念继承父概念（虚线）</p>
                <Popconfirm title={`删除该继承（${selParentEdge.child} 不再继承 ${selParentEdge.parent}）？`} onConfirm={() => removeEdge(selection!.id)}>
                  <Button size="small" danger block style={{ marginTop: 8 }}>
                    删除继承
                  </Button>
                </Popconfirm>
              </div>
            )}
            {selInstRel && (
              <div className="onto-flow-detail">
                <div className="onto-flow-detail-name">实例关系 · {selInstRel.rel}</div>
                <div className="onto-flow-detail-key">
                  {selInstRel.from} → {selInstRel.to}
                </div>
                <p className="onto-flow-detail-def muted">记录在 {selInstRel.from} 的 relations 上（细实线）</p>
                <Popconfirm title={`删除实例关系「${selInstRel.rel}」？`} onConfirm={() => removeEdge(selection!.id)}>
                  <Button size="small" danger block style={{ marginTop: 8 }}>
                    删除实例关系
                  </Button>
                </Popconfirm>
              </div>
            )}
          </div>
        </Splitter.Panel>
      </Splitter>

      <Modal
        title="添加概念"
        open={addOpen}
        okText="添加"
        cancelText="取消"
        onOk={addConcept}
        onCancel={() => {
          setAddOpen(false)
          addForm.resetFields()
        }}
        destroyOnHidden
      >
        <Form form={addForm} layout="vertical" initialValues={{ parents: [] }}>
          <Form.Item name="name" label="名称（唯一标识）" rules={[{ required: true, message: '请输入概念名' }]}>
            <Input placeholder="如 Paper" />
          </Form.Item>
          <Form.Item name="label" label="显示名（可选）">
            <Input placeholder="如 论文" />
          </Form.Item>
          <Form.Item name="definition" label="定义（可选）">
            <Input.TextArea rows={2} placeholder="一句话说明该概念是什么" />
          </Form.Item>
          <Form.Item name="parents" label="父概念（可选，多选）">
            <Select mode="multiple" allowClear placeholder="选择已有概念" options={d.concepts.map((c) => ({ value: c.name, label: c.label || c.name }))} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={
          connDraft ? (
            <span>
              连线 <Tag style={{ margin: 0 }}>{connDraft.source.startsWith('inst:') ? instNameOf(connDraft.source) : connDraft.source}</Tag> →{' '}
              <Tag style={{ margin: 0 }}>{connDraft.target.startsWith('inst:') ? instNameOf(connDraft.target) : connDraft.target}</Tag>
            </span>
          ) : (
            '连线'
          )
        }
        open={!!connDraft}
        okText="创建"
        cancelText="取消"
        onOk={applyConnect}
        onCancel={() => setConnDraft(null)}
        destroyOnHidden
      >
        <Form form={connForm} layout="vertical">
          {connDraft?.kind !== 'instance-relation' && (
            <Form.Item name="kind" label="连线类型" initialValue="relation">
              <Radio.Group
                onChange={(e) => setConnDraft((c) => (c ? { ...c, kind: e.target.value } : c))}
                options={[
                  { value: 'relation', label: '关系（实线，from → to）' },
                  { value: 'parent', label: `继承（虚线，${connDraft?.source} 为父）` },
                ]}
              />
            </Form.Item>
          )}
          {connDraft?.kind === 'relation' && (
            <>
              <Form.Item name="relName" label="关系名（唯一标识）" rules={[{ required: true, message: '请输入关系名' }]}>
                <Input placeholder="如 cites" />
              </Form.Item>
              <Form.Item name="relLabel" label="显示名（可选）">
                <Input placeholder="如 引用" />
              </Form.Item>
            </>
          )}
          {connDraft?.kind === 'parent' && (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              将为 {connDraft.target} 增加父概念 {connDraft.source}（若已存在则忽略）。
            </Typography.Text>
          )}
          {connDraft?.kind === 'instance-relation' && (
            <>
              <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginBottom: 8 }}>
                实例间关系（记录在 {instNameOf(connDraft.source)} 的 relations 上，指向 {instNameOf(connDraft.target)}）。
              </Typography.Text>
              <Form.Item name="relName" label="关系名（对应概念层关系）" rules={[{ required: true, message: '请输入关系名' }]}>
                <Input placeholder="如 cites（建议与概念层关系同名）" />
              </Form.Item>
            </>
          )}
        </Form>
      </Modal>

      <Modal
        title="添加实例"
        open={addInstOpen}
        okText="添加"
        cancelText="取消"
        onOk={addInstance}
        onCancel={() => {
          setAddInstOpen(false)
          addInstForm.resetFields()
        }}
        destroyOnHidden
      >
        <Form form={addInstForm} layout="vertical">
          <Form.Item name="name" label="实例名（唯一标识）" rules={[{ required: true, message: '请输入实例名' }]}>
            <Input placeholder="如 《知识图谱》" />
          </Form.Item>
          <Form.Item name="concept" label="所属概念" rules={[{ required: true, message: '请选择所属概念' }]}>
            <Select placeholder="选择概念" options={d.concepts.map((c) => ({ value: c.name, label: c.label || c.name }))} />
          </Form.Item>
          <Form.Item
            name="attributes"
            label="属性（可选，JSON 对象）"
            rules={[
              {
                validator: (_: unknown, v: string) => {
                  if (!v?.trim()) return Promise.resolve()
                  try {
                    const p = JSON.parse(v)
                    if (typeof p !== 'object' || p === null || Array.isArray(p)) return Promise.reject('需为 JSON 对象，如 {"year": 2024}')
                  } catch {
                    return Promise.reject('JSON 语法不合法')
                  }
                  return Promise.resolve()
                },
              },
            ]}
          >
            <Input.TextArea rows={2} placeholder='如 {"year": 2024}' />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}

/** 选中概念节点的属性编辑面板（key=name 保证切换节点时表单重置） */
function NodePanel({
  concept,
  draft,
  onApply,
  onRemove,
}: {
  concept: SpecConcept
  draft: Spec
  onApply: (next: Spec) => void
  onRemove: (name: string) => void
}) {
  const [label, setLabel] = useState(concept.label ?? '')
  const [definition, setDefinition] = useState(concept.definition ?? '')
  const [parents, setParents] = useState<string[]>(concept.parents ?? [])
  useEffect(() => {
    setLabel(concept.label ?? '')
    setDefinition(concept.definition ?? '')
    setParents(concept.parents ?? [])
  }, [concept])

  const others = draft.concepts.filter((c) => c.name !== concept.name)
  const instCount = (draft.instances ?? []).filter((i) => i.concept === concept.name).length
  const changed =
    label !== (concept.label ?? '') || definition !== (concept.definition ?? '') || parents.join(',') !== (concept.parents ?? []).join(',')

  return (
    <div className="onto-flow-detail">
      <div className="onto-flow-detail-name">{concept.label || concept.name}</div>
      <div className="onto-flow-detail-key">{concept.name}</div>
      <div style={{ display: 'grid', gap: 6, marginTop: 8 }}>
        <Input size="small" addonBefore="显示名" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="与名称相同可留空" />
        <Input.TextArea size="small" rows={2} value={definition} onChange={(e) => setDefinition(e.target.value)} placeholder="定义" />
        <Select
          size="small"
          mode="multiple"
          allowClear
          value={parents}
          onChange={setParents}
          placeholder="父概念"
          options={others.map((c) => ({ value: c.name, label: c.label || c.name }))}
        />
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          实例 {instCount} 个（v1 在此处只读；增删走 Spec 编辑或 CSV 灌装）
        </Typography.Text>
        <Space size={6}>
          <Button
            size="small"
            type="primary"
            disabled={!changed}
            onClick={() => {
              const next: SpecConcept = { ...concept }
              if (label.trim()) next.label = label.trim()
              else delete next.label
              if (definition.trim()) next.definition = definition.trim()
              else delete next.definition
              const ps = parents.filter((p) => p !== concept.name)
              if (ps.length) next.parents = ps
              else delete next.parents
              onApply({ ...draft, concepts: draft.concepts.map((c) => (c.name === concept.name ? next : c)) })
            }}
          >
            应用修改
          </Button>
          <Popconfirm title={`删除概念「${concept.name}」？其关系与实例将一并移除。`} onConfirm={() => onRemove(concept.name)}>
            <Button size="small" danger>
              删除概念
            </Button>
          </Popconfirm>
        </Space>
      </div>
    </div>
  )
}

/** 选中实例节点的属性编辑面板（v1.5）：换所属概念 / 属性 JSON / 删除；改名不进画布 */
function InstancePanel({
  instance,
  draft,
  onApply,
  onRemove,
}: {
  instance: SpecInstance
  draft: Spec
  onApply: (next: Spec) => void
  onRemove: (name: string) => void
}) {
  const [concept, setConcept] = useState(instance.concept)
  const [attrsText, setAttrsText] = useState(() => (instance.attributes ? JSON.stringify(instance.attributes, null, 0) : ''))
  const [attrsErr, setAttrsErr] = useState<string | null>(null)
  useEffect(() => {
    setConcept(instance.concept)
    setAttrsText(instance.attributes ? JSON.stringify(instance.attributes, null, 0) : '')
    setAttrsErr(null)
  }, [instance])

  const conceptChanged = concept !== instance.concept
  const attrsChanged = attrsText.trim() !== (instance.attributes ? JSON.stringify(instance.attributes, null, 0) : '')
  const changed = conceptChanged || attrsChanged

  const apply = () => {
    let attributes: Record<string, unknown> | undefined
    if (attrsText.trim()) {
      try {
        const parsed = JSON.parse(attrsText)
        if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) throw new Error('需为 JSON 对象')
        attributes = parsed
      } catch (e) {
        setAttrsErr((e as Error).message)
        return
      }
    }
    setAttrsErr(null)
    const next: SpecInstance = { ...instance, concept }
    if (attributes) next.attributes = attributes
    else delete next.attributes
    onApply({ ...draft, instances: (draft.instances ?? []).map((i) => (i.name === instance.name ? next : i)) })
  }

  return (
    <div className="onto-flow-detail">
      <div className="onto-flow-detail-name">{instance.name}</div>
      <div className="onto-flow-detail-key">实例</div>
      <div style={{ display: 'grid', gap: 6, marginTop: 8 }}>
        <Select
          size="small"
          value={concept}
          onChange={setConcept}
          placeholder="所属概念"
          options={draft.concepts.map((c) => ({ value: c.name, label: c.label || c.name }))}
        />
        <Input.TextArea
          size="small"
          rows={2}
          value={attrsText}
          onChange={(e) => {
            setAttrsText(e.target.value)
            setAttrsErr(null)
          }}
          placeholder='属性 JSON，如 {"year": 2024}'
          status={attrsErr ? 'error' : undefined}
        />
        {attrsErr && <Typography.Text type="danger" style={{ fontSize: 12 }}>属性不合法：{attrsErr}</Typography.Text>}
        {(instance.relations?.length ?? 0) > 0 && (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            实例关系：{instance.relations!.map((r) => `—${r.rel}→ ${r.target}`).join('，')}（画布点连线删除）
          </Typography.Text>
        )}
        <Space size={6}>
          <Button size="small" type="primary" disabled={!changed} onClick={apply}>
            应用修改
          </Button>
          <Popconfirm title={`删除实例「${instance.name}」？其被引用的实例关系将一并移除。`} onConfirm={() => onRemove(instance.name)}>
            <Button size="small" danger>
              删除实例
            </Button>
          </Popconfirm>
        </Space>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          实例改名不进画布（name 是引用键）；如需改名请到 Spec 编辑。
        </Typography.Text>
      </div>
    </div>
  )
}
