import { useEffect, useState } from 'react'
import { Alert, Button, Form, Input, Table, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { api, ApiError } from '../../../../api/client'
import type { Ontology, Spec, ValidationError } from '../../../../api/types'
import { useUI } from '../../../../store/ui'
import { ERR_COLUMNS, ReloadHintAlert } from '../../shared'
import JsonEditor from '../JsonEditor'

// ---------------------------------------------------------------------------
// Spec 编辑（原 S2：元信息 + JSON 编辑器 + 保存校验）。
// A1（REQ-145/M22）：TextArea → CodeMirror JSON 模式（语法高亮 + jsonParseLinter
// 行内错误标记）；保存路径保留 JSON.parse try/catch 与后端校验错误表格。
// ---------------------------------------------------------------------------

export function emptySpec(name: string): Spec {
  return { name, description: '', concepts: [], relations: [], instances: [] }
}

export default function SpecEditorPane({
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
          <div style={{ marginTop: 10 }}>
            <JsonEditor
              value={specText}
              onChange={setSpecText}
              readOnly={large}
              height="480px"
              placeholder='{ "name": "…", "concepts": [], "relations": [], "instances": [] }'
            />
          </div>
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
