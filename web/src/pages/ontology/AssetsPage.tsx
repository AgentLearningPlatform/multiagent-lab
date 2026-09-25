import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Empty,
  Form,
  Input,
  Modal,
  Popconfirm,
  Result,
  Segmented,
  Space,
  Table,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  BranchesOutlined,
  CheckCircleOutlined,
  CodeOutlined,
  DeleteOutlined,
  DownloadOutlined,
  EditOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { api, ApiError } from '../../api/client'
import type { ArtifactMeta, Ontology, RuntimeProfile, Spec, ValidationError } from '../../api/types'
import { useUI } from '../../store/ui'
import { ERR_COLUMNS, ReloadHintAlert, sourceTag, stageDoneFlags, ontoStatus, type ValidationState } from './shared'
import SpecGraph from './components/SpecGraph'
import Graph3D from './components/Graph3D'
import WebVowlView from './components/WebVowlView'
import GraphEditor from './components/GraphEditor'
import SourceView from './components/SourceView'
import CsvIngestPane from './components/CsvIngestPane'

// ---------------------------------------------------------------------------
// 本体资产（AssetsPage，REQ-104 ③）：全部已构建本体统一管理
//   列表（来源/形态/版本/构建段完成度/被引用）+ 详情工作区 Tabs：
//   Spec 编辑 | 校验 | 版本（含源码视图 REQ-93）| 产物 | 可视化 | TTL 导出 | CSV 灌装（REQ-96）
//   正交红线：不出现任何引擎、端口、启停配置（运行看本体运行栏）
// ---------------------------------------------------------------------------

function emptySpec(name: string): Spec {
  return { name, description: '', concepts: [], relations: [], instances: [] }
}

function StageDots({ flags }: { flags: boolean[] }) {
  const ready = flags.slice(0, 4).filter(Boolean).length
  return (
    <span className="onto-dots" title={`S1~S4 构建段已就绪 ${ready}/4（运行段见本体运行栏）`}>
      {flags.slice(0, 4).map((done, i) => (
        <i key={i} className={`onto-dot${done ? ' on' : ''}`} />
      ))}
    </span>
  )
}

export default function AssetsPage() {
  const { showToast } = useUI()
  const [ontos, setOntos] = useState<Ontology[]>([])
  const [profiles, setProfiles] = useState<RuntimeProfile[]>([])
  const [listErr, setListErr] = useState<string | null>(null)
  const [activeId, setActiveId] = useState<string | null>(null)

  const [spec, setSpec] = useState<Spec | null>(null)
  const [specErr, setSpecErr] = useState<string | null>(null)
  const [specLoading, setSpecLoading] = useState(false)
  const [specTick, setSpecTick] = useState(0)

  const [validations, setValidations] = useState<Record<string, ValidationState>>({})
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
      .then(setProfiles)
      .catch(() => setProfiles([]))
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

  /** 被 N 套方案引用（只读徽标，增强正交可见性） */
  const refCount = (o: Ontology) => profiles.filter((p) => (p.ontology_ids ?? []).includes(o.id)).length

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

  const refreshAfterSave = () => {
    reloadOntos()
    setSpecTick((t) => t + 1)
  }

  return (
    <div className="work-main">
      {/* 资产选择条（统一列表）：构建段完成度 dots 语义 = S1~S4 */}
      {!listErr && ontos.length > 0 && (
        <OntologyPicker ontos={ontos} profiles={profiles} activeId={activeId} onSelect={setActiveId} validations={validations} />
      )}
      {listErr ? (
        <div className="work-empty">
          <Result
            status="warning"
            title="本体平面服务未启动（BUILD_SVC_URL/:8091）"
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
                  本体资产
                </Typography.Title>
                <Tag color="blue" style={{ margin: 0 }}>统一管理</Tag>
              </div>
              <p className="work-head-desc">
                已构建本体的统一仓库（REQ-60）：新建请到「本体构建」选择路径；此处负责编辑、校验、版本、产物、可视化与 TTL 导出。
              </p>
            </div>
          </div>
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无本体资产——到「本体构建」栏选择一条构建路径开始" />
        </>
      ) : (
        <>
          <div className="work-head">
            <div className="work-head-text">
              <div className="work-head-title">
                <Typography.Title level={4} style={{ margin: 0 }}>
                  {active.name}
                </Typography.Title>
                <Tag color="geekblue" style={{ margin: 0 }}>v{active.version ?? '—'}</Tag>
                <Tag color={sourceTag(active).color} style={{ margin: 0 }}>{sourceTag(active).text}</Tag>
                <Tag style={{ margin: 0 }}>概念 {active.n_concepts ?? 0}</Tag>
                <Tag style={{ margin: 0 }}>关系 {active.n_relations ?? 0}</Tag>
                <Tag style={{ margin: 0 }}>实例 {active.n_instances ?? 0}</Tag>
                <Tooltip title="被 N 套运行方案引用（只读；启停操作在「本体运行」栏）">
                  <Tag color="purple" style={{ margin: 0 }}>被 {refCount(active)} 套方案引用</Tag>
                </Tooltip>
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
                description="级联删除其 Spec、产物与引用；已启动的运行方案不受影响（REQ-87）。"
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
            <Alert type="error" showIcon closable message="Fork 失败" description={forkErr} onClose={() => setForkErr(null)} />
          )}

          <Card className="work-card onto-stage-card" size="small">
            <Tabs
              destroyOnHidden
              items={[
                {
                  key: 'spec',
                  label: 'Spec 编辑',
                  children: (
                    <SpecEditor
                      ontology={active}
                      spec={spec}
                      specLoading={specLoading}
                      specErr={specErr}
                      onReloadSpec={() => setSpecTick((t) => t + 1)}
                      onMetaSaved={() => reloadOntos()}
                      onSpecSaved={refreshAfterSave}
                    />
                  ),
                },
                {
                  key: 'validate',
                  label: '校验',
                  children: (
                    <ValidatePane
                      ontologyId={active.id}
                      result={validation ?? null}
                      onResult={(r) => setValidations((v) => ({ ...v, [active.id]: r }))}
                    />
                  ),
                },
                {
                  key: 'versions',
                  label: '版本与源码',
                  children: <SourceView ontologyId={active.id} currentVersion={active.version} spec={spec} />,
                },
                {
                  key: 'artifacts',
                  label: '产物',
                  children: <ArtifactsPane ontologyId={active.id} />,
                },
                {
                  key: 'graph',
                  label: '可视化',
                  children: <VizTabs spec={spec} ontologyId={active.id} />,
                },
                {
                  key: 'graph-edit',
                  label: '图形编辑',
                  children: (
                    <GraphEditor ontologyId={active.id} spec={spec} onSpecSaved={refreshAfterSave} />
                  ),
                },
                {
                  key: 'export',
                  label: 'TTL 导出',
                  children: <ExportPane ontology={active} />,
                },
                {
                  key: 'ingest',
                  label: 'CSV 灌装',
                  children: (
                    <CsvIngestPane
                      ontologyId={active.id}
                      spec={spec}
                      onIngested={() => refreshAfterSave()}
                    />
                  ),
                },
              ]}
            />
          </Card>
        </>
      )}

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
    </div>
  )
}

