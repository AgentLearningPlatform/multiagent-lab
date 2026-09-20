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
  // 执行细节增强（06 §4 执行可观测）
  evType?: string // reasoning | run.started | run.finished | run.error | tool.call | tool.result | skill.loaded
  evData?: any // 事件 data（解析后，供详情展开与调试面板）
  reasoning?: string // 深度思考累积内容
  streamKey?: string // 运行中的 reasoning 卡合并键；运行结束置空收起
}

// 事件卡文案（实时流与历史回放共用）
function describeEvent(type: string, d: any): { text: string; err?: boolean } {
  switch (type) {
    case 'run.started':
      return { text: `▶ 运行开始 · ${d?.agent_name ?? ''} · ${d?.model ?? ''}`.replace(/ ·\s*$/, '') }
    case 'run.finished':
      return d?.reason === 'stopped' ? { text: '⏹ 已停止' } : { text: finishSummary(d) }
    case 'run.error':
      return { text: `⚠ ${d?.message ?? '运行失败'}`, err: true }
    case 'tool.call':
      return { text: `⚙ 调用工具 ${d?.tool_name ?? ''}` }
    case 'tool.result':
      return { text: `⚙ 工具结果 ${d?.tool_name ?? ''}` }
    case 'skill.loaded':
      return { text: `📚 技能 ${d?.skill_name ?? d?.name ?? ''}`.replace(/ $/, '') }
    default:
      return { text: `· ${type}` }
  }
}

// run.finished 摘要：耗时 / token 用量 / finish_reason
function finishSummary(d: any): string {
  const parts = ['✓ 运行完成']
  if (typeof d?.elapsed_ms === 'number') {
    parts.push(d.elapsed_ms >= 1000 ? `${(d.elapsed_ms / 1000).toFixed(1)}s` : `${d.elapsed_ms}ms`)
  }
  const u = d?.usage
  if (u && typeof u.total_tokens === 'number' && u.total_tokens > 0) {
    parts.push(`tokens ${u.prompt_tokens ?? 0}+${u.completion_tokens ?? 0}=${u.total_tokens}`)
  }
  if (d?.finish_reason) parts.push(`finish=${d.finish_reason}`)
  return parts.join(' · ')
}

