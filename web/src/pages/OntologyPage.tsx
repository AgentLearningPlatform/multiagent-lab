import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Badge,
  Button,
  Card,
  Collapse,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Result,
  Segmented,
  Select,
  Space,
  Spin,
  Splitter,
  Steps,
  Table,
  Tabs,
  Tag,
  Tooltip,
  Typography,
  Upload,
} from 'antd'
import type { BadgeProps } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  ApiOutlined,
  CheckCircleOutlined,
  CodeOutlined,
  CopyOutlined,
  DeleteOutlined,
  EditOutlined,
  InboxOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { api, ApiError } from '../api/client'
import type {
  AiDraftResult,
  Conversation,
  ImportReport,
  LearningExample,
  Ontology,
  RuntimeProfile,
  Spec,
  SpecConcept,
  TraceEntry,
  ValidationError,
  VersionMeta,
  VersionsResponse,
} from '../api/types'
import { useUI } from '../store/ui'

// ---------------------------------------------------------------------------
// 常量与派生规则
// ---------------------------------------------------------------------------

/** 七阶段定义：key / 简称 / 可用路径徽标（D-O7；S1 三条路径，其余内置） */
const STAGE_DEFS: { key: string; short: string; modes: { t: string; c: string }[] }[] = [
  { key: 's1', short: '本体来源', modes: [{ t: '内置示例', c: 'default' }, { t: 'AI 创建', c: 'blue' }, { t: '导入+手动', c: 'purple' }] },
  { key: 's2', short: '编辑', modes: [{ t: '内置', c: 'default' }] },
  { key: 's3', short: '校验', modes: [{ t: '内置', c: 'default' }] },
  { key: 's4', short: '可视化', modes: [{ t: '内置', c: 'default' }] },
  { key: 's5', short: '运行方式', modes: [{ t: '内置', c: 'default' }] },
  { key: 's6', short: '对外暴露', modes: [{ t: '内置', c: 'default' }] },
  { key: 's7', short: '对接智能体', modes: [{ t: '内置', c: 'default' }] },
]

interface ValidationState {
  ok: boolean
  errors: ValidationError[]
}

/**
 * 阶段完成度派生（诚实规则，不编造 n/7）：
 *  S1 创建即完成；S2/S4 = 有概念（n_concepts>0，或当前已加载 spec 有概念）；
 *  S3 = 最近一次校验 ok（缓存于 state）；S5/S6 = 存在 status=running 且 ontology_ids 含本体的方案；
 *  S7 为信息展示，不参与计数（始终 false）。
 */
