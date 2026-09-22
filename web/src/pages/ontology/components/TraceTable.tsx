import { useEffect, useState } from 'react'
import { Alert, Button, Empty, Select, Table, Tag, Tooltip, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { CopyOutlined, ReloadOutlined } from '@ant-design/icons'
import { api } from '../../../api/client'
import type { RuntimeProfile, TraceEntry } from '../../../api/types'
import { useUI } from '../../../store/ui'
import { PROFILE_BADGE } from '../shared'

// ---------------------------------------------------------------------------
// 翻译透视（REQ-94）：facade 执行 onto_* 的调用日志，时间倒序，行展开看 SPARQL 原文
// ---------------------------------------------------------------------------

export default function TraceTable({ profiles }: { profiles: RuntimeProfile[] }) {
  const { showToast } = useUI()
  const [pid, setPid] = useState<string | undefined>(profiles[0]?.id)
  const [traces, setTraces] = useState<TraceEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    if (pid && !profiles.some((p) => p.id === pid)) setPid(profiles[0]?.id)
    else if (!pid && profiles.length > 0) setPid(profiles[0]?.id)
  }, [profiles, pid])

  const load = () => {
    if (!pid) {
      setTraces([])
      return
    }
    setLoading(true)
    setErr(null)
    api
      .listTraces(pid, 50)
      .then((r) => setTraces(r.traces ?? []))
      .catch((e: any) => {
        setTraces([])
        setErr(e.message)
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pid])

  const copy = (text: string) => {
    navigator.clipboard
      ?.writeText(text ?? '')
      .then(() => showToast('SPARQL 已复制'))
      .catch(() => showToast('复制失败', 'err'))
  }

  const columns: ColumnsType<TraceEntry> = [
    { title: '时间', dataIndex: 'ts', width: 170 },
    { title: '工具', dataIndex: 'tool', width: 150, render: (v) => <Typography.Text code style={{ fontSize: 12 }}>{String(v)}</Typography.Text> },
    { title: '本体', dataIndex: 'ontology_id', width: 160, ellipsis: true },
    { title: '耗时', dataIndex: 'took_ms', width: 90, render: (v) => `${v} ms` },
    { title: '结果数', dataIndex: 'result_count', width: 80 },
    {
      title: '状态',
      dataIndex: 'ok',
      width: 80,
      render: (v: boolean) =>
        v ? (
          <Tag color="green" style={{ margin: 0 }}>
            成功
          </Tag>
        ) : (
          <Tag color="red" style={{ margin: 0 }}>
            失败
          </Tag>
        ),
    },
    {
      title: '错误',
      dataIndex: 'error',
      ellipsis: true,
      render: (v?: string) =>
        v ? (
          <Tooltip title={v}>
            <Typography.Text type="danger" style={{ fontSize: 12 }}>
              {v}
            </Typography.Text>
          </Tooltip>
        ) : (
          <Typography.Text type="secondary">—</Typography.Text>
        ),
    },
  ]

  if (profiles.length === 0) {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        style={{ margin: '24px 0' }}
        description="暂无运行方案；执行 onto_* 工具后在此查看翻译透视"
      />
    )
  }

  return (
    <>
      <div className="onto-sec" style={{ marginTop: 4 }}>
        <span className="onto-sec-title">翻译透视（REQ-94，最近 50 条，失败查询同样留痕）</span>
        <span className="hit-spacer" />
        <Select
          size="small"
          value={pid}
          onChange={setPid}
          style={{ width: 260 }}
          options={profiles.map((p) => ({ value: p.id, label: `${p.name}（${PROFILE_BADGE[p.status]?.text ?? p.status}）` }))}
        />
        <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={load}>
          刷新
        </Button>
      </div>
      {err && <Alert type="warning" showIcon style={{ marginBottom: 10 }} message="透视记录获取失败" description={err} />}
      <Table<TraceEntry>
        rowKey={(r) => String(r.id ?? `${r.ts}-${r.tool}-${r.ontology_id}`)}
        columns={columns}
        dataSource={traces}
        loading={loading}
        pagination={{ pageSize: 10, hideOnSinglePage: true }}
        size="small"
        locale={{ emptyText: '暂无透视记录（执行 onto_* 工具后生成）' }}
        expandable={{
          expandedRowRender: (r) => (
            <div className="onto-trace-expand">
              <div className="onto-sec" style={{ marginTop: 0 }}>
                <span className="onto-sec-title">SPARQL 原文</span>
                <span className="hit-spacer" />
                <Button size="small" icon={<CopyOutlined />} onClick={() => copy(r.sparql)}>
                  复制
                </Button>
              </div>
              <pre className="onto-guide-pre">{r.sparql || '（空）'}</pre>
            </div>
          ),
        }}
      />
    </>
  )
}
