import { useEffect, useMemo, useState } from 'react'
import { api } from '../api/client'
import type { Agent, Conversation, Project } from '../api/types'
import { useUI } from '../store/ui'

interface TreeNode {
  key: string
  name: string
  convs: Conversation[]
}

/**
 * 左栏树（原型 06 §3.1 / §3.2）：
 * - mode=agent：智能体折叠列表，每个智能体节点常驻「＋新对话」「⚙配置」，其下挂该智能体的对话
 * - mode=project：项目折叠列表，每个项目节点同样入口，其下挂项目对话
 */
export default function Sidebar({
  mode,
  agents,
  projects,
  conversations,
  activeAgentId,
  activeProjectId,
  onSelectAgent,
  onSelectProject,
  onNewAgent,
  onNewProject,
  onNewConversation,
  onConfigureAgent,
  onConfigureProject,
}: {
  mode: 'agent' | 'project'
  agents: Agent[]
  projects: Project[]
  conversations: Conversation[]
  activeAgentId: string | null
  activeProjectId: string | null
  onSelectAgent: (id: string) => void
  onSelectProject: (id: string) => void
  onNewAgent: () => void
  onNewProject: () => void
  onNewConversation: (nodeId: string) => void
  onConfigureAgent: (id: string) => void
  onConfigureProject: (id: string) => void
}) {
  const { currentConvId, setCurrentConv, bumpData, showToast } = useUI()
  const [open, setOpen] = useState<Record<string, boolean>>({})

  const nodes = useMemo<TreeNode[]>(() => {
    const byCreated = (a: Conversation, b: Conversation) => a.created_at.localeCompare(b.created_at)
    if (mode === 'agent') {
      return agents.map((a) => ({
        key: a.id,
        name: a.name,
        convs: conversations.filter((c) => c.scope === 'agent' && c.agent_id === a.id).sort(byCreated),
      }))
    }
    return projects.map((p) => ({
      key: p.id,
      name: p.name,
      convs: conversations.filter((c) => c.scope === 'project' && c.project_id === p.id).sort(byCreated),
    }))
  }, [mode, agents, projects, conversations])

  const activeId = mode === 'agent' ? activeAgentId : activeProjectId

  // 选中节点自动展开
  useEffect(() => {
    if (activeId) setOpen((o) => (o[activeId] ? o : { ...o, [activeId]: true }))
  }, [activeId])

  const selectNode = (id: string) => {
    setOpen((o) => ({ ...o, [id]: !o[id] }))
    if (mode === 'agent') onSelectAgent(id)
    else onSelectProject(id)
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

  const isAgent = mode === 'agent'
  const configure = (id: string) => {
    if (isAgent) onConfigureAgent(id)
    else onConfigureProject(id)
  }

  return (
    <div className="sidebar">
      <div className="side-head">
        <span className="side-title">{isAgent ? '智能体' : '项目'}</span>
        <button className="btn-mini" onClick={isAgent ? onNewAgent : onNewProject}>
          ＋ 新建{isAgent ? '智能体' : '项目'}
        </button>
      </div>
      <div className="conv-list">
        {nodes.length === 0 && (
          <div className="empty-hint">
            {isAgent ? '还没有智能体，点右上角创建' : '还没有项目，点右上角创建'}
          </div>
        )}
        {nodes.map((n) => (
          <div className={`tree-node ${activeId === n.key ? 'selected' : ''}`} key={n.key}>
            <div
              className={`tree-node-head ${activeId === n.key ? 'active' : ''}`}
              onClick={() => selectNode(n.key)}
              role="button"
              aria-current={activeId === n.key ? 'true' : undefined}
            >
              <span className={`caret ${open[n.key] ? 'open' : ''}`}>▸</span>
              <span className="title">{n.name}</span>
              <span className="node-actions">
                <button
                  className="na-btn"
                  title="＋ 新对话"
                  onClick={(e) => {
                    e.stopPropagation()
                    onNewConversation(n.key)
                  }}
                >
                  ＋
                </button>
                <button
                  className="na-btn"
                  title="⚙ 配置"
                  onClick={(e) => {
                    e.stopPropagation()
                    configure(n.key)
                  }}
                >
                  ⚙
                </button>
              </span>
            </div>
            {open[n.key] && (
              <div className="node-convs">
                {n.convs.map((c) => (
                  <div
                    key={c.id}
                    className={`conv-item ${currentConvId === c.id ? 'active' : ''}`}
                    aria-current={currentConvId === c.id ? 'true' : undefined}
                    onClick={() => setCurrentConv(c.id)}
                  >
                    <span className="title">{c.title || '未命名对话'}</span>
                    <button
                      className="del"
                      title="删除对话"
                      onClick={(e) => {
                        e.stopPropagation()
                        removeConversation(c.id)
                      }}
                    >
                      ✕
                    </button>
                  </div>
                ))}
                {n.convs.length === 0 && <div className="empty-hint">暂无对话，点节点上的 ＋ 开始</div>}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
