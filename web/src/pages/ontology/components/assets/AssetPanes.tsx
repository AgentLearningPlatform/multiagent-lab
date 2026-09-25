import { useEffect, useState } from 'react'
import { Alert, Button, Space, Table, Tag, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { CheckCircleOutlined, CodeOutlined, DownloadOutlined, ReloadOutlined } from '@ant-design/icons'
import { api } from '../../../../api/client'
import type { ArtifactMeta, Ontology, ValidationError } from '../../../../api/types'
import { useUI } from '../../../../store/ui'
import { ERR_COLUMNS, type ValidationState } from '../../shared'

// ---------------------------------------------------------------------------
// 资产详情辅助页签三件（B1 拆分，REQ-145）：校验 / 产物清单 / TTL 导出
// ---------------------------------------------------------------------------

export function ValidatePane({
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

export function ArtifactsPane({ ontologyId }: { ontologyId: string }) {
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

/** TTL 导出（自建本体入 SPARQL 型方案的前置；O1 rdflib sidecar） */
export function ExportPane({ ontology }: { ontology: Ontology }) {
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
