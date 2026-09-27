import { useMemo, useState, type MouseEvent, type ReactNode } from 'react'
import { Card, Menu, Space, Splitter, Tag, Typography } from 'antd'
import {
  ApartmentOutlined,
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

// 平台知识内容（REQ-116 / REQ-161 v2）：构建期内联扫描 platform-knowledge/ 全目录，
// 主题页发现仍是目录驱动（新增或迁移文档后重建即生效）；但页面组织不照搬存放顺序——
// L1 分组按 GROUP_ORDER 规划（导航栏模块及其顺序，整体设计置顶、设置殿后）；目录=页面树 1:1（G-5 增补），
// 个别专题跨目录挂载见 TOPIC_MOUNT（REQ-169 二轮，2026-09-26 开发者指定）；
// 组内主页置顶（模块导读/平台总览）、专题按文档编号序。
// 每篇头部 frontmatter（module/topic/desc/req/docs/decisions/synced）为页面元信息与源指针约定，
// 语义级变更（REQ 行/决策/口径）须同步更新命中的文档（AGENTS.md 纪律 7）。
const KB_RAW = import.meta.glob('../../../platform-knowledge/**/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

// 「外部资源」主题页（REQ-109/162）：内容单源仍在 seeds/learning/external-resources.md，构建期内联挂载
import EXTERNAL_RESOURCES_MD from '../../../seeds/learning/external-resources.md?raw'

/** 模块注册表：目录名 → 页面显示名 + 图标（platform-knowledge/README.md 同源维护） */
const MODULES: { dir: string; label: string; icon: ReactNode }[] = [
  { dir: '整体设计', label: '整体设计', icon: <CompassOutlined /> },
  { dir: '智能体', label: '智能体', icon: <RobotOutlined /> },
  { dir: '项目', label: '项目', icon: <ProjectOutlined /> },
  { dir: '本体', label: '本体', icon: <ApartmentOutlined /> },
  { dir: '知识库', label: '知识库', icon: <DatabaseOutlined /> },
  { dir: '技能', label: '技能', icon: <ThunderboltOutlined /> },
  { dir: '设置', label: '设置', icon: <SettingOutlined /> },
]

/** L1 分组顺序（规划态，非目录存放顺序）：平台总览置顶为入口 → 五业务模块按导航栏顺序 →
 *  设置对应导航栏最右齿轮殿后；跨目录挂载的专题随目标组出现，不单列 L1 */
const GROUP_ORDER = ['整体设计', '智能体', '项目', '本体', '知识库', '技能', '设置']

/** 跨目录挂载（REQ-169 二轮，开发者指定）：键=主题 key（存放目录/文件），value=挂载的 L1 组 + 展示名。
 *  仅改页面归属与标题；互引解析基准 base 仍按真实存放目录，保证文内相对链接不失效 */
const TOPIC_MOUNT: Record<string, { group: string; label: string }> = {
  '外部资源/外部资源导航': { group: '本体', label: '本体学习外部资源导航' },
}

/** 各组主页文件（组内置顶；默认 `${dir}模块` 即「模块导读」，总览组为 平台总览） */
const HOME_FILE: Record<string, string> = { 整体设计: '整体设计' }

interface Topic {
  key: string
  /** 文件名（不含 .md，保留编号前缀，供组内排序） */
  file: string
  /** 展示标题：去编号前缀/模块同名前缀，「X模块」归一为「模块导读」，下划线转间隔点 */
  title: string
  md: string
  /** 主题页所在仓库目录——正文相对引用的解析基准（REQ-161 补充：文档互引用相对路径） */
  base: string
  group: string
  groupLabel: string
  icon: ReactNode
}

/** 文件名 → 展示标题（通用规则，新文档落目录即自动获得可读标题） */
function topicTitle(dir: string, file: string): string {
  if (file === `${dir}模块`) return '模块导读'
  let t = file.replace(/^\d+_/, '')
  if (t.startsWith(`${dir}_`)) t = t.slice(dir.length + 1)
  return t.replace(/_/g, '·')
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
    const key = `${dir}/${file}`
    // 跨目录挂载（TOPIC_MOUNT）：归属组/展示名/图标随目标组，base 保持真实存放目录
    const mount = TOPIC_MOUNT[key]
    const effGroup = mount?.group ?? dir
    const effMod = MODULES.find((m) => m.dir === effGroup) ?? mod
    out.push({
      key,
      file,
      title: mount?.label ?? topicTitle(dir, file),
      md: raw,
      base: `platform-knowledge/${dir}/${file}`.split('/').slice(0, -1).join('/'),
      group: effGroup,
      groupLabel: effMod.label,
      icon: effMod.icon,
    })
  }
  // 外部资源主题页（seeds 单源）按 TOPIC_MOUNT 挂载到目标组（二轮起挂本体组）
  const extMount = TOPIC_MOUNT['外部资源/外部资源导航']
  const extMod = MODULES.find((m) => m.dir === extMount?.group)
  out.push({
    key: '外部资源/外部资源导航',
    file: '外部资源导航',
    title: extMount?.label ?? '外部资源导航',
    md: EXTERNAL_RESOURCES_MD,
    base: 'seeds/learning',
    group: extMount?.group ?? '外部资源',
    groupLabel: extMod?.label ?? '外部资源',
    icon: extMod?.icon ?? <LinkOutlined />,
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

/** 组内排序：组主页置顶（模块导读/平台总览）→ 有文档编号者按编号升序 → 无编号者按标题（zh）排在编号文档之后 */
function compareTopics(a: Topic, b: Topic): number {
  const ga = isGroupHome(a) ? 0 : 1
  const gb = isGroupHome(b) ? 0 : 1
  if (ga !== gb) return ga - gb
  const na = /^(\d+)_/.exec(a.file)?.[1]
  const nb = /^(\d+)_/.exec(b.file)?.[1]
  const va = na ? Number(na) : Number.POSITIVE_INFINITY
  const vb = nb ? Number(nb) : Number.POSITIVE_INFINITY
  if (va !== vb) return va - vb
  return a.title.localeCompare(b.title, 'zh-Hans-CN')
}

function isGroupHome(t: Topic): boolean {
  return t.title === '模块导读' || t.file === (HOME_FILE[t.group] ?? `${t.group}模块`)
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
  const [viewDoc, setViewDoc] = useState<string | null>(null) // REQ-169：点击互引相对路径 → 右侧 Drawer 阅读，默认关闭
  const topic = TOPICS.find((t) => t.key === active) ?? TOPICS[0]
  const { meta, body } = useMemo(() => parseFrontmatter(topic.md), [topic])
  const groups = useMemo(() => {
    const m = new Map<string, Topic[]>()
    for (const t of TOPICS) {
      const arr = m.get(t.group)
      if (arr) arr.push(t)
      else m.set(t.group, [t])
    }
    // 规划序在前；未来新增目录若未登记 GROUP_ORDER，按模块注册表序兜底追加（目录驱动不丢组）
    const ordered = GROUP_ORDER.filter((g) => m.has(g))
    const rest = MODULES.map((x) => x.dir).filter((d) => m.has(d) && !GROUP_ORDER.includes(d))
    return [...ordered, ...rest].map((g) => {
      const mod = MODULES.find((x) => x.dir === g)
      const topics = (m.get(g) ?? []).sort(compareTopics)
      return { group: g, label: mod?.label ?? g, icon: mod?.icon ?? <LinkOutlined />, topics }
    })
  }, [])
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
              items={groups.map((g) =>
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
                        for (const sub of [...subMap.keys()].sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))) {
                          items.push({ key: `${g.group}/${sub}`, label: sub, children: subMap.get(sub)!.map((t) => ({ key: t.key, label: t.title })) })
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
            ）；页面组织按导航模块规划，新增文档落入模块目录后重建即生效。
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
