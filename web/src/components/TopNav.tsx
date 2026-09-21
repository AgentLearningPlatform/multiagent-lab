import type { ReactNode } from 'react'
import { RobotOutlined, ProjectOutlined, ApartmentOutlined, DatabaseOutlined, ThunderboltOutlined, NodeIndexOutlined, SettingOutlined } from '@ant-design/icons'
import { useUI, type PageKey } from '../store/ui'

const PAGES: { key: PageKey; label: string; icon: ReactNode }[] = [
  { key: 'agents', label: '智能体', icon: <RobotOutlined /> },
  { key: 'projects', label: '项目', icon: <ProjectOutlined /> },
  { key: 'ontology', label: '本体', icon: <ApartmentOutlined /> },
  { key: 'knowledge', label: '知识库', icon: <DatabaseOutlined /> },
  { key: 'skills', label: '技能', icon: <ThunderboltOutlined /> },
  { key: 'semantica', label: 'Semantica', icon: <NodeIndexOutlined /> },
]

/**
 * 顶部导航（开发者工具风格，浅色克制）：
 * - 品牌区：渐变几何标记（多智能体联结点，内联 SVG 非 emoji）+ 词标 / Lab 徽标 / 副标题；
 * - 导航：自绘 pill 按钮（图标 + 文字节奏一致），激活态品牌底 + 底部 2px 强调条；
 * - 设置入口不在导航内，顶栏最右端独立齿轮按钮（原型 06 §2 / §3.6 v0.4），与各模块同一切换机制。
 */
export default function TopNav() {
  const { page, setPage } = useUI()
  return (
    <header className="topnav">
      <div className="brand">
        <span className="brand-mark" aria-hidden="true">
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" aria-hidden="true">
            <path d="M12 6.6 6.4 15.5M12 6.6l5.6 8.9M7.4 16.2h9.2" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" />
            <circle cx="12" cy="5.2" r="2.5" fill="currentColor" />
            <circle cx="5.6" cy="16.6" r="2.2" fill="currentColor" opacity="0.86" />
            <circle cx="18.4" cy="16.6" r="2.2" fill="currentColor" opacity="0.86" />
          </svg>
        </span>
        <span className="brand-text">
          <span className="brand-name">Eino</span>
          <span className="brand-chip">Lab</span>
          <span className="brand-sub">多智能体学习平台</span>
        </span>
      </div>

      <nav className="topnav-nav" aria-label="主导航">
        {PAGES.map((p) => (
          <button
            key={p.key}
            type="button"
            className={`nav-item${page === p.key ? ' active' : ''}`}
            aria-current={page === p.key ? 'page' : undefined}
            onClick={() => setPage(p.key)}
          >
            <span className="nav-item-icon">{p.icon}</span>
            <span className="nav-item-label">{p.label}</span>
          </button>
        ))}
      </nav>

      <span className="topnav-spacer" />
      <button
        type="button"
        className={`topnav-gear${page === 'settings' ? ' active' : ''}`}
        aria-label="设置"
        aria-current={page === 'settings' ? 'true' : undefined}
        title="设置（模型管理等）"
        onClick={() => setPage('settings')}
      >
        <SettingOutlined />
      </button>
    </header>
  )
}
