import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { Button, Col, Collapse, Divider, Form, Input, InputNumber, Modal, Row, Select, Space, Switch } from 'antd'
import { api, connDisplayName } from '../api/client'
import AIOptimizeButton from './AIOptimizeButton'
import type { InferenceBackendStatus, ModelConnection, ToolInfo } from '../api/types'
import { useUI } from '../store/ui'
import { inferenceBackendOptions } from './inferenceOptions'

/** 连接名已按 `{提供商}·{模型}` 约定时直接展示，否则补上模型名（兼容老数据） */
/** REQ-148：连接展示名（组别名优先替换提供商前缀，别名仅展示层） */
const connLabel = (c: ModelConnection) => connDisplayName(c)

/** 模型身份展示用：连接名按 `{提供商}·{模型}` 约定时取提供商前缀，否则取整名 */
const providerOfConn = (c: ModelConnection) => {
  const i = c.name.indexOf('·')
  return i > 0 ? c.name.slice(0, i) : c.name
}

/** 弹窗小节标题（左对齐小标题；inline 边距覆盖 antd Divider 默认间距） */
function Section({ children, first }: { children: ReactNode; first?: boolean }) {
  return (
    <Divider titlePlacement="left" plain style={{ margin: first ? '0 0 14px' : '6px 0 14px' }}>
      {children}
    </Divider>
  )
}

/**
 * 新建智能体弹窗（REQ-103：编辑用途已迁至右侧边栏配置视图，本弹窗仅用于新建）。
 * 布局分级（REQ-132②）：基本项（名称/描述/系统提示词/模型连接）填写即可创建；
 * 采样参数/工具/执行归入「高级配置」折叠区（默认收起，其余取默认：跟随全局模型/inprocess/审批 off）。
 * 短字段走两列 Row/Col，长文本整行 autoSize；创建后一律经右侧边栏修改（弹窗仅保留新建）。
 * M5：工具白名单来自工具注册表（api.listTools）；模型连接留空 = 跟随全局默认（M3）。
 */
