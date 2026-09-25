import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Empty, Form, Space, Splitter, Switch, Tag } from 'antd'
import {
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  ReactFlow,
  useEdgesState,
  useNodesState,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { api } from '../../../api/client'
import type { Spec, SpecRelation } from '../../../api/types'
import { useUI } from '../../../store/ui'
import { deriveEdges, deriveNodes, instNameOf } from './graph-editor/model'
import type { EditorFlowNode } from './graph-editor/model'
import { nodeTypes } from './graph-editor/nodes'
import { EdgeDetailPanel, InstancePanel, NodePanel } from './graph-editor/panels'
import { AddConceptModal, AddInstanceModal, ConnectModal } from './graph-editor/modals'
import { useEditorActions } from './graph-editor/useEditorActions'
import type { ConnDraft, Selection } from './graph-editor/types'

// ---------------------------------------------------------------------------
// REQ-71 图形化编辑器：React Flow 画布上直接编辑概念/关系/继承/实例，
// 保存时写回 spec_json 走既有 PUT 保存通道（校验门控、递增 version）。
// 与只读「可视化」Tab（SpecGraph）双形态并存；布局坐标仅会话内有效，
// 不做坐标持久化（04 决策 O-3）。
// v1.5：实例节点入画布（归属概念/实例间关系/增删与属性编辑，可开关显示）；
//       实例改名仍不进画布（name 是引用键，改名=级联重建，引导走 Spec 编辑）。
// B1（REQ-145）：派生规则/节点渲染/属性面板/弹窗/增删改动作拆至 graph-editor/。
// ---------------------------------------------------------------------------

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
  const [connForm] = Form.useForm<{ relName: string; relLabel?: string; kind?: 'relation' | 'parent' }>()

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
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges)

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

  const { onConnect, addConcept, addInstance, removeConcept, removeInstance, removeEdge } = useEditorActions({
    draft,
    mutate,
    showToast,
    addForm,
    addInstForm,
    connForm,
    setConnDraft,
    setAddOpen,
    setAddInstOpen,
    setSelection,
  })

  // 连线落子：依赖 connDraft 状态本体，留主组件
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
  const selEdgeId = selection?.kind === 'edge' ? selection.id : ''
  const selRelEdge = useMemo(() => {
    if (!draft || !selEdgeId.startsWith('rel:')) return null
    const [, relName, from, to] = selEdgeId.split(':')
    return (draft.relations ?? []).find((r) => r.name === relName && r.from === from && r.to === to) ?? null
  }, [draft, selEdgeId])
  const selInstRel = useMemo(() => {
    if (!selEdgeId.startsWith('instrel:')) return null
    const [, relName, from, to] = selEdgeId.split(':')
    return { rel: relName, from, to }
  }, [selEdgeId])
  const selParentEdge = useMemo(() => {
    if (!selEdgeId.startsWith('parent:')) return null
    const [, parent, child] = selEdgeId.split(':')
    return { parent, child }
  }, [selEdgeId])

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
          <div className="onto-flow-pane" role="application" aria-label="本体图谱画布（拖拽节点排版，点击节点/连线编辑属性）">
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
            {selection?.kind === 'edge' && (
              <EdgeDetailPanel
                edgeId={selection.id}
                rel={selRelEdge}
                parentEdge={selParentEdge}
                instRel={selInstRel}
                onRemove={removeEdge}
              />
            )}
          </div>
        </Splitter.Panel>
      </Splitter>

      <AddConceptModal
        open={addOpen}
        form={addForm}
        draft={d}
        onOk={addConcept}
        onCancel={() => {
          setAddOpen(false)
          addForm.resetFields()
        }}
      />
      <ConnectModal
        connDraft={connDraft}
        form={connForm}
        onOk={applyConnect}
        onCancel={() => setConnDraft(null)}
        onKindChange={(kind) => setConnDraft((c) => (c ? { ...c, kind } : c))}
      />
      <AddInstanceModal
        open={addInstOpen}
        form={addInstForm}
        draft={d}
        onOk={addInstance}
        onCancel={() => {
          setAddInstOpen(false)
          addInstForm.resetFields()
        }}
      />
    </div>
  )
}
