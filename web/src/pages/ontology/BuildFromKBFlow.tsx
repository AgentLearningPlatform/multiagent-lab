import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Empty, Input, Radio, Segmented, Select, Space, Steps, Table, Tag, Typography, Upload } from 'antd'
import { CheckCircleOutlined, DatabaseOutlined, ReloadOutlined, RightOutlined, ThunderboltOutlined, UploadOutlined } from '@ant-design/icons'
import { api, ApiError } from '../../api/client'
import type { OntoBuildSelectableKB, OntoBuildResult, ValidationError } from '../../api/types'
import { useUI } from '../../store/ui'
import { ERR_COLUMNS } from './shared'
import LoadErrorAlert from '../../components/LoadErrorAlert'

// ---------------------------------------------------------------------------
// 由知识库构建本体——独立流程页（O13，D-O14/REQ-108，04 §3.7）：
//   KB 选择器（mode 徽标 + chunk/KG 规模）→ 抽取策略（A chunk→LLM / B KG→直转 / C 混合）
//   → CQ 引导（REQ-90：auto / custom / skip）→ 生成草稿（spec_json + 校验报告）→ 预览调优 → 入库
// 与自定义构建 S1~S4 互不共用页面；产物入库走构建平面（POST /api/ontologies + PUT spec）。
// ---------------------------------------------------------------------------

const STEPS = [
  { key: 'kb', title: '选择知识库' },
  { key: 'config', title: '策略与 CQ' },
  { key: 'preview', title: '生成与预览' },
  { key: 'done', title: '入库' },
]

const MODE_TAG: Record<string, { color: string; text: string }> = {
  rag: { color: 'blue', text: 'RAG' },
  graphrag: { color: 'purple', text: 'GraphRAG' },
}

const STRATEGY_HINT: Record<string, string> = {
  'chunk-llm': '策略 A：chunk 语料 → LLM 抽取 spec_json（复用 REQ-82，可即时上线；需模型连接）',
  'kg-direct': '策略 B：GraphRAG KG 直转（entity→Concept / relation→Relation 薄映射，不做抽取；保真度依赖抽取质量）',
  hybrid: '策略 C：混合——KG 作初稿 + LLM 校验补全 definition / 缺失关系',
}

