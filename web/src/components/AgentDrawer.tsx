import { useEffect, useState } from 'react'
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
  const [form, setForm] = useState<Partial<Agent>>(agent)
  const [conns, setConns] = useState<ModelConnection[]>([])
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    setForm(agent)
    api.listConnections().then((cs) => setConns(cs.filter((c) => c.conn_type === 'chat' && c.enabled))).catch(() => {})
  }, [agent.id])

  // Esc 关闭抽屉（原型 06 §5）
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onClose])

  const set = (k: keyof Agent, v: any) => setForm((f) => ({ ...f, [k]: v }))

  const save = async () => {
    if (!form.name?.trim()) {
      showToast('名称必填', 'err')
      return
    }
    setSaving(true)
    try {
      await api.updateAgent(agent.id, {
        name: form.name,
        description: form.description ?? '',
        instruction: form.instruction ?? '',
        model_conn_id: form.model_conn_id || null,
        temperature: form.temperature ?? null,
        max_tokens: form.max_tokens ?? null,
        max_iteration: form.max_iteration ?? 25,
        runtime_backend: form.runtime_backend ?? 'inprocess',
      })
      showToast('已保存，下次运行生效')
      onChanged()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    if (!confirm(`删除智能体「${agent.name}」？其历史对话将保留。`)) return
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
    <div className="drawer">
      <h3>智能体属性</h3>
      <div className="field">
        <label>名称</label>
        <input value={form.name ?? ''} onChange={(e) => set('name', e.target.value)} />
      </div>
      <div className="field">
        <label>描述（用于多智能体协作时互相理解）</label>
        <textarea value={form.description ?? ''} onChange={(e) => set('description', e.target.value)} />
      </div>
      <div className="field">
        <label>系统提示词（Instruction）</label>
        <textarea
          style={{ minHeight: 140 }}
          value={form.instruction ?? ''}
          onChange={(e) => set('instruction', e.target.value)}
          placeholder="定义角色、能力边界、回答风格…"
        />
      </div>
      <div className="field">
        <label>模型连接</label>
        <select
          value={form.model_conn_id ?? ''}
          onChange={(e) => set('model_conn_id', e.target.value)}
        >
          <option value="">跟随默认（在设置中指定）</option>
          {conns.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name} · {c.model_name}
            </option>
          ))}
        </select>
        <div className="hint">
          没有合适的连接？到「设置-模型连接」新增。{conns.length === 0 && ' 当前无可用的 chat 连接。'}
        </div>
      </div>
      <div className="row2">
        <div className="field">
          <label>温度（0~2，留空默认）</label>
          <input
            type="number" step="0.1" min="0" max="2"
            value={form.temperature ?? ''}
            onChange={(e) => set('temperature', e.target.value === '' ? null : Number(e.target.value))}
          />
        </div>
        <div className="field">
          <label>最大回复 tokens</label>
          <input
            type="number" min="1"
            value={form.max_tokens ?? ''}
            onChange={(e) => set('max_tokens', e.target.value === '' ? null : Number(e.target.value))}
          />
        </div>
      </div>
      <div className="row2">
        <div className="field">
          <label>最大迭代次数（ReAct 上限）</label>
          <input
            type="number" min="1" max="100"
            value={form.max_iteration ?? 25}
            onChange={(e) => set('max_iteration', Number(e.target.value) || 25)}
          />
        </div>
        <div className="field">
          <label>运行后端</label>
          <input value={form.runtime_backend ?? 'inprocess'} disabled />
          <div className="hint">M2 默认 inprocess；subprocess/容器后端在 M5 开放</div>
        </div>
      </div>
      <div className="actions">
        <button className="btn-primary" onClick={save} disabled={saving}>
          {saving ? '保存中…' : '保存'}
        </button>
        <button className="btn-ghost" onClick={onClose}>关闭</button>
        <span style={{ flex: 1 }} />
        <button className="btn-danger-ghost" onClick={remove}>删除</button>
      </div>
    </div>
  )
}
