/**
 * 提供商表单（REQ-172 自 SettingsPage 抽取；REQ-177 协议重组 + 紧凑化）：
 * - 新建：后端无独立提供商实体，添加提供商将同时创建其首个模型连接——协议/Base URL/类型
 *   均为该连接自己的属性；顶部「厂商预设」快速填充（REQ-106），仅需补 API Key 与模型名；
 *   保存成功后返回新建分组 key，父级展开该行并挂自动发现面板（REQ-48）。
 * - 编辑（REQ-177①）：供应商身份只管 名称/别名/API Key/启用（批量应用，名称按约定重生成）；
 *   协议与 Base URL 是各模型连接自己的属性（同一供应商组内可混合 openai_compat 与 anthropic），
 *   在展开区「编辑模型」中逐连接维护，编辑提供商不再批量覆写。
 * - 协议联动（REQ-172）：anthropic 仅 chat（Anthropic 无官方向量接口）。
 */
import { useEffect, useState } from 'react'
import { Alert, Button, Divider, Form, Input, Modal, Select, Space, Switch, Tag, Typography } from 'antd'
import { api } from '../../api/client'
import { PROVIDER_PRESETS } from '../../api/providerPresets'
import type { ProviderPreset } from '../../api/providerPresets'
import type { ModelConnection } from '../../api/types'
import { useUI } from '../../store/ui'
import { isAnthropicProtocol, providerOfName, uniqueConnName, type ProviderGroup } from './grouping'

const PROTOCOL_OPTIONS = [
  { value: 'openai_compat', label: 'openai_compat · OpenAI 兼容（/chat/completions）' },
  { value: 'anthropic', label: 'anthropic · Anthropic Messages（/v1/messages）' },
]

