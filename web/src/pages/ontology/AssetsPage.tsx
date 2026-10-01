import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Empty, Input, Popconfirm, Result, Space, Splitter, Tabs, Tag, Tooltip, Typography } from 'antd'
import { BranchesOutlined, DeleteOutlined, EditOutlined, ImportOutlined } from '@ant-design/icons'
import { api, ApiError } from '../../api/client'
import { companionApi } from '../../api/companion'
import type { Ontology, RuntimeProfile, Spec } from '../../api/types'
import { useUI } from '../../store/ui'
import { sourceTag, type ValidationState } from './shared'
import CsvIngestPane from './components/CsvIngestPane'
import GraphEditor from './components/GraphEditor'
import SourceView from './components/SourceView'
import SpecEditorPane from './components/assets/SpecEditorPane'
import { ArtifactsPane, ExportPane, ValidatePane } from './components/assets/AssetPanes'
import { RenameModal, VizTabs } from './components/assets/AssetExtras'
import OntologyCompanionPane from './components/companion/OntologyCompanionPane'
import AssetList from './components/assets/AssetList'
import QualityCardPane from './components/assets/QualityCardPane'
import ImportMergeWizard from './components/assets/ImportMergeWizard'

// ---------------------------------------------------------------------------
// 本体资产（AssetsPage，REQ-104 ③）：全部已构建本体统一管理
//   REQ-181 v2 重构：平台统一「左列表（来源分组）+ 右主区」两栏（原选择条在上+详情在下）；
//   详情工作区 Tabs：
//   Spec 编辑 | 校验 | 版本（含源码视图 REQ-93）| 产物 | 可视化 | TTL 导出 | CSV 灌装（REQ-96）
//   正交红线：不出现任何引擎、端口、启停配置（运行看本体运行栏）
// B1（REQ-145/M22）：Spec 编辑/校验/产物/导出/选择条/重命名/可视化拆至 components/assets/。
// ---------------------------------------------------------------------------

export default function AssetsPage() {
  const { showToast } = useUI()
  const [tabKey, setTabKey] = useState<string | null>(null) // REQ-230②：显式切换优先；切本体回落默认规则
  const [ontos, setOntos] = useState<Ontology[]>([])
  const [profiles, setProfiles] = useState<RuntimeProfile[]>([])
  const [listErr, setListErr] = useState<string | null>(null)
  const [activeId, setActiveId] = useState<string | null>(null)

  const [spec, setSpec] = useState<Spec | null>(null)
  const [specErr, setSpecErr] = useState<string | null>(null)
  const [specLoading, setSpecLoading] = useState(false)
  const [specTick, setSpecTick] = useState(0)

  const [validations, setValidations] = useState<Record<string, ValidationState>>({})
  const [mergeOpen, setMergeOpen] = useState(false) // REQ-157 导入合并向导
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

  // REQ-216⑦：伴生绑定本体清单（详情页「伴生候选」页签「对话生长」徽标判定）
  const [boundIds, setBoundIds] = useState<Set<string>>(new Set())
  const reloadBound = () => {
    companionApi
      .boundOntologies()
      .then((r) => setBoundIds(new Set(r.ontology_ids ?? [])))
      .catch(() => setBoundIds(new Set()))
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
    reloadBound()
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
  // 切换选中本体时回落默认页签规则（REQ-230②）
  useEffect(() => {
    setTabKey(null)
  }, [activeId])
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
    <Splitter className="main sidebar-splitter">
      <Splitter.Panel defaultSize={Number(localStorage.getItem('eino.assets.width')) || 260} min={200} max={420} className="sidebar-panel">
        <aside className="sidebar">
          <div className="side-head">
            <span className="side-title">本体资产</span>
            <span className="side-count">{ontos.length}</span>
          </div>
          <div className="ref-menu" style={{ paddingBottom: 12 }}>
            <AssetList ontos={ontos} profiles={profiles} activeId={activeId} onSelect={setActiveId} validations={validations} />
          </div>
        </aside>
      </Splitter.Panel>
      <Splitter.Panel className="content-panel">
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
              <Button icon={<ImportOutlined />} onClick={() => setMergeOpen(true)}>
                导入合并
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

          {mergeOpen && active && (
            <ImportMergeWizard
              open={mergeOpen}
              onClose={() => setMergeOpen(false)}
              ontologyId={active.id}
              targetSpecText={spec ? JSON.stringify(spec, null, 2) : ''}
              onApplied={() => {
                reloadOntos(active.id)
                setSpecTick((t) => t + 1)
              }}
            />
          )}

          {forkErr && (
            <Alert type="error" showIcon closable message="Fork 失败" description={forkErr} onClose={() => setForkErr(null)} />
          )}

          <Card className="work-card onto-stage-card" size="small">
            <Tabs
              activeKey={tabKey ?? (boundIds.has(active.id) ? 'companion' : 'spec')} /* REQ-230②：伴生型本体默认打开候选页签 */
              onChange={(k) => setTabKey(k)}
              destroyOnHidden
              items={[
                {
                  key: 'spec',
                  label: 'Spec 编辑',
                  children: (
                    <SpecEditorPane
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
                  label: '质量卡',
                  children: <QualityCardPane ontologyId={active.id} />,
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
                  key: 'companion',
                  label: (
                    <span>
                      伴生候选
                      {boundIds.has(active.id) && (
                        <Tooltip title="对话生长：有智能体绑定该本体为伴生归属（REQ-216）">
                          <Tag color="geekblue" style={{ margin: 0, marginLeft: 6, fontSize: 10, lineHeight: '16px', padding: '0 5px' }}>
                            对话生长
                          </Tag>
                        </Tooltip>
                      )}
                    </span>
                  ),
                  children: <OntologyCompanionPane key={active.id} ontologyId={active.id} />,
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
      </Splitter.Panel>
    </Splitter>
  )
}
