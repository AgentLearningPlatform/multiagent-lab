import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Badge,
  Button,
  Card,
  Descriptions,
  Drawer,
  Empty,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Spin,
  Steps,
  Table,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  ApiOutlined,
  DeleteOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  RightOutlined,
} from '@ant-design/icons'
import { api } from '../../api/client'
import type { Conversation, Ontology, RuntimeProfile } from '../../api/types'
import { useUI } from '../../store/ui'
import { StatusBadge } from './shared'
import YasguiPane from './components/SparqlConsole'
import TraceTable from './components/TraceTable'

// ---------------------------------------------------------------------------
// 本体运行（RuntimePage，REQ-104 ④）：按运行方式（引擎）分二级模块
//   Oxigraph（可用）| Fuseki（O6 后点亮）| Open Ontologies（引导页）| Cayley（P2 置灰）
//   方案卡片 + 三步向导（REQ-85）+ 详情 Drawer（日志/facade/SPARQL/透视/挂载）
//   正交红线：不调用任何写仓库接口（编辑本体去「本体资产」栏）
// ---------------------------------------------------------------------------

type EngineKey = 'oxigraph' | 'fuseki' | 'oo' | 'cayley'

/** 运行栏引擎选中项（模块内持久化） */
const ONTO_ENGINE_KEY = 'eino.onto.engineKey'

const ENGINES: { key: EngineKey; label: string; state: 'ok' | 'soon' | 'guide' | 'disabled'; desc: string }[] = [
  { key: 'oxigraph', label: 'Oxigraph', state: 'ok', desc: 'SPARQL 型 · P1 唯一引擎（无推理）' },
  { key: 'fuseki', label: 'Fuseki', state: 'soon', desc: 'SPARQL 型 · 带推理（O6 交付后点亮）' },
  { key: 'oo', label: 'Open Ontologies', state: 'guide', desc: '独立托管双轨 · 引导页（非 managed 引擎）' },
  { key: 'cayley', label: 'Cayley', state: 'disabled', desc: '轻量内存图 · P2 可选（D-O5 v0.4）' },
]

const STATE_TAG: Record<EngineKey, { color: string; text: string }[]> = {
  oxigraph: [{ color: 'green', text: '可用' }],
  fuseki: [{ color: 'gold', text: '排期中' }],
  oo: [{ color: 'cyan', text: '引导页' }],
  cayley: [{ color: 'default', text: 'P2' }],
}

function readEngineKey(): EngineKey {
  const v = localStorage.getItem(ONTO_ENGINE_KEY)
  return v === 'fuseki' || v === 'oo' || v === 'cayley' ? (v as EngineKey) : 'oxigraph'
}

/** last_error 关键词 → 友好预检文案（D-O9：engines 预检段未提供前的兜底） */
function friendlyEngineError(err?: string): string | null {
  if (!err) return null
  if (err.includes('executable not found') || err.includes('未找到引擎可执行文件') || err.includes('not found'))
    return '引擎可执行文件缺失：请安装 oxigraph_server 并加入 PATH（或设置 OXIGRAPH_BIN），详见部署文档。'
  if (err.includes('address already in use') || err.includes('bind'))
    return '端口被占用：请更换方案端口后重试。'
  if (err.includes('load') && err.includes('失败'))
    return '本体数据装载失败：请检查所选本体是否可导出 TTL（自建本体先在「本体资产 → TTL 导出」确认）。'
  return null
}

