import { useEffect, useState } from 'react'
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
    if (!confirm(`删除连接「${c.name}」？`)) return
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

  return (
    <div className="page">
      <h2>设置 · 模型连接</h2>
      <div className="sub">
        通过 OpenAI 兼容协议接入对话/向量模型；API Key 使用 AES-256-GCM 加密存储于本地（密钥文件 data/.secret）。
      </div>

      {!hasChat && (
        <div className="placeholder" style={{ marginBottom: 16, textAlign: 'left' }}>
          <b>⚠ 尚未配置可用的对话模型</b>
          <p style={{ margin: '6px 0 0' }}>
            预置了「DeepSeek（预置）」连接：填入 API Key 并启用、设为默认，即可开始对话。
          </p>
        </div>
      )}

      <button className="btn-primary" style={{ marginBottom: 16 }} onClick={() => setEditing('new')}>
        ＋ 新建连接
      </button>

      <table className="tbl">
        <thead>
          <tr>
            <th>名称</th>
            <th>类型</th>
            <th>Base URL</th>
            <th>模型</th>
            <th>API Key</th>
            <th>状态</th>
            <th style={{ width: 240 }}>操作</th>
          </tr>
        </thead>
        <tbody>
          {conns.map((c) => (
            <tr key={c.id}>
              <td>
                <b>{c.name}</b>
                {c.is_default && <span className="badge" style={{ marginLeft: 8 }}>默认</span>}
              </td>
              <td>{c.conn_type === 'chat' ? '对话' : '向量'}</td>
              <td className="mono">{c.base_url}</td>
              <td className="mono">{c.model_name}</td>
              <td className="mono">{c.has_key ? c.api_key_hint : '未设置'}</td>
              <td>
                <span className={`badge ${c.enabled ? 'ok' : 'gray'}`}>{c.enabled ? '启用' : '停用'}</span>
              </td>
              <td>
                <button className="btn-ghost" style={{ marginRight: 6 }} onClick={() => test(c)} disabled={testing === c.id}>
                  {testing === c.id ? '测试中…' : '测试'}
                </button>
                {c.is_default ? (
                  <span className="badge gray" style={{ marginRight: 6 }}>默认</span>
                ) : (
                  <button className="btn-ghost" style={{ marginRight: 6 }} onClick={() => setDefault(c)}>设默认</button>
                )}
                <button className="btn-ghost" style={{ marginRight: 6 }} onClick={() => toggleEnabled(c)}>
                  {c.enabled ? '停用' : '启用'}
                </button>
                <button className="btn-ghost" style={{ marginRight: 6 }} onClick={() => setEditing(c)}>编辑</button>
                <button className="btn-danger-ghost" onClick={() => remove(c)}>删除</button>
              </td>
            </tr>
          ))}
          {conns.length === 0 && (
            <tr>
              <td colSpan={7} style={{ textAlign: 'center', color: 'var(--c-ink-3)' }}>暂无连接</td>
            </tr>
          )}
        </tbody>
      </table>

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
  const [form, setForm] = useState<Partial<ModelConnection> & { api_key?: string }>(
    conn ?? { name: '', conn_type: 'chat', base_url: 'https://api.deepseek.com/v1', model_name: 'deepseek-chat', api_key: '' },
  )
  const [busy, setBusy] = useState(false)
  const set = (k: string, v: any) => setForm((f) => ({ ...f, [k]: v }))

  // Esc 关闭弹窗（原型 06 §5 dialog 约定）
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onClose])

  const save = async () => {
    if (!form.name?.trim() || !form.base_url?.trim() || !form.model_name?.trim()) {
      showToast('名称、Base URL、模型必填', 'err')
      return
    }
    setBusy(true)
    try {
      if (conn) await api.updateConnection(conn.id, form)
      else await api.createConnection(form)
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
      const payload = conn
        ? { id: conn.id }
        : { conn_type: form.conn_type, base_url: form.base_url, model_name: form.model_name, api_key: form.api_key }
      const r = await api.testConnection(payload)
      showToast(r.ok ? `连接成功（${r.elapsed_ms}ms）` : `失败：${r.error}`, r.ok ? 'ok' : 'err')
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="modal-mask" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h3>{conn ? '编辑连接' : '新建连接'}</h3>
        <div className="row2">
          <div className="field">
            <label>名称</label>
            <input value={form.name ?? ''} onChange={(e) => set('name', e.target.value)} placeholder="DeepSeek 官方" />
          </div>
          <div className="field">
            <label>类型</label>
            <select value={form.conn_type ?? 'chat'} onChange={(e) => set('conn_type', e.target.value)}>
              <option value="chat">对话模型</option>
              <option value="embedding">向量模型</option>
            </select>
          </div>
        </div>
        <div className="field">
          <label>Base URL（OpenAI 兼容）</label>
          <input value={form.base_url ?? ''} onChange={(e) => set('base_url', e.target.value)} placeholder="https://api.deepseek.com/v1" />
        </div>
        <div className="row2">
          <div className="field">
            <label>模型名</label>
            <input value={form.model_name ?? ''} onChange={(e) => set('model_name', e.target.value)} placeholder="deepseek-chat" />
          </div>
          <div className="field">
            <label>API Key {conn?.has_key && <span className="badge ok" style={{ marginLeft: 6 }}>已存 {conn.api_key_hint}</span>}</label>
            <input
              type="password"
              value={form.api_key ?? ''}
              onChange={(e) => set('api_key', e.target.value)}
              placeholder={conn?.has_key ? '不修改请留空' : 'sk-…'}
            />
            <div className="hint">保存后 AES-256-GCM 加密，仅显示掩码</div>
          </div>
        </div>
        <div className="actions">
          <button className="btn-primary" onClick={save} disabled={busy}>保存</button>
          <button className="btn-ghost" onClick={testNow} disabled={busy}>先测试</button>
          <button className="btn-ghost" onClick={onClose}>取消</button>
        </div>
      </div>
    </div>
  )
}
