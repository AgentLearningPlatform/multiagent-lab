import { useEffect, useState, type MouseEvent } from 'react'
import { Drawer, Spin, Typography } from 'antd'
import { api } from '../api/client'
import XMarkdown from '@ant-design/x-markdown'
import { resolveRef } from '../lib/docref'
import { DRAWER_SIZES, drawerSizeProps } from '../lib/layout'

/**
 * 内部方案文档只读查看（REQ-140/169）：点击界面中链接的 docs/ 等文档指针 →
 * 后端只读读取（fsutil 限白名单目录内 .md）→ 弹层 Markdown 渲染。不要求在线编辑。
 * REQ-169 交付轮（2026-09-26）：默认关闭、点开才挂载、可手动关闭；
 * 正文内互引相对链接同样可点——按当前文档所在目录解析，抽屉内直接切换到目标文档。
 */
export default function DocViewerModal({
  path,
  open,
  onClose,
  onNavigate,
}: {
  /** 仓库相对路径，如 docs/01_智能体_需求文档_PRD.md */
  path: string | null
  open: boolean
  onClose: () => void
  /** 正文内相对引用点击 → 切换到目标文档（缺省时正文链接不拦截） */
  onNavigate?: (path: string) => void
}) {
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    if (!open || !path) return
    setLoading(true)
    setErr(null)
    api
      .docRead(path)
      .then((r) => {
        setTitle(r.title)
        setContent(r.content)
      })
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false))
  }, [open, path])

  const onBodyClick = (e: MouseEvent) => {
    if (!path || !onNavigate) return
    const a = (e.target as HTMLElement).closest?.('a')
    if (!a) return
    const base = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : ''
    const resolved = resolveRef(a.getAttribute('href') ?? '', base)
    if (!resolved) return
    e.preventDefault()
    onNavigate(resolved)
  }

  // platform-knowledge/ 主题页头部有 frontmatter 元信息块，阅读视图与主文一致将其剥去（docs/ 文档无该块不受影响）
  const displayContent = content.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n?/, '')

  return (
    <Drawer open={open} onClose={onClose} {...drawerSizeProps('docviewer', DRAWER_SIZES.large)} title={title ? `📄 ${title}` : '文档查看'}>
      <div onClick={onBodyClick}>
        {loading ? (
          <Spin size="small" />
        ) : err ? (
          <Typography.Text type="danger">{err}</Typography.Text>
        ) : (
          <XMarkdown className="chat-md" openLinksInNewTab>{displayContent}</XMarkdown>
        )}
      </div>
    </Drawer>
  )
}
