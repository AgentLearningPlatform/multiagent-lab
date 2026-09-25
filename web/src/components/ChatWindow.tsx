import { useEffect, useMemo, useRef, useState } from 'react'
import type { CSSProperties, ReactNode } from 'react'
import { Avatar, Alert, Button, Checkbox, Collapse, Dropdown, Input, InputNumber, Modal, Popover, Segmented, Select, Space, Switch, Tag, Tooltip, Typography } from 'antd'
import { AppstoreOutlined, BookOutlined, BugOutlined, BulbOutlined, ClusterOutlined, ThunderboltOutlined, UserOutlined } from '@ant-design/icons'
import { Bubble, Sender, ThoughtChain, Welcome } from '@ant-design/x'
import type { BubbleListProps } from '@ant-design/x'
import XMarkdown from '@ant-design/x-markdown'
import { api, connDisplayName, resumeConversation, runConversation } from '../api/client'
import type { ComparePaneConfig } from '../api/client'
import EventReplayDrawer from './EventReplayDrawer'
import { AgentLogo } from './AgentLogo'
import type {
  Agent,
  Conversation,
  KBHit,
  KnowledgeBase,
  Message,
  ModelConnection,
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

// REQ-19f/143 对比窗格单项覆盖（UI 本地形态：''= 继承对话当前配置；提交时映射 ComparePaneConfig）
interface PaneSel {
  agent: string // ''= 继承对话所属/项目主智能体；否则窗格级独立装配（REQ-143）
  model: string
  kb: string
  profile: string
  noHistory: boolean // REQ-143③：不携带对话历史（干净对照）
  temperature: number | null // REQ-144：推理参数覆盖（null=继承）
  instruction: string // REQ-144：系统提示词临时改写（''=继承）
  skills: '' | 'on' | 'off' // REQ-144：技能开关（''=继承，REQ-19g 口径）
}
const BLANK_PANE: PaneSel = { agent: '', model: '', kb: '', profile: '', noHistory: false, temperature: null, instruction: '', skills: '' }

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

/** REQ-149① 展示级别门控：简洁档（level 0）隐藏调试细节事件（model.step 等），详细/调试档展开 */
export function levelGated(type: string, level: number): boolean {
  return level < 1 && (type === 'model.step' || type === 'debug.cli')
}

/** 会话挂起中断卡数据（run.interrupted payload → 组件状态；实时与对比窗格共用） */
function toInterruptState(payload: any) {
  return {
    kind: (payload.kind === 'approval' || (!payload.question && payload.tool_name) ? 'approval' : 'ask_human') as 'approval' | 'ask_human',
    question: payload.question ?? '',
    choices: Array.isArray(payload.choices) ? payload.choices : [],
    toolName: payload.tool_name ?? '',
    arguments: payload.arguments ?? '',
  }
}

/**
 * 单路事件流归约（REQ-19e/19f：单路消息区与对比窗格共用同一事件→条目翻译）。
 * runKey 为本次流的自定义合并键（reasoning 卡按它合并增量）。
 */
function applyRunEvent(prev: ChatItem[], event: string, payload: any, runKey: string): ChatItem[] {
  switch (event) {
    case 'run.started':
    case 'run.finished': {
      const desc = describeEvent(event, payload)
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
    }
    case 'run.interrupted': {
      const desc = describeEvent(event, payload)
      const next = [...prev]
      const ev: ChatItem = { kind: 'event', evType: event, eventText: desc.text, evData: payload }
      next.splice(next.length - 1, 0, ev)
      return next
    }
    case 'reasoning.delta': {
      const next = [...prev]
      for (let i = next.length - 1; i >= 0; i--) {
        if (next[i].evType === 'reasoning' && next[i].streamKey === runKey) {
          next[i] = { ...next[i], reasoning: (next[i].reasoning ?? '') + (payload.delta ?? '') }
          return next
        }
      }
      const card: ChatItem = { kind: 'event', evType: 'reasoning', reasoning: payload.delta ?? '', streamKey: runKey, evKey: runKey }
      next.splice(Math.max(next.length - 1, 0), 0, card)
      return next
    }
    case 'message.delta': {
      const next = [...prev]
      const last = next[next.length - 1]
      if (last && last.kind === 'msg' && last.streaming) last.content += payload.delta ?? ''
      return next
    }
    case 'run.error': {
      const msg = payload.message ?? '运行失败'
      const next = [...prev]
      const last = next[next.length - 1]
      if (last && last.kind === 'msg' && last.streaming && !last.content) next.pop()
      const ev: ChatItem = { kind: 'event', evType: 'run.error', eventErr: true, eventText: `⚠ ${msg}`, evData: payload }
      return [...next, ev]
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
        const next = [...prev]
        const ev: ChatItem = { kind: 'event', evType: event, eventText: desc.text, eventErr: desc.err, eventWarn: desc.warn, evData: payload }
        next.splice(next.length - 1, 0, ev) // 流式助手消息存在时插到其前
        return next
      }
      return prev
  }
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
  onConversationUpdated,
  sidePanelOpen,
  onToggleSidePanel,
}: {
  conversation: Conversation
  agents: Agent[]
  projects: Project[]
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
  const [showRaw, setShowRaw] = useState(false) // 原始事件 JSON 调试开关（并入过程展示面板）
  // REQ-135②③：对话级过程展示配置——粒度 all|key|off + 深度思考显隐（localStorage 按会话记忆）
  const [granularity, setGranularity] = useState<'all' | 'key' | 'off'>('all')
  const [showReasoning, setShowReasoning] = useState(true)
  // REQ-117/M17 三档观测级别：0 简洁 / 1 详细 / 2 调试（每会话记忆；仅影响之后的运行）
  const [debugLevel, setDebugLevel] = useState(() => Number(localStorage.getItem(`eino.debug.${conversation?.id}`)) || 0)
  useEffect(() => {
    setDebugLevel(Number(localStorage.getItem(`eino.debug.${conversation?.id}`)) || 0)
  }, [conversation?.id])
  const changeDebugLevel = (lv: number) => {
    setDebugLevel(lv)
    if (conversation?.id) localStorage.setItem(`eino.debug.${conversation.id}`, String(lv))
    // REQ-149②：数据前提联动——级别≥1 而入库关闭时，明确提示历史生效范围
    if (lv >= 1 && !debugPersist) {
      showToast('详细/调试档仅影响之后的运行；未开启「调试事件入库」，调试细节不会留存到历史')
    }
  }
  // REQ-135②：对话级过程展示配置（粒度/深度思考），localStorage 按会话记忆，切会话回填
  const convCfgKey = `eino.convcfg.${conversation?.id}`
  useEffect(() => {
    try {
      const cfg = JSON.parse(localStorage.getItem(convCfgKey) || '{}')
      setGranularity(cfg.granularity ?? 'all')
      setShowReasoning(cfg.showReasoning ?? true)
      setDebugPersist(cfg.debugPersist ?? false)
    } catch {
      setGranularity('all')
      setShowReasoning(true)
      setDebugPersist(false)
    }
  }, [convCfgKey])
  // REQ-149②：调试事件入库开关（对话级记忆；级别≥1 时产生 model.step/装配快照落库）
  const [debugPersist, setDebugPersist] = useState(false)
  const patchConvCfg = (patch: { granularity?: 'all' | 'key' | 'off'; showReasoning?: boolean; debugPersist?: boolean }) => {
    if (!conversation?.id) return
    let cfg: any = {}
    try {
      cfg = JSON.parse(localStorage.getItem(convCfgKey) || '{}')
    } catch { /* 忽略坏数据 */ }
    localStorage.setItem(convCfgKey, JSON.stringify({ ...cfg, ...patch }))
    if (patch.granularity) setGranularity(patch.granularity)
    if (patch.showReasoning !== undefined) setShowReasoning(patch.showReasoning)
    if (patch.debugPersist !== undefined) setDebugPersist(patch.debugPersist)
  }
  // 深度思考卡的展开状态（按稳定 evKey 记录，独立于 items，历史重载不丢失）：
  // 无记录时默认「流式中展开、结束后收起」，用户手动开合后以用户选择为准
  const [reasoningOpen, setReasoningOpen] = useState<Record<string, boolean>>({})
  const runRef = useRef<{ abort: () => void; done: Promise<void> } | null>(null)
  const runKeyRef = useRef('')

  // 历史还原：消息表（对话正文）+ 事件表（执行时间线）按时间合并
  useEffect(() => {
    // B3（research/ChatWindow渲染与SSE链路审查）：切换会话先中止旧流，防止旧会话事件写入新会话列表
    runRef.current?.abort()
    runRef.current = null
    setRunning(false)
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
    // REQ-19f 窗格模型覆盖候选：已启用的对话模型连接
    api.listConnections().then((cs) => setConns(cs.filter((c) => c.enabled && c.conn_type === 'chat'))).catch(() => {})
  }
  useEffect(loadCfgOptions, [])
  const [conns, setConns] = useState<ModelConnection[]>([])

  // ---- REQ-19e/19f 对话对比模式：开关 + 2~4 窗格 + 每窗格单项覆盖（''= 继承对话当前配置）----
  const cmpKey = `eino.compare.${conversation?.id}`
  const [cmp, setCmp] = useState<{ on: boolean; panes: PaneSel[] }>({ on: false, panes: [{ ...BLANK_PANE }, { ...BLANK_PANE }] })
  useEffect(() => {
    try {
      const saved = JSON.parse(localStorage.getItem(cmpKey) || 'null')
      if (saved && typeof saved.on === 'boolean' && Array.isArray(saved.panes) && saved.panes.length >= 2 && saved.panes.length <= 4) {
        setCmp({ on: saved.on, panes: saved.panes.map((p: any) => ({ agent: p?.agent ?? '', model: p?.model ?? '', kb: p?.kb ?? '', profile: p?.profile ?? '', noHistory: !!p?.noHistory, temperature: typeof p?.temperature === 'number' ? p.temperature : null, instruction: p?.instruction ?? '', skills: p?.skills ?? '' })) })
        return
      }
    } catch { /* 忽略坏数据 */ }
    setCmp({ on: false, panes: [{ ...BLANK_PANE }, { ...BLANK_PANE }] })
  }, [cmpKey])
  const patchCmp = (patch: Partial<{ on: boolean; panes: PaneSel[] }>) => {
    setCmp((prev) => {
      const next = { ...prev, ...patch }
      try { localStorage.setItem(cmpKey, JSON.stringify(next)) } catch { /* 忽略配额 */ }
      return next
    })
  }
  const setPaneSel = (i: number, field: keyof PaneSel, value: string | number | boolean | null) => {
    setCmp((prev) => {
      if (!prev.on) return prev
      const panes = prev.panes.map((p, j) => (j === i ? { ...p, [field]: value } : p))
      const next = { ...prev, panes }
      try { localStorage.setItem(cmpKey, JSON.stringify(next)) } catch { /* 忽略配额 */ }
      return next
    })
  }
  const resizePanes = (panes: PaneSel[], n: number): PaneSel[] =>
    Array.from({ length: n }, (_, i) => panes[i] ?? { ...BLANK_PANE })

  // REQ-144：配置剖面对话级命名保存（localStorage，不做全局剖面库）
  const profilesKey = `eino.compare.profiles.${conversation?.id}`
  const [cmpProfiles, setCmpProfiles] = useState<{ name: string; cfg: Partial<PaneSel> }[]>([])
  const [profileSaveFor, setProfileSaveFor] = useState<number | null>(null)
  const [profileName, setProfileName] = useState('')
  useEffect(() => {
    try {
      const list = JSON.parse(localStorage.getItem(profilesKey) || '[]')
      if (Array.isArray(list)) setCmpProfiles(list)
    } catch { /* 忽略坏数据 */ }
  }, [profilesKey])
  const persistProfiles = (list: { name: string; cfg: Partial<PaneSel> }[]) => {
    setCmpProfiles(list)
    try { localStorage.setItem(profilesKey, JSON.stringify(list)) } catch { /* 忽略配额 */ }
  }
  // 剖面仅捕获「配置型」字段（智能体/模型/库/方案/温度/提示词/技能/历史口径）
  const capturePane = (p: PaneSel): Partial<PaneSel> => ({
    agent: p.agent, model: p.model, kb: p.kb, profile: p.profile,
    temperature: p.temperature, instruction: p.instruction, skills: p.skills, noHistory: p.noHistory,
  })
  const saveProfile = (i: number) => setProfileSaveFor(i)
  const doSaveProfile = () => {
    if (profileSaveFor == null) return
    const name = profileName.trim()
    if (!name) { showToast('请输入剖面名称', 'err'); return }
    const cfg = capturePane(cmp.panes[profileSaveFor])
    const next = [...cmpProfiles.filter((x) => x.name !== name), { name, cfg }]
    persistProfiles(next)
    showToast(`剖面「${name}」已保存（对话级，可在任意窗格应用）`)
    setProfileSaveFor(null)
    setProfileName('')
  }
  const applyProfile = (i: number, cfg: Partial<PaneSel>) => {
    setCmp((prev) => {
      if (!prev.on) return prev
      const panes = prev.panes.map((p, j) => (j === i ? { ...p, ...cfg } : p))
      const next = { ...prev, panes }
      try { localStorage.setItem(cmpKey, JSON.stringify(next)) } catch { /* 忽略配额 */ }
      return next
    })
    showToast(`已应用剖面到窗格 ${i + 1}（覆盖合并序：剖面 > 窗格单项 > 智能体 > 全局）`)
  }
  // REQ-144 可选增强：采纳窗格配置转正（agent 直聊：写回 agent_id/知识库/方案/技能开关继续对话；
  // 项目会话不提供——主智能体/协调者变更属项目配置职责）
  const adoptPane = (sel: PaneSel) => {
    if (isProjectScope) return
    const kb = sel.kb || conversation.kb_id
    const rp = sel.profile || conversation.runtime_profile_id
    const patch: Partial<Conversation> = {
      agent_id: sel.agent || conversation.agent_id,
      enable_kb: sel.kb ? true : conversation.enable_kb,
      ontology_enabled: sel.profile ? true : conversation.ontology_enabled,
    }
    if (kb) patch.kb_id = kb
    if (rp) patch.runtime_profile_id = rp
    if (sel.skills) patch.enable_skills = sel.skills === 'on'
    patchConv(patch)
    showToast(`已采纳窗格配置（智能体 ${agents.find((a) => a.id === patch.agent_id)?.name ?? '—'}），关闭对比后单流沿用`)
  }

  // REQ-144：复制上一窗格配置（纯前端操作，便于单变量 A/B）
  const copyPrevPane = (i: number) => {
    setCmp((prev) => {
      if (!prev.on || i <= 0) return prev
      const panes = prev.panes.map((p, j) => (j === i ? { ...prev.panes[i - 1] } : p))
      const next = { ...prev, panes }
      try { localStorage.setItem(cmpKey, JSON.stringify(next)) } catch { /* 忽略配额 */ }
      return next
    })
    showToast(`已复制窗格 ${i} 的配置到窗格 ${i + 1}`)
  }

  // 窗格消息流（会话内累计；切会话清空；窗格数变化对齐长度）
  const [paneItems, setPaneItems] = useState<ChatItem[][]>([[], []])
  const paneRunMapRef = useRef<Record<string, number>>({})
  useEffect(() => {
    setPaneItems((prev) => prev.map(() => []))
    paneRunMapRef.current = {}
  }, [conversation?.id])
  useEffect(() => {
    setPaneItems((prev) => (prev.length === cmp.panes.length ? prev : Array.from({ length: cmp.panes.length }, (_, i) => prev[i] ?? [])))
  }, [cmp.panes.length])
  const updatePane = (i: number, updater: (prev: ChatItem[]) => ChatItem[]) => {
    setPaneItems((prev) => prev.map((p, j) => (j === i ? updater(p) : p)))
  }

  // REQ-143：窗格 Agent（所选优先，缺省继承对话所属/项目主智能体）
  const paneAgentOf = (sel: PaneSel) => (sel.agent ? agents.find((a) => a.id === sel.agent) ?? null : agent)
  // Agent 候选：项目会话 = 项目成员（标记）+ 全局其余；Agent 会话 = 全部
  const agentOptions = useMemo(() => {
    const memberIds = new Set(isProjectScope ? project?.agent_ids ?? [] : [])
    return agents.map((a) => ({
      value: a.id,
      label: isProjectScope ? `${a.name}${memberIds.has(a.id) ? '（项目成员）' : '（全局）'}` : a.name,
    }))
  }, [agents, isProjectScope, project])

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
      withSubDepth(items.filter((it) => !(it.evType && levelGated(it.evType, debugLevel)))).map((it, i) => {
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
    [items, showRaw, reasoningOpen, granularity, showReasoning, debugLevel],
  )

  // REQ-19e 窗格消息流条目（与单路 listItems 同构映射：消息走角色、事件卡无边框）
  const paneListItems = (i: number) =>
    withSubDepth(paneItems[i] ?? []).map((it, j) => {
      if (it.kind === 'msg') {
        return {
          key: `p${i}m${j}`,
          role: it.role === 'user' ? 'user' : 'ai',
          content: it.content ?? '',
          loading: !!it.streaming && !it.content,
          extraInfo: { streaming: !!it.streaming },
        }
      }
      return {
        key: `p${i}e${j}`,
        role: 'event',
        content: renderEventCard(it, j),
      }
    })

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

  // 运行/恢复共用的事件翻译（send 与 resume 的 SSE 处理一致；归约见模块级 applyRunEvent）
  const handleRunEvent = ({ event, data }: { event: string; data: any }) => {
    const payload = data?.data ?? {}
    if (event === 'run.interrupted') setInterrupt(toInterruptState(payload))
    setItems((prev) => applyRunEvent(prev, event, payload, runKeyRef.current))
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
    // REQ-19e：对比模式走多窗格并行（一次提问 N 路，停止整组）
    if (cmp.on && cmp.panes.length >= 2) {
      await sendCompare(text)
      return
    }
    runKeyRef.current = `run-${Date.now()}`
    setItems((prev) => [...prev, { kind: 'msg', role: 'user', content: text }])
    await streamStart((handler) => runConversation(conversation.id, text, debugLevel, handler, undefined, debugPersist))
  }

  // REQ-19e/19f 对比发送：共用输入框一次提问 → N 路并行；meta 建窗格 run_id 映射，事件按 run_id 路由
  const sendCompare = async (text: string) => {
    const panesPayload: ComparePaneConfig[] = cmp.panes.map((p) => ({
      ...(p.agent ? { agent_id: p.agent } : {}),
      ...(p.model ? { model_conn_id: p.model } : {}),
      ...(p.kb ? { kb_id: p.kb } : {}),
      ...(p.profile ? { runtime_profile_id: p.profile } : {}),
      ...(p.temperature != null ? { temperature: p.temperature } : {}),
      ...(p.instruction ? { instruction: p.instruction } : {}),
      ...(p.skills ? { enable_skills: p.skills === 'on' } : {}),
      ...(p.noHistory ? { no_history: true } : {}),
    }))
    paneRunMapRef.current = {}
    setPaneItems((prev) =>
      prev.map(() => [
        { kind: 'msg' as const, role: 'user' as const, content: text },
        { kind: 'msg' as const, role: 'assistant' as const, content: '', streaming: true },
      ]),
    )
    setRunning(true)
    const aborter = runConversation(conversation.id, text, debugLevel, handleCompareEvent, panesPayload, debugPersist)
    runRef.current = aborter
    try {
      await aborter.done
    } catch { /* 用户中断 */ }
    setPaneItems((prev) => prev.map((p) => p.map((it) => (it.streamKey ? { ...it, streamKey: undefined } : it))))
    setRunning(false)
    runRef.current = null
    bumpData()
    onConversationUpdated()
  }

  // 对比 SSE 路由：meta 建映射 → 事件按 data.run_id 落到对应窗格；无映射的组级事件（守卫错误）广播全部窗格
  const handleCompareEvent = ({ event, data }: { event: string; data: any }) => {
    if (event === 'meta') {
      if (Array.isArray(data?.panes)) {
        const m: Record<string, number> = {}
        for (const p of data.panes) {
          if (p?.run_id != null && typeof p.pane === 'number') m[String(p.run_id)] = p.pane
        }
        paneRunMapRef.current = m
      }
      return
    }
    const payload = data?.data ?? {}
    const idx = paneRunMapRef.current[String(data?.run_id ?? '')]
    if (event === 'run.interrupted' && idx !== undefined) setInterrupt(toInterruptState(payload))
    if (idx === undefined) {
      setPaneItems((prev) => prev.map((p, i) => applyRunEvent(p, event, payload, `pane-${i}`)))
      return
    }
    updatePane(idx, (prev) => applyRunEvent(prev, event, payload, `pane-${idx}`))
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
    await streamStart((handler) => resumeConversation(conversation.id, text, debugLevel, handler, debugPersist))
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
          {/* REQ-19e 对比模式：开关 + 2~4 列（运行中锁定；开关与窗格配置按会话记忆） */}
          <Tooltip title="对比模式：一次提问多窗格并行，窗格可分别覆盖模型/知识库/运行方案——学习配置差异对同一问题的影响（SC-10）">
            <span className="cmp-switch" aria-label="对话对比模式开关">
              <Switch size="small" checked={cmp.on} disabled={running || !conversation.id} onChange={(v) => patchCmp({ on: v })} />
              <span className="cmp-switch-label">对比</span>
            </span>
          </Tooltip>
          {cmp.on && (
            <Segmented
              size="small"
              value={String(cmp.panes.length)}
              disabled={running}
              onChange={(v) => patchCmp({ panes: resizePanes(cmp.panes, Number(v)) })}
              options={[
                { value: '2', label: '2 列' },
                { value: '3', label: '3 列' },
                { value: '4', label: '4 列' },
              ]}
            />
          )}
          {/* REQ-135②：对话级过程展示面板（默认收起）——粒度/深度思考/原始 JSON/审批覆盖 */}
          <Popover
            trigger={"click"}
            placement="bottomRight"
            content={
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10, width: 250 }}>
                <div>
                  <Typography.Text style={{ fontSize: 12 }}>过程事件粒度</Typography.Text>
                  <div style={{ marginTop: 4 }}>
                    <Segmented
                      size="small"
                      value={granularity}
                      onChange={(v) => patchConvCfg({ granularity: v as 'all' | 'key' | 'off' })}
                      options={[
                        { value: 'all', label: '全部' },
                        { value: 'key', label: '关键' },
                        { value: 'off', label: '精简' },
                      ]}
                    />
                  </div>
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    关键=工具调用与运行状态；精简=仅消息与结论
                  </Typography.Text>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <Typography.Text style={{ fontSize: 12 }}>深度思考过程</Typography.Text>
                  <Switch size="small" checked={showReasoning} onChange={(v) => patchConvCfg({ showReasoning: v })} />
                </div>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <Typography.Text style={{ fontSize: 12 }}>原始事件 JSON</Typography.Text>
                  <Switch size="small" checked={showRaw} onChange={setShowRaw} />
                </div>
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                    <Typography.Text style={{ fontSize: 12 }}>调试事件入库</Typography.Text>
                    <Switch size="small" checked={debugPersist} onChange={(v) => patchConvCfg({ debugPersist: v })} />
                  </div>
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    开启后调试档细节（模型调用链路/装配快照）随运行落库，供历史与回放查看（级别≥详细时产生）
                  </Typography.Text>
                  {debugLevel >= 1 && !debugPersist && (
                    <Alert
                      type="warning"
                      showIcon
                      style={{ marginTop: 6 }}
                      message="当前级别≥详细，但入库关闭：调试细节不会留存到历史（REQ-149）"
                    />
                  )}
                </div>
                <div>
                  <Typography.Text style={{ fontSize: 12 }}>工具调用审批</Typography.Text>
                  <Select
                    size="small"
                    style={{ width: '100%', marginTop: 4 }}
                    value={conversation.tool_approval === 'on' || conversation.tool_approval === 'off' ? conversation.tool_approval : ''}
                    onChange={(v) => patchConv({ tool_approval: v as string })}
                    options={[
                      { value: '', label: '跟随智能体配置' },
                      { value: 'on', label: '本对话强制开启审批' },
                      { value: 'off', label: '本对话关闭审批' },
                    ]}
                  />
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                    合并顺序：对话级 &gt; 智能体级（REQ-135②）
                  </Typography.Text>
                </div>
              </div>
            }
          >
            <Button size="small">过程展示</Button>
          </Popover>
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
          {/* REQ-135①：配置入口并入右侧边栏收放按钮（原独立「配置」按钮移除） */}
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
        {cmp.on ? (
          <>
            {/* 对比开启前/外的对话历史（共享，折叠收纳；对比轮次的各窗格回答也会落库进此历史） */}
            {items.length > 0 && (
              <Collapse
                ghost
                size="small"
                className="cmp-history"
                items={[{
                  key: 'h',
                  label: <Typography.Text type="secondary" style={{ fontSize: 12 }}>对比外的对话历史（{items.length} 条，收起以聚焦本轮对照）</Typography.Text>,
                  children: <Bubble.List items={listItems} role={BUBBLE_ROLES} />,
                }]}
              />
            )}
            <div className="cmp-grid" style={{ '--n': cmp.panes.length } as CSSProperties}>
              {cmp.panes.map((sel, i) => (
                <section key={i} className="cmp-pane" aria-label={`对比窗格 ${i + 1}`}>
                  <header className="cmp-pane-head">
                    <span className="cmp-pane-title">窗格 {i + 1}</span>
                    <span className="cmp-pane-agent" title={sel.agent ? '窗格级智能体（REQ-143）' : '继承对话智能体'}>
                      <AgentLogo agent={paneAgentOf(sel)} size={16} />
                      <Typography.Text strong style={{ fontSize: 12 }}>{paneAgentOf(sel)?.name ?? '—'}</Typography.Text>
                    </span>
                    {/* REQ-144 可选增强：采纳该窗格配置写回对话（仅 agent 直聊；运行中禁用） */}
                    {!isProjectScope && conversation.agent_id && !running && (
                      <Tooltip title="采纳：把该窗格的智能体/知识库/方案/技能开关写回对话配置，之后单流对话沿用">
                        <Button
                          type="link" size="small" style={{ padding: '0 4px', fontSize: 11 }}
                          onClick={() => adoptPane(sel)}
                        >
                          采纳
                        </Button>
                      </Tooltip>
                    )}
                    <span className="cmp-pane-badges">
                      <span className={`cmp-badge${sel.model ? ' set' : ''}`} title={sel.model ? `模型覆盖：${conns.find((c) => c.id === sel.model)?.name ?? sel.model}` : '模型：继承对话/智能体配置'}>
                        模型{sel.model ? `·${conns.find((c) => c.id === sel.model)?.name ?? ''}` : '·继承'}
                      </span>
                      <span className={`cmp-badge${sel.kb ? ' set' : ''}`} title={sel.kb ? `知识库覆盖：${kbs.find((k) => k.id === sel.kb)?.name ?? sel.kb}` : '知识库：继承对话配置'}>
                        库{sel.kb ? `·${kbs.find((k) => k.id === sel.kb)?.name ?? ''}` : '·继承'}
                      </span>
                      <span className={`cmp-badge${sel.profile ? ' set' : ''}`} title={sel.profile ? `运行方案覆盖：${profiles.find((p) => p.id === sel.profile)?.name ?? sel.profile}` : '本体运行方案：继承对话配置'}>
                        方案{sel.profile ? `·${profiles.find((p) => p.id === sel.profile)?.name ?? ''}` : '·继承'}
                      </span>
                    </span>
                  </header>
                  <div className="cmp-pane-stream">
                    {(paneItems[i] ?? []).length > 0 ? (
                      <Bubble.List items={paneListItems(i)} role={BUBBLE_ROLES} />
                    ) : (
                      <div className="cmp-pane-empty">独立生成 · 等待提问</div>
                    )}
                  </div>
                  {/* REQ-19f 窗格独立配置区：未设置项继承对话当前配置（Q-6 不追溯，仅影响该窗格后续消息） */}
                  <footer className="cmp-pane-cfg">
                    <Select
                      size="small" allowClear disabled={running} showSearch
                      optionFilterProp="label"
                      placeholder="智能体 · 继承" value={sel.agent || undefined}
                      onChange={(v) => setPaneSel(i, 'agent', v ?? '')}
                      options={agentOptions}
                    />
                    <Select
                      size="small" allowClear disabled={running}
                      placeholder="模型 · 继承" value={sel.model || undefined}
                      onChange={(v) => setPaneSel(i, 'model', v ?? '')}
                      options={conns.map((c) => ({ value: c.id, label: connDisplayName(c) }))}
                    />
                    <Select
                      size="small" allowClear disabled={running}
                      placeholder="知识库 · 继承" value={sel.kb || undefined}
                      onChange={(v) => setPaneSel(i, 'kb', v ?? '')}
                      options={kbs.map((k) => ({ value: k.id, label: k.name }))}
                    />
                    <Select
                      size="small" allowClear disabled={running}
                      placeholder="本体方案 · 继承" value={sel.profile || undefined}
                      onChange={(v) => setPaneSel(i, 'profile', v ?? '')}
                      options={profiles.filter((p) => p.status === 'running').map((p) => ({ value: p.id, label: p.name }))}
                    />
                    <Checkbox
                      checked={sel.noHistory}
                      disabled={running}
                      onChange={(e) => setPaneSel(i, 'noHistory', e.target.checked)}
                      style={{ fontSize: 11 }}
                    >
                      不携带历史（干净对照，REQ-143）
                    </Checkbox>
                    {/* REQ-144：更多配置（推理参数/提示词改写/技能开关）+ 剖面与窗格间复制 */}
                    <Collapse
                      ghost
                      size="small"
                      className="cmp-more-cfg"
                      items={[{
                        key: 'more',
                        label: <span style={{ fontSize: 11, color: 'var(--ant-color-text-tertiary, #999)' }}>更多配置（温度 / 提示词改写 / 技能 · 剖面）</span>,
                        children: (
                          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                            <div style={{ display: 'flex', gap: 6 }}>
                              <InputNumber
                                size="small" style={{ flex: 1 }} min={0} max={2} step={0.1} disabled={running}
                                placeholder="温度 · 继承" value={sel.temperature ?? undefined}
                                onChange={(v) => setPaneSel(i, 'temperature', typeof v === 'number' ? v : null)}
                              />
                              <Select
                                size="small" style={{ flex: 1 }} disabled={running}
                                placeholder="技能 · 继承" value={sel.skills || undefined}
                                onChange={(v) => setPaneSel(i, 'skills', v ?? '')}
                                options={[{ value: 'on', label: '技能开' }, { value: 'off', label: '技能关' }]}
                              />
                            </div>
                            <Input.TextArea
                              size="small" rows={2} disabled={running} maxLength={2000}
                              placeholder="系统提示词临时改写 · 继承（仅本窗格生效）"
                              value={sel.instruction || undefined}
                              onChange={(e) => setPaneSel(i, 'instruction', e.target.value)}
                            />
                            <Space size={4} wrap>
                              <Select
                                size="small" style={{ minWidth: 130 }} allowClear disabled={running}
                                placeholder="应用剖面…" value={undefined}
                                onChange={(name) => { const pr = cmpProfiles.find((x) => x.name === name); if (pr) applyProfile(i, pr.cfg) }}
                                options={cmpProfiles.map((x) => ({ value: x.name, label: x.name }))}
                              />
                              <Button size="small" disabled={running} onClick={() => saveProfile(i)}>存为剖面</Button>
                              {i > 0 && (
                                <Button size="small" disabled={running} onClick={() => copyPrevPane(i)}>
                                  复制上一窗格
                                </Button>
                              )}
                            </Space>
                          </div>
                        ),
                      }]}
                    />
                  </footer>
                </section>
              ))}
            </div>
          </>
        ) : items.length === 0 ? (
          <div className="msg-empty">
            <Welcome
              variant="borderless"
              icon={<span className="agent-tile"><AgentLogo agent={agent} size={30} /></span>}
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

      {/* REQ-144：剖面命名保存（对话级） */}
      <Modal
        open={profileSaveFor != null}
        centered
        title={`保存窗格 ${(profileSaveFor ?? 0) + 1} 配置为剖面`}
        width={420}
        onCancel={() => { setProfileSaveFor(null); setProfileName('') }}
        onOk={doSaveProfile}
        okText="保存"
        cancelText="取消"
      >
        <Input
          value={profileName}
          onChange={(e) => setProfileName(e.target.value)}
          placeholder="剖面名称（对话级，如：低温度-严格事实）"
          onPressEnter={doSaveProfile}
          autoFocus
        />
        <Typography.Text type="secondary" style={{ fontSize: 11, display: 'block', marginTop: 8 }}>
          剖面捕获：智能体/模型/知识库/方案/温度/提示词改写/技能/历史口径。应用时按「剖面 &gt; 窗格单项 &gt; 智能体 &gt; 全局」合并。
        </Typography.Text>
      </Modal>

      {replayOpen && (
        <EventReplayDrawer
          conversationId={conversation.id}
          title={conversation.title || subjectName || '对话'}
          level={debugLevel}
          open={replayOpen}
          onClose={() => setReplayOpen(false)}
        />
      )}
    </div>
  )
}
