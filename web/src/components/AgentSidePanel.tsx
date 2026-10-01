import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Badge, Breadcrumb, Button, Card, Checkbox, Collapse, Divider, Empty, Form, FormInstance, Input, InputNumber, Modal, Popconfirm, Segmented, Select, Space, Spin, Switch, Tabs, Tag, Tooltip, Typography } from 'antd'
import {
  ApiOutlined,
  CloudOutlined,
  DoubleLeftOutlined,
  DoubleRightOutlined,
  ClusterOutlined,
  BranchesOutlined,
  CopyOutlined,
  ExportOutlined,
  FileOutlined,
  FolderOutlined,
  ReloadOutlined,
  SettingOutlined,
  ShareAltOutlined,
} from '@ant-design/icons'
import { api, connDisplayName } from '../api/client'
import AIOptimizeButton from './AIOptimizeButton'
import AgentCompanionManage from './AgentCompanionManage'
import AssistantProposalBanner from './AssistantProposalBanner'
import type { SandboxStatus } from '../api/client'
import type { Agent, AgentConfigVersion, Connector, InferenceBackendStatus, McpServeInfo, ModelConnection, Ontology, Skill, ToolInfo } from '../api/types'
import { companionApi } from '../api/companion'
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

/** REQ-189：侧板宽度三常量与 localStorage 键（拖拽 clamp 320~720，记忆 eino.agentpanel.width） */
const PANEL_WIDTH_KEY = 'eino.agentpanel.width'
const DEFAULT_PANEL_WIDTH = 560
const MIN_PANEL_WIDTH = 320
const MAX_PANEL_WIDTH = 720
// REQ-21705/M48: activity bar shu-tiao width (collapsed rail)
const PANEL_BAR_WIDTH = 44

/** REQ-214/M46：连接器类型徽标（产品层只呈现「连接器」，MCP 为交付驱动之一） */
const CONNECTOR_KINDS: { value: Connector['kind']; label: string; color: string }[] = [
  { value: 'mcp', label: '自定义 MCP', color: 'blue' },
  { value: 'kubernetes', label: 'Kubernetes', color: 'purple' },
  { value: 'ssh', label: 'SSH', color: 'cyan' },
]
const kindMeta = (k: string) => CONNECTOR_KINDS.find((x) => x.value === k) ?? CONNECTOR_KINDS[0]

/**
 * 智能体右侧侧边栏（REQ-103 统一范式 + REQ-132 四分类改版 / M18；REQ-193/M33 双入口）：
 * activity bar（~44px）双入口——「配置」（AgentConfigForm 八页签：基本/模型与参数/Context/
 * Harness/Loop/Graph/能力/对外服务——REQ-219/M50 五层视角落侧板：Context/Harness/Loop/Graph
 * 四层与「模型与参数」（Model 层）并列成栏，「配置聚合呈现≠层职责混淆」；REQ-218/M49 提级
 * activity bar 时本页签区随迁）与「伴生本体」（AgentCompanionView：伴生配置/伴生管理两页）
 * 同级切换（REQ-193 伴生自页签上提一级；选中记忆 localStorage eino.agentpanel.view）。
 * 页签面板 forceRender（跨页签字段同表单提交）；字段/校验/提交 API 不变，仅承载重组。
 */
const PANEL_VIEW_KEY = 'eino.agentpanel.view'

// REQ-213：内置助手基座工具（L0 五只读 + L1 平台知识/提案三件）——内置行不可摘除（禁用勾选态）。
const ASSISTANT_BASE_TOOLS = ['doc_read', 'list_model_connections', 'list_agents', 'list_kbs',
  'get_assistant_config', 'sync_platform_kb', 'search_platform_kb', 'propose_assistant_config']

