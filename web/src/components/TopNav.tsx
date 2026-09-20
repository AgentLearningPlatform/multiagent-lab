import type { ReactNode } from 'react'
import { Menu } from 'antd'
import { RobotOutlined, ProjectOutlined, ApartmentOutlined, DatabaseOutlined, ThunderboltOutlined, SettingOutlined } from '@ant-design/icons'
import { useUI, type PageKey } from '../store/ui'

const PAGES: { key: PageKey; label: string; icon: ReactNode }[] = [
  { key: 'agents', label: '智能体', icon: <RobotOutlined /> },
  { key: 'projects', label: '项目', icon: <ProjectOutlined /> },
  { key: 'ontology', label: '本体', icon: <ApartmentOutlined /> },
  { key: 'knowledge', label: '知识库', icon: <DatabaseOutlined /> },
  { key: 'skills', label: '技能', icon: <ThunderboltOutlined /> },
]

/**
 * 顶部导航（蚂蚁 Menu horizontal）。
 * 设置不在导航菜单内：顶栏最右端独立齿轮按钮进入（原型 06 §2 / §3.6 v0.4），
 * 悬停微旋转、激活态高亮，与五模块同一切换机制（useUI.setPage）。
 */
export default function TopNav() {
  const { page, setPage } = useUI()
  return (
    <div className="topnav">
      <span className="logo">◆ Eino 多智能体学习平台</span>
      <Menu
        mode="horizontal"
        selectedKeys={[page]}
        onClick={({ key }) => setPage(key as PageKey)}
        style={{ flex: 1, minWidth: 0, borderBottom: 'none', background: 'transparent' }}
        items={PAGES.map((p) => ({ key: p.key, label: p.label, icon: p.icon }))}
      />
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
    </div>
  )
}
