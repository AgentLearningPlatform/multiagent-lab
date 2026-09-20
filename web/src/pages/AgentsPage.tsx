import { useEffect, useMemo, useState } from 'react'
import { api } from '../api/client'
import type { Agent, Conversation, Project } from '../api/types'
import { useUI } from '../store/ui'
import Sidebar from '../components/Sidebar'
import ChatWindow from '../components/ChatWindow'
import AgentDrawer from '../components/AgentDrawer'

/** 智能体页：顶部 Agent 标签条 + 左栏对话树 + 中间对话 + 右侧属性抽屉 */
export default function AgentsPage() {
  const { currentConvId, setCurrentConv, dataVersion, bumpData, showToast } = useUI()
  const [agents, setAgents] = useState<Agent[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [convs, setConvs] = useState<Conversation[]>([])
  const [activeAgentId, setActiveAgentId] = useState<string | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [creatingAgent, setCreatingAgent] = useState(false)

  const reload = () => {
    api.listAgents().then((as) => {
      setAgents(as)
      setActiveAgentId((cur) => (cur && as.some((a) => a.id === cur) ? cur : (as[0]?.id ?? null)))
    }).catch(() => setAgents([]))
    api.listProjects().then(setProjects).catch(() => setProjects([]))
    api.listConversations().then(setConvs).catch(() => setConvs([]))
  }

  useEffect(reload, [dataVersion])

  const activeAgent = agents.find((a) => a.id === activeAgentId) ?? null
  const currentConv = useMemo(() => convs.find((c) => c.id === currentConvId) ?? null, [convs, currentConvId])

  // 选中对话不属于当前 Agent 时，自动归位到当前 agent 的首个对话或空
  useEffect(() => {
    if (currentConv && currentConv.scope === 'agent' && currentConv.agent_id !== activeAgentId) {
      const first = convs.find((c) => c.scope === 'agent' && c.agent_id === activeAgentId)
      setCurrentConv(first?.id ?? null)
    }
  }, [activeAgentId])

  const createAgent = async () => {
    const name = prompt('智能体名称：')
    if (!name?.trim()) return
    setCreatingAgent(true)
    try {
      const a = await api.createAgent({ name: name.trim(), description: '', instruction: '' })
      bumpData()
      setActiveAgentId(a.id)
      showToast('智能体已创建，请在属性中完善配置')
      setDrawerOpen(true)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setCreatingAgent(false)
    }
  }

  // 为当前 Agent 新建对话
  const newConversation = async () => {
    if (!activeAgent) return
    try {
      const c = await api.createConversation({ scope: 'agent', agent_id: activeAgent.id, title: '新对话' })
      bumpData()
      setCurrentConv(c.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  return (
    <div className="main">
      {/* Agent 标签条 */}
      <div style={{ position: 'absolute', top: 52, left: 0, right: 0, zIndex: 5, display: 'flex', gap: 6, padding: '8px 20px', background: 'var(--c-bg)', borderBottom: '1px solid var(--c-line)' }}>
        {agents.map((a) => (
          <button
            key={a.id}
            className={`nav-item ${a.id === activeAgentId ? 'active' : ''}`}
            style={{ background: a.id === activeAgentId ? 'var(--c-brand-weak)' : 'var(--c-surface)', border: '1px solid var(--c-line)' }}
            onClick={() => setActiveAgentId(a.id)}
          >
            {a.name}
          </button>
        ))}
        <button className="btn-mini" onClick={createAgent} disabled={creatingAgent}>＋ 新建智能体</button>
      </div>

      {/* 占位：顶部标签条高度补偿 */}
      <div style={{ height: 46, flex: 'none' }} />

      <Sidebar agents={agents} projects={projects} />

      {currentConv ? (
        <ChatWindow
          conversation={currentConv}
          agents={agents}
          onOpenDrawer={() => setDrawerOpen(true)}
          onConversationUpdated={reload}
        />
      ) : (
        <div className="page" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <div className="placeholder" style={{ maxWidth: 420 }}>
            <b>选择左侧对话，或为「{activeAgent?.name ?? '当前智能体'}」开始新对话</b>
            <p style={{ marginTop: 14 }}>
              <button className="btn-primary" onClick={newConversation} disabled={!activeAgent}>＋ 新建对话</button>
            </p>
          </div>
        </div>
      )}

      {drawerOpen && activeAgent && (
        <AgentDrawer
          agent={activeAgent}
          onClose={() => setDrawerOpen(false)}
          onChanged={reload}
        />
      )}
    </div>
  )
}
