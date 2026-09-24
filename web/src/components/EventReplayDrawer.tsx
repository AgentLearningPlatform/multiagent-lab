import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Alert, Button, Drawer, DrawerProps, Progress, Slider, Spin, Tag, Typography } from 'antd'
import { CaretRightOutlined, PauseOutlined, StepForwardOutlined } from '@ant-design/icons'
import { api } from '../api/client'
import type { RunEventDTO } from '../api/types'
import { describeEvent, levelGated } from './ChatWindow'

/**
 * 事件流重放视图（M17 阶段二：M17 行"事件流导出与重放"，合并需求池「事件流重放视图」）：
 * 拉取会话全量 run_events，按发生顺序步进/自动播放/跳转，逐步还原过程时间线——
 * 演示与教学"逐步重放"辅助。模型对话文本随 assistant.delta 累积渲染。
 * REQ-149：回放按会话当前展示级别过滤（简洁档隐藏 model.step 等调试细节），
 * 且对"未记录调试细节/入库截断"诚实标注而非静默留白。
 */


export default function EventReplayDrawer({
  conversationId,
  title,
  level = 0,
  open,
  onClose,
}: {
  conversationId: string
  title: string
  /** REQ-149：会话当前展示级别（0 简洁 / 1 详细 / 2 调试）——过滤调试细节事件 */
  level?: number
  open: boolean
  onClose: () => void
} & Pick<DrawerProps, 'open' | 'onClose'>) {
  const [events, setEvents] = useState<RunEventDTO[]>([])
  const [loading, setLoading] = useState(false)
  const [cursor, setCursor] = useState(0) // 已显示的事件数（0~events.length）
  const [playing, setPlaying] = useState(false)
  const [speed, setSpeed] = useState(2) // 每 tick 推进的事件数
  const timer = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    if (!open) {
      setPlaying(false)
      return
    }
    setLoading(true)
    setCursor(0)
    setPlaying(false)
    setEvents([])
    api
      .listEvents(conversationId)
      .then((r) => setEvents(r ?? []))
      .catch(() => setEvents([]))
      .finally(() => setLoading(false))
  }, [open, conversationId])

  // REQ-149①：按当前展示级别过滤渲染（简洁档隐藏调试细节事件）
  const gated = useMemo(() => events.filter((e) => !levelGated(e.type, level)), [events, level])
  // REQ-149③：诚实标注数据源——原始事件里是否有过调试细节（与过滤无关，取决于入库时的设置）
  const hasDebugRecorded = useMemo(() => events.some((e) => e.type === 'model.step'), [events])
  const truncated = useMemo(() => events.some((e) => e.type === 'model.step' && (e.data ?? '').includes('截断')), [events])

  const step = useCallback(() => {
    setCursor((c) => Math.min(c + 1, gated.length))
  }, [gated.length])

  // 自动播放：interval 推进（速度 = 每 400ms 推进 speed 条）
  useEffect(() => {
    if (!playing) return
    if (cursor >= events.length) {
      setPlaying(false)
      return
    }
    timer.current = setInterval(step, Math.max(120, 400 / speed))
    return () => {
      if (timer.current) clearInterval(timer.current)
    }
  }, [playing, cursor, gated.length, speed, step])

  const visible = useMemo(() => gated.slice(0, cursor), [gated, cursor])
  // 助手消息累积（message.delta 拼接，模拟打字机重放）
  const assistantText = useMemo(
    () =>
      visible
        .filter((e) => e.type === 'message.delta')
        .map((e) => {
          try {
            return (JSON.parse(e.data ?? '')?.delta as string) ?? ''
          } catch {
            return ''
          }
        })
        .join(''),
    [visible],
  )
  const percent = gated.length ? Math.round((cursor / gated.length) * 100) : 0

  return (
    <Drawer open={open} onClose={onClose} width={680} title={`事件流重放 · ${title}`} destroyOnHidden>
      {loading ? (
        <Spin size="small" />
      ) : events.length === 0 ? (
        <Typography.Text type="secondary">该会话暂无过程事件。</Typography.Text>
      ) : gated.length === 0 ? (
        <Typography.Text type="secondary">简洁档隐藏了全部 {events.length} 条调试细节事件；切换到详细/调试档后重放可见。</Typography.Text>
      ) : (
        <>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 8 }}>
            <Button
              size="small"
              type="primary"
              icon={playing ? <PauseOutlined /> : <CaretRightOutlined />}
              onClick={() => setPlaying((p) => !p)}
              disabled={cursor >= events.length && !playing}
            >
              {playing ? '暂停' : '播放'}
            </Button>
            <Button size="small" icon={<StepForwardOutlined />} onClick={step} disabled={cursor >= events.length}>
              单步
            </Button>
            <Slider min={1} max={8} value={speed} onChange={(v) => setSpeed(v)} style={{ width: 120 }} tooltip={{ formatter: (v) => `${v}x` }} />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {cursor}/{events.length}
            </Typography.Text>
            <span style={{ flex: 1 }} />
            <Button size="small" onClick={() => { setCursor(0); setPlaying(false) }}>
              重置
            </Button>
          </div>
          <Progress percent={percent} size="small" showInfo={false} />

          {/* REQ-149③ 诚实标注：过滤来源与数据前提，避免静默留白 */}
          {level < 1 && hasDebugRecorded && (
            <Alert
              type="info"
              showIcon
              style={{ margin: '8px 0' }}
              message={`简洁档：已隐藏 ${events.length - gated.length} 条调试细节事件（模型调用链路/装配快照）；切到详细/调试档可完整回放`}
            />
          )}
          {level >= 1 && !hasDebugRecorded && (
            <Alert
              type="warning"
              showIcon
              style={{ margin: '8px 0' }}
              message="该会话历史未记录调试细节"
              description="运行时未开启「调试事件入库」，或该会话早于该功能——存量运行不可追溯；在「过程展示」中开启后，之后的运行将留存。"
            />
          )}
          {hasDebugRecorded && truncated && (
            <Typography.Text type="secondary" style={{ fontSize: 11, display: 'block', margin: '4px 0 8px' }}>
              注：模型输入全文在入库时已截断（约 4000 字），超长部分不可完整回放。
            </Typography.Text>
          )}

          {/* 助手文本累积视图（重放核心画面） */}
          <pre
            style={{
              margin: '8px 0', padding: '10px 12px', minHeight: 72, maxHeight: 220, overflow: 'auto',
              background: '#f6f7fb', border: '1px solid #e3e6f0', borderRadius: 8,
              fontFamily: 'inherit', fontSize: 12.5, whiteSpace: 'pre-wrap', wordBreak: 'break-word',
            }}
          >
            {assistantText || '（等待输出…）'}
          </pre>

          {/* 过程事件时间线（已揭示部分） */}
          <ul style={{ margin: 0, paddingLeft: 16, maxHeight: 320, overflow: 'auto' }}>
            {visible.map((e) => {
              let data: any = null
              try {
                data = JSON.parse(e.data ?? '')
              } catch { /* 非 JSON 事件 */ }
              const d = describeEvent(e.type, data)
              return (
                <li key={e.id} style={{ fontSize: 12, marginBottom: 3, color: d.err ? '#cf1322' : undefined }}>
                  <Tag style={{ marginInlineEnd: 6, fontSize: 10 }} color={d.err ? 'red' : d.warn ? 'orange' : 'default'}>
                    {e.type}
                  </Tag>
                  {d.text}
                </li>
              )
            })}
          </ul>
        </>
      )}
    </Drawer>
  )
}