function stageDoneFlags(
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

/** 左栏状态派生：运行中（已挂载 running 方案）/ 已建（有 Spec）/ 空（无 Spec） */
function ontoStatus(o: Ontology, profiles: RuntimeProfile[]): { color: string; text: string } {
  const running = profiles.some((p) => p.status === 'running' && (p.ontology_ids ?? []).includes(o.id))
  if (running) return { color: 'green', text: '运行中' }
  if ((o.n_concepts ?? 0) > 0) return { color: 'blue', text: '已建' }
  return { color: 'default', text: '空' }
}

const PROFILE_BADGE: Record<RuntimeProfile['status'], { status: BadgeProps['status']; text: string }> = {
  created: { status: 'default', text: '已创建' },
  starting: { status: 'processing', text: '启动中' },
  running: { status: 'success', text: '运行中' },
  stopped: { status: 'default', text: '已停止' },
  error: { status: 'error', text: '错误' },
}

const ERR_COLUMNS: ColumnsType<ValidationError> = [
  { title: '路径', dataIndex: 'path', width: 260, render: (v: string) => <Typography.Text code style={{ fontSize: 12 }}>{v}</Typography.Text> },
  { title: '问题', dataIndex: 'message' },
]

const ONTO_TOOLS = [
  { name: 'onto_get_concept', desc: '按名称取概念（label / definition / 父子）', args: 'ontology_id, concept' },
  { name: 'onto_get_instance', desc: '按名称取实例（concept / attributes / relations）', args: 'ontology_id, instance' },
  { name: 'onto_list_instances', desc: '按概念列实例', args: 'ontology_id, concept（可选）' },
  { name: 'onto_neighbors', desc: '取概念 / 实例的邻接关系', args: 'ontology_id, node' },
]
const TOOL_COLUMNS: ColumnsType<(typeof ONTO_TOOLS)[number]> = [
  { title: '工具', dataIndex: 'name', width: 220, render: (v: string) => <Typography.Text code style={{ fontSize: 12 }}>{v}</Typography.Text> },
  { title: '说明', dataIndex: 'desc' },
  { title: '必填入参', dataIndex: 'args', width: 260, render: (v: string) => <Typography.Text type="secondary" style={{ fontSize: 12 }}>{v}</Typography.Text> },
]

function emptySpec(name: string): Spec {
  return { name, description: '', concepts: [], relations: [], instances: [] }
}

function truncate(s: string, n: number): string {
  return s.length > n ? `${s.slice(0, n - 1)}…` : s
}

function StageDots({ flags }: { flags: boolean[] }) {
  const ready = flags.slice(0, 6).filter(Boolean).length
  return (
    <span className="onto-dots" title={`S1~S6 已就绪 ${ready}/6（S7 对接智能体为信息展示，不计入完成度）`}>
      {flags.map((done, i) => (
        <i key={i} className={`onto-dot${done ? ' on' : ''}${i === 6 ? ' info' : ''}`} />
      ))}
    </span>
  )
}

// ---------------------------------------------------------------------------
// 主页面
// ---------------------------------------------------------------------------

/**
 * 本体视图（原型 06 §3.3 / 04 文档 D-O7）：S1~S7 七阶段工具链工作台。
 * 数据经双反代同源提供——构建平面 :8091（/api/ontologies*）、运行平面 :8090（/api/runtime-profiles*）。
 */
export default function OntologyPage() {
  const { showToast } = useUI()
  const [ontos, setOntos] = useState<Ontology[]>([])
  const [profiles, setProfiles] = useState<RuntimeProfile[]>([])
  const [listErr, setListErr] = useState<string | null>(null)
  const [profilesErr, setProfilesErr] = useState(false)
  const [activeId, setActiveId] = useState<string | null>(null)

  const [spec, setSpec] = useState<Spec | null>(null)
  const [specErr, setSpecErr] = useState<string | null>(null)
  const [specLoading, setSpecLoading] = useState(false)
  const [specTick, setSpecTick] = useState(0)

  const [validations, setValidations] = useState<Record<string, ValidationState>>({})
  const [step, setStep] = useState(0)
  const [renameOpen, setRenameOpen] = useState(false)

  const reloadOntos = (selectId?: string) => {
    api
      .listOntologies()
      .then((ls) => {
        setOntos(ls)
        setListErr(null)
        setActiveId((cur) => {
          if (selectId && ls.some((o) => o.id === selectId)) return selectId
          if (cur && ls.some((o) => o.id === cur)) return cur
          return ls[0]?.id ?? null
        })
      })
      .catch((e: any) => {
        setOntos([])
        setListErr(e?.message ?? '加载失败')
      })
  }

  const reloadProfiles = () => {
    api
      .listRuntimeProfiles()
      .then((ps) => {
        setProfiles(ps)
        setProfilesErr(false)
      })
      .catch(() => {
        setProfiles([])
        setProfilesErr(true)
      })
  }

  useEffect(() => {
    reloadOntos()
    reloadProfiles()
  }, [])

  // 选中本体 → 拉取 Spec（404 视为尚未保存，不算错误）
  useEffect(() => {
    if (!activeId) {
      setSpec(null)
      setSpecErr(null)
      return
    }
    let alive = true
    setSpecLoading(true)
    setSpecErr(null)
    api
      .getSpec(activeId)
      .then((s) => {
        if (alive) setSpec(s)
      })
      .catch((e: any) => {
        if (!alive) return
        setSpec(null)
        if (!(e instanceof ApiError && e.status === 404)) setSpecErr(e?.message ?? '加载失败')
      })
      .finally(() => {
        if (alive) setSpecLoading(false)
      })
    return () => {
      alive = false
    }
  }, [activeId, specTick])

  const active = useMemo(() => ontos.find((o) => o.id === activeId) ?? null, [ontos, activeId])
  const validation = activeId ? validations[activeId] : undefined
  const flags = useMemo(
    () => (active ? stageDoneFlags(active, validation, profiles, spec, true) : []),
    [active, validation, profiles, spec],
  )

  const handleCreated = (selectId?: string) => {
    reloadOntos(selectId)
    reloadProfiles()
  }

  const removeActive = async () => {
    if (!active) return
    try {
      await api.deleteOntology(active.id)
      showToast('已删除本体')
      setActiveId(null)
      reloadOntos()
      reloadProfiles()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  return (
    <Splitter
      className="main sidebar-splitter"
      onResizeEnd={(sizes) => localStorage.setItem('eino.sidebar.width', String(Math.round(sizes[0])))}
    >
      <Splitter.Panel
        defaultSize={Number(localStorage.getItem('eino.sidebar.width')) || 280}
        min={220}
        max={480}
        className="sidebar-panel"
      >
        <aside className="sidebar">
          <div className="side-head">
            <span className="side-title">本体流水线</span>
            <span className="side-count">{ontos.length}</span>
          </div>
          <div className="side-actions">
            <Button block icon={<PlusOutlined />} onClick={() => { setActiveId(null); setStep(0) }}>
              新建 / 导入本体
            </Button>
          </div>
          <div className="side-list">
            {ontos.map((o) => {
              const isActive = o.id === activeId
              const f = stageDoneFlags(o, validations[o.id], profiles, isActive ? spec : null, isActive)
              const st = ontoStatus(o, profiles)
              const ready = f.slice(0, 6).filter(Boolean).length
              return (
                <div key={o.id} className={`side-item${isActive ? ' active' : ''}`} onClick={() => setActiveId(o.id)}>
                  <div className="side-item-top">
                    <span className="side-item-name" title={o.name}>
                      {o.name}
                    </span>
                    <Tag color={st.color} style={{ margin: 0 }}>
                      {st.text}
                    </Tag>
                  </div>
                  <div className="side-item-meta">
                    <StageDots flags={f} />
                    <span>{ready}/6 就绪</span>
                    <span className="dot">·</span>
                    <span>v{o.version ?? '—'}</span>
                  </div>
                </div>
              )
            })}
            {ontos.length === 0 && <div className="empty-hint">{listErr ? '本体平面未就绪' : '暂无本体，点击上方新建 / 导入'}</div>}
          </div>
        </aside>
      </Splitter.Panel>

      <Splitter.Panel className="content-panel">
        <div className="work-main">
          {listErr ? (
            <div className="work-empty">
              <Result
                status="warning"
                title="本体平面服务未启动（BUILD_SVC_URL/:8091、RUNTIME_MGR_URL/:8090）"
                subTitle={listErr}
                extra={<Button onClick={() => reloadOntos()}>重试</Button>}
              />
            </div>
          ) : !active ? (
            <>
              <div className="work-head">
                <div className="work-head-text">
                  <div className="work-head-title">
                    <Typography.Title level={4} style={{ margin: 0 }}>
                      新建本体
                    </Typography.Title>
                    <Tag color="purple" style={{ margin: 0 }}>
                      S1 本体来源
                    </Tag>
                  </div>
                  <p className="work-head-desc">
                    选择导入文件 / AI 创建 / 内置示例 / 空白新建；创建后进入 S1~S7 七阶段工具链（来源 → 编辑 → 校验 → 可视化 → 运行方式 → 对外暴露 → 对接智能体）。
                  </p>
                </div>
              </div>
              <Card className="work-card onto-stage-card" size="small" title="S1 本体来源">
                <S1Source activeOntology={null} onChanged={handleCreated} />
              </Card>
            </>
          ) : (
            <>
              <div className="work-head">
                <div className="work-head-text">
                  <div className="work-head-title">
                    <Typography.Title level={4} style={{ margin: 0 }}>
                      {active.name}
                    </Typography.Title>
                    <Tag color="geekblue" style={{ margin: 0 }}>
                      v{active.version ?? '—'}
                    </Tag>
                    <Tag style={{ margin: 0 }}>概念 {active.n_concepts ?? 0}</Tag>
                    <Tag style={{ margin: 0 }}>关系 {active.n_relations ?? 0}</Tag>
                    <Tag style={{ margin: 0 }}>实例 {active.n_instances ?? 0}</Tag>
                  </div>
                  <p className="work-head-desc">{active.description || '未填写描述'}</p>
                </div>
                <Space>
                  <Button icon={<EditOutlined />} onClick={() => setRenameOpen(true)}>
                    重命名
                  </Button>
                  <Popconfirm
                    title={`删除本体「${active.name}」？`}
                    description="级联删除其 Spec、产物与引用；已启动的运行方案不受影响。"
                    okText="删除"
                    okButtonProps={{ danger: true }}
                    cancelText="取消"
                    onConfirm={removeActive}
                  >
                    <Button danger icon={<DeleteOutlined />}>
                      删除
                    </Button>
                  </Popconfirm>
                </Space>
              </div>

              <div className="onto-steps">
                <Steps
                  size="small"
                  orientation="horizontal"
                  titlePlacement="vertical"
                  responsive={false}
                  current={step}
                  onChange={setStep}
                  items={STAGE_DEFS.map((s, i) => ({
                    key: s.key,
                    title: s.short,
                    status: i === 6 ? 'wait' : flags[i] ? 'finish' : i === step ? 'process' : 'wait',
                    className: i === step ? 'onto-step-selected' : undefined,
                    content: (
                      <span className="onto-step-modes">
                        {s.modes.map((m) => (
                          <Tag key={m.t} color={m.c} style={{ margin: 0, fontSize: 10, lineHeight: '16px', padding: '0 4px' }}>
                            {m.t}
                          </Tag>
                        ))}
                      </span>
                    ),
                  }))}
                />
              </div>

              <Card
                className="work-card onto-stage-card"
                size="small"
                title={
                  <Space size={8} wrap>
                    <span>
                      {STAGE_DEFS[step].key.toUpperCase()} · {STAGE_DEFS[step].short}
                    </span>
                    {STAGE_DEFS[step].modes.map((m) => (
                      <Tag key={m.t} color={m.c} style={{ margin: 0 }}>
                        {m.t}
                      </Tag>
                    ))}
                    {step !== 6 && (flags[step] ? <Tag color="success" style={{ margin: 0 }}>已完成</Tag> : <Tag style={{ margin: 0 }}>待完成</Tag>)}
                    {step === 6 && <Tag color="cyan" style={{ margin: 0 }}>信息展示</Tag>}
                  </Space>
                }
              >
                {step === 0 && <S1Source activeOntology={active} onChanged={handleCreated} />}
                {step === 1 && (
                  <S2Edit
                    ontology={active}
                    spec={spec}
                    specLoading={specLoading}
                    specErr={specErr}
                    onReloadSpec={() => setSpecTick((t) => t + 1)}
                    onMetaSaved={() => reloadOntos()}
                    onSpecSaved={() => {
                      reloadOntos()
                      setSpecTick((t) => t + 1)
                    }}
                  />
                )}
                {step === 2 && (
                  <S3Validate
                    ontologyId={active.id}
                    result={validation ?? null}
                    onResult={(r) => setValidations((v) => ({ ...v, [active.id]: r }))}
                  />
                )}
                {step === 3 && <S4Graph spec={spec} />}
                {step === 4 && <S5Runtime ontologyId={active.id} profiles={profiles} profilesErr={profilesErr} onReload={reloadProfiles} />}
                {step === 5 && <S6Expose ontology={active} profiles={profiles} />}
                {step === 6 && <S7Agent ontology={active} profiles={profiles} />}
              </Card>
            </>
          )}
        </div>
      </Splitter.Panel>

      {renameOpen && active && (
        <RenameModal
          ontology={active}
          onClose={() => setRenameOpen(false)}
          onSaved={() => {
            setRenameOpen(false)
            reloadOntos()
          }}
        />
      )}
    </Splitter>
  )
}

// ---------------------------------------------------------------------------
// S1 本体来源
// ---------------------------------------------------------------------------

function S1Source({ activeOntology, onChanged }: { activeOntology: Ontology | null; onChanged: (selectId?: string) => void }) {
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

  // 学习示例库（seed-learning，REQ §4.8.3 内置领域示例）
  const [learnList, setLearnList] = useState<LearningExample[] | null>(null)
  const [learnBusy, setLearnBusy] = useState('')
  useEffect(() => {
    api
      .listLearningExamples()
      .then((r) => setLearnList(r))
      .catch(() => setLearnList([]))
  }, [])

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
      onChanged(r.ontology.id)
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
      onChanged(r.ontology.id)
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
      const cqs = aiCq
        .split('\n')
        .map((s) => s.trim())
        .filter(Boolean)
      const r = await api.aiDraftOntology(aiDesc.trim(), aiHint.trim() || undefined, cqs.length ? cqs : undefined)
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
      onChanged(created.id)
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
      if (r.seeded === false) {
        showToast(r.note || '内置示例已存在')
        onChanged(r.id)
      } else {
        showToast('内置示例已创建')
        onChanged(r.id)
      }
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setSampleBusy(false)
    }
  }

  const doLearn = async (key: string) => {
    setLearnBusy(key)
    try {
      const o = await api.seedLearningExample(key)
      if ((o as unknown as { seeded?: boolean }).seeded === false) showToast('该学习示例已存在')
      else showToast(`学习示例「${o.name}」已创建`)
      onChanged(o.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setLearnBusy('')
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
      onChanged(o.id)
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
    <>
      {activeOntology && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message={`当前本体「${activeOntology.name}」已建立；以下路径用于新建 / 导入另一个本体。`}
        />
      )}
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
                <Input.TextArea
                  style={{ marginTop: 8 }}
                  value={aiCq}
                  onChange={(e) => setAiCq(e.target.value)}
                  autoSize={{ minRows: 2, maxRows: 5 }}
                  placeholder={
                    '能力问题 CQ（可选，每行一条）：本体应能回答的关键问题\n如：某缺陷源于哪个需求？某故障应采取什么维护措施？'
                  }
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
                <p className="onto-detail-empty">内置「K8s 运维平台」示例本体（id=onto_k8s_ops），一键创建即可体验完整七阶段。</p>
                <Button type="primary" loading={sampleBusy} onClick={doSeed}>
                  创建内置示例
                </Button>
                <div className="onto-sec" style={{ marginTop: 18 }}>
                  <span className="onto-sec-title">学习示例库（领域本体样例，POST /api/ontologies/seed-learning，幂等灌装）</span>
                </div>
                {learnList === null ? (
                  <Spin size="small" />
                ) : learnList.length === 0 ? (
                  <Typography.Text type="secondary">构建平面未返回学习示例（需 ontology-service ≥ P1 尾版本）。</Typography.Text>
                ) : (
                  <Space direction="vertical" style={{ width: '100%' }} size={8}>
                    {learnList.map((le) => (
                      <div key={le.key} className="onto-learn-row">
                        <div style={{ flex: 1, minWidth: 0 }}>
                          <Typography.Text strong>{le.name}</Typography.Text>
                          <Typography.Paragraph type="secondary" style={{ marginBottom: 0, fontSize: 12 }}>
                            {le.description}
                          </Typography.Paragraph>
                        </div>
                        <Button size="small" loading={learnBusy === le.key} onClick={() => doLearn(le.key)}>
                          一键灌装
                        </Button>
                      </div>
                    ))}
                  </Space>
                )}
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
    </>
  )
}

// ---------------------------------------------------------------------------
// S2 编辑
// ---------------------------------------------------------------------------

function S2Edit({
  ontology,
  spec,
  specLoading,
  specErr,
  onReloadSpec,
  onMetaSaved,
  onSpecSaved,
}: {
  ontology: Ontology
  spec: Spec | null
  specLoading: boolean
  specErr: string | null
  onReloadSpec: () => void
  onMetaSaved: () => void
  onSpecSaved: (version: number) => void
}) {
  const { showToast } = useUI()
  const [metaForm] = Form.useForm()
  const [specText, setSpecText] = useState('')
  const [savingMeta, setSavingMeta] = useState(false)
  const [savingSpec, setSavingSpec] = useState(false)
  const [validationErrors, setValidationErrors] = useState<ValidationError[]>([])
  const [vhRefresh, setVhRefresh] = useState(0)

  useEffect(() => {
    metaForm.setFieldsValue({ name: ontology.name, description: ontology.description ?? '' })
  }, [ontology.id, ontology.name, ontology.description, metaForm])

  useEffect(() => {
    setSpecText(spec ? JSON.stringify(spec, null, 2) : '')
    setValidationErrors([])
  }, [spec])

  const large = specText.length > 200_000

  const saveMeta = async () => {
    let v: any
    try {
      v = await metaForm.validateFields()
    } catch {
      return
    }
    setSavingMeta(true)
    try {
      await api.updateOntologyMeta(ontology.id, { name: v.name, description: v.description ?? '' })
      showToast('基本信息已保存')
      onMetaSaved()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setSavingMeta(false)
    }
  }

  const saveSpec = async () => {
    let parsed: Spec
    try {
      parsed = JSON.parse(specText)
    } catch (e: any) {
      showToast(`JSON 解析失败：${e.message}`, 'err')
      return
    }
    setSavingSpec(true)
    setValidationErrors([])
    try {
      const r = await api.saveSpec(ontology.id, parsed)
      showToast(`Spec 已保存（version ${r.version}）`)
      onSpecSaved(r.version)
      setVhRefresh((x) => x + 1)
    } catch (e: any) {
      if (e instanceof ApiError && e.validationErrors?.length) {
        setValidationErrors(e.validationErrors)
        showToast('校验未通过，请修正后重试', 'err')
      } else {
        showToast(e.message, 'err')
      }
    } finally {
      setSavingSpec(false)
    }
  }

  return (
    <>
      <Form form={metaForm} layout="vertical" requiredMark={false}>
        <div className="onto-meta-row">
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '名称必填' }]} style={{ width: 260, marginBottom: 0 }}>
            <Input />
          </Form.Item>
          <Form.Item name="description" label="描述" style={{ flex: 1, marginBottom: 0 }}>
            <Input placeholder="本体用途说明" />
          </Form.Item>
          <Button type="primary" loading={savingMeta} onClick={saveMeta}>
            保存基本信息
          </Button>
        </div>
      </Form>

      <div className="onto-sec">
        <span className="onto-sec-title">Spec JSON（S2 编辑）</span>
        <span className="hit-spacer" />
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {specLoading
            ? '加载中…'
            : spec
              ? `概念 ${spec.concepts?.length ?? 0} · 关系 ${spec.relations?.length ?? 0} · 实例 ${spec.instances?.length ?? 0}`
              : '尚未保存过 Spec'}
        </Typography.Text>
        <Button size="small" icon={<ReloadOutlined />} onClick={onReloadSpec} disabled={specLoading}>
          重新加载
        </Button>
        {!spec && !specLoading && (
          <Button size="small" onClick={() => setSpecText(JSON.stringify(emptySpec(ontology.name), null, 2))}>
            初始化空 Spec
          </Button>
        )}
        <Button size="small" type="primary" loading={savingSpec} disabled={!specText || large} onClick={saveSpec}>
          保存 Spec
        </Button>
      </div>

      {specErr ? (
        <Alert
          type="error"
          showIcon
          style={{ marginTop: 10 }}
          message="Spec 加载失败"
          description={specErr}
          action={
            <Button size="small" onClick={onReloadSpec}>
              重试
            </Button>
          }
        />
      ) : (
        <>
          {large && (
            <Alert
              type="warning"
              showIcon
              style={{ margin: '10px 0' }}
              message="Spec 体积较大，已切换为只读"
              description="请在本地编辑后经导入 / 导出接口处理，避免浏览器卡顿。"
            />
          )}
          <Input.TextArea
            className="onto-spec-editor"
            value={specText}
            readOnly={large}
            onChange={(e) => setSpecText(e.target.value)}
            autoSize={{ minRows: 16, maxRows: 40 }}
            spellCheck={false}
            placeholder='{ "name": "…", "concepts": [], "relations": [], "instances": [] }'
            style={{ marginTop: 10 }}
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
      )}

      <div style={{ marginTop: 16 }}>
        <VersionHistory ontologyId={ontology.id} refreshSignal={vhRefresh} />
      </div>
    </>
  )
}

