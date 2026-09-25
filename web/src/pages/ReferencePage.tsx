import { useMemo, useState, type MouseEvent, type ReactNode } from 'react'
import { Card, Menu, Space, Splitter, Tag, Typography } from 'antd'
import {
  ApartmentOutlined,
  BookOutlined,
  CompassOutlined,
  DatabaseOutlined,
  HighlightOutlined,
  LinkOutlined,
  ProjectOutlined,
  RobotOutlined,
  SettingOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import XMarkdown from '@ant-design/x-markdown'
import DocViewerModal from '../components/DocViewerModal'

// 平台知识内容（REQ-116 / REQ-161 v2）：构建期内联扫描 platform-knowledge/ 全目录——
// 目录结构即页面结构（L1 模块子目录 / L2 主题页），新增或迁移文档后重建即生效。
// 每篇头部 frontmatter（module/topic/desc/req/docs/decisions/synced）为页面元信息与源指针约定，
// 语义级变更（REQ 行/决策/口径）须同步更新命中的文档（AGENTS.md 纪律 7）。
const KB_RAW = import.meta.glob('../../../platform-knowledge/**/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

// 「外部资源」主题页（REQ-109/162）：内容单源仍在 seeds/learning/external-resources.md，构建期内联挂载
import EXTERNAL_RESOURCES_MD from '../../../seeds/learning/external-resources.md?raw'

/** 模块注册表：目录名 → 页面显示名 + 图标；顺序即页面 L1 顺序（platform-knowledge/README.md 同源维护） */
const MODULES: { dir: string; label: string; icon: ReactNode }[] = [
  { dir: '总览', label: '平台总览', icon: <CompassOutlined /> },
  { dir: '智能体', label: '智能体', icon: <RobotOutlined /> },
  { dir: '项目', label: '项目', icon: <ProjectOutlined /> },
  { dir: '本体', label: '本体', icon: <ApartmentOutlined /> },
  { dir: '知识库', label: '知识库', icon: <DatabaseOutlined /> },
  { dir: '技能', label: '技能', icon: <ThunderboltOutlined /> },
  { dir: '设置', label: '设置', icon: <SettingOutlined /> },
  { dir: 'DeepSeek-Harness', label: 'DeepSeek Harness', icon: <BookOutlined /> },
  { dir: '产品设计', label: '产品设计', icon: <HighlightOutlined /> },
]

interface Topic {
  key: string
  title: string
  md: string
  /** 主题页所在仓库目录——正文相对引用的解析基准（REQ-161 补充：文档互引用相对路径） */
  base: string
  group: string
  groupLabel: string
  icon: ReactNode
}

const TOPICS: Topic[] = (() => {
  const out: Topic[] = []
  for (const [path, raw] of Object.entries(KB_RAW)) {
    const marker = 'platform-knowledge/'
    const idx = path.indexOf(marker)
    if (idx < 0) continue
    const rel = path.slice(idx + marker.length)
    const slash = rel.indexOf('/')
    if (slash < 0) continue // 根级 README.md 等不进页面
    const dir = rel.slice(0, slash)
    const file = rel.slice(slash + 1).replace(/\.md$/, '')
    const mod = MODULES.find((m) => m.dir === dir)
    if (!mod) continue
    out.push({ key: `${dir}/${file}`, title: file, md: raw, base: `platform-knowledge/${dir}`, group: dir, groupLabel: mod.label, icon: mod.icon })
  }
  out.push({
    key: '外部资源/外部资源导航',
    title: '外部资源导航',
    md: EXTERNAL_RESOURCES_MD,
    base: 'seeds/learning',
    group: '外部资源',
    groupLabel: '外部资源',
    icon: <LinkOutlined />,
  })
  return out
})()

interface Frontmatter {
  module?: string
  topic?: string
  desc?: string
  req?: string[]
  docs?: string[]
  decisions?: string[]
  synced?: string
}

const DOC_FILE_BY_NO: Record<string, string> = {
  '01': 'docs/01_智能体_需求文档_PRD.md',
  '02': 'docs/02_智能体_技术方案设计.md',
  '03': 'docs/03_本体_需求文档.md',
  '04': 'docs/04_本体_方案设计.md',
  '11': 'docs/11_知识库_需求文档.md',
  '12': 'docs/12_知识库_方案设计.md',
  '14': 'docs/14_本体_前端改造方案.md',
  '16': 'docs/16_部署与运行.md',
  '17': 'platform-knowledge/产品设计/17_产品_信息架构与界面设计.md',
  '18': 'docs/18_REQ编号注册表.md',
  '19': 'platform-knowledge/本体/19_本体_semantica集成方案.md',
  '20': 'docs/20_回归冒烟清单.md',
  '21': 'platform-knowledge/知识库/21_知识库能力增强调研.md',
  '22': 'platform-knowledge/智能体/22_多类型智能体方案研究.md',
  '23': 'platform-knowledge/本体/23_本体_开源实现方案借鉴研究.md',
  '24': 'platform-knowledge/本体/24_本体_本地工程化落地方案调研.md',
  '25': 'platform-knowledge/知识库/25_Agent知识库与知识图谱构建接入方案.md',
}
/** 相对引用解析（REQ-161 补充要求：文档互引用相对路径）——按主题页所在仓库目录解析，返回仓库相对 .md 路径或 null */
export function resolveRef(href: string, base: string): string | null {
  const h = href.trim()
  if (!h || h.startsWith('http://') || h.startsWith('https://') || h.startsWith('#') || h.startsWith('mailto:')) return null
  if (h.startsWith('/')) return null
  const joined = /^(platform-knowledge|docs|research|seeds)\//.test(h) ? h : `${base}/${h}`
  const parts: string[] = []
  for (const seg of joined.split('/')) {
    if (seg === '' || seg === '.') continue
    if (seg === '..') parts.pop()
    else parts.push(seg)
  }
  const out = parts.join('/')
  return out.endsWith('.md') ? out : null
}

export function docFileOf(ptr: string): string | null {
  const s = ptr.trim()
  // 完整路径直传（docs/ platform-knowledge/ research/ 下的 .md）
  if (s.endsWith('.md') && (s.startsWith('docs/') || s.startsWith('research/') || s.startsWith('platform-knowledge/'))) return s
  // 编号指针（如 "02" / "02 §6.4" / "17 §2.2"）：取前两位编号映射
  const no = s.slice(0, 2)
  return DOC_FILE_BY_NO[no] ?? null
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
function SourceMap({ meta, onOpenDoc }: { meta: Frontmatter; onOpenDoc: (path: string) => void }) {
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
            {r.items.map((it) => {
              const file = docFileOf(it)
              const clickable = r.label !== '需求编号' && file
              return clickable ? (
                <Tag
                  key={it}
                  style={{ marginInlineEnd: 6, cursor: 'pointer', color: '#4f46e5', borderColor: '#4f46e5' }}
                  onClick={() => onOpenDoc(file)}
                >
                  {it} · 点击查看
                </Tag>
              ) : (
                <Tag key={it} style={{ marginInlineEnd: 6 }}>
                  {it}
                </Tag>
              )
            })}
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
  const [active, setActive] = useState('总览/平台总览')
  const [viewDoc, setViewDoc] = useState<string | null>(null) // REQ-140/161：点击引用相对路径 → 右侧 Drawer 阅读
  const topic = TOPICS.find((t) => t.key === active) ?? TOPICS[0]
  const { meta, body } = useMemo(() => parseFrontmatter(topic.md), [topic])
  const groups = useMemo(() => {
    const m = new Map<string, Topic[]>()
    for (const t of TOPICS) {
      const arr = m.get(t.group)
      if (arr) arr.push(t)
      else m.set(t.group, [t])
    }
    return [...m.entries()].map(([g, ts]) => {
      const mod = MODULES.find((x) => x.dir === g)
      return { group: g, label: mod?.label ?? g, icon: mod?.icon ?? <LinkOutlined />, topics: ts }
    })
  }, [])
  /** 正文相对引用点击（REQ-161 补充要求）：拦截指向仓库内 .md 的相对路径 → 右侧 Drawer 阅读；http/锚点走默认 */
  const onBodyClick = (e: MouseEvent) => {
    const a = (e.target as HTMLElement).closest?.('a')
    if (!a) return
    const resolved = resolveRef(a.getAttribute('href') ?? '', topic.base)
    if (!resolved) return
    e.preventDefault()
    setViewDoc(resolved)
  }
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
            <span className="side-title">平台知识</span>
          </div>
          <Menu
            mode="inline"
            selectedKeys={[active]}
            defaultOpenKeys={groups.map((g) => g.group)}
            onClick={({ key }) => setActive(String(key))}
            style={{ padding: '0 10px', background: 'transparent' }}
            items={groups.map((g) => ({
              key: g.group,
              icon: g.icon,
              label: g.label,
              children: g.topics.map((t) => ({ key: t.key, label: t.title })),
            }))}
          />
          <div className="settings-note">
            目录即页面结构（
            <Typography.Text code style={{ fontSize: 11 }}>platform-knowledge/</Typography.Text>
            ）；L1 模块 / L2 主题页，新增或修改文档后重建即生效。
          </div>
        </aside>
      </Splitter.Panel>
      <Splitter.Panel className="content-panel">
        <div className="ref-main">
          <div className="settings-head">
            <Typography.Title level={5} style={{ marginTop: 0, marginBottom: 4 }}>
              <Space>{topic.icon}{topic.groupLabel}<Typography.Text type="secondary">/ {topic.title}</Typography.Text></Space>
            </Typography.Title>
            {meta.desc || meta.topic ? (
              <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
                {meta.desc || meta.topic}
              </Typography.Paragraph>
            ) : (
              <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
                产品定位 → 设计原理 → 相关资料；权威全文见源文档地图与 docs/ 对应文档编号。
              </Typography.Paragraph>
            )}
          </div>
          <div className="ref-body" onClick={onBodyClick}>
            <XMarkdown content={body} openLinksInNewTab />
            <SourceMap meta={meta} onOpenDoc={setViewDoc} />
          </div>
        </div>
      </Splitter.Panel>
      <DocViewerModal path={viewDoc} open={!!viewDoc} onClose={() => setViewDoc(null)} />
    </Splitter>
  )
}
