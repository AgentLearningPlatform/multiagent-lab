import { useState, type ReactNode } from 'react'
import { Menu, Space, Splitter, Typography } from 'antd'
import {
  ApartmentOutlined,
  CompassOutlined,
  DatabaseOutlined,
  ProjectOutlined,
  RobotOutlined,
  SettingOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import XMarkdown from '@ant-design/x-markdown'

// 内容资产（REQ-116）：构建期内联，编辑 seeds/learning/reference/*.md 后重建即生效（REQ-109 同模式）
import OVERVIEW_MD from '../../../seeds/learning/reference/overview.md?raw'
import AGENTS_MD from '../../../seeds/learning/reference/agents.md?raw'
import PROJECTS_MD from '../../../seeds/learning/reference/projects.md?raw'
import ONTOLOGY_MD from '../../../seeds/learning/reference/ontology.md?raw'
import KNOWLEDGE_MD from '../../../seeds/learning/reference/knowledge.md?raw'
import SKILLS_MD from '../../../seeds/learning/reference/skills.md?raw'
import SETTINGS_MD from '../../../seeds/learning/reference/settings.md?raw'

/**
 * 参考资料中心（REQ-116）：
 * 左栏以平台模块为单位分类（含平台总览），右栏展示该模块的产品定位 / 设计原理 / 相关资料。
 * 定位：把散在 docs/ 与 research/ 的设计语境带入产品内，服务「先懂为什么，再动手」的学习动线（17 号 J1）。
 */
const SECTIONS: { key: string; label: string; icon: ReactNode; md: string }[] = [
  { key: 'overview', label: '平台总览', icon: <CompassOutlined />, md: OVERVIEW_MD },
  { key: 'agents', label: '智能体', icon: <RobotOutlined />, md: AGENTS_MD },
  { key: 'projects', label: '项目', icon: <ProjectOutlined />, md: PROJECTS_MD },
  { key: 'ontology', label: '本体', icon: <ApartmentOutlined />, md: ONTOLOGY_MD },
  { key: 'knowledge', label: '知识库', icon: <DatabaseOutlined />, md: KNOWLEDGE_MD },
  { key: 'skills', label: '技能', icon: <ThunderboltOutlined />, md: SKILLS_MD },
  { key: 'settings', label: '设置', icon: <SettingOutlined />, md: SETTINGS_MD },
]

export default function ReferencePage() {
  const [active, setActive] = useState('overview')
  const section = SECTIONS.find((s) => s.key === active) ?? SECTIONS[0]
  return (
    <Splitter className="main sidebar-splitter">
      <Splitter.Panel
        defaultSize={Number(localStorage.getItem('eino.ref.width')) || 240}
        min={180}
        max={400}
        className="sidebar-panel"
      >
        <aside className="sidebar">
          <div className="side-head">
            <span className="side-title">参考资料</span>
          </div>
          <Menu
            mode="vertical"
            selectedKeys={[active]}
            onClick={({ key }) => setActive(key)}
            style={{ padding: '0 10px', background: 'transparent' }}
            items={SECTIONS.map((s) => ({ key: s.key, icon: s.icon, label: s.label }))}
          />
          <div className="settings-note">
            按「产品定位 → 设计原理 → 相关资料」组织；内容随仓库维护（
            <Typography.Text code style={{ fontSize: 11 }}>seeds/learning/reference/</Typography.Text>
            ，编辑后重建即生效）。
          </div>
        </aside>
      </Splitter.Panel>
      <Splitter.Panel className="content-panel">
        <div className="ref-main">
          <div className="settings-head">
            <Typography.Title level={5} style={{ marginTop: 0, marginBottom: 4 }}>
              <Space>{section.icon}{section.label}</Space>
            </Typography.Title>
            <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
              产品定位 → 设计原理 → 相关资料；进一步的设计全文见 docs/ 对应文档编号。
            </Typography.Paragraph>
          </div>
          <div className="ref-body">
            <XMarkdown content={section.md} openLinksInNewTab />
          </div>
        </div>
      </Splitter.Panel>
    </Splitter>
  )
}