// ---------------------------------------------------------------------------
// 版本历史 + 源码视图（REQ-93：GET /versions、GET /versions/{v}/original）
// ---------------------------------------------------------------------------

function VersionHistory({ ontologyId, refreshSignal }: { ontologyId: string; refreshSignal: number }) {
  const { showToast } = useUI()
  const [list, setList] = useState<VersionsResponse | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [tick, setTick] = useState(0)
  const [original, setOriginal] = useState<{ version: number; text: string } | null>(null)
  const [origLoading, setOrigLoading] = useState(false)

  useEffect(() => {
    let alive = true
    setLoading(true)
    setErr(null)
    api
      .listVersions(ontologyId)
      .then((r) => {
        if (alive) setList(r)
      })
      .catch((e: any) => {
        if (alive) setErr(e.message)
      })
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
  }, [ontologyId, refreshSignal, tick])

  const viewOriginal = async (v: number) => {
    setOrigLoading(true)
    try {
      const text = await api.getVersionOriginal(ontologyId, v)
      setOriginal({ version: v, text })
    } catch (e: any) {
      if (e instanceof ApiError && e.status === 404) showToast('该版本无原始源文件（由编辑/灌装产生，仅存 spec 快照）', 'err')
      else showToast(e.message, 'err')
    } finally {
      setOrigLoading(false)
    }
  }

  const copyOrig = () => {
    navigator.clipboard
      ?.writeText(original?.text ?? '')
      .then(() => showToast('源码已复制'))
      .catch(() => showToast('复制失败', 'err'))
  }

  const columns: ColumnsType<VersionMeta> = [
    { title: '版本', dataIndex: 'version', width: 70 },
    { title: '保存时间', dataIndex: 'created_at', width: 180 },
    {
      title: '原始源文件',
      width: 140,
      render: (_, r) =>
        r.has_original ? (
          <Tag color="blue" style={{ margin: 0 }}>
            {r.original_format} · {r.original_size ?? 0}B
          </Tag>
        ) : (
          <Typography.Text type="secondary">—</Typography.Text>
        ),
    },
    {
      title: '操作',
      width: 120,
      render: (_, r) => (
        <Button size="small" icon={<CodeOutlined />} disabled={!r.has_original} loading={origLoading} onClick={() => viewOriginal(r.version)}>
          查看源码
        </Button>
      ),
    },
  ]

  return (
    <>
      <div className="onto-sec">
        <span className="onto-sec-title">版本历史（REQ-93，每次保存 / 导入 / 灌装自动留快照）</span>
        <span className="hit-spacer" />
        <Button size="small" icon={<ReloadOutlined />} onClick={() => setTick((t) => t + 1)} disabled={loading}>
          刷新
        </Button>
      </div>
      {err ? (
        <Alert type="warning" showIcon message="版本列表获取失败" description={err} />
      ) : (
        <Table<VersionMeta>
          rowKey="version"
          columns={columns}
          dataSource={list?.versions ?? []}
          loading={loading}
          pagination={false}
          size="small"
        />
      )}
      {original && (
        <>
          <div className="onto-sec" style={{ marginTop: 14 }}>
            <span className="onto-sec-title">版本 {original.version} 的原始源文件</span>
            <span className="hit-spacer" />
            <Button size="small" icon={<CopyOutlined />} onClick={copyOrig}>
              复制
            </Button>
          </div>
          <pre className="onto-guide-pre">{original.text}</pre>
        </>
      )}
    </>
  )
}

