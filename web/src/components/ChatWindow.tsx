import { useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Avatar, Alert, Button, Collapse, Dropdown, Input, Popover, Space, Switch, Tag, Tooltip, Typography } from 'antd'
import { AppstoreOutlined, BookOutlined, BugOutlined, BulbOutlined, ClusterOutlined, ThunderboltOutlined, UserOutlined } from '@ant-design/icons'
import { Bubble, Sender, ThoughtChain, Welcome } from '@ant-design/x'
import type { BubbleListProps } from '@ant-design/x'
import XMarkdown from '@ant-design/x-markdown'
import { api, resumeConversation, runConversation } from '../api/client'
import EventReplayDrawer from './EventReplayDrawer'
import type {
  Agent,
  Conversation,
  KBHit,
  KnowledgeBase,
  Message,
  Project,
  RuntimeProfile,
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
  subDepth?: number // REQ-117：子智能体嵌套深度（缩进渲染）
}

// 子 Agent 名（§6.5 subagent.enter/exit payload = 子 Agent 名；字段名做兼容取值）
function subagentName(d: any): string {
  return String(d?.name ?? d?.agent_name ?? d?.sub_agent ?? d?.agent ?? '').trim()
}

// 事件卡文案（实时流与历史回放共用：新增事件必须在此登记，两条路径才一致）。
// 导出供事件重放视图（M17 阶段二 EventReplayDrawer）复用同一文案源。
export function describeEvent(type: string, d: any): { text: string; err?: boolean; warn?: boolean } {
  switch (type) {
    case 'run.started':
      return { text: `▶ 运行开始 · ${d?.agent_name ?? ''} · ${d?.model ?? ''}`.replace(/ ·\s*$/, '') }
    case 'run.finished':
      return d?.reason === 'stopped' ? { text: '⏹ 已停止' } : d?.reason === 'interrupted' ? { text: '⏸ 已挂起 · 等待答复' } : { text: finishSummary(d) }
    // M11 收尾 + REQ-14 审批：中断恢复（ask_human 答复 / 工具审批）
    case 'run.interrupted':
      return d?.kind === 'approval' || (!d?.question && d?.tool_name)
        ? { text: `⏸ 工具审批 · ${d?.tool_name ?? ''}` }
        : { text: `⏸ 等待答复 · ${d?.question ?? ''}` }
    case 'run.warning':
      return { text: `⚠ 运行警告 · ${d?.message ?? ''}`.replace(/ ·\s*$/, ''), warn: true }
    case 'run.error':
      return { text: `⚠ ${d?.message ?? '运行失败'}`, err: true }
    case 'model.step': {
      // REQ-117/M17：模型调用链路（调试观测）
      const parts = [`🧠 模型调用 #${d?.seq ?? '?'}`]
      if (d?.agent) parts.push(String(d.agent))
      if (typeof d?.duration_ms === 'number') parts.push(d.duration_ms >= 1000 ? `${(d.duration_ms / 1000).toFixed(1)}s` : `${d.duration_ms}ms`)
      const u = d?.usage
      if (u && typeof u.total_tokens === 'number') parts.push(`tokens ${u.prompt_tokens ?? 0}+${u.completion_tokens ?? 0}=${u.total_tokens}`)
      if (d?.input_count) parts.push(`输入 ${d.input_count} 条/${d.input_chars ?? 0} 字`)
      return { text: parts.join(' · ') }
    }
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

/** 会话配置 chip（渲染在 Sender footer 内）：ON = 品牌填充，OFF = 浅色描边；禁用置灰 + Tooltip 说明 */
function ChatChip({ on, disabled, icon, label, title, onClick }: {
  on: boolean
  disabled: boolean
  icon: ReactNode
  label: string
  title: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      className={`chat-chip${on ? ' on' : ''}${disabled ? ' disabled' : ''}`}
      aria-pressed={on}
      aria-disabled={disabled || undefined}
      title={disabled ? undefined : title}
      onClick={onClick}
    >
      <span className="chat-chip-icon">{icon}</span>
      <span>{label}</span>
    </button>
  )
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
    // 智能体标识：与顶栏品牌同源的三节点网络标记（侧栏节点 / 空态保持一致）
    avatar: (
      <Avatar shape="square" className="agent-avatar">
        <span className="agent-glyph" />
      </Avatar>
    ),
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
 * - M4-M8：子智能体 / 知识召回 / 本体事件卡（实时与回放共用 describeEvent）；
 *   会话级配置为输入卡内底部的三个开关 chip（知识库 / 本体 / 技能，X Sender footer），
 *   随改随存（merge-safe patchConv）；具体参数在「知识库」页与智能体属性中维护
 */
export default function ChatWindow({
  conversation,
  agents,
  projects,
  onOpenAgentDrawer,
  onOpenProjectDrawer,
  onConversationUpdated,
  sidePanelOpen,
  onToggleSidePanel,
}: {
  conversation: Conversation
  agents: Agent[]
  projects: Project[]
  onOpenAgentDrawer: () => void
  onOpenProjectDrawer: () => void
  onConversationUpdated: () => void
  /** REQ-102：项目侧边栏开合（仅 project scope 提供；由页面持有状态与面板） */
  sidePanelOpen?: boolean
  onToggleSidePanel?: () => void
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
  // REQ-117/M17 三档观测级别：0 简洁 / 1 详细 / 2 调试（每会话记忆；仅影响之后的运行）
  const [debugLevel, setDebugLevel] = useState(() => Number(localStorage.getItem(`eino.debug.${conversation?.id}`)) || 0)
  useEffect(() => {
    setDebugLevel(Number(localStorage.getItem(`eino.debug.${conversation?.id}`)) || 0)
  }, [conversation?.id])
  const changeDebugLevel = (lv: number) => {
    setDebugLevel(lv)
    if (conversation?.id) localStorage.setItem(`eino.debug.${conversation.id}`, String(lv))
  }
  // 深度思考卡的展开状态（按稳定 evKey 记录，独立于 items，历史重载不丢失）：
  // 无记录时默认「流式中展开、结束后收起」，用户手动开合后以用户选择为准
  const [reasoningOpen, setReasoningOpen] = useState<Record<string, boolean>>({})
  const runRef = useRef<{ abort: () => void; done: Promise<void> } | null>(null)
  const runKeyRef = useRef('')

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

  // ---- 会话配置 chips 的选项数据（知识库 / 本体运行方案）----
  const [profiles, setProfiles] = useState<RuntimeProfile[]>([])
  const [kbs, setKbs] = useState<KnowledgeBase[]>([])
  const [profilesErr, setProfilesErr] = useState(false)
  const [kbsErr, setKbsErr] = useState(false)
  const [picker, setPicker] = useState<'kb' | 'onto' | null>(null)

  // 选项列表：挂载时拉取；失败降级为空 + chip 置灰说明
  const loadCfgOptions = () => {
    api.listRuntimeProfiles().then((ps) => { setProfiles(ps); setProfilesErr(false) }).catch(() => setProfilesErr(true))
    api.listKBs().then((ks) => { setKbs(ks); setKbsErr(false) }).catch(() => setKbsErr(true))
  }
  useEffect(loadCfgOptions, [])

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

  // ---- 会话配置 chips（Sender footer 内）：纯开关，ON = 品牌填充 / OFF = 浅色描边 ----
  // 规则：ON 时若已绑定则直接启用；未绑定则弹轻量单选列表（选后记住绑定，写入会话字段）
  const agentSkills = agent?.skills ?? []
  const runningProfiles = profiles.filter((p) => p.status === 'running')
  const kbBound = conversation.kb_id ? kbs.find((k) => k.id === conversation.kb_id) ?? null : null
  const ontoBound = conversation.runtime_profile_id ? profiles.find((p) => p.id === conversation.runtime_profile_id) ?? null : null
  const skillsOn = conversation.enable_skills ?? true // 后端列待跟进：默认开，保持既有行为

  const kbDisabled = kbs.length === 0
  const ontoDisabled = runningProfiles.length === 0
  const skillsDisabled = agentSkills.length === 0

  const kbHint = kbDisabled
    ? (kbsErr ? '知识库列表暂不可用' : '暂无可用知识库，请先在「知识库」页创建')
    : (conversation.enable_kb ? `知识检索已开启${kbBound ? `（${kbBound.name}）` : ''}` : '开启知识检索')
  const ontoHint = ontoDisabled
    ? (profilesErr ? '本体运行方案列表暂不可用' : '暂无 running 状态的本体运行方案，请先在本体页启动')
    : (conversation.ontology_enabled ? `本体增强已开启${ontoBound ? `（${ontoBound.name}）` : ''}` : '开启本体增强')
  const skillsHint = skillsDisabled
    ? '该智能体未挂载技能，请在智能体属性中配置'
    : (skillsOn ? `技能已启用（${agentSkills.length} 个）` : '技能已停用')

  const toggleKb = () => {
    if (picker === 'kb') { setPicker(null); return }
    if (kbDisabled) return
    if (conversation.enable_kb) { patchConv({ enable_kb: false }); return } // 关闭但保留 kb_id 绑定
    if (conversation.kb_id) { patchConv({ enable_kb: true }); return }
    setPicker('kb')
  }
  const toggleOnto = () => {
    if (picker === 'onto') { setPicker(null); return }
    if (ontoDisabled) return
    if (conversation.ontology_enabled) { patchConv({ ontology_enabled: false }); return }
    if (conversation.runtime_profile_id) { patchConv({ ontology_enabled: true }); return }
    setPicker('onto')
  }
  const toggleSkills = () => {
    if (skillsDisabled) return
    patchConv({ enable_skills: !skillsOn })
  }

  // 轻量单选列表（Popover 内容）：Esc / 点击外部关闭，非 Modal
  const pickerList = (
    title: string,
    items: { id: string; name: string; meta?: string }[],
    current: string | null,
    onPick: (id: string) => void,
  ) => (
    <div className="chip-picker" onKeyDown={(e) => { if (e.key === 'Escape') setPicker(null) }}>
      <div className="chip-picker-title">{title}</div>
      <div className="chip-picker-list">
        {items.map((it, i) => (
          <button
            key={it.id}
            type="button"
            autoFocus={i === 0}
            className={`chip-picker-item${it.id === current ? ' current' : ''}`}
            onClick={() => { setPicker(null); onPick(it.id) }}
          >
            <span className="chip-picker-name">{it.name}</span>
            {it.meta ? <span className="chip-picker-meta">{it.meta}</span> : null}
          </button>
        ))}
      </div>
    </div>
  )

  const kbChip = <ChatChip on={conversation.enable_kb} disabled={kbDisabled} icon={<BookOutlined />} label="知识库" title={kbHint} onClick={toggleKb} />
  const ontoChip = <ChatChip on={conversation.ontology_enabled} disabled={ontoDisabled} icon={<ClusterOutlined />} label="本体" title={ontoHint} onClick={toggleOnto} />
  const skillsChip = <ChatChip on={skillsOn} disabled={skillsDisabled} icon={<ThunderboltOutlined />} label="技能" title={skillsHint} onClick={toggleSkills} />

  // 事件卡渲染（ThoughtChain 深度思考 / 工具详情 / 终态摘要）：
  // 紧凑、左侧色条区分来源、与助手文本列对齐（margin-left 44 = 头像 32 + 间距 12）
  // REQ-117：按 enter/exit 序列计算子智能体嵌套深度（回放与实时共用，渲染时缩进）
  const withSubDepth = (list: ChatItem[]): ChatItem[] => {
    let depth = 0
    return list.map((it) => {
      if (it.kind !== 'event') return it
      if (it.evType === 'subagent.enter') {
        const d = depth
        depth += 1
        return { ...it, subDepth: d }
      }
      if (it.evType === 'subagent.exit') {
        depth = Math.max(0, depth - 1)
        return { ...it, subDepth: depth }
      }
      return { ...it, subDepth: depth }
    })
  }

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
    if (it.evType === 'model.step') {
      const msgs: any[] = Array.isArray(it.evData?.input) ? it.evData.input : []
      const tools: any[] = Array.isArray(it.evData?.tools) ? it.evData.tools : []
      const roleLabel: Record<string, string> = { user: '用户', assistant: '助手', system: '系统', tool: '工具' }
      return (
        <div key={i} className="event-card src-builtin" style={{ marginLeft: (it.subDepth ?? 0) * 14 }}>
          <span>{it.eventText}</span>
          {(msgs.length > 0 || tools.length > 0) && (
            <Collapse
              ghost
              size="small"
              items={[{
                key: 'detail',
                label: <span className="event-link">调用链路详情</span>,
                children: (
                  <div>
                    {msgs.length > 0 && (
                      <>
                        <div style={{ fontSize: 11, color: 'var(--ant-color-text-secondary, #888)', marginBottom: 4 }}>
                          本次模型输入（{msgs.length} 条，含历史与系统提示词）
                        </div>
                        <pre className="raw-json">{msgs.map((m, mi) => `[${mi + 1}] ${roleLabel[m.role] ?? m.role}（${m.chars} 字）${m.content ? `\n${m.content}` : m.preview ? `\n${m.preview}` : ''}`).join('\n\n')}</pre>
                      </>
                    )}
                    {tools.length > 0 && (
                      <>
                        <div style={{ fontSize: 11, color: 'var(--ant-color-text-secondary, #888)', margin: '6px 0 4px' }}>
                          本次绑定工具（{tools.length}）
                        </div>
                        <pre className="raw-json">{tools.map((tt) => `• ${tt.name}${tt.desc ? `：${tt.desc}` : ''}`).join('\n')}</pre>
                      </>
                    )}
                  </div>
                ),
              }]}
            />
          )}
        </div>
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

    const assembly = it.evType === 'run.started' ? it.evData?.assembly : undefined
    return (
      <div key={i} className={`event-card src-${eventSource(it.evType, it.evData)}${it.eventErr ? ' err' : ''}${it.eventWarn ? ' warn' : ''}`} style={{ marginLeft: (it.subDepth ?? 0) * 14 }}>
        <span>{it.eventText}</span>
        {assembly && (
          <Collapse
            ghost
            size="small"
            items={[{
              key: 'assembly',
              label: <span className="event-link">装配快照（{assembly.mode} · {(assembly.agents ?? []).length} 个智能体）</span>,
              children: (
                <pre className="raw-json">{(assembly.agents ?? []).map((a: any, ai: number) => {
                  const lines = [`[${ai + 1}] ${a.name}（${a.role}）· ${a.model}`, `   工具: ${(a.tools ?? []).map((x: any) => x.name).join(', ') || '无'}`, `   技能: ${(a.skills ?? []).join(', ') || '无'}`, `   MCP: ${(a.mcp ?? []).join(', ') || '无'}`]
                  if (a.instruction) lines.push(`   指令: ${a.instruction}`)
                  return lines.join('\n')
                }).join('\n')}</pre>
              ),
            }]}
          />
        )}
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
      withSubDepth(items).map((it, i) => {
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

  // 中断恢复（M11 收尾 + REQ-14 审批）：会话挂起的 ask_human 提问 / 工具审批（随会话数据同步）
  const [interrupt, setInterrupt] = useState<{
    kind: 'ask_human' | 'approval'
    question: string
    choices: string[]
    toolName: string
    arguments: string
  } | null>(null)
  const [answer, setAnswer] = useState('')
  const [replayOpen, setReplayOpen] = useState(false)
  useEffect(() => {
    try {
      const st = conversation.interrupt_state ? JSON.parse(conversation.interrupt_state) : null
      setInterrupt(
        st && (st.question || st.tool_name)
          ? {
              kind: st.kind === 'approval' || (!st.question && st.tool_name) ? 'approval' : 'ask_human',
              question: st.question ?? '',
              choices: Array.isArray(st.choices) ? st.choices : [],
              toolName: st.tool_name ?? '',
              arguments: st.arguments ?? '',
            }
          : null,
      )
    } catch {
      setInterrupt(null)
    }
  }, [conversation.id, conversation.interrupt_state])

  // 运行/恢复共用的事件翻译（send 与 resume 的 SSE 处理一致）
  const handleRunEvent = ({ event, data }: { event: string; data: any }) => {
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
              if (!last.content) {
                last.content =
                  payload.reason === 'stopped' ? '（已停止生成）' : payload.reason === 'interrupted' ? '（已暂停，等待你的答复）' : ''
              }
            }
          }
          const ev: ChatItem = { kind: 'event', evType: event, eventText: desc.text, evData: payload }
          next.splice(next.length - 1, 0, ev)
          return next
        })
        break
      }
      case 'run.interrupted': {
        const desc = describeEvent(event, payload)
        setInterrupt({
          kind: payload.kind === 'approval' || (!payload.question && payload.tool_name) ? 'approval' : 'ask_human',
          question: payload.question ?? '',
          choices: Array.isArray(payload.choices) ? payload.choices : [],
          toolName: payload.tool_name ?? '',
          arguments: payload.arguments ?? '',
        })
        setItems((prev) => {
          const next = [...prev]
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
          event === 'run.warning' || event === 'model.step'
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
  }

  // streamStart 运行/恢复公共段：追加流式助手消息 → 消费 SSE → 收尾刷新
  const streamStart = async (
    start: (handler: (ev: { event: string; data: any }) => void) => { abort: () => void; done: Promise<void> },
  ) => {
    setItems((prev) => [...prev, { kind: 'msg', role: 'assistant', content: '', streaming: true }])
    setRunning(true)
    const aborter = start(handleRunEvent)
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
    await streamStart((handler) => runConversation(conversation.id, text, debugLevel, handler))
  }

  // resume 答复挂起中断（ask_human 自由答复 / 审批 批准|拒绝），事件流与运行同构
  const resume = async (decision?: string) => {
    const isApproval = interrupt?.kind === 'approval'
    const text = isApproval ? (decision ?? '') : answer.trim()
    if (!text || running || !interrupt) return
    setAnswer('')
    setInterrupt(null)
    runKeyRef.current = `resume-${Date.now()}`
    setItems((prev) => [
      ...prev,
      {
        kind: 'msg' as const,
        role: 'user' as const,
        content: isApproval
          ? `[审批] ${decision === 'approve' ? '批准' : '拒绝'} · ${interrupt.toolName}`
          : text,
      },
    ])
    await streamStart((handler) => resumeConversation(conversation.id, text, debugLevel, handler))
  }

  // M17 阶段二：事件流导出（JSON 全量，供归档/外部重放）
  const exportEventsJSON = async () => {
    try {
      const evs = await api.listEvents(conversation.id)
      const blob = new Blob([JSON.stringify(evs, null, 2)], { type: 'application/json;charset=utf-8' })
      const a = document.createElement('a')
      a.href = URL.createObjectURL(blob)
      a.download = `(conversation.title || '对话').replace(/[\/:*?"<>|]/g, '_') + '-events.json'`
      a.click()
      URL.revokeObjectURL(a.href)
    } catch (e: any) {
      showToast(e?.message ?? '导出失败', 'err')
    }
  }

  const stop = () => {
    runRef.current?.abort()
    api.stopConversation(conversation.id).catch(() => {})
  }

  // REQ-113①：导出对话为 Markdown 下载（events=含过程事件附录）
  const exportMarkdown = async (withEvents: boolean) => {
    try {
      const md = await api.exportConversation(conversation.id, withEvents)
      const blob = new Blob([md], { type: 'text/markdown;charset=utf-8' })
      const a = document.createElement('a')
      a.href = URL.createObjectURL(blob)
      a.download = `${(conversation.title || '对话').replace(/[\\/:*?"<>|]/g, '_')}.md`
      a.click()
      URL.revokeObjectURL(a.href)
    } catch (e: any) {
      showToast(e?.message ?? '导出失败', 'err')
    }
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
        {isProjectScope && project && <Tag>{project.collab_mode}</Tag>}
        {!isProjectScope && agent && (
          <>
            <Tag>{agent.runtime_backend}</Tag>
            <Tag>最大迭代 {agent.max_iteration}</Tag>
          </>
        )}
        <span className="spacer" />
        <Space size={4} className="chat-ops">
          {/* REQ-113①：对话导出 Markdown（仅消息 / 含过程事件附录） */}
          <Dropdown
            menu={{
              items: [
                { key: 'msg', label: '导出 Markdown（仅消息）' },
                { key: 'full', label: '导出 Markdown（含过程事件）' },
                { key: 'events-json', label: '导出事件流（JSON）' },
              ],
              onClick: ({ key }) => {
                if (key === 'events-json') exportEventsJSON()
                else exportMarkdown(key === 'full')
              },
            }}
            disabled={!conversation.id}
          >
            <Button size="small">导出</Button>
          </Dropdown>
          <Button size="small" onClick={() => setReplayOpen(true)}>重放</Button>
          <Dropdown
            trigger={["click"]}
            menu={{
              selectable: true,
              selectedKeys: [String(debugLevel)],
              items: [
                { key: '0', label: '简洁（默认）' },
                { key: '1', label: '详细 · 装配快照/分步用量/工具耗时' },
                { key: '2', label: '调试 · 另附模型输入全文/工具 schema' },
              ],
              onClick: ({ key }) => changeDebugLevel(Number(key)),
            }}
            disabled={running || !conversation.id}
          >
            <Button size="small" icon={<BugOutlined />} style={{ color: debugLevel > 0 ? 'var(--ant-color-primary, #4f46e5)' : undefined }}>
              调试{debugLevel > 0 ? ' L' + debugLevel : ''}
            </Button>
          </Dropdown>
          <Tooltip title="显示事件原始 JSON">
            <Switch size="small" checked={showRaw} onChange={setShowRaw} />
          </Tooltip>
          <Button
            size="small"
            onClick={isProjectScope ? onOpenProjectDrawer : onOpenAgentDrawer}
            disabled={isProjectScope ? !project : !agent}
          >
            配置
          </Button>
          {onToggleSidePanel && ((isProjectScope && project) || (!isProjectScope && agent)) && (
            <Tooltip
              title={
                sidePanelOpen
                  ? '收起侧边栏'
                  : isProjectScope
                    ? '项目侧边栏（文件 / Git / 配置）'
                    : '智能体侧边栏（配置）'
              }
            >
              <Button
                size="small"
                type={sidePanelOpen ? 'primary' : 'default'}
                icon={<AppstoreOutlined />}
                aria-label={isProjectScope ? '项目侧边栏' : '智能体侧边栏'}
                onClick={onToggleSidePanel}
              />
            </Tooltip>
          )}
        </Space>
      </div>

      <div className={`msg-list${running ? ' running' : ''}`}>
        {items.length === 0 ? (
          <div className="msg-empty">
            <Welcome
              variant="borderless"
              icon={<span className="agent-tile"><span className="agent-glyph" /></span>}
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

      {/* 中断恢复（M11 收尾 + REQ-14 审批）：ask_human 答复卡 / 工具审批卡 */}
      {interrupt && !running && (
        <div className="composer chat-interrupt">
          {interrupt.kind === 'approval' ? (
            <Alert
              type="warning"
              showIcon
              message={`工具调用等待审批：${interrupt.toolName}`}
              description={
                <div className="chat-interrupt-body">
                  <pre className="chat-interrupt-args">{interrupt.arguments || '（无参数）'}</pre>
                  <Space size={8}>
                    <Button type="primary" onClick={() => resume('approve')}>批准并执行</Button>
                    <Button danger onClick={() => resume('deny')}>拒绝</Button>
                  </Space>
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    批准后工具将真实执行；拒绝会把拒答结果返回给智能体。也可直接发送新消息（放弃本次审批）。
                  </Typography.Text>
                </div>
              }
            />
          ) : (
            <Alert
              type="warning"
              showIcon
              message={`智能体需要你的输入：${interrupt.question}`}
              description={
                <div className="chat-interrupt-body">
                  {interrupt.choices.length > 0 && (
                    <Space size={6} wrap>
                      {interrupt.choices.map((c) => (
                        <Button key={c} size="small" onClick={() => setAnswer(c)}>{c}</Button>
                      ))}
                    </Space>
                  )}
                  <Space.Compact style={{ width: '100%' }}>
                    <Input
                      value={answer}
                      onChange={(e) => setAnswer(e.target.value)}
                      placeholder="输入你的答复…"
                      onPressEnter={() => resume()}
                      autoFocus
                    />
                    <Button type="primary" onClick={() => resume()} disabled={!answer.trim()}>答复并继续</Button>
                  </Space.Compact>
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    也可直接发送新消息（将放弃本次提问，按新问题运行）。
                  </Typography.Text>
                </div>
              }
            />
          )}
        </div>
      )}

      <div className="composer">
        <div className="composer-inner">
          {/* 会话配置 chips 渲染在输入卡内底部（X Sender footer）：知识库 / 本体 / 技能 三个纯开关 */}
          <Sender
            value={input}
            onChange={setInput}
            onSubmit={() => send()}
            onCancel={stop}
            loading={running}
            placeholder={placeholder}
            disabled={!canSend}
            footer={
              <div className="chat-chips">
                {kbDisabled ? (
                  <Tooltip title={kbHint}><span className="chip-slot">{kbChip}</span></Tooltip>
                ) : (
                  <Popover
                    open={picker === 'kb'}
                    onOpenChange={(o) => { if (!o) setPicker(null) }}
                    trigger="click"
                    placement="topLeft"
                    arrow={false}
                    content={pickerList(
                      '选择知识库',
                      kbs.map((k) => ({ id: k.id, name: k.name, meta: typeof k.doc_count === 'number' ? `${k.doc_count} 文档` : undefined })),
                      conversation.kb_id,
                      (id) => patchConv({ kb_id: id, enable_kb: true }),
                    )}
                  >
                    <span className="chip-slot">{kbChip}</span>
                  </Popover>
                )}
                {ontoDisabled ? (
                  <Tooltip title={ontoHint}><span className="chip-slot">{ontoChip}</span></Tooltip>
                ) : (
                  <Popover
                    open={picker === 'onto'}
                    onOpenChange={(o) => { if (!o) setPicker(null) }}
                    trigger="click"
                    placement="topLeft"
                    arrow={false}
                    content={pickerList(
                      '选择本体运行方案',
                      runningProfiles.map((p) => ({ id: p.id, name: p.name, meta: p.engine })),
                      conversation.runtime_profile_id,
                      (id) => patchConv({ runtime_profile_id: id, ontology_enabled: true }),
                    )}
                  >
                    <span className="chip-slot">{ontoChip}</span>
                  </Popover>
                )}
                {skillsDisabled ? <Tooltip title={skillsHint}><span className="chip-slot">{skillsChip}</span></Tooltip> : skillsChip}
                <span className="chat-chips-hint">Enter 发送 · Shift+Enter 换行</span>
              </div>
            }
          />
        </div>
      </div>

      {replayOpen && (
        <EventReplayDrawer
          conversationId={conversation.id}
          title={conversation.title || subjectName || '对话'}
          open={replayOpen}
          onClose={() => setReplayOpen(false)}
        />
      )}
    </div>
  )
}
