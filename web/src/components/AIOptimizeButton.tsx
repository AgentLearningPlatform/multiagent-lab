import { useState } from 'react'
import { Button, Modal, Typography } from 'antd'
import { BulbOutlined } from '@ant-design/icons'
import { api } from '../api/client'
import { useUI } from '../store/ui'

/**
 * AI 内容优化按钮（M27/REQ-167 通用组件）：点击由内置系统级 Agent「平台助手」优化当前内容，
 * 优化前后左右对照预览，用户确认后回填。首批挂载：Agent 系统提示词 / 项目级约束。
 */
export default function AIOptimizeButton({
  kind,
  value,
  onApply,
  disabled,
}: {
  kind: 'agent_instruction' | 'project_constraints'
  value: string
  onApply: (optimized: string) => void
  disabled?: boolean
}) {
  const { showToast } = useUI()
  const [loading, setLoading] = useState(false)
  const [optimized, setOptimized] = useState<string | null>(null)
  const [applying, setApplying] = useState(false)

  const run = async () => {
    if (!value.trim()) {
      showToast('当前内容为空，无需优化', 'err')
      return
    }
    setLoading(true)
    try {
      const r = await api.assistantOptimize(kind, value)
      setOptimized(r.optimized)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setLoading(false)
    }
  }

  const apply = () => {
    if (optimized == null) return
    setApplying(true)
    try {
      onApply(optimized)
      setOptimized(null)
      showToast('已采用优化结果')
    } finally {
      setApplying(false)
    }
  }

  return (
    <>
      <Button size="small" icon={<BulbOutlined />} loading={loading} disabled={disabled || !value.trim()} onClick={run}>
        AI 优化
      </Button>
      <Modal
        open={optimized != null}
        onCancel={() => setOptimized(null)}
        width={900}
        title="AI 优化预览 · 平台助手（REQ-167）"
        footer={
          <Button type="primary" loading={applying} onClick={apply}>
            采用优化结果
          </Button>
        }
      >
        <div style={{ display: 'flex', gap: 12 }}>
          <div style={{ flex: 1, minWidth: 0 }}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>当前内容</Typography.Text>
            <pre
              style={{
                margin: '4px 0 0', padding: 10, maxHeight: 380, overflow: 'auto', whiteSpace: 'pre-wrap', wordBreak: 'break-word',
                background: '#f6f7fb', border: '1px solid #e3e6f0', borderRadius: 8, fontSize: 12, lineHeight: 1.6,
              }}
            >
              {value || '（空）'}
            </pre>
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>优化后（点击「采用」回填）</Typography.Text>
            <pre
              style={{
                margin: '4px 0 0', padding: 10, maxHeight: 380, overflow: 'auto', whiteSpace: 'pre-wrap', wordBreak: 'break-word',
                background: '#f0f9ff', border: '1px solid #b8e0ff', borderRadius: 8, fontSize: 12, lineHeight: 1.6,
              }}
            >
              {optimized ?? '生成中…'}
            </pre>
          </div>
        </div>
        <Typography.Text type="secondary" style={{ fontSize: 11, display: 'block', marginTop: 8 }}>
          优化由「平台助手」（内置系统级 Agent，REQ-166）完成；不采用则当前内容不变。
        </Typography.Text>
      </Modal>
    </>
  )
}
