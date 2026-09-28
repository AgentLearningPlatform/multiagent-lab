import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Checkbox, Empty, Input, Select, Space, Spin, Steps, Tag, Typography } from 'antd'
import { api } from '../../../api/client'
import LoadErrorAlert from '../../../components/LoadErrorAlert'

// ---------------------------------------------------------------------------
// M-O14 P2②（REQ-171 P2/26 号方案 §9）：OntoExtend 流程从引导卡升格「部分可用」。
// 三步流程：选目标本体 → ODP 精选推荐 + LOV 词表卡片（可选补充）→ 生成扩展草稿 →
// 既有 merge/preview 审查（REQ-157 底座：冲突检测/改名策略）→ merge/apply 入库（strict 门禁+版本快照）。
// 后端：GET /api/ontology/ontoextend/odps（人工精选 12 模式）+ POST /api/ontology/ontoextend/draft。
// ---------------------------------------------------------------------------

interface OdpItem {
  id: string
  name: string
  description: string
  source: string
  spec: { concepts: { name: string; label?: string; definition?: string }[]; relations: { name: string; from: string; to: string }[] }
}
interface OntoItem { id: string; name: string }
interface MergePreviewData {
  strategy: string
  /** 实测形态：扁平标记数组（'concept:组织' / 'relation:隶属于'）；conflicts/renamed 为对象数组 */
  added?: string[]
  renamed?: unknown[]
  conflicts?: unknown[]
}

