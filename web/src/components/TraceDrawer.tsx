import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Drawer, Input, Segmented, Select, Space, Switch, Tag, Tooltip, Typography } from 'antd'
import { ReloadOutlined, RollbackOutlined } from '@ant-design/icons'
import { api } from '../api/client'
import type { RunEventDTO } from '../api/types'
import { describeEvent, levelGated } from './ChatWindow'
import EventReplayDrawer from './EventReplayDrawer'

/**
 * 调用轨迹面板（REQ-217①②④/M48）：对话运行按 run 分组的时间线 + 过滤 + 显示设置收编。
 * - 行类型：工具调用/模型步骤（仅 debug_persist 落库时可见，缺失诚实标注）/run.warning/
 *   审批挂起（run.interrupted）/subagent（缩进）/retrieval/artifact.saved/debug.cli/运行始末；
 * - 过滤：类型多选 / 运行选择 / 关键字 / 全部展开收起；
 * - 显示设置收编（④定案）：头部「过程展示」Popover（粒度/深度思考/原始 JSON）、调试三档、
 *   调试事件入库与会话级审批覆盖自头部迁入（行为配置归会话配置面；agent 级审批开关在侧板
 *   Harness 页签 REQ-219，会话级覆盖优先级不变）；
 * - 「重放此运行」（②定案）：EventReplayDrawer 并入为面板内动作，头部重放按钮退役。
 */

const TYPE_GROUPS = [
  { value: 'tool', label: '工具调用', types: ['tool.call', 'tool.result'] },
  { value: 'model', label: '模型步骤', types: ['model.step'] },
  { value: 'warning', label: '警告', types: ['run.warning'] },
  { value: 'approval', label: '审批挂起', types: ['run.interrupted'] },
  // REQ-224/M52（51 号 W1）：harness 事件域（结构化审计）
  { value: 'harness', label: 'Harness 审计', types: ['approval.granted', 'approval.denied', 'hook.denied', 'verify.completed', 'verify.failed', 'connector.degraded'] },
  { value: 'subagent', label: '子智能体', types: ['subagent.enter', 'subagent.exit'] },
  { value: 'retrieval', label: '知识召回', types: ['retrieval'] },
  { value: 'artifact', label: '产物', types: ['artifact.saved'] },
  { value: 'cli', label: 'CLI 输出', types: ['debug.cli'] },
  { value: 'run', label: '运行始末', types: ['run.started', 'run.finished', 'run.error'] },
]
const typesOfGroups = (gs: string[]) => {
  const set = new Set<string>()
  for (const g of gs) for (const t of TYPE_GROUPS.find((x) => x.value === g)?.types ?? []) set.add(t)
  return set
}

const TYPE_COLOR: Record<string, string> = {
  'tool.call': 'blue', 'tool.result': 'geekblue', 'model.step': 'purple', 'run.warning': 'orange',
  'run.interrupted': 'gold', 'subagent.enter': 'cyan', 'subagent.exit': 'cyan', 'retrieval': 'green',
  'artifact.saved': 'magenta', 'debug.cli': 'default', 'run.started': 'default', 'run.finished': 'green',
  'run.error': 'red', 'reasoning': 'default', 'skill.loaded': 'default',
}

