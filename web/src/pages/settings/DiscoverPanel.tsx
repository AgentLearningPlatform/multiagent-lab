/**
 * 自动发现面板（REQ-172 自 SettingsPage 抽取；挂在提供商展开区）：
 * - 拉取 `POST /api/model-connections/{anchorId}/list-models`（REQ-48）；
 *   anthropic 提供商由后端走 /v1/models（x-api-key），openai_compat 走 GET {base_url}/models；
 * - 成功：多选清单（已添加项置灰）→ 批量创建连接（copy_key_from 锚点，名称按 `{提供商}·{模型}`）；
 * - 失败 / 404：提示「自动发现接口未就绪，可手动添加」，手动添加仍可用；
 * - anthropic 分组类型锁定 chat（Anthropic 无官方向量接口）。
 */
import { useEffect, useState } from 'react'
import { Alert, Button, Checkbox, Segmented, Spin, Tag } from 'antd'
import { api } from '../../api/client'
import type { ModelConnection } from '../../api/types'
import { useUI } from '../../store/ui'
import { isAnthropicProtocol, uniqueConnName, type ProviderGroup } from './grouping'

export function DiscoverPanel({ group, conns, onManualAdd, onClose, onAdded }: {
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
  const anthropic = isAnthropicProtocol(group.protocol)

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
          conn_type: anthropic ? 'chat' : connType, // anthropic 无向量接口，强制 chat
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
          title="自动发现接口未就绪，可手动添加"
          description={`${error}（POST /api/model-connections/{id}/list-models；anthropic 网关若未实现 /v1/models 请手动添加）`}
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
      {new Set(group.members.map((m) => m.protocol)).size > 1 && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 8 }}
          title="组内含多协议连接（REQ-177）：自动发现按锚点接入点协议执行；其他协议的模型请「手动添加」后按连接调整协议与 Base URL。"
        />
      )}
      <div className="discover-toolbar">
        <span className="discover-label">类型</span>
        <Segmented
          size="small"
          value={anthropic ? 'chat' : connType}
          onChange={(v) => setConnType(v as 'chat' | 'embedding')}
          options={[
            { label: 'chat', value: 'chat' },
            { label: 'embedding', value: 'embedding', disabled: anthropic },
          ]}
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