function KbBuildFlow() {
  const { showToast } = useUI()
  const [step, setStep] = useState(0)
  const [kbs, setKbs] = useState<OntoBuildSelectableKB[]>([])
  const [kbLoading, setKbLoading] = useState(false)
  const [kbId, setKbId] = useState<string | null>(null)
  const [buildingKG, setBuildingKG] = useState<string | null>(null)

  const [strategy, setStrategy] = useState<'chunk-llm' | 'kg-direct' | 'hybrid'>('chunk-llm')
  const [cqMode, setCqMode] = useState<'auto' | 'custom' | 'skip'>('auto')
  const [cqText, setCqText] = useState('')

  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<OntoBuildResult | null>(null)
  const [specText, setSpecText] = useState('')
  const [ontoName, setOntoName] = useState('')
  const [saving, setSaving] = useState(false)
  const [savedId, setSavedId] = useState<string | null>(null)

  const selected = useMemo(() => kbs.find((k) => k.id === kbId) ?? null, [kbs, kbId])

  const loadKBs = (keep?: string | null) => {
    setKbLoading(true)
    api
      .selectableKBsForBuild()
      .then((list) => {
        setKbs(list)
        if (keep && list.some((k) => k.id === keep)) setKbId(keep)
      })
      .catch((e) => showToast(e.message, 'err'))
      .finally(() => setKbLoading(false))
  }
  useEffect(() => loadKBs(), [])

  const buildKG = async (id: string) => {
    setBuildingKG(id)
    try {
      const r = await api.chunksToKG(id)
      if (r.graphrag?.degraded) showToast(`KG 构建降级：${r.graphrag.error ?? 'worker 不可达'}`, 'err')
      else showToast(`KG 已构建：${r.graphrag?.entities ?? 0} 实体 / ${r.graphrag?.relationships ?? 0} 关系`)
      loadKBs(id)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBuildingKG(null)
    }
  }

  const generate = async () => {
    if (!kbId) {
      showToast('请先选择知识库', 'err')
      return
    }
    const cqs = cqText
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean)
    if (cqMode === 'custom' && cqs.length === 0) {
      showToast('自定义 CQ 模式需至少输入一个问题', 'err')
      return
    }
    setBusy(true)
    setResult(null)
    try {
      const r = await api.buildFromKB({
        kb_id: kbId,
        strategy,
        cq_mode: cqMode,
        custom_cqs: cqMode === 'custom' ? cqs : undefined,
      })
      setResult(r)
      setSpecText(JSON.stringify(r.spec_json, null, 2))
      setOntoName(r.spec_json?.name ?? '')
      setStep(2)
      showToast(r.validation_report?.ok ? '草稿已生成且校验通过' : '草稿已生成，但校验存在问题，请检查或修改')
    } catch (e: any) {
      if (e instanceof ApiError && e.status === 503) showToast('生成失败：LLM 未配置（503），请到「设置-模型管理」配置', 'err')
      else showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    let spec
    try {
      spec = JSON.parse(specText)
    } catch (e: any) {
      showToast(`JSON 解析失败：${e.message}`, 'err')
      return
    }
    const nm = ontoName.trim() || spec?.name || ''
    if (!nm) {
      showToast('请输入本体名称', 'err')
      return
    }
    setSaving(true)
    try {
      const created = await api.createOntology({ name: nm, description: spec?.description ?? `由知识库「${result?.kb_name}」构建（O13 策略 ${result?.strategy}）` })
      await api.saveSpec(created.id, { ...spec, name: nm })
      showToast('已入库到本体资产')
      setSavedId(created.id)
      setStep(3)
    } catch (e: any) {
      if (e instanceof ApiError && e.validationErrors?.length) showToast(`构建平面校验未通过：${e.validationErrors[0].message}`, 'err')
      else showToast(e.message, 'err')
    } finally {
      setSaving(false)
    }
  }

  const report = result?.validation_report
  const specTags = result && (
    <Space size={6} wrap>
      <Tag color="blue" style={{ margin: 0 }}>概念 {result.spec_json?.concepts?.length ?? 0}</Tag>
      <Tag color="geekblue" style={{ margin: 0 }}>关系 {result.spec_json?.relations?.length ?? 0}</Tag>
      <Tag color="purple" style={{ margin: 0 }}>实例 {result.spec_json?.instances?.length ?? 0}</Tag>
      <Tag style={{ margin: 0 }}>rounds {result.rounds}</Tag>
      {result.method && <Tag color="cyan" style={{ margin: 0 }}>KG method {result.method}</Tag>}
      {result.cqs?.length ? <Tag color="orange" style={{ margin: 0 }}>CQ ×{result.cqs.length}</Tag> : null}
      {result.truncated && <Tag color="volcano" style={{ margin: 0 }}>语料已截断</Tag>}
    </Space>
  )

  return (
    <Card className="work-card onto-stage-card" size="small">
      <Steps
        size="small"
        current={step}
        onChange={setStep}
        items={STEPS.map((s, i) => ({ key: s.key, title: s.title, status: i === step ? 'process' : 'wait' }))}
        style={{ marginBottom: 14 }}
      />

      {step === 0 && (
        <>
          <div className="onto-sec" style={{ marginTop: 0 }}>
            <span className="onto-sec-title">选择作为数据源的知识库</span>
            <Button size="small" icon={<ReloadOutlined />} loading={kbLoading} onClick={() => loadKBs(kbId)}>
              刷新
            </Button>
          </div>
          {kbs.length === 0 && !kbLoading && (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无知识库；请先到「知识库」栏创建并导入文档" />
          )}
          <div className="onto-engine-nav" style={{ marginTop: 8 }}>
            {kbs.map((k) => {
              const mt = MODE_TAG[k.mode ?? 'rag']
              return (
                <button
                  key={k.id}
                  type="button"
                  className={`onto-engine-item${kbId === k.id ? ' active' : ''}`}
                  onClick={() => setKbId(k.id)}
                >
                  <span className="onto-engine-top">
                    <span className="onto-engine-label">{k.name}</span>
                    <Tag color={mt.color} style={{ margin: 0, fontSize: 10, lineHeight: '16px', padding: '0 4px' }}>
                      {mt.text}
                    </Tag>
                  </span>
                  <span className="onto-engine-desc">
                    文档 {k.doc_count} · chunk {k.chunk_count}
                    {k.mode === 'graphrag' || k.kg_ready ? ` · KG ${k.kg_entities} 实体 / ${k.kg_relationships} 关系` : ' · 无 KG'}
                  </span>
                  <span className="onto-engine-desc">
                    {(k.chunk_count ?? 0) === 0
                      ? '暂无语料（先到知识库栏导入文档）'
                      : !k.kg_ready
                        ? '策略 A 可用；B/C 需先构建 KG'
                        : '策略 A / B / C 均可用'}
                  </span>
                </button>
              )
            })}
          </div>
          {selected && !selected.kg_ready && (selected.chunk_count ?? 0) > 0 && (
            <Alert
              type="info"
              showIcon
              style={{ marginTop: 12 }}
              title={`「${selected.name}」尚无 KG`}
              description="策略 B / C 需要先抽取 KG。点击下方按钮用自研抽取器重建（REQ-98 LLM 主路径 + 规则回退，D-O15；graphrag 模式导入文档时也会自动构建）。"
              action={
                <Button size="small" icon={<DatabaseOutlined />} loading={buildingKG === selected.id} onClick={() => buildKG(selected.id)}>
                  构建 KG
                </Button>
              }
            />
          )}
          <Space style={{ marginTop: 12 }}>
            <Button type="primary" disabled={!kbId || (selected?.chunk_count ?? 0) === 0} onClick={() => setStep(1)}>
              下一步：策略与 CQ
            </Button>
          </Space>
        </>
      )}

      {step === 1 && selected && (
        <>
          <div className="onto-sec" style={{ marginTop: 0 }}>
            <span className="onto-sec-title">抽取策略（04 §3.7）</span>
          </div>
          <Segmented
            value={strategy}
            onChange={(v) => setStrategy(v as typeof strategy)}
            options={[
              { value: 'chunk-llm', label: 'A · chunk→LLM' },
              { value: 'kg-direct', label: 'B · KG→直转', disabled: !selected.kg_ready },
              { value: 'hybrid', label: 'C · 混合', disabled: !selected.kg_ready },
            ]}
          />
          <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 8, marginBottom: 0 }}>
            {STRATEGY_HINT[strategy]}
            {!selected.kg_ready && strategy !== 'chunk-llm' && '（当前 KB 无 KG，先回上一步构建）'}
          </Typography.Paragraph>

          <div className="onto-sec">
            <span className="onto-sec-title">CQ 引导模式（REQ-90）</span>
          </div>
          <Radio.Group value={cqMode} onChange={(e) => setCqMode(e.target.value)}>
            <Radio value="auto">自动生成 3~5 个能力问题</Radio>
            <Radio value="custom">用户自定义</Radio>
            <Radio value="skip">跳过</Radio>
          </Radio.Group>
          {cqMode === 'custom' && (
            <Input.TextArea
              style={{ marginTop: 8 }}
              value={cqText}
              onChange={(e) => setCqText(e.target.value)}
              autoSize={{ minRows: 2, maxRows: 5 }}
              placeholder={'本体应能回答的关键问题，每行一个：\n某故障源于什么原因？\n某原因对应什么维护措施？'}
            />
          )}
          <Space style={{ marginTop: 14 }}>
            <Button onClick={() => setStep(0)}>上一步</Button>
            <Button type="primary" icon={<ThunderboltOutlined />} loading={busy} onClick={generate}>
              生成草稿
            </Button>
          </Space>
        </>
      )}

      {step === 1 && !selected && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="请先选择知识库" />}

      {step === 2 && result && (
        <>
          {specTags}
          {result.warnings?.map((w, i) => (
            <Alert key={i} type="warning" showIcon style={{ marginTop: 8 }} title={w} />
          ))}
          {report?.ok ? (
            <Alert type="success" showIcon style={{ marginTop: 8 }} title="本地结构校验通过（入库时构建平面仍会做权威校验）" />
          ) : (
            <>
              <Alert type="error" showIcon style={{ marginTop: 8 }} title={`校验未通过（${report?.errors.length ?? 0} 项）——可直接修改下方 JSON 后入库，或重新生成`} />
              <Table<ValidationError>
                rowKey={(r) => `${r.path}::${r.message}`}
                columns={ERR_COLUMNS} scroll={{ x: 'max-content' }}
                dataSource={report?.errors ?? []}
                pagination={false}
                size="small"
                style={{ marginTop: 8 }}
              />
            </>
          )}
          {result.cqs?.length ? (
            <Alert
              type="info"
              showIcon
              style={{ marginTop: 8 }}
              title={`能力问题（CQ，REQ-90 ${result.cq_mode === 'auto' ? '自动生成' : '自定义'}）`}
              description={
                <ul className="onto-report-list" style={{ margin: 0 }}>
                  {result.cqs.map((c, i) => (
                    <li key={i}>{c}</li>
                  ))}
                </ul>
              }
            />
          ) : null}
          <Input style={{ marginTop: 10 }} value={ontoName} onChange={(e) => setOntoName(e.target.value)} placeholder="本体名称" />
          <Input.TextArea
            className="onto-spec-editor"
            style={{ marginTop: 8 }}
            value={specText}
            onChange={(e) => setSpecText(e.target.value)}
            autoSize={{ minRows: 14, maxRows: 34 }}
            spellCheck={false}
          />
          <Space style={{ marginTop: 10 }}>
            <Button onClick={() => { setResult(null); setStep(1) }}>上一步（重新配置）</Button>
            <Button icon={<ThunderboltOutlined />} loading={busy} onClick={generate}>
              重新生成
            </Button>
            <Button type="primary" icon={<CheckCircleOutlined />} loading={saving} onClick={save}>
              创建并保存到本体资产
            </Button>
          </Space>
        </>
      )}

      {step === 2 && !result && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚未生成草稿" />}

      {step === 3 && savedId && (
        <Alert
          type="success"
          showIcon
          title={`已入库：${ontoName || savedId}`}
          description="到「本体资产」栏查看产物与版本；到「本体运行」栏可部署为运行方案（如 SPARQL 型方案后可在工作台验证本体命中）。"
          action={
            <Button
              size="small"
              icon={<RightOutlined />}
              onClick={() => {
                localStorage.setItem('eino.onto.sidebar', 'assets')
                window.dispatchEvent(new CustomEvent('onto-sidebar-change'))
              }}
            >
              前往本体资产
            </Button>
          }
        />
      )}
    </Card>
  )
}

