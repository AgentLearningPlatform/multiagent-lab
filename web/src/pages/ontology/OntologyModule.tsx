import { useEffect, useState } from 'react'
import { Splitter, Tooltip } from 'antd'
import {
  ApartmentOutlined,
  BookOutlined,
  CloudServerOutlined,
  DatabaseOutlined,
} from '@ant-design/icons'
import LearnPage from './LearnPage'
import BuildPage from './BuildPage'
import AssetsPage from './AssetsPage'
import RuntimePage from './RuntimePage'

// ---------------------------------------------------------------------------
// 本体模块壳（D-O11 / REQ-104）：左侧边栏四栏
//   学习中心（默认页）| 本体构建（五路径）| 本体资产（统一管理）| 本体运行（按引擎分组）
// 路由沿用 page 状态机：page='ontology' 内部以 sidebarKey 切换；
// 构建栏以 buildPath 驱动路径页，运行栏以 engineKey 驱动引擎分组页。
// ---------------------------------------------------------------------------

export type SidebarKey = 'learn' | 'build' | 'assets' | 'runtime'

const NAV: { key: SidebarKey; label: string; icon: React.ReactNode; desc: string }[] = [
  { key: 'learn', label: '学习中心', icon: <BookOutlined />, desc: '七阶段路径 · 方法论 · 任务卡' },
  { key: 'build', label: '本体构建', icon: <ApartmentOutlined />, desc: '五条构建路径' },
  { key: 'assets', label: '本体资产', icon: <DatabaseOutlined />, desc: '已构建本体统一管理' },
  { key: 'runtime', label: '本体运行', icon: <CloudServerOutlined />, desc: '按运行方式分组' },
]

/** 侧边栏选中项（模块内持久化，切走再切回不丢位置）；默认页 = 学习中心 */
export const ONTO_SIDEBAR_KEY = 'eino.onto.sidebar'

export function readSidebarKey(): SidebarKey {
  const v = localStorage.getItem(ONTO_SIDEBAR_KEY)
  return v === 'build' || v === 'assets' || v === 'runtime' ? v : 'learn'
}

export default function OntologyModule() {
  const [sidebarKey, setSidebarKey] = useState<SidebarKey>(readSidebarKey)

  useEffect(() => {
    const sync = () => setSidebarKey(readSidebarKey())
    window.addEventListener('onto-sidebar-change', sync)
    return () => window.removeEventListener('onto-sidebar-change', sync)
  }, [])

  const select = (key: SidebarKey) => {
    localStorage.setItem(ONTO_SIDEBAR_KEY, key)
    setSidebarKey(key)
  }

  const body =
    sidebarKey === 'learn' ? (
      <LearnPage />
    ) : sidebarKey === 'build' ? (
      <BuildPage />
    ) : sidebarKey === 'assets' ? (
      <AssetsPage />
    ) : (
      <RuntimePage />
    )

  return (
    <Splitter className="main sidebar-splitter">
      <Splitter.Panel
        defaultSize={Number(localStorage.getItem('eino.sidebar.width')) || 240}
        min={200}
        max={420}
        className="sidebar-panel"
      >
        <aside className="sidebar">
          <div className="side-head">
            <span className="side-title">本体模块</span>
          </div>
          <div className="onto-nav">
            {NAV.map((n) => (
              <Tooltip key={n.key} title={n.desc} placement="right" mouseEnterDelay={0.4}>
                <button
                  type="button"
                  className={`onto-nav-item${sidebarKey === n.key ? ' active' : ''}`}
                  aria-current={sidebarKey === n.key ? 'page' : undefined}
                  onClick={() => select(n.key)}
                >
                  <span className="onto-nav-icon">{n.icon}</span>
                  <span className="onto-nav-text">
                    <span className="onto-nav-label">{n.label}</span>
                    <span className="onto-nav-desc">{n.desc}</span>
                  </span>
                </button>
              </Tooltip>
            ))}
          </div>
          <div className="side-reserve-wrap">
            <div className="reserve-note">
              <div className="reserve-title">模块定位</div>
              学习各种本体<strong>构建、运行</strong>方式的模块（D-O11）：构建 → 运行 → 消费 → 审计四环节闭环。
            </div>
          </div>
        </aside>
      </Splitter.Panel>
      <Splitter.Panel className="content-panel">{body}</Splitter.Panel>
    </Splitter>
  )
}