// ---------------------------------------------------------------------------
// 资产列表（左清单形态收进详情上方选择条？——按 14 v0.3 §5.3：列表 + 详情工作区）
// ---------------------------------------------------------------------------

/** 顶部资产选择条：紧凑横向列表（满高左清单在四栏壳下由模块侧边栏承担，这里做选择器） */
export function OntologyPicker({
  ontos,
  profiles,
  activeId,
  onSelect,
  validations,
}: {
  ontos: Ontology[]
  profiles: RuntimeProfile[]
  activeId: string | null
  onSelect: (id: string) => void
  validations: Record<string, ValidationState>
}) {
  return (
    <div className="onto-picker">
      {ontos.map((o) => {
        const f = stageDoneFlags(o, validations[o.id], profiles, null, false)
        const st = ontoStatus(o, profiles)
        return (
          <button key={o.id} type="button" className={`onto-picker-item${o.id === activeId ? ' active' : ''}`} onClick={() => onSelect(o.id)}>
            <span className="onto-picker-name" title={o.name}>{o.name}</span>
            <span className="onto-picker-meta">
              <StageDots flags={f} />
              <span>v{o.version ?? '—'}</span>
              <Tag color={st.color} style={{ margin: 0, fontSize: 10, lineHeight: '16px', padding: '0 4px' }}>{st.text}</Tag>
            </span>
          </button>
        )
      })}
 {ontos.length === 0 && <span className="empty-hint">暂无本体</span>}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Spec 编辑（原 S2：元信息 + JSON 编辑器 + 保存校验）
// ---------------------------------------------------------------------------

function SpecEditor({
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
  const [lastVersion, setLastVersion] = useState<number | null>(null)

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
      setLastVersion(r.version)
      onSpecSaved(r.version)
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
        <span className="onto-sec-title">Spec JSON（concepts / relations / instances 三要素）</span>
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
          {lastVersion != null && validationErrors.length === 0 && <ReloadHintAlert version={lastVersion} />}
        </>
      )}
    </>
  )
}

// ---------------------------------------------------------------------------
// 校验（原 S3）
// ---------------------------------------------------------------------------