export default KbBuildFlow

// ---------------------------------------------------------------------------
// M-O14 P2⑤（REQ-171 P2/23 号 §7.1 借鉴）：结构化数据映射（CSV/JSON → 本体骨架）。
// 独立于 KB 选择——规则推导（首列→实例名/主概念，余列→属性+类型推断），可选目标本体
// 概念命中标注；预览映射报告与骨架 → 一键创建为新本体（走既有 create+saveSpec+校验）。
// 关系推导不做规则臆断（后端诚实注记指向 REQ-82 LLM 加工）。
// ---------------------------------------------------------------------------
export function StructuredFlow() {
  const { showToast } = useUI()
  const [file, setFile] = useState<{ filename: string; content: string } | null>(null)
  const [targetId, setTargetId] = useState<string | undefined>(undefined)
  const [ontos, setOntos] = useState<{ id: string; name: string }[]>([])
  const [result, setResult] = useState<Awaited<ReturnType<typeof api.buildFromStructured>> | null>(null)
  const [loading, setLoading] = useState(false)
  const [creating, setCreating] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    api
      .listOntologies()
      .then((ls: any) => setOntos(Array.isArray(ls) ? ls : []))
      .catch(() => setOntos([]))
  }, [])

  const infer = async () => {
    if (!file) return
    setLoading(true)
    setErr(null)
    setResult(null)
    try {
      const r = await api.buildFromStructured({ filename: file.filename, content: file.content, target_ontology_id: targetId })
      setResult(r)
    } catch (e: any) {
      setErr(e?.message ?? '推导失败')
    } finally {
      setLoading(false)
    }
  }

  const createAsOntology = async () => {
    if (!result?.draft_spec) return
    setCreating(true)
    try {
      const draft = result.draft_spec as { name: string; description?: string; concepts: unknown[]; relations: unknown[]; instances: unknown[] }
      const created = await api.createOntology({ name: `${draft.name}-${new Date().toLocaleDateString()}`, description: draft.description ?? '由结构化数据映射推导（M-O14 P2⑤）' })
      await api.saveSpec(created.id, draft as any)
      showToast(`已创建新本体「${created.name ?? created.id}」（骨架含 ${draft.concepts.length} 概念 / ${draft.instances.length} 实例）——到「本体资产」查看与编辑`)
    } catch (e: any) {
      showToast(e?.message ?? '创建失败', 'err')
    } finally {
      setCreating(false)
    }
  }

  return (
    <Card size="small" style={{ marginTop: 12 }}>
      <div className="onto-sec" style={{ marginTop: 0 }}>
        <span className="onto-sec-title">结构化数据映射（CSV/JSON → 本体骨架，M-O14 P2⑤）</span>
      </div>
      <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
        规则推导：首列→实例名（主概念），其余列→属性（类型推断）；可选对目标本体做概念命中标注。关系推导需语义判断——建议在骨架上用「AI 创建」继续加工。
      </Typography.Paragraph>
      <Space wrap style={{ marginBottom: 8 }}>
        <Upload maxCount={1} accept=".csv,.json" showUploadList={false} beforeUpload={() => false} onChange={({ fileList }) => {
          const raw = (fileList[0] as any)?.originFileObj
          if (!raw) return
          const rd = new FileReader()
          rd.onload = () => {
            setFile({ filename: raw.name, content: String(rd.result ?? '') })
            setResult(null)
          }
          rd.readAsText(raw)
        }}>
          <Button icon={<UploadOutlined />}>选择 CSV/JSON 文件</Button>
        </Upload>
        <Select
          allowClear
          showSearch
          optionFilterProp="label"
          style={{ width: 240 }}
          placeholder="可选：目标本体（概念命中标注）"
          value={targetId}
          onChange={setTargetId}
          options={ontos.map((o) => ({ value: o.id, label: o.name }))}
        />
        <Button type="primary" disabled={!file} loading={loading} onClick={infer}>
          推导映射与骨架
        </Button>
        {result && (
          <Button type="primary" ghost loading={creating} onClick={createAsOntology}>
            创建为新本体
          </Button>
        )}
      </Space>
      {file && (
        <div style={{ marginBottom: 8 }}>
          <Tag color="geekblue">{file.filename}</Tag>
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>{file.content.length} 字符</Typography.Text>
        </div>
      )}
      {err && <LoadErrorAlert title="结构化推导失败" message={err} onRetry={() => setErr(null)} style={{ marginBottom: 8 }} />}
      {result && (
        <>
          <Space size={12} wrap style={{ marginBottom: 6 }}>
            <Tag color="processing">源 {result.source_kind.toUpperCase()}</Tag>
            <Tag color="purple">主概念 {result.main_concept}</Tag>
            <Tag>{result.mapping.length} 列</Tag>
            <Tag>{result.draft_spec ? (result.draft_spec as any).instances.length : 0} 实例（采样≤200）</Tag>
          </Space>
          <table className="onto-report-table" style={{ width: '100%', fontSize: 12, marginBottom: 6 }}>
            <thead>
              <tr><th>列</th><th>角色</th><th>推断类型</th><th>样例</th><th>目标本体命中</th></tr>
            </thead>
            <tbody>
              {result.mapping.map((m) => (
                <tr key={m.column}>
                  <td>{m.column}</td>
                  <td>{m.role === 'instance-name' ? '实例名' : '属性'}</td>
                  <td>{m.infer_type}</td>
                  <td>{m.sample || '—'}</td>
                  <td>{m.matched_concepts?.length ? <Tag color="green" style={{ margin: 0 }}>{m.matched_concepts.join(', ')}</Tag> : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {result.notes.map((n, i) => (
            <Typography.Paragraph key={i} type="secondary" style={{ fontSize: 11, marginBottom: 2 }}>· {n}</Typography.Paragraph>
          ))}
        </>
      )}
    </Card>
  )
}
