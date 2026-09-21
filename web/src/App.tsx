import { useUI } from './store/ui'
import TopNav from './components/TopNav'
import AgentsPage from './pages/AgentsPage'
import ProjectsPage from './pages/ProjectsPage'
import SettingsPage from './pages/SettingsPage'
import KnowledgePage from './pages/KnowledgePage'
import SkillsPage from './pages/SkillsPage'
import OntologyPage from './pages/OntologyPage'
import SemanticaPage from './pages/SemanticaPage'

export default function App() {
  const { page } = useUI()
  return (
    <div className="app">
      <TopNav />
      {/* 顶栏以下整块：左栏/右栏均为满高分栏（.app-body 为行向 flex，子页撑满剩余高度） */}
      <div className="app-body">
        {page === 'agents' && <AgentsPage />}
        {page === 'projects' && <ProjectsPage />}
        {page === 'ontology' && <OntologyPage />}
        {page === 'knowledge' && <KnowledgePage />}
        {page === 'skills' && <SkillsPage />}
        {page === 'semantica' && <SemanticaPage />}
        {page === 'settings' && <SettingsPage />}
      </div>
    </div>
  )
}
