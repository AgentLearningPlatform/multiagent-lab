import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Checkbox, DatePicker, Form, Input, Menu, Modal, Popconfirm, Result, Segmented, Select, Space, Splitter, Spin, Switch, Table, Tag, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import type { Dayjs } from 'dayjs'
import { api } from '../api/client'
import type { ProviderGroupMeta } from '../api/client'
import { PROVIDER_PRESETS } from '../api/providerPresets'
import type { InferenceBackendStatus, ModelConnection, UsageGroupBy, UsageRow } from '../api/types'
import { useUI } from '../store/ui'

type Category = 'models' | 'stats' | 'inference' | 'global' | 'security'
/** 统计维度：供应商为前端归并（后端无提供商实体），其余直接映射后端 group_by */
type StatsDimension = 'model' | 'supplier' | 'agent' | 'project'

/**
 * 提供商分组（REQ-148 起分组标识与 BaseURL 解耦）：
 * 后端 model_connection 带 provider_group_id（provider_group 表，含展示别名），同一供应商可多实例
 * （同 BaseURL 不同账号/Key）。老数据由后端按 (protocol, base_url) 幂等回填，存量行为不变。
 * - 连接命名约定：`{提供商名}·{模型名}`（间隔符 U+00B7），保证 name 的 UNIQUE 约束不冲突；
 * - 提供商展示名 = 组别名（仅展示层）> 连接名中第一个 · 之前的部分（锚点派生）；
 * - 提供商改名（真名变更）= 按新前缀批量重生成组内全部连接名；别名 = 仅展示层，两者语义分离；
 * - 合并列表：提供商为可展开行（聚合行），其模型直接嵌套在展开区（明细行）。
 */
interface ProviderGroup {
  key: string // `pg:${provider_group_id}`（空 id 回退 `${protocol}::${base_url}`，兼容未回填数据）
  id: string // provider_group_id（空 = 回退分组）
  alias: string // 组别名（展示层）
  name: string // 提供商展示名（别名优先，缺省从锚点派生）
  baseUrl: string
  protocol: string
  anchor: ModelConnection
  members: ModelConnection[]
}

function groupOf(c: ModelConnection): string {
  return c.provider_group_id ? `pg:${c.provider_group_id}` : `${c.protocol}::${c.base_url}`
}

const NAME_SEP = '·'

/** 提供商名 = 连接名中第一个 · 之前的部分；老数据（无 ·）取整名 */
function providerOfName(name: string): string {
  const i = name.indexOf(NAME_SEP)
  return i > 0 ? name.slice(0, i) : name
}

/** 生成唯一连接名 `{provider}·{model}`；与 taken 冲突时追加 ` (n)`（n 从 2 起） */
function uniqueConnName(provider: string, model: string, taken: Set<string>): string {
  const base = `${provider}${NAME_SEP}${model}`
  let name = base
  for (let n = 2; taken.has(name); n += 1) {
    name = `${base} (${n})`
  }
  return name
}

const fmtNum = (n?: number) => (n ?? 0).toLocaleString()

/**
 * 设置页（原型 06 §3.6 v0.4 布局）：
 * - 左栏设置分类：模型管理（默认选中）/ 使用统计 / 全局参数（P1 预留）/ 数据与安全（P2 预留），同级独立；
 * - 右栏「模型管理」：提供商与模型合并为单个可折叠列表；
 * - 右栏「使用统计」：按模型 / 供应商 / 智能体 / 项目聚合 + 时间范围筛选。
 */