export default function AgentSidePanel({
  agent,
  open,
  onOpenChange,
  onChanged,
}: {
  agent: Agent
  /** 展开态（true=内容面板，false=竖条）；REQ-237：面板常驻挂载（竖条对齐 REQ-217⑤「常驻右侧」定案），open 仅控制展开/收缩 */
  open: boolean
  /** 展开态变化（竖条点入口展开=true、再点收起=false）；与 ChatWindow 头部收放按钮同源 */
  onOpenChange?: (open: boolean) => void
  onChanged?: () => void
}) {
  // REQ-218①/M49：activity bar 六入口平级——智能体配置 / 模型 / 连接器 / 对外服务 / 文件 /
  // 伴生本体（模型/连接器/对外服务自配置视图页签提级；文件为 REQ-218④ work_dir 浏览视图）。
  // REQ-217⑤/M48：侧板默认收缩为一竖行按钮常驻右侧——点击按钮展开内容、再点同一按钮收起；
  // 宽度记忆保留（展开时生效）。
  type PanelView = 'config' | 'model' | 'connectors' | 'serve' | 'files' | 'companion'
  const [view, setView] = useState<PanelView>(() => {
    const saved = localStorage.getItem(PANEL_VIEW_KEY) as PanelView | null
    if (saved === 'companion' && agent.is_builtin) return 'config'
    return saved ?? 'config'
  })
  const [collapsed, setCollapsed] = useState(true) // 默认竖条态（REQ-217⑤「默认收缩」）
  // REQ-237：常驻挂载后 open=展开态权威（头部收放按钮/竖条入口双向同步）
  useEffect(() => {
    setCollapsed(!open)
  }, [open])
  // REQ-230①：候选触达——伴生入口红点（pending>0 亮起；选中 agent 即取数+对话运行后 dataVersion 刷新）
  const { dataVersion } = useUI()
  const [companionPending, setCompanionPending] = useState(0)
  useEffect(() => {
    if (agent.is_builtin || !agent.companion_ontology_id) {
      setCompanionPending(0)
      return
    }
    let alive = true
    companionApi
      .status(agent.id)
      .then((st) => {
        if (alive) setCompanionPending(st.pending_count ?? 0)
      })
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [agent.id, agent.companion_ontology_id, dataVersion])
  const switchView = (v: PanelView) => {
    if (v === view && !collapsed) {
      setCollapsed(true) // 再点同一按钮 = 收起回竖条态
      onOpenChange?.(false)
      return
    }
    setView(v)
    if (collapsed) onOpenChange?.(true)
    setCollapsed(false)
    localStorage.setItem(PANEL_VIEW_KEY, v)
  }
  // REQ-189：侧板宽度可调（默认上调 364→560；拖拽 320~720；localStorage 记忆；双击复位）
  const [panelWidth, setPanelWidth] = useState(() => {
    const saved = Number(localStorage.getItem(PANEL_WIDTH_KEY))
    return saved >= MIN_PANEL_WIDTH && saved <= MAX_PANEL_WIDTH ? saved : DEFAULT_PANEL_WIDTH
  })
  const startResize = (e: React.MouseEvent) => {
    e.preventDefault()
    const startX = e.clientX
    const startW = panelWidth
    const onMove = (ev: MouseEvent) => {
      const w = Math.min(MAX_PANEL_WIDTH, Math.max(MIN_PANEL_WIDTH, startW + (startX - ev.clientX)))
      setPanelWidth(w)
    }
    const onUp = () => {
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
      setPanelWidth((w) => {
        localStorage.setItem(PANEL_WIDTH_KEY, String(w))
        return w
      })
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }
  return (
    <aside className="proj-panel" style={{ width: collapsed ? PANEL_BAR_WIDTH : panelWidth }}>
      {!collapsed && (
        <div
          className="proj-panel-resizer"
          role="separator"
          aria-label="拖拽调整侧边栏宽度"
          aria-orientation="vertical"
          onMouseDown={(e) => startResize(e)}
          onDoubleClick={() => { setPanelWidth(DEFAULT_PANEL_WIDTH); localStorage.setItem(PANEL_WIDTH_KEY, String(DEFAULT_PANEL_WIDTH)) }}
        />
      )}
      <div className="proj-panel-bar" role="tablist" aria-label="智能体侧边栏视图">
        {/* REQ-218①/M49 六入口平级 + REQ-217⑤/M48 再点收起（竖条态常驻默认） */}
        <Tooltip title="智能体配置" placement="left">
          <button
            type="button"
            className={`proj-bar-btn${view === 'config' && !collapsed ? ' active' : ''}`}
            aria-label="智能体配置"
            aria-selected={view === 'config' && !collapsed}
            role="tab"
            onClick={() => switchView('config')}
          >
            <SettingOutlined />
          </button>
        </Tooltip>
        <Tooltip title="模型" placement="left">
          <button
            type="button"
            className={`proj-bar-btn${view === 'model' && !collapsed ? ' active' : ''}`}
            aria-label="模型"
            aria-selected={view === 'model' && !collapsed}
            role="tab"
            onClick={() => switchView('model')}
          >
            <CloudOutlined />
          </button>
        </Tooltip>
        <Tooltip title="连接器" placement="left">
          <button
            type="button"
            className={`proj-bar-btn${view === 'connectors' && !collapsed ? ' active' : ''}`}
            aria-label="连接器"
            aria-selected={view === 'connectors' && !collapsed}
            role="tab"
            onClick={() => switchView('connectors')}
          >
            <ApiOutlined />
          </button>
        </Tooltip>
        <Tooltip title="对外服务" placement="left">
          <button
            type="button"
            className={`proj-bar-btn${view === 'serve' && !collapsed ? ' active' : ''}`}
            aria-label="对外服务"
            aria-selected={view === 'serve' && !collapsed}
            role="tab"
            // REQ-213：内置行不开放对外服务（mcp_serve 锁死）
            hidden={!!agent.is_builtin}
            onClick={() => switchView('serve')}
          >
            <ShareAltOutlined />
          </button>
        </Tooltip>
        <Tooltip title="文件" placement="left">
          <button
            type="button"
            className={`proj-bar-btn${view === 'files' && !collapsed ? ' active' : ''}`}
            aria-label="文件"
            aria-selected={view === 'files' && !collapsed}
            role="tab"
            onClick={() => switchView('files')}
          >
            <FolderOutlined />
          </button>
        </Tooltip>
        <Tooltip title={companionPending > 0 ? `伴生本体（${companionPending} 条候选待确认）` : '伴生本体'} placement="left">
          <Badge count={companionPending} size="small" offset={[-2, 2]}>
            <button
              type="button"
              className={`proj-bar-btn${view === 'companion' && !collapsed ? ' active' : ''}`}
              aria-label="伴生本体"
              aria-selected={view === 'companion' && !collapsed}
              role="tab"
              // REQ-213：内置助手（平台辅助角色）无伴生诉求，伴生视图入口隐藏
              hidden={!!agent.is_builtin}
              onClick={() => switchView('companion')}
            >
              <ClusterOutlined />
            </button>
          </Badge>
        </Tooltip>
        <Tooltip title="Git 视图（后续扩展）" placement="left">
          <button type="button" className="proj-bar-btn" aria-label="Git 视图（后续扩展）" disabled>
            <BranchesOutlined />
          </button>
        </Tooltip>
        <span className="proj-bar-spacer" />
        {/* REQ-237：竖条常驻后无「关闭」态——展开/收起即开合（原「关闭侧边栏」按钮退役，与收起语义重复） */}
        <Tooltip title={collapsed ? '展开侧边栏' : '收起为竖条'} placement="left">
          <button
            type="button"
            className="proj-bar-btn"
            aria-label={collapsed ? '展开侧边栏' : '收起为竖条'}
            onClick={() => {
              if (collapsed) {
                onOpenChange?.(true)
                setCollapsed(false)
              } else {
                switchView(view)
              }
            }}
          >
            {collapsed ? <DoubleRightOutlined /> : <DoubleLeftOutlined />}
          </button>
        </Tooltip>
      </div>

      {!collapsed && (
        <div className="proj-panel-view">
          {view === 'companion' && <AgentCompanionView agent={agent} onChanged={onChanged} />}
          {view === 'files' && <AgentFilesView agent={agent} onChanged={onChanged} />}
          {(view === 'config' || view === 'model' || view === 'connectors' || view === 'serve') && (
            <AgentConfigForm
              agent={agent}
              onChanged={onChanged}
              visibleTabs={view === 'config' ? undefined : [view === 'model' ? 'model' : view]}
            />
          )}
        </div>
      )}
    </aside>
  )
}

// ---------------------------------------------------------------------------
// 伴生本体视图（REQ-193/M33：伴生自「配置」页签上提一级，与「智能体配置」同级；
// 视图内两页——「伴生配置」（开关+REQ-187 三字段，独立表单全量保存）与
// 「伴生管理」（AgentCompanionManage 铺平视图）。「左侧对话、右侧伴生管理」并行
// 几何已具备（ChatWindow 与本侧板 flex 兄弟节点）。整体摘除/成长图 3D 留本体模块
// 伴生子模块全量管理面（D-O19/D-O21 展示位不动）。
// ---------------------------------------------------------------------------

function AgentCompanionView({ agent, onChanged }: { agent: Agent; onChanged?: () => void }) {
  const { showToast, bumpData } = useUI()
  const [page, setPage] = useState<'config' | 'manage'>('config')
  const [form] = Form.useForm()
  const [allConns, setAllConns] = useState<ModelConnection[]>([])
  const [ontos, setOntos] = useState<Ontology[]>([])
  const [saving, setSaving] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')

  useEffect(() => {
    form.setFieldsValue({
      companion_ontology_id: agent.companion_ontology_id ?? '',
      companion_extract_hint: agent.companion_extract_hint ?? '',
      companion_extract_conn_id: agent.companion_extract_conn_id ?? '',
      companion_auto_threshold: agent.companion_auto_threshold ?? 0,
    })
  }, [agent.id, agent.companion_ontology_id, form])
  useEffect(() => {
    api.listConnections().then(setAllConns).catch(() => {})
    // REQ-216：绑定候选 = 全部本体资产（对话生长产物与其他本体同仓，可复用既有本体沉淀）
    api.listOntologies().then(setOntos).catch(() => {})
  }, [])

  const conns = useMemo(() => allConns.filter((c) => c.conn_type === 'chat' && c.enabled), [allConns])

  // REQ-216②：一键创建空本体（建议名「{agent 名}的伴生本体」）→ 立即绑定
  const doCreate = async () => {
    setCreating(true)
    try {
      const r = await companionApi.bindAgent(agent.id, { create: { name: createName.trim() || undefined } })
      showToast(`已创建并绑定「${r.ontology_id}」的伴生本体` + (r.plan_error ? `（宿主方案暂未就绪：${r.plan_error}）` : '，宿主方案已就绪'))
      setCreateOpen(false)
      form.setFieldsValue({ companion_ontology_id: r.ontology_id })
      bumpData()
      onChanged?.()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setCreating(false)
    }
  }

  // 保存走全量载荷（普通 agent PUT 为 full-replace）：非伴生字段以 props agent 现值透传，
  // 仅伴生字段来自本表单——与其他视图保存互不踩踏（REQ-189 全字段替换坑同源规避）。
  // REQ-216：绑定变更走 bind/unbind 专端点（绑定即确保宿主方案；关闭=解绑），
  // companion_ontology_id 随载荷落库（派生开关 companion_ontology 由后端回填）。
  const save = async () => {
    try {
      const v = await form.validateFields()
      const targetOnt: string = v.companion_ontology_id ?? ''
      if (targetOnt === '' && (agent.companion_ontology_id ?? '') !== '') {
        // REQ-230④：解绑不可逆（清候选+游标）——保存路径补确认弹窗（管理页解绑按钮已有 Popconfirm）
        Modal.confirm({
          title: '解绑伴生本体？',
          content: '将清空该智能体全部待确认候选与抽取游标并断开绑定；本体伴生子图数据保留。确认继续保存？',
          okText: '解绑并保存',
          okButtonProps: { danger: true },
          cancelText: '取消',
          onOk: () => doSave(),
        })
        return
      }
      await doSave()
    } catch (e: any) {
      if (e?.errorFields) return
      showToast(e.message, 'err')
      setSaving(false)
    }
  }

  const doSave = async () => {
    try {
      const v = await form.getFieldsValue()
      const targetOnt: string = v.companion_ontology_id ?? ''
      setSaving(true)
      if (targetOnt !== (agent.companion_ontology_id ?? '')) {
        if (targetOnt === '') {
          await companionApi.resetAgent(agent.id) // 解绑：清该 agent 候选与游标（本体伴生子图数据保留）
        } else {
          const r = await companionApi.bindAgent(agent.id, { ontology_id: targetOnt })
          if (r.plan_error) showToast(`绑定成功，但宿主方案暂未就绪（读路径会自动拉起）: ${r.plan_error}`, 'err')
        }
      }
      await api.updateAgent(agent.id, {
        name: agent.name,
        description: agent.description,
        instruction: agent.instruction,
        model_conn_id: agent.model_conn_id || null,
        temperature: agent.temperature ?? null,
        max_tokens: agent.max_tokens ?? null,
        max_iteration: agent.max_iteration ?? 25,
        runtime_backend: agent.runtime_backend ?? 'inprocess',
        sandbox_memory: agent.sandbox_memory ?? '',
        sandbox_cpus: agent.sandbox_cpus ?? 0,
        inference_backend: agent.inference_backend ?? 'eino-adk',
        logo_url: (agent.logo_url ?? '').trim(),
        context_mode: agent.context_mode ?? '', // REQ-201：full-replace 透传防清零（伴生表单不含此字段）
        work_dir: agent.work_dir ?? '', // REQ-202：透传防清零
        verify_command: agent.verify_command ?? '', // REQ-202：透传防清零
        tools: agent.tools ?? [],
        skills: agent.skills ?? [],
        mcp_servers: agent.mcp_servers ?? [],
        mcp_serve: { enabled: !!agent.mcp_serve?.enabled, tool_name: agent.mcp_serve?.tool_name ?? '', token: agent.mcp_serve?.token ?? '' },
        companion_ontology_id: targetOnt,
        companion_extract_hint: v.companion_extract_hint ?? '',
        companion_extract_conn_id: v.companion_extract_conn_id ?? '',
        companion_auto_threshold: v.companion_auto_threshold ?? 0,
      })
      showToast('伴生配置已保存，下次运行生效')
      bumpData()
      onChanged?.()
    } catch (e: any) {
      if (e?.errorFields) return
      showToast(e.message, 'err')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="proj-view-body">
      {/* REQ-195：两页各带标题+一句话说明（页切换右置），替代裸 Segmented 孤行 */}
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 8, marginBottom: 10 }}>
        <div style={{ minWidth: 0 }}>
          <Typography.Text strong style={{ fontSize: 13 }}>{page === 'config' ? '伴生配置' : '伴生管理'}</Typography.Text>
          <div>
            <Typography.Text type="secondary" style={{ fontSize: 11 }}>
              {page === 'config'
                ? '绑定伴生本体与抽取字段（REQ-216）；保存后下次运行生效'
                : '本智能体跨会话候选确认流（铺平视图）；入图写入绑定本体伴生子图'}
            </Typography.Text>
          </div>
        </div>
        <Segmented
          size="small"
          value={page}
          onChange={(v) => setPage(v as typeof page)}
          options={[
            { value: 'config', label: '伴生配置' },
            { value: 'manage', label: '伴生管理' },
          ]}
        />
      </div>
      {page === 'config' ? (
        <Form form={form} layout="vertical" requiredMark={false} size="small">
          <Form.Item
            name="companion_ontology_id"
            label="伴生本体（绑定，REQ-216）"
            extra="对话收尾抽取的知识写入该本体的伴生子图——多智能体绑同一本体即共享沉淀；绑定即确保运行平面宿主方案自动创建并运行。切换/清空绑定：清空=解绑（该智能体候选与游标清除，本体伴生子图数据保留）"
          >
            <Select
              allowClear
              showSearch
              optionFilterProp="label"
              placeholder="选择本体开启伴生（不选=关闭）"
              options={[
                ...ontos.map((o) => ({ value: o.id, label: `${o.name}（${o.id.slice(0, 14)}…）` })),
                { value: '__create__', label: '✚ 创建新伴生本体（空本体）' },
              ]}
              onChange={(v) => {
                if (v === '__create__') {
                  setCreateName(companionOntologyNameOf(agent))
                  setCreateOpen(true)
                  form.setFieldsValue({ companion_ontology_id: agent.companion_ontology_id ?? '' })
                }
              }}
            />
          </Form.Item>
          <Form.Item
            name="companion_extract_hint"
            label="领域聚焦提示（可选，REQ-187）"
            extra="追加到抽取提示词：定义本 agent 领域内「什么值得沉淀」（例：重点关注 Kubernetes 部署与回滚术语；忽略寒暄与操作细节）"
          >
            <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} placeholder="留空 = 通用抽取标准" />
          </Form.Item>
          <Form.Item
            name="companion_extract_conn_id"
            label="抽取模型连接（可选，REQ-187）"
            extra="留空 = 跟随「智能体配置」的模型连接（外部 CLI 后端 agent 无生效连接时须指定真实 chat 连接）"
          >
            <Select allowClear showSearch optionFilterProp="label" placeholder="跟随智能体模型连接" options={conns.map((c) => ({ value: c.id, label: connLabel(c) }))} />
          </Form.Item>
          <Form.Item
            name="companion_auto_threshold"
            label="自动入图置信阈值（REQ-187）"
            extra="0 = 全部候选人工确认（默认，REQ-82 草稿必审）；>0 时置信 ≥ 阈值的候选自动确认入图（图内带 autoConfirmed 标记），其余仍待人工审"
          >
            <InputNumber min={0} max={1} step={0.05} style={{ width: '100%' }} placeholder="0（全人工确认）" />
          </Form.Item>
          <div className="proj-view-actions">
            <Button type="primary" size="small" loading={saving} onClick={save}>
              保存伴生配置
            </Button>
          </div>
        </Form>
      ) : agent.companion_ontology ? (
        <AgentCompanionManage agent={agent} />
      ) : (
        // REQ-195：开关未开启时管理页不再照常呈现（此前与开关状态脱节）——引导先开启
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          style={{ padding: '24px 0' }}
          description={<span style={{ fontSize: 12 }}>伴生未开启——绑定伴生本体并对话一轮后，抽取候选在此跨会话确认入图</span>}
        >
          <Button type="primary" size="small" onClick={() => setPage('config')}>
            去绑定伴生本体
          </Button>
        </Empty>
      )}

      {/* REQ-216②：创建空本体弹窗（建议名默认「{agent 名}的伴生本体」） */}
      <Modal
        title="创建伴生本体"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={doCreate}
        okText="创建并绑定"
        okButtonProps={{ loading: creating }}
        cancelText="取消"
        width={420}
      >
        <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
          创建一个空本体作为本智能体的伴生归属（REQ-216 资产化：对话生长产物入其伴生子图，
          在「本体资产」列表可见并随「对话生长」徽标呈现）。
        </Typography.Paragraph>
        <Input value={createName} onChange={(e) => setCreateName(e.target.value)} placeholder="伴生本体名称" />
      </Modal>
    </div>
  )
}

