import { useEffect, useState } from 'react'
import { Button, Checkbox, Form, Input, Modal, Popconfirm, Select, Space } from 'antd'
import { api } from '../api/client'
import type { Agent, Project } from '../api/types'
import { useUI } from '../store/ui'

/**
 * 项目配置弹窗（原型 06：每个项目节点提供 ⚙ 配置入口）：
 * 基本信息协作模式 + 成员智能体（主智能体/成员）+ 删除。
 */
export default function ProjectDrawer({
  project,
  agents,
  onClose,
  onChanged,
  onDeleted,
}: {
  project: Project
  agents: Agent[]
  onClose: () => void
  onChanged: () => void
  onDeleted: () => void
}) {
  const { showToast, bumpData } = useUI()
  const [form] = Form.useForm()
  const [selected, setSelected] = useState<Record<string, 'coordinator' | 'member'>>(() => {
    const init: Record<string, 'coordinator' | 'member'> = {}
    for (const id of project.agent_ids) init[id] = 'member'
    if (project.coordinator) init[project.coordinator] = 'coordinator'
    return init
  })
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    form.setFieldsValue({
      name: project.name,
      description: project.description,
      collab_mode: project.collab_mode ?? 'agent_as_tool',
      workflow_mode: project.workflow_mode ?? 'free',
      constraints: project.constraints,
    })
  }, [project.id, form])

  const save = async () => {
    let v: any
    try {
      v = await form.validateFields()
    } catch {
      return
    }
    const members = Object.entries(selected).map(([agent_id, role]) => ({ agent_id, role }))
    setSaving(true)
    try {
      await api.updateProject(project.id, v)
      await api.setProjectAgents(project.id, members)
      showToast('已保存')
      bumpData()
      onChanged()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    try {
      await api.deleteProject(project.id)
      showToast('已删除')
      bumpData()
      onDeleted()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  return (
    <Modal
      open
      onCancel={onClose}
      title={`项目配置 · ${project.name}`}
      width={560}
      footer={
        <Space style={{ display: 'flex', justifyContent: 'space-between', width: '100%' }}>
          <Popconfirm title={`删除项目「${project.name}」？`} description="其对话与消息将一并删除。" okText="删除" okButtonProps={{ danger: true }} cancelText="取消" onConfirm={remove}>
            <Button danger type="text">删除项目</Button>
          </Popconfirm>
          <Space>
            <Button onClick={onClose}>取消</Button>
            <Button type="primary" loading={saving} onClick={save}>保存</Button>
          </Space>
        </Space>
      }
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        <Form.Item name="name" label="名称" rules={[{ required: true, message: '名称必填' }]}>
          <Input />
        </Form.Item>
        <Form.Item name="description" label="描述">
          <Input.TextArea autoSize={{ minRows: 2, maxRows: 5 }} />
        </Form.Item>
        <Space size={12} style={{ display: 'flex' }}>
          <Form.Item name="collab_mode" label="协作模式（M4 生效）" style={{ width: 250 }}>
            <Select
              options={[
                { value: 'agent_as_tool', label: 'agent_as_tool（主智能体调度）' },
                { value: 'transfer', label: 'transfer（路由移交）' },
                { value: 'single', label: 'single（单智能体）' },
              ]}
            />
          </Form.Item>
          <Form.Item name="workflow_mode" label="工作流模式（M4 生效）" style={{ width: 250 }}>
            <Select
              options={[
                { value: 'free', label: 'free（自由协作）' },
                { value: 'sequential', label: 'sequential（顺序）' },
                { value: 'parallel', label: 'parallel（并行）' },
                { value: 'loop', label: 'loop（循环）' },
              ]}
            />
          </Form.Item>
        </Space>
        <Form.Item name="constraints" label="项目级约束（统一注入成员提示词，P1）">
          <Input.TextArea autoSize={{ minRows: 2, maxRows: 6 }} />
        </Form.Item>
      </Form>

      <div className="member-block">
        <div className="member-title">成员智能体（项目会话由主智能体调度，M4 生效）</div>
        {agents.length === 0 && <div className="empty-hint">还没有智能体，请先到「智能体」页创建</div>}
        {agents.map((a) => (
          <div key={a.id} className="member-row">
            <Checkbox
              checked={!!selected[a.id]}
              onChange={(e) =>
                setSelected((s) => {
                  const next = { ...s }
                  if (e.target.checked) next[a.id] = 'member'
                  else delete next[a.id]
                  return next
                })
              }
            >
              {a.name}
            </Checkbox>
            {selected[a.id] && (
              <Select
                size="small"
                style={{ width: 130 }}
                value={selected[a.id]}
                onChange={(role) => setSelected((s) => ({ ...s, [a.id]: role }))}
                options={[
                  { value: 'member', label: '成员' },
                  { value: 'coordinator', label: '主智能体' },
                ]}
              />
            )}
          </div>
        ))}
      </div>
    </Modal>
  )
}