export function ProviderModal({ group, conns, onClose, onSaved }: {
  group: ProviderGroup | 'new'
  conns: ModelConnection[]
  onClose: () => void
  onSaved: (providerKey?: string) => void
}) {
  const { showToast } = useUI()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)
  const editGroup = group === 'new' ? null : group
  const keyMember = editGroup?.members.find((m) => m.has_key)
  // 选中的厂商预设（REQ-106；仅新建态）
  const [presetKey, setPresetKey] = useState<string | undefined>(undefined)
  const preset = PROVIDER_PRESETS.find((p) => p.key === presetKey)
  // REQ-172：协议驱动的表单联动（新建态首个连接的协议）
  const protocol = Form.useWatch('protocol', form)
  const anthropic = isAnthropicProtocol(protocol)

  const applyPreset = (key?: string) => {
    setPresetKey(key)
    const p: ProviderPreset | undefined = PROVIDER_PRESETS.find((x) => x.key === key)
    if (p) {
      form.setFieldsValue({ name: p.name, protocol: p.protocol ?? 'openai_compat', base_url: p.baseUrl, model_name: p.defaultModel, conn_type: p.connType })
    }
  }

  // 切到 anthropic 时类型锁定 chat；切回 openai_compat 不代选（保持用户已选值）
  useEffect(() => {
    if (anthropic && form.getFieldValue('conn_type') === 'embedding') {
      form.setFieldsValue({ conn_type: 'chat' })
    }
  }, [anthropic, form])

  useEffect(() => {
    if (editGroup) {
      // 名称（真名前缀）从锚点连接名派生；别名仅展示层——两者语义分离（REQ-148）
      form.setFieldsValue({ name: providerOfName(editGroup.anchor.name), alias: editGroup.alias, api_key: '', enabled: editGroup.members.every((m) => m.enabled) })
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
        // REQ-177①：只批量应用供应商身份字段（名称重生成/API Key/启用）；协议与 Base URL 随各连接保留
        const groupIds = new Set(editGroup.members.map((m) => m.id))
        const used = new Set(conns.filter((c) => !groupIds.has(c.id)).map((c) => c.name))
        for (const m of editGroup.members) {
          const name = uniqueConnName(v.name, m.model_name, used)
          used.add(name)
          const payload: any = { ...m, name, enabled: !!v.enabled }
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
          conn_type: anthropic ? 'chat' : v.conn_type, // anthropic 无向量接口，强制 chat
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
      // 编辑态测组内代表连接（优先取存有 Key 的成员）的已存配置；新建态测表单值（带协议）
      const rep = editGroup ? (editGroup.members.find((m) => m.has_key) ?? editGroup.anchor) : null
      const payload = rep
        ? { id: rep.id }
        : {
            conn_type: anthropic ? 'chat' : form.getFieldValue('conn_type'),
            protocol: form.getFieldValue('protocol'),
            base_url: form.getFieldValue('base_url'),
            model_name: form.getFieldValue('model_name'),
            api_key: form.getFieldValue('api_key'),
          }
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
      width={620}
      footer={
        <Space>
          <Button onClick={testNow} disabled={busy}>先测试</Button>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={busy} onClick={save}>保存</Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical" requiredMark={false} disabled={busy}>
        {!editGroup && (
          <Form.Item
            label="从厂商预设快速填充"
            style={{ marginBottom: 8 }}
            extra={
              preset?.console ? (
                <>
                  已按预设填充协议与 Base URL，仅需补 API Key 与模型名；Key 申请：
                  <Typography.Link href={preset.console} target="_blank" rel="noreferrer">{preset.console}</Typography.Link>
                </>
              ) : (
                '选择厂商自动填充协议与 Base URL（REQ-105 预设清单，含 Anthropic 系兼容网关）；也可留空手动填写'
              )
            }
          >
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="选择厂商预设（Anthropic / DeepSeek / 智谱 GLM / Kimi / 百炼 / 千帆 / 硅基流动 / MiniMax / 星火…）"
              allowClear
              value={presetKey}
              onChange={applyPreset}
              options={PROVIDER_PRESETS.map((p) => ({ value: p.key, label: `${p.name} · ${p.baseUrl}` }))}
            />
          </Form.Item>
        )}

        <Divider plain style={{ margin: '8px 0 12px' }}>基本信息</Divider>
        <div style={{ display: 'flex', gap: 12 }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '名称必填' }]} style={{ flex: 1, marginBottom: 12 }} extra={editGroup ? '改名将按「名称·模型名」重生成该提供商下全部连接名' : '作为连接名前缀（名称·模型名）'}>
            <Input placeholder="DeepSeek 官方" />
          </Form.Item>
          <Form.Item name="alias" label="显示别名（可选）" style={{ flex: 1, marginBottom: 12 }} extra="仅展示层，不改连接真名（REQ-148）">
            <Input placeholder="如：DeepSeek 工作号" />
          </Form.Item>
        </div>

        {editGroup ? (
          <>
            <Divider plain style={{ margin: '8px 0 12px' }}>批量应用</Divider>
            <div style={{ display: 'flex', gap: 12 }}>
              <Form.Item
                name="api_key"
                label={keyMember ? <span>API Key <Tag color="green" style={{ marginInlineStart: 6 }}>已存 {keyMember.api_key_hint}</Tag></span> : 'API Key'}
                extra="留空 = 各连接保留已存 Key；填写 = 应用到全部连接"
                style={{ flex: 1, marginBottom: 12 }}
              >
                <Input.Password placeholder="不修改请留空" autoComplete="new-password" />
              </Form.Item>
              <Form.Item name="enabled" label="启用" valuePropName="checked" extra="应用到全部连接" style={{ width: 140, marginBottom: 12 }}>
                <Switch checkedChildren="启用" unCheckedChildren="停用" />
              </Form.Item>
            </div>
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 4 }}
              title="协议与 Base URL 为各模型连接自己的属性（同一供应商下可混合 openai_compat 与 anthropic，REQ-177）——请在展开区逐连接「编辑」维护。"
            />
          </>
        ) : (
          <>
            <Divider plain style={{ margin: '8px 0 12px' }}>接入配置（首个连接）</Divider>
            <div style={{ display: 'flex', gap: 12 }}>
              <Form.Item
                name="protocol"
                label="协议"
                extra={anthropic ? '兼容官方与 DeepSeek / 智谱 / Kimi 等 Anthropic 端点' : '绝大多数厂商的 OpenAI 兼容端点'}
                style={{ width: 250, marginBottom: 12 }}
              >
                <Select options={PROTOCOL_OPTIONS} />
              </Form.Item>
              <Form.Item name="conn_type" label="类型" style={{ width: 170, marginBottom: 12 }} extra={anthropic ? '仅支持对话模型' : undefined}>
                <Select
                  disabled={anthropic}
                  options={[
                    { value: 'chat', label: 'chat（对话）' },
                    { value: 'embedding', label: 'embedding（向量）', disabled: anthropic },
                  ]}
                />
              </Form.Item>
            </div>
            <Form.Item
              name="base_url"
              label={anthropic ? 'Base URL（Anthropic 网关根地址）' : 'Base URL（OpenAI 兼容）'}
              rules={[{ required: true, message: 'Base URL 必填' }]}
              extra={anthropic
                ? '填网关根地址即可，平台自动拼接 /v1/messages（如 https://api.anthropic.com、https://open.bigmodel.cn/api/anthropic）'
                : '同一供应商可再次添加为独立实例（不同账号/Key）'}
              style={{ marginBottom: 12 }}
            >
              <Input placeholder={anthropic ? 'https://api.anthropic.com' : 'https://api.deepseek.com/v1'} />
            </Form.Item>
            <div style={{ display: 'flex', gap: 12 }}>
              <Form.Item name="api_key" label="API Key" extra="保存后 AES-256-GCM 加密，仅显示掩码" style={{ flex: 1, marginBottom: 12 }}>
                <Input.Password placeholder={anthropic ? 'sk-ant-…' : 'sk-…'} autoComplete="new-password" />
              </Form.Item>
              <Form.Item name="enabled" label="启用" valuePropName="checked" style={{ width: 140, marginBottom: 12 }}>
                <Switch checkedChildren="启用" unCheckedChildren="停用" />
              </Form.Item>
            </div>

            <Divider plain style={{ margin: '8px 0 12px' }}>首个模型</Divider>
            {anthropic && (
              <Alert
                type="info"
                showIcon
                style={{ marginBottom: 12 }}
                title="Anthropic 协议暂仅支持对话模型（无官方向量接口），类型已锁定为 chat"
              />
            )}
            <Form.Item name="model_name" label="首个模型名" rules={[{ required: true, message: '模型名必填' }]} style={{ marginBottom: 12 }} extra="后端无独立提供商实体，添加提供商将同时创建首个模型连接">
              <Input placeholder={anthropic ? 'claude-sonnet-4-5' : 'deepseek-chat'} />
            </Form.Item>
          </>
        )}
      </Form>
    </Modal>
  )
}
