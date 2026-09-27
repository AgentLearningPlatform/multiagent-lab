/**
 * REQ-174 对话执行过程展示优化（ZCode 式单行/收起）：
 * - groupToolPhases：把连续的 tool.call / tool.result 事件流归组为「执行过程」块，
 *   call/result 合并为一次调用的单行（状态点 + 工具名 + 参数摘要 + 耗时）；
 * - ToolPhaseBlock：渲染组——流式进行中（live）默认展开逐行；本轮结束后收起为
 *   一行摘要（N 次工具调用 · 总耗时），点击展开回看；历史回放默认收起。
 * 单行渲染替代原「调用工具卡 + 工具结果卡」两卡各带「详情」折叠的四行形态。
 */
import { useState } from 'react'
import './tool-phase.css'

/** 一次工具调用（call/result 合并后） */
export interface ToolCallRow {
  name: string
  source?: string
  args?: string
  argsSummary: string
  result?: string
  durationMs?: number
  running: boolean // 无 result：流式中=运行中；历史中=被中断未回结果
  err: boolean
}

/** 归组后的执行过程块（挂到 items 流中替代原 N 张事件卡） */
export interface ToolPhaseGroup {
  kind: '__tool_phase'
  calls: ToolCallRow[]
  totalMs: number
  live: boolean // 流式进行中（展开）；结束后 false（收起）
  subDepth: number
}

/** 来源分类（与 ChatWindow.eventSource 同口径的最小副本——避免环导入） */
function srcClassOf(source?: string, name = ''): string {
  const s = source ?? ''
  if (s.startsWith('skill:')) return 'skill'
  if (s.startsWith('mcp:')) return 'mcp'
  if (s.startsWith('ontology:')) return 'onto'
  if (s.startsWith('agent:')) return 'subagent'
  if (s === 'builtin') return 'builtin'
  if (name.startsWith('onto_')) return 'onto'
  if (name.startsWith('mcp_') || name.includes('__')) return 'mcp'
  return 'builtin'
}

/** 参数摘要：对象取前几组 k=v，标量直接截断 */
function summarizeArgs(args: unknown): string {
  if (args == null || args === '') return ''
  let obj: unknown = args
  if (typeof args === 'string') {
    try {
      obj = JSON.parse(args)
    } catch {
      return truncate(String(args), 64)
    }
  }
  if (obj && typeof obj === 'object' && !Array.isArray(obj)) {
    const parts: string[] = []
    for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
      parts.push(`${k}=${typeof v === 'string' ? truncate(v, 24) : truncate(JSON.stringify(v), 24)}`)
      if (parts.length >= 3) break
    }
    return truncate(parts.join(' '), 72)
  }
  return truncate(JSON.stringify(obj), 64)
}

function truncate(s: string, n: number): string {
  const t = String(s).replace(/\s+/g, ' ').trim()
  return t.length > n ? t.slice(0, n) + '…' : t
}

const isToolEv = (t?: string) => t === 'tool.call' || t === 'tool.result'

/** 归组：连续 tool.call/result 合并为 ToolPhaseGroup；其余事件照原样透传（打断分组） */
export function groupToolPhases(items: any[]): any[] {
  const out: any[] = []
  let buf: any[] = []
  for (const it of items) {
    if (it.kind === 'event' && isToolEv(it.evType)) buf.push(it)
    else {
      if (buf.length) out.push(buildGroup(buf, items))
      buf = []
      out.push(it)
    }
  }
  if (buf.length) out.push(buildGroup(buf, items))
  return out
}

