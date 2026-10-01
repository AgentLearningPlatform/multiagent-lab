import { lazy, Suspense, useEffect, useState } from 'react'
import { GithubOutlined } from '@ant-design/icons'
import { Spin } from 'antd'
import HomePage from './pages/Home'
import ModulePage from './pages/ModulePage'
import { MODULES, REPO_SLUG, REPO_URL } from './content'

// 演示页懒加载：three.js/react-force-graph 体量大（主包 ~3MB→首屏 ~1MB），首访演示页才拉取
const DemoPage = lazy(() => import('./pages/DemoPage'))

// ---------------------------------------------------------------------------
// 官网壳（REQ-238）：hash 路由（GitHub Pages 子路径静态托管免 404 配置）
//   #/                首页（项目概览）
//   #/module/<key>    模块详细介绍（platform-knowledge 导读页渲染）
//   #/demo            内置演示数据（种子本体可视化）
// 右上角 GitHub 仓库链接常驻。
// ---------------------------------------------------------------------------

function useHashRoute(): string {
  const [hash, setHash] = useState(() => window.location.hash || '#/')
  useEffect(() => {
    const on = () => setHash(window.location.hash || '#/')
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  return hash
}

/** 品牌标记：与平台 TopNav 同源的三节点网络 SVG */
function BrandMark({ size = 20 }: { size?: number }) {
  return (
    <svg viewBox="0 0 24 24" width={size} height={size} fill="none" aria-hidden="true">
      <path d="M12 6.6 6.4 15.5M12 6.6l5.6 8.9M7.4 16.2h9.2" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" />
      <circle cx="12" cy="5.2" r="2.5" fill="currentColor" />
      <circle cx="5.6" cy="16.6" r="2.2" fill="currentColor" opacity="0.86" />
      <circle cx="18.4" cy="16.6" r="2.2" fill="currentColor" opacity="0.86" />
    </svg>
  )
}

const NAVS: { hash: string; label: string; match: (h: string) => boolean }[] = [
  { hash: '#/', label: '首页', match: (h) => h === '#/' || h === '' },
  { hash: '#/module/agents', label: '功能模块', match: (h) => h.startsWith('#/module/') },
  { hash: '#/demo', label: '在线演示', match: (h) => h.startsWith('#/demo') },
]

function Body({ route }: { route: string }) {
  const moduleMatch = route.match(/^#\/module\/([\w-]+)/)
  if (moduleMatch) {
    const mod = MODULES.find((m) => m.key === moduleMatch[1])
    return mod ? <ModulePage info={mod} /> : <NotFound />
  }
  if (route.startsWith('#/demo'))
    return (
      <Suspense
        fallback={
          <main className="page">
            <div className="page-inner" style={{ display: 'flex', justifyContent: 'center', padding: 80 }}>
              <Spin size="large" />
            </div>
          </main>
        }
      >
        <DemoPage />
      </Suspense>
    )
  return <HomePage />
}

function NotFound() {
  return (
    <main className="page">
      <div className="page-inner">
        <h1>页面不存在</h1>
        <p>
          <a href="#/">返回首页</a>
        </p>
      </div>
    </main>
  )
}

export default function App() {
  const route = useHashRoute()
  return (
    <div className="site">
      <header className="topnav">
        <a className="brand" href="#/">
          <span className="brand-mark">
            <BrandMark />
          </span>
          <span className="brand-text">
            <span className="brand-name">AgentLab</span>
            <span className="brand-sub">智能体构建平台</span>
          </span>
        </a>
        <nav className="topnav-nav" aria-label="主导航">
          {NAVS.map((n) => (
            <a key={n.hash} className={`nav-item${n.match(route) ? ' active' : ''}`} href={n.hash}>
              {n.label}
            </a>
          ))}
        </nav>
        <span className="topnav-spacer" />
        <a className="gh-link" href={REPO_URL} target="_blank" rel="noreferrer" aria-label="GitHub 仓库" title={`github.com/${REPO_SLUG}`}>
          <GithubOutlined />
          <span className="gh-text">GitHub</span>
        </a>
      </header>

      <Body route={route} />

      <footer className="footer">
        <div className="footer-inner">
          <span>
            <BrandMark size={14} /> AgentLab · 智能体构建平台 — Apache License 2.0
          </span>
          <span className="footer-links">
            <a href={REPO_URL} target="_blank" rel="noreferrer">
              代码仓库
            </a>
            {' · '}
            <a href="#/module/design">整体设计</a>
            {' · '}
            <a href="#/demo">在线演示</a>
          </span>
        </div>
      </footer>
    </div>
  )
}
