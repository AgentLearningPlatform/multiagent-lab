import { useEffect, useState } from 'react'
import { Button, Form, Input, Modal, Segmented, Space, Tag } from 'antd'
import { api } from '../../../../api/client'
import type { Ontology, RuntimeProfile, Spec } from '../../../../api/types'
import { useUI } from '../../../../store/ui'
import { ontoStatus, stageDoneFlags, type ValidationState } from '../../shared'
import SpecGraph from '../SpecGraph'
import Graph3D from '../Graph3D'
import WebVowlView from '../WebVowlView'

// ---------------------------------------------------------------------------
// 资产页杂件（B1 拆分，REQ-145）：顶部选择条 / 构建段完成度 dots / 重命名弹窗 /
// 可视化三态 Tab（M21/VIZ-1+VIZ-3，三维懒加载）
// ---------------------------------------------------------------------------

export function StageDots({ flags }: { flags: boolean[] }) {
  const ready = flags.slice(0, 4).filter(Boolean).length
  return (
    <span className="onto-dots" title={`S1~S4 构建段已就绪 ${ready}/4（运行段见本体运行栏）`}>
      {flags.slice(0, 4).map((done, i) => (
        <i key={i} className={`onto-dot${done ? ' on' : ''}`} />
      ))}
    </span>
  )
}

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

export function RenameModal({ ontology, onClose, onSaved }: { ontology: Ontology; onClose: () => void; onSaved: () => void }) {
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
export function VizTabs({ spec, ontologyId }: { spec: Spec | null; ontologyId: string }) {
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
