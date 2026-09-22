import { useEffect, useMemo, useState } from 'react'
import { Button, Result, Splitter } from 'antd'
import { api } from '../api/client'
import type { Agent, Conversation } from '../api/types'
import { useUI } from '../store/ui'
import Sidebar from '../components/Sidebar'
import ChatWindow from '../components/ChatWindow'
import AgentModal from '../components/AgentModal'
import AgentSidePanel from '../components/AgentSidePanel'

/**
 * 智能体视图（原型 06 §3.1）：
 * 左栏为智能体折叠列表（每节点常驻「＋新对话」「⚙配置」），右侧为对话窗口。
 * REQ-103：智能体配置改为右侧边栏配置视图（原 AgentModal 编辑表单迁入）；AgentModal 仅用于新建。
 */
export default function AgentsPage() {
  const { currentConvId, setCurrentConv, dataVersion, bumpData, showToast } = useUI()
  const [agents, setAgents] = useState<Agent[]>([])
  const [convs, setConvs] = useState<Conversation[]>([])
  const [activeAgentId, setActiveAgentId] = useState<string | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  // REQ-103：右侧智能体侧边栏（配置视图）开合
  const [sidePanelOpen, setSidePanelOpen] = useState(false)

  const reload = () => {
    api.listAgents().then((as) => {
      setAgents(as)
      setActiveAgentId((cur) => (cur && as.some((a) => a.id === cur) ? cur : (as[0]?.id ?? null)))
    }).catch(() => setAgents([]))
    api.listConversations().then(setConvs).catch(() => setConvs([]))
  }

  useEffect(reload, [dataVersion])

  const activeAgent = agents.find((a) => a.id === activeAgentId) ?? null
  const currentConv = useMemo(() => convs.find((c) => c.id === currentConvId) ?? null, [convs, currentConvId])

  // 选中对话不属于当前智能体（或属于项目）时，归位到空态
  useEffect(() => {
    if (currentConv && !(currentConv.scope === 'agent' && currentConv.agent_id === activeAgentId)) {
      setCurrentConv(null)
    }
  }, [activeAgentId, currentConv])

  // 为指定智能体新建对话
  const newConversation = async (agentId: string) => {
    try {
      const c = await api.createConversation({ scope: 'agent', agent_id: agentId, title: '新对话' })
      bumpData()
      setActiveAgentId(agentId)
      setCurrentConv(c.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  /** 智能体节点 ⚙ → 右侧边栏配置视图（侧边栏若未开则同时打开） */
  const configureAgent = (agentId: string) => {
    setActiveAgentId(agentId)
    setSidePanelOpen(true)
  }

  /** ChatWindow「配置」→ 打开当前智能体侧边栏配置视图 */
  const openSidePanelConfig = () => setSidePanelOpen(true)

  const onAgentCreated = (id: string) => {
    setCreateOpen(false)
    reload()
    setActiveAgentId(id)
    setSidePanelOpen(true)
    showToast('智能体已创建，请在侧边栏完善配置')
  }

  return (
    <Splitter
      className="main sidebar-splitter"
      onResizeEnd={(sizes) => localStorage.setItem('eino.sidebar.width', String(Math.round(sizes[0])))}
    >
      <Splitter.Panel defaultSize={Number(localStorage.getItem('eino.sidebar.width')) || 280} min={220} max={480} className="sidebar-panel">
        <Sidebar
          mode="agent"
          agents={agents}
          projects={[]}
          conversations={convs}
          activeAgentId={activeAgentId}
          activeProjectId={null}
          onSelectAgent={setActiveAgentId}
          onSelectProject={() => {}}
          onNewAgent={() => setCreateOpen(true)}
          onNewProject={() => {}}
          onNewConversation={newConversation}
          onConfigureAgent={configureAgent}
          onConfigureProject={() => {}}
        />
      </Splitter.Panel>
      <Splitter.Panel className="content-panel">
        {/* REQ-103：对话区 + 右侧智能体侧边栏（flex 兄弟节点，互不遮挡） */}
        <div className="proj-content-row">
          <div className="proj-content-main">
            {currentConv && currentConv.scope === 'agent' ? (
              <ChatWindow
                conversation={currentConv}
                agents={agents}
                projects={[]}
                onOpenAgentDrawer={openSidePanelConfig}
                onOpenProjectDrawer={() => {}}
                onConversationUpdated={reload}
                sidePanelOpen={sidePanelOpen}
                onToggleSidePanel={() => setSidePanelOpen((o) => !o)}
              />
            ) : (
              <div className="page" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                <Result
                  icon={null}
                  title="选择左侧智能体，与其开始对话"
                  subTitle="每个智能体可挂多个对话；左侧节点上的 ＋ 可直接新建对话。"
                  extra={
                    <Button type="primary" disabled={!activeAgent} onClick={() => activeAgent && newConversation(activeAgent.id)}>
                      ＋ 为「{activeAgent?.name ?? '当前智能体'}」新建对话
                    </Button>
                  }
                />
              </div>
            )}
          </div>

          {activeAgent && sidePanelOpen && (
            <AgentSidePanel
              agent={activeAgent}
              open={sidePanelOpen}
              onClose={() => setSidePanelOpen(false)}
              onChanged={reload}
            />
          )}
        </div>

        {createOpen && <AgentModal onClose={() => setCreateOpen(false)} onCreated={onAgentCreated} />}
      </Splitter.Panel>
    </Splitter>
  )
}
