/**
 * 设置页「连接器」分区（REQ-214/M46）：
 * 外部连接器统一管理面（与「模型管理」平级）——连接器 = 连接对象 × 交付驱动两轴的产品抽象：
 * - 自定义 MCP 连接器：外部 MCP server URL 直通（兼容存量挂载与 oo 预设）；
 * - Kubernetes 连接器：集群 + kubeconfig 凭据（凭据可路径引用或加密内容二选一）；
 * - SSH 连接器：主机 + 私钥/口令凭据。
 * K8s/SSH 由平台托管自研 Go MCP 插件服务承载（进程内嵌 loopback 端口），凭据服务端
 * 绑定（AES-256-GCM）——不进 LLM 上下文、不进工具参数。侧板勾选授权（连接白名单），
 * 本分区只做连接器的生命周期管理与连接测试（REQ-191 先例）。
 */
import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Empty, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Spin, Switch, Table, Tag, Tooltip, Typography } from 'antd'
import { ApiOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { api } from '../../api/client'
import type { Connector } from '../../api/types'
import { useUI } from '../../store/ui'

const KIND_META: { value: Connector['kind']; label: string; color: string; desc: string }[] = [
  { value: 'mcp', label: '自定义 MCP', color: 'blue', desc: '外部 MCP server Streamable HTTP 直通' },
  { value: 'kubernetes', label: 'Kubernetes', color: 'purple', desc: '集群连接（kubectl CLI 包装，凭据服务端绑定）' },
  { value: 'ssh', label: 'SSH', color: 'cyan', desc: '远程主机命令执行（每次调用独立连接）' },
]
const kindMeta = (k: string) => KIND_META.find((x) => x.value === k) ?? KIND_META[0]

/** config 摘要（非敏感字段展示） */
function configSummary(c: Connector): string {
  if (c.kind === 'mcp') return String(c.config?.url ?? '—')
  if (c.kind === 'kubernetes') {
    const parts = [c.config?.kubeconfig_path, c.config?.context, c.config?.namespace].filter(Boolean)
    return parts.length ? parts.join(' · ') : (c.has_credentials ? '凭据态 kubeconfig' : '—')
  }
  const host = c.config?.host ?? '—'
  const user = c.config?.user ? `${c.config.user}@` : ''
  const port = c.config?.port ? `:${c.config.port}` : ''
  return `${user}${host}${port}`
}

export function ConnectorsPane() {
  const { showToast, bumpData } = useUI()
  const [connectors, setConnectors] = useState<Connector[] | null>(null)
  const [modal, setModal] = useState<{ open: boolean; editing?: Connector }>({ open: false })
  const [testing, setTesting] = useState<string | null>(null)
  // REQ-214 P2④：Modal 内「测试连接（不保存）」预检结果
  const [preview, setPreview] = useState<{ ok: boolean; detail: string; tools: string[] } | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [form] = Form.useForm()
  const kindWatched = (Form.useWatch('kind', form) ?? 'mcp') as Connector['kind']

  const load = useCallback(() => {
    api
      .listConnectors()
      .then((r) => setConnectors(r.connectors ?? []))
      .catch((e) => {
        setConnectors([])
        showToast(`加载连接器失败：${e.message}`, 'err')
      })
  }, [showToast])

  useEffect(() => {
    load()
  }, [load])

  const openCreate = () => {
    form.resetFields()
    form.setFieldsValue({ kind: 'mcp' })
    setPreview(null)
    setModal({ open: true })
  }
  const openEdit = (c: Connector) => {
    form.resetFields()
    form.setFieldsValue({
      kind: c.kind,
      name: c.name,
      description: c.description,
      url: c.config?.url ?? '',
      kubeconfig_path: c.config?.kubeconfig_path ?? '',
      context: c.config?.context ?? '',
      namespace: c.config?.namespace ?? '',
      host: c.config?.host ?? '',
      port: c.config?.port ?? undefined,
      user: c.config?.user ?? '',
      read_only: c.config?.read_only === true,
      // 凭据永不回填——占位提示「已加密存储」
    })
    setPreview(null)
    setModal({ open: true, editing: c })
  }

  const submit = async () => {
    try {
      const v = await form.validateFields()
      const kind = v.kind as Connector['kind']
      const config: Record<string, unknown> = {}
      const credentials: Record<string, unknown> = {}
      if (kind === 'mcp') config.url = (v.url ?? '').trim()
      if (kind === 'kubernetes') {
        if ((v.kubeconfig_path ?? '').trim()) config.kubeconfig_path = (v.kubeconfig_path ?? '').trim()
        if ((v.context ?? '').trim()) config.context = (v.context ?? '').trim()
        if ((v.namespace ?? '').trim()) config.namespace = (v.namespace ?? '').trim()
        config.read_only = v.read_only === true // REQ-214 P2⑦：只读=插件服务不注册 apply
        if ((v.kubeconfig ?? '').trim()) credentials.kubeconfig = v.kubeconfig
      }
      if (kind === 'ssh') {
        config.host = (v.host ?? '').trim()
        if (v.port) config.port = String(v.port)
        if ((v.user ?? '').trim()) config.user = (v.user ?? '').trim()
        if ((v.password ?? '').trim()) credentials.password = v.password
        if ((v.private_key ?? '').trim()) credentials.private_key = v.private_key
        if ((v.passphrase ?? '').trim()) credentials.passphrase = v.passphrase
      }
      if (kind === 'mcp' && (v.headers_json ?? '').trim()) {
        // REQ-214 P2③：认证头（JSON 对象，加密进 credentials.headers；如 {"Authorization": "Bearer …"}）
        try {
          const parsed = JSON.parse(v.headers_json)
          if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
            credentials.headers = parsed
          }
        } catch {
          showToast('认证头须为合法 JSON 对象', 'err')
          return
        }
      }
      if (modal.editing) {
        const payload: Record<string, unknown> = { name: v.name, description: v.description ?? '', config }
        if (Object.keys(credentials).length > 0) payload.credentials = credentials // 空 = 保留原凭据
        await api.updateConnector(modal.editing.id, payload)
        showToast('连接器已更新')
      } else {
        await api.createConnector({
          kind, name: v.name, description: v.description ?? '', config,
          ...(Object.keys(credentials).length > 0 ? { credentials } : {}),
        })
        showToast('连接器已创建——可在智能体侧板勾选授权')
      }
      setModal({ open: false })
      setPreview(null)
      load()
      bumpData()
    } catch (e: any) {
      if (e?.errorFields) return
      showToast(e.message, 'err')
    }
  }

  const runTest = async (c: Connector) => {
    setTesting(c.id)
    try {
      const r = await api.testConnector(c.id)
      showToast(`${c.name}：${r.detail}`, r.ok ? 'ok' : 'err')
      load()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setTesting(null)
    }
  }

  // REQ-214 P2④：Modal 内预检（不落库）——创建前先测通
  const runPreview = async () => {
    try {
      const v = await form.validateFields()
      const kind = v.kind as Connector['kind']
      const config: Record<string, unknown> = {}
      const credentials: Record<string, unknown> = {}
      if (kind === 'mcp') {
        config.url = (v.url ?? '').trim()
        if ((v.headers_json ?? '').trim()) {
          try {
            const parsed = JSON.parse(v.headers_json)
            if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) credentials.headers = parsed
          } catch {
            showToast('认证头须为合法 JSON 对象', 'err')
            return
          }
        }
      }
      if (kind === 'kubernetes') {
        if ((v.kubeconfig_path ?? '').trim()) config.kubeconfig_path = (v.kubeconfig_path ?? '').trim()
        if ((v.context ?? '').trim()) config.context = (v.context ?? '').trim()
        if ((v.namespace ?? '').trim()) config.namespace = (v.namespace ?? '').trim()
        config.read_only = v.read_only === true
        if ((v.kubeconfig ?? '').trim()) credentials.kubeconfig = v.kubeconfig
      }
      if (kind === 'ssh') {
        config.host = (v.host ?? '').trim()
        if (v.port) config.port = String(v.port)
        if ((v.user ?? '').trim()) config.user = (v.user ?? '').trim()
        if ((v.password ?? '').trim()) credentials.password = v.password
        if ((v.private_key ?? '').trim()) credentials.private_key = v.private_key
        if ((v.passphrase ?? '').trim()) credentials.passphrase = v.passphrase
      }
      setPreviewing(true)
      const r = await api.previewConnector({ kind, name: v.name || 'preview', config, credentials })
      setPreview(r)
      load()
    } catch (e: any) {
      if (e?.errorFields) return
      showToast(e.message, 'err')
    } finally {
      setPreviewing(false)
    }
  }

  const doDelete = async (c: Connector) => {
    try {
      await api.deleteConnector(c.id)
      showToast('连接器已删除')
      load()
      bumpData()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  if (connectors === null) {
    return <div style={{ padding: 40, textAlign: 'center' }}><Spin /></div>
  }

  return (
    <>
      <div className="settings-head">
        <Typography.Title level={5} style={{ marginTop: 0, marginBottom: 4 }}>连接器（REQ-214）</Typography.Title>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
          统一 agent 连接外部能力的产品抽象：连接对象（自定义 MCP / Kubernetes / SSH）× 交付驱动（MCP 直通 / 平台托管插件服务）。
          凭据服务端加密绑定，不进模型上下文、不进工具参数；智能体侧板勾选授权（连接白名单）。工具以 <code>{'{连接器名}__{tool}'}</code> 前缀并入白名单候选。
        </Typography.Paragraph>
      </div>
      <Space style={{ margin: '12px 0' }}>
        <Button type="primary" size="small" icon={<PlusOutlined />} onClick={openCreate}>新建连接器</Button>
        <Button size="small" icon={<ReloadOutlined />} onClick={load}>刷新</Button>
      </Space>
      <Table
        size="small"
        rowKey="id"
        dataSource={connectors}
        pagination={false}
        scroll={{ x: 860 }} /* REQ-237 F15：横向滚动兜底 */
        expandable={{
          // REQ-214 P2①：工具清单预览（授权前知道将得到什么工具；未测试过则不可展开）
          rowExpandable: (c) => (c.tools?.length ?? 0) > 0,
          expandedRowRender: (c) => (
            <div style={{ padding: '4px 0' }}>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>授权本连接器后，模型将获得以下工具（<code>{'{连接器名}__{工具名}'}</code> 前缀）：</Typography.Text>
              <div style={{ marginTop: 6, display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                {(c.tools ?? []).map((t) => <Tag key={t} style={{ margin: 0 }}><code style={{ fontSize: 11 }}>{t}</code></Tag>)}
              </div>
            </div>
          ),
        }}
        locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无连接器——点「新建连接器」创建；智能体侧板勾选授权后生效" /> }}
        columns={[
          {
            title: '名称', key: 'name', render: (_, c) => (
              <span>
                <Typography.Text strong style={{ fontSize: 13 }}>{c.name}</Typography.Text>
                {c.is_builtin && <Tag style={{ marginInlineStart: 6 }}>内置</Tag>}
              </span>
            ),
          },
          {
            title: '类型', key: 'kind', width: 120, render: (_, c) => {
              const m = kindMeta(c.kind)
              return <Tooltip title={m.desc}><Tag color={m.color} style={{ margin: 0 }}>{m.label}</Tag></Tooltip>
            },
          },
          { title: '连接目标', key: 'target', render: (_, c) => <Typography.Text code style={{ fontSize: 12 }}>{configSummary(c)}</Typography.Text> },
          {
            title: '凭据', key: 'cred', width: 80, align: 'center', render: (_, c) =>
              c.has_credentials ? <Tag color="green" style={{ margin: 0 }}>已加密</Tag> : <Tag style={{ margin: 0 }}>无</Tag>,
          },
          {
            title: '状态', key: 'status', width: 90, render: (_, c) => {
              // REQ-214 P2⑤：状态时效——Tooltip 带最近测试时间（状态为时点快照不自动刷新）
              const tested = c.tested_at ? new Date(c.tested_at).toLocaleString() : ''
              const tip = c.status === 'unknown' ? '未测试' : `${c.status_detail || ''}${tested ? `（${tested} 测试）` : ''}`
              return (
                <Tooltip title={tip}>
                  <Tag color={c.status === 'ok' ? 'green' : c.status === 'error' ? 'red' : 'default'} style={{ margin: 0 }}>
                    {c.status === 'ok' ? '可达' : c.status === 'error' ? '不可达' : '未测试'}
                  </Tag>
                </Tooltip>
              )
            },
          },
          {
            title: '被引用', key: 'refs', width: 140, render: (_, c) =>
              c.refs.length ? <Typography.Text type="secondary" style={{ fontSize: 12 }}>{c.refs.join('、')}</Typography.Text> : <Typography.Text type="secondary">—</Typography.Text>,
          },
          {
            title: '操作', key: 'ops', width: 190, render: (_, c) => (
              <Space size={4}>
                <Button size="small" icon={<ApiOutlined />} loading={testing === c.id} onClick={() => runTest(c)}>测试</Button>
                <Button size="small" onClick={() => openEdit(c)} disabled={c.is_builtin}>编辑</Button>
                {c.is_builtin ? (
                  <Tooltip title="内置连接器不可删除"><Button size="small" disabled>删除</Button></Tooltip>
                ) : (
                  <Popconfirm
                    title="删除连接器？"
                    description={c.refs.length ? `仍被 ${c.refs.join('、')} 引用，须先取消授权` : '删除后引用它的智能体装配时将告警降级'}
                    onConfirm={() => doDelete(c)}
                  >
                    <Button size="small" danger>删除</Button>
                  </Popconfirm>
                )}
              </Space>
            ),
          },
        ]}
      />
      <Alert
        style={{ marginTop: 12 }}
        type="info"
        showIcon
        title="安全边界：凭据仅在创建/编辑时提交一次（AES-256-GCM 加密落库），平台不回传明文；K8s/SSH 连接器由平台托管插件服务承载，工具调用仍受智能体「工具调用人工审批」约束。"
      />

      <Modal
        title={modal.editing ? `编辑连接器 · ${modal.editing.name}` : '新建连接器'}
        open={modal.open}
        onCancel={() => setModal({ open: false })}
        onOk={submit}
        okText="保存"
        cancelText="取消"
        width={520}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" requiredMark={false} size="small">
          <Space.Compact block style={{ marginBottom: 0 }}>
            <Form.Item name="kind" label="类型" style={{ width: 160 }} extra={kindMeta(kindWatched).desc}>
              <Select
                disabled={!!modal.editing}
                options={KIND_META.map((k) => ({ value: k.value, label: k.label }))}
              />
            </Form.Item>
            <Form.Item
              name="name"
              label="实例名"
              style={{ flex: 1, marginInlineStart: 8 }}
              rules={[
                { required: true, message: '实例名必填' },
                { pattern: /^[a-zA-Z0-9_-]+$/, message: '字母/数字/下划线/连字符（装配前缀槽位）' },
              ]}
              extra="工具前缀 {实例名}__{tool}；同类多实例天然区分"
            >
              <Input placeholder="如 prod-k8s / ops-ssh" disabled={!!modal.editing} />
            </Form.Item>
          </Space.Compact>
          <Form.Item name="description" label="描述">
            <Input placeholder="一句话说明（可选）" />
          </Form.Item>

          {kindWatched === 'mcp' && (
            <>
              <Form.Item
                name="url"
                label="MCP 端点 URL"
                rules={[{ required: true, message: 'URL 必填' }, { pattern: /^https?:\/\//, message: '须为 http(s) URL（Streamable HTTP MCP）' }]}
                extra="如 http://127.0.0.1:8092/mcp；指向本平台 /mcp 属自引用会被装配拒绝"
              >
                <Input placeholder="http://127.0.0.1:8092/mcp" />
              </Form.Item>
              <Form.Item
                name="headers_json"
                label="认证头（凭据，可选）"
                extra={modal.editing?.has_credentials ? '已加密存储，留空保持不变' : '需鉴权的托管 MCP 服务填写，如 {"Authorization": "Bearer sk-…"}（JSON 对象；加密落库，请求时注入）'}
              >
                <Input.TextArea rows={2} placeholder='{"Authorization": "Bearer …"}' />
              </Form.Item>
            </>
          )}

          {kindWatched === 'kubernetes' && (
            <>
              <Form.Item name="kubeconfig_path" label="kubeconfig 路径" extra="与下方凭据内容二选一；路径引用（REQ-191 同口径，如 ~/.kube/config）">
                <Input placeholder="/home/user/.kube/config" />
              </Form.Item>
              <Space.Compact block>
                <Form.Item name="context" label="context" style={{ width: '50%', paddingInlineEnd: 8 }}>
                  <Input placeholder="空 = 当前 context" />
                </Form.Item>
                <Form.Item name="namespace" label="缺省命名空间" style={{ width: '50%' }}>
                  <Input placeholder="空 = kubeconfig 默认" />
                </Form.Item>
              </Space.Compact>
              <Form.Item
                name="read_only"
                label="只读模式"
                valuePropName="checked"
                tooltip="开启后不注册 kubectl_apply 写工具（模型只能 get/describe/logs）——工具级白名单的最轻形态"
                extra="生产集群建议开启；需要写操作时关闭并配合「工具调用人工审批」使用"
              >
                <Switch size="small" />
              </Form.Item>
              <Form.Item
                name="kubeconfig"
                label="kubeconfig 内容（凭据）"
                extra={modal.editing?.has_credentials ? '已加密存储，留空保持不变' : '可选——粘贴 kubeconfig 文件内容（加密落库，永不上模型上下文）'}
              >
                <Input.TextArea rows={4} placeholder="apiVersion: v1&#10;kind: Config…" />
              </Form.Item>
            </>
          )}

          {kindWatched === 'ssh' && (
            <>
              <Space.Compact block>
                <Form.Item
                  name="host" label="主机" style={{ width: '45%', paddingInlineEnd: 8 }}
                  rules={[{ required: true, message: '主机必填' }]}
                >
                  <Input placeholder="10.0.0.5" />
                </Form.Item>
                <Form.Item name="port" label="端口" style={{ width: '20%', paddingInlineEnd: 8 }}>
                  <InputNumber min={1} max={65535} style={{ width: '100%' }} placeholder="22" />
                </Form.Item>
                <Form.Item name="user" label="用户" style={{ width: '35%' }}>
                  <Input placeholder="root（默认）" />
                </Form.Item>
              </Space.Compact>
              <Form.Item name="password" label="口令（凭据）" extra={modal.editing?.has_credentials ? '已加密存储，留空保持不变' : '与私钥二选一；加密落库'}>
                <Input.Password placeholder="••••••••" autoComplete="new-password" />
              </Form.Item>
              <Form.Item name="private_key" label="私钥（凭据）" extra={modal.editing?.has_credentials ? '已加密存储，留空保持不变' : 'PEM 内容；加密落库，永不上模型上下文'}>
                <Input.TextArea rows={4} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" />
              </Form.Item>
              <Form.Item name="passphrase" label="私钥口令（凭据）">
                <Input.Password placeholder="私钥无口令则留空" autoComplete="new-password" />
              </Form.Item>
            </>
          )}
        </Form>
        <div style={{ borderTop: '1px solid var(--c-border, #f0f0f0)', paddingTop: 10, marginTop: 4 }}>
          <Space size={8} align="center">
            <Button size="small" icon={<ApiOutlined />} loading={previewing} onClick={runPreview}>
              测试连接（不保存）
            </Button>
            {preview && (
              <Typography.Text type={preview.ok ? 'secondary' : 'danger'} style={{ fontSize: 12 }}>
                {preview.ok ? '✓ ' : '✗ '}{preview.detail}
              </Typography.Text>
            )}
          </Space>
          {preview?.ok && (preview.tools?.length ?? 0) > 0 && (
            <div style={{ marginTop: 6, display: 'flex', flexWrap: 'wrap', gap: 4 }}>
              {preview.tools.map((t) => <Tag key={t} style={{ margin: 0 }}><code style={{ fontSize: 11 }}>{t}</code></Tag>)}
            </div>
          )}
        </div>
      </Modal>
    </>
  )
}
