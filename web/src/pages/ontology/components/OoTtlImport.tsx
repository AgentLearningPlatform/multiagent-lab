import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Card, Select, Space, Spin, Tag, Upload, Typography } from 'antd'
import { UploadOutlined } from '@ant-design/icons'
import type { UploadFile } from 'antd'
import { api } from '../../../api/client'
import LoadErrorAlert from '../../../components/LoadErrorAlert'

// ---------------------------------------------------------------------------
// M-O14 P2③（REQ-171 P2 批次 / OoGuide 产物回流升级）：open-ontologies TTL → 主线 spec_json
// 走 REQ-157 审查底座——TTL 经 sidecar 有损导入（导入报告 warnings 如实呈现）→ merge 审查
// （冲突/改名/新增）→ apply 入库（strict 门禁 + 版本快照）。REQ-78 双轨互通（spec→TTL 反向
// 与自动同步）仍按其触发条件冻结，不在本块范围。
// ---------------------------------------------------------------------------

interface ImportReport {
  warnings?: string[]
  stats?: Record<string, unknown>
}
interface PvData {
  strategy?: string
  added?: string[]
  conflicts?: unknown[]
  renamed?: unknown[]
  stats?: { concepts_added?: number; relations_added?: number }
}
interface MergePreviewData extends PvData {
  import_report?: ImportReport
  preview?: PvData
}

export default function OoTtlImport() {
  const [ontos, setOntos] = useState<{ id: string; name: string }[]>([])
  const [targetId, setTargetId] = useState<string | undefined>(undefined)
  const [file, setFile] = useState<{ filename: string; content: string } | null>(null)
  const [preview, setPreview] = useState<MergePreviewData | null>(null)
  const [loading, setLoading] = useState(false)
  const [applying, setApplying] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [appliedVersion, setAppliedVersion] = useState<number | null>(null)

  useEffect(() => {
    api
      .listOntologies()
      .then((ls: any) => setOntos(Array.isArray(ls) ? ls : ls?.ontologies ?? []))
      .catch(() => setOntos([]))
  }, [])

  const onFile = useCallback((f: UploadFile) => {
    const raw = f.originFileObj ?? (f as unknown as { file?: File }).file
    if (!raw) return
    const reader = new FileReader()
    reader.onload = () => {
      setFile({ filename: raw.name, content: String(reader.result ?? '') })
      setPreview(null)
      setAppliedVersion(null)
    }
    reader.readAsText(raw)
  }, [])

  const doPreview = async () => {
    if (!targetId || !file) return
    setLoading(true)
    setErr(null)
    setAppliedVersion(null)
    try {
      const pv = await api.mergePreview(targetId, { filename: file.filename, content: file.content, strategy: 'merge' })
      setPreview(pv as unknown as MergePreviewData)
    } catch (e: any) {
      setErr(e?.message ?? '审查预览失败')
    } finally {
      setLoading(false)
    }
  }

  const doApply = async () => {
    if (!targetId || !file) return
    setApplying(true)
    setErr(null)
    try {
      const res = await api.mergeApply(targetId, { filename: file.filename, content: file.content, strategy: 'merge' })
      setAppliedVersion(res.version ?? null)
      setPreview(null)
    } catch (e: any) {
      setErr(e?.message ?? '入库失败')
    } finally {
      setApplying(false)
    }
  }

  const warnings = preview?.import_report?.warnings ?? []
  // 响应双形态：TTL 路径 = {import_report, preview: pv}；spec 直传 = pv 顶层（REQ-157 原口径）
  const pv = preview?.preview ?? preview
  const addedList: string[] = Array.isArray(pv?.added) ? (pv!.added as string[]) : []
  const addedConcepts = addedList.filter((s) => s.startsWith('concept:')).length
  const addedRelations = addedList.filter((s) => s.startsWith('relation:')).length
  const conflictCount = (pv?.conflicts?.length ?? 0) + (pv?.renamed?.length ?? 0)

  return (
    <Card size="small" style={{ marginTop: 12 }}>
      <div className="onto-sec" style={{ marginTop: 0 }}>
        <span className="onto-sec-title">产物回流（M-O14 P2③：TTL 走审查底座）</span>
      </div>
      <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
        oo 工作台导出的 TTL 经 sidecar 有损导入（导入 warnings 如实呈现）→ merge 审查 → 并入目标本体新版本（strict 门禁）。REQ-78 双轨自动互通仍按触发条件冻结。
      </Typography.Paragraph>
      <Space wrap style={{ marginBottom: 8 }}>
        <Select
          showSearch
          optionFilterProp="label"
          style={{ width: 260 }}
          placeholder="选择并入目标本体"
          value={targetId}
          onChange={(v) => {
            setTargetId(v)
            setPreview(null)
            setAppliedVersion(null)
          }}
          options={ontos.map((o) => ({ value: o.id, label: o.name }))}
        />
        <Upload maxCount={1} accept=".ttl,.turtle,.owl,.rdf" showUploadList={false} beforeUpload={() => false} onChange={({ fileList }) => fileList[0] && onFile(fileList[0])}>
          <Button icon={<UploadOutlined />}>选择 oo 导出的 TTL 文件</Button>
        </Upload>
        <Button type="primary" disabled={!targetId || !file} loading={loading} onClick={doPreview}>
          审查预览
        </Button>
        {preview && (
          <Button type="primary" ghost loading={applying} onClick={doApply}>
            应用入库
          </Button>
        )}
      </Space>
      {file && (
        <div style={{ marginBottom: 8 }}>
          <Tag color="geekblue">{file.filename}</Tag>
          <Typography.Text type="secondary" style={{ fontSize: 11 }}>{file.content.length} 字符</Typography.Text>
        </div>
      )}
      {err && <LoadErrorAlert title="TTL 回流失败" message={err} onRetry={() => setErr(null)} style={{ marginBottom: 8 }} />}
      {loading && <Spin size="small" />}
      {preview && (
        <Card size="small">
          <div style={{ fontWeight: 600, marginBottom: 4 }}>审查报告（有损导入 + merge，REQ-157 底座）</div>
          <Space size={16} wrap style={{ marginBottom: warnings.length ? 6 : 0 }}>
            <Tag color="green">新增概念 {addedConcepts}</Tag>
            <Tag color="geekblue">新增关系 {addedRelations}</Tag>
            <Tag color={conflictCount > 0 ? 'orange' : 'default'}>冲突/改名 {conflictCount}</Tag>
          </Space>
          {warnings.length > 0 && (
            <Alert
              type="warning"
              showIcon
              title={`有损导入 warnings（${warnings.length}）`}
              description={
                <ul style={{ margin: 0, paddingLeft: 18, fontSize: 12 }}>
                  {warnings.slice(0, 8).map((w, i) => (
                    <li key={i}>{w}</li>
                  ))}
                  {warnings.length > 8 && <li>…共 {warnings.length} 条</li>}
                </ul>
              }
              style={{ marginBottom: 6 }}
            />
          )}
        </Card>
      )}
      {appliedVersion !== null && (
        <Alert type="success" showIcon style={{ marginTop: 8 }} title={`TTL 已并入目标本体新版本（v${appliedVersion}）——strict 质量门禁通过。`} />
      )}
    </Card>
  )
}
