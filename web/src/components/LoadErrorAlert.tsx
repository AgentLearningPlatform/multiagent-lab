import { Alert, Button } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'

// ---------------------------------------------------------------------------
// 列表/数据加载失败统一错误卡（REQ-115 三态规范 + REQ-145/M22 B5）：
// 接口错误不再 toast 一闪而过，内联常驻 + 一键重试。
// ---------------------------------------------------------------------------

export default function LoadErrorAlert({
  title = '加载失败',
  message,
  onRetry,
  style,
}: {
  title?: string
  message: string
  onRetry?: () => void
  style?: React.CSSProperties
}) {
  return (
    <Alert
      type="error"
      showIcon
      message={title}
      description={message}
      style={style}
      action={
        onRetry ? (
          <Button size="small" icon={<ReloadOutlined />} onClick={onRetry} aria-label="重试加载">
            重试
          </Button>
        ) : undefined
      }
    />
  )
}