/** REQ-216②：伴生本体建议名（与后端 CompanionOntologyName 同口径）。 */
function companionOntologyNameOf(agent: Agent): string {
  return `${agent.name || '未命名智能体'}的伴生本体`
}

/**
 * REQ-219 顺修：普通 agent PUT 为 full-replace（store.UpdateAgent 全字段写入，空值也落库），
 * 载荷必须全字段构造——表单值 + 非本表单字段按 agent 现值透传防清零（REQ-189/213 模式）。
 * 保存与 SandboxPanel 启动共用（此前启动沙箱仅 PUT 两字段，会把其余配置清零）。
 */
function agentFullPayload(agent: Agent, v: Record<string, any>) {
  return {
    name: v.name ?? agent.name,
    description: v.description ?? '',
    instruction: v.instruction ?? '',
    model_conn_id: v.model_conn_id || null,
    temperature: v.temperature ?? null,
    max_tokens: v.max_tokens ?? null,
    max_iteration: v.max_iteration ?? 25,
    runtime_backend: v.runtime_backend ?? 'inprocess',
    sandbox_memory: v.sandbox_memory ?? '',
    sandbox_cpus: v.sandbox_cpus ?? 0,
    inference_backend: v.inference_backend ?? 'eino-adk',
    context_mode: v.context_mode ?? '', // REQ-201/M37：上下文预算档位（Context 层）
    work_dir: (v.work_dir ?? '').trim(), // REQ-202/M38：文件原语安全根（Harness 层）
    verify_command: (v.verify_command ?? '').trim(), // REQ-202/M38：verify_on_stop 背压（Harness 层）
    tool_approval: v.tool_approval ?? '', // REQ-219 顺修：表单项此前存在但未入载荷（审批开关保存不生效）
    approval_exempt: v.approval_exempt ?? [], // REQ-231②：审批豁免清单
    approval_timeout_hours: v.approval_timeout_hours ?? 0, // REQ-231③：挂起超时
    logo_url: (v.logo_url ?? '').trim(), // REQ-137
    tools: v.tools ?? [],
    // 后端 PUT 为 full-replace：保留当前挂载，避免未编辑字段被清空
    skills: agent.skills ?? [],
    connectors: (v.connectors ?? []) as string[], // REQ-214/M46：连接器授权白名单
    mcp_servers: agent.mcp_servers ?? [], // REQ-214：兼容残留透传（迁移后恒空）
    // REQ-131/M18：对外服务（token 原样保留——重置走专用端点）
    mcp_serve: {
      enabled: !!v.mcp_serve_enabled,
      tool_name: (v.mcp_serve_tool_name ?? '').trim(),
      token: agent.mcp_serve?.token ?? '',
    },
    // REQ-170/187 + REQ-193：伴生字段自「伴生本体」视图维护——按 agent 现值透传
    companion_ontology_id: agent.companion_ontology_id ?? '',
    companion_extract_hint: agent.companion_extract_hint ?? '',
    companion_extract_conn_id: agent.companion_extract_conn_id ?? '',
    companion_auto_threshold: agent.companion_auto_threshold ?? 0,
  }
}

// ---------------------------------------------------------------------------
// 配置视图（REQ-132：四分类页签；字段/校验/提交逻辑不变）
// ---------------------------------------------------------------------------

