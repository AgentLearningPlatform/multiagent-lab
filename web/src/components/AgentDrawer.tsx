import { useEffect, useState } from 'react'
import { Button, Drawer, Form, Input, InputNumber, Popconfirm, Select, Space } from 'antd'
import { api } from '../api/client'
import type { Agent, ModelConnection } from '../api/types'
import { useUI } from '../store/ui'

/** 右侧属性抽屉：Agent 配置（P0 字段），保存后下次运行生效（配置驱动） */
export default function AgentDrawer({
  agent,
  onClose,
  onChanged,
}: {
  agent: Agent
  onClose: () => void
  onChanged: () => void
}) {
  const { showToast } = useUI()
  const [form] = Form.useForm()
  const [conns, setConns] = useState<ModelConnection[]>([])
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    form.setFieldsValue(agent)
    api.listConnections().then((cs) => setConns(cs.filter((c) => c.conn_type === 'chat' && c.enabled))).catch(() => {})
  }, [agent.id, form])

  const save = async () => {
    try {
      const v = await form.validateFields()
      setSaving(true)
      await api.updateAgent(agent.id, {
        name: v.name,
        description: v.description ?? '',
        instruction: v.instruction ?? '',
        model_conn_id: v.model_conn_id || null,
        temperature: v.temperature ?? null,
        max_tokens: v.max_tokens ?? null,
        max_iteration: v.max_iteration ?? 25,
        runtime_backend: v.runtime_backend ?? 'inprocess',
      })
      showToast('已保存，下次运行生效')
      onChanged()
    } catch (e: any) {
      if (e?.errorFields) return // 表单校验错误，antd 已提示
      showToast(e.message, 'err')
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    try {
      await api.deleteAgent(agent.id)
      showToast('已删除')
      onChanged()
      onClose()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  return (
    <Drawer
      open
      onClose={onClose}
      title="智能体属性"
      width={420}
      footer={
        <Space style={{ display: 'flex', justifyContent: 'space-between', width: '100%' }}>
          <Popconfirm title={`删除智能体「${agent.name}」？`} description="其历史对话将保留。" okText="删除" okButtonProps={{ danger: true }} cancelText="取消" onConfirm={remove}>
            <Button danger type="text">删除</Button>
          </Popconfirm>
          <Space>
            <Button onClick={onClose}>关闭</Button>
            <Button type="primary" loading={saving} onClick={save}>保存</Button>
          </Space>
        </Space>
      }
    >
      <Form form={form} layout="vertical" initialValues={agent} requiredMark={false}>
        <Form.Item name="name" label="名称" rules={[{ required: true, message: '名称必填' }]}>
          <Input placeholder="智能体名称" />
        </Form.Item>
        <Form.Item name="description" label="描述（用于多智能体协作时互相理解）">
          <Input.TextArea autoSize={{ minRows: 2, maxRows: 5 }} />
        </Form.Item>
        <Form.Item name="instruction" label="系统提示词（Instruction）">
          <Input.TextArea autoSize={{ minRows: 6, maxRows: 14 }} placeholder="定义角色、能力边界、回答风格…" />
        </Form.Item>
        <Form.Item name="model_conn_id" label="模型连接" extra={conns.length === 0 ? '当前无可用的 chat 连接；可到「设置-模型连接」新增。' : '没有合适的连接？到「设置-模型连接」新增。'}>
          <Select
            allowClear
            placeholder="跟随默认（在设置中指定）"
            options={conns.map((c) => ({ value: c.id, label: `${c.name} · ${c.model_name}` }))}
          />
        </Form.Item>
        <Space size={12} style={{ display: 'flex' }}>
          <Form.Item name="temperature" label="温度（0~2，留空默认）" style={{ width: 180 }}>
            <InputNumber min={0} max={2} step={0.1} style={{ width: '100%' }} placeholder="默认" />
          </Form.Item>
          <Form.Item name="max_tokens" label="最大回复 tokens" style={{ width: 180 }}>
            <InputNumber min={1} style={{ width: '100%' }} placeholder="默认" />
          </Form.Item>
        </Space>
        <Space size={12} style={{ display: 'flex' }}>
          <Form.Item name="max_iteration" label="最大迭代次数（ReAct 上限）" style={{ width: 180 }} initialValue={25}>
            <InputNumber min={1} max={100} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="runtime_backend" label="运行后端" style={{ width: 180 }} initialValue="inprocess" extra="M2 默认 inprocess；subprocess/容器后端在 M5 开放">
            <Input disabled />
          </Form.Item>
        </Space>
      </Form>
    </Drawer>
  )
}
