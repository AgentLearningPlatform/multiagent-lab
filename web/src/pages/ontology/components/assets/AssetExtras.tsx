import { useEffect, useRef, useState } from 'react'
import { Button, Form, Input, Modal, Segmented, Space } from 'antd'
import { FullscreenExitOutlined, FullscreenOutlined } from '@ant-design/icons'
import { api } from '../../../../api/client'
import type { Ontology, Spec } from '../../../../api/types'
import { useUI } from '../../../../store/ui'
import SpecGraph from '../SpecGraph'
import Graph3D from '../Graph3D'
import WebVowlView from '../WebVowlView'
import OntologyCompanionGraph from '../companion/OntologyCompanionGraph'
import RuntimeGraph from './RuntimeGraph'

// ---------------------------------------------------------------------------
// 资产页杂件（B1 拆分，REQ-145）：重命名弹窗 / 可视化多形态 Tab（M21/VIZ-1+VIZ-3，三维懒加载）
// REQ-237 F17：原顶部选择条 OntologyPicker 与 StageDots 死代码删除（左清单两栏化后无引用）
// ---------------------------------------------------------------------------

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
 * / WebVOWL 对照三态切换。三维懒加载：首次切到「三维浏览」才挂载（WebGL 初始化成本）。
 * 数据同源 spec_json（WebVOWL 走平台 VOWL JSON 导出），零同步。
 * REQ-179：全屏按钮——对整个可视化区 requestFullscreen（三视图共用）；3D 进出场时重挂载
 * （key 置换，WebGL 初始化按新容器尺寸），WebVOWL 由组件内部监听尺寸变化刷新画布。
 */
export function VizTabs({ spec, ontologyId }: { spec: Spec | null; ontologyId: string }) {
  const [mode, setMode] = useState<'2d' | '3d' | 'webvowl' | 'companion' | 'runtime'>('2d')
  const [focus2d, setFocus2d] = useState<string | null>(null) // R3：3D 选中 → 2D 联动聚焦
  // VIZ-5（REQ-175）：含本体的 running 运行方案（渐进扩展 SPARQL 通道，REQ-163 同语义）
  const [sparqlProfile, setSparqlProfile] = useState<string | null>(null)
  useEffect(() => {
    api
      .listRuntimeProfiles()
      .then((ps) => setSparqlProfile(ps.find((p) => p.status === 'running' && p.ontology_ids?.includes(ontologyId))?.id ?? null))
      .catch(() => setSparqlProfile(null))
  }, [ontologyId])
  const [full, setFull] = useState(false)
  const wrapRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const sync = () => setFull(document.fullscreenElement === wrapRef.current)
    document.addEventListener('fullscreenchange', sync)
    return () => document.removeEventListener('fullscreenchange', sync)
  }, [])

  const toggleFull = () => {
    if (!wrapRef.current) return
    if (document.fullscreenElement) void document.exitFullscreen()
    else void wrapRef.current.requestFullscreen().catch(() => {}) // 内嵌环境拒绝时状态由事件同步，保持诚实
  }

  return (
    <div ref={wrapRef} className={`viz-wrap${full ? ' viz-full' : ''}`}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
        <Segmented
          size="small"
          value={mode}
          onChange={(v) => setMode(v as '2d' | '3d' | 'webvowl' | 'companion' | 'runtime')}
          options={[
            { value: '2d', label: '2D 结构（React Flow）' },
            { value: '3d', label: '三维浏览（沉浸只读）' },
            { value: 'webvowl', label: 'WebVOWL 对照（OWL 视觉语言）' },
            { value: 'companion', label: '伴生成长图（对话生长）' },
            { value: 'runtime', label: '运行态实渲（SPARQL）' },
          ]}
        />
        <span style={{ flex: 1 }} />
        <Button size="small" icon={full ? <FullscreenExitOutlined /> : <FullscreenOutlined />} onClick={toggleFull}>
          {full ? '退出全屏' : '全屏'}
        </Button>
      </div>
      {mode === '2d' && <SpecGraph spec={spec} focusName={focus2d} />}
      {mode === '3d' && (
        <Graph3D
          key={full ? 'fs' : 'inline'}
          spec={spec}
          sparqlProfile={sparqlProfile}
          onRequest2D={(name) => {
            setFocus2d(name)
            setMode('2d')
          }}
        />
      )}
      {mode === 'webvowl' && <WebVowlView ontologyId={ontologyId} />}
      {mode === 'companion' && <OntologyCompanionGraph ontologyId={ontologyId} />}
      {mode === 'runtime' && <RuntimeGraph ontologyId={ontologyId} />}
    </div>
  )
}
