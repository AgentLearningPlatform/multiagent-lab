import { useEffect, useState } from 'react'
import { Button, Input, Popconfirm, Select, Space, Typography } from 'antd'
import type { Spec, SpecConcept, SpecInstance, SpecRelation } from '../../../../api/types'
import JsonEditor from '../JsonEditor'

// ---------------------------------------------------------------------------
// GraphEditor 选中节点属性面板（B1 拆分，REQ-145）。
// A2：实例属性 JSON 编辑统一 CodeMirror（语法高亮 + 行内 lint 标记，小高度）；
// apply 路径保留 try/catch 校验防崩，错误态内联提示。
// ---------------------------------------------------------------------------

/** 选中概念节点的属性编辑面板（key=name 保证切换节点时表单重置） */
export function NodePanel({
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
export function InstancePanel({
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
        <JsonEditor
          value={attrsText}
          onChange={(v) => {
            setAttrsText(v)
            setAttrsErr(null)
          }}
          height="96px"
          placeholder='属性 JSON，如 {"year": 2024}'
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

/** 选中连线详情（关系/继承/实例关系三形态；删除统一走 edgeId 回主组件解析） */
export function EdgeDetailPanel({
  edgeId,
  rel,
  parentEdge,
  instRel,
  onRemove,
}: {
  edgeId: string
  rel: SpecRelation | null
  parentEdge: { parent: string; child: string } | null
  instRel: { rel: string; from: string; to: string } | null
  onRemove: (edgeId: string) => void
}) {
  if (rel) {
    return (
      <div className="onto-flow-detail">
        <div className="onto-flow-detail-name">{rel.label || rel.name}</div>
        <div className="onto-flow-detail-key">
          {rel.from} → {rel.to}
        </div>
        {rel.definition && <p className="onto-flow-detail-def">{rel.definition}</p>}
        <Popconfirm title={`删除关系「${rel.name}」？`} onConfirm={() => onRemove(edgeId)}>
          <Button size="small" danger block style={{ marginTop: 8 }}>
            删除关系
          </Button>
        </Popconfirm>
      </div>
    )
  }
  if (parentEdge) {
    return (
      <div className="onto-flow-detail">
        <div className="onto-flow-detail-name">继承</div>
        <div className="onto-flow-detail-key">
          {parentEdge.parent} → {parentEdge.child}
        </div>
        <p className="onto-flow-detail-def muted">子概念继承父概念（虚线）</p>
        <Popconfirm title={`删除该继承（${parentEdge.child} 不再继承 ${parentEdge.parent}）？`} onConfirm={() => onRemove(edgeId)}>
          <Button size="small" danger block style={{ marginTop: 8 }}>
            删除继承
          </Button>
        </Popconfirm>
      </div>
    )
  }
  if (instRel) {
    return (
      <div className="onto-flow-detail">
        <div className="onto-flow-detail-name">实例关系 · {instRel.rel}</div>
        <div className="onto-flow-detail-key">
          {instRel.from} → {instRel.to}
        </div>
        <p className="onto-flow-detail-def muted">记录在 {instRel.from} 的 relations 上（细实线）</p>
        <Popconfirm title={`删除实例关系「${instRel.rel}」？`} onConfirm={() => onRemove(edgeId)}>
          <Button size="small" danger block style={{ marginTop: 8 }}>
            删除实例关系
          </Button>
        </Popconfirm>
      </div>
    )
  }
  return null
}
