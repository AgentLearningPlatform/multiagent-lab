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

/** 顶部导航（蚂蚁 Menu horizontal） */
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
        items={[
          ...PAGES.map((p) => ({ key: p.key, label: p.label, icon: p.icon })),
          { key: 'settings', label: '设置', icon: <SettingOutlined /> },
        ]}
      />
    </div>
  )
}
