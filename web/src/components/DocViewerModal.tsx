import { useEffect, useState } from 'react'
import { Drawer, Spin, Typography } from 'antd'
import { api } from '../api/client'
import XMarkdown from '@ant-design/x-markdown'

/**
 * 内部方案文档只读查看（REQ-140）：点击界面中链接的 docs/ 文档指针 →
 * 后端只读读取（fsutil 限 docs/ 内 .md）→ 弹层 Markdown 渲染。不要求在线编辑。
 */
export default function DocViewerModal({
  path,
  open,
  onClose,
}: {
  /** docs/ 下相对路径，如 docs/01_智能体_需求文档_PRD.md */
  path: string | null
  open: boolean
  onClose: () => void
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

  return (
    <Drawer open={open} onClose={onClose} width={820} title={title ? `📄 ${title}` : '文档查看'}>
      {loading ? (
        <Spin size="small" />
      ) : err ? (
        <Typography.Text type="danger">{err}</Typography.Text>
      ) : (
        <XMarkdown className="chat-md">{content}</XMarkdown>
      )}
    </Drawer>
  )
}