function AgentConfigForm({
  agent,
  onChanged,
  visibleTabs,
}: {
  agent: Agent
  onChanged?: () => void
  /** REQ-218①/M49：activity bar 提级视图复用本表单（单实例全字段 forceRender，保存载荷不受限）——
   *  传定时仅渲染指定页签（如 ['model'] / ['connectors'] / ['serve']）；空 = 全部八页签（配置视图）。 */
  visibleTabs?: string[]
}) {
  const { showToast, bumpData, setPage } = useUI()
  const [form] = Form.useForm()
  // REQ-213：内置行（平台助手）适配——白名单 8 字段可编辑（instruction/模型/温度/tools/skills/
  // mcp/max_tokens/max_iteration），身份与角色字段禁用；基座工具不可摘除；提案横幅置顶。
  const isBuiltin = !!agent.is_builtin
  const instructionValue = Form.useWatch('instruction', form) ?? ''
  const [allConns, setAllConns] = useState<ModelConnection[]>([])
  const [tools, setTools] = useState<ToolInfo[]>([])
  const [toolsErr, setToolsErr] = useState(false)
  const [skills, setSkills] = useState<Skill[]>([]) // REQ-164：技能勾选候选（注册表来源，仅启用项）
  const [backends, setBackends] = useState<InferenceBackendStatus[]>([]) // M13：推理后端探测清单
  // REQ-231⑤⑥：运行时工具预览 + hooks 注册真相（后端读取——前端静态文案退役）
  const [previewTools, setPreviewTools] = useState<{ name: string; source: string }[]>([])
  const [previewNotes, setPreviewNotes] = useState<string[]>([])
  const [hooksInfo, setHooksInfo] = useState<{ name: string; active: boolean; description: string }[]>([])
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)
  // REQ-214/M46：连接器授权候选（设置页「连接器」分区为管理面，侧板只做勾选授权）
  const [connectors, setConnectors] = useState<Connector[]>([])

  useEffect(() => {
    form.setFieldsValue({
      ...agent,
      approval_exempt: agent.approval_exempt ?? [], // REQ-231②③：审批面新字段初始化
      approval_timeout_hours: agent.approval_timeout_hours ?? 0,
      // REQ-131：对外服务开关/工具名平铺为表单字段（token 不进表单，走专用端点管理）
      mcp_serve_enabled: agent.mcp_serve?.enabled ?? false,
      mcp_serve_tool_name: agent.mcp_serve?.tool_name ?? '',
    })
    api.listConnections().then(setAllConns).catch(() => {})
    // REQ-231⑤⑥：工具预览与 hooks 清单（agent 变更即取；后端读取——前端静态文案退役）
    api.agentToolPreview(agent.id).then((r) => {
      setPreviewTools(r.tools ?? [])
      setPreviewNotes(r.notes ?? [])
    }).catch(() => {})
    api.listHooks().then((r) => setHooksInfo(r.hooks ?? [])).catch(() => {})
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

  // REQ-164：技能注册表加载（勾选候选；失败静默，能力页签仍可用工具/MCP）
  useEffect(() => {
    api
      .listSkills()
      .then((ls) => setSkills(ls.filter((x) => x.enabled)))
      .catch(() => setSkills([]))
  }, [])

  // REQ-214/M46：连接器候选（失败静默——勾选区空态引导去设置页）
  useEffect(() => {
    api
      .listConnectors()
      .then((r) => setConnectors(r.connectors ?? []))
      .catch(() => setConnectors([]))
  }, [])

  // REQ-214 P2⑥：已授权但当前不可达的连接器（配置时点预警——运行时将降级告警）
  const watchedConnectors = Form.useWatch('connectors', form) as string[] | undefined
  const unreachableAuthorized = useMemo(
    () =>
      connectors.filter(
        (c) => c.status === 'error' && (watchedConnectors ?? agent.connectors ?? []).includes(c.id),
      ),
    [connectors, watchedConnectors, agent.connectors],
  )

  const goConnectorSettings = () => {
    localStorage.setItem('eino.settings.section', 'connectors')
    setPage('settings')
  }

  // 可选 chat 连接（启用中）与生效的全局默认（默认连接须启用，与后端 GetDefaultConnection 语义一致）
  const conns = useMemo(() => allConns.filter((c) => c.conn_type === 'chat' && c.enabled), [allConns])
  const defaultConn = useMemo(
    () => allConns.find((c) => c.conn_type === 'chat' && c.is_default && c.enabled) ?? null,
    [allConns],
  )
  // 当前选中连接（含已停用的历史绑定，便于如实展示身份）
  const modelConnId = Form.useWatch('model_conn_id', form)
  const runtimeBackend = (Form.useWatch('runtime_backend', form) as string | undefined) ?? agent.runtime_backend
  const selectedConn = modelConnId ? allConns.find((c) => c.id === modelConnId) ?? null : null

  const save = async () => {
    try {
      const v = await form.validateFields()
      setSaving(true)
      if (isBuiltin) {
        // REQ-213：内置行走白名单 8 字段（后端合并保护身份/角色字段；tools 基座并集兜底）
        await api.updateAgent(agent.id, {
          instruction: v.instruction ?? '',
          model_conn_id: v.model_conn_id || null,
          temperature: v.temperature ?? null,
          max_tokens: v.max_tokens ?? null,
          max_iteration: v.max_iteration ?? 15,
          tools: v.tools ?? [],
          skills: v.skills ?? [],
          mcp_servers: agent.mcp_servers ?? [], // REQ-214：兼容残留透传（迁移后恒空）
        })
        showToast('平台助手配置已保存，下次对话生效')
        bumpData()
        onChanged?.()
        return
      }
      // REQ-219：载荷统一走 agentFullPayload（表单值+非表单字段透传防清零；含 tool_approval 顺修）
      await api.updateAgent(agent.id, agentFullPayload(agent, v))
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

  /** 小节分隔（页签内二级标题） */
  const sec = (label: string) => (
    <Divider titlePlacement="left" plain style={{ margin: '4px 0 12px' }}>
      {label}
    </Divider>
  )

  return (
    <div className="proj-view-body">
      {isBuiltin && <AssistantProposalBanner onChanged={onChanged} />}
      <Form form={form} layout="vertical" initialValues={agent} requiredMark={false} size="small">
        <Tabs
          defaultActiveKey={visibleTabs?.[0] ?? 'basic'}
          size="small"
          items={[
            {
              key: 'basic',
              label: '基本',
              forceRender: true,
              children: (
                <>
                  <Form.Item name="name" label="名称" rules={isBuiltin ? [] : [{ required: true, message: '名称必填' }]} extra={isBuiltin ? '内置助手名称不可修改' : undefined}>
                    <Input placeholder="智能体名称" disabled={isBuiltin} />
                  </Form.Item>
                  <Form.Item name="description" label="描述（用于多智能体协作时互相理解）" extra={isBuiltin ? '内置助手描述不可修改' : undefined}>
                    <Input.TextArea autoSize={{ minRows: 2, maxRows: 5 }} disabled={isBuiltin} />
                  </Form.Item>
                  <Form.Item name="instruction" label={<Space size={6}>系统提示词（Instruction）<AIOptimizeButton kind="agent_instruction" value={instructionValue} onApply={(v) => form.setFieldValue('instruction', v)} /></Space>}>
                    <Input.TextArea autoSize={{ minRows: 6, maxRows: 14 }} placeholder="定义角色、能力边界、回答风格…" />
                  </Form.Item>
                  {sec('推理后端（谁来推理）——「在哪儿跑」迁 Harness 页签（REQ-219 分层归位）')}
                  <Form.Item
                    name="inference_backend"
                    label="推理后端"
                    extra={isBuiltin ? '内置助手固定 eino-adk 自研后端' : '「在哪儿跑」由运行后端决定，「谁来推理」由此决定：eino-adk 为平台自研（完整能力）；外部 CLI 后端模型由其自身配置决定（Agent 模型连接不生效），技能/MCP 降级为提示注入，不支持多 Agent 编排'}
                  >
                    <Select
                      disabled={isBuiltin}
                      options={inferenceBackendOptions(backends)}
                      showSearch
                      optionFilterProp="label"
                      placeholder="eino-adk（自研默认）"
                    />
                  </Form.Item>
                  <Form.Item
                    name="logo_url"
                    label="自定义后端 Logo URL（REQ-137）"
                    extra={isBuiltin ? '内置助手不可自定义' : '推理后端为自定义/外部部署（非内置）时，会话列表与对话界面将展示此图标；未配置回退默认图标'}
                  >
                    <Input placeholder="https://…/logo.png" allowClear disabled={isBuiltin} />
                  </Form.Item>
                </>
              ),
            },
            {
              key: 'model',
              label: '模型与参数',
              forceRender: true,
              children: (
                <>
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
                    <Select allowClear showSearch optionFilterProp="label" placeholder="跟随全局默认" options={conns.map((c) => ({ value: c.id, label: connLabel(c) }))} />
                  </Form.Item>
                  {sec('采样参数')}
                  <Form.Item name="temperature" label="温度（0~2，留空默认）">
                    <InputNumber min={0} max={2} step={0.1} style={{ width: '100%' }} placeholder="默认" />
                  </Form.Item>
                  <Form.Item name="max_tokens" label="最大回复 tokens">
                    <InputNumber min={1} style={{ width: '100%' }} placeholder="默认" />
                  </Form.Item>
                  {/* REQ-219：context_mode 迁 Context 层页签（配置聚合呈现，层职责仍归 Context） */}
                </>
              ),
            },
            // REQ-219/M50 五层视角落侧板：Context/Harness/Loop/Graph 与「模型与参数」（Model 层）
            // 并列成栏（开发者定案：context 不并入 Harness）——配置聚合呈现≠层职责混淆
            {
              key: 'context',
              label: 'Context',
              forceRender: true,
              children: (
                <>
                  <Typography.Paragraph type="secondary" style={{ fontSize: 11, marginTop: 0 }}>
                    Context 上下文层（REQ-201/M37 · REQ-219 分层呈现）——历史预算、压缩与工具结果剪枝：决定模型每轮「真正看到什么」，压缩/裁剪均以运行警告诚实标注。
                  </Typography.Paragraph>
                  <Form.Item
                    name="context_mode"
                    label="上下文预算"
                    tooltip={isBuiltin ? '内置助手固定标准档' : 'REQ-201：历史 token 预算档位——超限时先压缩（LLM 摘要持久化）再裁剪，压缩/裁剪均以运行警告诚实标注。「完整」不限量（存量行为）；「紧凑」约 6k tokens 适合长对话省成本；默认标准约 24k。'}
                  >
                    <Select
                      allowClear
                      disabled={isBuiltin}
                      placeholder="标准（约 24k tokens）"
                      options={[
                        { value: 'compact', label: '紧凑（约 6k tokens）' },
                        { value: 'standard', label: '标准（约 24k tokens）' },
                        { value: 'full', label: '完整（不限量，存量行为）' },
                      ]}
                    />
                  </Form.Item>
                  {sec('进程内固定（观察项，暂不可配）')}
                  <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginBottom: 4 }}>
                    · 压缩策略：超预算先八段式 LLM 摘要（复用本智能体模型连接）持久化，再尾半预算裁剪；
                    <br />· 召回链：知识库与伴生图召回按序注入（embedding 未配置自动词法兜底）；
                    <br />· 工具结果剪枝：超 4k 字符保留首尾并标注截断。
                  </Typography.Text>
                </>
              ),
            },
            {
              key: 'harness',
              label: 'Harness',
              forceRender: true,
              children: (
                <>
                  <Typography.Paragraph type="secondary" style={{ fontSize: 11, marginTop: 0 }}>
                    Harness 执行面（REQ-202/M38 · REQ-219 分层归位）——工具装配、防护 hooks、验证背压、审批 gating 与沙箱运行后端：模型之外「怎么把事做安全、做扎实」的一层。
                  </Typography.Paragraph>
                  {sec('运行后端（在哪儿跑）')}
                  <Form.Item name="runtime_backend" label="运行后端" initialValue="inprocess" extra={isBuiltin ? '内置助手固定进程内执行' : 'M10：inprocess=平台进程内装配；docker=per-Agent agentd 容器沙箱；k8s=Pod 沙箱；auto=自动检测（REQ-190：k8s pod 优先→docker 次之→均不可用进程内兜底；平台需配置 SANDBOX_IMAGE）'}>
                    <Select
                      disabled={isBuiltin}
                      options={[
                        { value: 'inprocess', label: 'inprocess（进程内）' },
                        { value: 'docker', label: 'docker（沙箱容器）' },
                        { value: 'k8s', label: 'k8s（Pod 沙箱）' },
                        { value: 'auto', label: 'auto（自动检测：k8s 优先）' },
                      ]}
                    />
                  </Form.Item>
                  {(runtimeBackend === 'docker' || runtimeBackend === 'k8s' || runtimeBackend === 'auto') && (
                    <>
                      <Form.Item name="sandbox_memory" label="沙箱内存上限" extra="M10/10b：留空 = 默认 512m">
                        <Select
                          allowClear
                          placeholder="512m（默认）"
                          options={[{ value: '256m', label: '256m' }, { value: '512m', label: '512m' }, { value: '1g', label: '1g' }, { value: '2g', label: '2g' }]}
                        />
                      </Form.Item>
                      <Form.Item name="sandbox_cpus" label="沙箱 CPU 核数" extra="留空 = 默认 1 CPU">
                        <InputNumber min={0.5} max={8} step={0.5} style={{ width: '100%' }} placeholder="1（默认）" />
                      </Form.Item>
                      <SandboxPanel agent={agent} form={form} />
                    </>
                  )}
                  {sec('文件原语安全根与验证背压（REQ-202，自「能力」迁入）')}
                  <Form.Item
                    name="work_dir"
                    label="工作目录"
                    tooltip={isBuiltin ? '内置助手不开放文件原语安全根配置' : '文件原语工具（grep/glob/read_file/write_file）的安全根：绝对路径，越界由 SafeJoin 强制拒绝；空=仅项目会话具备文件能力。'}
                  >
                    <Input allowClear disabled={isBuiltin} placeholder="如 /home/user/project（绝对路径，空=不装配文件原语）" />
                  </Form.Item>
                  <Form.Item
                    name="verify_command"
                    label="验证命令（verify_on_stop）"
                    tooltip={isBuiltin ? '内置助手不开放 verify_on_stop 配置' : '运行标记完成前在安全根执行（sh -c，10s 超时）：退出码非 0 即背压——本次运行标记为 verify_failed 并在过程时间线透出输出。示例：go build ./...。'}
                  >
                    <Input allowClear disabled={isBuiltin} placeholder="如 go build ./...（空=不验证）" />
                  </Form.Item>
                  {sec('工具调用人工审批（REQ-231/M58 三档精细化）')}
                  <Form.Item
                    name="tool_approval"
                    label="审批策略"
                    initialValue=""
                    extra={isBuiltin ? '内置助手不开放审批策略配置' : 'danger=仅危险工具（写类内置 write_file/save_file/todo_write、http_fetch、连接器/MCP 写操作；read-only 免审）；all=全部工具。开启审批后对外 MCP 服务（server 模式）调用默认拒绝。对话级开关可覆盖本策略（生效策略随 run 事件透出）'}
                  >
                    <Select
                      disabled={isBuiltin}
                      options={[
                        { value: '', label: '关闭（直接执行）' },
                        { value: 'danger', label: '危险工具审批（写类/出网/连接器写）' },
                        { value: 'all', label: '全部工具调用前审批' },
                      ]}
                    />
                  </Form.Item>
                  <Form.Item
                    name="approval_exempt"
                    label="审批豁免清单（可选，REQ-231②）"
                    extra="danger/all 档下勾选的工具直接放行不挂起（个工具覆盖档位；恢复重入的旧挂起不受影响）"
                  >
                    <Select
                      mode="multiple"
                      allowClear
                      disabled={isBuiltin}
                      placeholder="从运行时工具清单选择豁免项"
                      options={previewTools.map((t) => ({ value: t.name, label: `${t.name}（${t.source}）` }))}
                    />
                  </Form.Item>
                  <Form.Item
                    name="approval_timeout_hours"
                    label="审批挂起超时（小时，可选，REQ-231③）"
                    extra="0=不限（默认）；恢复运行时挂起超过该时长将自动拒绝并告知模型超时语义（治长期挂起遗忘）"
                  >
                    <InputNumber min={0} max={168} step={1} style={{ width: '100%' }} disabled={isBuiltin} placeholder="0（不限）" />
                  </Form.Item>
                  <ToolPreviewCard agentId={agent.id} approval={Form.useWatch('tool_approval', form) ?? ''} notes={previewNotes} />
                  {sec('防护 hooks（进程内确定性层，只读——清单来自后端 /api/hooks）')}
                  <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginBottom: 4 }}>
                    {hooksInfo.length > 0
                      ? hooksInfo.map((h) => `· ${h.name}（${h.active ? '启用' : '预置未启用'}）：${h.description}`).join(' ｜ ')
                      : 'hook 清单加载中…（/api/hooks）'}
                    <br />· hook 纪律：只放行/拒绝、不改写参数与结果——确定性检查不进模型上下文决策。
                  </Typography.Text>
                </>
              ),
            },
            {
              key: 'loop',
              label: 'Loop',
              forceRender: true,
              children: (
                <>
                  <Typography.Paragraph type="secondary" style={{ fontSize: 11, marginTop: 0 }}>
                    Loop 循环层（REQ-204/M39 · REQ-219 分层呈现）——ReAct 迭代上限、挂起恢复与长任务推进：防死循环与跨会话续跑在这一层。
                  </Typography.Paragraph>
                  <Form.Item name="max_iteration" label="最大迭代次数（ReAct 上限）" initialValue={25}>
                    <InputNumber min={1} max={100} style={{ width: '100%' }} />
                  </Form.Item>
                  {sec('挂起恢复与长任务（会话级，对话窗口配置）')}
                  <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginBottom: 4 }}>
                    · 挂起恢复：审批/追问挂起经 checkpoint 持久化，后端重启后仍可恢复（REQ-204 C1）；
                    <br />· 对话级定时续跑：对话内配置、cap 硬上限（进程内形态，重启失效）；
                    <br />· 进度产物：todo_write 落库 + todo.md 导出；usage tokens 随运行事件落库。
                  </Typography.Text>
                </>
              ),
            },
            {
              key: 'graph',
              label: 'Graph',
              forceRender: true,
              children: (
                <>
                  <Typography.Paragraph type="secondary" style={{ fontSize: 11, marginTop: 0 }}>
                    Graph 协作层（REQ-219 分层呈现）——多智能体编排与工作流，五层视角的第五层。当前智能体级暂无配置项（诚实占位）。
                  </Typography.Paragraph>
                  <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginBottom: 4 }}>
                    · 成员协作：多智能体团队（成员/协调者/collab_mode/workflow_mode）在「项目」侧板配置；
                    <br />· 子智能体委派：成员经装配期 agent_as_tool / transfer_to_agent 并入（项目会话生效）；
                    <br />· 工作流编排（REQ-205/M40）：触发驱动待领取——出现真实编排诉求后在此层落地。
                  </Typography.Text>
                </>
              ),
            },
            {
              key: 'ability',
              label: '能力',
              forceRender: true,
              children: (
                <>
                  <Form.Item
                    name="skills"
                    label="技能"
                    extra="技能 = 指令 + 工具集 + 资源的打包能力单元（M9，REQ-120/121）；勾选后按对话级开关挂载注入，外部 CLI 后端降级为提示注入。"
                  >
                    <Select
                      mode="multiple"
                      allowClear
                      showSearch
                      optionFilterProp="label"
                      placeholder="不挂载技能"
                      options={skills.map((k) => ({ value: k.id, label: k.name, title: k.description }))}
                      notFoundContent="技能注册表为空（到「技能」模块创建）"
                      optionRender={(opt) => (
                        <div className="tool-option">
                          <div className="tool-option-name">
                            <span>{opt.data?.label}</span>
                          </div>
                          {opt.data?.title ? <div className="tool-option-desc">{opt.data.title}</div> : null}
                        </div>
                      )}
                    />
                  </Form.Item>

                  <Form.Item
                    name="tools"
                    label="工具白名单"
                    extra={toolsErr ? '工具注册表暂不可用，可稍后重试。' : isBuiltin ? '内置基座工具（L0/L1）不可摘除（禁用项）；可另行勾选通用工具，保存后生效。' : '来自工具注册表（内置 / 本体 / MCP 动态工具），勾选后随运行装配；「propose_assistant_config」为平台助手专属，不可勾选。'}
                  >
                    <Select
                      mode="multiple"
                      allowClear
                      virtual={false}
                      placeholder={toolsErr ? '工具注册表暂不可用' : '选择可用工具'}
                      options={tools.map((t) => ({
                        value: t.id,
                        label: t.name,
                        title: t.description,
                        source: t.source,
                        // REQ-213：内置行基座八工具禁选（不可摘除）；普通行提案工具禁选（内置专属）
                        disabled: isBuiltin ? ASSISTANT_BASE_TOOLS.includes(t.id) : t.id === 'propose_assistant_config',
                      }))}
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

                  {/* REQ-219：work_dir/verify_command/tool_approval 迁 Harness 层页签 */}

                  {/* REQ-218①/M49：外部连接器授权区块提级 activity bar「连接器」独立视图（connectors 页签） */}
                </>
              ),
            },
            {
              key: 'connectors',
              label: '连接器',
              forceRender: true,
              children: (
                <>
                  {sec('外部连接器（REQ-214）')}
                  <div style={{ marginBottom: 8 }}>
                    <ApiOutlined style={{ marginRight: 6 }} />
                    <span className="model-meta">
                      连接外部能力的统一入口（自定义 MCP / Kubernetes / SSH）。工具以 <code>{'{连接器名}__{tool}'}</code> 前缀并入白名单候选，凭据服务端绑定不进模型上下文；勾选 = 授权本智能体使用（连接白名单），连接失败降级不阻断运行。
                    </span>
                  </div>
                  {unreachableAuthorized.length > 0 && (
                    <Alert
                      style={{ marginBottom: 8 }}
                      type="warning"
                      showIcon
                      title={`已授权连接器当前不可达：${unreachableAuthorized.map((c) => c.name).join('、')}——运行时将降级告警（不加载其工具），可到设置页重新测试`}
                    />
                  )}
                  <Form.Item name="connectors" style={{ marginBottom: 8 }}>
                    <Checkbox.Group style={{ display: 'flex', flexDirection: 'column', gap: 4 }} disabled={isBuiltin}>
                      {connectors.map((c) => {
                        const meta = kindMeta(c.kind)
                        // REQ-214 P2②：工具集提示——授权前知道模型将得到什么工具
                        const tip =
                          (c.tools?.length ?? 0) > 0
                            ? `工具集：${c.tools.join('、')}${c.tested_at ? `（${new Date(c.tested_at).toLocaleString()} 测试）` : ''}`
                            : '尚未测试——设置页「测试」后显示工具集'
                        return (
                          <Checkbox key={c.id} value={c.id}>
                            <Tooltip title={tip}>
                              <span style={{ fontSize: 12 }}>
                                <Tag color={meta.color} style={{ marginInlineEnd: 4 }}>{meta.label}</Tag>
                                {c.name}
                                {c.is_builtin && <Tag style={{ marginInlineEnd: 0 }}>内置</Tag>}
                                {c.status === 'error' && <Tag color="red" style={{ marginInlineEnd: 0 }}>不可达</Tag>}
                                {c.status === 'ok' && <Tag color="green" style={{ marginInlineEnd: 0 }}>可达</Tag>}
                              </span>
                            </Tooltip>
                          </Checkbox>
                        )
                      })}
                    </Checkbox.Group>
                  </Form.Item>
                  {connectors.length === 0 && (
                    <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginBottom: 6 }}>
                      暂无连接器——在设置页「连接器」分区创建（内置 open-ontologies 已就绪）。
                    </Typography.Text>
                  )}
                  <Button size="small" icon={<SettingOutlined />} onClick={goConnectorSettings}>
                    管理连接器
                  </Button>
                </>
              ),
            },
            // REQ-193：伴生本体页签摘除——上提为侧板一级视图（activity bar「伴生本体」按钮），
            // 开关+REQ-187 三字段迁「伴生配置」页、候选管理迁「伴生管理」页（铺平）
            // REQ-213：内置行不开放对外服务页（mcp_serve 锁死，无从编辑）
            ...(isBuiltin ? [] : [{
              key: 'serve',
              label: '对外服务',
              forceRender: true,
              children: <McpServeTab agent={agent} />,
            }]),
          ].filter((t: { key: string }) => !visibleTabs || visibleTabs.includes(t.key))}
        />
      </Form>

      {/* REQ-226/M54：配置版本与一键回滚（保存即版本；回滚动作自身先快照可再滚回） */}
      {!isBuiltin && <ConfigVersionCard agentId={agent.id} onRolled={onChanged} />}

      <div className="proj-view-actions">
        <Button type="primary" size="small" loading={saving} onClick={save}>
          保存
        </Button>
        {!isBuiltin && (
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
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// 对外服务（REQ-131/M18）：开关/工具名随本表单保存提交；Token 经专用端点管理
// ---------------------------------------------------------------------------

/**
 * M10/10b 沙箱面板：容器状态可见 + 启动/停止（docker 运行后端的 Agent）。
 * 状态经 /api/agents/{id}/sandbox 轮询（10s），启停后即时刷新；未启用 SANDBOX_IMAGE 时降级提示。
 */
function SandboxPanel({ agent, form }: { agent: Agent; form: FormInstance }) {
  const agentId = agent.id
  const { showToast } = useUI()
  const [st, setSt] = useState<SandboxStatus | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    api
      .sandboxStatus(agentId)
      .then((r) => setSt(r))
      .catch(() => setSt(null))
  }, [agentId])
  useEffect(() => {
    load()
    const t = setInterval(load, 10000)
    return () => clearInterval(t)
  }, [load])

  const act = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true)
    try {
      await fn()
      showToast(ok)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
      load()
    }
  }

  if (!st?.enabled) {
    return (
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        title="沙箱后端未启用"
        description="平台未配置 SANDBOX_IMAGE——配置后此处可管理该智能体的 agentd 容器。"
      />
    )
  }
  const running = st.state === 'running'
  return (
    <Card size="small" style={{ marginBottom: 12 }}>
      <Space size={8} wrap style={{ marginBottom: 6 }}>
        <Badge status={running ? 'success' : 'default'} text={running ? '容器运行中' : st.state === 'error' ? `异常：${st.detail ?? ''}` : '容器未运行'} />
        {st.memory || st.cpus ? (
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>
            限制 {st.memory || '512m'} / {st.cpus || 1} CPU
          </Typography.Text>
        ) : (
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>限制 512m / 1 CPU（默认）</Typography.Text>
        )}
      </Space>
      <div>
        {running ? (
          <Button size="small" loading={busy} onClick={() => act(() => api.sandboxStop(agentId), '沙箱容器已停止并移除')}>
            停止沙箱
          </Button>
        ) : (
          <Button
            size="small"
            type="primary"
            loading={busy}
            onClick={async () => {
              // REQ-219 顺修：启动前落库资源限制须走全量载荷——后端 PUT 为 full-replace，
              // 此前仅传沙箱两字段会把其余配置清零（agentFullPayload 与保存共用）
              await act(async () => {
                await api.updateAgent(agentId, agentFullPayload(agent, form.getFieldsValue()))
                await api.sandboxStart(agentId)
              }, '沙箱容器已启动（per-Agent agentd）')
            }}
          >
            启动沙箱
          </Button>
        )}
        <Typography.Text type="secondary" style={{ fontSize: 11, marginLeft: 8 }}>
          每 Agent 一个 agentd 容器（agt-{agentId.slice(0, 8)}…）；对话时自动拉起，此处可手动管理
        </Typography.Text>
      </div>
    </Card>
  )
}

