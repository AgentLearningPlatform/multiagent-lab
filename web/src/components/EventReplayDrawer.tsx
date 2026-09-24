import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Button, Drawer, DrawerProps, Progress, Slider, Spin, Tag, Typography } from 'antd'
import { CaretRightOutlined, PauseOutlined, StepForwardOutlined } from '@ant-design/icons'
import { api } from '../api/client'
import type { RunEventDTO } from '../api/types'
import { describeEvent } from './ChatWindow'

/**
 * 事件流重放视图（M17 阶段二：M17 行"事件流导出与重放"，合并需求池「事件流重放视图」）：
 * 拉取会话全量 run_events，按发生顺序步进/自动播放/跳转，逐步还原过程时间线——
 * 演示与教学"逐步重放"辅助。模型对话文本随 assistant.delta 累积渲染。
 */


export default function EventReplayDrawer({
  conversationId,
  title,
  open,
  onClose,
}: {
  conversationId: string
  title: string
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

  const step = useCallback(() => {
    setCursor((c) => Math.min(c + 1, events.length))
  }, [events.length])

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
  }, [playing, cursor, events.length, speed, step])

  const visible = useMemo(() => events.slice(0, cursor), [events, cursor])
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
  const percent = events.length ? Math.round((cursor / events.length) * 100) : 0

  return (
    <Drawer open={open} onClose={onClose} width={680} title={`事件流重放 · ${title}`} destroyOnHidden>
      {loading ? (
        <Spin size="small" />
      ) : events.length === 0 ? (
        <Typography.Text type="secondary">该会话暂无过程事件。</Typography.Text>
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
