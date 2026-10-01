import { useEffect, useState, type ReactNode } from 'react'
import { Button, Tooltip } from 'antd'
import { FullscreenExitOutlined, FullscreenOutlined } from '@ant-design/icons'

/**
 * 页内最大化容器（REQ-240 前端优化③，开发者指令「图形编辑、可视化等页面需要能最大化」）：
 * 绝对定位铺满视口（不依赖 Fullscreen API——嵌入式环境常被拒），Esc 或按钮退出。
 * 按钮浮于容器右上角；最大化时 z-index 提升遮盖平台导航。
 */
export default function Maximizeable({ children, label = '最大化' }: { children: ReactNode; label?: string }) {
  const [max, setMax] = useState(false)

  useEffect(() => {
    if (!max) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMax(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [max])

  const btn = (
    <Tooltip title={max ? '退出最大化（Esc）' : label}>
      <Button
        size="small"
        icon={max ? <FullscreenExitOutlined /> : <FullscreenOutlined />}
        onClick={() => setMax((v) => !v)}
        style={max ? { position: 'fixed', top: 16, right: 20, zIndex: 1100 } : undefined}
      >
        {max ? '退出最大化' : label}
      </Button>
    </Tooltip>
  )

  if (max) {
    return (
      <div
        style={{
          position: 'fixed',
          inset: 12,
          zIndex: 1050,
          background: 'var(--c-bg, #fff)',
          borderRadius: 10,
          boxShadow: '0 12px 48px rgba(0,0,0,0.25)',
          padding: 12,
          display: 'flex',
          flexDirection: 'column',
          overflow: 'hidden',
        }}
      >
        <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 8 }}>{btn}</div>
        <div style={{ flex: 1, minHeight: 0 }}>{children}</div>
      </div>
    )
  }
  return (
    <div style={{ position: 'relative' }}>
      <div style={{ position: 'absolute', top: 0, right: 0, zIndex: 20 }}>{btn}</div>
      {children}
    </div>
  )
}