function McpServeTab({ agent }: { agent: Agent }) {
  const { showToast, bumpData } = useUI()
  const form = Form.useFormInstance()
  const enabled = Form.useWatch('mcp_serve_enabled', form) ?? false
  const toolName = Form.useWatch('mcp_serve_tool_name', form) ?? ''
  const [info, setInfo] = useState<McpServeInfo | null>(null)
  const [resetting, setResetting] = useState(false)

  const loadInfo = () => {
    api.getAgentMcpServe(agent.id).then(setInfo).catch(() => setInfo(null))
  }
  useEffect(loadInfo, [agent.id])

  const resetToken = async () => {
    setResetting(true)
    try {
      await api.resetAgentMcpToken(agent.id)
      showToast('Token 已重置（旧 Token 立即失效）')
      loadInfo()
      bumpData()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setResetting(false)
    }
  }

  const effToolName = (toolName || '').trim() || `agent_${agent.id}`
  const baseUrl = typeof window !== 'undefined' ? window.location.origin : ''
  const curl = `curl -X POST "${baseUrl}/mcp" \\\n  -H "Authorization: Bearer $TOKEN" \\\n  -H "Content-Type: application/json" \\\n  -d '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"${effToolName}","arguments":{"input":"你好"}},"id":1}'`

  const hasToken = !!(info?.configured || agent.mcp_serve?.token)

  return (
    <>
      <Form.Item name="mcp_serve_enabled" label="开启对外服务" valuePropName="checked" extra="开启后，本智能体作为 MCP 工具经平台 /mcp 端点（Streamable HTTP）暴露给外部 MCP 客户端；服务默认仅回环监听，跨机访问需经反代按需暴露">
        <Switch />
      </Form.Item>
      <Form.Item
        name="mcp_serve_tool_name"
        label="工具名（可选覆盖）"
        extra={<>缺省为 <Typography.Text code>agent_{agent.id}</Typography.Text>；须全局唯一，冲突时后注册者跳过</>}
      >
        <Input placeholder={`agent_${agent.id}`} allowClear disabled={!enabled} />
      </Form.Item>

      {enabled && (
        <Collapse
          ghost
          size="small"
          items={[
            {
              key: 'access',
              label: <span className="event-link">接入信息（端点 / Token / 调用示例）</span>,
              children: (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                  <div className="proj-kv">
                    <span className="proj-k">端点</span>
                    <span className="proj-mono">{baseUrl}/mcp</span>
                    <Button
                      size="small"
                      type="text"
                      icon={<CopyOutlined />}
                      aria-label="复制端点"
                      onClick={() => {
                        navigator.clipboard?.writeText(`${baseUrl}/mcp`).then(
                          () => showToast('端点已复制'),
                          () => showToast('复制失败', 'err'),
                        )
                      }}
                    />
                  </div>
                  <div className="proj-kv">
                    <span className="proj-k">工具名</span>
                    <span className="proj-mono">{effToolName}</span>
                  </div>
                  <div className="proj-kv">
                    <span className="proj-k">Token</span>
                    <span className="proj-mono">{hasToken ? info?.token_mask || '已配置' : '未生成'}</span>
                    <Tooltip title={hasToken ? '重置后旧 Token 立即失效' : '生成 Agent 级 Bearer Token'}>
                      <Button size="small" icon={hasToken ? <ReloadOutlined /> : <ExportOutlined />} loading={resetting} onClick={resetToken} aria-label={hasToken ? '重置 Token' : '生成 Token'} />
                    </Tooltip>
                  </div>
                  <Typography.Paragraph type="secondary" style={{ fontSize: 11, marginBottom: 4 }}>
                    调用示例（$TOKEN 为生成/重置后的 Token 值，仅服务端保存，请妥善保管）：
                  </Typography.Paragraph>
                  <pre className="raw-json" style={{ maxHeight: 170, overflow: 'auto' }}>{curl}</pre>
                  <Typography.Paragraph type="secondary" style={{ fontSize: 11, marginBottom: 0 }}>
                    安全边界：开启「工具审批」的智能体在 server 模式下调用将被默认拒绝；本平台 /mcp 端点不可配置进其他智能体的 MCP servers（自引用拦截，装配期告警）。
                  </Typography.Paragraph>
                </div>
              ),
            },
          ]}
        />
      )}
      {!enabled && (
        <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
          与 §6.11（Agent 作为 MCP client 调用外部服务）构成双向闭环——本页是 server 侧。开启并保存后可见接入信息。
        </Typography.Paragraph>
      )}
    </>
  )
}

