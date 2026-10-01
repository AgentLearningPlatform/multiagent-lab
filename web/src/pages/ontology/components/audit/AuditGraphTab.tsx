import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Empty, Skeleton, Space, Table, Tag, Tooltip, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { ExportOutlined, ReloadOutlined } from '@ant-design/icons'
import LoadErrorAlert from '../../../../components/LoadErrorAlert'
import { api } from '../../../../api/client'
import type { KGClaim, KGEntity, KGReadResult, KGRelationship } from '../../../../api/types'
import { useUI } from '../../../../store/ui'

// ---------------------------------------------------------------------------
// 消费与审计 · KG 图谱页签（消费链路：实体 / 关系 / claim 溯源）
// A4（REQ-145/M22）：claim 溯源表开 AntD 6 Table virtual 虚拟滚动（一库可达上千
// claim，1000+ 行滚动不卡）+ 分页统一 pageSize 10（与 TraceTable 对齐）。
// M36/KB-13（I1 裁定）：本页收敛为「观测台只读展示」——数据来源标注 + 治理跳转；
// 原「重建 KG」按钮退役（治理入口收敛知识库模块图谱视图，P2 原则「展示位≠管理入口」）。
// ---------------------------------------------------------------------------

export default function AuditGraphTab({ kbId, kbName }: { kbId?: string; kbName?: string }) {
  const { setPage } = useUI()
  const [data, setData] = useState<KGReadResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const load = () => {
    if (!kbId) return
    setLoading(true)
    api
      .kgRead(kbId)
      .then((d) => {
        setData(d)
        setErr(null)
      })
      .catch((e: any) => {
        setData(null)
        setErr(e?.message ?? 'KG 读取失败')
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    setData(null)
    setErr(null)
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kbId])

  const entities = data?.entities ?? []
  const rels = data?.relationships ?? []

  // 实体按 type 分组展示（无 type 归「未分类」；hook 须在早退之前调用）
  const byType = useMemo(() => {
    const m = new Map<string, KGEntity[]>()
    for (const e of entities) {
      const k = e.type || '未分类'
      if (!m.has(k)) m.set(k, [])
      m.get(k)!.push(e)
    }
    return [...m.entries()].sort((a, b) => b[1].length - a[1].length)
  }, [entities])

  if (!kbId) return <Empty description="先在顶部选择知识库" style={{ marginTop: 24 }} />
  if (err) return <LoadErrorAlert title="KG 读取失败" message={err} onRetry={load} style={{ marginTop: 24 }} />

  const claimColumns: ColumnsType<KGClaim> = [
    { title: '主体', dataIndex: 'subject', width: 140, ellipsis: true },
    { title: '陈述（原文句）', dataIndex: 'text', ellipsis: true },
    {
      title: '出处 chunk',
      dataIndex: 'chunk_id',
      width: 200,
      render: (v) =>
        v ? (
          <Tooltip title={v}>
            <Typography.Text code style={{ fontSize: 12 }}>
              {String(v).slice(0, 12)}…
            </Typography.Text>
          </Tooltip>
        ) : (
          '—'
        ),
    },
  ]

  return (
    <div className="sema-home">
      {/* M36/KB-13（D-O19 I1 裁定）：KG 展示位标注数据来源=知识库，观测台只读定位——
          「承载不等于拥有，展示不等于管理」；治理/重建/审核入口收敛知识库模块图谱视图 */}
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        title={
          <Space size={8} wrap>
            <span>数据来源：知识库「{kbName || kbId}」（观测台只读展示；治理与重建入口在知识库模块）。本体 TTL 装载引擎后的运行态图谱在「本体资产 → 可视化 → 运行态实渲」（REQ-234③ 对齐 D-O19 观测台定位）</span>
            <Button
              size="small"
              type="link"
              onClick={() => {
                localStorage.setItem('eino.kb.focus', kbId)
                setPage('knowledge')
              }}
            >
              前往知识库治理 <ExportOutlined />
            </Button>
          </Space>
        }
      />
      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">1</span>
            <span>实体（按类型分组）</span>
          </Space>
        }
        extra={
          <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={load}>
            刷新
          </Button>
        }
      >
        {!data && loading ? (
          <Skeleton active title={false} paragraph={{ rows: 4 }} style={{ padding: '12px 0' }} />
        ) : entities.length === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description="该库暂无 KG（graphrag 模式导入文档时自动抽取；重建入口在知识库模块图谱视图——本页为只读观测台）"
          />
        ) : (
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12 }}>
            {byType.map(([t, es]) => (
              <Card
                key={t}
                type="inner"
                size="small"
                title={
                  <Space size={6}>
                    <Tag color="blue" style={{ margin: 0 }}>
                      {t}
                    </Tag>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {es.length}
                    </Typography.Text>
                  </Space>
                }
                style={{ width: 320 }}
              >
                {es.slice(0, 12).map((e) => (
                  <Tooltip key={e.id} title={e.description || e.name}>
                    <Tag style={{ marginBottom: 4 }}>{e.name}</Tag>
                  </Tooltip>
                ))}
                {es.length > 12 && (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    …共 {es.length} 个
                  </Typography.Text>
                )}
              </Card>
            ))}
          </div>
        )}
      </Card>

      {rels.length > 0 && (
        <Card
          size="small"
          className="work-card sema-card"
          title={
            <Space size={8}>
              <span className="sema-card-no">2</span>
              <span>关系（{rels.length}）</span>
            </Space>
          }
        >
          <div className="sema-claims">
            {rels.slice(0, 40).map((r: KGRelationship) => (
              <div className="sema-claim" key={r.id}>
                <p className="sema-claim-text">
                  <Typography.Text strong>{r.source}</Typography.Text>
                  <Tag color="purple" style={{ margin: '0 8px' }}>
                    {r.type || '关联'}
                  </Tag>
                  <Typography.Text strong>{r.target}</Typography.Text>
                </p>
              </div>
            ))}
            {rels.length > 40 && (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                …仅展示前 40 条（共 {rels.length}）
              </Typography.Text>
            )}
          </div>
        </Card>
      )}

      {(data?.claims?.length ?? 0) > 0 && (
        <Card
          size="small"
          className="work-card sema-card"
          title={
            <Space size={8}>
              <span className="sema-card-no">3</span>
              <span>claim 溯源（陈述 → 原文 chunk）</span>
            </Space>
          }
        >
          <Table<KGClaim>
            rowKey="id"
            columns={claimColumns}
            dataSource={data!.claims}
            virtual
            pagination={{ pageSize: 10, hideOnSinglePage: true }}
            size="small"
            scroll={{ x: 'max-content', y: 400 }}
          />
        </Card>
      )}
    </div>
  )
}
