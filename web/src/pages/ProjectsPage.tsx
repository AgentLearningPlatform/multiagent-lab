import { useEffect, useMemo, useState } from 'react'
import { Button, Result, Splitter } from 'antd'
import { api } from '../api/client'
import type { Agent, Conversation, Project } from '../api/types'
import { useUI } from '../store/ui'
import Sidebar from '../components/Sidebar'
import ChatWindow from '../components/ChatWindow'
import ProjectModal from '../components/ProjectModal'
import NameModal from '../components/NameModal'

/**
 * 项目视图（原型 06 §3.2）：
 * 左栏为项目折叠列表（每节点常驻「＋新对话」「⚙配置」），右侧为与智能体视图一致的对话窗口。
 */
export default function ProjectsPage() {
  const { currentConvId, setCurrentConv, dataVersion, bumpData, showToast } = useUI()
  const [projects, setProjects] = useState<Project[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [convs, setConvs] = useState<Conversation[]>([])
  const [activeProjectId, setActiveProjectId] = useState<string | null>(null)
  const [configProjectId, setConfigProjectId] = useState<string | null>(null)
  const [createOpen, setCreateOpen] = useState(false)

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
  const configProject = projects.find((p) => p.id === configProjectId) ?? null
  const currentConv = useMemo(() => convs.find((c) => c.id === currentConvId) ?? null, [convs, currentConvId])

  // 选中对话不属于当前项目（或属于智能体）时，归位到空态
  useEffect(() => {
    if (currentConv && !(currentConv.scope === 'project' && currentConv.project_id === activeProjectId)) {
      setCurrentConv(null)
    }
  }, [activeProjectId, currentConv])

  const createProject = async (name: string) => {
    setCreateOpen(false)
    try {
      const p = await api.createProject({
        name,
        description: '',
        collab_mode: 'agent_as_tool',
        workflow_mode: 'free',
        constraints: '',
      })
      bumpData()
      setActiveProjectId(p.id)
      showToast('项目已创建，请在配置中完善信息并添加成员智能体')
      setConfigProjectId(p.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

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

  const configureProject = (projectId: string) => {
    setActiveProjectId(projectId)
    setConfigProjectId(projectId)
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

        {currentConv && currentConv.scope === 'project' ? (
          <ChatWindow
            conversation={currentConv}
            agents={agents}
            projects={projects}
            onOpenAgentDrawer={() => {}}
            onOpenProjectDrawer={() => activeProject && setConfigProjectId(activeProject.id)}
            onConversationUpdated={reload}
          />
        ) : (
          <div className="page" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <Result
              icon={null}
              title="选择左侧项目，与其开始对话"
              subTitle="会话界面与智能体视图一致；项目对话由成员智能体协作处理（M4 起生效）。"
              extra={
                <Button type="primary" disabled={!activeProject} onClick={() => activeProject && newConversation(activeProject.id)}>
                  ＋ 为「{activeProject?.name ?? '当前项目'}」新建对话
                </Button>
              }
            />
          </div>
        )}

        {configProject && (
          <ProjectModal
            project={configProject}
            agents={agents}
            onClose={() => setConfigProjectId(null)}
            onChanged={reload}
            onDeleted={() => {
              setConfigProjectId(null)
              reload()
            }}
          />
        )}

        <NameModal open={createOpen} title="新建项目" placeholder="项目名称" onCancel={() => setCreateOpen(false)} onSubmit={createProject} />
      </Splitter.Panel>
    </Splitter>
  )
}
