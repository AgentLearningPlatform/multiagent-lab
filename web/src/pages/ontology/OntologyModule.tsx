import { useEffect, useState } from 'react'
import { Button, Splitter, Tooltip } from 'antd'
import { MenuFoldOutlined, MenuUnfoldOutlined } from '@ant-design/icons'
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
import { SIDEBAR_WIDTH, sidebarDefaultSize, sidebarRemember } from '../../lib/layout'

// ---------------------------------------------------------------------------
// 本体模块壳（D-O11 / REQ-104）：左侧边栏栏位
//   学习中心（默认页）| 本体构建（六路径）| 本体资产（统一管理）| 本体运行（按引擎分组）
//   | 消费与审计（D-O15/REQ-110 第五栏）
// REQ-216⑨/D-O21 反转：伴生本体第六栏退役（回五栏）——六栏前提「伴生图非仓库资产」
// 被归属容器化推翻，栏职能被资产详情页「伴生候选」页签+可视化区伴生成长图吸收。
// 路由沿用 page 状态机：page='ontology' 内部以 sidebarKey 切换；
// 构建栏以 buildPath 驱动路径页，运行栏以 engineKey 驱动引擎分组页。
// ---------------------------------------------------------------------------

export type SidebarKey = 'learn' | 'build' | 'assets' | 'runtime' | 'audit'

const NAV: { key: SidebarKey; label: string; icon: React.ReactNode; desc: string }[] = [
  { key: 'learn', label: '学习中心', icon: <BookOutlined />, desc: '七阶段路径 · 方法论 · 任务卡' },
  { key: 'build', label: '本体构建', icon: <ApartmentOutlined />, desc: '六条构建路径' },
  { key: 'assets', label: '本体资产', icon: <DatabaseOutlined />, desc: '已构建本体统一管理 · 伴生候选' },
  { key: 'runtime', label: '本体运行', icon: <CloudServerOutlined />, desc: '按运行方式分组' },
  { key: 'audit', label: '消费与审计', icon: <AuditOutlined />, desc: 'KG 图谱 · GraphRAG 试查 · 决策溯源' },
]

/** 侧边栏选中项（模块内持久化，切走再切回不丢位置）；默认页 = 学习中心。
 *  REQ-216：旧 'companion' 值（第六栏）非法化回退默认页。 */
export const ONTO_SIDEBAR_KEY = 'eino.onto.sidebar'

export function readSidebarKey(): SidebarKey {
  const v = localStorage.getItem(ONTO_SIDEBAR_KEY)
  return v === 'build' || v === 'assets' || v === 'runtime' || v === 'audit' ? v : 'learn'
}

export default function OntologyModule() {
  const [sidebarKey, setSidebarKey] = useState<SidebarKey>(readSidebarKey)
  // REQ-240 前端优化①：模块左栏可收起为图标列（48px，只剩栏位图标；localStorage 记忆）
  const [navCollapsed, setNavCollapsed] = useState(() => localStorage.getItem('eino.onto.nav.collapsed') === '1')
  const toggleNav = () => {
    setNavCollapsed((v) => {
      localStorage.setItem('eino.onto.nav.collapsed', v ? '0' : '1')
      return !v
    })
  }

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

  // 收起态：图标列（48px）+ 内容区，不用 Splitter（重展开时恢复）
  if (navCollapsed) {
    return (
      <div className="main" style={{ display: 'flex', minHeight: 0, flex: 1 }}>
        <aside
          className="sidebar"
          style={{ width: 48, flex: '0 0 48px', maxWidth: 48, alignItems: 'center', padding: '8px 0', gap: 2 }}
          aria-label="本体模块导航（已收起）"
        >
          <Button
            type="text"
            size="small"
            icon={<MenuUnfoldOutlined />}
            aria-label="展开模块导航"
            onClick={toggleNav}
            style={{ marginBottom: 6 }}
          />
          {NAV.map((n) => (
            <Tooltip key={n.key} title={n.label} placement="right" mouseEnterDelay={0.3}>
              <button
                type="button"
                className={`onto-nav-item${sidebarKey === n.key ? ' active' : ''}`}
                aria-current={sidebarKey === n.key ? 'page' : undefined}
                aria-label={n.label}
                onClick={() => select(n.key)}
                style={{ width: 40, justifyContent: 'center', padding: '8px 0' }}
              >
                <span className="onto-nav-icon">{n.icon}</span>
              </button>
            </Tooltip>
          ))}
        </aside>
        <div className="content-panel" style={{ flex: 1, minWidth: 0 }}>{body}</div>
      </div>
    )
  }

  return (
    // REQ-237（57 号 F4）：左栏宽度并入全站单一约定（同 key/默认/边界，写入回填——原只读他页 key 且默认 240 互踩）
    <Splitter className="main sidebar-splitter" onResizeEnd={sidebarRemember}>
      <Splitter.Panel
        defaultSize={sidebarDefaultSize()}
        min={SIDEBAR_WIDTH.min}
        max={SIDEBAR_WIDTH.max}
        className="sidebar-panel"
      >
        <aside className="sidebar">
          <div className="side-head">
            <span className="side-title">本体模块</span>
            <Button type="text" size="small" icon={<MenuFoldOutlined />} aria-label="收起模块导航" onClick={toggleNav} />
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
