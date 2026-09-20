import { useEffect, useState } from 'react'
import { Alert, Button, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { api } from '../api/client'
import type { ModelConnection } from '../api/types'
import { useUI } from '../store/ui'

/** 设置页：模型连接管理（M1）——CRUD / 测试 / 默认 / 启停 */
export default function SettingsPage() {
  const { showToast } = useUI()
  const [conns, setConns] = useState<ModelConnection[]>([])
  const [editing, setEditing] = useState<ModelConnection | 'new' | null>(null)
  const [testing, setTesting] = useState<string | null>(null)

  const reload = () => { api.listConnections().then(setConns).catch(() => setConns([])) }
  useEffect(reload, [])

  const hasChat = conns.some((c) => c.conn_type === 'chat' && c.has_key && c.enabled)

  const toggleEnabled = async (c: ModelConnection) => {
    try {
      await api.updateConnection(c.id, { ...c, enabled: !c.enabled })
      reload()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
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

  const columns: ColumnsType<ModelConnection> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (_, c) => (
        <Space size={6}>
          <Typography.Text strong>{c.name}</Typography.Text>
          {c.is_default && <Tag color="purple">默认</Tag>}
        </Space>
      ),
    },
    { title: '类型', dataIndex: 'conn_type', width: 80, render: (t: string) => (t === 'chat' ? '对话' : '向量') },
    { title: 'Base URL', dataIndex: 'base_url', render: (v: string) => <Typography.Text code style={{ fontSize: 12 }}>{v}</Typography.Text> },
    { title: '模型', dataIndex: 'model_name', render: (v: string) => <Typography.Text code style={{ fontSize: 12 }}>{v}</Typography.Text> },
    { title: 'API Key', dataIndex: 'has_key', render: (_, c) => <Typography.Text type={c.has_key ? 'secondary' : 'warning'} style={{ fontSize: 12 }}>{c.has_key ? c.api_key_hint : '未设置'}</Typography.Text> },
    { title: '状态', dataIndex: 'enabled', width: 80, render: (on: boolean) => <Tag color={on ? 'green' : 'default'}>{on ? '启用' : '停用'}</Tag> },
    {
      title: '操作',
      key: 'ops',
      width: 300,
      render: (_, c) => (
        <Space size={0} wrap>
          <Button type="link" size="small" loading={testing === c.id} onClick={() => test(c)}>测试</Button>
          {c.is_default ? (
            <Tag style={{ margin: 0 }}>默认</Tag>
          ) : (
            <Button type="link" size="small" onClick={() => setDefault(c)}>设默认</Button>
          )}
          <Button type="link" size="small" onClick={() => toggleEnabled(c)}>{c.enabled ? '停用' : '启用'}</Button>
          <Button type="link" size="small" onClick={() => setEditing(c)}>编辑</Button>
          <Popconfirm title={`删除连接「${c.name}」？`} okText="删除" okButtonProps={{ danger: true }} cancelText="取消" onConfirm={() => remove(c)}>
            <Button type="link" size="small" danger>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div className="page">
      <Typography.Title level={4} style={{ marginTop: 0 }}>设置 · 模型连接</Typography.Title>
      <Typography.Paragraph type="secondary">
        通过 OpenAI 兼容协议接入对话/向量模型；API Key 使用 AES-256-GCM 加密存储于本地（密钥文件 data/.secret）。
      </Typography.Paragraph>

      {!hasChat && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          message="尚未配置可用的对话模型"
          description="预置了「DeepSeek（预置）」连接：填入 API Key 并启用、设为默认，即可开始对话。"
        />
      )}

      <Button type="primary" style={{ marginBottom: 16 }} onClick={() => setEditing('new')}>
        ＋ 新建连接
      </Button>

      <Table<ModelConnection>
        rowKey="id"
        columns={columns}
        dataSource={conns}
        pagination={false}
        size="middle"
        locale={{ emptyText: '暂无连接' }}
      />

      {editing && (
        <ConnModal
          conn={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => { setEditing(null); reload() }}
        />
      )}
    </div>
  )
}

function ConnModal({ conn, onClose, onSaved }: { conn: ModelConnection | null; onClose: () => void; onSaved: () => void }) {
  const { showToast } = useUI()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    form.setFieldsValue(
      conn ?? { name: '', conn_type: 'chat', base_url: 'https://api.deepseek.com/v1', model_name: 'deepseek-chat', api_key: '' },
    )
  }, [conn, form])

  const save = async () => {
    let v: any
    try {
      v = await form.validateFields()
    } catch {
      return
    }
    setBusy(true)
    try {
      if (conn) await api.updateConnection(conn.id, { ...conn, ...v })
      else await api.createConnection(v)
      showToast('已保存')
      onSaved()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const testNow = async () => {
    const v = form.getFieldsValue()
    setBusy(true)
    try {
      const payload = conn
        ? { id: conn.id }
        : { conn_type: v.conn_type, base_url: v.base_url, model_name: v.model_name, api_key: v.api_key }
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
      title={conn ? '编辑连接' : '新建连接'}
      onCancel={onClose}
      width={520}
      footer={
        <Space>
          <Button onClick={testNow} disabled={busy}>先测试</Button>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={busy} onClick={save}>保存</Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        <Space size={12} style={{ display: 'flex' }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '名称必填' }]} style={{ flex: 1 }}>
            <Input placeholder="DeepSeek 官方" />
          </Form.Item>
          <Form.Item name="conn_type" label="类型" initialValue="chat" style={{ width: 150 }}>
            <Select
              options={[
                { value: 'chat', label: '对话模型' },
                { value: 'embedding', label: '向量模型' },
              ]}
            />
          </Form.Item>
        </Space>
        <Form.Item name="base_url" label="Base URL（OpenAI 兼容）" rules={[{ required: true, message: 'Base URL 必填' }]}>
          <Input placeholder="https://api.deepseek.com/v1" />
        </Form.Item>
        <Space size={12} style={{ display: 'flex' }}>
          <Form.Item name="model_name" label="模型名" rules={[{ required: true, message: '模型名必填' }]} style={{ flex: 1 }}>
            <Input placeholder="deepseek-chat" />
          </Form.Item>
          <Form.Item
            name="api_key"
            label={conn?.has_key ? <span>API Key <Tag color="green" style={{ marginInlineStart: 6 }}>已存 {conn.api_key_hint}</Tag></span> : 'API Key'}
            style={{ flex: 1 }}
            extra="保存后 AES-256-GCM 加密，仅显示掩码"
          >
            <Input.Password placeholder={conn?.has_key ? '不修改请留空' : 'sk-…'} autoComplete="new-password" />
          </Form.Item>
        </Space>
      </Form>
    </Modal>
  )
}
