import { useEffect, useRef, useState } from 'react'
import { api, runConversation } from '../api/client'
import type { Agent, Conversation, Message, ModelConnection, Project } from '../api/types'
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

/**
 * 中间对话窗口（原型 06 §3.1 / §3.2）：
 * - agent 直聊：头部显示智能体名，「属性」打开智能体配置
 * - project 会话：头部注明「项目：xxx · 会话使用的智能体：xxx」，「属性」打开项目配置
 */
export default function ChatWindow({
  conversation,
  agents,
  projects,
  onOpenAgentDrawer,
  onOpenProjectDrawer,
  onConversationUpdated,
}: {
  conversation: Conversation
  agents: Agent[]
  projects: Project[]
  onOpenAgentDrawer: () => void
  onOpenProjectDrawer: () => void
  onConversationUpdated: () => void
}) {
  const { bumpData, showToast } = useUI()
  const isProjectScope = conversation.scope === 'project'

  const project = isProjectScope
    ? projects.find((p) => p.id === conversation.project_id) ?? null
    : null
  // agent 直聊 = 绑定的智能体；项目会话 = 主智能体，缺省取第一个成员
  const agent =
    conversation.scope === 'agent'
      ? agents.find((a) => a.id === conversation.agent_id) ?? null
      : project
        ? agents.find((a) => a.id === (project.coordinator || project.agent_ids[0])) ?? null
        : null

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

  // 模型标签：智能体指定连接 或 默认连接
  useEffect(() => {
    api.listConnections().then((conns) => {
      connRef.current = conns
    }).catch(() => {})
  }, [conversation.id])

  const modelLabel = (() => {
    if (!agent) return '项目未配置成员'
    const connId = agent.model_conn_id
    const conn = connId ? connRef.current.find((c) => c.id === connId) : connRef.current.find((c) => c.conn_type === 'chat' && c.is_default)
    return conn ? `${conn.name} · ${conn.model_name}` : agent.model_conn_id ? '指定连接' : '默认模型'
  })()

  useEffect(() => {
    listRef.current?.scrollTo({ top: listRef.current.scrollHeight })
  }, [items])

  const send = async () => {
    const text = input.trim()
    if (!text || running) return
    if (!isProjectScope && !agent) return
    if (isProjectScope && !agent) {
      showToast('项目还没有成员智能体，请先在项目配置中添加成员', 'err')
      return
    }
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
            const ev: ChatItem = { kind: 'event', eventText: `▶ 运行开始 · ${payload.agent_name ?? agent?.name ?? ''} · ${payload.model ?? ''}` }
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
  const subjectName = isProjectScope ? project?.name : agent?.name
  const canSend = isProjectScope ? !!project : !!agent

  return (
    <div className="chat">
      <div className="chat-header">
        {isProjectScope && project ? (
          <>
            <span className="agent-name">项目：{project.name}</span>
            <span className="badge gray">会话使用的智能体：{agent ? agent.name : '未配置成员'}</span>
          </>
        ) : (
          <span className="agent-name">{agent ? agent.name : '对话'}</span>
        )}
        <span className="badge">{modelLabel}</span>
        {isProjectScope && project && <span className="badge gray">{project.collab_mode}</span>}
        {!isProjectScope && agent && (
          <>
            <span className="badge gray">{agent.runtime_backend}</span>
            <span className="badge gray">最大迭代 {agent.max_iteration}</span>
          </>
        )}
        <span className="spacer" />
        <button
          className="btn-ghost"
          onClick={isProjectScope ? onOpenProjectDrawer : onOpenAgentDrawer}
          disabled={isProjectScope ? !project : !agent}
        >
          配置
        </button>
      </div>

      <div className="msg-list" ref={listRef}>
        {emptyState && (
          <div className="placeholder" style={{ marginTop: 60 }}>
            <b>
              开始与「{subjectName ?? '智能体'}」对话
            </b>
            <p style={{ fontSize: 13 }}>
              {isProjectScope
                ? '项目会话由成员智能体协作处理；「配置」中可维护项目成员与协作模式（M4 起生效）。'
                : '消息将流式返回；「配置」中可调整系统提示词、模型与采样参数（下次运行生效）。'}
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
            placeholder={
              canSend
                ? `给「${subjectName}」发消息…（Enter 发送，Shift+Enter 换行）`
                : isProjectScope
                  ? '项目尚未配置成员智能体'
                  : '请先创建智能体'
            }
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                send()
              }
            }}
            disabled={!canSend}
          />
          {running ? (
            <button className="btn-primary btn-stop" onClick={stop}>
              停止
            </button>
          ) : (
            <button className="btn-primary" onClick={send} disabled={!input.trim() || !canSend}>
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