export default function OntoExtendFlow() {
  const [ontos, setOntos] = useState<OntoItem[]>([])
  const [ontosErr, setOntosErr] = useState<string | null>(null)
  const [targetId, setTargetId] = useState<string | undefined>(undefined)

  const [odps, setOdps] = useState<OdpItem[]>([])
  const [odpsLoading, setOdpsLoading] = useState(false)
  const [odpsErr, setOdpsErr] = useState<string | null>(null)
  const [odpId, setOdpId] = useState<string | undefined>(undefined)

  const [lovQuery, setLovQuery] = useState('')
  const [lovHits, setLovHits] = useState<{ label: string; uri: string; desc?: string }[]>([])
  const [lovLoading, setLovLoading] = useState(false)
  const [lovErr, setLovErr] = useState<string | null>(null)
  const [lovPicked, setLovPicked] = useState<Record<string, { label: string; uri: string; desc?: string }>>({})

  const [preview, setPreview] = useState<MergePreviewData | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [applyErr, setApplyErr] = useState<string | null>(null)
  const [applying, setApplying] = useState(false)
  const [appliedVersion, setAppliedVersion] = useState<number | null>(null)
  const [step, setStep] = useState(0)

  useEffect(() => {
    api
      .listOntologies()
      .then((ls: any) => setOntos(Array.isArray(ls) ? ls : ls?.ontologies ?? ls?.items ?? []))
      .catch((e: any) => setOntosErr(e?.message ?? '本体列表加载失败'))
  }, [])

  const loadOdps = useCallback(() => {
    setOdpsLoading(true)
    fetch('/api/ontology/ontoextend/odps')
      .then((r) => r.json())
      .then((d) => {
        setOdps(d.odps ?? [])
        setOdpsErr(null)
      })
      .catch((e: any) => setOdpsErr(e?.message ?? 'ODP 加载失败'))
      .finally(() => setOdpsLoading(false))
  }, [])
  useEffect(() => {
    loadOdps()
  }, [loadOdps])

  const odp = useMemo(() => odps.find((o) => o.id === odpId) ?? null, [odps, odpId])

  const searchLov = useCallback(() => {
    const q = lovQuery.trim()
    if (!q) return
    setLovLoading(true)
    setLovErr(null)
    fetch(`/api/ontology/vocabularies/search?q=${encodeURIComponent(q)}`)
      .then(async (r) => {
        const d = await r.json()
        if (!r.ok) throw new Error(d.error || `HTTP ${r.status}`)
        const results = d.results ?? d.items ?? []
        setLovHits(results.slice(0, 12))
      })
      .catch((e: any) => setLovErr(e?.message ?? 'LOV 检索失败（上游不可达时为预期降级）'))
      .finally(() => setLovLoading(false))
  }, [lovQuery])

  /** 草稿 = ODP 片段 + LOV 勾选术语（作为附加概念并入 spec 后送 merge 审查） */
  const buildDraftSpec = useCallback(() => {
    if (!odp) return null
    const spec: any = {
      name: odp.spec.concepts?.length ? `${odp.name}扩展片段` : odp.name,
      concepts: [...(odp.spec.concepts ?? [])],
      relations: [...(odp.spec.relations ?? [])],
      instances: [],
    }
    for (const term of Object.values(lovPicked)) {
      spec.concepts.push({ name: term.label, label: term.label, definition: term.desc || `LOV 词表术语（${term.uri}）` })
    }
    return spec
  }, [odp, lovPicked])

  const doPreview = async () => {
    if (!targetId || !odp) return
    const spec = buildDraftSpec()
    if (!spec) return
    setPreviewLoading(true)
    setApplyErr(null)
    setAppliedVersion(null)
    try {
      const pv = await api.mergePreview(targetId, { filename: `ontoextend-${odp.id}.json`, spec, strategy: 'merge' })
      setPreview(pv as unknown as MergePreviewData)
      setStep(2)
    } catch (e: any) {
      setApplyErr(e?.message ?? '审查预览失败')
    } finally {
      setPreviewLoading(false)
    }
  }

  const doApply = async () => {
    if (!targetId || !odp) return
    const spec = buildDraftSpec()
    if (!spec) return
    setApplying(true)
    setApplyErr(null)
    try {
      const res = await api.mergeApply(targetId, { filename: `ontoextend-${odp.id}.json`, spec, strategy: 'merge' })
      setAppliedVersion(res.version ?? null)
      setPreview(null)
    } catch (e: any) {
      setApplyErr(e?.message ?? '入库失败（strict 质量门禁可能拦截：见错误详情）')
    } finally {
      setApplying(false)
    }
  }

  const addedList = useMemo(() => (Array.isArray(preview?.added) ? (preview!.added as string[]) : []), [preview])
  const addedConcepts = addedList.filter((s) => s.startsWith('concept:')).length
  const addedRelations = addedList.filter((s) => s.startsWith('relation:')).length
  const conflictCount = (preview?.conflicts?.length ?? 0) + (preview?.renamed?.length ?? 0)

  return (
    <Card className="work-card" size="small">
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="OntoExtend 流程——ODP 模式推荐 + LOV 词表扩展现有本体（部分可用，M-O14 P2②）"
        description="不新建本体：选取人工精选的设计模式（ODP）与 LOV 词表术语生成扩展片段，经导入审查（冲突检测/改名）与质量门禁后并入目标本体新版本。"
      />
      <Steps
        size="small"
        current={step}
        items={[{ title: '选目标本体' }, { title: 'ODP + 词表' }, { title: '审查入库' }]}
        style={{ marginBottom: 12 }}
      />

      <div className="onto-sec">
        <span className="onto-sec-title">① 目标本体（扩展并入对象）</span>
      </div>
      {ontosErr ? (
        <LoadErrorAlert title="本体列表加载失败" message={ontosErr} onRetry={() => api.listOntologies().then((ls: any) => setOntos(Array.isArray(ls) ? ls : []))} style={{ marginBottom: 8 }} />
      ) : (
        <Select
          showSearch
          optionFilterProp="label"
          style={{ width: 320 }}
          placeholder="选择要扩展的本体"
          value={targetId}
          onChange={(v) => {
            setTargetId(v)
            setPreview(null)
            setAppliedVersion(null)
            setStep(v ? 1 : 0)
          }}
          options={ontos.map((o) => ({ value: o.id, label: o.name }))}
          notFoundContent={ontos.length === 0 ? '暂无本体' : undefined}
        />
      )}

      <div className="onto-sec">
        <span className="onto-sec-title">
          ② ODP 精选推荐（人工精选 12 模式）
          <Button size="small" type="text" onClick={loadOdps} style={{ marginLeft: 6 }}>刷新</Button>
        </span>
      </div>
      {odpsLoading ? (
        <Spin size="small" />
      ) : odpsErr ? (
        <LoadErrorAlert title="ODP 加载失败" message={odpsErr} onRetry={loadOdps} />
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))', gap: 8 }}>
          {odps.map((o) => (
            <Card
              key={o.id}
              size="small"
              hoverable
              onClick={() => {
                setOdpId(o.id)
                setPreview(null)
                if (targetId) setStep(1)
              }}
              style={{ borderColor: odpId === o.id ? 'var(--ant-color-primary, #4f46e5)' : undefined }}
            >
              <div style={{ fontWeight: 600, fontSize: 13 }}>
                {o.name} {odpId === o.id && <Tag color="processing" style={{ marginInlineStart: 4 }}>已选</Tag>}
              </div>
              <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 4 }}>{o.description}</Typography.Paragraph>
              <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                {o.spec.concepts?.length ?? 0} 概念 · {o.spec.relations?.length ?? 0} 关系 ｜ 出处：{o.source}
              </Typography.Text>
            </Card>
          ))}
        </div>
      )}

      <div className="onto-sec">
        <span className="onto-sec-title">③ LOV 词表卡片（可选补充术语）</span>
      </div>
      <Space.Compact style={{ width: 'min(520px, 100%)', marginBottom: 6 }}>
        <Input
          placeholder="检索 LOV 词表术语（如 prov、time）——上游不可达时优雅降级"
          value={lovQuery}
          onChange={(e) => setLovQuery(e.target.value)}
          onPressEnter={searchLov}
          allowClear
        />
        <Button onClick={searchLov} loading={lovLoading}>检索</Button>
      </Space.Compact>
      {lovErr && <LoadErrorAlert title="LOV 检索失败" message={lovErr} onRetry={searchLov} style={{ marginBottom: 6 }} />}
      {lovHits.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4, marginBottom: 8, maxHeight: 180, overflowY: 'auto' }}>
          {lovHits.map((h) => (
            <Checkbox
              key={h.uri}
              checked={!!lovPicked[h.uri]}
              onChange={(e) =>
                setLovPicked((cur) => {
                  const next = { ...cur }
                  if (e.target.checked) next[h.uri] = h
                  else delete next[h.uri]
                  return next
                })
              }
            >
              <span style={{ fontSize: 12 }}>{h.label}</span>
              <Typography.Text type="secondary" style={{ fontSize: 11, marginInlineStart: 6 }}>{h.uri}</Typography.Text>
            </Checkbox>
          ))}
        </div>
      )}
      {Object.keys(lovPicked).length > 0 && (
        <div style={{ marginBottom: 8 }}>
          已选术语：{Object.values(lovPicked).map((t) => <Tag key={t.uri} color="geekblue">{t.label}</Tag>)}
        </div>
      )}

      <div className="onto-sec">
        <span className="onto-sec-title">④ 生成草稿并审查入库</span>
      </div>
      <Space wrap style={{ marginBottom: 8 }}>
        <Button type="primary" disabled={!targetId || !odp} loading={previewLoading} onClick={doPreview}>
          生成扩展草稿并审查预览
        </Button>
        {preview && (
          <Button type="primary" ghost loading={applying} onClick={doApply}>
            应用入库（新版本）
          </Button>
        )}
      </Space>
      {applyErr && <LoadErrorAlert title="OntoExtend 操作失败" message={applyErr} onRetry={() => setApplyErr(null)} style={{ marginBottom: 8 }} />}
      {preview && (
        <Card size="small" style={{ marginBottom: 8 }}>
          <div style={{ fontWeight: 600, marginBottom: 4 }}>审查报告（strategy=merge，REQ-157 底座）</div>
          <Space size={16} wrap>
            <Tag color="green">新增概念 {addedConcepts}</Tag>
            <Tag color="geekblue">新增关系 {addedRelations}</Tag>
            <Tag color={conflictCount > 0 ? 'orange' : 'default'}>冲突/改名 {conflictCount}</Tag>
          </Space>
          {conflictCount > 0 && (
            <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 6 }}>
              存在撞名冲突/改名项——应用后按 merge 策略自动改名并入（前缀避让），详情见资产栏版本 diff。
            </Typography.Paragraph>
          )}
        </Card>
      )}
      {appliedVersion !== null && (
        <Alert type="success" showIcon message={`已并入目标本体新版本（v${appliedVersion}）——strict 质量门禁通过；到「本体资产」查看版本与质量卡。`} />
      )}
      {!targetId && (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="先选择目标本体——OntoExtend 对既有本体做增量扩展，不新建" />
      )}
    </Card>
  )
}