export default function SettingsPage() {
  const { showToast } = useUI()
  const [conns, setConns] = useState<ModelConnection[]>([])
  const [category, setCategory] = useState<Category>('models')
  // undefined = 关闭；'new' = 新建；对象 = 编辑
  const [providerModal, setProviderModal] = useState<ProviderGroup | 'new' | undefined>(undefined)
  const [modelModal, setModelModal] = useState<ModelConnection | 'new' | undefined>(undefined)
  // 新建模型时预选的提供商分组 key（从某提供商行「＋添加模型」进入）
  const [modelInitProvider, setModelInitProvider] = useState<string | undefined>(undefined)
  const [testing, setTesting] = useState<string | null>(null)
  // 合并列表：展开的提供商行；以及当前触发「自动获取模型」的提供商 key
  const [expandedKeys, setExpandedKeys] = useState<string[]>([])
  const [discoverKey, setDiscoverKey] = useState<string | null>(null)

  const [groupMetas, setGroupMetas] = useState<ProviderGroupMeta[]>([])
  const reload = () => {
    api.listConnections().then(setConns).catch(() => setConns([]))
    api.listProviderGroups().then(setGroupMetas).catch(() => setGroupMetas([]))
  }
  useEffect(reload, [])

  // 按创建顺序聚合（后端 ORDER BY created_at，id）：组内首个成员即锚点；展示名别名优先、缺省从锚点派生
  const groups = useMemo<ProviderGroup[]>(() => {
    const aliasOf = new Map(groupMetas.map((g) => [g.id, g.alias]))
    const map = new Map<string, ProviderGroup>()
    for (const c of conns) {
      const key = groupOf(c)
      let g = map.get(key)
      if (!g) {
        const alias = c.provider_group_id ? aliasOf.get(c.provider_group_id) ?? '' : ''
        g = { key, id: c.provider_group_id ?? '', alias, name: alias || providerOfName(c.name), baseUrl: c.base_url, protocol: c.protocol, anchor: c, members: [] }
        map.set(key, g)
      }
      g.members.push(c)
    }
    return [...map.values()]
  }, [conns, groupMetas])

  const hasChat = conns.some((c) => c.conn_type === 'chat' && c.has_key && c.enabled)

  const openModelModal = (conn: ModelConnection | 'new', providerKey?: string) => {
    setModelInitProvider(providerKey)
    setModelModal(conn)
  }

  const setDefault = async (c: ModelConnection) => {
    try {
      await api.setDefaultConnection(c.id)
      showToast(`已设为默认 ${c.conn_type === 'chat' ? '对话' : '向量'}模型`)
      reload()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  const remove = async (c: ModelConnection) => {
    try {
      await api.deleteConnection(c.id)
      showToast('已删除')
      reload()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  // 提供商删除 = 删除组内全部连接（Popconfirm 中明示数量；被智能体引用的后端会拒绝并中断）
  const removeGroup = async (g: ProviderGroup) => {
    for (const c of g.members) {
      try {
        await api.deleteConnection(c.id)
      } catch (e: any) {
        showToast(e.message, 'err')
        reload()
        return
      }
    }
    showToast(`已删除提供商「${g.name}」及 ${g.members.length} 个模型连接`)
    reload()
  }

  const test = async (c: ModelConnection) => {
    setTesting(c.id)
    try {
      const r = await api.testConnection({ id: c.id })
      showToast(r.ok ? `连接成功（${r.elapsed_ms}ms）` : `失败：${r.error}`, r.ok ? 'ok' : 'err')
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setTesting(null)
    }
  }

  // 「自动获取模型」：展开该提供商行并挂载发现面板（面板内自行拉取，失败降级为手动添加）
  const startDiscover = (g: ProviderGroup) => {
    setExpandedKeys((prev) => (prev.includes(g.key) ? prev : [...prev, g.key]))
    setDiscoverKey(g.key)
  }

  // 提供商行：名称 / Base URL / API Key 掩码 / 启用 / 模型数 / 行内操作
  const providerColumns: ColumnsType<ProviderGroup> = [
    {
      title: '提供商',
      dataIndex: 'name',
      render: (_, g) => <Typography.Text strong>{g.name}</Typography.Text>,
    },
    { title: 'Base URL', dataIndex: 'baseUrl', ellipsis: true, render: (v: string) => <Typography.Text code style={{ fontSize: 12 }}>{v}</Typography.Text> },
    {
      title: 'API Key',
      key: 'key',
      width: 150,
      render: (_, g) => {
        const withKey = g.members.find((m) => m.has_key)
        return <Typography.Text type={withKey ? 'secondary' : 'warning'} style={{ fontSize: 12 }}>{withKey ? withKey.api_key_hint : '未设置'}</Typography.Text>
      },
    },
    {
      title: '启用',
      key: 'status',
      width: 110,
      render: (_, g) => {
        const on = g.members.filter((m) => m.enabled).length
        if (on === g.members.length) return <Tag color="green" style={{ margin: 0 }}>启用</Tag>
        if (on === 0) return <Tag style={{ margin: 0 }}>停用</Tag>
        return <Tag color="orange" style={{ margin: 0 }}>启用 {on}/{g.members.length}</Tag>
      },
    },
    { title: '模型数', key: 'count', width: 80, align: 'center', render: (_, g) => <Tag style={{ margin: 0 }}>{g.members.length}</Tag> },
    {
      title: '操作',
      key: 'ops',
      width: 360,
      render: (_, g) => {
        // 测试代表连接：优先取组内存有 Key 的成员
        const rep = g.members.find((m) => m.has_key) ?? g.anchor
        return (
          <Space size={0} wrap>
            <Button type="link" size="small" loading={testing === rep.id} onClick={() => test(rep)}>测试连接</Button>
            <Button type="link" size="small" onClick={() => setProviderModal(g)}>编辑</Button>
            <Popconfirm
              title={`删除提供商「${g.name}」？`}
              description={`将同时删除其下全部 ${g.members.length} 个模型连接；被智能体引用的连接需先解除引用。`}
              okText="删除"
              okButtonProps={{ danger: true }}
              cancelText="取消"
              onConfirm={() => removeGroup(g)}
            >
              <Button type="link" size="small" danger>删除</Button>
            </Popconfirm>
            <Button type="link" size="small" onClick={() => openModelModal('new', g.key)}>＋添加模型</Button>
            <Button type="link" size="small" onClick={() => startDiscover(g)}>自动获取模型</Button>
          </Space>
        )
      },
    },
  ]

  // 展开区：一条连接一行（模型名 / 类型 / 默认 / 操作）
  const modelColumns: ColumnsType<ModelConnection> = [
    { title: '模型', dataIndex: 'model_name', render: (v: string) => <Typography.Text code style={{ fontSize: 12 }}>{v}</Typography.Text> },
    { title: '类型', dataIndex: 'conn_type', width: 110, render: (t: string) => (t === 'chat' ? <Tag color="blue">chat</Tag> : <Tag color="green">embedding</Tag>) },
    { title: '默认', dataIndex: 'is_default', width: 130, render: (_, c) => (c.is_default ? <Tag color="gold" style={{ margin: 0 }}>{c.conn_type} 默认</Tag> : <Typography.Text type="secondary">—</Typography.Text>) },
    {
      title: '操作',
      key: 'ops',
      width: 250,
      render: (_, c) => (
        <Space size={0} wrap>
          <Button type="link" size="small" loading={testing === c.id} onClick={() => test(c)}>测试</Button>
          <Button type="link" size="small" onClick={() => openModelModal(c)}>编辑</Button>
          {c.is_default ? null : <Button type="link" size="small" onClick={() => setDefault(c)}>设为默认</Button>}
          <Popconfirm
            title={`删除模型「${c.model_name}」？`}
            description="仅删除该模型连接；同提供商的其他模型不受影响。"
            okText="删除"
            okButtonProps={{ danger: true }}
            cancelText="取消"
            onConfirm={() => remove(c)}
          >
            <Button type="link" size="small" danger>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const expandedRowRender = (g: ProviderGroup) => (
    <div className="provider-models">
      {discoverKey === g.key && (
        <DiscoverPanel
          key={g.key}
          group={g}
          conns={conns}
          onManualAdd={() => openModelModal('new', g.key)}
          onClose={() => setDiscoverKey(null)}
          onAdded={() => { setDiscoverKey(null); reload() }}
        />
      )}
      <Table<ModelConnection>
        rowKey="id"
        columns={modelColumns}
        dataSource={g.members}
        pagination={false}
        size="small"
        showHeader={false}
        locale={{ emptyText: '暂无模型，点击该行「＋添加模型」' }}
      />
    </div>
  )

  return (
    <Splitter
      className="main sidebar-splitter"
      onResizeEnd={(sizes) => localStorage.setItem('eino.sidebar.width', String(Math.round(sizes[0])))}
    >
      <Splitter.Panel defaultSize={Number(localStorage.getItem('eino.sidebar.width')) || 280} min={220} max={480} className="sidebar-panel">
        <aside className="sidebar">
          <div className="side-head">
            <span className="side-title">设置</span>
          </div>
          <Menu
            mode="vertical"
            selectedKeys={[category]}
            onClick={({ key }) => setCategory(key as Category)}
            style={{ padding: '0 10px', background: 'transparent' }}
            items={[
              { key: 'models', label: '模型管理' },
              { key: 'inference', label: '推理后端' },
              { key: 'stats', label: '使用统计' },
              { key: 'global', label: <Space size={6}>全局参数<Tag style={{ margin: 0 }}>P1 预留</Tag></Space>, disabled: true },
              { key: 'security', label: '数据与安全' },
            ]}
          />
          <div className="settings-note">
            模型连接集中在此维护，智能体配置只做<strong>引用</strong>（chat / embedding 各至多一条默认）。
          </div>
        </aside>
      </Splitter.Panel>
      <Splitter.Panel className="content-panel">

        <div className="settings-main">
          {category === 'inference' ? (
            <InferencePanel />
          ) : category === 'security' ? (
            <SecurityPanel />
          ) : category === 'stats' ? (
            <>
              <div className="settings-head">
                <Typography.Title level={5} style={{ marginTop: 0, marginBottom: 4 }}>使用统计</Typography.Title>
                <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
                  按模型 / 供应商 / 智能体 / 项目聚合调用次数与 token 消耗；「按供应商」在前端按 Base URL 归并（后端无提供商实体）。支持按时间范围（含首尾）筛选。
                </Typography.Paragraph>
              </div>
              <div style={{ marginTop: 12 }}>
                <StatsView />
              </div>
            </>
          ) : (
            <>
              <div className="settings-head">
                <Typography.Title level={5} style={{ marginTop: 0, marginBottom: 4 }}>模型管理</Typography.Title>
                <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
                  提供商按 Base URL 聚合（同一 Base URL 下的多个模型共享提供商身份）；chat / embedding 各设一条默认模型，供智能体「跟随全局默认」引用。API Key 使用 AES-256-GCM 加密存储于本地（密钥文件 data/.secret）。添加提供商可从厂商预设（DeepSeek / 百炼 / 千帆 / 智谱 / Kimi / 硅基流动 / MiniMax / 星火）快速填充；已预置「百度千帆（预置）」embeddings-v1 向量连接候选——填入 Key 并启用即为默认向量连接。
                </Typography.Paragraph>
              </div>

              {!hasChat && (
                <Alert
                  type="warning"
                  showIcon
                  style={{ marginTop: 12 }}
                  message="尚未配置可用的对话模型"
                  description="预置了「DeepSeek（预置）」连接：填入 API Key 并启用、设为默认，即可开始对话。"
                />
              )}

              <div style={{ marginTop: 12 }}>
                <Table<ProviderGroup>
                  rowKey="key"
                  columns={providerColumns}
                  dataSource={groups}
                  pagination={false}
                  size="middle"
                  scroll={{ x: 960 }}
                  expandable={{
                    expandedRowKeys: expandedKeys,
                    onExpandedRowsChange: (keys) => {
                      const arr = keys as string[]
                      setExpandedKeys(arr)
                      if (discoverKey && !arr.includes(discoverKey)) setDiscoverKey(null)
                    },
                    expandedRowRender,
                  }}
                  locale={{ emptyText: '暂无提供商，点击下方按钮添加' }}
                />
                <div className="tab-footer">
                  <Button type="primary" onClick={() => setProviderModal('new')}>＋ 添加提供商</Button>
                  <Button onClick={() => openModelModal('new')}>＋ 添加模型</Button>
                  <span className="hint">展开提供商行查看其模型；「自动获取模型」从接入点拉取可用模型并批量创建连接</span>
                </div>
              </div>
            </>
          )}
        </div>

        {providerModal !== undefined && (
          <ProviderModal
            group={providerModal}
            conns={conns}
            onClose={() => setProviderModal(undefined)}
            onSaved={(providerKey) => {
              setProviderModal(undefined)
              reload()
              // REQ-106 串联 REQ-48：从预设/表单新建提供商后，展开该分组并挂自动发现面板，
              // 可直接拉取该厂商可用模型批量建连（面板失败仍有手动添加降级）。
              if (providerKey) {
                setExpandedKeys((prev) => (prev.includes(providerKey) ? prev : [...prev, providerKey]))
                setDiscoverKey(providerKey)
              }
            }}
          />
        )}
        {modelModal !== undefined && (
          <ModelModal
            conn={modelModal}
            groups={groups}
            conns={conns}
            initialProvider={modelInitProvider}
            onClose={() => { setModelModal(undefined); setModelInitProvider(undefined) }}
            onSaved={() => { setModelModal(undefined); setModelInitProvider(undefined); reload() }}
          />
        )}
      </Splitter.Panel>
    </Splitter>
  )
}

/**
 * 自动发现面板（挂在提供商展开区）：
 * - 拉取 `POST /api/model-connections/{anchorId}/list-models`（ASSUMED 契约）；
 * - 成功：多选清单（已添加项置灰）→ 批量创建连接（copy_key_from 锚点，名称按 `{提供商}·{模型}`）；
 * - 失败 / 404：提示「自动发现接口未就绪，可手动添加」，手动添加仍可用。
 */
function DiscoverPanel({ group, conns, onManualAdd, onClose, onAdded }: {
  group: ProviderGroup
  conns: ModelConnection[]
  onManualAdd: () => void
  onClose: () => void
  onAdded: () => void
}) {
  const { showToast } = useUI()
  const [loading, setLoading] = useState(true)
  const [models, setModels] = useState<string[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const [connType, setConnType] = useState<'chat' | 'embedding'>('chat')
  const [busy, setBusy] = useState(false)

  const existing = new Set(group.members.map((m) => m.model_name))

  useEffect(() => {
    let alive = true
    setLoading(true)
    setError(null)
    api.listProviderModels(group.anchor.id)
      .then((r) => {
        if (!alive) return
        const found = Array.isArray(r.models) ? r.models : []
        setModels(found)
        // 默认勾选「尚未添加」的模型
        setSelected(found.filter((m) => !group.members.some((x) => x.model_name === m)))
      })
      .catch((e: any) => { if (alive) setError(e?.message ?? '请求失败') })
      .finally(() => { if (alive) setLoading(false) })
    return () => { alive = false }
    // 面板以 group.key 为 React key，切换提供商即重挂载；仅随锚点变化重取
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [group.anchor.id])

  const addable = (models ?? []).filter((m) => !existing.has(m))

  const apply = async () => {
    setBusy(true)
    try {
      const taken = new Set(conns.map((c) => c.name))
      let n = 0
      for (const m of selected) {
        const name = uniqueConnName(group.name, m, taken)
        taken.add(name)
        await api.createConnection({
          name,
          protocol: group.protocol,
          base_url: group.baseUrl,
          model_name: m,
          conn_type: connType,
          provider_group_id: group.id, // REQ-148：归属该实例分组
          copy_key_from: group.anchor.id, // Key 归属提供商：与锚点共享同一份密文
          enabled: true,
          is_default: false,
        })
        n += 1
      }
      showToast(`已添加 ${n} 个模型连接`)
      onAdded()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  if (loading) {
    return (
      <div className="discover-panel">
        <Spin size="small" />
        <span className="discover-loading-text">正在获取模型列表…</span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="discover-panel">
        <div className="discover-head">
          <span className="discover-title">自动获取模型列表</span>
          <span className="discover-provider">{group.name}</span>
        </div>
        <Alert
          type="warning"
          showIcon
          message="自动发现接口未就绪，可手动添加"
          description={`${error}（POST /api/model-connections/{id}/list-models 待后端跟进）`}
        />
        <div className="discover-foot">
          <Button size="small" type="primary" onClick={onManualAdd}>手动添加模型</Button>
          <Button size="small" onClick={onClose}>关闭</Button>
        </div>
      </div>
    )
  }

  return (
    <div className="discover-panel">
      <div className="discover-head">
        <span className="discover-title">发现 {models?.length ?? 0} 个模型</span>
        <Tag color="blue" style={{ margin: 0 }}>可添加 {addable.length}</Tag>
        <span className="discover-provider">{group.name} · 沿用提供商 Key</span>
      </div>
      <div className="discover-toolbar">
        <span className="discover-label">类型</span>
        <Segmented
          size="small"
          value={connType}
          onChange={(v) => setConnType(v as 'chat' | 'embedding')}
          options={[{ label: 'chat', value: 'chat' }, { label: 'embedding', value: 'embedding' }]}
        />
        <Button type="link" size="small" disabled={addable.length === 0} onClick={() => setSelected(addable)}>全选可添加</Button>
        <Button type="link" size="small" disabled={selected.length === 0} onClick={() => setSelected([])}>清空</Button>
      </div>
      <Checkbox.Group value={selected} onChange={(v) => setSelected(v as string[])} style={{ display: 'block' }}>
        <div className="discover-list">
          {(models ?? []).map((m) => (
            <Checkbox key={m} value={m} disabled={existing.has(m)}>
              <span className="discover-model">{m}</span>
              {existing.has(m) && <Tag style={{ marginInlineStart: 6 }}>已添加</Tag>}
            </Checkbox>
          ))}
        </div>
      </Checkbox.Group>
      <div className="discover-foot">
        <Button type="primary" size="small" loading={busy} disabled={selected.length === 0} onClick={apply}>
          添加所选（{selected.length}）
        </Button>
        <Button size="small" onClick={onManualAdd}>手动添加</Button>
        <Button size="small" type="text" onClick={onClose}>取消</Button>
      </div>
    </div>
  )
}

const UNMATCHED_PROVIDER = '未匹配供应商'

/**
 * 按供应商聚合（前端归并，后端无提供商实体）：
 * - 以 `model_name → 提供商名` 建立索引（提供商名沿用命名约定 `{提供商}·{模型}` 的前缀，复用 providerOfName）；
 * - 将 group_by=model 的统计行按提供商累加 calls / tokens；
 * - 未匹配到任何连接的模型归入「未匹配供应商」（恒排末位），其余按总 tokens 降序。
 */
function aggregateByProvider(rows: UsageRow[], conns: ModelConnection[], groupAliasOf: (c: ModelConnection) => string): UsageRow[] {
  // run.started 的 model 标签 = `{连接名}@{模型名}`（assembler.buildModel），按该键映射供应商展示名；
  // 兼容更早版本只以模型名为 label 的存量统计行（同模型名多实例时映射取先到者）
  const labelToProvider = new Map<string, string>()
  for (const c of conns) {
    labelToProvider.set(`${c.name}@${c.model_name}`, groupAliasOf(c))
    if (!labelToProvider.has(c.model_name)) labelToProvider.set(c.model_name, groupAliasOf(c))
  }
  const acc = new Map<string, UsageRow>()
  for (const r of rows) {
    const label = labelToProvider.get(r.label) ?? UNMATCHED_PROVIDER
    const key = `provider::${label}`
    let a = acc.get(key)
    if (!a) {
      a = { key, label, calls: 0, prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 }
      acc.set(key, a)
    }
    a.calls += r.calls || 0
    a.prompt_tokens += r.prompt_tokens || 0
    a.completion_tokens += r.completion_tokens || 0
    a.total_tokens += r.total_tokens || 0
  }
  return [...acc.values()].sort((a, b) => {
    if (a.label === UNMATCHED_PROVIDER) return 1
    if (b.label === UNMATCHED_PROVIDER) return -1
    return b.total_tokens - a.total_tokens
  })
}

/**
 * 使用统计（ASSUMED 契约 `GET /api/stats/usage?group_by=…&from=…&to=…`）：
 * - 维度切换：按模型 / 按供应商（前端归并）/ 按智能体 / 按项目；
 * - 时间范围：DatePicker.RangePicker（含首尾），转为 YYYY-MM-DD 传给后端；
 * - 总 tokens 用纯 CSS 横条表示（不引入图表依赖）；接口未就绪 → Result 提示 + 重试。
 */
function StatsView() {
  const [dimension, setDimension] = useState<StatsDimension>('model')
  const [range, setRange] = useState<[Dayjs, Dayjs] | null>(null)
  const [rows, setRows] = useState<UsageRow[] | null>(null)
  const [conns, setConns] = useState<ModelConnection[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [tick, setTick] = useState(0)

  // 供应商为前端维度：数据仍取后端 model 维度行（label = model_name）
  const apiGroupBy: UsageGroupBy = dimension === 'supplier' ? 'model' : dimension
  const from = range ? range[0].format('YYYY-MM-DD') : undefined
  const to = range ? range[1].format('YYYY-MM-DD') : undefined

  useEffect(() => {
    let alive = true
    setLoading(true)
    setError(null)
    api.usageStats(apiGroupBy, { from, to })
      .then((r) => { if (alive) setRows(Array.isArray(r.rows) ? r.rows : []) })
      .catch((e: any) => { if (alive) { setError(e?.message ?? '请求失败'); setRows(null) } })
      .finally(() => { if (alive) setLoading(false) })
    return () => { alive = false }
  }, [apiGroupBy, from, to, tick])

  // 「按供应商」需连接列表做归并；连接拉取失败仅退化为「未匹配供应商」，不影响统计本身
  useEffect(() => {
    if (dimension !== 'supplier') return
    let alive = true
    api.listConnections()
      .then((c) => { if (alive) setConns(c) })
      .catch(() => { if (alive) setConns([]) })
    return () => { alive = false }
  }, [dimension, tick])

  const groupAliasOf = (c: ModelConnection) =>
    c.provider_alias
      ? c.name.includes('·')
        ? `${c.provider_alias}·${c.name.slice(c.name.indexOf('·') + 1)}`
        : `${c.provider_alias}·${c.model_name}`
      : providerOfName(c.name)
  const list = dimension === 'supplier' ? aggregateByProvider(rows ?? [], conns, groupAliasOf) : (rows ?? [])
  const totalCalls = list.reduce((s, r) => s + (r.calls || 0), 0)
  const totalTokens = list.reduce((s, r) => s + (r.total_tokens || 0), 0)
  const maxTotal = Math.max(1, ...list.map((r) => r.total_tokens || 0))

  const columns: ColumnsType<UsageRow> = [
    { title: '名称', dataIndex: 'label', render: (v: string, r) => <Typography.Text strong>{v || r.key}</Typography.Text> },
    { title: '调用次数', dataIndex: 'calls', width: 120, align: 'right', render: (v: number) => fmtNum(v) },
    { title: 'Prompt tokens', dataIndex: 'prompt_tokens', width: 150, align: 'right', render: (v: number) => fmtNum(v) },
    { title: 'Completion tokens', dataIndex: 'completion_tokens', width: 170, align: 'right', render: (v: number) => fmtNum(v) },
    {
      title: '总 tokens',
      dataIndex: 'total_tokens',
      width: 260,
      render: (v: number) => (
        <div className="usage-bar-cell">
          <div className="usage-bar-track">
            <div className="usage-bar-fill" style={{ width: `${Math.round(((v || 0) / maxTotal) * 100)}%` }} />
          </div>
          <span className="usage-bar-num">{fmtNum(v)}</span>
        </div>
      ),
    },
  ]

  if (error) {
    return (
      <div className="usage-main">
        <Result
          status="warning"
          title="统计接口未就绪"
          subTitle={`${error}（后端车道跟进）`}
          extra={<Button onClick={() => setTick((t) => t + 1)}>重试</Button>}
        />
      </div>
    )
  }

  return (
    <div className="usage-main">
      <div className="usage-toolbar">
        <Segmented
          value={dimension}
          onChange={(v) => setDimension(v as StatsDimension)}
          options={[
            { label: '按模型', value: 'model' },
            { label: '按供应商', value: 'supplier' },
            { label: '按智能体', value: 'agent' },
            { label: '按项目', value: 'project' },
          ]}
        />
        <DatePicker.RangePicker
          value={range}
          onChange={(dates) => {
            const s = dates?.[0]
            const e = dates?.[1]
            setRange(s && e ? [s, e] : null)
          }}
          placeholder={['开始日期', '结束日期']}
          allowClear
        />
        {range && (
          <>
            <Tag color="blue" style={{ margin: 0 }}>{from} ~ {to}</Tag>
            <Button type="link" size="small" onClick={() => setRange(null)}>清除</Button>
          </>
        )}
        <span className="usage-hint">时间范围含首尾；留空为全部</span>
        <Button size="small" style={{ marginLeft: 'auto' }} loading={loading} onClick={() => setTick((t) => t + 1)}>刷新</Button>
      </div>
      <div className="usage-summary">
        <div className="usage-stat">
          <div className="usage-stat-label">条目数</div>
          <div className="usage-stat-value">{fmtNum(list.length)}</div>
        </div>
        <div className="usage-stat">
          <div className="usage-stat-label">总调用次数</div>
          <div className="usage-stat-value">{fmtNum(totalCalls)}</div>
        </div>
        <div className="usage-stat">
          <div className="usage-stat-label">总 tokens</div>
          <div className="usage-stat-value">{fmtNum(totalTokens)}</div>
        </div>
      </div>
      <Table<UsageRow>
        rowKey="key"
        columns={columns}
        dataSource={list}
        loading={loading}
        pagination={false}
        size="middle"
        locale={{ emptyText: '暂无统计数据' }}
      />
    </div>
  )
}

/**
 * 提供商表单：
 * - 新建：后端无独立提供商实体，添加提供商将同时创建其首个模型连接（含模型名/类型）；
 *   顶部「厂商预设」快速填充协议与 Base URL（REQ-106），仅需补 API Key 与模型名；
 *   保存成功后返回新建分组的 key，由父级展开该行并挂自动发现面板（串联 REQ-48）。
 * - 编辑：名称/协议/Base URL/API Key/启用 批量应用到组内全部连接（名称按约定重生成）；API Key 留空 = 各连接保留已存 Key。
 */
function ProviderModal({ group, conns, onClose, onSaved }: { group: ProviderGroup | 'new'; conns: ModelConnection[]; onClose: () => void; onSaved: (providerKey?: string) => void }) {
  const { showToast } = useUI()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)
  const editGroup = group === 'new' ? null : group
  const keyMember = editGroup?.members.find((m) => m.has_key)
  // 选中的厂商预设（REQ-106；仅新建态）
  const [presetKey, setPresetKey] = useState<string | undefined>(undefined)
  const preset = PROVIDER_PRESETS.find((p) => p.key === presetKey)

  const applyPreset = (key?: string) => {
    setPresetKey(key)
    const p = PROVIDER_PRESETS.find((x) => x.key === key)
    if (p) {
      form.setFieldsValue({ name: p.name, protocol: 'openai_compat', base_url: p.baseUrl, model_name: p.defaultModel, conn_type: p.connType })
    }
  }

  useEffect(() => {
    if (editGroup) {
      // 名称（真名前缀）从锚点连接名派生；别名仅展示层——两者语义分离（REQ-148）
      form.setFieldsValue({ name: providerOfName(editGroup.anchor.name), alias: editGroup.alias, protocol: editGroup.protocol, base_url: editGroup.baseUrl, api_key: '', enabled: editGroup.members.every((m) => m.enabled) })
    } else {
      form.setFieldsValue({ name: '', protocol: 'openai_compat', base_url: 'https://api.deepseek.com/v1', model_name: 'deepseek-chat', conn_type: 'chat', api_key: '', enabled: true })
    }
  }, [group, form])

  const save = async () => {
    let v: any
    try {
      v = await form.validateFields()
    } catch {
      return
    }
    setBusy(true)
    try {
      if (editGroup) {
        // 批量应用到组内全部连接（顺序调用）；名称按 `{提供商}·{模型}` 重生成（重名追加 (n)）
        const groupIds = new Set(editGroup.members.map((m) => m.id))
        const used = new Set(conns.filter((c) => !groupIds.has(c.id)).map((c) => c.name))
        for (const m of editGroup.members) {
          const name = uniqueConnName(v.name, m.model_name, used)
          used.add(name)
          const payload: any = { ...m, name, protocol: v.protocol, base_url: v.base_url, enabled: !!v.enabled }
          if (v.api_key) payload.api_key = v.api_key
          await api.updateConnection(m.id, payload)
        }
        // 别名（REQ-148）：仅展示层；变化时独立保存，不影响真名
        if (editGroup.id && (v.alias ?? '') !== editGroup.alias) {
          await api.updateProviderGroupAlias(editGroup.id, v.alias ?? '')
        }
      } else {
        // REQ-148：先建供应商分组（别名缺省 = 名称），连接归属该组——同 BaseURL 可再次添加为独立实例
        const g = await api.createProviderGroup((v.alias ?? '').trim() || v.name.trim())
        await api.createConnection({
          name: uniqueConnName(v.name, v.model_name, new Set(conns.map((c) => c.name))),
          protocol: v.protocol,
          base_url: v.base_url,
          model_name: v.model_name,
          conn_type: v.conn_type,
          provider_group_id: g.id,
          api_key: v.api_key ?? '',
          enabled: v.enabled ?? true,
          is_default: false,
        })
        // 新建成功：把分组 key 交回父级——展开该行并挂自动发现面板（REQ-106 串联 REQ-48）
        showToast('已保存')
        onSaved(`pg:${g.id}`)
        return
      }
      showToast('已保存')
      onSaved()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const testNow = async () => {
    setBusy(true)
    try {
      // 编辑态测组内代表连接（优先取存有 Key 的成员）的已存配置；新建态测表单值
      const rep = editGroup ? (editGroup.members.find((m) => m.has_key) ?? editGroup.anchor) : null
      const payload = rep
        ? { id: rep.id }
        : { conn_type: form.getFieldValue('conn_type'), base_url: form.getFieldValue('base_url'), model_name: form.getFieldValue('model_name'), api_key: form.getFieldValue('api_key') }
      const r = await api.testConnection(payload)
      showToast(r.ok ? `连接成功（${r.elapsed_ms}ms）` : `失败：${r.error}`, r.ok ? 'ok' : 'err')
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      centered
      title={editGroup ? `编辑提供商 · ${editGroup.name}` : '添加提供商'}
      onCancel={onClose}
      width={560}
      footer={
        <Space>
          <Button onClick={testNow} disabled={busy}>先测试</Button>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={busy} onClick={save}>保存</Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        {!editGroup && (
          <Form.Item
            label="从厂商预设快速填充"
            extra={
              preset?.console ? (
                <>
                  已自动填充协议与 Base URL，仅需补 API Key 与模型名；Key 申请：
                  <Typography.Link href={preset.console} target="_blank" rel="noreferrer">{preset.console}</Typography.Link>
                </>
              ) : (
                '选择厂商自动填充协议与 Base URL（REQ-105 预设清单）；也可留空手动填写'
              )
            }
          >
            <Select
              placeholder="选择厂商预设（DeepSeek / 百炼 / 千帆 / 智谱 / Kimi / 硅基流动 / MiniMax / 星火）"
              allowClear
              value={presetKey}
              onChange={applyPreset}
              options={PROVIDER_PRESETS.map((p) => ({ value: p.key, label: `${p.name} · ${p.baseUrl}` }))}
            />
          </Form.Item>
        )}
        <Form.Item name="name" label="名称" rules={[{ required: true, message: '名称必填' }]} extra={editGroup ? '改名将按「名称·模型名」重生成该提供商下全部连接名' : undefined}>
          <Input placeholder="DeepSeek 官方" />
        </Form.Item>
        <Form.Item name="alias" label="显示别名（可选）" extra="仅展示层：列表/模型下拉显示别名·模型名；不改连接真名，真名变更用上方「名称」（REQ-148）">
          <Input placeholder="如：DeepSeek 工作号" />
        </Form.Item>
        <Form.Item name="protocol" label="协议">
          <Select options={[{ value: 'openai_compat', label: 'openai_compat（OpenAI 兼容）' }]} />
        </Form.Item>
        <Form.Item name="base_url" label="Base URL（OpenAI 兼容）" rules={[{ required: true, message: 'Base URL 必填' }]} extra={editGroup ? '修改本实例的接入点（分组身份独立于 Base URL，不影响其他同名供应商实例）' : '同一供应商可再次添加为独立实例（不同账号/Key）'}>
          <Input placeholder="https://api.deepseek.com/v1" />
        </Form.Item>
        <Form.Item
          name="api_key"
          label={keyMember ? <span>API Key <Tag color="green" style={{ marginInlineStart: 6 }}>已存 {keyMember.api_key_hint}</Tag></span> : 'API Key'}
          extra={editGroup ? '留空 = 各模型连接保留已存 Key；填写 = 应用到该提供商下全部连接' : '保存后 AES-256-GCM 加密，仅显示掩码'}
        >
          <Input.Password placeholder={keyMember ? '不修改请留空' : 'sk-…'} autoComplete="new-password" />
        </Form.Item>
        {!editGroup && (
          <Space size={12} style={{ display: 'flex' }}>
            <Form.Item name="model_name" label="首个模型名" rules={[{ required: true, message: '模型名必填' }]} style={{ flex: 1 }} extra="后端无独立提供商实体，添加提供商将同时创建首个模型连接">
              <Input placeholder="deepseek-chat" />
            </Form.Item>
            <Form.Item name="conn_type" label="类型" style={{ width: 140 }}>
              <Select
                options={[
                  { value: 'chat', label: 'chat（对话）' },
                  { value: 'embedding', label: 'embedding（向量）' },
                ]}
              />
            </Form.Item>
          </Space>
        )}
        <Form.Item name="enabled" label="启用" valuePropName="checked" extra={editGroup ? '将应用到该提供商下全部模型连接' : undefined}>
          <Switch checkedChildren="启用" unCheckedChildren="停用" />
        </Form.Item>
      </Form>
    </Modal>
  )
}

/**
 * 模型表单：所属提供商（分组下拉，name/base_url/protocol 随组）· 模型名 · 类型 · 设为默认。
 * - API Key 归属提供商：新建时用 copy_key_from 复用组锚点的密文，模型表单不再出现 Key；
 * - 编辑时切换提供商 = 移动到目标组（名称/Base URL/协议随目标组，Key 亦改用目标提供商）；
 * - 提供商未变则两个 Key 字段都不发送（后端保留原 Key）。
 * - initialProvider：从某提供商行「＋添加模型」进入时预选该分组。
 */
function ModelModal({ conn, groups, conns, initialProvider, onClose, onSaved }: {
  conn: ModelConnection | 'new'
  groups: ProviderGroup[]
  conns: ModelConnection[]
  initialProvider?: string
  onClose: () => void
  onSaved: () => void
}) {
  const { showToast } = useUI()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)
  const editConn = conn === 'new' ? null : conn

  useEffect(() => {
    if (editConn) {
      form.setFieldsValue({ provider: groupOf(editConn), model_name: editConn.model_name, conn_type: editConn.conn_type, is_default: editConn.is_default })
    } else {
      form.setFieldsValue({ provider: initialProvider ?? groups[0]?.key, model_name: '', conn_type: 'chat', is_default: false })
    }
  }, [conn, form, groups, initialProvider])

  const providerKey = Form.useWatch('provider', form)
  const providerGroup = groups.find((g) => g.key === providerKey)

  const save = async () => {
    let v: any
    try {
      v = await form.validateFields()
    } catch {
      return
    }
    setBusy(true)
    try {
      if (editConn) {
        const target = providerGroup && providerGroup.key !== groupOf(editConn) ? providerGroup : null
        // 名称按 `{提供商}·{模型}` 维护：移动或改模型名时重生成（排除自身，重名追加 (n)）
        const takenOthers = new Set(conns.filter((c) => c.id !== editConn.id).map((c) => c.name))
        const name = target
          ? uniqueConnName(target.name, v.model_name, takenOthers)
          : v.model_name !== editConn.model_name
            ? uniqueConnName(providerGroup?.name ?? providerOfName(editConn.name), v.model_name, takenOthers)
            : editConn.name
        await api.updateConnection(editConn.id, {
          ...editConn,
          ...(target ? { protocol: target.protocol, base_url: target.baseUrl, copy_key_from: target.anchor.id, provider_group_id: target.id } : {}),
          name,
          model_name: v.model_name,
          conn_type: v.conn_type,
          is_default: v.is_default,
        })
        if (v.is_default) await api.setDefaultConnection(editConn.id)
      } else {
        if (!providerGroup) throw new Error('请先选择提供商；新提供商请先添加')
        const created = await api.createConnection({
          name: uniqueConnName(providerGroup.name, v.model_name, new Set(conns.map((c) => c.name))),
          protocol: providerGroup.protocol,
          base_url: providerGroup.baseUrl,
          model_name: v.model_name,
          conn_type: v.conn_type,
          provider_group_id: providerGroup.id, // REQ-148：归属所选实例分组
          copy_key_from: providerGroup.anchor.id, // Key 归属提供商：与组锚点共享同一份密文
          enabled: true,
          is_default: false,
        })
        if (v.is_default) await api.setDefaultConnection(created.id)
      }
      showToast('已保存')
      onSaved()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      centered
      title={editConn ? `编辑模型 · ${editConn.model_name}` : '添加模型'}
      onCancel={onClose}
      width={560}
      footer={
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={busy} onClick={save}>保存</Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        {!editConn && groups.length === 0 && (
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 16 }}
            message="尚无提供商"
            description="请先关闭本弹窗，用「＋ 添加提供商」从厂商预设快速填充访问配置（仅需补 API Key）。"
          />
        )}
        <Form.Item name="provider" label="所属提供商" rules={[{ required: true, message: '请选择提供商；新提供商请先添加' }]}>
          <Select
            placeholder="选择提供商"
            options={groups.map((g) => ({ value: g.key, label: g.name }))}
          />
        </Form.Item>
        <Typography.Paragraph type="secondary" style={{ marginTop: -14, marginBottom: 16, fontSize: 12 }}>
          接入点：{providerGroup?.baseUrl || '—'}（随所选提供商）
        </Typography.Paragraph>
        <Space size={12} style={{ display: 'flex' }}>
          <Form.Item name="model_name" label="模型名" rules={[{ required: true, message: '模型名必填' }]} style={{ flex: 1 }}>
            <Input placeholder="deepseek-chat" />
          </Form.Item>
          <Form.Item name="conn_type" label="类型" style={{ width: 140 }}>
            <Select
              options={[
                { value: 'chat', label: 'chat（对话）' },
                { value: 'embedding', label: 'embedding（向量）' },
              ]}
            />
          </Form.Item>
        </Space>
        <Form.Item label="API Key">
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            API Key 由提供商统一管理{editConn ? '；移动到其他提供商时自动沿用目标提供商的 Key。' : '；保存后自动沿用所选提供商的 Key。'}
          </Typography.Text>
        </Form.Item>
        <Form.Item name="is_default" label="设为该类型默认" valuePropName="checked" extra="chat / embedding 各至多一条默认；「跟随全局默认」的智能体将使用它">
          <Switch />
        </Form.Item>
      </Form>
    </Modal>
  )
}

/** M13/D-O13 §6.16：推理后端面板——已发现清单（PATH 探测 + 版本）+ 重新探测；能力矩阵（§6.16.4） */
function InferencePanel() {
  const { showToast } = useUI()
  const [backends, setBackends] = useState<InferenceBackendStatus[]>([])
  const [loading, setLoading] = useState(true)
  const [probing, setProbing] = useState(false)

  const load = (force = false) => {
    setLoading(true)
    const p = force ? api.reprobeInferenceBackends() : api.listInferenceBackends()
    p.then((r) => setBackends(r.backends ?? []))
      .catch((e) => showToast(e.message, 'err'))
      .finally(() => setLoading(false))
  }
  useEffect(() => { load(false) }, [])

  const columns: ColumnsType<InferenceBackendStatus> = [
    {
      title: '后端', dataIndex: 'name', width: 160,
      render: (v: string, r) => (
        <Space size={6}>
          <span style={{ fontWeight: 600 }}>{v}</span>
          {r.default && <Tag color="blue" style={{ margin: 0 }}>默认</Tag>}
        </Space>
      ),
    },
    {
      title: '状态', dataIndex: 'available', width: 110,
      render: (ok: boolean, r) => ok
        ? <Tag color="green" style={{ margin: 0 }}>可用{r.version ? ` · ${r.version}` : ''}</Tag>
        : <Tag color="default" style={{ margin: 0 }}>未发现</Tag>,
    },
    {
      title: '能力', key: 'caps',
      render: (_, r) => {
        const c = r.capabilities
        if (c.agent_as_tool) return <span>对话 / 流式 / 技能(工具) / MCP(工具) / 多 Agent / 工作流 / 可恢复</span>
        return <span>对话 / 流式；技能→instruction 注入；MCP→prompt 注入；无多 Agent 与工作流；仅中断</span>
      },
    },
    {
      title: '路径 / 说明', key: 'where',
      render: (_, r) => <Typography.Text type="secondary" style={{ fontSize: 12 }}>{r.path || r.reason || '-'}</Typography.Text>,
    },
  ]

  return (
    <div style={{ marginTop: 12 }}>
      <Table<InferenceBackendStatus>
        rowKey="name"
        loading={loading}
        columns={columns}
        dataSource={backends}
        pagination={false}
        size="middle"
        locale={{ emptyText: '暂无推理后端' }}
      />
      <div className="tab-footer">
        <Button loading={probing} onClick={() => { setProbing(true); load(true); setProbing(false) }}>重新探测</Button>
        <span className="hint">外部 CLI 后端按 PATH 探测（结果缓存 10 分钟）；eino-adk 为平台自研默认（完整能力），外部后端能力降级（技能/MCP 注入为提示，不支持多 Agent 编排）</span>
      </div>
    </div>
  )
}

/**
 * 数据与安全（REQ-113② 最小版）：
 * - 数据量概览：DB 文件体积 + 各表行数（SQLite 单文件，学习尺度全表 COUNT）；
 * - 会话/项目管理：列表附级联规模（消息数 / 会话数），删除 Popconfirm 明示级联范围——
 *   删除对话级联其消息与过程事件；删除项目级联其全部对话（含消息与事件）。
 */
function SecurityPanel() {
  const { showToast } = useUI()
  const [data, setData] = useState<Awaited<ReturnType<typeof api.storageOverview>> | null>(null)
  const [loading, setLoading] = useState(true)
  const [tick, setTick] = useState(0)

  useEffect(() => {
    let alive = true
    setLoading(true)
    api
      .storageOverview()
      .then((r) => alive && setData(r))
      .catch((e) => alive && showToast(e.message, 'err'))
      .finally(() => alive && setLoading(false))
    return () => {
      alive = false
    }
  }, [tick, showToast])

  const delConv = async (c: { id: string; title: string; messages: number }) => {
    try {
      await api.deleteConversation(c.id)
      showToast(`已删除对话「${c.title}」及其 ${c.messages} 条消息与过程事件`)
      setTick((t) => t + 1)
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }
  const delProject = async (p: { id: string; name: string; conversations: number }) => {
    try {
      await api.deleteProject(p.id)
      showToast(`已删除项目「${p.name}」及其 ${p.conversations} 个对话（含消息与事件）`)
      setTick((t) => t + 1)
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  const fmtBytes = (n: number) => (n >= 1 << 20 ? `${(n / 1024 / 1024).toFixed(1)} MB` : `${(n / 1024).toFixed(1)} KB`)
  const s = data?.stats

  return (
    <>
      <div className="settings-head">
        <Typography.Title level={5} style={{ marginTop: 0, marginBottom: 4 }}>数据与安全</Typography.Title>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
          全部数据存于本机 SQLite 单文件（密钥 AES-256-GCM 加密）；删除对话/项目会级联删除其下数据，操作前请确认级联范围。API Key 只能整库管理，无单独导出。
        </Typography.Paragraph>
      </div>
      <div className="usage-summary" style={{ marginTop: 12 }}>
        {[
          { label: 'DB 体积', value: data ? fmtBytes(data.db_bytes) : '—' },
          { label: '会话', value: s?.conversations },
          { label: '消息', value: s?.messages },
          { label: '过程事件', value: s?.run_events },
          { label: '智能体', value: s?.agents },
          { label: '项目', value: s?.projects },
          { label: '技能', value: s?.skills },
          { label: '知识库', value: s?.knowledge_bases },
          { label: '模型连接', value: s?.model_conns },
        ].map((it) => (
          <div key={it.label} className="usage-stat">
            <div className="usage-stat-label">{it.label}</div>
            <div className="usage-stat-value">{typeof it.value === 'number' ? fmtNum(it.value) : (it.value ?? '—')}</div>
          </div>
        ))}
      </div>

      <Typography.Title level={5} style={{ marginTop: 20, marginBottom: 8 }}>项目管理（删除级联其全部对话）</Typography.Title>
      <Table
        rowKey="id"
        size="small"
        loading={loading}
        pagination={false}
        dataSource={data?.projects ?? []}
        columns={[
          { title: '项目', dataIndex: 'name' },
          { title: '会话数', dataIndex: 'conversations', width: 100, render: (v: number) => fmtNum(v) },
          {
            title: '操作', width: 100,
            render: (_: unknown, p) => (
              <Popconfirm
                title={`删除项目「${p.name}」？`}
                description={`将级联删除其下全部 ${p.conversations} 个对话（含消息与过程事件），不可恢复。`}
                okText="删除" okButtonProps={{ danger: true }} cancelText="取消"
                onConfirm={() => delProject(p)}
              >
                <Button size="small" danger>删除</Button>
              </Popconfirm>
            ),
          },
        ]}
        locale={{ emptyText: '暂无项目' }}
      />

      <Typography.Title level={5} style={{ marginTop: 20, marginBottom: 8 }}>会话管理（删除级联其消息与事件）</Typography.Title>
      <Table
        rowKey="id"
        size="small"
        loading={loading}
        pagination={{ pageSize: 10 }}
        dataSource={data?.conversations ?? []}
        columns={[
          { title: '标题', dataIndex: 'title', ellipsis: true },
          { title: '归属', dataIndex: 'scope', width: 90, render: (v: string) => (v === 'agent' ? <Tag color="blue" style={{ margin: 0 }}>智能体</Tag> : <Tag color="purple" style={{ margin: 0 }}>项目</Tag>) },
          { title: '消息数', dataIndex: 'messages', width: 90, render: (v: number) => fmtNum(v) },
          { title: '更新时间', dataIndex: 'updated_at', width: 180, render: (v: string) => (v || '').replace('T', ' ').slice(0, 16) },
          {
            title: '操作', width: 100,
            render: (_: unknown, c) => (
              <Popconfirm
                title={`删除对话「${c.title}」？`}
                description={`将级联删除其 ${c.messages} 条消息与全部过程事件，不可恢复。`}
                okText="删除" okButtonProps={{ danger: true }} cancelText="取消"
                onConfirm={() => delConv(c)}
              >
                <Button size="small" danger>删除</Button>
              </Popconfirm>
            ),
          },
        ]}
        locale={{ emptyText: '暂无会话' }}
      />
    </>
  )
}