export default function TraceDrawer({
  conversationId,
  title,
  open,
  onClose,
  // 显示设置（收编自头部，状态单源仍在 ChatWindow——同时治理会话流事件卡展示）
  granularity,
  showReasoning,
  showRaw,
  debugLevel,
  debugPersist,
  toolApproval,
  running,
  patch,
  onShowRaw,
  onChangeDebugLevel,
  onPatchConv,
}: {
  conversationId: string
  title: string
  open: boolean
  onClose: () => void
  granularity: 'all' | 'key' | 'off'
  showReasoning: boolean
  showRaw: boolean
  debugLevel: number
  debugPersist: boolean
  toolApproval: string
  running: boolean
  patch: (p: { granularity?: 'all' | 'key' | 'off'; showReasoning?: boolean; debugPersist?: boolean }) => void
  onShowRaw: (v: boolean) => void
  onChangeDebugLevel: (n: number) => void
  onPatchConv: (p: { tool_approval: string }) => void
}) {
  const [events, setEvents] = useState<RunEventDTO[]>([])
  const [loading, setLoading] = useState(false)
  const [typeGroups, setTypeGroups] = useState<string[]>([])
  const [runFilter, setRunFilter] = useState<string>('')
  const [keyword, setKeyword] = useState('')
  const [expandAll, setExpandAll] = useState(false)
  const [replayRun, setReplayRun] = useState<string | null>(null)

  const load = () => {
    if (!conversationId) return
    setLoading(true)
    api
      .listEvents(conversationId)
      .then((r) => setEvents(r ?? []))
      .catch(() => setEvents([]))
      .finally(() => setLoading(false))
  }
  useEffect(() => {
    if (open) load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, conversationId])

  // 按运行分组（created_at 升序；run_id 空的散事件归「未分组」段）
  const runs = useMemo(() => {
    const map = new Map<string, RunEventDTO[]>()
    for (const e of events) {
      const k = e.run_id || '__none__'
      if (!map.has(k)) map.set(k, [])
      map.get(k)!.push(e)
    }
    return [...map.entries()].map(([runId, evs]) => ({
      runId,
      events: evs,
      startedAt: evs[0]?.created_at ?? '',
      finished: evs.find((e) => e.type === 'run.finished' || e.type === 'run.error'),
    }))
  }, [events])

  const hasDebugRecorded = events.some((e) => e.type === 'model.step')

  // 过滤链：运行 → 类型组 → 关键字（匹配描述文案或原始 data）→ 展示级别门控（与回放同口径）
  const filteredRuns = useMemo(() => {
    const kw = keyword.trim().toLowerCase()
    const allow = typeGroups.length > 0 ? typesOfGroups(typeGroups) : null
    return runs
      .filter((r) => !runFilter || r.runId === runFilter)
      .map((r) => ({
        ...r,
        events: r.events.filter((e) => {
          if (allow && !allow.has(e.type)) return false
          if (levelGated(e.type, debugLevel)) return false
          if (!kw) return true
          const desc = describeEvent(e.type, safeParse(e.data))
          return desc.text.toLowerCase().includes(kw) || (e.data ?? '').toLowerCase().includes(kw)
        }),
      }))
      .filter((r) => r.events.length > 0 || (!runFilter && r.runId !== '__none__' && r.events.length === 0 && false))
  }, [runs, runFilter, typeGroups, keyword, debugLevel])

  return (
    <Drawer
      title={`调用轨迹 · ${title}`}
      placement="right"
      width={560}
      open={open}
      onClose={onClose}
      destroyOnClose
      extra={
        <Space size={6}>
          <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={load}>刷新</Button>
        </Space>
      }
    >
      {/* 显示设置（REQ-217④ 收编自头部） */}
      <Typography.Text strong style={{ fontSize: 12 }}>显示设置（同时作用于会话流过程卡）</Typography.Text>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8, margin: '6px 0 12px' }}>
        <div>
          <div style={{ marginBottom: 4 }}><Typography.Text style={{ fontSize: 12 }}>过程事件粒度</Typography.Text></div>
          <Segmented
            size="small"
            value={granularity}
            onChange={(v) => patch({ granularity: v as 'all' | 'key' | 'off' })}
            options={[{ value: 'all', label: '全部' }, { value: 'key', label: '关键' }, { value: 'off', label: '精简' }]}
          />
          <Typography.Text type="secondary" style={{ fontSize: 11, display: 'block' }}>
            关键=工具调用与运行状态；精简=仅消息与结论
          </Typography.Text>
        </div>
        <Space size={24} wrap>
          <span>
            <Typography.Text style={{ fontSize: 12 }}>深度思考过程 </Typography.Text>
            <Switch size="small" checked={showReasoning} onChange={(v) => patch({ showReasoning: v })} />
          </span>
          <span>
            <Typography.Text style={{ fontSize: 12 }}>原始事件 JSON </Typography.Text>
            <Switch size="small" checked={showRaw} onChange={onShowRaw} />
          </span>
        </Space>
      </div>

      {/* 运行治理（行为配置归会话配置面） */}
      <Typography.Text strong style={{ fontSize: 12 }}>运行治理</Typography.Text>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8, margin: '6px 0 12px' }}>
        <div>
          <Typography.Text style={{ fontSize: 12 }}>观测级别（仅影响之后的运行）</Typography.Text>
          <Select
            size="small"
            style={{ width: '100%', marginTop: 4 }}
            value={String(debugLevel)}
            disabled={running}
            onChange={(v) => onChangeDebugLevel(Number(v))}
            options={[
              { value: '0', label: '简洁（默认）' },
              { value: '1', label: '详细 · 装配快照/分步用量/工具耗时' },
              { value: '2', label: '调试 · 另附模型输入全文/工具 schema' },
            ]}
          />
        </div>
        <span>
          <Typography.Text style={{ fontSize: 12 }}>调试事件入库 </Typography.Text>
          <Switch size="small" checked={debugPersist} onChange={(v) => patch({ debugPersist: v })} />
          <Typography.Text type="secondary" style={{ fontSize: 11, display: 'block' }}>
            开启后调试档细节随运行落库，供轨迹面板与回放查看（级别≥详细时产生）
          </Typography.Text>
          {debugLevel >= 1 && !debugPersist && (
            <Alert type="warning" showIcon style={{ marginTop: 6 }} message="当前级别≥详细，但入库关闭：调试细节不会留存到历史（REQ-149）" />
          )}
        </span>
        <div>
          <Typography.Text style={{ fontSize: 12 }}>工具调用审批（会话级覆盖）</Typography.Text>
          <Select
            size="small"
            style={{ width: '100%', marginTop: 4 }}
            value={toolApproval}
            onChange={(v) => onPatchConv({ tool_approval: v })}
            options={[
              { value: '', label: '跟随智能体配置（侧板 Harness 页签）' },
              { value: 'on', label: '本对话强制开启审批' },
              { value: 'off', label: '本对话关闭审批' },
            ]}
          />
        </div>
        {!hasDebugRecorded && (
          <Alert type="info" showIcon message="历史运行未见模型步骤事件（未开调试事件入库）——该类行不可用为诚实标注，非缺陷" />
        )}
      </div>

      {/* 过滤 */}
      <Typography.Text strong style={{ fontSize: 12 }}>过滤</Typography.Text>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6, margin: '6px 0 10px' }}>
        <Select
          size="small"
          mode="multiple"
          allowClear
          placeholder="类型多选（空=全部）"
          value={typeGroups}
          onChange={setTypeGroups}
          options={TYPE_GROUPS.map((g) => ({ value: g.value, label: g.label }))}
        />
        {/* REQ-224/M52（51 号 W1）：harness 视角预置过滤器——治理面一键聚焦 */}
        <Space size={4} wrap style={{ marginBottom: 4 }}>
          <a
            style={{ fontSize: 11 }}
            onClick={() => setTypeGroups(['harness', 'approval', 'warning'])}
          >
            harness 视角
          </a>
          <a style={{ fontSize: 11 }} onClick={() => setTypeGroups([])}>全部</a>
        </Space>
        <Space size={6} wrap>
          <Select
            size="small"
            showSearch
            allowClear
            placeholder="运行选择（全部）"
            style={{ width: 220 }}
            value={runFilter || undefined}
            onChange={(v) => setRunFilter(v || '')}
            options={runs.filter((r) => r.runId !== '__none__').map((r, i) => ({
              value: r.runId,
              label: `运行 ${i + 1} · ${fmtTime(r.startedAt)}（${r.events.length} 事件）`,
            }))}
          />
          <Input size="small" placeholder="关键字过滤" style={{ width: 150 }} value={keyword} onChange={(e) => setKeyword(e.target.value)} allowClear />
          <Button size="small" onClick={() => setExpandAll(true)}>全部展开</Button>
          <Button size="small" onClick={() => setExpandAll(false)}>收起</Button>
        </Space>
      </div>

      {/* 时间线：按运行分组 */}
      {filteredRuns.length === 0 && <Typography.Text type="secondary" style={{ fontSize: 12 }}>暂无事件（运行一轮后刷新，或调整过滤条件）</Typography.Text>}
      {filteredRuns.map((r, ri) => (
        <RunBlock key={r.runId} run={r} index={ri} showRaw={showRaw} expandAll={expandAll}
          onReplay={() => setReplayRun(r.runId)} />
      ))}

      {replayRun !== null && (
        <EventReplayDrawer
          conversationId={conversationId}
          title={title}
          level={debugLevel}
          runId={replayRun === '__none__' ? undefined : replayRun}
          open
          onClose={() => setReplayRun(null)}
        />
      )}
    </Drawer>
  )
}

