import { Alert, Badge, Tag } from 'antd'
import type { BadgeProps } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import type { Ontology, RuntimeProfile, Spec, ValidationError } from '../../api/types'

// ---------------------------------------------------------------------------
// 七阶段定义（REQ-74 模型；D-O11 后为学习中心理论骨架 + 自定义构建 S1~S4）
// ---------------------------------------------------------------------------

/** 七阶段：key / 简称 / 可用路径徽标（S1 三条路径，其余内置） */
export const STAGE_DEFS: { key: string; short: string; modes: { t: string; c: string }[] }[] = [
  { key: 's1', short: '本体来源', modes: [{ t: '内置示例', c: 'default' }, { t: 'AI 创建', c: 'blue' }, { t: '导入+手动', c: 'purple' }] },
  { key: 's2', short: '编辑', modes: [{ t: '内置', c: 'default' }] },
  { key: 's3', short: '校验', modes: [{ t: '内置', c: 'default' }] },
  { key: 's4', short: '可视化', modes: [{ t: '内置', c: 'default' }] },
  { key: 's5', short: '运行方式', modes: [{ t: '内置', c: 'default' }] },
  { key: 's6', short: '对外暴露', modes: [{ t: '内置', c: 'default' }] },
  { key: 's7', short: '对接智能体', modes: [{ t: '内置', c: 'default' }] },
]

export interface ValidationState {
  ok: boolean
  errors: ValidationError[]
}

/**
 * 阶段完成度派生（诚实规则，不编造 n/7）：
 *  S1 创建即完成；S2/S4 = 有概念（n_concepts>0，或当前已加载 spec 有概念）；
 *  S3 = 最近一次校验 ok（缓存于调用方 state）；S5/S6 = 存在 status=running 且 ontology_ids 含本体的方案；
 *  S7 为信息展示，不参与计数（始终 false）。
 */
export function stageDoneFlags(
  o: Ontology,
  validation: ValidationState | undefined,
  profiles: RuntimeProfile[],
  activeSpec: Spec | null | undefined,
  isActive: boolean,
): boolean[] {
  const concepts = isActive ? activeSpec?.concepts?.length ?? o.n_concepts ?? 0 : o.n_concepts ?? 0
  const hasConcepts = concepts > 0
  const running = profiles.some((p) => p.status === 'running' && (p.ontology_ids ?? []).includes(o.id))
  return [true, hasConcepts, !!validation?.ok, hasConcepts, running, running, false]
}

/** 资产状态派生：运行中（已挂载 running 方案）/ 已建（有 Spec）/ 空（无 Spec） */
export function ontoStatus(o: Ontology, profiles: RuntimeProfile[]): { color: string; text: string } {
  const running = profiles.some((p) => p.status === 'running' && (p.ontology_ids ?? []).includes(o.id))
  if (running) return { color: 'green', text: '运行中' }
  if ((o.n_concepts ?? 0) > 0) return { color: 'blue', text: '已建' }
  return { color: 'default', text: '空' }
}

export const PROFILE_BADGE: Record<RuntimeProfile['status'], { status: BadgeProps['status']; text: string }> = {
  created: { status: 'default', text: '已创建' },
  starting: { status: 'processing', text: '启动中' },
  running: { status: 'success', text: '运行中' },
  stopped: { status: 'default', text: '已停止' },
  error: { status: 'error', text: '错误' },
}

export const ERR_COLUMNS: ColumnsType<ValidationError> = [
  { title: '路径', dataIndex: 'path', width: 260, render: (v: string) => <Tag style={{ margin: 0, fontFamily: 'var(--mono)', fontSize: 12 }}>{v}</Tag> },
  { title: '问题', dataIndex: 'message' },
]

/** 资产来源徽标（fork 来源记录 forked_from） */
export function sourceTag(o: Ontology): { color: string; text: string } {
  if (o.forked_from) return { color: 'orange', text: 'Fork' }
  if (o.id === 'onto_k8s_ops') return { color: 'cyan', text: '内置示例' }
  return { color: 'default', text: '自定义' }
}

/** 运行方案「需重载」提示（REQ-87：仓库新版本需在方案页显式重载） */
export function ReloadHintAlert({ version }: { version?: number }) {
  return (
    <Alert
      type="info"
      showIcon
      style={{ marginTop: 10 }}
      message={`已保存新版本${version != null ? `（v${version}）` : ''}；运行方案需在其页面显式重载后生效`}
      description="构建与运行解耦（REQ-87）：仓库是唯一事实源，运行方案是部署视图——到「本体运行」栏对应方案执行「重载」后查询按新版本返回。"
    />
  )
}

export function StatusBadge({ p }: { p: RuntimeProfile }) {
  const badge = PROFILE_BADGE[p.status] ?? { status: 'default' as const, text: p.status }
  if (p.status === 'error' && p.last_error) {
    return (
      <Badge status={badge.status} text={<span title={p.last_error}>{badge.text}</span>} />
    )
  }
  return <Badge status={badge.status} text={badge.text} />
}
