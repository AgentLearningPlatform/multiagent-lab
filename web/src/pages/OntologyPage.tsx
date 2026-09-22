import '@xyflow/react/dist/style.css'
import '@triply/yasgui/build/yasgui.min.css'
import { useEffect, useMemo, useRef, useState } from 'react'
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
  BranchesOutlined,
  CheckCircleOutlined,
  CodeOutlined,
  CopyOutlined,
  DeleteOutlined,
  DownloadOutlined,
  EditOutlined,
  InboxOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { api, ApiError } from '../api/client'
import Yasgui from '@triply/yasgui'
import CodeMirror from '@uiw/react-codemirror'
import { EditorView } from '@codemirror/view'
import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  useEdgesState,
  useNodesState,
} from '@xyflow/react'
import type { Edge, Node, NodeProps, NodeTypes } from '@xyflow/react'
import type {
  AiDraftResult,
  Conversation,
  CsvIngestPreview,
  DiffCollection,
  DiffImpact,
  DiffItem,
  DiffResult,
  ImportReport,
  LearningExample,
  Ontology,
  RuntimeProfile,
  Spec,
  SpecConcept,
  SpecInstance,
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
  const [forkName, setForkName] = useState('')
  const [forkBusy, setForkBusy] = useState(false)
  const [forkErr, setForkErr] = useState<string | null>(null)

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

  /** REQ-83：fork 为独立新本体（forked_from=源 id，version 重置 1），并选中新本体 */
  const doFork = async () => {
    if (!active) return
    setForkBusy(true)
    setForkErr(null)
    try {
      const o = await api.forkOntology(active.id, forkName.trim() ? { name: forkName.trim() } : {})
      showToast(`已 Fork 为「${o.name}」`)
      setForkName('')
      reloadOntos(o.id)
      reloadProfiles()
    } catch (e: any) {
      setForkErr(e?.message ?? 'Fork 失败')
    } finally {
      setForkBusy(false)
    }
  }

  /** CSV 灌装入库后：刷新 Spec / 左栏进度（版本列表在 S2 挂载时自取） */
  const handleIngested = () => {
    reloadOntos(activeId ?? undefined)
    setSpecTick((t) => t + 1)
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
                  <Popconfirm
                    icon={null}
                    title="Fork 为新本体"
                    description={
                      <Space direction="vertical" style={{ width: 300 }} size={8}>
                        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                          复制该本体的全部 Spec 与产物为新本体（forked_from 记录来源，版本重置为 1）。
                        </Typography.Text>
                        <Input
                          value={forkName}
                          onChange={(e) => setForkName(e.target.value)}
                          placeholder={`新名称（可选，默认「${active.name} 副本」）`}
                        />
                      </Space>
                    }
                    okText="Fork"
                    cancelText="取消"
                    okButtonProps={{ loading: forkBusy }}
                    onOpenChange={(o) => {
                      if (o) {
                        setForkName('')
                        setForkErr(null)
                      }
                    }}
                    onConfirm={doFork}
                  >
                    <Button icon={<BranchesOutlined />}>Fork 本体</Button>
                  </Popconfirm>
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

              {forkErr && (
                <Alert
                  type="error"
                  showIcon
                  closable
                  message="Fork 失败"
                  description={forkErr}
                  onClose={() => setForkErr(null)}
                />
              )}

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
                {step === 0 && <S1Source activeOntology={active} onChanged={handleCreated} onIngested={handleIngested} />}
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

              {/* P1：本体级「查询 & 源码」入口（REQ-92/93），不随阶段切换隐藏 */}
              <QuerySourceEntry ontology={active} profiles={profiles} profilesErr={profilesErr} spec={spec} />
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

function S1Source({
  activeOntology,
  onChanged,
  onIngested,
}: {
  activeOntology: Ontology | null
  onChanged: (selectId?: string) => void
  /** CSV 灌装入库后回调（刷新 Spec / 版本列表 / 左栏进度）；无目标本体时不传 */
  onIngested?: () => void
}) {
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
      // REQ-82：CQ 引导折叠进 extraHint（后端 extraHint 参数已支持，无需 capability_questions 字段）
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
                <div className="onto-sec" style={{ marginTop: 0 }}>
                  <span className="onto-sec-title">能力问题（CQ）引导（可选，每行一个）</span>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    先列 3~5 个本体要回答的问题，会并入生成上下文（REQ-82）
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
            key: 'csv',
            label: 'CSV 灌装',
            children: activeOntology ? (
              <CsvIngest ontology={activeOntology} onIngested={onIngested} />
            ) : (
              <Alert
                type="info"
                showIcon
                message="CSV 灌装需先选择目标本体"
                description="该路径把 CSV 行按同名映射灌装为 Spec 实例（REQ-96 P2a，数据进 spec_json 体系）。请先创建 / 导入一个本体并选中它，再回到 S1 执行灌装。"
              />
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
// S1 路径：CSV 灌装（REQ-96 P2a：同名映射 → Spec 实例草稿 → 校验入库）
// ---------------------------------------------------------------------------

/** 解析 CSV 首行表头（简单逗号切分；去引号 / BOM / 空白） */
function parseCsvHeader(text: string): string[] {
  const first = text.split(/\r?\n/, 1)[0] ?? ''
  return first
    .split(',')
    .map((h) => h.trim().replace(/^"|"$/g, '').replace(/^\uFEFF/, ''))
    .filter((h) => h.length > 0)
}

/** 组装灌装 multipart（preview / apply 共用，mode 区分；relation/attribute 逗号连接） */
function buildIngestForm(
  file: File,
  cfg: { concept: string; keyColumn: string; relationColumns: string[]; attributeColumns: string[]; skipRows: number },
  mode: 'preview' | 'apply',
): FormData {
  const fd = new FormData()
  fd.append('csv', file)
  fd.append('concept', cfg.concept)
  fd.append('key_column', cfg.keyColumn)
  fd.append('relation_columns', cfg.relationColumns.join(','))
  fd.append('attribute_columns', cfg.attributeColumns.join(','))
  fd.append('skip_rows', String(cfg.skipRows))
  fd.append('mode', mode)
  return fd
}

function attrSummary(attrs?: Record<string, unknown>): string {
  if (!attrs) return '—'
  const parts = Object.entries(attrs).map(([k, v]) => `${k}=${v === null || v === undefined ? '' : String(v)}`)
  return parts.length ? parts.join(', ') : '—'
}

function relSummary(rels?: SpecInstance['relations']): string {
  if (!rels || rels.length === 0) return '—'
  return rels.map((r) => `${r.rel}→${r.target}`).join(', ')
}

const DRAFT_COLUMNS: ColumnsType<SpecInstance> = [
  { title: '实例名', dataIndex: 'name', width: 180, render: (v) => <Typography.Text code style={{ fontSize: 12 }}>{String(v)}</Typography.Text> },
  { title: '概念', dataIndex: 'concept', width: 140 },
  { title: '属性', dataIndex: 'attributes', render: (_, r) => <Typography.Text type="secondary" style={{ fontSize: 12 }}>{attrSummary(r.attributes)}</Typography.Text> },
  { title: '关系', dataIndex: 'relations', render: (_, r) => <Typography.Text type="secondary" style={{ fontSize: 12 }}>{relSummary(r.relations)}</Typography.Text> },
]

function CsvIngest({ ontology, onIngested }: { ontology: Ontology; onIngested?: () => void }) {
  const { showToast } = useUI()
  const [concepts, setConcepts] = useState<SpecConcept[]>([])
  const [specErr, setSpecErr] = useState<string | null>(null)

  const [file, setFile] = useState<File | null>(null)
  const [headers, setHeaders] = useState<string[]>([])
  const [concept, setConcept] = useState<string | undefined>()
  const [keyColumn, setKeyColumn] = useState<string | undefined>()
  const [relationColumns, setRelationColumns] = useState<string[]>([])
  const [attributeColumns, setAttributeColumns] = useState<string[]>([])
  const [skipRows, setSkipRows] = useState<number>(0)

  const [previewBusy, setPreviewBusy] = useState(false)
  const [preview, setPreview] = useState<CsvIngestPreview | null>(null)
  const [previewErr, setPreviewErr] = useState<string | null>(null)

  const [applyBusy, setApplyBusy] = useState(false)
  const [applyErrors, setApplyErrors] = useState<ValidationError[]>([])

  // 目标本体的 Spec 概念（供 concept 选择；404 视为尚未保存）
  useEffect(() => {
    let alive = true
    api
      .getSpec(ontology.id)
      .then((s) => {
        if (alive) {
          setConcepts(s.concepts ?? [])
          setSpecErr(null)
        }
      })
      .catch((e: any) => {
        if (!alive) return
        setConcepts([])
        if (!(e instanceof ApiError && e.status === 404)) setSpecErr(e?.message ?? 'Spec 加载失败')
      })
    return () => {
      alive = false
    }
  }, [ontology.id])

  // 切换本体：重置映射与预览（避免跨本体残留）
  useEffect(() => {
    setFile(null)
    setHeaders([])
    setConcept(undefined)
    setKeyColumn(undefined)
    setRelationColumns([])
    setAttributeColumns([])
    setSkipRows(0)
    setPreview(null)
    setPreviewErr(null)
    setApplyErrors([])
  }, [ontology.id])

  const pickFile = async (f: File) => {
    try {
      const text = await f.text()
      setFile(f)
      setHeaders(parseCsvHeader(text))
      setPreview(null)
      setPreviewErr(null)
      setApplyErrors([])
    } catch (e: any) {
      showToast(e?.message ?? '文件读取失败', 'err')
    }
  }

  const validate = (): boolean => {
    if (!file) {
      showToast('请先上传 CSV 文件', 'err')
      return false
    }
    if (!concept) {
      showToast('请选择目标概念（concept）', 'err')
      return false
    }
    if (!keyColumn) {
      showToast('请选择主键列（key_column）', 'err')
      return false
    }
    return true
  }

  const cfg = { concept: concept ?? '', keyColumn: keyColumn ?? '', relationColumns, attributeColumns, skipRows }

  const doPreview = async () => {
    if (!validate() || !file) return
    setPreviewBusy(true)
    setPreviewErr(null)
    setApplyErrors([])
    try {
      const r = await api.ingestCsvPreview(ontology.id, buildIngestForm(file, cfg, 'preview'))
      setPreview(r)
    } catch (e: any) {
      setPreview(null)
      setPreviewErr(e?.message ?? '预览失败')
    } finally {
      setPreviewBusy(false)
    }
  }

  const doApply = async () => {
    if (!validate() || !file) return
    setApplyBusy(true)
    setApplyErrors([])
    try {
      const r = await api.ingestCsvApply(ontology.id, buildIngestForm(file, cfg, 'apply'))
      showToast(`灌装成功（version ${r.version}）`)
      setPreview(null)
      onIngested?.()
    } catch (e: any) {
      if (e instanceof ApiError && e.validationErrors?.length) setApplyErrors(e.validationErrors)
      showToast(e?.message ?? '灌装失败', 'err')
    } finally {
      setApplyBusy(false)
    }
  }

  return (
    <>
      <div className="onto-sec" style={{ marginTop: 0 }}>
        <span className="onto-sec-title">CSV 灌装（REQ-96 P2a · 同名映射）</span>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          目标本体「{ontology.name}」· 概念 {concepts.length} 个
        </Typography.Text>
      </div>

      {specErr && <Alert type="warning" showIcon style={{ marginBottom: 10 }} message="Spec 加载失败" description={specErr} />}
      {!specErr && concepts.length === 0 && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 10 }}
          message="该本体尚无概念"
          description="CSV 灌装需目标概念与列映射；请先在 S2 保存含概念的 Spec 后再灌装。"
        />
      )}

      <Upload.Dragger
        accept=".csv,.txt"
        multiple={false}
        showUploadList={false}
        beforeUpload={(f) => {
          void pickFile(f)
          return false
        }}
      >
        <p className="ant-upload-drag-icon">
          <InboxOutlined />
        </p>
        <p className="ant-upload-text">点击或拖拽 CSV 文件到此区域</p>
        <p className="ant-upload-hint">首行作为表头；行按同名映射生成 Spec 实例草稿，确认后校验入库为新版本</p>
      </Upload.Dragger>

      {file && (
        <div className="onto-sec">
          <span className="onto-sec-title">文件</span>
          <Tag color="blue" style={{ margin: 0 }}>
            {file.name}
          </Tag>
          <Tag style={{ margin: 0 }}>{headers.length} 列</Tag>
        </div>
      )}

      {file && (
        <div className="onto-csv-grid">
          <div className="onto-csv-field">
            <span className="cfg-label">目标概念 concept（必填）</span>
            <Select
              style={{ width: '100%' }}
              value={concept}
              onChange={setConcept}
              placeholder={concepts.length ? '选择 Spec 概念' : '无可用概念'}
              options={concepts.map((c) => ({ value: c.name, label: `${c.label || c.name}（${c.name}）` }))}
            />
          </div>
          <div className="onto-csv-field">
            <span className="cfg-label">主键列 key_column（必填）</span>
            <Select
              style={{ width: '100%' }}
              value={keyColumn}
              onChange={setKeyColumn}
              placeholder="选择 CSV 表头列"
              options={headers.map((h) => ({ value: h, label: h }))}
            />
          </div>
          <div className="onto-csv-field">
            <span className="cfg-label">关系列 relation_columns（可选）</span>
            <Select
              mode="multiple"
              allowClear
              style={{ width: '100%' }}
              value={relationColumns}
              onChange={setRelationColumns}
              placeholder="选择列（逗号连接）"
              options={headers.map((h) => ({ value: h, label: h }))}
            />
          </div>
          <div className="onto-csv-field">
            <span className="cfg-label">属性列 attribute_columns（可选）</span>
            <Select
              mode="multiple"
              allowClear
              style={{ width: '100%' }}
              value={attributeColumns}
              onChange={setAttributeColumns}
              placeholder="选择列（逗号连接）"
              options={headers.map((h) => ({ value: h, label: h }))}
            />
          </div>
          <div className="onto-csv-field">
            <span className="cfg-label">跳过行 skip_rows</span>
            <InputNumber
              min={0}
              max={100000}
              value={skipRows}
              onChange={(v) => setSkipRows(typeof v === 'number' ? v : 0)}
              style={{ width: '100%' }}
            />
          </div>
        </div>
      )}

      {file && (
        <Space size={10} wrap style={{ marginTop: 10 }}>
          <Button type="primary" loading={previewBusy} onClick={doPreview}>
            预览草稿
          </Button>
          <Button type="primary" ghost loading={applyBusy} disabled={!preview} onClick={doApply}>
            确认灌装
          </Button>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            预览仅统计与草稿；确认后经校验入库并递增版本（400 校验失败展示结构化错误）。
          </Typography.Text>
        </Space>
      )}

      {previewErr && <Alert type="error" showIcon style={{ marginTop: 10 }} message="预览失败" description={previewErr} />}

      {preview && (
        <>
          <div className="onto-sec">
            <span className="onto-sec-title">预览结果</span>
          </div>
          <div className="onto-csv-stats">
            <span>
              读取行 <b>{preview.stats?.rows_read ?? 0}</b>
            </span>
            <span>
              生成实例 <b>{preview.stats?.instances_generated ?? 0}</b>
            </span>
            <span>
              跳过空主键 <b>{preview.stats?.skipped_empty_key ?? 0}</b>
            </span>
          </div>
          {preview.warnings && preview.warnings.length > 0 && (
            <Alert
              type="warning"
              showIcon
              style={{ marginBottom: 10 }}
              message={`警告 ${preview.warnings.length} 条`}
              description={
                <ul className="onto-report-list">
                  {preview.warnings.map((w, i) => (
                    <li key={i}>{w}</li>
                  ))}
                </ul>
              }
            />
          )}
          <Table<SpecInstance>
            rowKey={(r, i) => `${r.name}-${i ?? 0}`}
            columns={DRAFT_COLUMNS}
            dataSource={(preview.draft ?? []).slice(0, 20)}
            pagination={false}
            size="small"
            scroll={{ x: 'max-content' }}
            locale={{ emptyText: '无草稿实例' }}
          />
          {(preview.draft?.length ?? 0) > 20 && (
            <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 6 }}>
              仅展示前 20 条（共 {preview.draft?.length ?? 0} 条）
            </Typography.Text>
          )}
        </>
      )}

      {applyErrors.length > 0 && (
        <>
          <Alert type="error" showIcon style={{ marginTop: 12 }} message={`灌装校验未通过（${applyErrors.length} 项）`} />
          <Table<ValidationError>
            rowKey={(r) => `${r.path}::${r.message}`}
            columns={ERR_COLUMNS}
            dataSource={applyErrors}
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

      <div style={{ marginTop: 16 }}>
        <VersionDiff ontologyId={ontology.id} refreshSignal={vhRefresh} />
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
// 版本对比（REQ-95：GET /api/ontologies/{id}/diff?from&to）
// ---------------------------------------------------------------------------

/** 元素摘要（按联合类型判别：实例 / 关系 / 概念） */
function diffItemSummary(it: DiffItem): string {
  if ('concept' in it) return `概念 ${String(it.concept)}`
  if ('from' in it && 'to' in it) return `${String(it.from)} → ${String(it.to)}`
  if ('definition' in it && it.definition) return String(it.definition)
  if ('parents' in it && (it.parents?.length ?? 0) > 0) return `父 ${(it.parents ?? []).join(', ')}`
  return ''
}

function fmtDiffValue(v: unknown): string {
  if (v === null || v === undefined) return '（空）'
  if (typeof v === 'string') return v.length > 120 ? `${v.slice(0, 120)}…` : v
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

/** 防御式归一化（后端缺集合字段时不崩） */
function normalizeCollection(c?: DiffCollection): DiffCollection {
  return { added: c?.added ?? [], removed: c?.removed ?? [], changed: c?.changed ?? [] }
}

const IMPACT_COLUMNS: ColumnsType<DiffImpact> = [
  { title: '名称', dataIndex: 'name', render: (v) => <Typography.Text code style={{ fontSize: 12 }}>{String(v)}</Typography.Text> },
  { title: '被引用次数', dataIndex: 'referenced_by', width: 140 },
]

/** 客户端生成 Markdown 对比报告（REQ-95） */
function buildDiffMarkdown(r: DiffResult): string {
  const lines: string[] = [`# 版本对比报告 v${r.from_version} → v${r.to_version}`, '']
  const sections: [string, DiffCollection][] = [
    ['概念', normalizeCollection(r.concepts)],
    ['关系', normalizeCollection(r.relations)],
    ['实例', normalizeCollection(r.instances)],
  ]
  for (const [title, c] of sections) {
    lines.push(`## ${title}`, '')
    lines.push(`### 新增（${c.added.length}）`)
    c.added.forEach((it) => lines.push(`- ${it.name}${diffItemSummary(it) ? ` — ${diffItemSummary(it)}` : ''}`))
    lines.push('', `### 删除（${c.removed.length}）`)
    c.removed.forEach((it) => lines.push(`- ${it.name}${diffItemSummary(it) ? ` — ${diffItemSummary(it)}` : ''}`))
    lines.push('', `### 修改（${c.changed.length}）`)
    c.changed.forEach((ch) => {
      lines.push(`- ${ch.name}`)
      Object.entries(ch.fields ?? {}).forEach(([f, d]) => lines.push(`  - ${f}: \`${fmtDiffValue(d?.from)}\` → \`${fmtDiffValue(d?.to)}\``))
    })
    lines.push('')
  }
  lines.push('## 引用影响', '', '| 名称 | 被引用次数 |', '| --- | --- |')
  ;[...(r.impact ?? [])]
    .sort((a, b) => (b.referenced_by ?? 0) - (a.referenced_by ?? 0))
    .forEach((i) => lines.push(`| ${i.name} | ${i.referenced_by} |`))
  return lines.join('\n')
}

function DiffSectionLabel({ title, c }: { title: string; c: DiffCollection }) {
  return (
    <Space size={8} wrap>
      <span>{title}</span>
      <Tag color="green" style={{ margin: 0 }}>
        +{c.added.length}
      </Tag>
      <Tag color="red" style={{ margin: 0 }}>
        -{c.removed.length}
      </Tag>
      <Tag color="orange" style={{ margin: 0 }}>
        ~{c.changed.length}
      </Tag>
    </Space>
  )
}

/** 单集合渲染：added/removed 双栏并排 + changed 字段级 from→to */
function DiffCollectionView({ collection }: { collection: DiffCollection }) {
  const { added, removed, changed } = collection
  if (added.length + removed.length + changed.length === 0) {
    return (
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        无变化
      </Typography.Text>
    )
  }
  return (
    <div className="onto-diff-collection">
      <div className="onto-diff-cols">
        <div className="onto-diff-col add">
          <div className="onto-diff-col-head">
            新增 <Tag color="green" style={{ margin: 0 }}>{added.length}</Tag>
          </div>
          {added.length === 0 ? (
            <span className="onto-diff-empty">—</span>
          ) : (
            <ul className="onto-diff-list">
              {added.map((it, i) => (
                <li key={`${it.name}-${i}`}>
                  <span className="onto-diff-name">{it.name}</span>
                  {diffItemSummary(it) && <span className="onto-diff-sub">{diffItemSummary(it)}</span>}
                </li>
              ))}
            </ul>
          )}
        </div>
        <div className="onto-diff-col del">
          <div className="onto-diff-col-head">
            删除 <Tag color="red" style={{ margin: 0 }}>{removed.length}</Tag>
          </div>
          {removed.length === 0 ? (
            <span className="onto-diff-empty">—</span>
          ) : (
            <ul className="onto-diff-list">
              {removed.map((it, i) => (
                <li key={`${it.name}-${i}`}>
                  <span className="onto-diff-name">{it.name}</span>
                  {diffItemSummary(it) && <span className="onto-diff-sub">{diffItemSummary(it)}</span>}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
      <div className="onto-diff-changed">
        <div className="onto-diff-col-head">
          修改 <Tag color="orange" style={{ margin: 0 }}>{changed.length}</Tag>
        </div>
        {changed.length === 0 ? (
          <span className="onto-diff-empty">—</span>
        ) : (
          changed.map((c, i) => (
            <div className="onto-diff-changed-item" key={`${c.name}-${i}`}>
              <span className="onto-diff-name">{c.name}</span>
              <ul className="onto-diff-fields">
                {Object.entries(c.fields ?? {}).map(([f, ch]) => (
                  <li key={f}>
                    <Typography.Text code style={{ fontSize: 12 }}>
                      {f}
                    </Typography.Text>
                    <span className="onto-diff-from">{fmtDiffValue(ch?.from)}</span>
                    <span className="onto-diff-arrow">→</span>
                    <span className="onto-diff-to">{fmtDiffValue(ch?.to)}</span>
                  </li>
                ))}
              </ul>
            </div>
          ))
        )}
      </div>
    </div>
  )
}

function VersionDiff({ ontologyId, refreshSignal }: { ontologyId: string; refreshSignal: number }) {
  const { showToast } = useUI()
  const [versions, setVersions] = useState<VersionMeta[] | null>(null)
  const [vErr, setVErr] = useState<string | null>(null)
  const [vLoading, setVLoading] = useState(false)
  const [from, setFrom] = useState<number | undefined>()
  const [to, setTo] = useState<number | undefined>()
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<DiffResult | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    let alive = true
    setVLoading(true)
    setVErr(null)
    api
      .listVersions(ontologyId)
      .then((r) => {
        if (alive) setVersions(r.versions ?? [])
      })
      .catch((e: any) => {
        if (alive) {
          setVersions(null)
          setVErr(e?.message ?? '版本列表获取失败')
        }
      })
      .finally(() => {
        if (alive) setVLoading(false)
      })
    return () => {
      alive = false
    }
  }, [ontologyId, refreshSignal])

  // 切换本体：重置选择与结果
  useEffect(() => {
    setFrom(undefined)
    setTo(undefined)
    setResult(null)
    setErr(null)
  }, [ontologyId])

  // 默认 from=倒数第二、to=最新
  useEffect(() => {
    if (!versions || versions.length === 0) return
    const asc = versions
    setTo((cur) => cur ?? asc[asc.length - 1].version)
    setFrom((cur) => cur ?? (asc.length >= 2 ? asc[asc.length - 2].version : asc[0].version))
  }, [versions])

  const run = async () => {
    if (from == null || to == null) {
      showToast('请选择对比版本', 'err')
      return
    }
    if (from === to) {
      showToast('请选择两个不同版本', 'err')
      return
    }
    setBusy(true)
    setErr(null)
    setResult(null)
    try {
      const r = await api.diffOntologyVersions(ontologyId, from, to)
      setResult(r)
    } catch (e: any) {
      setErr(e?.message ?? '版本对比失败')
    } finally {
      setBusy(false)
    }
  }

  const exportMd = () => {
    if (!result) return
    const blob = new Blob([buildDiffMarkdown(result)], { type: 'text/markdown;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `diff_${ontologyId}_v${result.from_version}_v${result.to_version}.md`
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  }

  const versionOptions = (versions ?? []).map((v) => ({ value: v.version, label: `v${v.version} · ${v.created_at}` }))

  return (
    <>
      <div className="onto-sec">
        <span className="onto-sec-title">版本对比（REQ-95，三集合结构化 diff + 引用影响）</span>
        <span className="hit-spacer" />
        {result && (
          <Button size="small" icon={<DownloadOutlined />} onClick={exportMd}>
            导出 Markdown 报告
          </Button>
        )}
      </div>

      {vErr ? (
        <Alert type="warning" showIcon message="版本列表获取失败" description={vErr} />
      ) : (versions?.length ?? 0) < 2 ? (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          至少需要两个版本才能对比（每次保存 / 导入 / 灌装自动留快照）。
        </Typography.Text>
      ) : (
        <>
          <Space size={8} wrap>
            <span className="cfg-label">从</span>
            <Select size="small" style={{ width: 220 }} value={from} onChange={setFrom} options={versionOptions} loading={vLoading} />
            <span className="onto-diff-arrow">→</span>
            <span className="cfg-label">到</span>
            <Select size="small" style={{ width: 220 }} value={to} onChange={setTo} options={versionOptions} loading={vLoading} />
            <Button size="small" type="primary" loading={busy} onClick={run}>
              对比
            </Button>
          </Space>

          {err && <Alert type="error" showIcon style={{ marginTop: 10 }} message="版本对比失败" description={err} />}

          {result && (
            <div style={{ marginTop: 12 }}>
              <Collapse
                defaultActiveKey={['concepts', 'relations', 'instances']}
                items={[
                  {
                    key: 'concepts',
                    label: <DiffSectionLabel title="概念" c={normalizeCollection(result.concepts)} />,
                    children: <DiffCollectionView collection={normalizeCollection(result.concepts)} />,
                  },
                  {
                    key: 'relations',
                    label: <DiffSectionLabel title="关系" c={normalizeCollection(result.relations)} />,
                    children: <DiffCollectionView collection={normalizeCollection(result.relations)} />,
                  },
                  {
                    key: 'instances',
                    label: <DiffSectionLabel title="实例" c={normalizeCollection(result.instances)} />,
                    children: <DiffCollectionView collection={normalizeCollection(result.instances)} />,
                  },
                ]}
              />
              <div className="onto-sec">
                <span className="onto-sec-title">引用影响（变更元素被引用次数）</span>
              </div>
              {(result.impact ?? []).length === 0 ? (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  无引用影响记录
                </Typography.Text>
              ) : (
                <Table<DiffImpact>
                  rowKey="name"
                  columns={IMPACT_COLUMNS}
                  dataSource={[...(result.impact ?? [])].sort((a, b) => (b.referenced_by ?? 0) - (a.referenced_by ?? 0))}
                  pagination={false}
                  size="small"
                  scroll={{ x: 'max-content' }}
                />
              )}
            </div>
          )}
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
// S4 可视化（React Flow 交互式图谱，@xyflow/react v12）
// ---------------------------------------------------------------------------

const NODE_W = 168
const COL_GAP = 46
const ROW_STEP = 112
const PAD = 32

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

function layoutGraph(spec: Spec): NodePos[] {
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
  const width = PAD * 2 + maxCols * NODE_W + Math.max(0, maxCols - 1) * COL_GAP
  const nodes: NodePos[] = []
  for (const [d, cs] of rows) {
    cs.sort((a, b) => a.name.localeCompare(b.name))
    const rowW = cs.length * NODE_W + Math.max(0, cs.length - 1) * COL_GAP
    const startX = (width - rowW) / 2
    cs.forEach((c, i) => nodes.push({ c, x: startX + i * (NODE_W + COL_GAP), y: PAD + d * ROW_STEP }))
  }
  return nodes
}

/** 节点数据（React Flow node.data）：显示名 + 原名 + 实例数 + 定义（悬停/详情） */
interface ConceptData extends Record<string, unknown> {
  label: string
  name: string
  count: number
  definition?: string
}
type ConceptFlowNode = Node<ConceptData, 'concept'>

/** 每个概念的实例数（节点徽标 / 详情） */
function instanceCounts(spec: Spec): Map<string, number> {
  const m = new Map<string, number>()
  for (const inst of spec.instances ?? []) m.set(inst.concept, (m.get(inst.concept) ?? 0) + 1)
  return m
}

/** Spec → React Flow 节点：沿用「按父深度分层」布局给初始坐标，之后由 React Flow 接管拖拽/缩放 */
function buildNodes(spec: Spec): ConceptFlowNode[] {
  const counts = instanceCounts(spec)
  return layoutGraph(spec).map(({ c, x, y }) => ({
    id: c.name,
    type: 'concept' as const,
    position: { x, y },
    style: { width: NODE_W },
    data: { label: c.label || c.name, name: c.name, count: counts.get(c.name) ?? 0, definition: c.definition },
  }))
}

/**
 * Spec → React Flow 边：
 *  - 关系：实线 + 标签 + 箭头（source=from → target=to）；
 *  - 父子：虚线（source=父 → target=子，保证自上而下走向）。
 */
function buildEdges(spec: Spec): Edge[] {
  const names = new Set((spec.concepts ?? []).map((c) => c.name))
  const edges: Edge[] = []
  for (const r of spec.relations ?? []) {
    if (!names.has(r.from) || !names.has(r.to)) continue
    edges.push({
      id: `rel:${r.name}:${r.from}:${r.to}`,
      source: r.from,
      target: r.to,
      label: r.label || r.name,
      type: 'smoothstep',
      className: 'onto-flow-edge-rel',
      markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 },
    })
  }
  for (const c of spec.concepts ?? []) {
    for (const p of c.parents ?? []) {
      if (!names.has(p)) continue
      edges.push({
        id: `parent:${p}:${c.name}`,
        source: p,
        target: c.name,
        type: 'smoothstep',
        className: 'onto-flow-edge-parent',
        style: { strokeDasharray: '5 4' },
      })
    }
  }
  return edges
}

/** 自定义概念节点：名称 + 实例数徽标；title 承载定义 */
function ConceptNode({ data, selected }: NodeProps<ConceptFlowNode>) {
  return (
    <div className={`onto-flow-node${selected ? ' selected' : ''}`} title={data.definition || data.label}>
      <Handle type="target" position={Position.Top} className="onto-flow-handle" />
      <span className="onto-flow-node-label">{data.label}</span>
      {data.count > 0 && <span className="onto-flow-node-badge">{data.count}</span>}
      <Handle type="source" position={Position.Bottom} className="onto-flow-handle" />
    </div>
  )
}

// nodeTypes 必须定义在组件外，避免每次渲染重建导致 React Flow 重挂载
const nodeTypes: NodeTypes = { concept: ConceptNode }

function S4Graph({ spec }: { spec: Spec | null }) {
  const hasConcepts = !!spec && (spec.concepts?.length ?? 0) > 0
  const initialNodes = useMemo(() => (spec ? buildNodes(spec) : []), [spec])
  const initialEdges = useMemo(() => (spec ? buildEdges(spec) : []), [spec])
  const [nodes, setNodes, onNodesChange] = useNodesState<ConceptFlowNode>(initialNodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>(initialEdges)
  const [selectedId, setSelectedId] = useState<string | null>(null)

  // Spec 变化时重置图谱（拖拽后的坐标不跨 Spec 版本保留）
  useEffect(() => {
    setNodes(initialNodes)
    setEdges(initialEdges)
    setSelectedId(null)
  }, [initialNodes, initialEdges, setNodes, setEdges])

  const counts = useMemo(() => (spec ? instanceCounts(spec) : new Map<string, number>()), [spec])
  const selected = useMemo(
    () => (spec && selectedId ? spec.concepts.find((c) => c.name === selectedId) ?? null : null),
    [spec, selectedId],
  )
  const selectedInstances = useMemo(
    () => (spec && selected ? (spec.instances ?? []).filter((i) => i.concept === selected.name) : []),
    [spec, selected],
  )

  if (!hasConcepts || !spec) {
    return (
      <div className="work-empty" style={{ minHeight: 220 }}>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无概念可可视化；请先在 S1 创建 / 导入，或到 S2 保存 Spec" />
      </div>
    )
  }

  return (
    <Splitter className="onto-flow-split" orientation="horizontal">
      <Splitter.Panel defaultSize="68%" min="40%">
        <div className="onto-flow-pane">
          <ReactFlow
            key={initialNodes.map((n) => n.id).join('|')}
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            nodeTypes={nodeTypes}
            fitView
            fitViewOptions={{ padding: 0.2 }}
            minZoom={0.2}
            maxZoom={2}
            nodesConnectable={false}
            onNodeClick={(_, n) => setSelectedId(n.id)}
            onPaneClick={() => setSelectedId(null)}
            className="onto-flow"
          >
            <Background variant={BackgroundVariant.Dots} gap={18} size={1.5} color="#c9cee0" />
            <MiniMap position="top-right" pannable zoomable nodeColor="#c9cef3" maskColor="rgba(246, 247, 251, 0.72)" />
            <Controls showInteractive={false} position="bottom-left" />
          </ReactFlow>
        </div>
      </Splitter.Panel>
      <Splitter.Panel min="22%">
        <div className="onto-flow-info">
          <div className="onto-flow-info-title">图例</div>
          <div className="onto-flow-legend">
            <span className="onto-flow-legend-line rel" />
            <span>实线 = 关系（from → to）</span>
          </div>
          <div className="onto-flow-legend">
            <span className="onto-flow-legend-line parent" />
            <span>虚线 = 继承（父 → 子）</span>
          </div>
          <div className="onto-flow-legend">
            <span className="onto-flow-legend-badge">n</span>
            <span>节点徽标 = 实例数</span>
          </div>

          <div className="onto-flow-info-title spaced">统计</div>
          <div className="onto-flow-stats">
            <span>概念 <b>{spec.concepts.length}</b></span>
            <span>关系 <b>{spec.relations?.length ?? 0}</b></span>
            <span>实例 <b>{spec.instances?.length ?? 0}</b></span>
          </div>

          <div className="onto-flow-info-title spaced">选中节点</div>
          {selected ? (
            <div className="onto-flow-detail">
              <div className="onto-flow-detail-name">{selected.label || selected.name}</div>
              <div className="onto-flow-detail-key">{selected.name}</div>
              <p className={`onto-flow-detail-def${selected.definition ? '' : ' muted'}`}>
                {selected.definition || '未填写定义'}
              </p>
              <div className="onto-flow-detail-row">
                <span className="onto-flow-detail-label">父概念</span>
                {selected.parents && selected.parents.length > 0 ? (
                  <Space size={4} wrap>
                    {selected.parents.map((p) => (
                      <Tag key={p} style={{ margin: 0 }}>{p}</Tag>
                    ))}
                  </Space>
                ) : (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>—</Typography.Text>
                )}
              </div>
              <div className="onto-flow-detail-row">
                <span className="onto-flow-detail-label">实例</span>
                <Typography.Text style={{ fontSize: 12 }}>{counts.get(selected.name) ?? 0} 个</Typography.Text>
              </div>
              {selectedInstances.length > 0 && (
                <Space size={4} wrap style={{ marginTop: 6 }}>
                  {selectedInstances.slice(0, 12).map((i) => (
                    <Tag key={i.name} color="purple" style={{ margin: 0 }}>{i.name}</Tag>
                  ))}
                  {selectedInstances.length > 12 && (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>等 {selectedInstances.length} 个</Typography.Text>
                  )}
                </Space>
              )}
            </div>
          ) : (
            <p className="onto-flow-hint">点击图中节点查看定义、父概念与实例；滚轮缩放、拖拽平移 / 节点。</p>
          )}
        </div>
      </Splitter.Panel>
    </Splitter>
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

  const [tab, setTab] = useState<'plan' | 'sparql' | 'trace'>('plan')

  // 「方案」页签：既有运行方案卡片 / 新建行 / 运行日志，行为保持不变
  const planPane = (
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

  return (
    <Tabs
      activeKey={tab}
      onChange={(k) => setTab(k as 'plan' | 'sparql' | 'trace')}
      destroyOnHidden
      items={[
        { key: 'plan', label: '方案', children: planPane },
        { key: 'sparql', label: 'SPARQL', children: <SparqlTab profiles={mine} profilesErr={profilesErr} /> },
        { key: 'trace', label: '透视', children: <TraceTab profiles={mine} /> },
      ]}
    />
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
// P1 学习增强：SPARQL 工作台（REQ-92）/ 源码视图（REQ-93）/ 翻译透视（REQ-94）
// ---------------------------------------------------------------------------

/** SPARQL 工作台默认模板（REQ-92：/api/runtime-profiles/{id}/sparql，引擎直连） */
const DEFAULT_SPARQL = `PREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT ?s ?o WHERE { ?s rdf:type ?o } LIMIT 20`

/** 源码视图：超过此体积不渲染，改为下载查看（REQ-93） */
const LARGE_SOURCE = 1_000_000

/** 版本原始源文件格式 → 展示名（与构建平面 original_format 口径一致） */
const FORMAT_LABEL: Record<string, string> = {
  turtle: 'Turtle',
  owl_rdfxml: 'OWL / RDF-XML',
  spec_json: 'Spec JSON',
  csv: 'CSV',
  graphml: 'GraphML',
}

/**
 * Yasgui 实例挂载（vanilla JS → React 桥，REQ-92）：
 *  - 以 endpoint / persistenceId 为生命周期边界，绑定方案变更即销毁重建；查询历史走 Yasgui 自带 localStorage；
 *  - StrictMode 双挂载由 cleanup 的 destroy() 兜底（destroy 会移除其 rootEl 与全局监听）；
 *  - method 用 GET（?query=）匹配运行平面反代：POST 侧要求 application/sparql-query 原文，
 *    而 Yasgui 的 POST 会发 x-www-form-urlencoded，故改用 GET（两端点均支持）。
 */
function YasguiPane({ endpoint, persistenceId }: { endpoint: string; persistenceId: string }) {
  const hostRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const y = new Yasgui(host, {
      // Yasgui 对顶层 config 为浅合并：requestConfig 需给全量，否则会丢失 Accept / 参数默认值
      requestConfig: {
        endpoint,
        method: 'GET',
        acceptHeaderSelect: 'application/sparql-results+json,*/*;q=0.9',
        acceptHeaderGraph: 'application/n-triples,*/*;q=0.9',
        acceptHeaderUpdate: 'text/plain,*/*;q=0.9',
        namedGraphs: [],
        defaultGraphs: [],
        args: [],
        headers: {},
        withCredentials: false,
        adjustQueryBeforeRequest: false,
      },
      persistenceId,
      autoAddOnInit: true,
      copyEndpointOnNewTab: false,
    })
    const tab = y.getTab()
    // 仅在空白新标签页填默认模板；已从 localStorage 恢复的查询保持原样
    if (tab && !tab.getQuery().trim()) tab.setQuery(DEFAULT_SPARQL)
    return () => {
      try {
        y.destroy()
      } catch {
        /* 已销毁 */
      }
      host.innerHTML = ''
    }
  }, [endpoint, persistenceId])

  return <div ref={hostRef} className="onto-yasgui" />
}

/** S5「SPARQL」页签：按本体的运行方案选择器 + Yasgui（每方案一个实例） */
function SparqlTab({ profiles, profilesErr }: { profiles: RuntimeProfile[]; profilesErr: boolean }) {
  const [pid, setPid] = useState<string | undefined>(profiles[0]?.id)

  useEffect(() => {
    if (pid && !profiles.some((p) => p.id === pid)) setPid(profiles[0]?.id)
    else if (!pid && profiles.length > 0) setPid(profiles[0]?.id)
  }, [profiles, pid])

  if (profiles.length === 0) {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        style={{ margin: '24px 0' }}
        description={profilesErr ? '运行平面暂不可达，无法加载运行方案' : '该本体暂无运行方案；先在「方案」页新建并启动'}
      />
    )
  }

  const cur = profiles.find((p) => p.id === pid) ?? null

  return (
    <>
      <div className="onto-sec" style={{ marginTop: 4 }}>
        <span className="onto-sec-title">SPARQL 工作台（Yasgui，REQ-92）</span>
        <span className="hit-spacer" />
        <Select
          size="small"
          value={pid}
          onChange={setPid}
          style={{ width: 260 }}
          options={profiles.map((p) => ({ value: p.id, label: `${p.name}（${PROFILE_BADGE[p.status]?.text ?? p.status}）` }))}
        />
      </div>
      {cur && cur.status !== 'running' && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 10 }}
          message="该方案未运行：查询将被运行平面拒绝（409）"
          description="在「方案」页启动后再执行查询；Yasgui 的查询历史仍保留在浏览器本地。"
        />
      )}
      {cur && (
        <>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            端点 <Typography.Text code style={{ fontSize: 12 }}>{api.sparqlEndpointUrl(cur.id)}</Typography.Text>
            · 结果表格 / 图 / 原始响应三视图由 Yasgui 提供，查询历史存浏览器本地。
          </Typography.Text>
          <YasguiPane key={cur.id} endpoint={api.sparqlEndpointUrl(cur.id)} persistenceId={`onto-sparql-${cur.id}`} />
        </>
      )}
    </>
  )
}

/** S5「透视」页签：翻译透视表（REQ-94，时间倒序，行展开看 SPARQL 原文） */
function TraceTab({ profiles }: { profiles: RuntimeProfile[] }) {
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
        description="该本体暂无运行方案；执行 onto_* 工具后在此查看翻译透视"
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

/**
 * 「查询 & 源码」标签组（REQ-92/93，§4.8.1 同页组织）——本体级入口：
 *  查询 = 自动关联包含本体的 running 方案，绑定其 SPARQL 端点（无则提示 + 禁用态）；
 *  源码 = 版本选择 + CodeMirror 只读源码 + Spec JSON 格式化视图。
 */
function QuerySourceEntry({
  ontology,
  profiles,
  profilesErr,
  spec,
}: {
  ontology: Ontology
  profiles: RuntimeProfile[]
  profilesErr: boolean
  spec: Spec | null
}) {
  const [tab, setTab] = useState('query')
  const running = profiles.find((p) => p.status === 'running' && (p.ontology_ids ?? []).includes(ontology.id)) ?? null

  return (
    <Card
      className="work-card onto-stage-card"
      size="small"
      title={
        <Space size={8} wrap>
          <span>查询 &amp; 源码</span>
          <Tag color="geekblue" style={{ margin: 0 }}>
            REQ-92 / 93
          </Tag>
        </Space>
      }
    >
      <Tabs
        activeKey={tab}
        onChange={setTab}
        destroyOnHidden
        items={[
          {
            key: 'query',
            label: '查询',
            children: running ? (
              <>
                <div className="onto-sec" style={{ marginTop: 4 }}>
                  <span className="onto-sec-title">SPARQL 工作台</span>
                  <Tag color="green" style={{ margin: 0 }}>
                    {running.name} · 运行中
                  </Tag>
                  <span className="hit-spacer" />
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    端点 <Typography.Text code style={{ fontSize: 12 }}>{api.sparqlEndpointUrl(running.id)}</Typography.Text>
                  </Typography.Text>
                </div>
                <YasguiPane endpoint={api.sparqlEndpointUrl(running.id)} persistenceId={`onto-query-${running.id}`} />
              </>
            ) : (
              <>
                <Alert
                  type={profilesErr ? 'warning' : 'info'}
                  showIcon
                  message="启动包含本体的运行方案后可用"
                  description={
                    profilesErr
                      ? '运行平面暂不可达（RUNTIME_MGR_URL :8090），无法自动关联运行方案。'
                      : '在 S5 运行方式新建并启动一个包含本体的运行方案，查询工作台将自动关联该方案端点。'
                  }
                />
                <div className="onto-disabled-pane">SPARQL 工作台未启用</div>
              </>
            ),
          },
          {
            key: 'source',
            label: '源码',
            children: <SourceView ontologyId={ontology.id} currentVersion={ontology.version} spec={spec} />,
          },
        ]}
      />
    </Card>
  )
}

/** 源码视图（REQ-93）：版本选择 + CodeMirror 只读渲染 + Spec JSON 格式化视图 */
function SourceView({ ontologyId, currentVersion, spec }: { ontologyId: string; currentVersion?: number; spec: Spec | null }) {
  const { showToast } = useUI()
  const [list, setList] = useState<VersionMeta[] | null>(null)
  const [listErr, setListErr] = useState<string | null>(null)
  const [listLoading, setListLoading] = useState(false)
  const [version, setVersion] = useState<number | null>(null)
  const [original, setOriginal] = useState<string | null>(null)
  const [origErr, setOrigErr] = useState<string | null>(null)
  const [origLoading, setOrigLoading] = useState(false)
  const [tooLarge, setTooLarge] = useState(false)
  const [sub, setSub] = useState<'original' | 'spec'>('original')

  // 版本列表（失败 → 回退仅当前版本，隐藏选择器）
  useEffect(() => {
    let alive = true
    setListLoading(true)
    setListErr(null)
    api
      .listVersions(ontologyId)
      .then((r) => {
        if (alive) setList(r.versions ?? [])
      })
      .catch((e: any) => {
        if (alive) {
          setList(null)
          setListErr(e.message)
        }
      })
      .finally(() => {
        if (alive) setListLoading(false)
      })
    return () => {
      alive = false
    }
  }, [ontologyId])

  // 默认选中最新含原始源文件的版本，否则最后一版
  useEffect(() => {
    if (!list || list.length === 0) {
      setVersion(null)
      return
    }
    const pick = [...list].reverse().find((v) => v.has_original) ?? list[list.length - 1]
    setVersion(pick.version)
  }, [list])

  // 版本列表不可用 → 回退当前版本
  useEffect(() => {
    if (listErr && currentVersion) setVersion(currentVersion)
  }, [listErr, currentVersion])

  const meta = list?.find((v) => v.version === version) ?? null

  // 原始源文件（按体积与 has_original 决定是否拉取）
  useEffect(() => {
    if (version == null) {
      setOriginal(null)
      setTooLarge(false)
      return
    }
    if (meta && !meta.has_original) {
      setOriginal(null)
      setOrigErr(null)
      setTooLarge(false)
      return
    }
    if (meta && (meta.original_size ?? 0) > LARGE_SOURCE) {
      setOriginal(null)
      setTooLarge(true)
      return
    }
    let alive = true
    setOrigLoading(true)
    setOrigErr(null)
    setTooLarge(false)
    api
      .getVersionOriginal(ontologyId, version)
      .then((t) => {
        if (!alive) return
        if (t.length > LARGE_SOURCE) {
          setOriginal(null)
          setTooLarge(true)
        } else {
          setOriginal(t)
        }
      })
      .catch((e: any) => {
        if (alive) {
          setOriginal(null)
          setOrigErr(e.message)
        }
      })
      .finally(() => {
        if (alive) setOrigLoading(false)
      })
    return () => {
      alive = false
    }
  }, [ontologyId, version, meta?.has_original, meta?.original_size])

  const copyOriginal = () => {
    navigator.clipboard
      ?.writeText(original ?? '')
      .then(() => showToast('源码已复制'))
      .catch(() => showToast('复制失败', 'err'))
  }

  const specText = spec ? JSON.stringify(spec, null, 2) : ''
  const dlUrl = api.versionOriginalUrl(ontologyId, version ?? currentVersion ?? 1)

  return (
    <Tabs
      size="small"
      activeKey={sub}
      onChange={(k) => setSub(k as 'original' | 'spec')}
      items={[
        {
          key: 'original',
          label: '原始源文件',
          children: (
            <>
              <div className="onto-sec" style={{ marginTop: 4 }}>
                {listErr ? (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    版本列表不可用（{listErr}），仅显示当前版本 v{currentVersion ?? '—'}
                  </Typography.Text>
                ) : (
                  <>
                    <span className="onto-sec-title">版本</span>
                    <Select
                      size="small"
                      value={version ?? undefined}
                      loading={listLoading}
                      onChange={setVersion}
                      style={{ width: 260 }}
                      placeholder="选择版本"
                      options={(list ?? []).map((v) => ({
                        value: v.version,
                        label: `v${v.version} · ${v.created_at}${v.has_original ? '' : '（无源文件）'}`,
                      }))}
                    />
                  </>
                )}
                <span className="hit-spacer" />
                {meta?.original_format && (
                  <Tag color="blue" style={{ margin: 0 }}>
                    {FORMAT_LABEL[meta.original_format] ?? meta.original_format}
                  </Tag>
                )}
                {original != null && (
                  <Button size="small" icon={<CopyOutlined />} onClick={copyOriginal}>
                    复制
                  </Button>
                )}
              </div>
              {origErr ? (
                <Alert type="warning" showIcon message="原始源文件获取失败" description={origErr} />
              ) : tooLarge ? (
                <Alert
                  type="info"
                  showIcon
                  message="源文件超过 1MB，已切换为下载查看"
                  description={
                    <Button size="small" icon={<DownloadOutlined />} href={dlUrl} download target="_blank" rel="noreferrer">
                      下载查看
                    </Button>
                  }
                />
              ) : origLoading ? (
                <Spin size="small" />
              ) : original != null ? (
                <div className="onto-cm-wrap">
                  <CodeMirror
                    value={original}
                    readOnly
                    editable={false}
                    height="360px"
                    basicSetup={{ lineNumbers: true, foldGutter: true, highlightActiveLine: false }}
                    extensions={[EditorView.lineWrapping]}
                  />
                </div>
              ) : meta && !meta.has_original ? (
                <Typography.Text type="secondary">
                  该版本无原始源文件（由编辑 / 灌装产生，仅存 Spec 快照）
                </Typography.Text>
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="选择版本查看原始源文件" />
              )}
            </>
          ),
        },
        {
          key: 'spec',
          label: 'Spec JSON',
          children: spec ? (
            <div className="onto-cm-wrap">
              <CodeMirror
                value={specText}
                readOnly
                editable={false}
                height="360px"
                basicSetup={{ lineNumbers: true, foldGutter: true, highlightActiveLine: false }}
                extensions={[EditorView.lineWrapping]}
              />
            </div>
          ) : (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚未保存 Spec（S2 保存后可在此查看格式化 JSON）" />
          ),
        },
      ]}
    />
  )
}

// ---------------------------------------------------------------------------
// S6 对外暴露
// ---------------------------------------------------------------------------

function S6Expose({ ontology, profiles }: { ontology: Ontology; profiles: RuntimeProfile[] }) {
  const { showToast } = useUI()
  const running = profiles.find((p) => p.status === 'running' && (p.ontology_ids ?? []).includes(ontology.id))
  const [guide, setGuide] = useState<string | null>(null)
  const [guideErr, setGuideErr] = useState<string | null>(null)
  const [guideLoading, setGuideLoading] = useState(false)

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

      <Alert
        type="info"
        showIcon
        style={{ marginTop: 14 }}
        message="SPARQL 工作台与翻译透视已迁至 S5 运行方式（方案 | SPARQL | 透视）与本页「查询 & 源码」入口"
        description="SPARQL 工作台（REQ-92）按运行方案绑定；翻译透视（REQ-94）在 S5「透视」页签；源码视图（REQ-93）在「查询 & 源码 → 源码」。"
      />
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
