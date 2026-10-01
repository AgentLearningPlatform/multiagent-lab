import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Card, Empty, Space, Spin, Switch, Table, Tag, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ReloadOutlined, SafetyOutlined } from '@ant-design/icons'
import { api } from '../../../../api/client'
import type { QualityReport } from '../../../../api/client'

// ---------------------------------------------------------------------------
// REQ-156/M-O15 资产详情「质量卡」页签：qualitygate 报告呈现（复用 REQ-171 引擎，零重复建设）。
//   三维评分（完备性/一致性/可维护性，扣分制加权综合）+ 检查项命中明细 + 本体级 strict 门禁开关
//   （开启后保存/导入合并的错误级命中将被 400 拦截——默认宽松仅告警）。
// ---------------------------------------------------------------------------

const SEV_META: Record<string, { color: string; text: string }> = {
  error: { color: 'red', text: '错误' },
  warning: { color: 'orange', text: '告警' },
  info: { color: 'default', text: '提示' },
}

export default function QualityCardPane({ ontologyId }: { ontologyId: string }) {
  const [config, setConfig] = useState<{ strict: boolean } | null>(null)
  const [report, setReport] = useState<QualityReport | null>(null)
  const [reportAt, setReportAt] = useState<string>('')
  const [loading, setLoading] = useState(false)
  const [running, setRunning] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    api
      .qualityConfig(ontologyId)
      .then((c) => setConfig({ strict: c.strict }))
      .catch(() => setConfig(null))
    api
      .qualityReport(ontologyId)
      .then((r) => {
        setReport(r.report)
        setReportAt(r.imported_at)
        setErr(null)
      })
      .catch(() => {
        setReport(null)
        setReportAt('')
      })
      .finally(() => setLoading(false))
  }, [ontologyId])

  useEffect(load, [load])

  const run = (strictFlag: boolean) => {
    setRunning(true)
    api
      .qualityRun(ontologyId, strictFlag)
      .then((r) => {
        setReport(r.report)
        setReportAt(new Date().toISOString())
        setErr(null)
      })
      .catch((e: any) => setErr(e?.message ?? '质量检查失败'))
      .finally(() => setRunning(false))
  }

  const flipStrict = (v: boolean) => {
    api
      .setQualityConfig(ontologyId, v)
      .then((c) => {
        setConfig({ strict: c.strict })
      })
      .catch((e: any) => setErr(e?.message ?? '开关更新失败'))
  }

  const columns: ColumnsType<NonNullable<QualityReport['findings'][number]>> = [
    {
      title: '级别',
      dataIndex: 'severity',
      width: 70,
      render: (v: string) => <Tag color={SEV_META[v]?.color ?? 'default'} style={{ margin: 0 }}>{SEV_META[v]?.text ?? v}</Tag>,
    },
    { title: '检查项', dataIndex: 'title', ellipsis: true },
    { title: '维度', dataIndex: 'dimension', width: 84, render: (v: string) => ({ completeness: '完备性', consistency: '一致性', maintainability: '可维护性' }[v] ?? v) },
    { title: '命中', dataIndex: 'count', width: 60, render: (v: number) => <b>{v}</b> },
    {
      title: '样例',
      dataIndex: 'samples',
      ellipsis: true,
      render: (v: string[]) => <Typography.Text type="secondary" style={{ fontSize: 12 }}>{(v ?? []).slice(0, 3).join('；')}</Typography.Text>,
    },
  ]

  if (loading) return <div style={{ padding: '24px 0', textAlign: 'center' }}><Spin /></div>

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <Card size="small" className="work-card">
        <div style={{ display: 'flex', alignItems: 'center', gap: 16, flexWrap: 'wrap' }}>
          <Space size={6}>
            <SafetyOutlined style={{ color: 'var(--c-brand)' }} />
            <Typography.Text strong>strict 门禁</Typography.Text>
            <Switch checked={config?.strict ?? false} onChange={flipStrict} disabled={!config} />
          </Space>
          <Typography.Text type="secondary" style={{ fontSize: 12, flex: 1, minWidth: 240 }}>
            开启后，保存 Spec / 导入合并时若质量检查存在错误级命中将被拦截（400 + 明细）；默认宽松（仅本卡告警不阻断）。检查项清单见 qualitygate（REQ-171，11 项数据驱动）。
          </Typography.Text>
          <Button size="small" icon={<ReloadOutlined />} loading={running} onClick={() => run(config?.strict ?? false)}>
            {report ? '重新生成报告' : '生成质量报告'}
          </Button>
        </div>
      </Card>

      {err && <Alert type="error" showIcon title="质量检查失败" description={err} onClose={() => setErr(null)} />}

      {!report ? (
        <div className="work-empty" style={{ minHeight: 200 }}>
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={
              <>
                <p>尚未生成质量报告。</p>
                <p style={{ fontSize: 12, color: 'var(--ant-color-text-tertiary, #888)' }}>
                  点上方「生成质量报告」运行 qualitygate（11 检查项：孤立概念 / 缺失定义 / 层级循环 / 命名风格 / 悬空引用 / 重复命名等）。
                </p>
              </>
            }
          />
        </div>
      ) : (
        <>
          <Card size="small" className="work-card">
            <div style={{ display: 'flex', gap: 24, alignItems: 'center', flexWrap: 'wrap' }}>
              <div style={{ textAlign: 'center' }}>
                <Typography.Title level={2} style={{ margin: 0, color: report.score.overall >= 90 ? '#16a34a' : report.score.overall >= 70 ? '#d97706' : '#dc2626' }}>
                  {report.score.overall.toFixed(1)}
                </Typography.Title>
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>综合质量分</Typography.Text>
              </div>
              {(
                [
                  ['完备性', report.score.completeness],
                  ['一致性', report.score.consistency],
                  ['可维护性', report.score.maintainability],
                ] as [string, number][]
              ).map(([name, v]) => (
                <div key={name} style={{ minWidth: 160, flex: 1 }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12 }}>
                    <span>{name}</span>
                    <Typography.Text type="secondary">{v.toFixed(0)}</Typography.Text>
                  </div>
                  <div style={{ height: 6, borderRadius: 3, background: '#eef0f6', marginTop: 4 }}>
                    <div style={{ width: `${v}%`, height: '100%', borderRadius: 3, background: v >= 90 ? '#16a34a' : v >= 70 ? '#d97706' : '#dc2626' }} />
                  </div>
                </div>
              ))}
              <Space size={6} wrap>
                <Tag color="red" style={{ margin: 0 }}>错误 {report.error_count}</Tag>
                <Tag color="orange" style={{ margin: 0 }}>告警 {report.warning_count}</Tag>
                <Tag style={{ margin: 0 }}>提示 {report.info_count}</Tag>
                <Tag color={report.pass ? 'green' : 'default'} style={{ margin: 0 }}>{report.pass ? '门禁通过' : '存在错误级命中'}</Tag>
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                  规模：{report.stats.concepts} 概念 / {report.stats.relations} 关系 / {report.stats.instances} 实例{reportAt ? ` · ${reportAt.slice(0, 19).replace('T', ' ')}` : ''}
                </Typography.Text>
              </Space>
            </div>
          </Card>

          <Table
            rowKey={(r) => r.check_id}
            columns={columns}
            dataSource={report.findings}
            size="small"
            pagination={false}
            locale={{ emptyText: '全部检查项通过，无命中' }}
          />
        </>
      )}
    </div>
  )
}
