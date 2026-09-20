import { useUI } from '../store/ui'

const PAGES: { key: string; label: string }[] = [
  { key: 'agents', label: '智能体' },
  { key: 'projects', label: '项目' },
  { key: 'ontology', label: '本体' },
  { key: 'knowledge', label: '知识库' },
  { key: 'skills', label: '技能' },
]

export default function TopNav() {
  const { page, setPage } = useUI()
  return (
    <div className="topnav">
      <span className="logo">◆ Eino 多智能体学习平台</span>
      {PAGES.map((p) => (
        <button key={p.key} className={`nav-item ${page === p.key ? 'active' : ''}`} onClick={() => setPage(p.key as any)}>
          {p.label}
        </button>
      ))}
      <span className="spacer" />
      <button className={`nav-item ${page === 'settings' ? 'active' : ''}`} onClick={() => setPage('settings')}>
        设置
      </button>
    </div>
  )
}
