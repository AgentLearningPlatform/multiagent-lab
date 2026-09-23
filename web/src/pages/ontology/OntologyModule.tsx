import { useEffect, useState } from 'react'
import { Splitter, Tooltip } from 'antd'
import {
  ApartmentOutlined,
  AuditOutlined,
  BookOutlined,
  CloudServerOutlined,
  DatabaseOutlined,
} from '@ant-design/icons'
import LearnPage from './LearnPage'
import BuildPage from './BuildPage'
import AssetsPage from './AssetsPage'
import RuntimePage from './RuntimePage'
import AuditPage from './AuditPage'

// ---------------------------------------------------------------------------
// 本体模块壳（D-O11 / REQ-104）：左侧边栏栏位
//   学习中心（默认页）| 本体构建（六路径）| 本体资产（统一管理）| 本体运行（按引擎分组）
//   | 消费与审计（D-O15/REQ-110 第五栏：原 Semantica 独立栏改造并入，导航栏收敛）
// 路由沿用 page 状态机：page='ontology' 内部以 sidebarKey 切换；
// 构建栏以 buildPath 驱动路径页，运行栏以 engineKey 驱动引擎分组页。
// ---------------------------------------------------------------------------

export type SidebarKey = 'learn' | 'build' | 'assets' | 'runtime' | 'audit'

const NAV: { key: SidebarKey; label: string; icon: React.ReactNode; desc: string }[] = [
  { key: 'learn', label: '学习中心', icon: <BookOutlined />, desc: '七阶段路径 · 方法论 · 任务卡' },
  { key: 'build', label: '本体构建', icon: <ApartmentOutlined />, desc: '六条构建路径' },
  { key: 'assets', label: '本体资产', icon: <DatabaseOutlined />, desc: '已构建本体统一管理' },
  { key: 'runtime', label: '本体运行', icon: <CloudServerOutlined />, desc: '按运行方式分组' },
  { key: 'audit', label: '消费与审计', icon: <AuditOutlined />, desc: 'KG 图谱 · GraphRAG 试查 · 决策溯源' },
]

/** 侧边栏选中项（模块内持久化，切走再切回不丢位置）；默认页 = 学习中心 */
export const ONTO_SIDEBAR_KEY = 'eino.onto.sidebar'

export function readSidebarKey(): SidebarKey {
  const v = localStorage.getItem(ONTO_SIDEBAR_KEY)
  return v === 'build' || v === 'assets' || v === 'runtime' || v === 'audit' ? v : 'learn'
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
    ) : sidebarKey === 'audit' ? (
      <AuditPage />
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
              学习各种本体<strong>构建、运行、消费、审计</strong>方式的模块（D-O11 五栏）：构建 → 运行 → 消费 → 审计全环节闭环。
            </div>
          </div>
        </aside>
      </Splitter.Panel>
      <Splitter.Panel className="content-panel">{body}</Splitter.Panel>
    </Splitter>
  )
}