function buildGroup(buf: any[], all: any[]): ToolPhaseGroup {
  const calls: ToolCallRow[] = []
  for (const it of buf) {
    const d = it.evData ?? {}
    if (it.evType === 'tool.call') {
      calls.push({
        name: String(d.tool_name ?? ''),
        source: d.source,
        args: typeof d.arguments === 'string' ? d.arguments : d.arguments != null ? JSON.stringify(d.arguments) : undefined,
        argsSummary: summarizeArgs(d.arguments),
        running: true,
        err: false,
      })
    } else {
      // tool.result：回填最后一个同名未回结果调用；无匹配时独立成行
      const hit = [...calls].reverse().find((c) => c.name === String(d.tool_name ?? '') && c.running)
      const content = typeof d.content === 'string' ? d.content : d.content != null ? JSON.stringify(d.content) : ''
      const err = /^Error|^错误|执行失败|QUERY_REJECTED|invalid/i.test(content)
      if (hit) {
        hit.result = content
        hit.durationMs = typeof d.duration_ms === 'number' ? d.duration_ms : undefined
        hit.running = false
        hit.err = err
      } else {
        calls.push({
          name: String(d.tool_name ?? ''),
          source: d.source,
          argsSummary: '',
          result: content,
          durationMs: typeof d.duration_ms === 'number' ? d.duration_ms : undefined,
          running: false,
          err,
        })
      }
    }
  }
  // live：本组之后只有流式中的助手消息（本轮仍在执行）；其余组一律视为已结束（收起）
  const lastBuf = buf[buf.length - 1]
  const groupEndIdx = all.lastIndexOf(lastBuf)
  const trailing = all.slice(groupEndIdx + 1)
  const live = trailing.length >= 1 && trailing.every((m) => m.kind === 'msg' && m.streaming)
  return {
    kind: '__tool_phase',
    calls,
    totalMs: calls.reduce((s, c) => s + (c.durationMs ?? 0), 0),
    live,
    subDepth: buf[0]?.subDepth ?? 0,
  }
}

const fmtMs = (ms?: number) => (ms == null ? '' : ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`)

/** 单次调用行：点击展开原始入参/结果 */
function ToolLine({ call, defaultOpen }: { call: ToolCallRow & { pending?: boolean }; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(!!defaultOpen)
  const cls = srcClassOf(call.source, call.name)
  const dotCls = call.running ? 'run' : call.pending ? 'pend' : call.err ? 'bad' : 'ok'
  const dotTxt = call.running ? '' : call.pending ? '–' : call.err ? '✕' : '✓'
  return (
    <div className={`tool-line src-${cls}${call.err ? ' err' : ''}`}>
      <div className="tool-line-head" onClick={() => setOpen((o) => !o)} title={open ? '收起详情' : '展开入参与结果'}>
        <span className={`tool-dot ${dotCls}`}>{dotTxt}</span>
        <span className="tool-name">{call.name}</span>
        {call.argsSummary && <span className="tool-args">{call.argsSummary}</span>}
        {call.durationMs != null && <span className="tool-dur">{fmtMs(call.durationMs)}</span>}
        <span className={`tool-caret${open ? ' open' : ''}`} />
      </div>
      {open && (
        <pre className="tool-raw">
          {call.args ? `入参：${prettyJson(call.args)}\n` : ''}
          {call.result ? `结果：${prettyJson(call.result)}` : call.running ? '执行中…' : '（无结果）'}
        </pre>
      )}
    </div>
  )
}

function prettyJson(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2)
  } catch {
    return s
  }
}

/** 执行过程块：live 展开逐行；结束收起为摘要行（可展开） */
export function ToolPhaseBlock({ group }: { group: ToolPhaseGroup }) {
  const [open, setOpen] = useState(false)
  const okCount = group.calls.filter((c) => !c.err).length
  const running = group.calls.some((c) => c.running)
  if (group.live) {
    return (
      <div className={`tool-phase${running ? ' live' : ''}`} style={{ marginLeft: group.subDepth * 14 }}>
        {group.calls.map((c, i) => (
          <ToolLine key={i} call={c} />
        ))}
      </div>
    )
  }
  return (
    <div className="tool-phase" style={{ marginLeft: group.subDepth * 14 }}>
      {!open && (
        <div className="tool-phase-head" onClick={() => setOpen(true)} title="展开每次调用的详情">
          <span className="tool-caret" />
          <span className="tool-phase-title">
            执行过程 · {group.calls.length} 次工具调用{group.totalMs > 0 ? ` · ${fmtMs(group.totalMs)}` : ''}
            {okCount < group.calls.length ? ` · ${group.calls.length - okCount} 失败` : ''}
          </span>
        </div>
      )}
      {open && (
        <>
          <div className="tool-phase-head open" onClick={() => setOpen(false)} title="收起">
            <span className="tool-caret open" />
            <span className="tool-phase-title">执行过程 · {group.calls.length} 次工具调用{group.totalMs > 0 ? ` · ${fmtMs(group.totalMs)}` : ''}</span>
          </div>
          {group.calls.map((c, i) => (
            <ToolLine key={i} call={c.running ? { ...c, running: false, pending: true } : c} />
          ))}
        </>
      )}
    </div>
  )
}