// ---------------------------------------------------------------------------
// S3 校验
// ---------------------------------------------------------------------------

function S3Validate({
  ontologyId,
  result,
  onResult,
}: {
  ontologyId: string
  result: ValidationState | null
  onResult: (r: ValidationState) => void
}) {
  const { showToast } = useUI()
  const [busy, setBusy] = useState(false)

  const run = async () => {
    setBusy(true)
    try {
      const r = await api.validateOntology(ontologyId)
      const res: ValidationState = { ok: !!r.ok, errors: r.validation_errors ?? [] }
      onResult(res)
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
      </Space>
      <p className="onto-detail-empty">JSON Schema + 引用完整性校验（就地展示通过 / 错误；结果缓存用于左栏进度派生）。</p>
      {result?.ok && (
        <Alert
          type="success"
          showIcon
          style={{ marginTop: 4 }}
          message="校验通过"
          description="Spec 满足 Schema 与引用完整性约束，可进入 S4 可视化 / S5 运行方式。"
        />
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
// S4 可视化（手绘 SVG，无新依赖）
// ---------------------------------------------------------------------------

const NODE_W = 150
const NODE_H = 42
const GAP_X = 34
const GAP_Y = 74
const PAD = 26

function computeDepths(concepts: SpecConcept[]): Map<string, number> {
  const byName = new Map(concepts.map((c) => [c.name, c]))
  const depth = new Map<string, number>()
  const visiting = new Set<string>()
  const calc = (name: string): number => {
    if (depth.has(name)) return depth.get(name)!
    if (visiting.has(name)) return 0 // 环：降级
    visiting.add(name)
    const c = byName.get(name)
    const parents = (c?.parents ?? []).filter((p) => byName.has(p))
    const d = parents.length ? 1 + Math.max(...parents.map(calc)) : 0
    visiting.delete(name)
    depth.set(name, d)
    return d
  }
  for (const c of concepts) calc(c.name)
  return depth
}

interface NodePos {
  c: SpecConcept
  x: number
  y: number
}

function layoutGraph(spec: Spec): { nodes: NodePos[]; width: number; height: number } {
  const concepts = spec.concepts ?? []
  const depth = computeDepths(concepts)
  const levels = new Map<number, SpecConcept[]>()
  for (const c of concepts) {
    const d = depth.get(c.name) ?? 0
    if (!levels.has(d)) levels.set(d, [])
    levels.get(d)!.push(c)
  }
  const rows = [...levels.entries()].sort((a, b) => a[0] - b[0])
  const maxCols = Math.max(1, ...rows.map(([, cs]) => cs.length))
  const width = PAD * 2 + maxCols * NODE_W + Math.max(0, maxCols - 1) * GAP_X
  const nodes: NodePos[] = []
  for (const [d, cs] of rows) {
    cs.sort((a, b) => a.name.localeCompare(b.name))
    const rowW = cs.length * NODE_W + Math.max(0, cs.length - 1) * GAP_X
    const startX = (width - rowW) / 2
    cs.forEach((c, i) => nodes.push({ c, x: startX + i * (NODE_W + GAP_X), y: PAD + d * (NODE_H + GAP_Y) }))
  }
  const height = PAD * 2 + rows.length * NODE_H + Math.max(0, rows.length - 1) * GAP_Y
  return { nodes, width, height }
}

function S4Graph({ spec }: { spec: Spec | null }) {
  const graph = useMemo(() => (spec ? layoutGraph(spec) : null), [spec])

  if (!spec || (spec.concepts?.length ?? 0) === 0) {
    return (
      <div className="work-empty" style={{ minHeight: 220 }}>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无概念可可视化；请先在 S1 创建 / 导入，或到 S2 保存 Spec" />
      </div>
    )
  }

  const nodes = graph!.nodes
  const pos = new Map(nodes.map((n) => [n.c.name, n]))
  const center = (n: NodePos) => ({ x: n.x + NODE_W / 2, y: n.y + NODE_H / 2 })
  const instCount = new Map<string, number>()
  for (const inst of spec.instances ?? []) instCount.set(inst.concept, (instCount.get(inst.concept) ?? 0) + 1)

  const edges: { key: string; a: NodePos; b: NodePos; label?: string; parent?: boolean }[] = []
  for (const r of spec.relations ?? []) {
    const a = pos.get(r.from)
    const b = pos.get(r.to)
    if (a && b) edges.push({ key: `r-${r.name}-${r.from}-${r.to}`, a, b, label: r.label || r.name, parent: false })
  }
  for (const c of spec.concepts) {
    for (const p of c.parents ?? []) {
      const a = pos.get(c.name)
      const b = pos.get(p)
      if (a && b) edges.push({ key: `p-${c.name}-${p}`, a, b, parent: true })
    }
  }

  return (
    <div className="onto-graph-wrap">
      <div className="onto-graph-legend">
        <Tag color="blue" style={{ margin: 0 }}>概念 {spec.concepts.length}</Tag>
        <Tag color="geekblue" style={{ margin: 0 }}>关系 {spec.relations?.length ?? 0}</Tag>
        <Tag color="purple" style={{ margin: 0 }}>实例 {spec.instances?.length ?? 0}</Tag>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          实线 = 关系（from → to）；虚线 = 父子（parents）；节点右上角数字为该概念的实例数；悬停查看定义。
        </Typography.Text>
      </div>
      <svg className="onto-graph" viewBox={`0 0 ${graph!.width} ${graph!.height}`} preserveAspectRatio="xMidYMid meet" role="img">
        <defs>
          <marker id="onto-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
            <path d="M 0 0 L 10 5 L 0 10 z" className="onto-arrow-head" />
          </marker>
        </defs>
        {edges.map((e) => {
          const ca = center(e.a)
          const cb = center(e.b)
          const mx = (ca.x + cb.x) / 2
          const my = (ca.y + cb.y) / 2
          return (
            <g key={e.key}>
              <line
                x1={ca.x}
                y1={ca.y}
                x2={cb.x}
                y2={cb.y}
                className={`onto-edge${e.parent ? ' parent' : ''}`}
                markerEnd="url(#onto-arrow)"
              />
              {e.label && !e.parent && (
                <text x={mx} y={my - 4} textAnchor="middle" className="onto-edge-label">
                  {truncate(e.label, 14)}
                </text>
              )}
            </g>
          )
        })}
        {nodes.map((n) => {
          const cnt = instCount.get(n.c.name) ?? 0
          return (
            <g key={n.c.name} transform={`translate(${n.x},${n.y})`} className="onto-node">
              <title>{n.c.definition || n.c.label || n.c.name}</title>
              <rect width={NODE_W} height={NODE_H} rx={9} />
              <text x={NODE_W / 2} y={NODE_H / 2 + 1} textAnchor="middle" dominantBaseline="middle">
                {truncate(n.c.label || n.c.name, 16)}
              </text>
              {cnt > 0 && (
                <g transform={`translate(${NODE_W - 8}, 8)`} className="onto-node-badge">
                  <circle r={9} />
                  <text textAnchor="middle" dominantBaseline="middle">
                    {cnt}
                  </text>
                </g>
              )}
            </g>
          )
        })}
      </svg>
    </div>
  )
}

// ---------------------------------------------------------------------------
// S5 运行方式
// ---------------------------------------------------------------------------

function S5Runtime({
  ontologyId,
  profiles,
  profilesErr,
  onReload,
}: {
  ontologyId: string
  profiles: RuntimeProfile[]
  profilesErr: boolean
  onReload: () => void
}) {
  const { showToast } = useUI()
  const mine = profiles.filter((p) => (p.ontology_ids ?? []).includes(ontologyId))
  const [name, setName] = useState('')
  const [port, setPort] = useState<number | null>(null)
  const [creating, setCreating] = useState(false)
  const [busyId, setBusyId] = useState<string | null>(null)

  const create = async () => {
    if (!name.trim()) {
      showToast('请输入方案名称', 'err')
      return
    }
    setCreating(true)
    try {
      await api.createRuntimeProfile({
        name: name.trim(),
        engine: 'oxigraph',
        ontology_ids: [ontologyId],
        ...(port ? { port } : {}),
      })
      showToast('运行方案已创建')
      setName('')
      setPort(null)
      onReload()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setCreating(false)
    }
  }

  const act = async (id: string, fn: () => Promise<unknown>, ok: string) => {
    setBusyId(id)
    try {
      await fn()
      showToast(ok)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusyId(null)
      onReload()
    }
  }

  const remove = async (id: string) => {
    try {
      await api.deleteRuntimeProfile(id)
      showToast('运行方案已删除')
      onReload()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  return (
    <>
      <div className="onto-sec">
        <span className="onto-sec-title">运行方案（本体的 S5 段）</span>
        <span className="hit-spacer" />
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          引擎固定 oxigraph（P1 唯一引擎）；启动后由 Runtime Manager 托管。
        </Typography.Text>
        <Button size="small" icon={<ReloadOutlined />} onClick={onReload}>
          刷新
        </Button>
      </div>

      {profilesErr && (
        <Alert
          type="warning"
          showIcon
          style={{ marginTop: 10 }}
          message="运行平面暂不可达"
          description="RUNTIME_MGR_URL（:8090）未就绪，无法读取 / 管理运行方案。"
        />
      )}

      {mine.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} style={{ margin: '18px 0' }} description="该本体暂无运行方案，请在下方新建" />
      ) : (
        <div className="onto-profile-grid">
          {mine.map((p) => {
            const badge = PROFILE_BADGE[p.status] ?? { status: 'default' as const, text: p.status }
            return (
              <div className="onto-profile-card" key={p.id}>
                <div className="onto-profile-head">
                  <span className="onto-profile-name" title={p.name}>
                    {p.name}
                  </span>
                  {p.status === 'error' && p.last_error ? (
                    <Tooltip title={p.last_error}>
                      <span>
                        <Badge status={badge.status} text={badge.text} />
                      </span>
                    </Tooltip>
                  ) : (
                    <Badge status={badge.status} text={badge.text} />
                  )}
                  {p.status === 'starting' && <Spin size="small" />}
                </div>
                <div className="onto-profile-meta">
                  <span>引擎 {p.engine ?? '—'}</span>
                  <span className="dot">·</span>
                  <span>端口 {p.port ?? '—'}</span>
                  {typeof p.pid === 'number' && (
                    <>
                      <span className="dot">·</span>
                      <span>PID {p.pid}</span>
                    </>
                  )}
                </div>
                <div className="onto-profile-ops">
                  {(p.status === 'created' || p.status === 'stopped' || p.status === 'error') && (
                    <Button
                      type="link"
                      size="small"
                      icon={<PlayCircleOutlined />}
                      loading={busyId === p.id}
                      onClick={() => act(p.id, () => api.startRuntimeProfile(p.id), '已请求启动')}
                    >
                      启动
                    </Button>
                  )}
                  {(p.status === 'running' || p.status === 'starting') && (
                    <Button
                      type="link"
                      size="small"
                      icon={<PauseCircleOutlined />}
                      loading={busyId === p.id}
                      onClick={() => act(p.id, () => api.stopRuntimeProfile(p.id), '已停止')}
                    >
                      停止
                    </Button>
                  )}
                  <Button
                    type="link"
                    size="small"
                    icon={<ReloadOutlined />}
                    loading={busyId === p.id}
                    onClick={() => act(p.id, () => api.reloadRuntimeProfile(p.id), '已请求重载')}
                  >
                    重载
                  </Button>
                  <Popconfirm
                    title={`删除运行方案「${p.name}」？`}
                    description="运行中将先停止再删除。"
                    okText="删除"
                    okButtonProps={{ danger: true }}
                    cancelText="取消"
                    onConfirm={() => remove(p.id)}
                  >
                    <Button type="link" size="small" danger icon={<DeleteOutlined />}>
                      删除
                    </Button>
                  </Popconfirm>
                </div>
              </div>
            )
          })}
        </div>
      )}

      <div className="onto-new-profile">
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="新方案名称（如 k8s-ops-oxigraph）" style={{ width: 280 }} />
        <InputNumber min={1} max={65535} value={port} onChange={(v) => setPort(typeof v === 'number' ? v : null)} placeholder="端口（可选）" style={{ width: 140 }} />
        <Button type="primary" icon={<PlusOutlined />} loading={creating} onClick={create}>
          新建方案
        </Button>
      </div>

      {mine.length > 0 && <RuntimeLogs profiles={mine} />}
    </>
  )
}

function RuntimeLogs({ profiles }: { profiles: RuntimeProfile[] }) {
  const [pid, setPid] = useState<string | undefined>(profiles[0]?.id)
  const [tail, setTail] = useState<number | null>(200)
  const [lines, setLines] = useState<string[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    if (pid && !profiles.some((p) => p.id === pid)) setPid(profiles[0]?.id)
  }, [profiles, pid])

  const load = () => {
    if (!pid) return
    setLoading(true)
    setErr(null)
    api
      .runtimeProfileLogs(pid, tail ?? 200)
      .then((r) => setLines(r.lines ?? []))
      .catch((e: any) => {
        setLines(null)
        setErr(e.message)
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pid])

  return (
    <Collapse
      ghost
      style={{ marginTop: 12 }}
      items={[
        {
          key: 'logs',
          label: '运行日志',
          children: (
            <>
              <div className="onto-sec">
                <Select
                  size="small"
                  value={pid}
                  onChange={setPid}
                  style={{ width: 220 }}
                  options={profiles.map((p) => ({ value: p.id, label: p.name }))}
                />
                <InputNumber
                  size="small"
                  min={20}
                  max={2000}
                  value={tail}
                  onChange={(v) => setTail(typeof v === 'number' ? v : null)}
                  prefix={<Typography.Text type="secondary" style={{ fontSize: 12 }}>tail</Typography.Text>}
                  style={{ width: 130 }}
                />
                <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={load}>
                  刷新
                </Button>
                <span className="hit-spacer" />
                {err && <Typography.Text type="danger" style={{ fontSize: 12 }}>{err}</Typography.Text>}
              </div>
              <pre className="onto-log-pre">{lines && lines.length ? lines.join('\n') : loading ? '加载中…' : err ? '（日志不可用）' : '（暂无日志）'}</pre>
            </>
          ),
        },
      ]}
    />
  )
}

// ---------------------------------------------------------------------------
// S6 对外暴露
// ---------------------------------------------------------------------------

/** SPARQL 工作台默认模板（REQ-92：POST /api/runtime-profiles/{id}/sparql，引擎直连） */
const DEFAULT_SPARQL = `PREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT ?s ?o WHERE { ?s rdf:type ?o } LIMIT 20`

const TRACE_COLUMNS: ColumnsType<TraceEntry> = [
  { title: '时间', dataIndex: 'ts', width: 165 },
  { title: '工具', dataIndex: 'tool', width: 130 },
  {
    title: 'SPARQL',
    dataIndex: 'sparql',
    ellipsis: true,
    render: (v) => (
      <Typography.Text code style={{ fontSize: 12 }}>
        {String(v).slice(0, 140)}
      </Typography.Text>
    ),
  },
  { title: '耗时', dataIndex: 'took_ms', width: 85, render: (v) => `${v} ms` },
  { title: '结果数', dataIndex: 'result_count', width: 80 },
  {
    title: '状态',
    dataIndex: 'ok',
    width: 80,
    render: (v) =>
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
  { title: '错误', dataIndex: 'error', ellipsis: true },
]

function S6Expose({ ontology, profiles }: { ontology: Ontology; profiles: RuntimeProfile[] }) {
  const { showToast } = useUI()
  const running = profiles.find((p) => p.status === 'running' && (p.ontology_ids ?? []).includes(ontology.id))
  const [guide, setGuide] = useState<string | null>(null)
  const [guideErr, setGuideErr] = useState<string | null>(null)
  const [guideLoading, setGuideLoading] = useState(false)

  // SPARQL 工作台（REQ-92）
  const [sparql, setSparql] = useState(DEFAULT_SPARQL)
  const [sparqlBusy, setSparqlBusy] = useState(false)
  const [sparqlErr, setSparqlErr] = useState<string | null>(null)
  const [sparqlCols, setSparqlCols] = useState<string[]>([])
  const [sparqlRows, setSparqlRows] = useState<Record<string, string>[]>([])
  const [sparqlRaw, setSparqlRaw] = useState<string | null>(null)

  // 翻译透视（REQ-94）
  const [traces, setTraces] = useState<TraceEntry[]>([])
  const [tracesLoading, setTracesLoading] = useState(false)

  const loadTraces = async () => {
    if (!running) {
      setTraces([])
      return
    }
    setTracesLoading(true)
    try {
      const r = await api.listTraces(running.id, 50)
      setTraces(r.traces ?? [])
    } catch {
      setTraces([])
    } finally {
      setTracesLoading(false)
    }
  }

  useEffect(() => {
    loadTraces()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [running?.id])

  useEffect(() => {
    let alive = true
    setGuideLoading(true)
    setGuideErr(null)
    api
      .getOntologyGuide(ontology.id)
      .then((r) => {
        if (alive) setGuide(r.guide ?? '')
      })
      .catch((e: any) => {
        if (alive) setGuideErr(e.message)
      })
      .finally(() => {
        if (alive) setGuideLoading(false)
      })
    return () => {
      alive = false
    }
  }, [ontology.id])

  const copyGuide = () => {
    navigator.clipboard
      ?.writeText(guide ?? '')
      .then(() => showToast('指引已复制'))
      .catch(() => showToast('复制失败', 'err'))
  }

  const runSparql = async () => {
    if (!running) {
      showToast('请先在 S5 启动运行方案', 'err')
      return
    }
    if (!sparql.trim()) {
      showToast('请输入 SPARQL 查询', 'err')
      return
    }
    setSparqlBusy(true)
    setSparqlErr(null)
    setSparqlRaw(null)
    setSparqlCols([])
    setSparqlRows([])
    try {
      const r = await api.runSparql(running.id, sparql.trim())
      const vars: string[] = r.json?.head?.vars ?? []
      const binds: Record<string, { value?: string }>[] = r.json?.results?.bindings ?? []
      if (vars.length && binds.length) {
        setSparqlCols(vars)
        setSparqlRows(
          binds.map((b, i) => {
            const row: Record<string, string> = { __key: String(i) }
            for (const v of vars) row[v] = b[v]?.value ?? ''
            return row
          }),
        )
      } else {
        setSparqlRaw((r.raw || '（空结果）').slice(0, 4000))
      }
      loadTraces()
    } catch (e: any) {
      setSparqlErr(e.message)
      loadTraces()
    } finally {
      setSparqlBusy(false)
    }
  }

  return (
    <>
      <Alert
        type={running ? 'success' : 'info'}
        showIcon
        style={{ marginBottom: 12 }}
        message={
          running
            ? `统一 MCP facade 已随运行方案「${running.name}」暴露（端口 ${running.port ?? '—'}）`
            : '尚未启动运行方案：S6 需在 S5 启动方案后由 Runtime Manager 暴露 facade'
        }
      />

      <div className="onto-expose-grid">
        <Card size="small" className="work-card" title={<Space size={6}><ApiOutlined />MCP facade 端点</Space>}>
          <p className="onto-detail-empty">
            运行平面 Runtime Manager 统一暴露 <Typography.Text code>POST /mcp</Typography.Text>
            （默认 <Typography.Text code>http://127.0.0.1:8090/mcp</Typography.Text>），按 <Typography.Text code>ontology_id</Typography.Text> 路由到对应本体。
          </p>
          <div className="onto-expose-meta">
            <span>本方案端口</span>
            <Tag color={running ? 'green' : 'default'} style={{ margin: 0 }}>{running?.port ?? '—'}</Tag>
            <span>状态</span>
            {running ? <Badge status="success" text="运行中" /> : <Badge status="default" text="未运行" />}
          </div>
        </Card>
      </div>

      <div className="onto-sec">
        <span className="onto-sec-title">固定签名工具（onto_*，共 4 个）</span>
      </div>
      <Table rowKey="name" columns={TOOL_COLUMNS} dataSource={ONTO_TOOLS} pagination={false} size="small" />

      <div className="onto-sec">
        <span className="onto-sec-title">注入指引（guide）</span>
        <span className="hit-spacer" />
        <Button size="small" icon={<CopyOutlined />} disabled={!guide} onClick={copyGuide}>
          复制
        </Button>
      </div>
      {guideErr ? (
        <Alert type="warning" showIcon message="指引获取失败" description={guideErr} />
      ) : (
        <pre className="onto-guide-pre">{guideLoading ? '加载中…' : guide || '（暂无指引）'}</pre>
      )}

      <div className="onto-sec" style={{ marginTop: 18 }}>
        <span className="onto-sec-title">SPARQL 工作台（REQ-92，直连方案端点 /api/runtime-profiles/{'{id}'}/sparql）</span>
      </div>
      {!running ? (
        <Alert type="info" showIcon message="方案未运行：在 S5 启动后可在此直接执行 SPARQL（非 running 状态后端返回 409）。" />
      ) : (
        <>
          <Input.TextArea
            className="onto-spec-editor"
            value={sparql}
            onChange={(e) => setSparql(e.target.value)}
            autoSize={{ minRows: 4, maxRows: 12 }}
            spellCheck={false}
          />
          <Space style={{ marginTop: 8 }} size={10}>
            <Button type="primary" icon={<PlayCircleOutlined />} loading={sparqlBusy} onClick={runSparql}>
              执行查询
            </Button>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              Accept: application/sparql-results+json · 状态码与错误原样透传引擎
            </Typography.Text>
          </Space>
          {sparqlErr && <Alert type="error" showIcon style={{ marginTop: 10 }} message="查询失败（引擎返回）" description={sparqlErr} />}
          {sparqlCols.length > 0 && (
            <Table
              rowKey="__key"
              columns={sparqlCols.map((v) => ({ title: v, dataIndex: v }))}
              dataSource={sparqlRows}
              pagination={{ pageSize: 10, hideOnSinglePage: true }}
              size="small"
              style={{ marginTop: 10 }}
              scroll={{ x: 'max-content' }}
            />
          )}
          {sparqlRaw && <pre className="onto-guide-pre" style={{ marginTop: 10 }}>{sparqlRaw}</pre>}
        </>
      )}

      <div className="onto-sec" style={{ marginTop: 18 }}>
        <span className="onto-sec-title">翻译透视（REQ-94，最近 50 条，失败查询同样留痕）</span>
        <span className="hit-spacer" />
        <Button size="small" icon={<ReloadOutlined />} onClick={loadTraces} disabled={!running || tracesLoading}>
          刷新
        </Button>
      </div>
      {!running ? (
        <Typography.Text type="secondary">方案未运行，暂无翻译记录。</Typography.Text>
      ) : (
        <Table<TraceEntry>
          rowKey={(r) => String(r.id ?? `${r.ts}-${r.tool}`)}
          columns={TRACE_COLUMNS}
          dataSource={traces}
          loading={tracesLoading}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          size="small"
        />
      )}
    </>
  )
}

// ---------------------------------------------------------------------------
// S7 对接智能体
// ---------------------------------------------------------------------------

function S7Agent({ ontology, profiles }: { ontology: Ontology; profiles: RuntimeProfile[] }) {
  const running = profiles.find((p) => p.status === 'running' && (p.ontology_ids ?? []).includes(ontology.id))
  const [convs, setConvs] = useState<Conversation[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    if (!running) {
      setConvs(null)
      return
    }
    let alive = true
    setLoading(true)
    setErr(null)
    api
      .listConversations()
      .then((cs) => {
        if (alive) setConvs(cs.filter((c) => c.runtime_profile_id === running.id))
      })
      .catch((e: any) => {
        if (alive) {
          setConvs(null)
          setErr(e.message)
        }
      })
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
  }, [running?.id])

  if (!running) {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description="尚未运行方案：请先在 S5 启动一个包含本体的运行方案，再回此挂载到对话"
      />
    )
  }

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message={`对话经「运行方案」挂载本本体：在对话输入框的「本体增强」chip 中选择 running 方案「${running.name}」。`}
        description="装配期注入 guide；单次失败即降级（ontology.unavailable 事件卡），配置保留，下一条消息自动恢复（REQ-97）。"
      />

      <div className="onto-sec">
        <span className="onto-sec-title">已挂载对话</span>
        <span className="hit-spacer" />
        {loading && <Typography.Text type="secondary" style={{ fontSize: 12 }}>加载中…</Typography.Text>}
      </div>

      {err && <Alert type="warning" showIcon message="对话列表获取失败" description={err} />}

      {convs && convs.length === 0 && (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          style={{ margin: '14px 0' }}
          description="暂无对话挂载该运行方案；到「智能体 / 项目」对话的输入框下方选择本体增强方案即可"
        />
      )}

      {convs && convs.length > 0 && (
        <div className="onto-conv-list">
          {convs.map((c) => (
            <div className="onto-conv-item" key={c.id}>
              <span className="onto-conv-title" title={c.title}>
                {c.title || '未命名对话'}
              </span>
              <Tag style={{ margin: 0 }}>{c.scope === 'project' ? '项目' : '智能体'}</Tag>
            </div>
          ))}
        </div>
      )}
    </>
  )
}

// ---------------------------------------------------------------------------
// 重命名弹窗
// ---------------------------------------------------------------------------

function RenameModal({ ontology, onClose, onSaved }: { ontology: Ontology; onClose: () => void; onSaved: () => void }) {
  const { showToast } = useUI()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    form.setFieldsValue({ name: ontology.name, description: ontology.description ?? '' })
  }, [ontology.id, ontology.name, ontology.description, form])

  const save = async () => {
    let v: any
    try {
      v = await form.validateFields()
    } catch {
      return
    }
    setBusy(true)
    try {
      await api.updateOntologyMeta(ontology.id, { name: v.name, description: v.description ?? '' })
      showToast('已保存')
      onSaved()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      centered
      title="重命名本体"
      width={480}
      onCancel={onClose}
      footer={
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={busy} onClick={save}>
            保存
          </Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        <Form.Item name="name" label="名称" rules={[{ required: true, message: '名称必填' }]}>
          <Input maxLength={80} />
        </Form.Item>
        <Form.Item name="description" label="描述">
          <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} />
        </Form.Item>
      </Form>
    </Modal>
  )
}
