import type { ReactNode } from 'react'
import { Button, Typography } from 'antd'

/**
 * 空态引导卡（REQ-112/111② 模块首屏空态规范承载组件，17 §三态规范）：
 * 无数据时不留白——标题 + 三步/要点描述 + 主行动按钮，引导至学习中心或对应创建动作。
 * 左栏窄容器（220~480px）内使用；全站模块首用引导统一走本组件，保证文案与结构一致。
 */
export default function EmptyGuide({
  title,
  steps,
  actionLabel,
  onAction,
  footer,
}: {
  title: string
  /** 引导要点（按序渲染为编号列表） */
  steps: string[]
  actionLabel: string
  onAction: () => void
  /** 可选补充说明（如学习中心跳转提示） */
  footer?: ReactNode
}) {
  return (
    <div className="empty-guide" style={{ marginTop: 28, padding: '0 12px' }}>
      <Typography.Text strong style={{ fontSize: 13 }}>{title}</Typography.Text>
      <ol className="empty-guide-steps">
        {steps.map((s, i) => (
          <li key={i}>{s}</li>
        ))}
      </ol>
      <Button type="primary" size="small" onClick={onAction}>
        {actionLabel}
      </Button>
      {footer && (
        <div style={{ marginTop: 8 }}>
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>{footer}</Typography.Text>
        </div>
      )}
    </div>
  )
}
