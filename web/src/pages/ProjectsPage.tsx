import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { Agent, Project } from '../api/types'
import { useUI } from '../store/ui'

/** 项目页：项目卡片 + 成员配置（协作模式字段 M4 生效，先可保存） */
export default function ProjectsPage() {
  const { bumpData, showToast } = useUI()
  const [projects, setProjects] = useState<Project[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [editing, setEditing] = useState<Project | 'new' | null>(null)
  const [membersOf, setMembersOf] = useState<Project | null>(null)

  const reload = () => {
    api.listProjects().then(setProjects).catch(() => setProjects([]))
    api.listAgents().then(setAgents).catch(() => setAgents([]))
  }
  useEffect(reload, [])

  const remove = async (p: Project) => {
    if (!confirm(`删除项目「${p.name}」？`)) return
    try {
      await api.deleteProject(p.id)
      showToast('已删除')
      reload()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  return (
    <div className="page">
      <h2>项目</h2>
      <div className="sub">
        项目把多个智能体组织为协作团队（M4 起可运行项目对话）。当前可创建项目、维护成员与配置。
      </div>
      <button className="btn-primary" style={{ marginBottom: 16 }} onClick={() => setEditing('new')}>
        ＋ 新建项目
      </button>

      {projects.length === 0 ? (
        <div className="placeholder">
          <b>还没有项目</b>
          <p>创建一个项目，把多个智能体组成协作团队。</p>
        </div>
      ) : (
        <div className="cards">
          {projects.map((p) => (
            <div className="card" key={p.id}>
              <h4>{p.name}</h4>
              <div className="desc">{p.description || '（无描述）'}</div>
              <div style={{ marginTop: 8, display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                <span className="badge">{p.collab_mode}</span>
                <span className="badge gray">{p.workflow_mode}</span>
                <span className="badge gray">{p.agent_ids.length} 成员</span>
              </div>
              <div className="foot">
                <button className="btn-ghost" onClick={() => setMembersOf(p)}>成员</button>
                <button className="btn-ghost" onClick={() => setEditing(p)}>编辑</button>
                <span style={{ flex: 1 }} />
                <button className="btn-danger-ghost" onClick={() => remove(p)}>删除</button>
              </div>
            </div>
          ))}
        </div>
      )}

      {editing && (
        <ProjectModal
          project={editing === 'new' ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => { setEditing(null); reload(); bumpData() }}
        />
      )}
      {membersOf && (
        <MembersModal
          project={membersOf}
          agents={agents}
          onClose={() => setMembersOf(null)}
          onSaved={() => { setMembersOf(null); reload(); bumpData() }}
        />
      )}
    </div>
  )
}

function ProjectModal({ project, onClose, onSaved }: { project: Project | null; onClose: () => void; onSaved: () => void }) {
  const { showToast } = useUI()
  const [form, setForm] = useState<Partial<Project>>(
    project ?? { name: '', description: '', collab_mode: 'agent_as_tool', workflow_mode: 'free', constraints: '' },
  )
  const set = (k: keyof Project, v: any) => setForm((f) => ({ ...f, [k]: v }))

  const save = async () => {
    if (!form.name?.trim()) {
      showToast('名称必填', 'err')
      return
    }
    try {
      if (project) await api.updateProject(project.id, form)
      else await api.createProject(form)
      showToast('已保存')
      onSaved()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  return (
    <div className="modal-mask" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h3>{project ? '编辑项目' : '新建项目'}</h3>
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
              <option value="agent_as_tool">agent_as_tool（主 Agent 调度）</option>
              <option value="transfer">transfer（路由移交）</option>
              <option value="single">single（单 Agent）</option>
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
        <div className="actions">
          <button className="btn-primary" onClick={save}>保存</button>
          <button className="btn-ghost" onClick={onClose}>取消</button>
        </div>
      </div>
    </div>
  )
}

function MembersModal({ project, agents, onClose, onSaved }: { project: Project; agents: Agent[]; onClose: () => void; onSaved: () => void }) {
  const { showToast } = useUI()
  const [selected, setSelected] = useState<Record<string, 'coordinator' | 'member'>>(() => {
    const init: Record<string, 'coordinator' | 'member'> = {}
    for (const id of project.agent_ids) init[id] = 'member'
    if (project.coordinator) init[project.coordinator] = 'coordinator'
    return init
  })

  const save = async () => {
    const members = Object.entries(selected).map(([agent_id, role]) => ({ agent_id, role }))
    try {
      await api.setProjectAgents(project.id, members)
      showToast('成员已保存')
      onSaved()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  return (
    <div className="modal-mask" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h3>项目成员 · {project.name}</h3>
        {agents.length === 0 && <div className="empty-hint">还没有智能体，请先到「智能体」页创建</div>}
        {agents.map((a) => (
          <div key={a.id} className="field" style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
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
                style={{ width: 130 }}
              >
                <option value="member">成员</option>
                <option value="coordinator">主 Agent</option>
              </select>
            )}
          </div>
        ))}
        <div className="actions">
          <button className="btn-primary" onClick={save}>保存</button>
          <button className="btn-ghost" onClick={onClose}>取消</button>
        </div>
      </div>
    </div>
  )
}
