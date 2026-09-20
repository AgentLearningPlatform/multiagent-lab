import { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Collapse, Space, Switch, Tag, Typography, Avatar } from 'antd'
import { RobotOutlined, UserOutlined, BulbOutlined, BugOutlined } from '@ant-design/icons'
import { Bubble, Sender, ThoughtChain, Welcome } from '@ant-design/x'
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
 * 中间对话窗口（原型 06 §3.1 / §3.2，Ant Design X Bubble/Sender/ThoughtChain）：
 * - agent 直聊：头部显示智能体名，「配置」打开智能体抽屉
 * - project 会话：头部注明「项目：xxx · 会话使用的智能体：xxx」
 * - 执行细节：历史事件回放、token 用量与耗时、深度思考 ThoughtChain、工具调用 JSON、原始事件调试开关
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

  // 事件卡渲染（ThoughtChain 深度思考 / 工具详情 / 终态摘要）
  const renderEventCard = (it: ChatItem, i: number) => {
    if (it.evType === 'reasoning') {
      const len = it.reasoning?.length ?? 0
      return (
        <ThoughtChain
          key={i}
          style={{ marginLeft: 34, maxWidth: '80%' }}
          items={[
            {
              key: 'think',
              title: `深度思考${len ? `（${len} 字）` : ''}`,
              status: it.streamKey ? 'loading' : 'success',
              icon: <BulbOutlined />,
              content: len ? <pre className="thinking">{it.reasoning}</pre> : undefined,
            },
          ]}
        />
      )
    }
    const isTool = it.evType === 'tool.call' || it.evType === 'tool.result'
    return (
      <div
        key={i}
        className={`event-card ${it.eventErr ? 'err' : ''}`}
        style={{ marginLeft: 34, maxWidth: '80%' }}
      >
        <span>{it.eventText}</span>
        {isTool && it.evData && (
          <Collapse
            ghost
            size="small"
            items={[{
              key: 'detail',
              label: <span style={{ fontSize: 11.5, color: 'var(--ant-color-primary, #4f46e5)' }}>详情</span>,
              children: <pre className="raw-json">{JSON.stringify(it.evData, null, 2)}</pre>,
            }]}
          />
        )}
        {showRaw && it.evData && !isTool && (
          <pre className="raw-json">{JSON.stringify(it.evData, null, 2)}</pre>
        )}
      </div>
    )
  }

  // Bubble.List 数据（消息走 user/assistant 角色，事件卡为无边框自定义内容）
  const listItems = useMemo(
    () =>
      items.map((it, i) => {
        if (it.kind === 'msg') {
          return {
            key: `m${i}`,
            role: it.role === 'user' ? 'user' : 'assistant',
            content: it.content ?? '',
            loading: !!it.streaming && !it.content,
          }
        }
        return {
          key: `e${i}`,
          role: 'event',
          content: renderEventCard(it, i),
        }
      }),
    [items, showRaw],
  )

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
    api.stopConversation(conversation.id).catch(() => {})
  }

  const subjectName = isProjectScope ? project?.name : agent?.name
  const canSend = isProjectScope ? !!project : !!agent
  const placeholder = canSend
    ? `给「${subjectName}」发消息…`
    : isProjectScope
      ? '项目尚未配置成员智能体'
      : '请先创建智能体'

  return (
    <div className="chat">
      <div className="chat-header">
        {isProjectScope && project ? (
          <>
            <Typography.Text strong>项目：{project.name}</Typography.Text>
            <Tag>会话使用的智能体：{agent ? agent.name : '未配置成员'}</Tag>
          </>
        ) : (
          <Typography.Text strong>{agent ? agent.name : '对话'}</Typography.Text>
        )}
        <Tag color="purple">{modelLabel}</Tag>
        {isProjectScope && project && <Tag>{project.collab_mode}</Tag>}
        {!isProjectScope && agent && (
          <>
            <Tag>{agent.runtime_backend}</Tag>
            <Tag>最大迭代 {agent.max_iteration}</Tag>
          </>
        )}
        <span className="spacer" />
        <Space size={4}>
          <BugOutlined style={{ color: showRaw ? 'var(--ant-color-primary, #4f46e5)' : undefined }} />
          <span style={{ fontSize: 12 }}>调试</span>
          <Switch size="small" checked={showRaw} onChange={setShowRaw} />
          <Button
            size="small"
            onClick={isProjectScope ? onOpenProjectDrawer : onOpenAgentDrawer}
            disabled={isProjectScope ? !project : !agent}
          >
            配置
          </Button>
        </Space>
      </div>

      <div className="msg-list" ref={listRef}>
        {items.length === 0 && (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
            <Welcome
              variant="borderless"
              icon={<RobotOutlined style={{ fontSize: 36, color: 'var(--ant-color-primary, #4f46e5)' }} />}
              title={`开始与「${subjectName ?? '智能体'}」对话`}
              description={
                isProjectScope
                  ? '项目会话由成员智能体协作处理；「配置」中可维护项目成员与协作模式（M4 起生效）。'
                  : '消息将流式返回；「配置」中可调整系统提示词、模型与采样参数（下次运行生效）。'
              }
            />
          </div>
        )}
        {items.length > 0 && (
          <Bubble.List
            items={listItems}
            role={{
              user: {
                placement: 'end',
                avatar: <Avatar icon={<UserOutlined />} style={{ background: '#4f46e5', color: '#fff' }} />,
                styles: { content: { background: '#4f46e5', color: '#fff', borderRadius: 12, borderBottomRightRadius: 4 } },
              },
              ai: {
                placement: 'start',
                avatar: <Avatar icon={<RobotOutlined />} style={{ background: '#eef0fe', color: '#4f46e5' }} />,
                styles: { content: { background: '#fff', border: '1px solid var(--c-line)', borderRadius: 12, borderBottomLeftRadius: 4 } },
              },
              event: {
                variant: 'borderless',
                styles: { content: { padding: 0, background: 'transparent', width: '100%', maxWidth: '100%' } },
              },
            }}
          />
        )}
      </div>

      <div className="composer">
        <Sender
          value={input}
          onChange={setInput}
          onSubmit={() => send()}
          onCancel={stop}
          loading={running}
          placeholder={placeholder}
          disabled={!canSend}
        />
        <div className="tips">
          Enter 发送 · Shift+Enter 换行 · 运行过程（开始/思考/工具/用量/完成）以卡片显示，「调试」可查看原始事件
        </div>
      </div>
    </div>
  )
}
