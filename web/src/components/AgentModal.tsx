import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { Button, Col, Divider, Form, Input, InputNumber, Modal, Popconfirm, Row, Select, Space } from 'antd'
import { api } from '../api/client'
import type { Agent, ModelConnection, ToolInfo } from '../api/types'
import { useUI } from '../store/ui'

/** 连接名已按 `{提供商}·{模型}` 约定时直接展示，否则补上模型名（兼容老数据） */
const connLabel = (c: ModelConnection) => (c.name.endsWith(`·${c.model_name}`) ? c.name : `${c.name} · ${c.model_name}`)

/** 弹窗小节标题（左对齐小标题；inline 边距覆盖 antd Divider 默认间距） */
function Section({ children, first }: { children: ReactNode; first?: boolean }) {
  return (
    <Divider titlePlacement="left" plain style={{ margin: first ? '0 0 14px' : '6px 0 14px' }}>
      {children}
    </Divider>
  )
}

/**
 * 智能体属性弹窗（P0 字段），保存后下次运行生效（配置驱动）；与 NameModal 同一弹窗范式。
 * 布局分组：基本信息 → 模型 → 采样参数 → 工具 → 执行；短字段走两列 Row/Col，长文本整行 autoSize。
 * M5：工具白名单来自工具注册表（api.listTools），勾选落 agent.tools；模型连接留空 = 跟随全局默认（M3）。
 */
export default function AgentModal({
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
  const [tools, setTools] = useState<ToolInfo[]>([])
  const [toolsErr, setToolsErr] = useState(false)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    form.setFieldsValue(agent)
    api.listConnections().then((cs) => setConns(cs.filter((c) => c.conn_type === 'chat' && c.enabled))).catch(() => {})
    // M5：工具注册表（失败降级为空 + 提示，不阻塞保存）
    api.listTools().then((ts) => { setTools(ts); setToolsErr(false) }).catch(() => setToolsErr(true))
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
        tools: v.tools ?? [],
        // 后端 PUT 为 full-replace：保留当前挂载，避免未编辑字段被清空
        skills: agent.skills ?? [],
        mcp_servers: agent.mcp_servers ?? [],
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
    <Modal
      open
      onCancel={onClose}
      title="智能体属性"
      width={680}
      centered
      styles={{ body: { maxHeight: 'calc(100vh - 220px)', overflowY: 'auto', paddingRight: 8 } }}
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
        <Section first>基本信息</Section>
        <Form.Item name="name" label="名称" rules={[{ required: true, message: '名称必填' }]}>
          <Input placeholder="智能体名称" />
        </Form.Item>
        <Form.Item name="description" label="描述（用于多智能体协作时互相理解）">
          <Input.TextArea autoSize={{ minRows: 2, maxRows: 5 }} />
        </Form.Item>
        <Form.Item name="instruction" label="系统提示词（Instruction）">
          <Input.TextArea autoSize={{ minRows: 6, maxRows: 14 }} placeholder="定义角色、能力边界、回答风格…" />
        </Form.Item>

        <Section>模型</Section>
        <Form.Item
          name="model_conn_id"
          label="模型连接"
          extra={
            conns.length === 0
              ? '留空 = 跟随全局默认（设置中 chat 类型的默认连接）；当前无可用 chat 连接，可到「设置-模型管理」新增。'
              : '留空 = 跟随全局默认（设置中 chat 类型的默认连接）。'
          }
        >
          <Select
            allowClear
            placeholder="跟随全局默认"
            options={conns.map((c) => ({ value: c.id, label: connLabel(c) }))}
          />
        </Form.Item>

        <Section>采样参数</Section>
        <Row gutter={16}>
          <Col span={12}>
            <Form.Item name="temperature" label="温度（0~2，留空默认）">
              <InputNumber min={0} max={2} step={0.1} style={{ width: '100%' }} placeholder="默认" />
            </Form.Item>
          </Col>
          <Col span={12}>
            <Form.Item name="max_tokens" label="最大回复 tokens">
              <InputNumber min={1} style={{ width: '100%' }} placeholder="默认" />
            </Form.Item>
          </Col>
        </Row>

        <Section>工具</Section>
        <Form.Item
          name="tools"
          label="工具白名单"
          extra={toolsErr ? '工具注册表暂不可用，可稍后重开弹窗重试。' : '来自工具注册表（内置 / 本体 / MCP 动态工具），勾选后随运行装配。'}
        >
          <Select
            mode="multiple"
            allowClear
            virtual={false}
            placeholder={toolsErr ? '工具注册表暂不可用' : '选择可用工具'}
            options={tools.map((t) => ({ value: t.id, label: t.name, title: t.description, source: t.source }))}
            notFoundContent={toolsErr ? '工具注册表暂不可用' : '暂无工具'}
            classNames={{ popup: { root: 'tool-select-popup' } }}
            optionRender={(opt) => (
              <div className="tool-option">
                <div className="tool-option-name">
                  <span>{opt.data?.label}</span>
                  {opt.data?.source ? <span className="tool-option-src">{opt.data.source}</span> : null}
                </div>
                {opt.data?.title ? <div className="tool-option-desc">{opt.data.title}</div> : null}
              </div>
            )}
          />
        </Form.Item>

        <Section>执行</Section>
        <Row gutter={16}>
          <Col span={12}>
            <Form.Item name="max_iteration" label="最大迭代次数（ReAct 上限）" initialValue={25}>
              <InputNumber min={1} max={100} style={{ width: '100%' }} />
            </Form.Item>
          </Col>
          <Col span={12}>
            <Form.Item name="runtime_backend" label="运行后端" initialValue="inprocess" extra="M2 默认 inprocess；subprocess/容器后端在 M5 开放">
              <Input disabled />
            </Form.Item>
          </Col>
        </Row>
      </Form>
    </Modal>
  )
}
