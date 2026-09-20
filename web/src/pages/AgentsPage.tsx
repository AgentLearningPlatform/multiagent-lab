import { useEffect, useMemo, useState } from 'react'
import { Button, Result } from 'antd'
import { api } from '../api/client'
import type { Agent, Conversation } from '../api/types'
import { useUI } from '../store/ui'
import Sidebar from '../components/Sidebar'
import ChatWindow from '../components/ChatWindow'
import AgentModal from '../components/AgentModal'
import NameModal from '../components/NameModal'

/**
 * 智能体视图（原型 06 §3.1）：
 * 左栏为智能体折叠列表（每节点常驻「＋新对话」「⚙配置」），右侧为对话窗口。
 */
export default function AgentsPage() {
  const { currentConvId, setCurrentConv, dataVersion, bumpData, showToast } = useUI()
  const [agents, setAgents] = useState<Agent[]>([])
  const [convs, setConvs] = useState<Conversation[]>([])
  const [activeAgentId, setActiveAgentId] = useState<string | null>(null)
  const [configOpen, setConfigOpen] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)

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

  const createAgent = async (name: string) => {
    setCreateOpen(false)
    try {
      const a = await api.createAgent({ name, description: '', instruction: '' })
      bumpData()
      setActiveAgentId(a.id)
      showToast('智能体已创建，请在配置中完善信息')
      setConfigOpen(true)
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

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

  const configureAgent = (agentId: string) => {
    setActiveAgentId(agentId)
    setConfigOpen(true)
  }

  return (
    <div className="main">
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

      {currentConv && currentConv.scope === 'agent' ? (
        <ChatWindow
          conversation={currentConv}
          agents={agents}
          projects={[]}
          onOpenAgentDrawer={() => setConfigOpen(true)}
          onOpenProjectDrawer={() => {}}
          onConversationUpdated={reload}
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

      {configOpen && activeAgent && (
        <AgentModal agent={activeAgent} onClose={() => setConfigOpen(false)} onChanged={reload} />
      )}

      <NameModal open={createOpen} title="新建智能体" placeholder="智能体名称" onCancel={() => setCreateOpen(false)} onSubmit={createAgent} />
    </div>
  )
}
