import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Card, Drawer, Empty, Space, Spin, Table, Tag, Tooltip, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PlayCircleOutlined, ReloadOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { api } from '../../../api/client'
import type { LifecyclePlan, LifecycleAction } from '../../../api/client'

// ---------------------------------------------------------------------------
// REQ-155/M-O15 阶段二：方案生命周期面板（Terraform 式 plan/apply，monitor=plan 只读形态）。
//   期望态 = 方案配置声明（全部声明为期望运行）；实际态 = 运行平面 status + 加载版本快照；
//   plan = start（已声明未运行）/ reload（spec 版本漂移：当前版本超前于加载快照）；
//   apply = 逐项执行（经主平台反代调运行平面）；stop 不入 plan（摘除方案 = 删配置）。
// ---------------------------------------------------------------------------

export default function LifecyclePane({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [plan, setPlan] = useState<LifecyclePlan | null>(null)
  const [loading, setLoading] = useState(false)
  const [applying, setApplying] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [results, setResults] = useState<{ ok: number; total: number; fails: string[] } | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    api
      .lifecyclePlan()
      .then((p) => {
        setPlan(p)
        setErr(null)
      })
      .catch((e: any) => setErr(e?.message ?? '生命周期计划加载失败'))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    if (open) {
      setResults(null)
      load()
    }
  }, [open, load])

  const applyAll = () => {
    if (!plan || plan.actions.length === 0) return
    setApplying(true)
    const actions: LifecycleAction[] = plan.actions
    api
      .lifecycleApply(actions)
      .then((r) => {
        const fails = (r.results ?? []).filter((x: any) => !x.ok).map((x: any) => `${x.action} ${x.profile_id}: ${x.error}`)
        setResults({ ok: r.applied, total: r.total, fails })
        load()
      })
      .catch((e: any) => setErr(e?.message ?? '应用失败'))
      .finally(() => setApplying(false))
  }

  const columns: ColumnsType<NonNullable<LifecyclePlan['profiles'][number]>> = [
    {
      title: '方案',
      dataIndex: 'profile_name',
      width: 160,
      render: (v, r) => (
        <Tooltip title={r.profile_id}>
          <Typography.Text strong style={{ fontSize: 12.5 }}>{v}</Typography.Text>
        </Tooltip>
      ),
    },
    { title: '引擎', dataIndex: 'engine', width: 84 },
    {
      title: '实际状态',
      dataIndex: 'status',
      width: 90,
      render: (v: string) => (
        <Tag color={v === 'running' ? 'green' : v === 'error' ? 'red' : 'default'} style={{ margin: 0 }}>
          {v === 'running' ? '运行中' : v === 'error' ? '错误' : v === 'starting' ? '启动中' : '停止'}
        </Tag>
      ),
    },
    {
      title: '本体（当前版本 / 加载快照）',
      render: (_, r) => (
        <Space size={4} wrap>
          {r.ontologies.length === 0 && <Typography.Text type="secondary" style={{ fontSize: 12 }}>未配置本体</Typography.Text>}
          {r.ontologies.map((oid) => {
            const drifted = (r.drifted ?? []).includes(oid)
            const loaded = r.loaded[oid]
            const cur = r.current[oid]
            return (
              <Tag key={oid} color={drifted ? 'orange' : 'default'} style={{ margin: 0, fontSize: 11 }}>
                {oid.slice(0, 10)}… {cur ?? '?'}/{loaded ?? '—'}
                {drifted ? ' ⚠' : ''}
              </Tag>
            )
          })}
          {r.unknown_drift && <Tag color="default" style={{ margin: 0, fontSize: 11 }}>无加载基线</Tag>}
        </Space>
      ),
    },
    { title: '备注', dataIndex: 'last_error', ellipsis: true, render: (v) => v || '—' },
  ]

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width={880}
      title={
        <Space>
          <ThunderboltOutlined style={{ color: 'var(--c-brand)' }} />
          方案生命周期（REQ-155 · plan / apply）
        </Space>
      }
      destroyOnHidden
      extra={
        <Button size="small" icon={<ReloadOutlined />} onClick={load}>
          重新计算计划
        </Button>
      }
    >
      {err && <Alert type="error" showIcon style={{ marginBottom: 10 }} message="生命周期计划加载失败" description={err} action={<Button size="small" onClick={load}>重试</Button>} />}

      {results && (
        <Alert
          type={results.fails.length === 0 ? 'success' : 'warning'}
          showIcon
          style={{ marginBottom: 10 }}
          message={`已执行 ${results.ok}/${results.total} 项动作`}
          description={results.fails.length > 0 ? results.fails.join('；') : '全部动作成功；计划已重算。'}
        />
      )}

      {loading ? (
        <div style={{ padding: '32px 0', textAlign: 'center' }}><Spin /></div>
      ) : plan ? (
        <>
          <Alert
            type={plan.actions.length === 0 ? 'success' : 'info'}
            showIcon
            style={{ marginBottom: 10 }}
            message={plan.actions.length === 0 ? '无待执行动作——期望态与实际态一致' : `计划包含 ${plan.actions.length} 项动作（start=拉起未运行方案；reload=spec 版本漂移重载）`}
          />
          <Table
            rowKey="profile_id"
            columns={columns}
            dataSource={plan.profiles}
            size="small"
            pagination={false}
            locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无运行方案（到「本体构建」创建并在此声明部署）" /> }}
          />
          {plan.actions.length > 0 && (
            <Card size="small" style={{ marginTop: 12 }}>
              <Typography.Paragraph strong style={{ fontSize: 12.5, marginBottom: 6 }}>动作清单</Typography.Paragraph>
              {plan.actions.map((a, i) => (
                <div key={i} style={{ fontSize: 12, marginBottom: 4 }}>
                  <Tag color={a.action === 'start' ? 'green' : 'orange'} style={{ margin: 0 }}>
                    {a.action === 'start' ? <PlayCircleOutlined /> : <ReloadOutlined />} {a.action}
                  </Tag>
                  <b>{a.profile_name || a.profile_id}</b>：{a.reason}
                </div>
              ))}
              <Button type="primary" size="small" icon={<ThunderboltOutlined />} loading={applying} style={{ marginTop: 8 }} onClick={applyAll}>
                应用全部动作（{plan.actions.length}）
              </Button>
            </Card>
          )}
          {plan.actions.length === 0 && plan.profiles.length > 0 && (
            <Typography.Text type="secondary" style={{ fontSize: 11 }}>
              漂移检测说明：方案启动/重载成功时会记录各本体加载版本快照；本体保存新版本后此处将出现 reload 计划。stop 不入计划——摘除方案 = 删除其配置。
            </Typography.Text>
          )}
        </>
      ) : null}
    </Drawer>
  )
}
