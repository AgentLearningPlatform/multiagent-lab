import { Form, Input, Modal, Radio, Select, Tag, Typography } from 'antd'
import type { FormInstance } from 'antd'
import type { Spec } from '../../../../api/types'
import { instNameOf } from './model'
import type { ConnDraft } from './types'
import JsonEditor from '../JsonEditor'

// ---------------------------------------------------------------------------
// GraphEditor 三个弹窗（B1 拆分，REQ-145）：添加概念 / 连线 / 添加实例。
// 表单实例由主组件持有（回调逻辑留主组件），此处仅受控呈现。
// A2：添加实例的属性输入用 CodeMirror JSON（语法高亮 + 行内 lint），校验规则不变。
// ---------------------------------------------------------------------------

export function AddConceptModal({
  open,
  form,
  draft,
  onOk,
  onCancel,
}: {
  open: boolean
  form: FormInstance<{ name: string; label?: string; definition?: string; parents?: string[] }>
  draft: Spec
  onOk: () => void
  onCancel: () => void
}) {
  return (
    <Modal title="添加概念" open={open} okText="添加" cancelText="取消" onOk={onOk} onCancel={onCancel} destroyOnHidden>
      <Form form={form} layout="vertical" initialValues={{ parents: [] }}>
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
          <Select mode="multiple" allowClear placeholder="选择已有概念" options={draft.concepts.map((c) => ({ value: c.name, label: c.label || c.name }))} />
        </Form.Item>
      </Form>
    </Modal>
  )
}

export function ConnectModal({
  connDraft,
  form,
  onOk,
  onCancel,
  onKindChange,
}: {
  connDraft: ConnDraft | null
  form: FormInstance<{ relName: string; relLabel?: string }>
  onOk: () => void
  onCancel: () => void
  onKindChange: (kind: 'relation' | 'parent') => void
}) {
  return (
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
      onOk={onOk}
      onCancel={onCancel}
      destroyOnHidden
    >
      <Form form={form} layout="vertical">
        {connDraft?.kind !== 'instance-relation' && (
          <Form.Item name="kind" label="连线类型" initialValue="relation">
            <Radio.Group
              onChange={(e) => onKindChange(e.target.value)}
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
  )
}

export function AddInstanceModal({
  open,
  form,
  draft,
  onOk,
  onCancel,
}: {
  open: boolean
  form: FormInstance<{ name: string; concept: string; attributes?: string }>
  draft: Spec
  onOk: () => void
  onCancel: () => void
}) {
  return (
    <Modal title="添加实例" open={open} okText="添加" cancelText="取消" onOk={onOk} onCancel={onCancel} destroyOnHidden>
      <Form form={form} layout="vertical">
        <Form.Item name="name" label="实例名（唯一标识）" rules={[{ required: true, message: '请输入实例名' }]}>
          <Input placeholder="如 《知识图谱》" />
        </Form.Item>
        <Form.Item name="concept" label="所属概念" rules={[{ required: true, message: '请选择所属概念' }]}>
          <Select placeholder="选择概念" options={draft.concepts.map((c) => ({ value: c.name, label: c.label || c.name }))} />
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
          <JsonEditor height="110px" placeholder='如 {"year": 2024}' />
        </Form.Item>
      </Form>
    </Modal>
  )
}
