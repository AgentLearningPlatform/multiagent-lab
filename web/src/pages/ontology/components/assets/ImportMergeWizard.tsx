import { useState } from 'react'
import { Alert, Button, Input, Modal, Segmented, Space, Steps, Table, Tag, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ReactDiffViewer from 'react-diff-viewer-continued'
import { api } from '../../../../api/client'
import type { MergePreview } from '../../../../api/client'

// ---------------------------------------------------------------------------
// REQ-157/M-O15 导入审查向导（OrionBelt 模式）：外部本体文件并入既有本体的受控流程——
//   ① 选择文件与合并策略（replace=导入版整体替换冲突实体 / merge-overwrite=字段级覆盖 /
//      merge=保留现行+导入实体前缀重命名并入【增量消歧】）→ 冲突预览；
//   ② 字段级冲突报告 + 新增清单 + 现行 vs 合并结果全文 diff（react-diff-viewer-continued 复用 M22 A3）；
//   ③ 应用（结构校验 + strict 门禁，M-O15）→ 版本快照 vN+1。
// 复杂语义合并明确不做（26 号方案 §8-3）。
// ---------------------------------------------------------------------------

const STRATEGY_META: Record<string, { label: string; desc: string }> = {
  replace: { label: '替换', desc: '冲突实体以导入版整体替换，无冲突实体新增' },
  'merge-overwrite': { label: '字段合并', desc: '冲突实体按字段覆盖：导入版非空字段覆盖现行，保留现行独有字段' },
  merge: { label: '重命名并入', desc: '冲突实体保留现行，导入版加前缀重命名并入（增量消歧，引用随改写）' },
}

export default function ImportMergeWizard({
  open,
  onClose,
  ontologyId,
  targetSpecText,
  onApplied,
}: {
  open: boolean
  onClose: () => void
  ontologyId: string
  /** 现行 spec 全文（diff 左侧） */
  targetSpecText: string
  onApplied: (version: number) => void
}) {
  const [step, setStep] = useState(0)
  const [filename, setFilename] = useState('')
  const [content, setContent] = useState('')
  const [strategy, setStrategy] = useState<string>('merge-overwrite')
  const [prefix, setPrefix] = useState('ext')
  const [preview, setPreview] = useState<MergePreview | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [appliedVersion, setAppliedVersion] = useState<number | null>(null)

  const reset = () => {
    setStep(0)
    setFilename('')
    setContent('')
    setPreview(null)
    setErr(null)
    setAppliedVersion(null)
  }

  const close = () => {
    onClose()
    setTimeout(reset, 300)
  }

  const onFile = async (file: File) => {
    setFilename(file.name)
    setContent(await file.text())
  }

  const doPreview = () => {
    if (!content.trim()) {
      setErr('请先选择外部本体文件')
      return
    }
    setBusy(true)
    setErr(null)
    api
      .mergePreview(ontologyId, { filename, content, strategy })
      .then((pv) => {
        setPreview(pv)
        setStep(1)
      })
      .catch((e: any) => setErr(e?.message ?? '冲突预览失败'))
      .finally(() => setBusy(false))
  }

  const doApply = () => {
    setBusy(true)
    setErr(null)
    api
      .mergeApply(ontologyId, { filename, content, strategy, prefix })
      .then((r) => {
        setAppliedVersion(r.version)
        setStep(2)
        onApplied(r.version)
      })
      .catch((e: any) => setErr(e?.message ?? '合并应用失败'))
      .finally(() => setBusy(false))
  }

  const conflictColumns: ColumnsType<NonNullable<MergePreview['conflicts'][number]>> = [
    {
      title: '类型',
      dataIndex: 'kind',
      width: 76,
      render: (v: string) => <Tag style={{ margin: 0 }} color={{ concept: 'geekblue', relation: 'purple', instance: 'cyan' }[v] ?? 'default'}>{v}</Tag>,
    },
    { title: '实体', dataIndex: 'name', width: 140, ellipsis: true },
    {
      title: '冲突字段',
      dataIndex: 'fields',
      width: 150,
      render: (v: string[]) => (v ?? []).map((f) => <Tag key={f} style={{ margin: 2 }}>{f}</Tag>),
    },
    { title: '处置', dataIndex: 'resolution', width: 110, render: (v: string, r) => <Tag style={{ margin: 0 }} color="blue">{v}{r.resolved_as ? ` → ${r.resolved_as}` : ''}</Tag> },
    {
      title: '对照（导入 ← 现行）',
      ellipsis: true,
      render: (_, r) => (
        <Typography.Text type="secondary" style={{ fontSize: 11 }} copyable={false}>
          {r.incoming.slice(0, 90)}
          <br />
          {r.current.slice(0, 90)}
        </Typography.Text>
      ),
    },
  ]

  return (
    <Modal
      open={open}
      centered
      width={880}
      title="导入合并 · 审查向导（REQ-157）"
      onCancel={close}
      footer={null}
      destroyOnHidden
    >
      <Steps
        size="small"
        current={step}
        style={{ marginBottom: 14 }}
        items={[{ title: '文件与策略' }, { title: '冲突审查' }, { title: '应用' }]}
      />
      {err && <Alert type="error" showIcon style={{ marginBottom: 10 }} title="操作失败" description={err} closable onClose={() => setErr(null)} />}

      {step === 0 && (
        <>
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 10 }}
            title="将外部本体文件并入当前本体（增量消歧而非一次性重建）"
            description="支持构建平面可导入的格式（TTL/OWL 走 sidecar、CSV、GraphML、spec_json）。先做冲突预览，确认策略后再应用；应用会生成新版本快照，可随时回退。"
          />
          <Space direction="vertical" style={{ width: '100%' }} size={10}>
            <Space size={10} wrap>
              <input
                type="file"
                accept=".ttl,.owl,.rdf,.xml,.csv,.json,.md,.txt"
                onChange={(e) => {
                  const f = e.target.files?.[0]
                  if (f) void onFile(f)
                }}
              />
              {filename && <Tag color="blue" style={{ margin: 0 }}>{filename}</Tag>}
            </Space>
            <div>
              <Typography.Text strong style={{ fontSize: 12 }}>合并策略</Typography.Text>
              <div style={{ marginTop: 6 }}>
                <Segmented
                  value={strategy}
                  onChange={(v) => setStrategy(v as string)}
                  options={Object.entries(STRATEGY_META).map(([k, m]) => ({ value: k, label: m.label }))}
                />
                <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 6, marginBottom: 0 }}>
                  {STRATEGY_META[strategy]?.desc}
                </Typography.Paragraph>
              </div>
            </div>
            {strategy === 'merge' && (
              <Space size={6}>
                <Typography.Text style={{ fontSize: 12 }}>冲突实体重命名前缀：</Typography.Text>
                <Input size="small" style={{ width: 160 }} value={prefix} onChange={(e) => setPrefix(e.target.value)} placeholder="ext" />
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>与现行实体撞名时自动追加序号避让</Typography.Text>
              </Space>
            )}
            <Space>
              <Button type="primary" loading={busy} onClick={doPreview}>
                冲突预览
              </Button>
              <Button onClick={close}>取消</Button>
            </Space>
          </Space>
        </>
      )}

      {step === 1 && preview && (
        <>
          <Space size={6} wrap style={{ marginBottom: 10 }}>
            <Tag color="blue" style={{ margin: 0 }}>新增 {preview.added.length}</Tag>
            <Tag color="orange" style={{ margin: 0 }}>冲突 {preview.conflicts.length}</Tag>
            <Tag color="purple" style={{ margin: 0 }}>重命名 {preview.renamed.length}</Tag>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              概念 +{preview.stats.concepts_added}/改{preview.stats.concepts_updated} · 关系 +{preview.stats.relations_added}/改{preview.stats.relations_updated} · 实例 +{preview.stats.instances_added}/改{preview.stats.instances_updated}/更名{preview.stats.instances_renamed}
            </Typography.Text>
          </Space>
          <Table
            rowKey={(r) => r.kind + r.name}
            columns={conflictColumns}
            dataSource={preview.conflicts}
            size="small"
            pagination={preview.conflicts.length > 8 ? { pageSize: 8 } : false}
            locale={{ emptyText: '无冲突——全部为新增实体' }}
            style={{ marginBottom: 12 }}
          />
          {preview.renamed.length > 0 && (
            <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
              重命名清单：{preview.renamed.join('；')}
            </Typography.Paragraph>
          )}
          <Typography.Paragraph strong style={{ fontSize: 12, marginBottom: 4 }}>
            现行 vs 合并结果（全文对照）
          </Typography.Paragraph>
          <div style={{ border: '1px solid var(--c-line, #e3e6f0)', borderRadius: 8, overflow: 'hidden', maxHeight: 360 }}>
            <ReactDiffViewer
              oldValue={targetSpecText}
              newValue={JSON.stringify(preview.merged_spec, null, 2)}
              splitView
              leftTitle="现行 spec"
              rightTitle="合并结果"
              styles={{ diffContainer: { fontSize: 11 } }}
            />
          </div>
          <Space style={{ marginTop: 12 }}>
            <Button onClick={() => setStep(0)}>上一步</Button>
            <Button type="primary" loading={busy} onClick={doApply}>
              应用合并（生成版本快照）
            </Button>
            <Button onClick={close}>取消</Button>
          </Space>
        </>
      )}

      {step === 2 && (
        <div style={{ textAlign: 'center', padding: '16px 0' }}>
          <Alert
            type="success"
            showIcon
            title={`合并已应用：新版本 v${appliedVersion}`}
            description={
              <>
                <p>
                  新增 {preview?.added.length ?? 0} · 冲突处置 {preview?.conflicts.length ?? 0} · 重命名 {preview?.renamed.length ?? 0}
                </p>
                <p style={{ fontSize: 12, color: 'var(--ant-color-text-tertiary, #888)' }}>
                  已生成版本快照（可在「版本与源码」页签回看与回退）；strict 门禁开启时错误级命中会被拦截。
                </p>
              </>
            }
          />
          <Button type="primary" style={{ marginTop: 12 }} onClick={close}>
            完成
          </Button>
        </div>
      )}
    </Modal>
  )
}
