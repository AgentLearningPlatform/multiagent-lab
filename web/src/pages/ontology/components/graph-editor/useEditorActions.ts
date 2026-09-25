import { useCallback } from 'react'
import { Form } from 'antd'
import type { Connection } from '@xyflow/react'
import type { Spec, SpecConcept, SpecInstance } from '../../../../api/types'
import { INST_ID } from './model'
import type { ConnDraft } from './types'

// ---------------------------------------------------------------------------
// GraphEditor 增删改动作集（B1 拆分，REQ-145）：概念/实例/连线在 Spec 草稿上的
// 全部变更操作。draft 是编辑事实源，所有变更经 mutate 回主组件（置脏 + 重建画布）。
// ---------------------------------------------------------------------------

interface EditorActionsDeps {
  draft: Spec | null
  mutate: (next: Spec) => void
  showToast: (msg: string, type?: 'ok' | 'err') => void
  addForm: ReturnType<typeof Form.useForm<{ name: string; label?: string; definition?: string; parents?: string[] }>>[0]
  addInstForm: ReturnType<typeof Form.useForm<{ name: string; concept: string; attributes?: string }>>[0]
  connForm: ReturnType<typeof Form.useForm<{ relName: string; relLabel?: string; kind?: 'relation' | 'parent' }>>[0]
  setConnDraft: (d: ConnDraft | null) => void
  setAddOpen: (o: boolean) => void
  setAddInstOpen: (o: boolean) => void
  setSelection: (s: { kind: 'node'; id: string } | { kind: 'edge'; id: string } | null) => void
}

export function useEditorActions({
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
}: EditorActionsDeps) {
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
    [draft, connForm, showToast, setConnDraft],
  )

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
  }, [draft, mutate, addForm, showToast, setAddOpen, setSelection])

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
  }, [draft, mutate, addInstForm, showToast, setAddInstOpen, setSelection])

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
    [draft, mutate, setSelection],
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
    [draft, mutate, setSelection],
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
    [draft, mutate, showToast, setSelection],
  )

  return { onConnect, addConcept, addInstance, removeConcept, removeInstance, removeEdge }
}
