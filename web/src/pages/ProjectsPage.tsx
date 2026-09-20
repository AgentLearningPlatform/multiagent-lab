import { useEffect, useMemo, useState } from 'react'
import { api } from '../api/client'
import type { Agent, Conversation, Project } from '../api/types'
import { useUI } from '../store/ui'
import Sidebar from '../components/Sidebar'
import ChatWindow from '../components/ChatWindow'
import ProjectDrawer from '../components/ProjectDrawer'

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
  const [drawerProjectId, setDrawerProjectId] = useState<string | null>(null)

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
  const drawerProject = projects.find((p) => p.id === drawerProjectId) ?? null
  const currentConv = useMemo(() => convs.find((c) => c.id === currentConvId) ?? null, [convs, currentConvId])

  // 选中对话不属于当前项目（或属于智能体）时，归位到空态
  useEffect(() => {
    if (currentConv && !(currentConv.scope === 'project' && currentConv.project_id === activeProjectId)) {
      setCurrentConv(null)
    }
  }, [activeProjectId, currentConv])

  const createProject = async () => {
    const name = prompt('项目名称：')
    if (!name?.trim()) return
    try {
      const p = await api.createProject({
        name: name.trim(),
        description: '',
        collab_mode: 'agent_as_tool',
        workflow_mode: 'free',
        constraints: '',
      })
      bumpData()
      setActiveProjectId(p.id)
      showToast('项目已创建，请在配置中完善信息并添加成员智能体')
      setDrawerProjectId(p.id)
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
    setDrawerProjectId(projectId)
  }

  return (
    <div className="main">
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
        onNewProject={createProject}
        onNewConversation={newConversation}
        onConfigureAgent={() => {}}
        onConfigureProject={configureProject}
      />

      {currentConv && currentConv.scope === 'project' ? (
        <ChatWindow
          conversation={currentConv}
          agents={agents}
          projects={projects}
          onOpenAgentDrawer={() => {}}
          onOpenProjectDrawer={() => activeProject && setDrawerProjectId(activeProject.id)}
          onConversationUpdated={reload}
        />
      ) : (
        <div className="page" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <div className="placeholder" style={{ maxWidth: 440 }}>
            <b>选择左侧项目，与其开始对话</b>
            <p style={{ marginTop: 8, fontSize: 13 }}>
              会话界面与智能体视图一致；项目对话由成员智能体协作处理（M4 起生效）。
            </p>
            <p style={{ marginTop: 14 }}>
              <button
                className="btn-primary"
                onClick={() => activeProject && newConversation(activeProject.id)}
                disabled={!activeProject}
              >
                ＋ 为「{activeProject?.name ?? '当前项目'}」新建对话
              </button>
            </p>
          </div>
        </div>
      )}

      {drawerProject && (
        <ProjectDrawer
          project={drawerProject}
          agents={agents}
          onClose={() => setDrawerProjectId(null)}
          onChanged={reload}
          onDeleted={() => {
            setDrawerProjectId(null)
            reload()
          }}
        />
      )}
    </div>
  )
}
