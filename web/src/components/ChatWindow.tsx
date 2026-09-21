import { useEffect, useMemo, useRef, useState } from 'react'
import { Avatar, Badge, Button, Collapse, Drawer, InputNumber, Select, Space, Switch, Tag, Typography } from 'antd'
import {
  ApartmentOutlined,
  BugOutlined,
  BulbOutlined,
  DatabaseOutlined,
  RobotOutlined,
  SlidersOutlined,
  ThunderboltOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { Bubble, Sender, ThoughtChain, Welcome } from '@ant-design/x'
import type { BubbleListProps } from '@ant-design/x'
import XMarkdown from '@ant-design/x-markdown'
import { api, runConversation } from '../api/client'
import type {
  Agent,
  Conversation,
  KBHit,
  KnowledgeBase,
  Message,
  ModelConnection,
  Project,
  RuntimeProfile,
  Skill,
} from '../api/types'
import { useUI } from '../store/ui'

interface ChatItem {
  kind: 'msg' | 'event'
  role?: Message['role']
  content?: string
  metaAgent?: string
  eventText?: string
  eventErr?: boolean
  eventWarn?: boolean
  streaming?: boolean
  // 执行细节增强（06 §4 执行可观测）
  evType?: string // reasoning | run.started | run.finished | run.warning | run.error | tool.call | tool.result | skill.loaded | subagent.enter | subagent.exit | retrieval | ontology.query | ontology.unavailable
  evData?: any // 事件 data（解析后，供详情展开与调试面板）
  reasoning?: string // 深度思考累积内容
  evKey?: string // 稳定卡片键（深度思考展开状态按它记录，历史/实时各自生成）
  streamKey?: string // 运行中的 reasoning 卡合并键；运行结束置空收起
}

// 子 Agent 名（§6.5 subagent.enter/exit payload = 子 Agent 名；字段名做兼容取值）
function subagentName(d: any): string {
  return String(d?.name ?? d?.agent_name ?? d?.sub_agent ?? d?.agent ?? '').trim()
}

// 事件卡文案（实时流与历史回放共用：新增事件必须在此登记，两条路径才一致）
function describeEvent(type: string, d: any): { text: string; err?: boolean; warn?: boolean } {
  switch (type) {
    case 'run.started':
      return { text: `▶ 运行开始 · ${d?.agent_name ?? ''} · ${d?.model ?? ''}`.replace(/ ·\s*$/, '') }
    case 'run.finished':
      return d?.reason === 'stopped' ? { text: '⏹ 已停止' } : { text: finishSummary(d) }
    case 'run.warning':
      return { text: `⚠ 运行警告 · ${d?.message ?? ''}`.replace(/ ·\s*$/, ''), warn: true }
    case 'run.error':
      return { text: `⚠ ${d?.message ?? '运行失败'}`, err: true }
    case 'tool.call':
      return { text: `⚙ 调用工具 ${d?.tool_name ?? ''}` }
    case 'tool.result':
      return { text: `⚙ 工具结果 ${d?.tool_name ?? ''}` }
    case 'skill.loaded': {
      // §6.5 v0.6 payload 为 skills[{id,name}]；兼容旧的 skill_name/name 单值写法
      const many = Array.isArray(d?.skills) ? d.skills.map((s: any) => s?.name).filter(Boolean).join('、') : ''
      const one = d?.skill_name ?? d?.name ?? ''
      return { text: `📚 技能 ${many || one}`.replace(/ $/, '') }
    }
    // M4：多智能体协作（AgentAsTool / Transfer）
    case 'subagent.enter': {
      const n = subagentName(d)
      return { text: n ? `↳ 进入子智能体 ${n}` : '↳ 进入子智能体' }
    }
    case 'subagent.exit': {
      const n = subagentName(d)
      return { text: n ? `↳ 子智能体 ${n} 完成` : '↳ 子智能体已完成' }
    }
    // M6：知识召回（引用块展开由 renderEventCard 处理）
    case 'retrieval': {
      const n = Array.isArray(d?.hits) ? d.hits.length : 0
      return { text: `📚 知识召回 · ${n} 条` }
    }
    // M8：本体（经 facade，via=mcp）
    case 'ontology.query':
      return { text: `🔗 本体查询${d?.profile_id ? ` · ${d.profile_id}` : ''}` }
    case 'ontology.unavailable':
      return { text: '⚠ 本体方案不可用 · 已降级', err: true }
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

type EventSource = 'builtin' | 'skill' | 'mcp' | 'onto' | 'subagent' | 'retrieval'

/**
 * 事件来源分类（原型 06 §10 色彩语义：内置 灰 / 技能 绿 / MCP 紫 / 本体 青 / 异常 红；
 * 子智能体取紫族、知识召回取绿族）。
 * §6.5：tool.call/result 已带 source（builtin | skill:{id} | mcp:{server} | ontology:facade | agent:{id}），按前缀采信；
 * 缺失时回退到 tool_name 命名约定：onto_* → 本体；mcp_ 前缀或 server__tool 双下划线 → MCP。
 */
function eventSource(evType: string | undefined, evData: any): EventSource {
  if (evType === 'skill.loaded') return 'skill'
  if (evType === 'subagent.enter' || evType === 'subagent.exit') return 'subagent'
  if (evType === 'retrieval') return 'retrieval'
  if (evType === 'ontology.query' || evType === 'ontology.unavailable') return 'onto'
  const src = evData?.source
  if (typeof src === 'string' && src) {
    if (src === 'builtin') return 'builtin'
    if (src.startsWith('skill:')) return 'skill'
    if (src.startsWith('mcp:')) return 'mcp'
    if (src.startsWith('ontology:')) return 'onto'
    if (src.startsWith('agent:')) return 'subagent'
    return 'onto' // 其余 source 视为本体运行方案 profile_id（§6.5）
  }
  const name = String(evData?.tool_name ?? '')
  if (name.startsWith('onto_')) return 'onto'
  if (name.startsWith('mcp_') || name.includes('__')) return 'mcp'
  return 'builtin'
}

// M8：本体运行方案状态 → 状态点（Badge）/ 文案；仅 running 可选（draft 禁选，PRD FR-11）
function profileBadge(status?: string): 'success' | 'processing' | 'error' | 'default' | 'warning' {
  if (status === 'running') return 'success'
  if (status === 'error') return 'error'
  if (status === 'draft') return 'warning'
  return 'default'
}
function profileStatusText(status?: string): string {
  switch (status) {
    case 'running':
      return '运行中'
    case 'stopped':
      return '已停止'
    case 'draft':
      return '草稿'
    case 'error':
      return '异常'
    default:
      return ''
  }
}

// Bubble 角色映射（X 2.x：role 单数；条目 role 必须命中此处定义的 key）
const BUBBLE_ROLES: BubbleListProps['role'] = {
  user: {
    placement: 'end',
    avatar: <Avatar icon={<UserOutlined />} style={{ background: '#4f46e5', color: '#fff' }} />,
    styles: { content: { background: '#4f46e5', color: '#fff', borderRadius: 12, borderBottomRightRadius: 4 } },
  },
  ai: {
    placement: 'start',
    avatar: <Avatar icon={<RobotOutlined />} style={{ background: '#eef0fe', color: '#4f46e5' }} />,
    styles: { content: { background: '#fff', border: '1px solid var(--c-line)', borderRadius: 12, borderBottomLeftRadius: 4 } },
    // 助手正文走 Markdown（XMarkdown）：流式期间尾部游标，hasNextChunk=false 时收尾刷新
    contentRender: (content, info) => (
      <XMarkdown
        className="chat-md"
        content={String(content ?? '')}
        openLinksInNewTab
        streaming={{ hasNextChunk: !!info.extraInfo?.streaming, tail: true }}
      />
    ),
  },
  event: {
    variant: 'borderless',
    styles: {
      root: { paddingBlock: 4 },
      content: { padding: 0, background: 'transparent', width: '100%', maxWidth: '100%' },
    },
  },
}

/**
 * 中间对话窗口（原型 06 §3.1 / §3.2，Ant Design X Bubble/Sender/ThoughtChain）：
 * - 垂直三段：配置条（单行徽标）/ 消息流（Bubble.List 内建滚动 + autoScroll 置底）/ 输入区；
 * - 阅读尺度：气泡自身 max-width 880 居中（滚动条因此贴对话区最右缘），输入区同宽对齐；
 * - 助手正文：XMarkdown 渲染（流式期间尾部游标），用户消息保持纯文本；
 * - 运行中：消息流右下角悬浮「停止生成」入口，复用 stop()（abort + POST /stop）；
 * - agent 直聊：头部显示智能体名，「配置」打开智能体弹窗；
 * - project 会话：头部注明「项目：xxx · 会话使用的智能体：xxx」；
 * - 执行细节：历史事件回放、token 用量与耗时、深度思考 ThoughtChain、工具调用 JSON、原始事件调试开关
 * - M4-M8：子智能体 / 知识召回 / 本体事件卡（实时与回放共用 describeEvent）；「对话配置」抽屉管理
 *   本体运行方案（仅 running 可选）、知识库（kb_id/enable_kb/top_k/min_score）与已挂技能展示
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
  // 深度思考卡的展开状态（按稳定 evKey 记录，独立于 items，历史重载不丢失）：
  // 无记录时默认「流式中展开、结束后收起」，用户手动开合后以用户选择为准
  const [reasoningOpen, setReasoningOpen] = useState<Record<string, boolean>>({})
  const runRef = useRef<{ abort: () => void; done: Promise<void> } | null>(null)
  const runKeyRef = useRef('')
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
                evKey: `hist-${k}`,
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
            item: { kind: 'event', evType: e.type, eventText: desc.text, eventErr: desc.err, eventWarn: desc.warn, evData: d },
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
    // 连接名已按 `{提供商}·{模型}` 约定时直接展示，避免模型名重复（兼容老数据）
    return conn
      ? conn.name.endsWith(`·${conn.model_name}`)
        ? conn.name
        : `${conn.name} · ${conn.model_name}`
      : agent.model_conn_id
        ? '指定连接'
        : '默认模型'
  })()

  // ---- M8 对话配置 + M5/M6/M7 选项数据 ----
  const [cfgOpen, setCfgOpen] = useState(false)
  const [profiles, setProfiles] = useState<RuntimeProfile[]>([])
  const [kbs, setKbs] = useState<KnowledgeBase[]>([])
  const [skills, setSkills] = useState<Skill[]>([])
  const [profilesErr, setProfilesErr] = useState(false)
  const [kbsErr, setKbsErr] = useState(false)

  // 选项列表（本体运行方案 / 知识库 / 技能）：挂载与打开抽屉时拉取；失败降级为空 + 提示
  const loadCfgOptions = () => {
    api.listRuntimeProfiles().then((ps) => { setProfiles(ps); setProfilesErr(false) }).catch(() => setProfilesErr(true))
    api.listKBs().then((ks) => { setKbs(ks); setKbsErr(false) }).catch(() => setKbsErr(true))
    api.listSkills().then(setSkills).catch(() => {})
  }
  useEffect(loadCfgOptions, [])

  const skillName = (id: string) => skills.find((s) => s.id === id)?.name ?? id

  // 对话级配置落库（M8）：后端 PUT 为 full-replace，必须合并当前会话字段，避免重置 title/kb_id/top_k 等
  const patchConv = async (patch: Partial<Conversation>) => {
    try {
      await api.updateConversation(conversation.id, { ...conversation, ...patch })
      showToast('对话配置已更新')
      onConversationUpdated()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  // 事件卡渲染（ThoughtChain 深度思考 / 工具详情 / 终态摘要）：
  // 紧凑、左侧色条区分来源、与助手文本列对齐（margin-left 44 = 头像 32 + 间距 12）
  const renderEventCard = (it: ChatItem, i: number) => {
    if (it.evType === 'reasoning') {
      const len = it.reasoning?.length ?? 0
      const cardKey = it.evKey ?? `reasoning-${i}`
      const streaming = !!it.streamKey
      const open = reasoningOpen[cardKey] ?? streaming // 流式中默认展开；结束后默认收起，用户可手动开合
      return (
        <ThoughtChain
          key={i}
          className="event-side"
          expandedKeys={open ? [cardKey] : []}
          onExpand={(keys) => setReasoningOpen((prev) => ({ ...prev, [cardKey]: keys.includes(cardKey) }))}
          items={[
            {
              key: cardKey,
              title: `深度思考${len ? `（${len} 字）` : ''}`,
              status: streaming ? 'loading' : 'success',
              icon: <BulbOutlined />,
              collapsible: true,
              content: len ? <pre className="thinking">{it.reasoning}</pre> : undefined,
            },
          ]}
        />
      )
    }
    const isTool = it.evType === 'tool.call' || it.evType === 'tool.result'

    // M6：知识召回引用块（可展开命中片段，绿族强调）
    if (it.evType === 'retrieval') {
      const hits: KBHit[] = Array.isArray(it.evData?.hits) ? it.evData.hits : []
      return (
        <div key={i} className="event-card src-retrieval retrieval-card">
          <span>{it.eventText}</span>
          {hits.length > 0 && (
            <Collapse
              ghost
              size="small"
              items={[{
                key: 'hits',
                label: <span className="event-link">展开命中片段</span>,
                children: (
                  <ul className="retrieval-hits">
                    {hits.map((h, hi) => (
                      <li key={hi} className="retrieval-hit">
                        <div className="retrieval-meta">
                          <span className="retrieval-doc" title={h.doc}>{h.doc}</span>
                          <span className="retrieval-seq">#{h.seq}</span>
                          <span className="retrieval-score">{typeof h.score === 'number' ? h.score.toFixed(3) : '—'}</span>
                        </div>
                        <div className="retrieval-excerpt">{h.excerpt}</div>
                      </li>
                    ))}
                  </ul>
                ),
              }]}
            />
          )}
          {showRaw && it.evData && <pre className="raw-json">{JSON.stringify(it.evData, null, 2)}</pre>}
        </div>
      )
    }

    return (
      <div key={i} className={`event-card src-${eventSource(it.evType, it.evData)}${it.eventErr ? ' err' : ''}${it.eventWarn ? ' warn' : ''}`}>
        <span>{it.eventText}</span>
        {it.evType === 'ontology.unavailable' && (it.evData?.reason != null || it.evData?.detail != null) && (
          <div className="event-detail">{String(it.evData.reason ?? it.evData.detail)}</div>
        )}
        {isTool && it.evData && (
          <Collapse
            ghost
            size="small"
            items={[{
              key: 'detail',
              label: <span className="event-link">详情</span>,
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

  // Bubble.List 数据（消息走 user/ai 角色，事件卡为无边框自定义内容）
  const listItems = useMemo(
    () =>
      items.map((it, i) => {
        if (it.kind === 'msg') {
          return {
            key: `m${i}`,
            role: it.role === 'user' ? 'user' : 'ai',
            content: it.content ?? '',
            loading: !!it.streaming && !it.content,
            extraInfo: { streaming: !!it.streaming }, // 供 contentRender 判断流式状态（尾部游标）
          }
        }
        return {
          key: `e${i}`,
          role: 'event',
          content: renderEventCard(it, i),
        }
      }),
    [items, showRaw, reasoningOpen],
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
            const card: ChatItem = { kind: 'event', evType: 'reasoning', reasoning: payload.delta ?? '', streamKey: runKeyRef.current, evKey: runKeyRef.current }
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
          // 过程类事件（工具/技能/子智能体/知识召回/本体）：统一插到流式助手消息之前
          if (
            event === 'tool.call' || event === 'tool.result' || event === 'skill.loaded' ||
            event === 'subagent.enter' || event === 'subagent.exit' ||
            event === 'retrieval' || event === 'ontology.query' || event === 'ontology.unavailable' ||
            event === 'run.warning'
          ) {
            const desc = describeEvent(event, payload)
            setItems((prev) => {
              const next = [...prev]
              const ev: ChatItem = { kind: 'event', evType: event, eventText: desc.text, eventErr: desc.err, eventWarn: desc.warn, evData: payload }
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
            <Typography.Text strong className="subject">项目：{project.name}</Typography.Text>
            <Tag>会话使用的智能体：{agent ? agent.name : '未配置成员'}</Tag>
          </>
        ) : (
          <Typography.Text strong className="subject">{agent ? agent.name : '对话'}</Typography.Text>
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
        <Space size={4} className="chat-ops">
          <BugOutlined style={{ color: showRaw ? 'var(--ant-color-primary, #4f46e5)' : undefined }} />
          <span style={{ fontSize: 12 }}>调试</span>
          <Switch size="small" checked={showRaw} onChange={setShowRaw} />
          <Button size="small" icon={<SlidersOutlined />} onClick={() => { setCfgOpen(true); loadCfgOptions() }}>
            对话配置
          </Button>
          <Button
            size="small"
            onClick={isProjectScope ? onOpenProjectDrawer : onOpenAgentDrawer}
            disabled={isProjectScope ? !project : !agent}
          >
            配置
          </Button>
        </Space>
      </div>

      <div className={`msg-list${running ? ' running' : ''}`}>
        {items.length === 0 ? (
          <div className="msg-empty">
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
        ) : (
          <Bubble.List items={listItems} role={BUBBLE_ROLES} />
        )}
        {running && (
          <div className="chat-stop">
            <button type="button" className="chat-stop-btn" onClick={stop} title="停止本次生成">
              <span className="chat-stop-glyph" aria-hidden="true" />
              <span>停止生成</span>
            </button>
          </div>
        )}
      </div>

      <div className="composer">
        <div className="composer-inner">
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

      {/* M8 对话配置：紧凑抽屉（保持头部单行），本体运行方案 / 知识库 / 已挂技能 */}
      <Drawer
        open={cfgOpen}
        onClose={() => setCfgOpen(false)}
        title="对话配置"
        width={380}
        styles={{ body: { padding: '16px 20px 28px' } }}
      >
        <section className="cfg-section">
          <div className="cfg-title"><ApartmentOutlined /> 本体运行方案</div>
          <Select
            style={{ width: '100%' }}
            allowClear
            placeholder="未挂载（不启用本体）"
            value={conversation.runtime_profile_id ?? undefined}
            onChange={(v) => patchConv({ runtime_profile_id: v ?? null, ontology_enabled: !!v })}
            options={profiles.map((p) => ({ value: p.id, label: p.name, disabled: p.status !== 'running', status: p.status }))}
            optionRender={(opt) => (
              <Space size={8} style={{ width: '100%' }}>
                <Badge status={profileBadge(opt.data?.status)} />
                <span>{opt.data?.label}</span>
                <span className="cfg-opt-status">{profileStatusText(opt.data?.status)}</span>
              </Space>
            )}
          />
          {profilesErr
            ? <div className="cfg-hint warn">本体运行方案列表暂不可用</div>
            : <div className="cfg-hint">仅 running 方案可选；draft / stopped / error 禁选（FR-11）。</div>}
        </section>

        <section className="cfg-section">
          <div className="cfg-title"><DatabaseOutlined /> 知识库</div>
          <Select
            style={{ width: '100%' }}
            allowClear
            placeholder="未选择知识库"
            value={conversation.kb_id ?? undefined}
            onChange={(v) => patchConv({ kb_id: v ?? null })}
            options={kbs.map((k) => ({ value: k.id, label: k.name }))}
            notFoundContent={kbsErr ? '知识库列表暂不可用' : undefined}
          />
          <div className="cfg-row">
            <span className="cfg-row-label">启用检索</span>
            <Switch
              size="small"
              checked={conversation.enable_kb}
              disabled={!conversation.kb_id}
              onChange={(v) => patchConv({ enable_kb: v })}
            />
          </div>
          <div className="cfg-row">
            <span className="cfg-row-label">召回条数 top_k</span>
            <InputNumber
              size="small"
              min={1}
              max={20}
              value={conversation.top_k}
              onChange={(v) => patchConv({ top_k: typeof v === 'number' ? v : conversation.top_k })}
            />
          </div>
          <div className="cfg-row">
            <span className="cfg-row-label">最低分 min_score</span>
            <InputNumber
              size="small"
              min={0}
              max={1}
              step={0.05}
              value={conversation.min_score}
              onChange={(v) => patchConv({ min_score: typeof v === 'number' ? v : conversation.min_score })}
            />
          </div>
          <div className="cfg-hint">「启用检索」开启且已选知识库时，召回片段随回答注入并显示为「知识召回」卡片。</div>
        </section>

        <section className="cfg-section">
          <div className="cfg-title"><ThunderboltOutlined /> 已挂技能</div>
          {agent?.skills?.length
            ? (
              <Space size={[6, 6]} wrap>
                {agent.skills.map((id) => <Tag key={id} color="green" style={{ marginInlineEnd: 0 }}>{skillName(id)}</Tag>)}
              </Space>
            )
            : <div className="cfg-hint">当前智能体未挂载技能</div>}
        </section>
      </Drawer>
    </div>
  )
}
