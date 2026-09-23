import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { Button, Divider, Form, Input, InputNumber, Popconfirm, Select, Space, Tag, Tooltip } from 'antd'
import {
  ApiOutlined,
  BranchesOutlined,
  CloseOutlined,
  DeleteOutlined,
  FolderOutlined,
  PlusOutlined,
  SettingOutlined,
} from '@ant-design/icons'
import { api } from '../api/client'
import type { Agent, InferenceBackendStatus, ModelConnection, ToolInfo } from '../api/types'
import { useUI } from '../store/ui'
import { inferenceBackendOptions } from './inferenceOptions'

/** 连接名已按 `{提供商}·{模型}` 约定时直接展示，否则补上模型名（兼容老数据） */
const connLabel = (c: ModelConnection) => (c.name.endsWith(`·${c.model_name}`) ? c.name : `${c.name} · ${c.model_name}`)

/** 模型身份展示用：连接名按 `{提供商}·{模型}` 约定时取提供商前缀，否则取整名 */
const providerOfConn = (c: ModelConnection) => {
  const i = c.name.indexOf('·')
  return i > 0 ? c.name.slice(0, i) : c.name
}

/** 侧边栏小节标题（左对齐小标题；窄面板收紧边距） */
function Section({ children, first }: { children: ReactNode; first?: boolean }) {
  return (
    <Divider titlePlacement="left" plain style={{ margin: first ? '0 0 12px' : '4px 0 12px' }}>
      {children}
    </Divider>
  )
}

/** 本地已知 MCP server 预设（REQ-99 ③ 通用挂载契约保留；D-O15 起 semantica 预设随「去-semantica 化」移除，
 *  通用 MCP servers 编辑能力不变，后续 open-ontologies 等预设随里程碑补入） */
const MCP_PRESETS: { name: string; url: string; desc: string }[] = []

