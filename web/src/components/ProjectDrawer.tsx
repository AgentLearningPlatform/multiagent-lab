import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { Agent, Project } from '../api/types'
import { useUI } from '../store/ui'

/**
 * 项目配置抽屉（原型 06：每个项目节点提供 ⚙ 配置入口）：
 * 基本信息协作模式 + 成员智能体（主智能体/成员）+ 删除。
 */
export default function ProjectDrawer({
  project,
  agents,
  onClose,
  onChanged,
  onDeleted,
}: {
  project: Project
  agents: Agent[]
  onClose: () => void
  onChanged: () => void
  onDeleted: () => void
}) {
  const { showToast, bumpData } = useUI()
  const [form, setForm] = useState<Partial<Project>>({
    name: project.name,
    description: project.description,
    collab_mode: project.collab_mode,
    workflow_mode: project.workflow_mode,
    constraints: project.constraints,
  })
  const [selected, setSelected] = useState<Record<string, 'coordinator' | 'member'>>(() => {
    const init: Record<string, 'coordinator' | 'member'> = {}
    for (const id of project.agent_ids) init[id] = 'member'
    if (project.coordinator) init[project.coordinator] = 'coordinator'
    return init
  })
  const set = (k: keyof Project, v: any) => setForm((f) => ({ ...f, [k]: v }))

  // Esc 关闭（原型 06 §5 dialog 约定）
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onClose])

  const save = async () => {
    if (!form.name?.trim()) {
      showToast('名称必填', 'err')
      return
    }
    const members = Object.entries(selected).map(([agent_id, role]) => ({ agent_id, role }))
    try {
      await api.updateProject(project.id, form)
      await api.setProjectAgents(project.id, members)
      showToast('已保存')
      bumpData()
      onChanged()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  const remove = async () => {
    if (!confirm(`删除项目「${project.name}」？其对话与消息将一并删除。`)) return
    try {
      await api.deleteProject(project.id)
      showToast('已删除')
      bumpData()
      onDeleted()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  return (
    <div className="modal-mask" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h3>项目配置 · {project.name}</h3>

        <div className="field">
          <label>名称</label>
          <input value={form.name ?? ''} onChange={(e) => set('name', e.target.value)} />
        </div>
        <div className="field">
          <label>描述</label>
          <textarea value={form.description ?? ''} onChange={(e) => set('description', e.target.value)} />
        </div>
        <div className="row2">
          <div className="field">
            <label>协作模式（M4 生效）</label>
            <select value={form.collab_mode ?? 'agent_as_tool'} onChange={(e) => set('collab_mode', e.target.value)}>
              <option value="agent_as_tool">agent_as_tool（主智能体调度）</option>
              <option value="transfer">transfer（路由移交）</option>
              <option value="single">single（单智能体）</option>
            </select>
          </div>
          <div className="field">
            <label>工作流模式（M4 生效）</label>
            <select value={form.workflow_mode ?? 'free'} onChange={(e) => set('workflow_mode', e.target.value)}>
              <option value="free">free（自由协作）</option>
              <option value="sequential">sequential（顺序）</option>
              <option value="parallel">parallel（并行）</option>
              <option value="loop">loop（循环）</option>
            </select>
          </div>
        </div>
        <div className="field">
          <label>项目级约束（统一注入成员提示词，P1）</label>
          <textarea value={form.constraints ?? ''} onChange={(e) => set('constraints', e.target.value)} />
        </div>

        <div className="field">
          <label>成员智能体（项目会话由主智能体调度，M4 生效）</label>
          {agents.length === 0 && <div className="empty-hint">还没有智能体，请先到「智能体」页创建</div>}
          {agents.map((a) => (
            <div key={a.id} style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 8 }}>
              <input
                type="checkbox"
                checked={!!selected[a.id]}
                onChange={(e) =>
                  setSelected((s) => {
                    const next = { ...s }
                    if (e.target.checked) next[a.id] = 'member'
                    else delete next[a.id]
                    return next
                  })
                }
              />
              <span style={{ flex: 1 }}>{a.name}</span>
              {selected[a.id] && (
                <select
                  value={selected[a.id]}
                  onChange={(e) => setSelected((s) => ({ ...s, [a.id]: e.target.value as any }))}
                  style={{ width: 140 }}
                >
                  <option value="member">成员</option>
                  <option value="coordinator">主智能体</option>
                </select>
              )}
            </div>
          ))}
        </div>

        <div className="actions">
          <button className="btn-primary" onClick={save}>保存</button>
          <button className="btn-ghost" onClick={onClose}>取消</button>
          <span style={{ flex: 1 }} />
          <button className="btn-danger-ghost" onClick={remove}>删除项目</button>
        </div>
      </div>
    </div>
  )
}
