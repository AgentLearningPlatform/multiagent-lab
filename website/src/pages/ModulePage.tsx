import { useMemo } from 'react'
import { LinkOutlined } from '@ant-design/icons'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { MODULES, REPO_URL, resolveMdLink, stripFrontmatter, type ModuleInfo } from '../content'

// ---------------------------------------------------------------------------
// 模块详细介绍页（REQ-238）：内容 = platform-knowledge/<模块>/00_导读页.md 构建期内联
// （单源：内容只在平台知识目录维护，站点随构建自动跟随）。导读页内的相对 md 互引
// 解析为 GitHub 源文档链接（静态站不落盘全部文档，深入阅读回到仓库）。
// ---------------------------------------------------------------------------

export default function ModulePage({ info }: { info: ModuleInfo }) {
  const body = useMemo(() => stripFrontmatter(info.md), [info.md])
  const sourceUrl = `${REPO_URL}/blob/main/${info.path}`
  const idx = MODULES.findIndex((m) => m.key === info.key)
  const prev = MODULES[idx - 1]
  const next = MODULES[idx + 1]

  return (
    <main className="page">
      <div className="page-inner">
        <nav className="crumbs">
          <a href="#/module/agents">功能模块</a>
          {' / '}
          <span>{info.title}</span>
        </nav>
        <h1>{info.title}</h1>
        <p className="module-tagline">{info.tagline}</p>
        <div className="module-source">
          <a href={sourceUrl} target="_blank" rel="noreferrer">
            <LinkOutlined /> 在 GitHub 查看源文档（platform-knowledge/{info.path.split('/').pop()}）
          </a>
        </div>
        <article className="md-body md-cards-on">
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            components={{
              a: ({ href, children }) => {
                const resolved = href ? resolveMdLink(href, info.path) : null
                if (resolved) {
                  return (
                    <a href={resolved} target="_blank" rel="noreferrer">
                      {children}
                    </a>
                  )
                }
                return <a href={href}>{children}</a>
              },
            }}
          >
            {body}
          </ReactMarkdown>
        </article>
        <nav className="module-pager">
          {prev ? (
            <a href={`#/module/${prev.key}`}>← {prev.title}</a>
          ) : (
            <span />
          )}
          {next ? <a href={`#/module/${next.key}`}>{next.title} →</a> : <span />}
        </nav>
      </div>
    </main>
  )
}
