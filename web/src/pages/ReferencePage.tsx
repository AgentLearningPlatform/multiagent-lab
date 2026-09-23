import { useMemo, useState, type ReactNode } from 'react'
import { Card, Menu, Space, Splitter, Tag, Typography } from 'antd'
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

// 内容资产（REQ-116）：构建期内联，编辑 seeds/learning/reference/*.md 后重建即生效（REQ-109 同模式）。
// 每篇头部带 frontmatter 源指针（module/req/docs/decisions/synced）——它是该模块知识的权威出处地图，
// 语义级变更（REQ 行/决策/口径）须同步更新命中的 reference 文件（AGENTS.md 纪律 7）。
import OVERVIEW_MD from '../../../seeds/learning/reference/overview.md?raw'
import AGENTS_MD from '../../../seeds/learning/reference/agents.md?raw'
import PROJECTS_MD from '../../../seeds/learning/reference/projects.md?raw'
import ONTOLOGY_MD from '../../../seeds/learning/reference/ontology.md?raw'
import KNOWLEDGE_MD from '../../../seeds/learning/reference/knowledge.md?raw'
import SKILLS_MD from '../../../seeds/learning/reference/skills.md?raw'
import SETTINGS_MD from '../../../seeds/learning/reference/settings.md?raw'

/**
 * 参考资料中心（REQ-116）：
 * 左栏以平台模块为单位分类（含平台总览），右栏展示该模块的产品定位 / 设计原理 / 相关资料，
 * 文末附「源文档地图」（frontmatter 解析）——把 docs/research 的权威出处带回给读者。
 * 定位：模块知识库的学习层 + 索引层（17 §1.4 知识三层），权威事实仍在 docs/ 与 research/。
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

interface Frontmatter {
  module?: string
  req?: string[]
  docs?: string[]
  decisions?: string[]
  synced?: string
}

/** 解析文章头部 `---` frontmatter（轻量 key: [a, b] 格式，无需引入 YAML 依赖） */
function parseFrontmatter(raw: string): { meta: Frontmatter; body: string } {
  const m = raw.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?/)
  if (!m) return { meta: {}, body: raw }
  const meta: Frontmatter = {}
  for (const line of m[1].split('\n')) {
    const kv = line.match(/^(\w+):\s*(.*)$/)
    if (!kv) continue
    const [, key, value] = kv
    if (value.startsWith('[')) {
      const items = value
        .slice(1, value.lastIndexOf(']'))
        .split(',')
        .map((s) => s.trim().replace(/^["']|["']$/g, ''))
        .filter(Boolean)
      ;(meta as Record<string, unknown>)[key] = items
    } else {
      ;(meta as Record<string, unknown>)[key] = value.trim()
    }
  }
  return { meta, body: raw.slice(m[0].length) }
}

/** 源文档地图（frontmatter → 文末导读卡）：权威事实在 docs/ 与 research/，此处只做指路 */
function SourceMap({ meta }: { meta: Frontmatter }) {
  const rows: { label: string; items: string[] }[] = [
    { label: '需求编号', items: meta.req ?? [] },
    { label: '文档章节', items: meta.docs ?? [] },
    { label: '关联决策', items: meta.decisions ?? [] },
  ].filter((r) => r.items.length > 0 && !(r.items.length === 1 && r.items[0] === '—'))
  if (rows.length === 0) return null
  return (
    <Card size="small" className="ref-sourcemap" title="深入阅读 · 源文档地图">
      <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
        本页是学习视图；权威事实以下列出处为准（编号见 docs/18 注册表）。
      </Typography.Paragraph>
      {rows.map((r) => (
        <div key={r.label} className="ref-sourcemap-row">
          <span className="ref-sourcemap-label">{r.label}</span>
          <span>
            {r.items.map((it) => (
              <Tag key={it} style={{ marginInlineEnd: 6 }}>
                {it}
              </Tag>
            ))}
          </span>
        </div>
      ))}
      {meta.synced && (
        <Typography.Paragraph type="secondary" style={{ fontSize: 11, marginBottom: 0, marginTop: 6 }}>
          最后同步：{meta.synced}（语义级变更须按 AGENTS.md 纪律 7 同步本页）
        </Typography.Paragraph>
      )}
    </Card>
  )
}

export default function ReferencePage() {
  const [active, setActive] = useState('overview')
  const section = SECTIONS.find((s) => s.key === active) ?? SECTIONS[0]
  const { meta, body } = useMemo(() => parseFrontmatter(section.md), [section])
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
            <XMarkdown content={body} openLinksInNewTab />
            <SourceMap meta={meta} />
          </div>
        </div>
      </Splitter.Panel>
    </Splitter>
  )
}
