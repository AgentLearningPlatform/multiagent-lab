import { useEffect, useMemo, useState } from 'react'
import { Button, Result, Space, Splitter } from 'antd'
import { FolderOutlined } from '@ant-design/icons'
import { api } from '../api/client'
import type { Agent, Conversation, Project } from '../api/types'
import { useUI } from '../store/ui'
import Sidebar from '../components/Sidebar'
import ChatWindow from '../components/ChatWindow'
import ProjectModal from '../components/ProjectModal'
import ProjectSidePanel, { type PanelView } from '../components/ProjectSidePanel'

/**
 * 项目视图（原型 06 §3.2）：
 * 左栏为项目折叠列表（每节点常驻「＋新对话」「⚙配置」），右侧为与智能体视图一致的对话窗口。
 * REQ-103：项目配置改为右侧边栏配置视图（原 ProjectModal 编辑表单迁入）；ProjectModal 仅用于新建。
 */
export default function ProjectsPage() {
  const { currentConvId, setCurrentConv, dataVersion, bumpData, showToast } = useUI()
  const [projects, setProjects] = useState<Project[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [convs, setConvs] = useState<Conversation[]>([])
  const [activeProjectId, setActiveProjectId] = useState<string | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  // REQ-102/103：右侧侧边栏（文件 / Git / 配置）开合与当前视图
  const [sidePanelOpen, setSidePanelOpen] = useState(false)
  const [panelView, setPanelView] = useState<PanelView>('files')

  const reload = () => {
    api.listProjects().then((ps) => {
      setProjects(ps)
      setActiveProjectId((cur) => (cur && ps.some((p) => p.id === cur) ? cur : (ps[0]?.id ?? null)))
    }).catch(() => setProjects([]))
    api.listAgents().then(setAgents).catch(() => setAgents([]))
    api.listConversations().then(setConvs).catch(() => setConvs([]))
  }

  useEffect(reload, [dataVersion])

  const activeProject = projects.find((p) => p.id === activeProjectId) ?? null
  const currentConv = useMemo(() => convs.find((c) => c.id === currentConvId) ?? null, [convs, currentConvId])

  // 选中对话不属于当前项目（或属于智能体）时，归位到空态
  useEffect(() => {
    if (currentConv && !(currentConv.scope === 'project' && currentConv.project_id === activeProjectId)) {
      setCurrentConv(null)
    }
  }, [activeProjectId, currentConv])

  const newConversation = async (projectId: string) => {
    try {
      const c = await api.createConversation({ scope: 'project', project_id: projectId, title: '新对话' })
      bumpData()
      setActiveProjectId(projectId)
      setCurrentConv(c.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  /** 项目节点 ⚙ → 右侧边栏配置视图（侧边栏若未开则同时打开） */
  const configureProject = (projectId: string) => {
    setActiveProjectId(projectId)
    setSidePanelOpen(true)
    setPanelView('config')
  }

  /** ChatWindow「配置」→ 打开当前项目侧边栏配置视图 */
  const openSidePanelConfig = () => {
    setSidePanelOpen(true)
    setPanelView('config')
  }

  const onProjectCreated = (id: string) => {
    setCreateOpen(false)
    reload()
    setActiveProjectId(id)
    setSidePanelOpen(true)
    setPanelView('config')
    showToast('项目已创建，请在侧边栏完善配置')
  }

  return (
    <Splitter
      className="main sidebar-splitter"
      onResizeEnd={(sizes) => localStorage.setItem('eino.sidebar.width', String(Math.round(sizes[0])))}
    >
      <Splitter.Panel defaultSize={Number(localStorage.getItem('eino.sidebar.width')) || 280} min={220} max={480} className="sidebar-panel">
        <Sidebar
          mode="project"
          agents={[]}
          projects={projects}
          conversations={convs}
          activeAgentId={null}
          activeProjectId={activeProjectId}
          onSelectAgent={() => {}}
          onSelectProject={setActiveProjectId}
          onNewAgent={() => {}}
          onNewProject={() => setCreateOpen(true)}
          onNewConversation={newConversation}
          onConfigureAgent={() => {}}
          onConfigureProject={configureProject}
        />
      </Splitter.Panel>
      <Splitter.Panel className="content-panel">
        {/* REQ-102：对话区 + 右侧项目侧边栏（flex 兄弟节点，互不遮挡） */}
        <div className="proj-content-row">
          <div className="proj-content-main">
            {currentConv && currentConv.scope === 'project' ? (
              <ChatWindow
                conversation={currentConv}
                agents={agents}
                projects={projects}
                onOpenAgentDrawer={() => {}}
                onOpenProjectDrawer={openSidePanelConfig}
                onConversationUpdated={reload}
                sidePanelOpen={sidePanelOpen}
                onToggleSidePanel={() => setSidePanelOpen((o) => !o)}
              />
            ) : (
              <div className="page" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                <Result
                  icon={null}
                  title="选择左侧项目，与其开始对话"
                  subTitle="会话界面与智能体视图一致；项目对话由成员智能体协作处理（M4 起生效）。"
                  extra={
                    <Space>
                      <Button type="primary" disabled={!activeProject} onClick={() => activeProject && newConversation(activeProject.id)}>
                        ＋ 为「{activeProject?.name ?? '当前项目'}」新建对话
                      </Button>
                      <Button disabled={!activeProject} icon={<FolderOutlined />} onClick={() => { setSidePanelOpen(true); setPanelView('config') }}>
                        项目配置 / 侧边栏
                      </Button>
                    </Space>
                  }
                />
              </div>
            )}
          </div>

          {activeProject && sidePanelOpen && (
            <ProjectSidePanel
              project={activeProject}
              agents={agents}
              open={sidePanelOpen}
              view={panelView}
              onViewChange={setPanelView}
              onClose={() => setSidePanelOpen(false)}
              onChanged={reload}
            />
          )}
        </div>

        {createOpen && <ProjectModal agents={agents} onClose={() => setCreateOpen(false)} onCreated={onProjectCreated} />}
      </Splitter.Panel>
    </Splitter>
  )
}