/** MCP server 编辑行（name + url，Form.List 受控） */
function McpServerRow({ name, remove }: { name: number; remove: (i: number) => void }) {
  return (
    <Space.Compact block style={{ marginBottom: 6 }}>
      <Form.Item
        name={[name, 'name']}
        noStyle
        rules={[
          { required: true, message: '名称必填' },
          { pattern: /^[a-zA-Z0-9_-]+$/, message: '字母/数字/下划线/连字符' },
        ]}
      >
        <Input placeholder="名称（如 my-mcp）" style={{ width: '38%' }} />
      </Form.Item>
      <Form.Item
        name={[name, 'url']}
        noStyle
        rules={[
          { required: true, message: 'URL 必填' },
          { pattern: /^https?:\/\//, message: '须为 http(s) URL（Streamable HTTP MCP）' },
        ]}
      >
        <Input placeholder="http://127.0.0.1:8093/mcp" style={{ width: '52%' }} />
      </Form.Item>
      <Button icon={<DeleteOutlined />} onClick={() => remove(name)} aria-label="移除该 MCP server" />
    </Space.Compact>
  )
}

/**
 * 智能体右侧侧边栏（REQ-103 统一范式）：
 * 复用 ProjectSidePanel 的 activity bar（~44px）+ 视图区（~320px）结构。
 * 本轮 activity bar 仅「配置」可用，文件 / Git 为占位（后续扩展）。
 * 配置视图 = 原 AgentModal 编辑表单迁入（字段 / 校验 / 提交 API 不变，仅纵向排布适配窄面板）。
 */
export default function AgentSidePanel({
  agent,
  open,
  onClose,
  onChanged,
}: {
  agent: Agent
  open: boolean
  onClose: () => void
  onChanged?: () => void
}) {
  return (
    <aside className={`proj-panel${open ? ' open' : ''}`}>
      <div className="proj-panel-bar" role="tablist" aria-label="智能体侧边栏视图">
        <Tooltip title="配置" placement="left">
          <button type="button" className="proj-bar-btn active" aria-label="配置" aria-selected role="tab">
            <SettingOutlined />
          </button>
        </Tooltip>
        <Tooltip title="文件视图（后续扩展）" placement="left">
          <button type="button" className="proj-bar-btn" aria-label="文件视图（后续扩展）" disabled>
            <FolderOutlined />
          </button>
        </Tooltip>
        <Tooltip title="Git 视图（后续扩展）" placement="left">
          <button type="button" className="proj-bar-btn" aria-label="Git 视图（后续扩展）" disabled>
            <BranchesOutlined />
          </button>
        </Tooltip>
        <span className="proj-bar-spacer" />
        <Tooltip title="收起侧边栏" placement="left">
          <button type="button" className="proj-bar-btn" aria-label="收起侧边栏" onClick={onClose}>
            <CloseOutlined />
          </button>
        </Tooltip>
      </div>

      <div className="proj-panel-view">
        <AgentConfigForm agent={agent} onChanged={onChanged} />
      </div>
    </aside>
  )
}

// ---------------------------------------------------------------------------
// 配置视图（原 AgentModal 编辑表单迁入；字段 / 校验 / 提交逻辑不变）
// ---------------------------------------------------------------------------

function AgentConfigForm({ agent, onChanged }: { agent: Agent; onChanged?: () => void }) {
  const { showToast, bumpData } = useUI()
  const [form] = Form.useForm()
  const [allConns, setAllConns] = useState<ModelConnection[]>([])
  const [tools, setTools] = useState<ToolInfo[]>([])
  const [toolsErr, setToolsErr] = useState(false)
  const [backends, setBackends] = useState<InferenceBackendStatus[]>([]) // M13：推理后端探测清单
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)
  // MCP servers 实时值（预设挂载态判重用）。必须在组件顶层调用——Form.List 渲染槽内是
  // rc Field 类组件的 render 上下文，在其中调 useWatch 属非法 hook 调用，会整页白屏。
  const mcpWatched = (Form.useWatch('mcp_servers', form) ?? []) as { name?: string; url?: string }[]

  useEffect(() => {
    form.setFieldsValue(agent)
    api.listConnections().then(setAllConns).catch(() => {})
    // M5：工具注册表（失败降级为空 + 提示，不阻塞保存）
    api
      .listTools()
      .then((ts) => {
        setTools(ts)
        setToolsErr(false)
      })
      .catch(() => setToolsErr(true))
    // M13：推理后端探测清单（失败降级为仅 eino-adk 默认项）
    api.listInferenceBackends().then((r) => setBackends(r.backends ?? [])).catch(() => {})
  }, [agent.id, form])

  // 可选 chat 连接（启用中）与生效的全局默认（默认连接须启用，与后端 GetDefaultConnection 语义一致）
  const conns = useMemo(() => allConns.filter((c) => c.conn_type === 'chat' && c.enabled), [allConns])
  const defaultConn = useMemo(
    () => allConns.find((c) => c.conn_type === 'chat' && c.is_default && c.enabled) ?? null,
    [allConns],
  )
  // 当前选中连接（含已停用的历史绑定，便于如实展示身份）
  const modelConnId = Form.useWatch('model_conn_id', form)
  const selectedConn = modelConnId ? allConns.find((c) => c.id === modelConnId) ?? null : null

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
        inference_backend: v.inference_backend ?? 'eino-adk', // M13：推理后端（§6.16）
        tools: v.tools ?? [],
        // 后端 PUT 为 full-replace：保留当前挂载，避免未编辑字段被清空
        skills: agent.skills ?? [],
        mcp_servers: (v.mcp_servers ?? []).filter((s: { name?: string; url?: string }) => s?.name && s?.url),
      })
      showToast('已保存，下次运行生效')
      bumpData()
      onChanged?.()
    } catch (e: any) {
      if (e?.errorFields) return // 表单校验错误，antd 已提示
      showToast(e.message, 'err')
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    setDeleting(true)
    try {
      await api.deleteAgent(agent.id)
      showToast('已删除')
      bumpData()
      onChanged?.()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="proj-view-body">
      <Form form={form} layout="vertical" initialValues={agent} requiredMark={false} size="small">
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

        <Section>采样参数</Section>
        <Form.Item name="temperature" label="温度（0~2，留空默认）">
          <InputNumber min={0} max={2} step={0.1} style={{ width: '100%' }} placeholder="默认" />
        </Form.Item>
        <Form.Item name="max_tokens" label="最大回复 tokens">
          <InputNumber min={1} style={{ width: '100%' }} placeholder="默认" />
        </Form.Item>

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

        <Section>MCP Servers</Section>
        <div style={{ marginBottom: 8 }}>
          <ApiOutlined style={{ marginRight: 6 }} />
          <span className="model-meta">外部 MCP 工具源（Streamable HTTP）；工具以 <code>{'{server}__{tool}'}</code> 前缀并入白名单候选，连接失败降级不阻断运行。</span>
        </div>
        <Form.List name="mcp_servers">
          {(fields, { add, remove }) => (
            <>
              {fields.map(({ key, name }) => (
                <McpServerRow key={key} name={name} remove={remove} />
              ))}
              <Space wrap size={4}>
                <Button size="small" icon={<PlusOutlined />} onClick={() => add({ name: '', url: '' })}>
                  添加 Server
                </Button>
                {MCP_PRESETS.map((p) => {
                  const mounted = mcpWatched.some((s) => s?.name === p.name || s?.url === p.url)
                  return (
                    <Button
                      key={p.name}
                      size="small"
                      disabled={mounted}
                      onClick={() => add({ name: p.name, url: p.url })}
                      title={p.desc}
                    >
                      {mounted ? <Tag color="green" style={{ marginInlineEnd: 0 }}>已挂载 {p.name}</Tag> : `挂载 ${p.name}`}
                    </Button>
                  )
                })}
              </Space>
            </>
          )}
        </Form.List>

        <Section>执行</Section>
        <Form.Item name="max_iteration" label="最大迭代次数（ReAct 上限）" initialValue={25}>
          <InputNumber min={1} max={100} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item name="runtime_backend" label="运行后端" initialValue="inprocess" extra="M2 默认 inprocess；subprocess/容器后端在 M5 开放">
          <Input disabled />
        </Form.Item>
        <Form.Item
          name="inference_backend"
          label="推理后端"
          extra="「在哪儿跑」由运行后端决定，「谁来推理」由此决定：eino-adk 为平台自研（完整能力）；外部 CLI 后端模型由其自身配置决定（Agent 模型连接不生效），技能/MCP 降级为提示注入，不支持多 Agent 编排"
        >
          <Select
            options={inferenceBackendOptions(backends)}
            showSearch
            optionFilterProp="label"
            placeholder="eino-adk（自研默认）"
          />
        </Form.Item>
        <Form.Item
          name="tool_approval"
          label="工具调用人工审批"
          initialValue=""
          extra="开启后，本智能体每次调用工具前都会挂起等待你批准或拒绝（REQ-14 恢复语义 / 危险操作审批）；关闭则工具直接执行"
        >
          <Select
            options={[
              { value: '', label: '关闭（直接执行）' },
              { value: 'all', label: '全部工具调用前审批' },
            ]}
          />
        </Form.Item>
      </Form>

      <div className="proj-view-actions">
        <Button type="primary" size="small" loading={saving} onClick={save}>
          保存
        </Button>
        <Popconfirm
          title={`删除智能体「${agent.name}」？`}
          description="其历史对话将保留。"
          okText="删除"
          okButtonProps={{ danger: true }}
          cancelText="取消"
          onConfirm={remove}
        >
          <Button danger size="small" loading={deleting}>
            删除智能体
          </Button>
        </Popconfirm>
      </div>
    </div>
  )
}