export default function RuntimePage() {
  const [engineKey, setEngineKey] = useState<EngineKey>(readEngineKey)

  useEffect(() => {
    const sync = () => setEngineKey(readEngineKey())
    window.addEventListener('onto-sidebar-change', sync)
    return () => window.removeEventListener('onto-sidebar-change', sync)
  }, [])

  const select = (key: EngineKey) => {
    localStorage.setItem(ONTO_ENGINE_KEY, key)
    setEngineKey(key)
  }

  return (
    <div className="work-main">
      <div className="work-head">
        <div className="work-head-text">
          <div className="work-head-title">
            <Typography.Title level={4} style={{ margin: 0 }}>
              本体运行
            </Typography.Title>
            <Tag color="blue" style={{ margin: 0 }}>按运行方式分组</Tag>
          </div>
          <p className="work-head-desc">
            运行方案是仓库内容的「部署视图」（REQ-87）：同一本体可被多套方案加载；方案停止/删除不影响仓库资产；本体更新后需在方案上显式重载。
          </p>
        </div>
      </div>

      <div className="onto-engine-nav">
        {ENGINES.map((e) => (
          <button
            key={e.key}
            type="button"
            className={`onto-engine-item${engineKey === e.key ? ' active' : ''}${e.state === 'disabled' ? ' disabled' : ''}`}
            onClick={() => e.state !== 'disabled' && select(e.key)}
            disabled={e.state === 'disabled'}
          >
            <span className="onto-engine-top">
              <span className="onto-engine-label">{e.label}</span>
              {STATE_TAG[e.key].map((t) => (
                <Tag key={t.text} color={t.color} style={{ margin: 0, fontSize: 10, lineHeight: '16px', padding: '0 4px' }}>
                  {t.text}
                </Tag>
              ))}
            </span>
            <span className="onto-engine-desc">{e.desc}</span>
          </button>
        ))}
      </div>

      {engineKey === 'oo' ? (
        <OpenOntologiesGuide />
      ) : engineKey === 'fuseki' ? (
        <Card className="work-card" size="small">
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Fuseki 引擎随 O6 交付后点亮；届时支持推理开关与「推理对照」（REQ-94）。" />
        </Card>
      ) : (
        <EngineProfilesPage engine={engineKey} />
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// 引擎分组页（Oxigraph；Fuseki 交付后共用模板）
// ---------------------------------------------------------------------------

function EngineProfilesPage({ engine }: { engine: EngineKey }) {
  const { showToast } = useUI()
  const [profiles, setProfiles] = useState<RuntimeProfile[]>([])
  const [ontos, setOntos] = useState<Ontology[]>([])
  const [profilesErr, setProfilesErr] = useState(false)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [wizardOpen, setWizardOpen] = useState(false)
  const [detailId, setDetailId] = useState<string | null>(null)

  const reload = () => {
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
    reload()
    api.listOntologies().then(setOntos).catch(() => setOntos([]))
  }, [])

  // 运行分组页 5s 轮询（14 v0.3 §6 刷新策略）
  useEffect(() => {
    const t = setInterval(reload, 5000)
    return () => clearInterval(t)
  }, [])

  const mine = useMemo(() => profiles.filter((p) => (p.engine ?? 'oxigraph') === engine), [profiles, engine])
  const detail = mine.find((p) => p.id === detailId) ?? null
  const anyError = mine.some((p) => p.status === 'error' && p.last_error)

  const act = async (id: string, fn: () => Promise<unknown>, ok: string) => {
    setBusyId(id)
    try {
      await fn()
      showToast(ok)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusyId(null)
      reload()
    }
  }

  const remove = async (id: string) => {
    try {
      await api.deleteRuntimeProfile(id)
      showToast('运行方案已删除')
      reload()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  const ontoName = (id: string) => ontos.find((o) => o.id === id)?.name ?? id

  return (
    <>
      {profilesErr && (
        <Alert
          type="warning"
          showIcon
          message="运行平面暂不可达"
          description="RUNTIME_MGR_URL（:8090）未就绪，无法读取 / 管理运行方案。"
        />
      )}
      {anyError && (
        <Alert
          type="error"
          showIcon
          message="引擎可用性预检（last_error 映射）"
          description={friendlyEngineError(mine.find((p) => p.status === 'error')?.last_error) ?? '存在错误状态的方案，悬停状态徽标查看详情。'}
        />
      )}

      <div className="onto-sec" style={{ marginTop: 0 }}>
        <span className="onto-sec-title">{ENGINES.find((e) => e.key === engine)?.label} 运行方案（{mine.length}）</span>
        <span className="hit-spacer" />
        <Button size="small" icon={<ReloadOutlined />} onClick={reload}>
          刷新
        </Button>
        <Button type="primary" size="small" icon={<PlusOutlined />} onClick={() => setWizardOpen(true)}>
          新建方案
        </Button>
      </div>

      {mine.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="该引擎暂无运行方案；点击「新建方案」创建（勾选本体集合 → 配置细节 → 启动）" />
      ) : (
        <div className="onto-profile-grid">
          {mine.map((p) => (
            <div className="onto-profile-card" key={p.id}>
              <div className="onto-profile-head">
                <span className="onto-profile-name" title={p.name}>
                  {p.name}
                </span>
                <StatusBadge p={p} />
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
              <div className="onto-profile-ontos">
                {(p.ontology_ids ?? []).length === 0 ? (
                  <Typography.Text type="secondary" style={{ fontSize: 11 }}>未加载本体</Typography.Text>
                ) : (
                  (p.ontology_ids ?? []).map((oid) => (
                    <Tag key={oid} style={{ margin: 0, fontSize: 11 }} color="geekblue">
                      {ontoName(oid)}
                    </Tag>
                  ))
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
                <Tooltip title="仓库本体更新后，在此显式重载生效（REQ-87）">
                  <Button
                    type="link"
                    size="small"
                    icon={<ReloadOutlined />}
                    loading={busyId === p.id}
                    onClick={() => act(p.id, () => api.reloadRuntimeProfile(p.id), '已请求重载')}
                  >
                    重载
                  </Button>
                </Tooltip>
                <Button type="link" size="small" icon={<RightOutlined />} onClick={() => setDetailId(p.id)}>
                  详情
                </Button>
                <Popconfirm
                  title={`删除运行方案「${p.name}」？`}
                  description="运行中将先停止再删除；不影响本体仓库资产。"
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
          ))}
        </div>
      )}

      <ProfileWizard
        open={wizardOpen}
        engine={engine}
        ontos={ontos}
        onClose={() => setWizardOpen(false)}
        onCreated={() => {
          setWizardOpen(false)
          reload()
        }}
      />

      <ProfileDetail
        profile={detail}
        profilesErr={profilesErr}
        onClose={() => setDetailId(null)}
      />
    </>
  )
}

// ---------------------------------------------------------------------------
// 新建/编辑向导（Steps 三步，REQ-85）：①运行时类型 ②本体集合 ③细节参数
// ---------------------------------------------------------------------------

function ProfileWizard({
  open,
  engine,
  ontos,
  onClose,
  onCreated,
}: {
  open: boolean
  engine: EngineKey
  ontos: Ontology[]
  onClose: () => void
  onCreated: () => void
}) {
  const { showToast } = useUI()
  const [step, setStep] = useState(0)
  const [name, setName] = useState('')
  const [port, setPort] = useState<number | null>(null)
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (open) {
      setStep(0)
      setName('')
      setPort(null)
      setSelectedIds([])
    }
  }, [open])

  const create = async () => {
    if (!name.trim()) {
      showToast('请输入方案名称', 'err')
      setStep(2)
      return
    }
    if (selectedIds.length === 0) {
      showToast('请至少勾选一个本体', 'err')
      setStep(1)
      return
    }
    setBusy(true)
    try {
      await api.createRuntimeProfile({
        name: name.trim(),
        engine,
        ontology_ids: selectedIds,
        ...(port ? { port } : {}),
      })
      showToast('运行方案已创建（启动后生效）')
      onCreated()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      centered
      title={`新建运行方案 · ${ENGINES.find((e) => e.key === engine)?.label}`}
      width={640}
      onCancel={onClose}
      footer={
        <Space>
          <Button onClick={onClose}>取消</Button>
          {step > 0 && <Button onClick={() => setStep(step - 1)}>上一步</Button>}
          {step < 2 && (
            <Button type="primary" onClick={() => setStep(step + 1)}>
              下一步
            </Button>
          )}
          {step === 2 && (
            <Button type="primary" loading={busy} onClick={create}>
              创建方案
            </Button>
          )}
        </Space>
      }
    >
      <Steps
        size="small"
        current={step}
        onChange={setStep}
        items={[{ title: '运行时类型' }, { title: '本体集合' }, { title: '细节参数' }]}
        style={{ marginBottom: 16 }}
      />
      {step === 0 && (
        <Space direction="vertical" style={{ width: '100%' }} size={8}>
          <Alert type="info" showIcon message={`本分组内固定为 ${engine}（P1 仅 Oxigraph 可用；Fuseki 随 O6 点亮）。`} />
          <div className="onto-wizard-engine">
            <Tag color="green" style={{ margin: 0 }}>{engine}</Tag>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {ENGINES.find((e) => e.key === engine)?.desc}
            </Typography.Text>
          </div>
        </Space>
      )}
      {step === 1 && (
        <>
          <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginBottom: 8 }}>
            勾选要加载的本体集合（来自构建平面仓库；自建本体需先在「本体资产 → TTL 导出」确认可导出）。
          </Typography.Text>
          <Select
            mode="multiple"
            style={{ width: '100%' }}
            placeholder="选择本体（可多选）"
            value={selectedIds}
            onChange={setSelectedIds}
            options={ontos.map((o) => ({ value: o.id, label: `${o.name}（v${o.version ?? '—'} · 概念 ${o.n_concepts ?? 0}）` }))}
          />
          {ontos.length === 0 && <Alert type="warning" showIcon style={{ marginTop: 8 }} message="构建平面暂无本体，请先到「本体构建」创建。" />}
        </>
      )}
      {step === 2 && (
        <Space direction="vertical" style={{ width: '100%' }} size={10}>
          <div className="onto-csv-field">
            <span className="cfg-label">方案名称（必填）</span>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="如 k8s-ops-oxigraph" />
          </div>
          <div className="onto-csv-field">
            <span className="cfg-label">监听端口（可选，留空由 Manager 分配）</span>
            <InputNumber min={1} max={65535} value={port} onChange={(v) => setPort(typeof v === 'number' ? v : null)} style={{ width: 200 }} />
          </div>
          <Alert
            type="info"
            showIcon
            message="配置变更后需重启方案生效；SPARQL 端点经平台反代访问（浏览器不直连引擎端口）。"
          />
        </Space>
      )}
    </Modal>
  )
}

// ---------------------------------------------------------------------------
// 方案详情 Drawer：概览 | 日志 | facade 与工具 | 查询与挂载 | 透视
// ---------------------------------------------------------------------------

const ONTO_TOOLS = [
  { name: 'onto_get_concept', desc: '按名称取概念（label / definition / 父子）', args: 'ontology_id, concept' },
  { name: 'onto_get_instance', desc: '按名称取实例（concept / attributes / relations）', args: 'ontology_id, instance' },
  { name: 'onto_list_instances', desc: '按概念列实例', args: 'ontology_id, concept（可选）' },
  { name: 'onto_neighbors', desc: '取概念 / 实例的邻接关系', args: 'ontology_id, node' },
]
const TOOL_COLUMNS: ColumnsType<(typeof ONTO_TOOLS)[number]> = [
  { title: '工具', dataIndex: 'name', width: 220, render: (v: string) => <Typography.Text code style={{ fontSize: 12 }}>{v}</Typography.Text> },
  { title: '说明', dataIndex: 'desc' },
  { title: '必填入参', dataIndex: 'args', width: 240, render: (v: string) => <Typography.Text type="secondary" style={{ fontSize: 12 }}>{v}</Typography.Text> },
]

function ProfileDetail({
  profile,
  profilesErr,
  onClose,
}: {
  profile: RuntimeProfile | null
  profilesErr: boolean
  onClose: () => void
}) {
  return (
    <Drawer
      open={!!profile}
      onClose={onClose}
      width={860}
      title={
        profile ? (
          <Space size={8} wrap>
            <span>{profile.name}</span>
            <StatusBadge p={profile} />
            <Tag style={{ margin: 0 }}>{profile.engine ?? '—'}</Tag>
          </Space>
        ) : (
          ''
        )
      }
      destroyOnHidden
    >
      {profile && (
        <Tabs
          destroyOnHidden
          items={[
            {
              key: 'overview',
              label: '概览',
              children: <OverviewPane profile={profile} />,
            },
            {
              key: 'logs',
              label: '日志',
              children: <LogsPane profileId={profile.id} />,
            },
            {
              key: 'facade',
              label: 'facade 与工具',
              children: <FacadePane profile={profile} />,
            },
            {
              key: 'query',
              label: '查询与挂载',
              children: <QueryMountPane profile={profile} profilesErr={profilesErr} />,
            },
            {
              key: 'trace',
              label: '翻译透视',
              children: <TraceTable profiles={[profile]} />,
            },
          ]}
        />
      )}
    </Drawer>
  )
}

function OverviewPane({ profile }: { profile: RuntimeProfile }) {
  return (
    <>
      <Descriptions size="small" column={2} bordered>
        <Descriptions.Item label="方案 ID"><Typography.Text copyable style={{ fontSize: 12 }}>{profile.id}</Typography.Text></Descriptions.Item>
        <Descriptions.Item label="状态"><StatusBadge p={profile} /></Descriptions.Item>
        <Descriptions.Item label="引擎">{profile.engine ?? '—'}</Descriptions.Item>
        <Descriptions.Item label="端口">{profile.port ?? '—'}</Descriptions.Item>
        <Descriptions.Item label="已加载本体" span={2}>
          {(profile.ontology_ids ?? []).map((oid) => (
            <Tag key={oid} style={{ margin: 2 }}>{oid}</Tag>
          ))}
        </Descriptions.Item>
        <Descriptions.Item label="创建时间">{profile.created_at ?? '—'}</Descriptions.Item>
        <Descriptions.Item label="更新时间">{profile.updated_at ?? '—'}</Descriptions.Item>
        {profile.last_error && (
          <Descriptions.Item label="最近错误" span={2}>
            <Typography.Text type="danger" style={{ fontSize: 12 }}>{profile.last_error}</Typography.Text>
          </Descriptions.Item>
        )}
      </Descriptions>
      <Alert
        type="info"
        showIcon
        style={{ marginTop: 12 }}
        message="显式重载语义（REQ-87）"
        description="本体在仓库更新版本后，需在此方案执行「重载」才按新版本返回查询结果；页面不静默生效。"
      />
    </>
  )
}

function LogsPane({ profileId }: { profileId: string }) {
  const [tail, setTail] = useState<number | null>(200)
  const [lines, setLines] = useState<string[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const load = () => {
    setLoading(true)
    setErr(null)
    api
      .runtimeProfileLogs(profileId, tail ?? 200)
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
  }, [profileId])

  return (
    <>
      <div className="onto-sec" style={{ marginTop: 0 }}>
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
  )
}

function FacadePane({ profile }: { profile: RuntimeProfile }) {
  const running = profile.status === 'running'
  return (
    <>
      <Alert
        type={running ? 'success' : 'info'}
        showIcon
        style={{ marginBottom: 12 }}
        message={
          running
            ? `统一 MCP facade 已随本方案暴露（端口 ${profile.port ?? '—'}）`
            : '方案未运行：启动后由 Runtime Manager 暴露 facade'
        }
      />
      <Card size="small" className="work-card" title={<Space size={6}><ApiOutlined />MCP facade 端点</Space>}>
        <p className="onto-detail-empty">
          运行平面 Runtime Manager 统一暴露 <Typography.Text code>POST /mcp</Typography.Text>
          （默认 <Typography.Text code>http://127.0.0.1:8090/mcp</Typography.Text>），按 <Typography.Text code>ontology_id</Typography.Text> 路由到对应本体。
        </p>
        <div className="onto-expose-meta">
          <span>本方案端口</span>
          <Tag color={running ? 'green' : 'default'} style={{ margin: 0 }}>{profile.port ?? '—'}</Tag>
          <span>状态</span>
          {running ? <Badge status="success" text="运行中" /> : <Badge status="default" text="未运行" />}
        </div>
      </Card>
      <div className="onto-sec">
        <span className="onto-sec-title">固定签名工具（onto_*，共 4 个）</span>
      </div>
      <Table rowKey="name" columns={TOOL_COLUMNS} dataSource={ONTO_TOOLS} pagination={false} size="small" />
    </>
  )
}

function QueryMountPane({ profile, profilesErr }: { profile: RuntimeProfile; profilesErr: boolean }) {
  const [convs, setConvs] = useState<Conversation[] | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    let alive = true
    api
      .listConversations()
      .then((cs) => {
        if (alive) setConvs(cs.filter((c) => c.runtime_profile_id === profile.id))
      })
      .catch((e: any) => {
        if (alive) {
          setConvs(null)
          setErr(e.message)
        }
      })
    return () => {
      alive = false
    }
  }, [profile.id])

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="对话经「运行方案」挂载本体"
        description={`在对话输入框的「本体增强」chip 中选择 running 方案「${profile.name}」；装配期注入 guide，单次失败即降级（ontology.unavailable 事件卡），配置保留，下一条消息自动恢复。`}
      />
      {profile.status === 'running' ? (
        <>
          <div className="onto-sec" style={{ marginTop: 4 }}>
            <span className="onto-sec-title">SPARQL 工作台（REQ-92）</span>
            <span className="hit-spacer" />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              端点 <Typography.Text code style={{ fontSize: 12 }}>{api.sparqlEndpointUrl(profile.id)}</Typography.Text>
            </Typography.Text>
          </div>
          <YasguiPane endpoint={api.sparqlEndpointUrl(profile.id)} persistenceId={`onto-detail-${profile.id}`} />
        </>
      ) : (
        <Alert
          type={profilesErr ? 'warning' : 'info'}
          showIcon
          message="启动方案后可执行 SPARQL 查询"
          description="SPARQL 工作台按运行方案绑定端点；非 running 状态查询将被运行平面拒绝（409）。"
        />
      )}

      <div className="onto-sec">
        <span className="onto-sec-title">已挂载对话</span>
      </div>
      {err && <Alert type="warning" showIcon message="对话列表获取失败" description={err} />}
      {convs && convs.length === 0 && (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} style={{ margin: '14px 0' }} description="暂无对话挂载该运行方案" />
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
// Open Ontologies 引导页（非 managed 引擎，无启停管理）
// ---------------------------------------------------------------------------

function OpenOntologiesGuide() {
  return (
    <Card className="work-card" size="small">
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="Open Ontologies：独立托管双轨（oo-worker :8092）"
        description="Rust 单二进制（MIT，Oxigraph 0.5 后端）：RDFS/OWL-RL 物化推理、SHACL 校验、不一致检查、变更影响分析、数据装载、MCP server（39 个 onto_* 工具）。与主线两平面并行双轨、无接口依赖、可整体摘除。"
      />
      <div className="onto-sec" style={{ marginTop: 0 }}>
        <span className="onto-sec-title">能力面</span>
      </div>
      <ul className="onto-report-list">
        <li>物化推理：RDFS / OWL-RL 全量物化，subClassOf / subPropertyOf 传导可见</li>
        <li>SHACL 校验与不一致检查：约束违规与矛盾路径输出</li>
        <li>变更影响分析（plan / blast radius）：改一条公理前先看波及面</li>
        <li>数据装载：CSV / XLSX / JSON → RDF（S1 段 guided 候选）</li>
        <li>MCP server：39 个 onto_* 工具（主线 facade 为 4 个固定签名的子集）</li>
      </ul>
      <div className="onto-sec">
        <span className="onto-sec-title">数据边界</span>
      </div>
      <Alert type="warning" showIcon message="oo 数据不进主线仓库（spec_json 体系）" description="双轨数据 P1 不互通；回流路径：导出 TTL → 主线「本体构建 → 导入文件」（REQ-78 双轨互通 P2 后自动化）。" />
      <div className="onto-sec">
        <span className="onto-sec-title">工作台入口</span>
      </div>
      <Space>
        <Button type="primary" href="/api/oo/" target="_blank" rel="noreferrer">
          前往 Open Ontologies 工作台（oo-worker :8092）
        </Button>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          需先启动 oo-worker（run-dev.sh 编排；未启动时反代 502）
        </Typography.Text>
      </Space>
    </Card>
  )
}

