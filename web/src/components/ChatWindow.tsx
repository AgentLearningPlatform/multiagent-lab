import { useEffect, useRef, useState } from 'react'
import { api, runConversation } from '../api/client'
import type { Agent, Conversation, Message, ModelConnection } from '../api/types'
import { useUI } from '../store/ui'

interface ChatItem {
  kind: 'msg' | 'event'
  role?: Message['role']
  content?: string
  metaAgent?: string
  eventText?: string
  eventErr?: boolean
  streaming?: boolean
}

/** 中间对话窗口：配置条 + 消息流（含过程事件卡）+ 输入区 */
export default function ChatWindow({
  conversation,
  agents,
  onOpenDrawer,
  onConversationUpdated,
}: {
  conversation: Conversation
  agents: Agent[]
  onOpenDrawer: () => void
  onConversationUpdated: () => void
}) {
  const { bumpData } = useUI()
  const agent = agents.find((a) => a.id === conversation.agent_id) ?? null
  const [items, setItems] = useState<ChatItem[]>([])
  const [input, setInput] = useState('')
  const [running, setRunning] = useState(false)
  const runRef = useRef<{ abort: () => void; done: Promise<void> } | null>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const connRef = useRef<ModelConnection[]>([])

  // 历史还原：进入对话时拉取消息
  useEffect(() => {
    let alive = true
    setItems([])
    if (!conversation) return
    api.listMessages(conversation.id).then((msgs) => {
      if (!alive) return
      setItems(
        msgs.map((m) => ({
          kind: 'msg' as const,
          role: m.role,
          content: m.content,
          metaAgent: undefined,
        })),
      )
    })
    return () => {
      alive = false
    }
  }, [conversation.id])

  // 模型标签：agent 指定连接 或 默认连接
  useEffect(() => {
    api.listConnections().then((conns) => {
      connRef.current = conns
    }).catch(() => {})
  }, [conversation.id])

  const modelLabel = (() => {
    const connId = agent?.model_conn_id
    const conn = connId ? connRef.current.find((c) => c.id === connId) : connRef.current.find((c) => c.conn_type === 'chat' && c.is_default)
    return conn ? `${conn.name} · ${conn.model_name}` : agent?.model_conn_id ? '指定连接' : '默认模型'
  })()

  useEffect(() => {
    listRef.current?.scrollTo({ top: listRef.current.scrollHeight })
  }, [items])

  const send = async () => {
    const text = input.trim()
    if (!text || running || !agent) return
    setInput('')
    setItems((prev) => [...prev, { kind: 'msg', role: 'user', content: text }])

    // 追加流式助手消息
    const streamKey = items.length + 1
    setItems((prev) => [...prev, { kind: 'msg', role: 'assistant', content: '', streaming: true, metaAgent: `run-${streamKey}` }])
    setRunning(true)
    const aborter = runConversation(conversation.id, text, ({ event, data }) => {
      const payload = data?.data ?? {}
      switch (event) {
        case 'run.started':
          setItems((prev) => {
            const next = [...prev]
            const ev: ChatItem = { kind: 'event', eventText: `▶ 运行开始 · ${payload.agent_name ?? agent.name} · ${payload.model ?? ''}` }
            next.splice(next.length - 1, 0, ev)
            return next
          })
          break
        case 'message.delta':
          setItems((prev) => {
            const next = [...prev]
            const last = next[next.length - 1]
            if (last && last.kind === 'msg' && last.streaming) last.content += payload.delta ?? ''
            return next
          })
          break
        case 'run.finished':
          setItems((prev) => {
            const next = [...prev]
            const last = next[next.length - 1]
            if (last && last.kind === 'msg' && last.streaming) {
              last.streaming = false
              if (!last.content) last.content = payload.reason === 'stopped' ? '（已停止生成）' : ''
            }
            const ev: ChatItem = { kind: 'event', eventText: payload.reason === 'stopped' ? '⏹ 已停止' : '✓ 运行完成' }
            next.splice(next.length - 1, 0, ev)
            return next
          })
          break
        case 'run.error': {
          const msg = payload.message ?? '运行失败'
          setItems((prev) => {
            const next = [...prev]
            const last = next[next.length - 1]
            if (last && last.kind === 'msg' && last.streaming && !last.content) next.pop()
            const ev: ChatItem = { kind: 'event', eventErr: true, eventText: `⚠ ${msg}` }
            return [...next, ev]
          })
          break
        }
        default:
          if (event === 'tool.call' || event === 'tool.result' || event === 'skill.loaded') {
            setItems((prev) => [
              ...prev.slice(0, -1),
              { kind: 'event', eventText: `⚙ ${event} ${payload.tool_name ?? ''}`.trim() },
              prev[prev.length - 1],
            ])
          }
      }
    })
    runRef.current = aborter
    try {
      await aborter.done
    } catch { /* 用户中断 */ }
    setRunning(false)
    runRef.current = null
    bumpData()
    onConversationUpdated()
  }

  const stop = () => {
    runRef.current?.abort()
    api.updateConversation(conversation.id, conversation).catch(() => {})
    fetch(`/api/conversations/${conversation.id}/stop`, { method: 'POST' }).catch(() => {})
  }

  const emptyState = items.length === 0

  return (
    <div className="chat">
      <div className="chat-header">
        <span className="agent-name">{agent ? agent.name : '对话'}</span>
        <span className="badge">{modelLabel}</span>
        <span className="badge gray">{agent?.runtime_backend ?? 'inprocess'}</span>
        {agent && <span className="badge gray">最大迭代 {agent.max_iteration}</span>}
        <span className="spacer" />
        <button className="btn-ghost" onClick={onOpenDrawer} disabled={!agent}>
          属性
        </button>
      </div>

      <div className="msg-list" ref={listRef}>
        {emptyState && (
          <div className="placeholder" style={{ marginTop: 60 }}>
            <b>开始与「{agent?.name ?? '智能体'}」对话</b>
            <p style={{ fontSize: 13 }}>
              消息将流式返回；可在右侧「属性」中调整系统提示词、模型与采样参数（下次运行生效）。
            </p>
          </div>
        )}
        {items.map((it, i) =>
          it.kind === 'msg' ? (
            <div key={i} className={`msg ${it.role === 'user' ? 'user' : 'assistant'}`}>
              <div className="bubble">
                {it.role === 'assistant' && it.metaAgent && <div className="meta">助手</div>}
                {it.content}
                {it.streaming && <span className="cursor-blink" />}
              </div>
            </div>
          ) : (
            <div key={i} className={`event-card ${it.eventErr ? 'err' : ''}`}>{it.eventText}</div>
          ),
        )}
      </div>

      <div className="composer">
        <div className="box">
          <textarea
            rows={2}
            placeholder={agent ? `给「${agent.name}」发消息…（Enter 发送，Shift+Enter 换行）` : '请先创建智能体'}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                send()
              }
            }}
            disabled={!agent}
          />
          {running ? (
            <button className="btn-primary btn-stop" onClick={stop}>
              停止
            </button>
          ) : (
            <button className="btn-primary" onClick={send} disabled={!input.trim() || !agent}>
              发送
            </button>
          )}
        </div>
        <div className="tips">
          Enter 发送 · Shift+Enter 换行 · 运行过程事件（运行开始/完成/错误）会以卡片显示在消息流中

        </div>
      </div>
    </div>
  )
}
