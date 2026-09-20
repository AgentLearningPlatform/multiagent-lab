import { Layout, Result, Tag, Typography } from 'antd'
import { useUI } from './store/ui'
import TopNav from './components/TopNav'
import AgentsPage from './pages/AgentsPage'
import ProjectsPage from './pages/ProjectsPage'
import SettingsPage from './pages/SettingsPage'

const { Content } = Layout

const PLACEHOLDERS: Record<string, { title: string; desc: string; milestone: string }> = {
  ontology: {
    title: '本体建模',
    desc: '本体 Schema 设计、实例管理与运行方案将在这里进行。本体模块为独立维护平面（runtime-manager + ontology-service），后续里程碑接入主平台。',
    milestone: 'M6-M8',
  },
  knowledge: {
    title: '知识库',
    desc: '文档上传、切分、向量化与检索评测将在这里进行，依赖向量模型连接与向量库。',
    milestone: 'M7',
  },
  skills: {
    title: '技能',
    desc: '技能市场、绑定与运行链路将在这里进行：内置技能、Skillfile 描述的智能体技能与 MCP 工具。',
    milestone: 'M6',
  },
}

function Placeholder({ page }: { page: string }) {
  const p = PLACEHOLDERS[page]
  return (
    <div className="page">
      <Result
        icon={<Typography.Title level={3} style={{ marginBottom: 0 }}>{p.title}</Typography.Title>}
        title={
          <span>
            该模块将在后续里程碑开放 <Tag color="purple">{p.milestone}</Tag>
          </span>
        }
        subTitle={<span style={{ maxWidth: 520, display: 'inline-block' }}>{p.desc}</span>}
      />
    </div>
  )
}

export default function App() {
  const { page } = useUI()
  return (
    <Layout className="app" style={{ minHeight: '100vh' }}>
      <TopNav />
      <Content style={{ minHeight: 0 }}>
        {page === 'agents' && <AgentsPage />}
        {page === 'projects' && <ProjectsPage />}
        {page === 'settings' && <SettingsPage />}
        {(page === 'ontology' || page === 'knowledge' || page === 'skills') && <Placeholder page={page} />}
      </Content>
    </Layout>
  )
}
