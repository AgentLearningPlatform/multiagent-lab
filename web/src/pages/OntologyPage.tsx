import { useEffect, useMemo, useState } from 'react'
import { Button, Card, Descriptions, Progress, Result, Space, Steps, Tag, Typography } from 'antd'
import { api } from '../api/client'
import type { OntologyDetail, OntologyStage, OntologySummary } from '../api/types'

/** 七阶段顺序与默认名称（后端 stages 缺失时回退，保证 S1~S7 始终可见） */
const STAGE_ORDER = ['s1', 's2', 's3', 's4', 's5', 's6', 's7']
const STAGE_NAMES: Record<string, string> = {
  s1: '本体来源',
  s2: '编辑',
  s3: '校验',
  s4: '可视化',
  s5: '运行方式',
  s6: '对外暴露',
  s7: '对接智能体',
}

/** 执行模式徽标（builtin / guided / managed） */
const MODE: Record<string, { color: string; text: string }> = {
  builtin: { color: 'default', text: '内置' },
  guided: { color: 'blue', text: '引导' },
  managed: { color: 'purple', text: '托管' },
}

/** 流水线状态 → Tag */
const ONTO_STATUS: Record<string, { color: string; text: string }> = {
  running: { color: 'green', text: '运行中' },
  draft: { color: 'default', text: '草稿' },
  importing: { color: 'blue', text: '导入中' },
}

/** 阶段状态（done/current/pending）→ antd Steps 状态（finish/process/wait） */
function stepStatus(s?: OntologyStage['status']): 'wait' | 'process' | 'finish' | 'error' {
  if (s === 'done') return 'finish'
  if (s === 'current') return 'process'
  return 'wait'
}

/** 归一化 stages：按 s1~s7 补齐，缺失项按进度推导 done/current/pending */
function normalizeStages(detail: OntologyDetail): OntologyStage[] {
  const byKey = new Map<string, OntologyStage>()
  for (const s of detail.stages ?? []) byKey.set(String(s.key).toLowerCase(), s)
  const done = detail.progress ?? 0
  return STAGE_ORDER.map((key, i) => {
    const s = byKey.get(key)
    return {
      key,
      title: s?.title || `${key.toUpperCase()} ${STAGE_NAMES[key]}`,
      mode: s?.mode,
      status: s?.status ?? (i < done ? 'done' : i === done ? 'current' : 'pending'),
      detail: s?.detail,
    }
  })
}

function OntoStatusTag({ status }: { status?: string }) {
  const s = ONTO_STATUS[status ?? ''] ?? { color: 'default', text: status || '未知' }
  return (
    <Tag color={s.color} style={{ margin: 0 }}>
      {s.text}
    </Tag>
  )
}

function StageStatusTag({ status }: { status?: OntologyStage['status'] }) {
  if (status === 'done') return <Tag color="green" style={{ margin: 0 }}>已完成</Tag>
  if (status === 'current') return <Tag color="blue" style={{ margin: 0 }}>进行中</Tag>
  return <Tag style={{ margin: 0 }}>待配置</Tag>
}

/** 只读渲染阶段 detail 的值：布尔→是/否，原始数组→Tag 组，对象/复杂数组→JSON */
function DetailValue({ value }: { value: unknown }) {
  if (value === null || value === undefined || value === '') return <Typography.Text type="secondary">—</Typography.Text>
  if (typeof value === 'boolean') return <Tag color={value ? 'green' : 'default'} style={{ margin: 0 }}>{value ? '是' : '否'}</Tag>
  if (Array.isArray(value)) {
    if (value.length === 0) return <Typography.Text type="secondary">—</Typography.Text>
    if (value.every((x) => typeof x === 'string' || typeof x === 'number')) {
      return (
        <Space size={4} wrap>
          {value.map((x, i) => (
            <Tag key={i} style={{ margin: 0 }}>
              {String(x)}
            </Tag>
          ))}
        </Space>
      )
    }
    return <pre className="onto-json">{JSON.stringify(value, null, 2)}</pre>
  }
  if (typeof value === 'object') return <pre className="onto-json">{JSON.stringify(value, null, 2)}</pre>
  return <span>{String(value)}</span>
}

function StageDetail({ stage }: { stage: OntologyStage }) {
  const entries = Object.entries(stage.detail ?? {})
  if (entries.length === 0) {
    return (
      <div className="onto-detail-empty">
        该阶段尚未配置。本体平面按 S1~S7 逐节点配置：来源 / 编辑 / 校验 / 可视化 / 运行方式 / 对外暴露 / 对接智能体。
      </div>
    )
  }
  return (
    <Descriptions
      bordered
      size="small"
      column={1}
      labelStyle={{ width: 170, color: 'var(--c-ink-2)' }}
      items={entries.map(([k, v]) => ({ key: k, label: k, children: <DetailValue value={v} /> }))}
    />
  )
}

/**
 * 本体视图（原型 06 §3.3 / 02 文档 §10，M8 只读壳）：
 * 左栏本体流水线列表（进度 n/7 + running/draft/importing）→ 右栏 S1~S7 七阶段步骤条 + 阶段配置卡。
 * 内容经双反代同源提供（§6.10）；本页按反代契约只读展示，未来由本体平面同源嵌入替换。
 */