export default function AgentModal({
  onClose,
  onCreated,
}: {
  onClose: () => void
  onCreated: (id: string) => void
}) {
  const { showToast, bumpData } = useUI()
  const [form] = Form.useForm()
  const instructionValue = Form.useWatch('instruction', form) ?? ''
  const [allConns, setAllConns] = useState<ModelConnection[]>([])
  const [tools, setTools] = useState<ToolInfo[]>([])
  const [toolsErr, setToolsErr] = useState(false)
  const [backends, setBackends] = useState<InferenceBackendStatus[]>([]) // M13：推理后端探测清单
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    api.listConnections().then(setAllConns).catch(() => {})
    api.listInferenceBackends().then((r) => setBackends(r.backends ?? [])).catch(() => {})
    // M5：工具注册表（失败降级为空 + 提示，不阻塞保存）
    api
      .listTools()
      .then((ts) => {
        setTools(ts)
        setToolsErr(false)
      })
      .catch(() => setToolsErr(true))
  }, [])

  // 可选 chat 连接（启用中）与生效的全局默认（默认连接须启用，与后端 GetDefaultConnection 语义一致）
  const conns = useMemo(() => allConns.filter((c) => c.conn_type === 'chat' && c.enabled), [allConns])
  const defaultConn = useMemo(
    () => allConns.find((c) => c.conn_type === 'chat' && c.is_default && c.enabled) ?? null,
    [allConns],
  )
  const modelConnId = Form.useWatch('model_conn_id', form)
  const selectedConn = modelConnId ? allConns.find((c) => c.id === modelConnId) ?? null : null

  const save = async () => {
    try {
      const v = await form.validateFields()
      setSaving(true)
      const a = await api.createAgent({
        name: v.name,
        description: v.description ?? '',
        instruction: v.instruction ?? '',
        model_conn_id: v.model_conn_id || null,
        temperature: v.temperature ?? null,
        max_tokens: v.max_tokens ?? null,
        max_iteration: v.max_iteration ?? 25,
        runtime_backend: v.runtime_backend ?? 'inprocess',
        inference_backend: v.inference_backend ?? 'eino-adk', // M13：推理后端（§6.16）
        companion_ontology: !!v.companion_ontology, // M28/REQ-170：伴生本体开关
        logo_url: (v.logo_url ?? '').trim(), // REQ-137：非内置后端登记原 logo
        tools: v.tools ?? [],
      })
      showToast('智能体已创建')
      bumpData()
      onCreated(a.id)
    } catch (e: any) {
      if (e?.errorFields) return // 表单校验错误，antd 已提示
      showToast(e.message, 'err')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open
      onCancel={onClose}
      title={
        <span className="modal-title">
          <span className="agent-tile agent-tile-sm">
            <span className="agent-glyph" />
          </span>
          新建智能体
        </span>
      }
      width={680}
      centered
      styles={{ body: { maxHeight: 'calc(100vh - 220px)', overflowY: 'auto', paddingRight: 8 } }}
      footer={
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={saving} onClick={save}>
            创建
          </Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical" initialValues={{ runtime_backend: 'inprocess', max_iteration: 25 }} requiredMark={false}>
        <Section first>基本信息</Section>
        <Form.Item name="name" label="名称" rules={[{ required: true, message: '名称必填' }]}>
          <Input placeholder="智能体名称" />
        </Form.Item>
        <Form.Item name="description" label="描述（用于多智能体协作时互相理解）">
          <Input.TextArea autoSize={{ minRows: 2, maxRows: 5 }} />
        </Form.Item>
        <Form.Item name="instruction" label={<Space size={6}>系统提示词（Instruction）<AIOptimizeButton kind="agent_instruction" value={instructionValue} onApply={(v) => form.setFieldValue('instruction', v)} /></Space>}>
          <Input.TextArea autoSize={{ minRows: 6, maxRows: 14 }} placeholder="定义角色、能力边界、回答风格…" />
        </Form.Item>

        <Section>模型</Section>
        <Form.Item
          name="model_conn_id"
          label="模型连接"
          extra={
            selectedConn ? (
              <span className="model-meta" title={selectedConn.base_url}>
                当前模型：{providerOfConn(selectedConn)} · <span className="model-meta-name">{selectedConn.model_name}</span>
              </span>
            ) : defaultConn ? (
              <span className="model-meta" title={defaultConn.base_url}>
                留空 = 跟随全局默认：{providerOfConn(defaultConn)} · <span className="model-meta-name">{defaultConn.model_name}</span>
              </span>
            ) : (
              <span className="model-meta warn">
                {conns.length === 0
                  ? '留空 = 跟随全局默认；当前无可用 chat 连接，可到「设置-模型管理」新增。'
                  : '留空 = 跟随全局默认；当前无启用的 chat 默认连接，可到「设置-模型管理」设置默认。'}
              </span>
            )
          }
        >
          <Select allowClear placeholder="跟随全局默认" options={conns.map((c) => ({ value: c.id, label: connLabel(c) }))} />
        </Form.Item>

        <Collapse
          ghost
          size="small"
          items={[{
            key: 'advanced',
            label: <span style={{ fontSize: 13 }}>高级配置（采样参数 / 工具 / 执行——默认取平台默认值，创建后可在右侧边栏调整）</span>,
            children: (
              <>
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
          extra={toolsErr ? '工具注册表暂不可用，可稍后重试。' : '来自工具注册表（内置 / 本体 / MCP 动态工具），勾选后随运行装配。'}
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
          <Col span={12}>
            <Form.Item
              name="inference_backend"
              label="推理后端"
              initialValue="eino-adk"
              extra="「谁来推理」：eino-adk 自研默认；外部 CLI 后端模型由其自身配置决定，技能/MCP 降级为提示注入"
            >
              <Select options={inferenceBackendOptions(backends)} showSearch optionFilterProp="label" />
            </Form.Item>
          </Col>
          <Col span={12}>
            <Form.Item
              name="tool_approval"
              label="工具调用人工审批"
              initialValue=""
              extra="开启后每次工具调用前挂起等待批准/拒绝（REQ-14 恢复语义）"
            >
              <Select
                options={[
                  { value: '', label: '关闭（直接执行）' },
                  { value: 'all', label: '全部工具调用前审批' },
                ]}
              />
            </Form.Item>
          </Col>
          <Col span={24}>
            <Form.Item
              name="logo_url"
              label="自定义后端 Logo URL（REQ-137）"
              extra="推理后端为自定义/外部部署（非内置）时，会话列表与对话界面将展示此图标；未配置回退默认图标"
            >
              <Input placeholder="https://…/logo.png" allowClear />
            </Form.Item>
          </Col>
          <Col span={24}>
            <Form.Item
              name="companion_ontology"
              label="伴生本体"
              valuePropName="checked"
              initialValue={false}
              extra="M28/REQ-170：对话收尾后旁路抽取知识图谱入伴生引擎；资产栏「伴生本体」页签可查询；默认关闭"
            >
              <Switch checkedChildren="开" unCheckedChildren="关" />
            </Form.Item>
          </Col>
        </Row>
              </>
            ),
          }]}
        />
      </Form>
    </Modal>
  )
}