function fmtTime(ts: string) {
  if (!ts) return '—'
  const d = new Date(ts)
  return isNaN(+d) ? ts : d.toLocaleString()
}

function safeParse(s?: string): any {
  if (!s) return undefined
  try { return JSON.parse(s) } catch { return undefined }
}

function RunBlock({
  run, index, showRaw, expandAll, onReplay,
}: {
  run: { runId: string; events: RunEventDTO[]; startedAt: string; finished?: RunEventDTO }
  index: number
  showRaw: boolean
  expandAll: boolean
  onReplay: () => void
}) {
  const [openId, setOpenId] = useState<Record<string, boolean>>({})
  useEffect(() => {
    if (expandAll) {
      const m: Record<string, boolean> = {}
      run.events.forEach((e) => (m[e.id] = true))
      setOpenId(m)
    } else setOpenId({})
  }, [expandAll, run.events])
  const fin = run.finished
  const finState = fin ? safeParse(fin.data) : undefined
  // REQ-231④：生效审批策略（run.started 透出 tool_approval {mode, source}——治 P-H2 生效不可见）
  const approvalChip = (() => {
    const started = run.events.find((e) => e.type === 'run.started')
    const ap = started ? safeParse(started.data)?.tool_approval : undefined
    if (!ap || !ap.mode) return null
    const label = ap.mode === 'danger' ? '危险工具审批' : ap.mode === 'all' ? '全部审批' : String(ap.mode)
    return (
      <Tooltip title={`生效来源：${ap.source === 'conversation' ? '会话级覆盖' : '智能体级策略'}`}>
        <Tag color={ap.mode === 'all' ? 'purple' : 'gold'} style={{ margin: 0, fontSize: 10 }}>
          审批·{label}{ap.source === 'conversation' ? '（会话覆盖）' : ''}
        </Tag>
      </Tooltip>
    )
  })()
  return (
    <div style={{ marginBottom: 14, border: '1px solid var(--c-border, #e3e6f0)', borderRadius: 8, overflow: 'hidden' }}>
      <div style={{ background: 'var(--c-bg-soft, #f6f7fb)', padding: '6px 10px', display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
        <Typography.Text strong style={{ fontSize: 12 }}>运行 {index + 1}</Typography.Text>
        <Typography.Text type="secondary" style={{ fontSize: 11 }}>{fmtTime(run.startedAt)}</Typography.Text>
        {approvalChip}
        {fin && (
          <Tag color={fin.type === 'run.error' ? 'red' : (finState?.reason === 'verify_failed' ? 'orange' : 'green')} style={{ margin: 0, fontSize: 10 }}>
            {fin.type === 'run.error' ? '出错' : finState?.reason ? String(finState.reason) : '完成'}
          </Tag>
        )}
        {finState?.duration_ms != null && <Typography.Text type="secondary" style={{ fontSize: 11 }}>{finState.duration_ms}ms</Typography.Text>}
        {finState?.usage?.total_tokens != null && (
          <Tooltip title={`输入 ${finState.usage.input_tokens ?? '—'} / 输出 ${finState.usage.output_tokens ?? '—'}`}>
            <Tag style={{ margin: 0, fontSize: 10 }}>{finState.usage.total_tokens} tok</Tag>
          </Tooltip>
        )}
        <span style={{ flex: 1 }} />
        <Button size="small" type="link" icon={<RollbackOutlined />} onClick={onReplay}>重放此运行</Button>
      </div>
      <div style={{ padding: '4px 0' }}>
        {run.events.map((e) => {
          const d = safeParse(e.data)
          const desc = describeEvent(e.type, d)
          const depth = e.type === 'subagent.enter' || e.type === 'subagent.exit' ? 1 : 0
          const dur = e.type === 'tool.result' && d?.duration_ms != null ? `${d.duration_ms}ms` : ''
          return (
            <div key={e.id} style={{ padding: '3px 10px 3px' }}>
              <div style={{ display: 'flex', alignItems: 'baseline', gap: 6, paddingLeft: depth * 16, cursor: 'pointer' }}
                onClick={() => setOpenId((m) => ({ ...m, [e.id]: !m[e.id] }))}>
                <Tag color={TYPE_COLOR[e.type] ?? 'default'} style={{ margin: 0, fontSize: 10 }}>{e.type}</Tag>
                <span style={{ fontSize: 12, flex: 1 }} className={desc.warn ? 'warn-text' : ''}>{desc.text}</span>
                {d?.source && <Tag style={{ margin: 0, fontSize: 10 }}>{String(d.source)}</Tag>}
                {dur && <span style={{ fontSize: 11, color: 'var(--c-ink-2, #8a90a5)' }}>{dur}</span>}
              </div>
              {(showRaw || openId[e.id]) && e.data && (
                <pre className="raw-json" style={{ margin: '2px 0 2px 34px', maxHeight: 240, overflow: 'auto' }}>{JSON.stringify(d ?? e.data, null, 2)}</pre>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