// ---------------------------------------------------------------------------
// 智能体文件视图（REQ-218④/M49）：以 agent.work_dir 为安全根浏览——面包屑 + 单层列表 +
// 1MB 文本预览（沿项目 FilesView 交互模型）；「设为工作目录」把当前浏览目录持久化为
// agent.work_dir（全量载荷防清零），装配期 resolveWorkRoot 随新值生效 = 新 Run 文件工具安全根切换。
// 诚实边界：沙箱容器执行世界与 work_dir 零 volume 绑定（REQ-202 口径，文件原语平台进程内执行）。
// ---------------------------------------------------------------------------

function AgentFilesView({ agent, onChanged }: { agent: Agent; onChanged?: () => void }) {
  const { showToast } = useUI()
  const [path, setPath] = useState('') // 相对 work_dir 的目录（'' = 根）
  const [listing, setListing] = useState<{ path: string; root: string; entries: { name: string; is_dir: boolean; size: number; mod_time: string }[] } | null>(null)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [preview, setPreview] = useState<{ path: string; content: string } | null>(null)
  const [previewErr, setPreviewErr] = useState<string | null>(null)
  const [manualDir, setManualDir] = useState('') // 空态手工输入
  const [setting, setSetting] = useState(false)

  const load = useCallback((p = path) => {
    setLoading(true)
    setErr(null)
    api
      .listAgentDirFiles(agent.id, p)
      .then((r) => setListing(r))
      .catch((e) => {
        setListing(null)
        setErr(e.message)
      })
      .finally(() => setLoading(false))
  }, [agent.id, path])
  useEffect(() => {
    setPreview(null)
    setPreviewErr(null)
    load(path)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agent.id, path])

  // 「设为工作目录」：当前浏览目录（绝对路径；空=work_dir 本身则无意义，按钮置灰）持久化（全量载荷防清零）
  const root: string = listing?.root || agent.work_dir || ''
  const absOf = (rel: string) => (rel ? `${root.replace(/\/+$/, '')}/${rel}` : root)
  const setWorkDir = async (dir: string) => {
    setSetting(true)
    try {
      await api.updateAgent(agent.id, { ...agentFullPayload(agent, { ...agent }), work_dir: dir })
      showToast(`工作目录已设为：${dir || agent.work_dir}（新 Run 的文件工具安全根即此目录）`)
      onChanged?.()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setSetting(false)
    }
  }

  const openFile = async (name: string) => {
    const rel = path ? `${path}/${name}` : name
    setPreviewErr(null)
    try {
      const content = await api.getAgentDirFile(agent.id, rel)
      setPreview({ path: rel, content })
    } catch (e: any) {
      setPreview(null)
      setPreviewErr(`${rel}: ${e.message}`)
    }
  }

  if (!agent.work_dir) {
    // 空 work_dir 引导态：手工输入路径设为工作目录
    return (
      <div className="proj-view-body">
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="未设置工作目录——设置后文件工具（grep/glob/read/write）以此为安全根"
        />
        <Space.Compact style={{ width: '100%', marginTop: 8 }}>
          <Input placeholder="输入绝对路径，如 /home/me/project" value={manualDir} onChange={(e) => setManualDir(e.target.value)} />
          <Button
            type="primary"
            loading={setting}
            onClick={async () => {
              const d = manualDir.trim()
              if (!d) return
              await setWorkDir(d)
              setPath('')
            }}
          >
            设为工作目录
          </Button>
        </Space.Compact>
        <Typography.Text type="secondary" style={{ fontSize: 11, display: 'block', marginTop: 8 }}>
          也可在 Harness 页签填写 work_dir；后端以 fsutil.SafeJoin 限制浏览与文件工具不越出该目录。
        </Typography.Text>
      </div>
    )
  }

  const crumbs = path ? path.split('/') : []
  return (
    <div className="proj-view-body">
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap', marginBottom: 8 }}>
        <FolderOutlined />
        <Breadcrumb
          items={[
            { title: <a onClick={() => setPath('')}>{agent.work_dir}</a> },
            ...crumbs.map((c, i) => ({
              title: <a onClick={() => setPath(crumbs.slice(0, i + 1).join('/'))}>{c}</a>,
            })),
          ]}
        />
        <span style={{ flex: 1 }} />
        <Tooltip title="把当前浏览目录持久化为工作目录（新 Run 文件工具安全根切换）">
          <Button size="small" disabled={!path} loading={setting} onClick={() => setWorkDir(absOf(path))}>
            设为工作目录
          </Button>
        </Tooltip>
      </div>
      {err && <Alert type="error" showIcon title={err} style={{ marginBottom: 8 }} />}
      {loading ? (
        <Spin size="small" />
      ) : listing ? (
        listing.entries.length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="目录为空" />
        ) : (
          <ul className="agent-files" style={{ listStyle: 'none', margin: 0, padding: 0 }}>
            {listing.entries.map((e) => (
              <li
                key={e.name}
                style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '4px 6px', borderRadius: 6, cursor: 'pointer' }}
                onClick={() => (e.is_dir ? setPath(path ? `${path}/${e.name}` : e.name) : openFile(e.name))}
              >
                {e.is_dir ? <FolderOutlined style={{ color: 'var(--c-ink-2)' }} /> : <FileOutlined style={{ color: 'var(--c-ink-2)' }} />}
                <span style={{ fontSize: 12, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{e.name}</span>
                {!e.is_dir && (
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    {e.size > 1024 * 1024 ? `${(e.size / 1024 / 1024).toFixed(1)}MB` : `${(e.size / 1024).toFixed(1)}KB`}
                  </Typography.Text>
                )}
              </li>
            ))}
          </ul>
        )
      ) : null}
      {previewErr && <Alert type="warning" showIcon title={previewErr} style={{ marginTop: 8 }} />}
      {preview && (
        <Card size="small" title={preview.path} style={{ marginTop: 10 }}
          extra={<Button size="small" type="text" onClick={() => setPreview(null)}>关闭</Button>}>
          <pre style={{ margin: 0, maxHeight: 320, overflow: 'auto', fontSize: 11, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{preview.content}</pre>
        </Card>
      )}
    </div>
  )
}


// ---------------------------------------------------------------------------
// REQ-231⑤（51 号 W3）：运行时工具预览卡——「这个智能体运行时实际会拿到哪些工具」
// 的确定性呈现（后端四源合并结果+遮蔽告警；治 P-H4 仅 debug 快照可见）。危险工具
// 高亮 + 审批策略摘要（与 /api/hooks 卡同面板）。
// ---------------------------------------------------------------------------
function ToolPreviewCard({ agentId, approval, notes: notesProp }: { agentId: string; approval: string; notes?: string[] }) {
  const [tools, setTools] = useState<{ name: string; source: string }[]>([])
  const [danger, setDanger] = useState<string[]>([])
  const [notes, setNotes] = useState<string[]>(notesProp ?? [])
  const [err, setErr] = useState<string | null>(null)
  useEffect(() => {
    api
      .agentToolPreview(agentId)
      .then((r) => {
        setTools(r.tools ?? [])
        setNotes(r.notes ?? [])
        // danger 清单由后端审批策略口径推导（同 IsDangerousTool）——前端按 source 近似标注：
        // builtin 写类 + 非 ontology/oo 连接器前缀
        const builtinDanger = ['write_file', 'save_file', 'todo_write', 'http_fetch']
        setDanger((r.tools ?? []).filter((t) => builtinDanger.includes(t.name) || (t.name.includes('__') && !t.name.startsWith('ontology__') && !t.name.startsWith('oo__'))).map((t) => t.name))
      })
      .catch((e: any) => setErr(e?.message ?? '预览加载失败'))
  }, [agentId])
  if (err) return <Typography.Text type="secondary" style={{ fontSize: 11 }}>工具预览不可用：{err}</Typography.Text>
  return (
    <div style={{ border: '1px solid var(--ant-color-border, #ddd)', borderRadius: 6, padding: '8px 10px', marginBottom: 10 }}>
      <Typography.Text strong style={{ fontSize: 12 }}>运行时工具预览（{tools.length}）</Typography.Text>
      <Typography.Text type="secondary" style={{ fontSize: 11, display: 'block', margin: '2px 0 6px' }}>
        审批策略生效：{approval === '' ? '关闭' : approval === 'danger' ? '危险工具档' : approval === 'all' ? '全部工具' : approval || '跟随 agent'}{danger.length > 0 && approval !== '' ? ` · 危险清单 ${danger.length} 项将挂起审批` : ''}
      </Typography.Text>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
        {tools.map((t) => (
          <Tooltip key={t.name} title={`来源：${t.source}`}>
            <Tag color={danger.includes(t.name) ? 'orange' : 'default'} style={{ margin: 0, fontSize: 11 }}>{t.name}</Tag>
          </Tooltip>
        ))}
      </div>
      {notes.length > 0 && (
        <Typography.Text type="secondary" style={{ fontSize: 10.5, display: 'block', marginTop: 6 }}>
          {notes.join('；')}
        </Typography.Text>
      )}
    </div>
  )
}


// ---------------------------------------------------------------------------
// REQ-226/M54：配置版本与一键回滚——「保存即版本」（PUT 成功后端自动快照「更新前」
// 整包配置）；版本列表新→旧 + 选中版本与当前的 diff + 一键回滚（回滚动作自身先快照
// 「回滚前」状态，可再滚回）；保留最近 50 版（后端惰性裁剪）。治 P-2「改坏配置无法还原」。
// ---------------------------------------------------------------------------
function ConfigVersionCard({ agentId, onRolled }: { agentId: string; onRolled?: () => void }) {
  const { showToast, bumpData } = useUI()
  const [versions, setVersions] = useState<AgentConfigVersion[]>([])
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState<number | null>(null)
  const [diffs, setDiffs] = useState<{ field: string; old: string; new: string }[] | null>(null)
  const [rolling, setRolling] = useState(false)

  const load = useCallback(() => {
    setLoading(true)
    api
      .listConfigVersions(agentId)
      .then((ls) => {
        setVersions(ls ?? [])
        setSelected(null)
        setDiffs(null)
      })
      .catch(() => setVersions([]))
      .finally(() => setLoading(false))
  }, [agentId])

  useEffect(() => {
    load()
  }, [load])

  const showDiff = async (version: number) => {
    setSelected(version)
    setDiffs(null)
    try {
      const res = await fetch(`/api/agents/${agentId}/config-versions/${version}/diff`)
      const body = await res.json()
      setDiffs(body.diffs ?? [])
    } catch {
      setDiffs([])
    }
  }

  const rollback = async (version: number) => {
    setRolling(true)
    try {
      await api.rollbackConfig(agentId, version)
      showToast(`已回滚到版本 v${version}（回滚前状态已自动快照，可再滚回）`)
      bumpData()
      onRolled?.()
      load()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setRolling(false)
    }
  }

  return (
    <Card
      size="small"
      style={{ marginTop: 10 }}
      title={
        <span style={{ fontSize: 12 }}>
          配置版本（保存即版本，REQ-226）
          <Typography.Text type="secondary" style={{ fontSize: 11, marginLeft: 8 }}>保留最近 50 版</Typography.Text>
        </span>
      }
      extra={
        <Button size="small" icon={<ReloadOutlined />} onClick={load} aria-label="刷新配置版本" />
      }
    >
      {loading ? (
        <Spin size="small" />
      ) : versions.length === 0 ? (
        <Typography.Text type="secondary" style={{ fontSize: 11 }}>暂无历史版本——下次保存配置后自动生成（保存前状态入版本）</Typography.Text>
      ) : (
        <>
          <Select
            size="small"
            style={{ width: '100%' }}
            placeholder="选择历史版本查看与当前配置的差异"
            value={selected}
            onChange={(v) => showDiff(v as number)}
            options={versions.map((v) => ({
              value: v.version,
              label: `v${v.version} · ${v.created_at?.slice(5, 16).replace('T', ' ')} · ${v.note}`,
            }))}
          />
          {selected !== null && (
            <div style={{ marginTop: 8 }}>
              {diffs === null ? (
                <Spin size="small" />
              ) : diffs.length === 0 ? (
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>该版本与当前配置无差异</Typography.Text>
              ) : (
                <div style={{ maxHeight: 160, overflowY: 'auto', fontSize: 11 }}>
                  {diffs.map((d) => (
                    <div key={d.field} style={{ padding: '2px 0', borderBottom: '1px dashed var(--ant-color-border, #eee)' }}>
                      <Tag style={{ margin: 0 }}>{d.field}</Tag>
                      <Typography.Text type="secondary" delete style={{ fontSize: 11 }}>{(d.old || '∅').slice(0, 60)}</Typography.Text>
                      {' → '}
                      <Typography.Text style={{ fontSize: 11 }}>{(d.new || '∅').slice(0, 60)}</Typography.Text>
                    </div>
                  ))}
                </div>
              )}
              <Popconfirm
                title={`回滚到 v${selected}？`}
                description="整包写回该版本配置；回滚前状态会自动快照（可再滚回）。"
                okText="回滚"
                okButtonProps={{ danger: true, loading: rolling }}
                cancelText="取消"
                onConfirm={() => rollback(selected)}
              >
                <Button size="small" danger style={{ marginTop: 8 }} disabled={diffs !== null && diffs.length === 0}>
                  一键回滚到 v{selected}
                </Button>
              </Popconfirm>
            </div>
          )}
        </>
      )}
    </Card>
  )
}