export default function OntologyPage() {
  const [list, setList] = useState<OntologySummary[]>([])
  const [listErr, setListErr] = useState<string | null>(null)
  const [activeId, setActiveId] = useState<string | null>(null)
  const [detail, setDetail] = useState<OntologyDetail | null>(null)
  const [detailErr, setDetailErr] = useState<string | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [step, setStep] = useState(0)
  // 详情重试：同一 activeId 下强制重跑 effect（setActiveId 同值不会触发）
  const [tick, setTick] = useState(0)

  const reload = () => {
    api.listOntologies()
      .then((ls) => {
        setList(ls)
        setListErr(null)
        setActiveId((cur) => (cur && ls.some((o) => o.id === cur) ? cur : (ls[0]?.id ?? null)))
      })
      .catch((e: any) => {
        setList([])
        setActiveId(null)
        setListErr(e?.message ?? '加载失败')
      })
  }

  useEffect(reload, [])

  useEffect(() => {
    if (!activeId) {
      setDetail(null)
      setDetailErr(null)
      return
    }
    setDetailLoading(true)
    setDetailErr(null)
    setStep(0)
    api.getOntology(activeId)
      .then((d) => {
        setDetail(d)
        setStep(Math.min(Math.max(d.progress ?? 0, 0), STAGE_ORDER.length - 1))
      })
      .catch((e: any) => {
        setDetail(null)
        setDetailErr(e?.message ?? '加载失败')
      })
      .finally(() => setDetailLoading(false))
  }, [activeId, tick])

  const stages = useMemo(() => (detail ? normalizeStages(detail) : []), [detail])
  const selectedStage = stages[step]
  const activeSummary = list.find((o) => o.id === activeId)

  return (
    <div className="main">
      <aside className="sidebar">
        <div className="side-head">
          <span className="side-title">本体流水线</span>
          <span className="side-count">{list.length}</span>
        </div>
        <div className="side-list">
          {list.map((o) => (
            <div key={o.id} className={`side-item${o.id === activeId ? ' active' : ''}`} onClick={() => setActiveId(o.id)}>
              <div className="side-item-top">
                <span className="side-item-name" title={o.name}>
                  {o.name}
                </span>
                <OntoStatusTag status={o.status} />
              </div>
              <div className="side-item-progress">
                <Progress percent={Math.round(((o.progress ?? 0) / STAGE_ORDER.length) * 100)} size="small" showInfo={false} strokeColor="#4f46e5" />
                <span className="side-item-meta">{o.progress ?? 0}/7</span>
              </div>
            </div>
          ))}
          {list.length === 0 && <div className="empty-hint">{listErr ? '本体平面未就绪' : '暂无本体流水线'}</div>}
        </div>
      </aside>

      <div className="work-main">
        {listErr ? (
          <div className="work-empty">
            <Result
              status="warning"
              title="本体平面未就绪（M8 后端另行部署）"
              subTitle={listErr}
              extra={<Button onClick={reload}>重试</Button>}
            />
          </div>
        ) : !activeId ? (
          <div className="work-empty">
            <Result
              icon={null}
              title="选择左侧本体流水线"
              subTitle="本体视图由本体平面（构建平面 + 运行平面）反代同源提供，主平台按只读契约展示 S1~S7 七阶段。"
            />
          </div>
        ) : detailErr ? (
          <div className="work-empty">
            <Result
              status="warning"
              title="本体详情加载失败"
              subTitle={detailErr}
              extra={<Button onClick={() => setTick((t) => t + 1)}>重试</Button>}
            />
          </div>
        ) : (
          <>
            <div className="work-head">
              <div className="work-head-text">
                <div className="work-head-title">
                  <Typography.Title level={4} style={{ margin: 0 }}>
                    {activeSummary?.name ?? detail?.name ?? '本体流水线'}
                  </Typography.Title>
                  <OntoStatusTag status={activeSummary?.status ?? detail?.status} />
                  <Tag style={{ margin: 0 }}>进度 {activeSummary?.progress ?? detail?.progress ?? 0}/7</Tag>
                </div>
                <p className="work-head-desc">
                  本体流水线（D-O7 七阶段）：来源 → 编辑 → 校验 → 可视化 → 运行方式 → 对外暴露 → 对接智能体；每阶段标注执行模式（内置 / 引导 / 托管）。主平台经反代只读展示。
                </p>
              </div>
            </div>

            <div className="onto-steps">
              <Steps
                size="small"
                orientation="horizontal"
                titlePlacement="vertical"
                current={step}
                onChange={(i) => setStep(i)}
                items={stages.map((s, i) => ({
                  key: s.key,
                  title: s.title,
                  status: stepStatus(s.status),
                  className: i === step ? 'onto-step-selected' : undefined,
                  content: s.mode ? (
                    <Tag color={MODE[s.mode]?.color} style={{ margin: 0, fontSize: 11 }}>
                      {MODE[s.mode]?.text ?? s.mode}
                    </Tag>
                  ) : undefined,
                }))}
              />
            </div>

            {selectedStage && (
              <Card
                className="work-card onto-stage-card"
                size="small"
                loading={detailLoading}
                title={
                  <Space size={8} wrap>
                    {selectedStage.title}
                    {selectedStage.mode && (
                      <Tag color={MODE[selectedStage.mode]?.color} style={{ margin: 0 }}>
                        {MODE[selectedStage.mode]?.text ?? selectedStage.mode}
                      </Tag>
                    )}
                    <StageStatusTag status={selectedStage.status} />
                  </Space>
                }
                extra={<Typography.Text type="secondary" style={{ fontSize: 12 }}>阶段配置（只读）</Typography.Text>}
              >
                <StageDetail stage={selectedStage} />
              </Card>
            )}
          </>
        )}
      </div>
    </div>
  )
}