/**
 * 中间对话窗口（原型 06 §3.1 / §3.2）：
 * - agent 直聊：头部显示智能体名，「属性」打开智能体配置
 * - project 会话：头部注明「项目：xxx · 会话使用的智能体：xxx」，「属性」打开项目配置
 * - 执行细节：历史事件回放、token 用量与耗时、深度思考折叠卡、工具调用入参/出参 JSON、原始事件调试开关
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
  const [showRaw, setShowRaw] = useState(false) // 原始事件 JSON 调试开关
  const runRef = useRef<{ abort: () => void; done: Promise<void> } | null>(null)
  const runKeyRef = useRef('')
  const listRef = useRef<HTMLDivElement>(null)
  const connRef = useRef<ModelConnection[]>([])

  // 历史还原：消息表（对话正文）+ 事件表（执行时间线）按时间合并
  useEffect(() => {
    let alive = true
    setItems([])
    if (!conversation) return
    Promise.all([api.listMessages(conversation.id), api.listEvents(conversation.id).catch(() => [] as never[])])
      .then(([msgs, evs]) => {
        if (!alive) return
        const timeline: { ts: string; item: ChatItem }[] = []
        msgs.forEach((m) =>
          timeline.push({ ts: m.created_at, item: { kind: 'msg', role: m.role, content: m.content } }),
        )
        const reasoningCards: Record<string, ChatItem> = {}
        for (const e of evs) {
          let d: any = {}
          try {
            d = e.data ? JSON.parse(e.data) : {}
          } catch {
            /* 忽略非 JSON 数据 */
          }
          if (e.type === 'message.delta') continue // 正文已由消息表还原，避免重复
          if (e.type === 'reasoning.delta') {
            const k = e.run_id || '_'
            if (!reasoningCards[k]) {
              reasoningCards[k] = {
                kind: 'event',
                evType: 'reasoning',
                reasoning: '',
                evData: { run_id: e.run_id, type: 'reasoning' },
              }
              timeline.push({ ts: e.created_at, item: reasoningCards[k] })
            }
            reasoningCards[k].reasoning += d?.delta ?? ''
            continue
          }
          const desc = describeEvent(e.type, d)
          timeline.push({
            ts: e.created_at,
            item: { kind: 'event', evType: e.type, eventText: desc.text, eventErr: desc.err, evData: d },
          })
        }
        timeline.sort((a, b) => (a.ts < b.ts ? -1 : a.ts > b.ts ? 1 : 0))
        setItems(timeline.map((t) => t.item))
      })
      .catch(() => {})
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
    runKeyRef.current = `run-${Date.now()}`
    setItems((prev) => [...prev, { kind: 'msg', role: 'user', content: text }])

    // 追加流式助手消息
    setItems((prev) => [...prev, { kind: 'msg', role: 'assistant', content: '', streaming: true }])
    setRunning(true)
    const aborter = runConversation(conversation.id, text, ({ event, data }) => {
      const payload = data?.data ?? {}
      switch (event) {
        case 'run.started':
        case 'run.finished': {
          const desc = describeEvent(event, payload)
          setItems((prev) => {
            const next = [...prev]
            if (event === 'run.finished') {
              const last = next[next.length - 1]
              if (last && last.kind === 'msg' && last.streaming) {
                last.streaming = false
                if (!last.content) last.content = payload.reason === 'stopped' ? '（已停止生成）' : ''
              }
            }
            const ev: ChatItem = { kind: 'event', evType: event, eventText: desc.text, evData: payload }
            next.splice(next.length - 1, 0, ev)
            return next
          })
          break
        }
        case 'reasoning.delta':
          setItems((prev) => {
            const next = [...prev]
            for (let i = next.length - 1; i >= 0; i--) {
              if (next[i].evType === 'reasoning' && next[i].streamKey === runKeyRef.current) {
                next[i] = { ...next[i], reasoning: (next[i].reasoning ?? '') + (payload.delta ?? '') }
                return next
              }
            }
            const card: ChatItem = { kind: 'event', evType: 'reasoning', reasoning: payload.delta ?? '', streamKey: runKeyRef.current }
            next.splice(Math.max(next.length - 1, 0), 0, card)
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
        case 'run.error': {
          const msg = payload.message ?? '运行失败'
          setItems((prev) => {
            const next = [...prev]
            const last = next[next.length - 1]
            if (last && last.kind === 'msg' && last.streaming && !last.content) next.pop()
            const ev: ChatItem = { kind: 'event', evType: 'run.error', eventErr: true, eventText: `⚠ ${msg}`, evData: payload }
            return [...next, ev]
          })
          break
        }
        default:
          if (event === 'tool.call' || event === 'tool.result' || event === 'skill.loaded') {
            const desc = describeEvent(event, payload)
            setItems((prev) => {
              const next = [...prev]
              const ev: ChatItem = { kind: 'event', evType: event, eventText: desc.text, evData: payload }
              next.splice(next.length - 1, 0, ev) // 流式助手消息存在时插到其前
              return next
            })
          }
      }
    })
    runRef.current = aborter
    try {
      await aborter.done
    } catch { /* 用户中断 */ }
    // 运行结束：reasoning 卡收起（保留内容，可手动展开）
    setItems((prev) => prev.map((it) => (it.streamKey ? { ...it, streamKey: undefined } : it)))
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
          className={`btn-ghost ${showRaw ? 'on' : ''}`}
          onClick={() => setShowRaw((v) => !v)}
          title="显示运行事件的原始 JSON（调试用）"
        >
          {showRaw ? '调试：开' : '调试'}
        </button>
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
                {it.role === 'assistant' && <div className="meta">助手</div>}
                {it.content}
                {it.streaming && <span className="cursor-blink" />}
              </div>
            </div>
          ) : it.evType === 'reasoning' ? (
            <div key={i} className="event-card reasoning">
              <details open={!!it.streamKey}>
                <summary>💭 深度思考{it.reasoning ? `（${it.reasoning.length} 字）` : ''}</summary>
                <pre className="thinking">{it.reasoning}</pre>
              </details>
              {showRaw && it.evData && <pre className="raw-json">{JSON.stringify(it.evData, null, 2)}</pre>}
            </div>
          ) : (
            <div key={i} className={`event-card ${it.eventErr ? 'err' : ''}`}>
              <span>{it.eventText}</span>
              {(it.evType === 'tool.call' || it.evType === 'tool.result') && it.evData && (
                <details className="tool-detail">
                  <summary>详情</summary>
                  <pre>{JSON.stringify(it.evData, null, 2)}</pre>
                </details>
              )}
              {showRaw && it.evData && <pre className="raw-json">{JSON.stringify(it.evData, null, 2)}</pre>}
            </div>
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
          Enter 发送 · Shift+Enter 换行 · 运行过程（开始/思考/工具/用量/完成）以卡片显示，「调试」可查看原始事件
        </div>
      </div>
    </div>
  )
}
