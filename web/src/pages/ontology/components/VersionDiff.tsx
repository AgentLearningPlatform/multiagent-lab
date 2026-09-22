import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Empty, Select, Spin, Statistic, Table, Tabs, Tag, Typography } from 'antd'
import { SwapOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { api } from '../../../api/client'
import type { DiffChanged, DiffCollection, DiffItem, DiffResult, VersionMeta } from '../../../api/types'

// ---------------------------------------------------------------------------
// 版本 diff（REQ-95，04 §4.8.4）：双版本选择 → 三集合 added/removed/changed + 引用影响
// ---------------------------------------------------------------------------

const KIND_TAG: Record<string, { color: string; label: string }> = {
  added: { color: 'green', label: '新增' },
  removed: { color: 'red', label: '删除' },
  changed: { color: 'orange', label: '修改' },
}

/** 单集合变更表（added/removed 展示元素名，changed 展示字段级变化） */
function DiffSetTable({ set, kind }: { set: DiffCollection; kind: 'concepts' | 'relations' | 'instances' }) {
  const rows = useMemo(() => {
    const out: { key: string; kind: string; name: string; detail: string }[] = []
    for (const it of set.added as DiffItem[]) {
      out.push({ key: `a-${it.name}`, kind: 'added', name: it.name, detail: describeItem(kind, it) })
    }
    for (const it of set.removed as DiffItem[]) {
      out.push({ key: `r-${it.name}`, kind: 'removed', name: it.name, detail: describeItem(kind, it) })
    }
    for (const ch of set.changed as DiffChanged[]) {
      out.push({ key: `c-${ch.name}`, kind: 'changed', name: ch.name, detail: describeFields(ch) })
    }
    return out
  }, [set, kind])

  const columns: ColumnsType<(typeof rows)[number]> = [
    {
      title: '变更',
      dataIndex: 'kind',
      width: 80,
      render: (k: string) => <Tag color={KIND_TAG[k]?.color}>{KIND_TAG[k]?.label ?? k}</Tag>,
    },
    { title: '名称', dataIndex: 'name', width: 180, render: (n: string) => <Typography.Text code>{n}</Typography.Text> },
    { title: '详情', dataIndex: 'detail' },
  ]

  if (rows.length === 0) {
    return <Typography.Text type="secondary">无变更</Typography.Text>
  }
  return (
    <Table
      size="small"
      columns={columns}
      dataSource={rows}
      rowKey="key"
      pagination={rows.length > 20 ? { pageSize: 20 } : false}
    />
  )
}

function describeItem(kind: string, it: DiffItem): string {
  if (kind === 'concepts') {
    const c = it as { label?: string; definition?: string; parents?: string[] }
    const parts: string[] = []
    if (c.label) parts.push(`label=${c.label}`)
    if (c.parents?.length) parts.push(`parents=[${c.parents.join(', ')}]`)
    if (c.definition) parts.push(c.definition)
    return parts.join(' · ')
  }
  if (kind === 'relations') {
    const r = it as { from?: string; to?: string; label?: string }
    return `${r.from ?? '?'} → ${r.to ?? '?'}${r.label ? `（${r.label}）` : ''}`
  }
  const i = it as { concept?: string; attributes?: Record<string, unknown> }
  const attrs = i.attributes ? Object.entries(i.attributes).map(([k, v]) => `${k}=${String(v)}`).join(', ') : ''
  return `${i.concept ?? '?'}${attrs ? ` · ${attrs}` : ''}`
}

function describeFields(ch: DiffChanged): string {
  return (
    Object.entries(ch.fields ?? {})
      // antd Table 单元格内保持单行摘要；完整 from/to 在 Tooltip/展开行可见性由体积决定，学习场景摘要足够
      .map(([f, d]) => `${f}: ${shortVal(d.from)} → ${shortVal(d.to)}`)
      .join('；')
  )
}

function shortVal(v: unknown): string {
  if (v == null) return '∅'
  const s = typeof v === 'string' ? v : JSON.stringify(v)
  return s.length > 40 ? s.slice(0, 40) + '…' : s
}

/** 版本 diff 面板：版本对选择 + 汇总统计 + 三集合表 + 引用影响 */
export default function VersionDiff({ ontologyId, versions, currentVersion }: { ontologyId: string; versions: VersionMeta[]; currentVersion?: number }) {
  const opts = versions.map((v) => ({ value: v.version, label: `v${v.version} · ${v.created_at}` }))
  const latest = versions.length ? versions[versions.length - 1].version : currentVersion
  const [fromV, setFromV] = useState<number | null>(versions.length > 1 ? versions[versions.length - 2].version : null)
  const [toV, setToV] = useState<number | null>(latest ?? null)
  const [result, setResult] = useState<DiffResult | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (fromV == null || toV == null || fromV === toV) {
      setResult(null)
      setErr(fromV === toV && fromV != null ? '请选择两个不同版本' : null)
      return
    }
    let alive = true
    setLoading(true)
    setErr(null)
    api
      .diffOntologyVersions(ontologyId, fromV, toV)
      .then((r) => alive && setResult(r))
      .catch((e: any) => alive && (setResult(null), setErr(e.message)))
      .finally(() => alive && setLoading(false))
    return () => {
      alive = false
    }
  }, [ontologyId, fromV, toV])

  const swap = () => {
    if (fromV != null && toV != null) {
      setFromV(toV)
      setToV(fromV)
    }
  }

  const total = result
    ? result.concepts.added.length + result.concepts.removed.length + result.concepts.changed.length +
      result.relations.added.length + result.relations.removed.length + result.relations.changed.length +
      result.instances.added.length + result.instances.removed.length + result.instances.changed.length
    : 0

  if (!versions.length) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无版本快照（保存后生成）" />
  }

  return (
    <div>
      <div className="onto-sec" style={{ marginTop: 4 }}>
        <span className="onto-sec-title">对比</span>
        <Select size="small" value={fromV ?? undefined} onChange={setFromV} style={{ width: 220 }} placeholder="起始版本" options={opts} />
        <Button size="small" icon={<SwapOutlined />} onClick={swap} aria-label="交换对比方向" />
        <Select size="small" value={toV ?? undefined} onChange={setToV} style={{ width: 220 }} placeholder="目标版本" options={opts} />
      </div>
      {err ? (
        <Alert type="warning" showIcon message="版本对比失败" description={err} />
      ) : loading ? (
        <Spin size="small" />
      ) : !result ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="选择两个版本开始对比" />
      ) : (
        <>
          <div style={{ display: 'flex', gap: 24, margin: '8px 0' }}>
            <Statistic title="概念" value={result.concepts.added.length} suffix={`/ +${result.concepts.added.length} -${result.concepts.removed.length} ~${result.concepts.changed.length}`} valueStyle={{ fontSize: 14 }} />
            <Statistic title="关系" value={result.relations.added.length} suffix={`/ +${result.relations.added.length} -${result.relations.removed.length} ~${result.relations.changed.length}`} valueStyle={{ fontSize: 14 }} />
            <Statistic title="实例" value={result.instances.added.length} suffix={`/ +${result.instances.added.length} -${result.instances.removed.length} ~${result.instances.changed.length}`} valueStyle={{ fontSize: 14 }} />
            <Statistic title="变更合计" value={total} valueStyle={{ fontSize: 14 }} />
          </div>
          <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
            v{result.from_version} → v{result.to_version}；+ 新增 / − 删除 / ~ 修改
          </Typography.Paragraph>
          <Tabs
            size="small"
            items={[
              { key: 'concepts', label: `概念 (${totalConcepts(result)})`, children: <DiffSetTable set={result.concepts} kind="concepts" /> },
              { key: 'relations', label: `关系 (${totalRelations(result)})`, children: <DiffSetTable set={result.relations} kind="relations" /> },
              { key: 'instances', label: `实例 (${totalInstances(result)})`, children: <DiffSetTable set={result.instances} kind="instances" /> },
              {
                key: 'impact',
                label: `引用影响 (${result.impact.length})`,
                children: result.impact.length ? (
                  <Table
                    size="small"
                    rowKey="name"
                    columns={[
                      { title: '概念', dataIndex: 'name', render: (n: string) => <Typography.Text code>{n}</Typography.Text> },
                      { title: '被引用次数', dataIndex: 'referenced_by', width: 120 },
                    ]}
                    dataSource={result.impact}
                    pagination={false}
                  />
                ) : (
                  <Typography.Text type="secondary">无删除/修改的概念，无引用影响</Typography.Text>
                ),
              },
            ]}
          />
        </>
      )}
    </div>
  )
}

const totalConcepts = (r: DiffResult) => r.concepts.added.length + r.concepts.removed.length + r.concepts.changed.length
const totalRelations = (r: DiffResult) => r.relations.added.length + r.relations.removed.length + r.relations.changed.length
const totalInstances = (r: DiffResult) => r.instances.added.length + r.instances.removed.length + r.instances.changed.length
