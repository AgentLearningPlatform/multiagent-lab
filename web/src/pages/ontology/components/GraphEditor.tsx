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
import type { Spec, SpecConcept, SpecRelation } from '../../../api/types'
import { useUI } from '../../../store/ui'

// ---------------------------------------------------------------------------
// REQ-71 图形化编辑器 v1：React Flow 画布上直接编辑概念/关系/继承，
// 保存时写回 spec_json 走既有 PUT 保存通道（校验门控、递增 version）。
// 与只读「可视化」Tab（SpecGraph）双形态并存；布局坐标仅会话内有效，
// 不做坐标持久化（04 决策 O-3：维持 localStorage 方案，整体排 O8）。
// v1 边界：概念/关系/继承的结构编辑；实例仅在详情面板展示，增删走 Spec 编辑或 CSV 灌装。
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
  isNew?: boolean
}
type EditorFlowNode = Node<EditorNodeData, 'concept'>

/** Spec + 上一轮节点坐标 → 画布节点（保留已拖拽位置，新节点走分层布局） */
function deriveNodes(spec: Spec, prev: EditorFlowNode[]): EditorFlowNode[] {
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

  const nodes: EditorFlowNode[] = []
  for (const [d, cs] of rows) {
    cs.sort((a, b) => a.name.localeCompare(b.name))
    const rowW = cs.length * NODE_W + Math.max(0, cs.length - 1) * COL_GAP
    const startX = (width - rowW) / 2
    cs.forEach((c, i) => {
      const fallback = { x: startX + i * (NODE_W + COL_GAP), y: PAD + d * ROW_STEP }
      nodes.push({
        id: c.name,
        type: 'concept' as const,
        position: prevPos.get(c.name) ?? fallback,
        style: { width: NODE_W },
        data: { label: c.label || c.name, name: c.name, count: counts.get(c.name) ?? 0, definition: c.definition },
      })
    })
  }
  return nodes
}

/** Spec → 画布边：关系实线带标签，继承虚线（父 → 子，与只读视图同方向） */
function deriveEdges(spec: Spec): Edge[] {
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

const nodeTypes: NodeTypes = { concept: EditorConceptNode }

type Selection = { kind: 'node'; id: string } | { kind: 'edge'; id: string } | null

interface ConnDraft {
  source: string
  target: string
  kind: 'parent' | 'relation'
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
  const [connDraft, setConnDraft] = useState<ConnDraft | null>(null)
  const [addForm] = Form.useForm<{ name: string; label?: string; definition?: string; parents?: string[] }>()
  const [connForm] = Form.useForm<{ relName: string; relLabel?: string }>()

  // 外部 Spec 变化（切换本体 / 保存后刷新）→ 丢弃本地草稿
  useEffect(() => {
    setDraft(spec)
    setDirty(false)
    setSaveErrors(null)
    setSelection(null)
  }, [spec, ontologyId])

  const initialNodes = useMemo(() => (draft ? deriveNodes(draft, []) : []), [draft])
  const [nodes, setNodes, onNodesChange] = useNodesState<EditorFlowNode>(initialNodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>(deriveEdges(draft ?? { name: '', concepts: [], relations: [], instances: [] }))

  // 结构变化时以 draft 重建画布（保留已有节点坐标）
  const rebuild = useCallback(
    (next: Spec) => {
      setNodes((prev) => deriveNodes(next, prev))
      setEdges(deriveEdges(next))
    },
    [setNodes, setEdges],
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
      setConnDraft({ source: conn.source, target: conn.target, kind: 'relation' })
      connForm.setFieldsValue({ relName: `rel_${(draft.relations?.length ?? 0) + 1}`, relLabel: '' })
    },
    [draft, connForm],
  )

  const applyConnect = useCallback(() => {
    if (!draft || !connDraft) return
    const { source, target } = connDraft
    if (connDraft.kind === 'parent') {
      mutate({
        ...draft,
        concepts: draft.concepts.map((c) =>
          c.name === target && !(c.parents ?? []).includes(source) ? { ...c, parents: [...(c.parents ?? []), source] } : c,
        ),
      })
    } else {
      const v = connForm.getFieldsValue()
      const relName = (v.relName || '').trim()
      if (!relName) {
        showToast('关系名不能为空', 'err')
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
    }
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
      } else {
        const [, relName, from, to] = edgeId.split(':')
        mutate({
          ...draft,
          relations: (draft.relations ?? []).filter((r) => !(r.name === relName && r.from === from && r.to === to)),
        })
      }
      setSelection(null)
    },
    [draft, mutate],
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
    () => (draft && selection?.kind === 'node' ? draft.concepts.find((c) => c.name === selection.id) ?? null : null),
    [draft, selection],
  )
  const selRelEdge = useMemo(() => {
    if (!draft || selection?.kind !== 'edge') return null
    const [, relName, from, to] = selection.id.split(':')
    return (draft.relations ?? []).find((r) => r.name === relName && r.from === from && r.to === to) ?? null
  }, [draft, selection])
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
          <Tag>概念 {d.concepts.length}</Tag>
          <Tag>关系 {d.relations?.length ?? 0}</Tag>
          <Tag>实例 {d.instances?.length ?? 0}</Tag>
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
                拖拽节点边缘连线即创建关系/继承；点击节点或连线在右侧编辑；「添加概念」新建节点。保存走统一校验门控并递增版本。
              </p>
            )}
            {selNode && <NodePanel key={selNode.name} concept={selNode} draft={d} onApply={mutate} onRemove={removeConcept} />}
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
              连线 <Tag style={{ margin: 0 }}>{connDraft.source}</Tag> → <Tag style={{ margin: 0 }}>{connDraft.target}</Tag>
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
          <Form.Item name="kind" label="连线类型" initialValue="relation">
            <Radio.Group
              onChange={(e) => setConnDraft((c) => (c ? { ...c, kind: e.target.value } : c))}
              options={[
                { value: 'relation', label: '关系（实线，from → to）' },
                { value: 'parent', label: `继承（虚线，${connDraft?.source} 为父）` },
              ]}
            />
          </Form.Item>
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
