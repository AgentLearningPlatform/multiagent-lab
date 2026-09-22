import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Statistic,
  Steps,
  Table,
  Tag,
  Typography,
  Upload,
} from 'antd'
import { InboxOutlined, SaveOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { api, ApiError } from '../../../api/client'
import type { CsvIngestPreview, Spec, SpecInstance } from '../../../api/types'
import { useUI } from '../../../store/ui'
import { ERR_COLUMNS } from '../shared'

// ---------------------------------------------------------------------------
// CSV 灌装（REQ-96）：P2a 同名映射 + P2b 映射向导
//   流程：上传 CSV → 选概念/主键 → 列绑定（属性/关系 + P2b 类型转换/多值分隔符）→
//         预览（统计 + warnings + 草稿表）→ 校验入库为新版本。
//   映射配置可保存/复用（GET/PUT ingest-mapping，artifact ingest_mapping_json，
//   不 bump version、不进产物列表、不随 fork 复制）。
// ---------------------------------------------------------------------------

const TYPE_OPTIONS = [
  { value: 'string', label: 'string（原样）' },
  { value: 'int', label: 'int（整数）' },
  { value: 'number', label: 'number（数值）' },
  { value: 'date', label: 'date（2006-01-02）' },
  { value: 'bool', label: 'bool（true/false）' },
]

/** 解析 CSV 文本：返回表头与数据行（容忍参差行，与后端 csv.Reader 口径一致） */
function parseCsvText(text: string): string[][] {
  const rows: string[][] = []
  let cur: string[] = []
  let field = ''
  let inQuote = false
  for (let i = 0; i < text.length; i++) {
    const ch = text[i]
    if (inQuote) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          field += '"'
          i++
        } else inQuote = false
      } else field += ch
    } else if (ch === '"') {
      inQuote = true
    } else if (ch === ',') {
      cur.push(field)
      field = ''
    } else if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && text[i + 1] === '\n') i++
      cur.push(field)
      field = ''
      if (cur.length > 1 || cur[0] !== '') rows.push(cur)
      cur = []
    } else field += ch
  }
  cur.push(field)
  if (cur.length > 1 || cur[0] !== '') rows.push(cur)
  return rows
}

