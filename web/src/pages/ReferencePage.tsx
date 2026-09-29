import { useMemo, useState, type MouseEvent, type ReactNode } from 'react'
import { Card, Menu, Space, Splitter, Tag, Typography } from 'antd'
import {
  ApartmentOutlined,
  FileTextOutlined,
  CompassOutlined,
  DatabaseOutlined,
  LinkOutlined,
  ProjectOutlined,
  RobotOutlined,
  SettingOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import XMarkdown from '@ant-design/x-markdown'
import DocViewerModal from '../components/DocViewerModal'
import { docFileOf, resolveRef } from '../lib/docref'

// 平台知识内容（REQ-116 / REQ-161 v2）：构建期内联扫描 platform-knowledge/ 全目录。
// REQ-188 起全链目录驱动：目录即页面树——顶层目录=L1 组、子目录=L2、文档=主题页；
// 组序/组内序均由名称数字前缀（NN_）决定，新增目录/文档落入即上页并自动入位，无需改本文件；
// 未登记的新顶层目录兜底成组置尾（不丢档）。个别跨目录挂载见 TOPIC_MOUNT。
const KB_RAW = import.meta.glob('../../../platform-knowledge/**/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

// REQ-184：docs/ 十编号文档挂载为 L1「需求与进度」组（需求/方案/治理/冒烟事实源直达）
const DOCS_RAW = import.meta.glob('../../../docs/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

// 「外部资源」主题页（REQ-109/162）：内容单源仍在 seeds/learning/external-resources.md，构建期内联挂载
import EXTERNAL_RESOURCES_MD from '../../../seeds/learning/external-resources.md?raw'

/** 图标表（可选定制，键 = 去前缀目录名；未登记目录用默认图标，补一行即生效） */
/** REQ-184：docs 组显示名（目录驱动保持，仅展示层覆盖） */
const GROUP_LABELS: Record<string, string> = { docs: '需求与进度' }
/** REQ-184：docs 组图标 */
const GROUP_ICONS_EXT: Record<string, ReactNode> = { docs: <FileTextOutlined /> }
/** REQ-184：docs 文件友好标题（编号文档名 → 页面标题） */
const DOCS_TITLES: Record<string, string> = {
  '01_智能体_需求文档_PRD': '智能体需求文档（PRD）',
  '02_智能体_技术方案设计': '智能体技术方案设计',
  '03_本体_需求文档': '本体需求文档',
  '04_本体_方案设计': '本体方案设计',
  '11_知识库_需求文档': '知识库需求文档',
  '12_知识库_方案设计': '知识库方案设计',
  '14_本体_前端改造方案': '本体前端改造方案',
  '15_开源项目及论文登记簿': '开源项目及论文登记簿',
  '18_REQ编号注册表': 'REQ 编号注册表',
  '20_回归冒烟清单': '回归冒烟清单',
}

const GROUP_ICONS: Record<string, ReactNode> = {
  整体设计: <CompassOutlined />,
  智能体: <RobotOutlined />,
  项目: <ProjectOutlined />,
  本体: <ApartmentOutlined />,
  知识库: <DatabaseOutlined />,
  技能: <ThunderboltOutlined />,
  设置: <SettingOutlined />,
}
const DEFAULT_ICON = <LinkOutlined />

/** 目录/文件名前缀解析：`NN_名称` → { order, label }；无前缀 order=Infinity（排序置尾） */
function parsePrefix(name: string): { order: number; label: string } {
  const m = /^(\d+)_(.+)$/.exec(name)
  return m ? { order: Number(m[1]), label: m[2] } : { order: Number.POSITIVE_INFINITY, label: name }
}

/** 跨目录挂载（REQ-169 二轮，开发者指定）：键=主题 key（存放目录/文件），value=挂载的 L1 组 + 展示名。
 *  仅改页面归属与标题；互引解析基准 base 仍按真实存放目录，保证文内相对链接不失效 */
const TOPIC_MOUNT: Record<string, { group: string; label: string }> = {
  '外部资源/外部资源导航': { group: '本体', label: '本体学习外部资源导航' },
}

interface Topic {
  key: string
  /** 文件路径（目录下相对，保留编号前缀，供排序） */
  file: string
  /** 展示标题：去 `NN_`/`NN-` 前缀，`_`转间隔点；模块主页归一「模块导读」 */
  title: string
  md: string
  /** 主题页所在仓库目录——正文相对引用的解析基准（REQ-161 补充：文档互引用相对路径） */
  base: string
  group: string
  groupLabel: string
  icon: ReactNode
  isHome: boolean
  /** 组排序值（原始顶层目录的数字前缀；挂载主题随目标组） */
  groupOrder: number
}

/** 文件名 → 展示标题（通用规则，新文档落目录即自动获得可读标题） */
function topicTitle(file: string, groupLabel: string): string {
  const baseName = file.slice(file.lastIndexOf('/') + 1)
  const m = /^\d+[_-](.+)$/.exec(baseName)
  let base = m ? m[1] : baseName
  if (base === groupLabel) return base // 组主页（如 00_整体设计）
  if (base === `${groupLabel}模块`) return '模块导读' // 模块导读（如 00_本体模块）
  return base.replace(/_/g, '·')
}

const TOPICS: Topic[] = (() => {
  const out: Topic[] = []
  for (const [path, raw] of Object.entries({ ...KB_RAW, ...DOCS_RAW })) {
    const marker = 'platform-knowledge/'
    const docsMarker = '/docs/'
    const di = path.indexOf(docsMarker)
    const idx = path.indexOf(marker)
    if (idx < 0 && di < 0) continue
    const rel = idx >= 0
      ? path.slice(idx + marker.length)
      : 'docs/' + path.slice(di + docsMarker.length)
    const slash = rel.indexOf('/')
    if (slash < 0) continue // 根级 README.md 等不进页面
    const dir = rel.slice(0, slash)
    const file = rel.slice(slash + 1).replace(/\.md$/, '')
    const { label } = parsePrefix(dir)
    const key = `${dir}/${file}`
    // 跨目录挂载（TOPIC_MOUNT）：归属组/展示名/图标随目标组，base 保持真实存放目录
    const mount = TOPIC_MOUNT[key]
    const effGroup = mount?.group ?? label
    const docTitle = dir === 'docs' ? DOCS_TITLES[file] : undefined
    out.push({
      key,
      file,
      title: mount?.label ?? docTitle ?? topicTitle(file, label),
      md: raw,
      base: `platform-knowledge/${dir}/${file}`.split('/').slice(0, -1).join('/'),
      group: effGroup,
      groupLabel: GROUP_LABELS[mount?.group ?? label] ?? mount?.group ?? label,
      icon: GROUP_ICONS[mount?.group ?? label] ?? GROUP_ICONS_EXT[mount?.group ?? label] ?? DEFAULT_ICON,
      isHome: /^00[_-]/.test(file) || file === `${label}模块`,
      groupOrder: parsePrefix(dir).order,
    })
  }
  // 外部资源主题页（seeds 单源）按 TOPIC_MOUNT 挂载到目标组
  const extMount = TOPIC_MOUNT['外部资源/外部资源导航']
  out.push({
    key: '外部资源/外部资源导航',
    file: '外部资源导航',
    title: extMount?.label ?? '外部资源导航',
    md: EXTERNAL_RESOURCES_MD,
    base: 'seeds/learning',
    group: extMount?.group ?? '外部资源',
    groupLabel: extMount?.group ?? '外部资源',
    icon: GROUP_ICONS[extMount?.group ?? ''] ?? DEFAULT_ICON,
    isHome: false,
    groupOrder: Number.POSITIVE_INFINITY,
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

/** 组内排序：组主页置顶（00_ 前缀/模块导读）→ 有数字前缀者按编号升序（兼容 NN_/NN-）→ 无前缀者按标题（zh）殿后 */
function compareTopics(a: Topic, b: Topic): number {
  if (a.isHome !== b.isHome) return a.isHome ? -1 : 1
  const aBase = a.file.slice(a.file.lastIndexOf('/') + 1)
  const bBase = b.file.slice(b.file.lastIndexOf('/') + 1)
  const na = /^(\d+)[_-]/.exec(aBase)?.[1]
  const nb = /^(\d+)[_-]/.exec(bBase)?.[1]
  const va = na ? Number(na) : Number.POSITIVE_INFINITY
  const vb = nb ? Number(nb) : Number.POSITIVE_INFINITY
  if (va !== vb) return va - vb
  return a.title.localeCompare(b.title, 'zh-Hans-CN')
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

/** L1 分组：按顶层目录自动派生（REQ-188）——前缀序 → 未登记/无前缀目录兜底置尾（不丢档） */
const GROUPS = (() => {
  const m = new Map<string, Topic[]>()
  for (const t of TOPICS) {
    const arr = m.get(t.group)
    if (arr) arr.push(t)
    else m.set(t.group, [t])
  }
  return [...m.entries()]
    .map(([g, topics]) => ({
      group: g,
      label: GROUP_LABELS[g] ?? g,
      icon: GROUP_ICONS[g] ?? GROUP_ICONS_EXT[g] ?? DEFAULT_ICON,
      order: Math.min(...topics.map((t) => t.groupOrder)),
      topics: topics.sort(compareTopics),
    }))
    .sort((a, b) => a.order - b.order || a.label.localeCompare(b.label, 'zh-Hans-CN'))
})()

/** 默认选中：首组主页（修复历史遗留的失效 key「总览/平台总览」） */
const DEFAULT_ACTIVE = GROUPS[0]?.topics.find((t) => t.isHome)?.key ?? GROUPS[0]?.topics[0]?.key ?? ''

export default function ReferencePage() {
  const [active, setActive] = useState(DEFAULT_ACTIVE)
  const [viewDoc, setViewDoc] = useState<string | null>(null) // REQ-169：点击互引相对路径 → 右侧 Drawer 阅读，默认关闭
  const topic = TOPICS.find((t) => t.key === active) ?? TOPICS[0]
  const { meta, body } = useMemo(() => parseFrontmatter(topic.md), [topic])
  /** 正文相对引用点击（REQ-169）：拦截指向仓库内 .md 的相对路径 → 右侧 Drawer 阅读；http/锚点走默认 */
  const onBodyClick = (e: MouseEvent) => {
    const a = (e.target as HTMLElement).closest?.('a')
    if (!a) return
    const resolved = resolveRef(a.getAttribute('href') ?? '', topic.base)
    if (!resolved) return
    e.preventDefault()
    setViewDoc(resolved)
  }
  return (
    <>
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
          <div className="ref-menu">
            <Menu
              mode="inline"
              selectedKeys={[active]}
              defaultOpenKeys={[]}
              onClick={({ key }) => setActive(String(key))}
              style={{ background: 'transparent' }}
              items={GROUPS.map((g) =>
                g.topics.length === 1
                  ? { key: g.topics[0].key, icon: g.icon, label: g.label }
                  : {
                      key: g.group,
                      icon: g.icon,
                      label: g.label,
                      children: (() => {
                        const roots = g.topics.filter((t) => !t.file.includes('/'))
                        const subMap = new Map<string, Topic[]>()
                        for (const t of g.topics) {
                          if (!t.file.includes('/')) continue
                          const sub = t.file.slice(0, t.file.indexOf('/'))
                          if (!subMap.has(sub)) subMap.set(sub, [])
                          subMap.get(sub)!.push(t)
                        }
                        const items: any = roots.map((t) => ({ key: t.key, label: t.title }))
                        // 子目录按数字前缀排序（REQ-188；无前缀按名置尾）
                        const subs = [...subMap.keys()].sort((a, b) => {
                          const pa = parsePrefix(a)
                          const pb = parsePrefix(b)
                          return pa.order - pb.order || pa.label.localeCompare(pb.label, 'zh-Hans-CN')
                        })
                        for (const sub of subs) {
                          items.push({ key: `${g.group}/${sub}`, label: parsePrefix(sub).label, children: subMap.get(sub)!.map((t) => ({ key: t.key, label: t.title })) })
                        }
                        return items
                      })(),
                    },
              )}
            />
          </div>
          <div className="settings-note">
            内容收录目录驱动（
            <Typography.Text code style={{ fontSize: 11 }}>platform-knowledge/</Typography.Text>
            ）；新增目录/文档按数字前缀落入后重建即生效（REQ-188）。
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
    </Splitter>
    {/* 抽屉必须挂在 Splitter 之外：AntD Splitter 只认 Splitter.Panel 子元素，
        混入其他组件会被吞成一个空白面板（REQ-169「右侧空白栏/链接无反应」的根因） */}
    <DocViewerModal path={viewDoc} open={!!viewDoc} onClose={() => setViewDoc(null)} onNavigate={setViewDoc} />
    </>
  )
}
