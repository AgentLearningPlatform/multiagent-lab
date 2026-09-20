import { useEffect, useMemo, useState } from 'react'
import { api } from '../api/client'
import type { Agent, Conversation, Project } from '../api/types'
import { useUI } from '../store/ui'

interface Group {
  key: string
  title: string
  convs: Conversation[]
}

/** 左栏：直聊对话 / 项目对话 分组折叠树 */
export default function Sidebar({
  agents,
  projects,
}: {
  agents: Agent[]
  projects: Project[]
}) {
  const { currentConvId, setCurrentConv, dataVersion, bumpData, showToast } = useUI()
  const [convs, setConvs] = useState<Conversation[]>([])
  const [open, setOpen] = useState<Record<string, boolean>>({ direct: true, project: true })
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    api.listConversations().then(setConvs).catch(() => setConvs([]))
  }, [dataVersion])

  const groups = useMemo<Group[]>(() => {
    const direct: Conversation[] = []
    const project: Conversation[] = []
    for (const c of convs) {
      if (c.scope === 'agent') direct.push(c)
      else project.push(c)
    }
    direct.sort((a, b) => a.created_at.localeCompare(b.created_at))
    project.sort((a, b) => a.created_at.localeCompare(b.created_at))
    return [
      { key: 'direct', title: `直聊对话（${direct.length}）`, convs: direct },
      { key: 'project', title: `项目对话（${project.length}）`, convs: project },
    ]
  }, [convs, agents])

  const agentLabel = (c: Conversation) => {
    if (c.scope === 'agent') return agents.find((a) => a.id === c.agent_id)?.name ?? '未绑定'
    return projects.find((p) => p.id === c.project_id)?.name ?? '未绑定项目'
  }

  const createConversation = async () => {
    if (agents.length === 0) {
      showToast('请先在右侧新建智能体', 'err')
      return
    }
    setCreating(true)
    try {
      const c = await api.createConversation({ scope: 'agent', agent_id: agents[0].id, title: '新对话' })
      bumpData()
      setCurrentConv(c.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setCreating(false)
    }
  }

  const removeConversation = async (id: string) => {
    if (!confirm('删除该对话及其全部消息？')) return
    try {
      await api.deleteConversation(id)
      if (currentConvId === id) setCurrentConv(null)
      bumpData()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  return (
    <div className="sidebar">
      <div className="side-head">
        <span className="side-title">对话</span>
        <button className="btn-mini" onClick={createConversation} disabled={creating}>
          ＋ 新建对话
        </button>
      </div>
      <div className="conv-list">
        {groups.map((g) => (
          <div className="tree-group" key={g.key}>
            <div
              className="tree-group-title"
              onClick={() => setOpen((o) => ({ ...o, [g.key]: !o[g.key] }))}
            >
              <span className={`caret ${open[g.key] ? 'open' : ''}`}>▸</span>
              {g.title}
            </div>
            {open[g.key] &&
              g.convs.map((c) => (
                <div
                  key={c.id}
                  className={`conv-item ${currentConvId === c.id ? 'active' : ''}`}
                  onClick={() => setCurrentConv(c.id)}
                >
                  <span className="title">
                    {c.title || '未命名对话'}
                    <span style={{ color: 'var(--c-ink-3)', marginLeft: 6, fontSize: 11 }}>{agentLabel(c)}</span>
                  </span>
                  <button className="del" title="删除对话" onClick={(e) => { e.stopPropagation(); removeConversation(c.id) }}>
                    ✕
                  </button>
                </div>
              ))}
            {open[g.key] && g.convs.length === 0 && <div className="empty-hint">暂无对话</div>}
          </div>
        ))}
      </div>
    </div>
  )
}
