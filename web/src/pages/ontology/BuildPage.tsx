import { useEffect, useState } from 'react'
import { Alert, Button, Card, Empty, Input, Segmented, Space, Steps, Table, Tabs, Tag, Typography, Upload } from 'antd'
import {
  ApiOutlined,
  CheckCircleOutlined,
  InboxOutlined,
  RightOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { api, ApiError } from '../../api/client'
import type { AiDraftResult, ImportReport, Spec, ValidationError } from '../../api/types'
import { useUI } from '../../store/ui'
import { ERR_COLUMNS } from './shared'
import SpecGraph from './components/SpecGraph'
import OntoChatFlow from './OntoChatFlow'

// ---------------------------------------------------------------------------
// 本体构建（BuildPage，REQ-104 ②）：按构建路径分二级模块（五路径分层标注状态）
//   自定义构建（可用，现有页面主体 S1~S4）| OntoChat 流程（部分可用，REQ-103 模式 A 载体）
//   | semantica 流程（入口卡）| OntoExtend 流程（引导卡）| Open Ontologies 流程（引导+回流）
//   未工程化路径显示引导卡、不做空壳交互（D-O11）
// ---------------------------------------------------------------------------

type BuildPath = 'custom' | 'ontochat' | 'semantica' | 'ontoextend' | 'oo' | 'kb'

const PATHS: { key: BuildPath; label: string; state: 'ok' | 'partial' | 'guide'; desc: string }[] = [
  { key: 'custom', label: '自定义构建', state: 'ok', desc: 'S1 来源 → S2 编辑 → S3 校验 → S4 可视化（七阶段前 4 步）' },
  { key: 'ontochat', label: 'OntoChat 流程', state: 'ok', desc: '对话式 CQ 引导 → 逐轮补全 → 草稿入库（REQ-103 模式 A）' },
  { key: 'kb', label: '由知识库构建', state: 'guide', desc: 'KB chunk→LLM 抽取 / KG→直转（D-O14 第六路径，工程化排期 P2 前段）' },
  { key: 'semantica', label: 'semantica 流程', state: 'guide', desc: '入口卡跳转 semantica 独立栏（D-O10 零侵入不破）' },
  { key: 'ontoextend', label: 'OntoExtend 流程', state: 'guide', desc: '对话式扩展现有本体（引导先行，工程化另行评估）' },
  { key: 'oo', label: 'Open Ontologies 流程', state: 'guide', desc: '双轨引导 + 产物回流（REQ-78 互通后顺畅）' },
]

const STATE_TAG: Record<BuildPath, { color: string; text: string }> = {
  custom: { color: 'green', text: '可用' },
  ontochat: { color: 'green', text: '可用' },
  kb: { color: 'cyan', text: '引导' },
  semantica: { color: 'cyan', text: '引导' },
  ontoextend: { color: 'cyan', text: '引导先行' },
  oo: { color: 'cyan', text: '引导' },
}

const ONTO_BUILD_PATH_KEY = 'eino.onto.buildPath'

function readBuildPath(): BuildPath {
  const v = localStorage.getItem(ONTO_BUILD_PATH_KEY)
  return v === 'ontochat' || v === 'semantica' || v === 'ontoextend' || v === 'oo' ? (v as BuildPath) : 'custom'
}

export default function BuildPage() {
  const [buildPath, setBuildPath] = useState<BuildPath>(readBuildPath)

  const select = (key: BuildPath) => {
    localStorage.setItem(ONTO_BUILD_PATH_KEY, key)
    setBuildPath(key)
  }

  return (
    <div className="work-main">
      <div className="work-head">
        <div className="work-head-text">
          <div className="work-head-title">
            <Typography.Title level={4} style={{ margin: 0 }}>
              本体构建
            </Typography.Title>
            <Tag color="blue" style={{ margin: 0 }}>五条构建路径</Tag>
          </div>
          <p className="work-head-desc">
            同一个本体可以由不同路径建成（学习要点各不相同）；产物统一进入「本体资产」栏管理。
          </p>
        </div>
      </div>

      <div className="onto-engine-nav">
        {PATHS.map((p) => (
          <button
            key={p.key}
            type="button"
            className={`onto-engine-item${buildPath === p.key ? ' active' : ''}`}
            onClick={() => select(p.key)}
          >
            <span className="onto-engine-top">
              <span className="onto-engine-label">{p.label}</span>
              <Tag color={STATE_TAG[p.key].color} style={{ margin: 0, fontSize: 10, lineHeight: '16px', padding: '0 4px' }}>
                {STATE_TAG[p.key].text}
              </Tag>
            </span>
            <span className="onto-engine-desc">{p.desc}</span>
          </button>
        ))}
      </div>

      {buildPath === 'custom' && <CustomFlow />}
      {buildPath === 'ontochat' && <OntoChatFlow onSaved={() => { /* 入库后产物进资产栏；此处留在会话页展示 done 态 */ }} />}
      {buildPath === 'kb' && <KbBuildGuide />}
      {buildPath === 'semantica' && <SemanticaGuide />}
      {buildPath === 'ontoextend' && <OntoExtendGuide />}
      {buildPath === 'oo' && <OoGuide />}
    </div>
  )
}

// ---------------------------------------------------------------------------
// 自定义构建（现有页面主体：S1~S4 四步，S5~S7 卡片移除——运行看运行栏、facade/挂载看方案详情）
// ---------------------------------------------------------------------------

const CUSTOM_STAGES = [
  { key: 's1', title: 'S1 本体来源' },
  { key: 's2', title: 'S2 编辑' },
  { key: 's3', title: 'S3 校验' },
  { key: 's4', title: 'S4 可视化' },
]

function CustomFlow() {
  const [step, setStep] = useState(0)
  const [activeId, setActiveId] = useState<string | null>(null)
  const [spec, setSpec] = useState<Spec | null>(null)
  const [specTick, setSpecTick] = useState(0)

  // 选中本体 → 拉取 Spec（404 视为尚未保存）
  useEffect(() => {
    if (!activeId) {
      setSpec(null)
      return
    }
    let alive = true
    api
      .getSpec(activeId)
      .then((s) => {
        if (alive) setSpec(s)
      })
      .catch(() => {
        if (alive) setSpec(null)
      })
    return () => {
      alive = false
    }
  }, [activeId, specTick])

  const handleCreated = (selectId?: string) => {
    if (selectId) setActiveId(selectId)
    setSpecTick((t) => t + 1)
    if (selectId) setStep(1)
  }

  return (
    <Card className="work-card onto-stage-card" size="small">
      <Steps
        size="small"
        current={step}
        onChange={setStep}
        items={CUSTOM_STAGES.map((s, i) => ({
          key: s.key,
          title: s.title,
          status: i === step ? 'process' : 'wait',
          className: i === step ? 'onto-step-selected' : undefined,
        }))}
        style={{ marginBottom: 14 }}
      />
      {step === 0 && <S1Source onCreated={handleCreated} />}
      {step === 1 && activeId && (
        <S2EditPane
          ontologyId={activeId}
          spec={spec}
          onSaved={() => setSpecTick((t) => t + 1)}
          onNext={() => setStep(2)}
        />
      )}
      {step === 1 && !activeId && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="请先在 S1 创建 / 导入本体" />}
      {step === 2 && activeId && <S3ValidatePane ontologyId={activeId} onNext={() => setStep(3)} />}
      {step === 2 && !activeId && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="请先在 S1 创建 / 导入本体" />}
      {step === 3 && <SpecGraph spec={spec} />}
      {activeId && step >= 1 && (
        <Alert
          type="success"
          showIcon
          style={{ marginTop: 12 }}
          message={`当前工作本体：${activeId}`}
          description="S1~S4 完成后：到「本体资产」栏查看产物与版本，到「本体运行」栏部署为运行方案（S5~S7 已移交）。"
          action={
            <Button
              size="small"
              icon={<RightOutlined />}
              onClick={() => {
                localStorage.setItem('eino.onto.sidebar', 'runtime')
                window.dispatchEvent(new CustomEvent('onto-sidebar-change'))
              }}
            >
              前往本体运行
            </Button>
          }
        />
      )}
    </Card>
  )
}

// ---------------------------------------------------------------------------
// S1 本体来源（导入 / AI 创建 / 内置示例 / 空白 / CSV 灌装）
// ---------------------------------------------------------------------------

function S1Source({ onCreated }: { onCreated: (selectId?: string) => void }) {
  const { showToast } = useUI()
  const [importMode, setImportMode] = useState<'file' | 'paste'>('file')
  const [pasteFilename, setPasteFilename] = useState('')
  const [pasteContent, setPasteContent] = useState('')
  const [report, setReport] = useState<ImportReport | null>(null)
  const [importBusy, setImportBusy] = useState(false)

  const [aiDesc, setAiDesc] = useState('')
  const [aiHint, setAiHint] = useState('')
  const [aiCq, setAiCq] = useState('')
  const [aiBusy, setAiBusy] = useState(false)
  const [aiResult, setAiResult] = useState<AiDraftResult | null>(null)
  const [aiName, setAiName] = useState('')

  const [sampleBusy, setSampleBusy] = useState(false)

  const [blankName, setBlankName] = useState('')
  const [blankDesc, setBlankDesc] = useState('')
  const [blankBusy, setBlankBusy] = useState(false)

  const doImportFile = async (file: File) => {
    setImportBusy(true)
    setReport(null)
    try {
      const r = await api.importOntologyFile(file)
      setReport(r.report)
      showToast(`已导入「${r.ontology.name}」`)
      onCreated(r.ontology.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setImportBusy(false)
    }
  }

  const doImportPaste = async () => {
    if (!pasteContent.trim()) {
      showToast('请粘贴文件内容', 'err')
      return
    }
    setImportBusy(true)
    setReport(null)
    try {
      const r = await api.importOntologyContent(pasteFilename.trim() || 'pasted.ttl', pasteContent)
      setReport(r.report)
      showToast(`已导入「${r.ontology.name}」`)
      onCreated(r.ontology.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setImportBusy(false)
    }
  }

  const doAiDraft = async () => {
    if (!aiDesc.trim()) {
      showToast('请描述本体用途', 'err')
      return
    }
    setAiBusy(true)
    setAiResult(null)
    try {
      // REQ-82：CQ 引导折叠进 extraHint（后端 extraHint 参数已支持）
      const cqs = aiCq
        .split('\n')
        .map((s) => s.trim())
        .filter(Boolean)
      const hintParts: string[] = []
      if (cqs.length) hintParts.push(`能力问题：\n${cqs.map((c, i) => `${i + 1}. ${c}`).join('\n')}`)
      if (aiHint.trim()) hintParts.push(aiHint.trim())
      const r = await api.aiDraftOntology(aiDesc.trim(), hintParts.join('\n\n') || undefined)
      setAiResult(r)
      setAiName(r.spec?.name ?? '')
      showToast('草案已生成，请确认后创建')
    } catch (e: any) {
      if (e instanceof ApiError && e.status === 503) showToast('AI 草案不可用：LLM 未配置（503）', 'err')
      else showToast(e.message, 'err')
    } finally {
      setAiBusy(false)
    }
  }

  const saveAiDraft = async () => {
    if (!aiResult) return
    const nm = aiName.trim() || aiResult.spec?.name || ''
    if (!nm) {
      showToast('请输入本体名称', 'err')
      return
    }
    setAiBusy(true)
    try {
      const created = await api.createOntology({ name: nm, description: aiResult.spec?.description ?? aiDesc })
      await api.saveSpec(created.id, { ...aiResult.spec, name: nm })
      showToast('AI 草案已创建并保存')
      setAiResult(null)
      setAiDesc('')
      setAiHint('')
      setAiCq('')
      onCreated(created.id)
    } catch (e: any) {
      if (e instanceof ApiError && e.validationErrors?.length) showToast(`草案校验未通过：${e.validationErrors[0].message}`, 'err')
      else showToast(e.message, 'err')
    } finally {
      setAiBusy(false)
    }
  }

  const doSeed = async () => {
    setSampleBusy(true)
    try {
      const r = await api.seedSampleOntology()
      if (r.seeded === false) showToast(r.note || '内置示例已存在')
      else showToast('内置示例已创建')
      onCreated(r.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setSampleBusy(false)
    }
  }

  const doBlank = async () => {
    if (!blankName.trim()) {
      showToast('请输入名称', 'err')
      return
    }
    setBlankBusy(true)
    try {
      const o = await api.createOntology({ name: blankName.trim(), description: blankDesc })
      showToast('已创建空白本体')
      setBlankName('')
      setBlankDesc('')
      onCreated(o.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBlankBusy(false)
    }
  }

  const reportAlert = report ? (
    <Alert
      type={report.lossy ? 'warning' : 'success'}
      showIcon
      style={{ marginTop: 12 }}
      message={`导入报告：格式 ${report.format}${report.lossy ? '（有损）' : '（无损）'}`}
      description={
        report.warnings?.length || report.lossy_note ? (
          <ul className="onto-report-list">
            {(report.warnings ?? []).map((w, i) => (
              <li key={i}>{w}</li>
            ))}
            {report.lossy_note && <li>{report.lossy_note}</li>}
          </ul>
        ) : undefined
      }
    />
  ) : null

  return (
    <Tabs
      items={[
        {
          key: 'import',
          label: '导入文件',
          children: (
            <>
              <Segmented
                value={importMode}
                onChange={(v) => setImportMode(v as 'file' | 'paste')}
                options={[
                  { value: 'file', label: '上传文件' },
                  { value: 'paste', label: '粘贴内容' },
                ]}
                style={{ marginBottom: 12 }}
              />
              {importMode === 'file' ? (
                <Upload.Dragger
                  accept=".ttl,.owl,.graphml,.csv,.json,.txt"
                  multiple={false}
                  showUploadList={false}
                  disabled={importBusy}
                  beforeUpload={(file) => {
                    doImportFile(file)
                    return false
                  }}
                >
                  <p className="ant-upload-drag-icon">
                    <InboxOutlined />
                  </p>
                  <p className="ant-upload-text">点击或拖拽文件到此区域导入</p>
                  <p className="ant-upload-hint">支持 TTL / OWL / GraphML / CSV / spec_json，自动嗅探格式</p>
                </Upload.Dragger>
              ) : (
                <Space direction="vertical" style={{ width: '100%' }} size={8}>
                  <Input
                    value={pasteFilename}
                    onChange={(e) => setPasteFilename(e.target.value)}
                    placeholder="文件名（如 sample.ttl，用于格式嗅探）"
                  />
                  <Input.TextArea
                    className="onto-spec-editor"
                    value={pasteContent}
                    onChange={(e) => setPasteContent(e.target.value)}
                    autoSize={{ minRows: 8, maxRows: 18 }}
                    placeholder="粘贴 TTL / OWL / GraphML / CSV / spec_json 内容…"
                    spellCheck={false}
                  />
                  <Button type="primary" loading={importBusy} onClick={doImportPaste}>
                    导入内容
                  </Button>
                </Space>
              )}
              {reportAlert}
            </>
          ),
        },
        {
          key: 'ai',
          label: 'AI 创建',
          children: (
            <>
              <div className="onto-sec" style={{ marginTop: 0 }}>
                <span className="onto-sec-title">能力问题（CQ）引导（可选，每行一个）</span>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  先列 3~5 个本体要回答的问题，会并入生成上下文（REQ-82 / REQ-90 CQ 引导）
                </Typography.Text>
              </div>
              <Input.TextArea
                value={aiCq}
                onChange={(e) => setAiCq(e.target.value)}
                autoSize={{ minRows: 2, maxRows: 5 }}
                placeholder={'本体应能回答的关键问题，每行一个：\n某缺陷源于哪个需求？\n某故障应采取什么维护措施？'}
              />
              <div className="onto-sec">
                <span className="onto-sec-title">领域描述（必填）</span>
              </div>
              <Input.TextArea
                value={aiDesc}
                onChange={(e) => setAiDesc(e.target.value)}
                autoSize={{ minRows: 3, maxRows: 6 }}
                placeholder="用自然语言描述要建模的领域（如：K8s 运维平台的概念、关系与实例）…"
              />
              <Input
                style={{ marginTop: 8 }}
                value={aiHint}
                onChange={(e) => setAiHint(e.target.value)}
                placeholder="额外提示（可选，如：聚焦 Deployment / Service / Pod 三类）"
              />
              <Button
                type="primary"
                icon={<ThunderboltOutlined />}
                loading={aiBusy}
                style={{ marginTop: 10 }}
                onClick={doAiDraft}
              >
                生成草案
              </Button>
              {aiResult && (
                <div className="onto-ai-preview">
                  <Space size={6} wrap>
                    <Tag color="blue" style={{ margin: 0 }}>概念 {aiResult.spec?.concepts?.length ?? 0}</Tag>
                    <Tag color="geekblue" style={{ margin: 0 }}>关系 {aiResult.spec?.relations?.length ?? 0}</Tag>
                    <Tag color="purple" style={{ margin: 0 }}>实例 {aiResult.spec?.instances?.length ?? 0}</Tag>
                    <Tag style={{ margin: 0 }}>rounds {aiResult.rounds ?? '—'}</Tag>
                  </Space>
                  {aiResult.warning && <Alert type="warning" showIcon style={{ marginTop: 8 }} message={aiResult.warning} />}
                  <Space style={{ marginTop: 10 }} size={8}>
                    <Input
                      value={aiName}
                      onChange={(e) => setAiName(e.target.value)}
                      placeholder="本体名称"
                      style={{ width: 260 }}
                    />
                    <Button type="primary" loading={aiBusy} onClick={saveAiDraft}>
                      创建并保存
                    </Button>
                  </Space>
                </div>
              )}
            </>
          ),
        },
        {
          key: 'sample',
          label: '内置示例',
          children: (
            <>
              <p className="onto-detail-empty">内置「K8s 运维平台」示例本体（id=onto_k8s_ops），一键创建即可体验完整流程。</p>
              <Button type="primary" loading={sampleBusy} onClick={doSeed}>
                创建内置示例
              </Button>
            </>
          ),
        },
        {
          key: 'blank',
          label: '空白新建',
          children: (
            <Space direction="vertical" style={{ width: '100%' }} size={8}>
              <Input value={blankName} onChange={(e) => setBlankName(e.target.value)} placeholder="本体名称（必填）" />
              <Input value={blankDesc} onChange={(e) => setBlankDesc(e.target.value)} placeholder="描述（可选）" />
              <Button type="primary" loading={blankBusy} onClick={doBlank}>
                创建空白本体
              </Button>
            </Space>
          ),
        },
      ]}
    />
  )
}

// ---------------------------------------------------------------------------
// S2 编辑（自定义构建内简化版：JSON 编辑 + 保存；版本/产物在资产栏）
// ---------------------------------------------------------------------------

function S2EditPane({ ontologyId, spec, onSaved, onNext }: { ontologyId: string; spec: Spec | null; onSaved: () => void; onNext: () => void }) {
  const { showToast } = useUI()
  const [specText, setSpecText] = useState('')
  const [saving, setSaving] = useState(false)
  const [validationErrors, setValidationErrors] = useState<ValidationError[]>([])
  const [ontoName, setOntoName] = useState('')

  useEffect(() => {
    setSpecText(spec ? JSON.stringify(spec, null, 2) : '')
    setValidationErrors([])
  }, [spec, ontologyId])

  useEffect(() => {
    api
      .getOntology(ontologyId)
      .then((o) => setOntoName(o.name))
      .catch(() => setOntoName(ontologyId))
  }, [ontologyId])

  const save = async () => {
    let parsed: Spec
    try {
      parsed = JSON.parse(specText)
    } catch (e: any) {
      showToast(`JSON 解析失败：${e.message}`, 'err')
      return
    }
    setSaving(true)
    setValidationErrors([])
    try {
      const r = await api.saveSpec(ontologyId, parsed)
      showToast(`Spec 已保存（version ${r.version}）`)
      onSaved()
    } catch (e: any) {
      if (e instanceof ApiError && e.validationErrors?.length) {
        setValidationErrors(e.validationErrors)
        showToast('校验未通过，请修正后重试', 'err')
      } else {
        showToast(e.message, 'err')
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <div className="onto-sec" style={{ marginTop: 0 }}>
        <span className="onto-sec-title">Spec JSON · {ontoName}</span>
        <span className="hit-spacer" />
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {spec ? `概念 ${spec.concepts?.length ?? 0} · 关系 ${spec.relations?.length ?? 0} · 实例 ${spec.instances?.length ?? 0}` : '尚未保存过 Spec'}
        </Typography.Text>
        <Button size="small" type="primary" loading={saving} disabled={!specText} onClick={save}>
          保存 Spec
        </Button>
        <Button size="small" onClick={onNext}>
          下一步：校验
        </Button>
      </div>
      <Input.TextArea
        className="onto-spec-editor"
        value={specText}
        onChange={(e) => setSpecText(e.target.value)}
        autoSize={{ minRows: 16, maxRows: 40 }}
        spellCheck={false}
        placeholder='{ "name": "…", "concepts": [], "relations": [], "instances": [] }'
      />
      {validationErrors.length > 0 && (
        <>
          <Alert type="error" showIcon style={{ marginTop: 12 }} message={`校验未通过（${validationErrors.length} 项）`} />
          <Table<ValidationError>
            rowKey={(r) => `${r.path}::${r.message}`}
            columns={ERR_COLUMNS}
            dataSource={validationErrors}
            pagination={false}
            size="small"
            style={{ marginTop: 8 }}
          />
        </>
      )}
    </>
  )
}

// ---------------------------------------------------------------------------
// S3 校验（自定义构建内简化版）
// ---------------------------------------------------------------------------

function S3ValidatePane({ ontologyId, onNext }: { ontologyId: string; onNext: () => void }) {
  const { showToast } = useUI()
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; errors: ValidationError[] } | null>(null)

  const run = async () => {
    setBusy(true)
    try {
      const r = await api.validateOntology(ontologyId)
      const res = { ok: !!r.ok, errors: r.validation_errors ?? [] }
      setResult(res)
      showToast(res.ok ? '校验通过' : `校验发现 ${res.errors.length} 个问题`, res.ok ? 'ok' : 'err')
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <Space>
        <Button type="primary" icon={<CheckCircleOutlined />} loading={busy} onClick={run}>
          运行校验
        </Button>
        {result && (result.ok ? <Tag color="success" style={{ margin: 0 }}>通过</Tag> : <Tag color="error" style={{ margin: 0 }}>未通过 · {result.errors.length}</Tag>)}
        <Button onClick={onNext}>
          下一步：可视化
        </Button>
      </Space>
      <p className="onto-detail-empty">JSON Schema + 引用完整性校验（保存时同样自动执行；此处可手动复跑）。</p>
      {result?.ok && (
        <Alert type="success" showIcon style={{ marginTop: 4 }} message="校验通过" description="可进入 S4 可视化；运行部署到「本体运行」栏。" />
      )}
      {result && result.errors.length > 0 && (
        <Table<ValidationError>
          rowKey={(r) => `${r.path}::${r.message}`}
          columns={ERR_COLUMNS}
          dataSource={result.errors}
          pagination={false}
          size="small"
          style={{ marginTop: 8 }}
        />
      )}
    </>
  )
}

// ---------------------------------------------------------------------------
// OntoChat 流程：完整交互见 OntoChatFlow.tsx（REQ-103 模式 A 已交付）
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// 由知识库构建（第六路径引导卡，D-O14 / REQ-108；工程化 O13 排期 P2 前段）
// ---------------------------------------------------------------------------

function KbBuildGuide() {
  return (
    <Card className="work-card" size="small">
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="由知识库构建本体——构建栏第六路径（D-O14，引导先行）"
        description="定位：把已有知识库（KB）作为本体构建的数据源——RAG 子模块的 chunk 语料经 LLM 抽取 spec_json（策略 A），GraphRAG 子模块的 KG 实体/关系直转（策略 B），或 KG 作初稿 + LLM 校验补全（策略 C）。与 semantica 流程互为对偶：本路径是「KB→本体」构建方向，semantica 流程是「本体→KG」消费方向。"
      />
      <div className="onto-sec" style={{ marginTop: 0 }}>
        <span className="onto-sec-title">规划的三种抽取策略（04 §3.7）</span>
      </div>
      <ol className="onto-report-list">
        <li><strong>策略 A：chunk → LLM</strong>（可即时上线）——选 KB → 按知识包选 chunk 语料 → CQ 引导（REQ-90）→ 复用 /api/ontology-llm/generate 抽取 spec_json → 校验 → 预览 → 入库</li>
        <li><strong>策略 B：KG → 直转</strong>（P2 后评估）——GraphRAG KG 的 entity→Concept / relation→Relation / claim→Attribute 薄映射层（&lt;300 行），不做抽取；保真度依赖 semantica 抽取质量</li>
        <li><strong>策略 C：混合</strong>（P2）——策略 B 产初稿 → 策略 A 喂 LLM 做校验 + 补全 definition / domain / range</li>
      </ol>
      <Alert type="warning" showIcon style={{ marginTop: 12 }} message="工程化排期" description="入口端点（POST /api/ontologies/build-from-kb、GET /api/kbs/selectable-for-ontology-build、kg-to-spec-json）与独立流程页随 O13（P2 前段）交付；依赖 M6 KB + M14 KB 双子模块（已就绪）。当前可先到「知识库」栏体验 KB 构建与 GraphRAG。" />
    </Card>
  )
}

// ---------------------------------------------------------------------------
// semantica 流程（入口卡，D-O10 零侵入不破）
// ---------------------------------------------------------------------------

function SemanticaGuide() {
  return (
    <Card className="work-card" size="small">
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="semantica 流程——KG 构建 / 图谱查询 / GraphRAG（独立栏承载）"
        description="semantica（MIT，Python）是 AI Agent 的语义层与决策智能层。按 D-O10 零侵入原则，其能力不重收进构建平面：本平台本体导出 TTL → semantica 摄取建库（消费环节）→ GraphRAG 语义问答。"
      />
      <div className="onto-sec" style={{ marginTop: 0 }}>
        <span className="onto-sec-title">与主线边界</span>
      </div>
      <ul className="onto-report-list">
        <li>本体建模与校验：归主线构建平面（本栏）</li>
        <li>KG 建库 / 图谱浏览 / GraphRAG 问答：归 semantica 独立栏</li>
        <li>数据入口：本体资产栏「TTL 导出」→ semantica「数据摄取」</li>
      </ul>
      <Space style={{ marginTop: 12 }}>
        <Button
          type="primary"
          icon={<ApiOutlined />}
          onClick={() => {
            // 顶部导航切页：page 状态机经 URL 同步（store/ui PAGE_PATHS）
            window.location.assign('/semantica')
          }}
        >
          前往 Semantica 独立栏
        </Button>
      </Space>
    </Card>
  )
}

// ---------------------------------------------------------------------------
// OntoExtend 流程（引导卡先行，工程化登记需求池）
// ---------------------------------------------------------------------------

function OntoExtendGuide() {
  return (
    <Card className="work-card" size="small">
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="OntoExtend 流程——对话式扩展现有本体（引导先行，工程化另行评估）"
        description="定位：不新建本体，而是对已有本体做对话式增量扩展（补概念/关系/实例）。工程化 = fork（REQ-83）+ REQ-82 扩展语料组合，增量成本待主人体验引导卡后评估。"
      />
      <div className="onto-sec" style={{ marginTop: 0 }}>
        <span className="onto-sec-title">手工路径（当前可用）</span>
      </div>
      <ol className="onto-report-list">
        <li>「本体资产」栏选中本体 → <strong>Fork 本体</strong>（复制为独立新本体，forked_from 记录来源）</li>
        <li>「自定义构建 → S1 → AI 创建」描述扩展方向（如"在 K8s 运维本体上补充网络策略域"）</li>
        <li>生成草稿 → 预览 → 入库 → 回资产栏合并所需片段到 Fork 本体</li>
      </ol>
      <Alert type="warning" showIcon style={{ marginTop: 12 }} message="工程化排期" description="对话式扩展交互登记需求池（P2/P3 评估）；当前以 Fork + AI 创建组合覆盖核心诉求。" />
    </Card>
  )
}

// ---------------------------------------------------------------------------
// Open Ontologies 流程（双轨引导 + 产物回流）
// ---------------------------------------------------------------------------

function OoGuide() {
  return (
    <Card className="work-card" size="small">
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="Open Ontologies 流程——双轨构建（oo-worker :8092 独立托管）"
        description="open-ontologies（Rust 单二进制，MIT）：RDFS/OWL-RL 物化推理、SHACL 校验、不一致检查、变更影响分析、多源数据装载。其本体为 TTL 文件集（worker data-dir 自管），不进主线 spec_json 体系。"
      />
      <div className="onto-sec" style={{ marginTop: 0 }}>
        <span className="onto-sec-title">学习要点</span>
      </div>
      <ul className="onto-report-list">
        <li>物化推理与主线「显式重载」的差异：oo 建库即物化，主线查询时精确匹配</li>
        <li>SHACL 约束建模 vs 主线 JSON Schema + 引用完整性校验</li>
        <li>39 个 onto_* 工具 vs 主线 facade 4 个固定签名工具</li>
      </ul>
      <div className="onto-sec">
        <span className="onto-sec-title">产物回流（P1 手工）</span>
      </div>
      <ol className="onto-report-list">
        <li>oo 工作台导出 TTL</li>
        <li>「自定义构建 → S1 → 导入文件」上传该 TTL（有损导入，映射规则见导入报告）</li>
        <li>REQ-78 双轨 TTL 互通（P2）后自动化</li>
      </ol>
      <Space style={{ marginTop: 12 }}>
        <Button type="primary" href="/api/oo/" target="_blank" rel="noreferrer">
          前往 Open Ontologies 工作台
        </Button>
        <Button icon={<RightOutlined />} onClick={() => { localStorage.setItem('eino.onto.sidebar', 'runtime'); window.dispatchEvent(new CustomEvent('onto-sidebar-change')) }}>
          查看运行栏 oo 引导页
        </Button>
      </Space>
    </Card>
  )
}

// ---------------------------------------------------------------------------
// CSV 灌装（REQ-96 P2a 同名映射 + P2b 映射向导）在资产栏「CSV 灌装」页签（CsvIngestPane）
// ---------------------------------------------------------------------------