function ValidatePane({
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
      <p className="onto-detail-empty">JSON Schema + 引用完整性校验（就地展示通过 / 错误；结果缓存用于构建段完成度派生）。</p>
      {result?.ok && (
        <Alert
          type="success"
          showIcon
          style={{ marginTop: 4 }}
          message="校验通过"
          description="Spec 满足 Schema 与引用完整性约束，可导出 TTL 装载进 SPARQL 型运行方案。"
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
// 产物清单
// ---------------------------------------------------------------------------

function ArtifactsPane({ ontologyId }: { ontologyId: string }) {
  const [list, setList] = useState<ArtifactMeta[] | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [tick, setTick] = useState(0)

  useEffect(() => {
    let alive = true
    setLoading(true)
    setErr(null)
    api
      .listArtifacts(ontologyId)
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
  }, [ontologyId, tick])

  const columns: ColumnsType<ArtifactMeta> = [
    { title: '形态', dataIndex: 'format', width: 160, render: (v: string) => <Tag style={{ margin: 0 }}>{v}</Tag> },
    { title: '大小', dataIndex: 'size', width: 120, render: (v: number) => `${(v / 1024).toFixed(1)} KB` },
    { title: '归一化', dataIndex: 'is_normalized', width: 100, render: (v?: boolean) => (v ? <Tag color="blue" style={{ margin: 0 }}>是</Tag> : <Typography.Text type="secondary">—</Typography.Text>) },
    { title: '入库时间', dataIndex: 'imported_at' },
  ]

  return (
    <>
      <div className="onto-sec">
        <span className="onto-sec-title">多形态资产（原始文件 + 归一化 spec_json）</span>
        <span className="hit-spacer" />
        <Button size="small" icon={<ReloadOutlined />} onClick={() => setTick((t) => t + 1)} disabled={loading}>
          刷新
        </Button>
      </div>
      {err ? (
        <Alert type="warning" showIcon message="产物清单获取失败" description={err} />
      ) : (
        <Table<ArtifactMeta>
          rowKey="format"
          columns={columns}
          dataSource={list ?? []}
          loading={loading}
          pagination={false}
          size="small"
          locale={{ emptyText: '暂无产物（导入 / 保存后生成）' }}
        />
      )}
    </>
  )
}

// ---------------------------------------------------------------------------
// TTL 导出（自建本体入 SPARQL 型方案的前置；O1 rdflib sidecar）
// ---------------------------------------------------------------------------

function ExportPane({ ontology }: { ontology: Ontology }) {
  const { showToast } = useUI()
  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="spec_json → Turtle 导出（O1 sidecar）"
        description="从零创建的本体（仅有 spec_json）经此导出 RDF 形态后，可在 SPARQL 型运行方案（Oxigraph/Fuseki）中加载运行；消费与审计环节见本体模块「消费与审计」栏（D-O15 自研 KG）。"
      />
      <Space>
        <Button
          type="primary"
          icon={<DownloadOutlined />}
          href={api.ontologyExportUrl(ontology.id, 'turtle')}
          onClick={() => showToast('已开始下载 Turtle 导出')}
        >
          导出 Turtle（.ttl）
        </Button>
        <Button icon={<CodeOutlined />} href={api.ontologyExportUrl(ontology.id, 'spec_json')} download>
          导出 spec_json（备份/分享）
        </Button>
      </Space>
      <p className="onto-detail-empty" style={{ marginTop: 10 }}>
        导出地址：<Typography.Text code style={{ fontSize: 12 }}>{api.ontologyExportUrl(ontology.id, 'turtle')}</Typography.Text>
      </p>
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



/**
 * M21/VIZ-1（REQ-154）：可视化 Tab 内 2D（React Flow，D-O12 默认）/ 三维（3d-force-graph 沉浸浏览）
 * 切换。三维懒加载：首次切到「三维浏览」才挂载（WebGL 初始化成本）。两视图数据同源 spec_json，零同步。
 */
function VizTabs({ spec, ontologyId }: { spec: Spec | null; ontologyId: string }) {
  const [mode, setMode] = useState<'2d' | '3d' | 'webvowl'>('2d')
  return (
    <div>
      <Segmented
        size="small"
        style={{ marginBottom: 8 }}
        value={mode}
        onChange={(v) => setMode(v as '2d' | '3d' | 'webvowl')}
        options={[
          { value: '2d', label: '2D 结构（React Flow）' },
          { value: '3d', label: '三维浏览（沉浸只读）' },
          { value: 'webvowl', label: 'WebVOWL 对照（OWL 视觉语言）' },
        ]}
      />
      {mode === '2d' && <SpecGraph spec={spec} />}
      {mode === '3d' && <Graph3D spec={spec} />}
      {mode === 'webvowl' && <WebVowlView ontologyId={ontologyId} />}
    </div>
  )
}