export default function CsvIngestPane({
  ontologyId,
  spec,
  onIngested,
}: {
  ontologyId: string
  spec: Spec | null
  /** 入库成功（版本 +1）后通知父级刷新列表与 Spec */
  onIngested: (version: number) => void
}) {
  const { showToast } = useUI()
  const [file, setFile] = useState<File | null>(null)
  const [header, setHeader] = useState<string[]>([])
  const [preview, setPreview] = useState<CsvIngestPreview | null>(null)
  const [warnings, setWarnings] = useState<string[]>([])
  const [validationErrors, setValidationErrors] = useState<{ path: string; message: string }[]>([])
  const [busy, setBusy] = useState(false)
  const [appliedVersion, setAppliedVersion] = useState<number | null>(null)
  const [mappingLoaded, setMappingLoaded] = useState(false)

  const [form] = Form.useForm()

  const concepts = useMemo(() => (spec?.concepts ?? []).map((c) => c.name), [spec])
  const relations = useMemo(() => (spec?.relations ?? []).map((r) => r.name), [spec])

  // 换本体：重置全部状态并尝试加载已保存映射（404 = 尚未保存，静默）
  useEffect(() => {
    setFile(null)
    setHeader([])
    setPreview(null)
    setWarnings([])
    setValidationErrors([])
    setAppliedVersion(null)
    setMappingLoaded(false)
    form.resetFields()
    let alive = true
    api
      .getIngestMapping(ontologyId)
      .then((m) => {
        if (!alive) return
        form.setFieldsValue({
          concept: m.concept,
          key_column: m.key_column,
          relation_columns: m.relation_columns ?? [],
          attribute_columns: m.attribute_columns ?? [],
          skip_rows: m.skip_rows ?? 0,
          multi_value_sep: m.multi_value_sep ?? '',
        })
        setMappingLoaded(true)
      })
      .catch((e) => {
        if (alive && !(e instanceof ApiError && e.status === 404)) showToast(`映射配置读取失败：${e.message}`, 'err')
      })
    return () => {
      alive = false
    }
  }, [ontologyId, form, showToast])

  const typeRules: Record<string, string> = Form.useWatch('type_rules', form) ?? {}
  const relCols: string[] = Form.useWatch('relation_columns', form) ?? []

  const onFile = (f: File) => {
    setFile(f)
    setPreview(null)
    setWarnings([])
    setValidationErrors([])
    setAppliedVersion(null)
    f.text().then((text) => {
      const rows = parseCsvText(text.replace(/^﻿/, ''))
      if (rows.length > 0) {
        setHeader(rows[0].map((h, i) => h.trim() || `列${i + 1}`))
        // 首次上传且无已存映射：主键列默认第一列
        if (!mappingLoaded && !form.getFieldValue('key_column')) form.setFieldValue('key_column', rows[0][0]?.trim() || `列1`)
      } else {
        setHeader([])
        showToast('CSV 为空或无法解析表头', 'err')
      }
    })
  }

  const buildForm = () => {
    const fd = new FormData()
    if (file) fd.append('csv', file)
    const cfg = {
      concept: form.getFieldValue('concept'),
      key_column: form.getFieldValue('key_column'),
      relation_columns: (form.getFieldValue('relation_columns') ?? []).filter(Boolean),
      attribute_columns: (form.getFieldValue('attribute_columns') ?? []).filter(Boolean),
      skip_rows: form.getFieldValue('skip_rows') ?? 0,
      type_rules: Object.fromEntries(Object.entries(typeRules).filter(([, v]) => v && v !== 'string')),
      multi_value_sep: form.getFieldValue('multi_value_sep') ?? '',
    }
    fd.append('config', JSON.stringify(cfg))
    return fd
  }

  const runPreview = async () => {
    try {
      await form.validateFields()
    } catch {
      return
    }
    if (!file) {
      showToast('请先上传 CSV 文件', 'err')
      return
    }
    setBusy(true)
    setValidationErrors([])
    setAppliedVersion(null)
    try {
      const r = await api.ingestCsvPreview(ontologyId, buildForm())
      setPreview(r)
      setWarnings(r.warnings ?? [])
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const runApply = async () => {
    setBusy(true)
    try {
      const r = await api.ingestCsvApply(ontologyId, buildForm())
      setAppliedVersion(r.version)
      setWarnings(r.stats ? warnings : [])
      showToast(`已入库为新版本 v${r.version}`)
      onIngested(r.version)
    } catch (e: any) {
      if (e instanceof ApiError && e.validationErrors?.length) setValidationErrors(e.validationErrors)
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const saveMapping = async () => {
    try {
      await form.validateFields()
    } catch {
      return
    }
    try {
      await api.putIngestMapping(ontologyId, {
        concept: form.getFieldValue('concept'),
        key_column: form.getFieldValue('key_column'),
        relation_columns: (form.getFieldValue('relation_columns') ?? []).filter(Boolean),
        attribute_columns: (form.getFieldValue('attribute_columns') ?? []).filter(Boolean),
        skip_rows: form.getFieldValue('skip_rows') ?? 0,
        type_rules: Object.fromEntries(Object.entries(typeRules).filter(([, v]) => v && v !== 'string')),
        multi_value_sep: form.getFieldValue('multi_value_sep') ?? '',
      })
      setMappingLoaded(true)
      showToast('映射配置已保存（下次进入自动带出）')
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  const draftColumns: ColumnsType<SpecInstance> = [
    { title: '实例名（主键）', dataIndex: 'name', width: 180 },
    { title: '概念', dataIndex: 'concept', width: 140, render: (v: string) => <Tag style={{ margin: 0 }}>{v}</Tag> },
    {
      title: '属性',
      dataIndex: 'attributes',
      render: (v: Record<string, unknown> | undefined) =>
        v && Object.keys(v).length > 0 ? (
          <Typography.Text style={{ fontSize: 12 }} code>
            {Object.entries(v)
              .map(([k, val]) => `${k}=${String(val)}`)
              .join('  ')}
          </Typography.Text>
        ) : (
          <Typography.Text type="secondary">—</Typography.Text>
        ),
    },
    {
      title: '关系',
      dataIndex: 'relations',
      render: (v: SpecInstance['relations']) =>
        v && v.length > 0 ? (
          <Typography.Text style={{ fontSize: 12 }} code>
            {v.map((r) => `${r.rel}→${r.target}`).join('  ')}
          </Typography.Text>
        ) : (
          <Typography.Text type="secondary">—</Typography.Text>
        ),
    },
  ]

  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="CSV 灌装（REQ-96）：同名映射 + 映射向导"
        description="表头即属性/关系名（同名映射）；向导增量：列类型转换（int/number/date/bool）、关系列多值分隔符、跳行规则。映射配置可保存复用。入库走校验门控并生成新版本。"
      />
      <Steps
        size="small"
        style={{ marginBottom: 16 }}
        items={[{ title: '上传 CSV' }, { title: '列绑定' }, { title: '预览' }, { title: '入库' }]}
      />
      <Form form={form} layout="vertical" style={{ maxWidth: 860 }}>
        <Space align="start" size={16} wrap style={{ display: 'flex' }}>
          <Form.Item label="目标概念（实例归属）" name="concept" rules={[{ required: true, message: '选择已定义概念' }]} style={{ minWidth: 220 }}>
            <Select
              placeholder="从 Spec 概念中选择"
              options={concepts.map((c) => ({ value: c, label: c }))}
              showSearch
              disabled={concepts.length === 0}
            />
          </Form.Item>
          <Form.Item label="主键列（实例名来源）" name="key_column" rules={[{ required: true, message: '选择主键列' }]} style={{ minWidth: 200 }}>
            <Select placeholder="从表头列中选择" options={header.map((h) => ({ value: h, label: h }))} disabled={header.length === 0} />
          </Form.Item>
          <Form.Item label="跳过行数（跳行规则）" name="skip_rows" style={{ minWidth: 160 }}>
            <InputNumber min={0} max={1000} style={{ width: '100%' }} />
          </Form.Item>
        </Space>

        <Upload.Dragger
          accept=".csv,.txt"
          multiple={false}
          showUploadList={false}
          disabled={busy}
          beforeUpload={(f) => {
            onFile(f)
            return false
          }}
          style={{ marginBottom: 16 }}
        >
          <p className="ant-upload-drag-icon">
            <InboxOutlined />
          </p>
          <p className="ant-upload-text">{file ? file.name : '点击或拖拽 CSV 文件'}</p>
          <p className="ant-upload-hint">
            {header.length > 0 ? `表头（${header.length} 列）：${header.join(' | ')}` : 'UTF-8 CSV，首行为表头；表头名与属性/关系同名即映射'}
          </p>
        </Upload.Dragger>

        <Space align="start" size={16} wrap style={{ display: 'flex' }}>
          <Form.Item label="属性列（同名映射为属性）" name="attribute_columns" style={{ minWidth: 280 }}>
            <Select
              mode="multiple"
              placeholder="可多选"
              options={header.filter((h) => h !== form.getFieldValue('key_column')).map((h) => ({ value: h, label: h }))}
              disabled={header.length === 0}
            />
          </Form.Item>
          <Form.Item
            label="关系列（同名映射为关系，值=目标实例名）"
            name="relation_columns"
            style={{ minWidth: 280 }}
            extra={relations.length > 0 ? undefined : <Typography.Text type="warning">Spec 尚未定义任何关系，关系列入库会被校验拒绝</Typography.Text>}
          >
            <Select mode="multiple" placeholder="可多选" options={header.map((h) => ({ value: h, label: h }))} disabled={header.length === 0} />
          </Form.Item>
          <Form.Item
            label="多值分隔符（P2b）"
            name="multi_value_sep"
            style={{ minWidth: 160 }}
            extra={relCols.length > 0 ? '关系列一格多个目标时拆分（如 ;）' : '先选关系列后生效'}
          >
            <Input placeholder="如 ; 或 |，留空=单值" maxLength={4} />
          </Form.Item>
        </Space>

        {header.length > 0 && (
          <Form.Item label="列类型转换（P2b，缺省 string 原样；非法值记 warning 并按原字符串保留）">
            <Space size={8} wrap>
              {header.map((h) => (
                <Space.Compact key={h}>
                  <Input readOnly value={h} style={{ width: 130, fontSize: 12 }} />
                  <Form.Item name={['type_rules', h]} noStyle initialValue="string">
                    <Select options={TYPE_OPTIONS} style={{ width: 150 }} size="middle" />
                  </Form.Item>
                </Space.Compact>
              ))}
            </Space>
          </Form.Item>
        )}

        <Space style={{ marginBottom: 16 }}>
          <Button type="primary" loading={busy} onClick={runPreview} disabled={!file}>
            生成预览
          </Button>
          <Button onClick={saveMapping} icon={<SaveOutlined />} disabled={!file}>
            保存映射配置
          </Button>
          {mappingLoaded && <Tag color="green" style={{ margin: 0 }}>已加载/保存过映射</Tag>}
        </Space>
      </Form>

      {validationErrors.length > 0 && (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 12 }}
          message="校验未通过，不允许保存坏本体"
          description={
            <Table
              rowKey={(r) => r.path + r.message}
              columns={ERR_COLUMNS}
              dataSource={validationErrors}
              pagination={false}
              size="small"
            />
          }
        />
      )}

      {preview && (
        <>
          <div className="onto-sec">
            <span className="onto-sec-title">预览（前 20 条草稿）</span>
            <span className="hit-spacer" />
            <Space>
              <Statistic title="读取行" value={preview.stats.rows_read} groupSeparator="" />
              <Statistic title="生成实例" value={preview.stats.instances_generated} groupSeparator="" />
              <Statistic title="空主键跳过" value={preview.stats.skipped_empty_key} groupSeparator="" />
            </Space>
          </div>
          {warnings.length > 0 && (
            <Alert
              type="warning"
              showIcon
              style={{ marginBottom: 12 }}
              message={`warnings（${warnings.length}）`}
              description={
                <ul style={{ margin: 0, paddingLeft: 18, maxHeight: 160, overflow: 'auto' }}>
                  {warnings.slice(0, 50).map((wn, i) => (
                    <li key={i} style={{ fontSize: 12 }}>
                      {wn}
                    </li>
                  ))}
                </ul>
              }
            />
          )}
          <Table<SpecInstance>
            rowKey="name"
            columns={draftColumns}
            dataSource={preview.draft ?? []}
            pagination={false}
            size="small"
            style={{ marginBottom: 12 }}
          />
          <Descriptions size="small" column={1} style={{ marginBottom: 12 }}>
            <Descriptions.Item label="入库方式">
              合并进当前 Spec（同名实例跳过）→ 校验门控 → 保存为新版本（版本 +1，可在「版本与源码」查看 diff）
            </Descriptions.Item>
          </Descriptions>
          <Button type="primary" loading={busy} onClick={runApply} danger={false}>
            确认入库为新版本
          </Button>
          {appliedVersion !== null && (
            <Alert type="success" showIcon style={{ marginTop: 12 }} message={`已入库：当前版本 v${appliedVersion}（Spec 编辑/可视化/版本 diff 已同步）`} />
          )}
        </>
      )}
    </>
  )
}
