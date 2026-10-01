import { useEffect, useState } from 'react'
import { Alert, Button, Form, Input, Modal, Segmented, Select, Space, Spin, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { api } from '../../../../api/client'
import type { Ontology, Spec } from '../../../../api/types'
import { useUI } from '../../../../store/ui'
import SpecGraph from '../SpecGraph'
import Graph3D from '../Graph3D'
import WebVowlView from '../WebVowlView'
import OntologyCompanionGraph from '../companion/OntologyCompanionGraph'
import { useRuntimeSpec } from './RuntimeGraph'
import Maximizeable from '../../../../components/Maximizeable'

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
export function VizTabs({ spec, ontologyId, isCompanion = false }: { spec: Spec | null; ontologyId: string; isCompanion?: boolean }) {
  // REQ-240⑤：运行态实渲并入三维浏览数据源切换（原第 5 形态退役，消除体验重复）——
  // 仓库=事实源全量直渲；运行态=引擎部署视图 TBox 实渲（REQ-87 两种视角，渐进通道仅仓库源可用）
  const [mode, setMode] = useState<'2d' | '3d' | 'webvowl' | 'companion'>('2d')
  const [src, setSrc] = useState<'repo' | 'runtime'>('repo')
  const [focus2d, setFocus2d] = useState<string | null>(null)
  // VIZ-5（REQ-175）：含本体的 running 运行方案（渐进扩展 SPARQL 通道，REQ-163 同语义）
  const [sparqlProfile, setSparqlProfile] = useState<string | null>(null)
  useEffect(() => {
    api
      .listRuntimeProfiles()
      .then((ps) => setSparqlProfile(ps.find((p) => p.status === 'running' && p.ontology_ids?.includes(ontologyId))?.id ?? null))
      .catch(() => setSparqlProfile(null))
  }, [ontologyId])
  const rt = useRuntimeSpec(ontologyId)
  const hasRuntime = rt.profiles.length > 0

  const body =
    mode === '2d' ? (
      <SpecGraph spec={spec} focusName={focus2d} />
    ) : mode === '3d' ? (
      <div>
        {hasRuntime && (
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8, flexWrap: 'wrap' }}>
            <span style={{ fontSize: 12, color: 'var(--c-ink-3)' }}>数据源</span>
            <Segmented
              size="small"
              value={src}
              onChange={(v) => setSrc(v as 'repo' | 'runtime')}
              options={[
                { value: 'repo', label: '仓库（事实源）' },
                { value: 'runtime', label: '运行态实渲（引擎）' },
              ]}
            />
            {src === 'runtime' && (
              <>
                <Select
                  size="small"
                  style={{ minWidth: 180 }}
                  value={rt.profileId}
                  onChange={rt.setProfileId}
                  options={rt.profiles.map((p) => ({ value: p.id, label: `${p.name}（:${p.port}）` }))}
                />
                <Button size="small" icon={<ReloadOutlined />} onClick={rt.reload}>
                  重新拉取
                </Button>
                {rt.loading && <Spin size="small" />}
              </>
            )}
          </div>
        )}
        {src === 'runtime' && rt.err && <Alert type="warning" showIcon message="运行态拉取失败" description={rt.err} style={{ marginBottom: 8 }} />}
        {src === 'runtime' && !rt.spec && !rt.loading && !rt.err && (
          <Alert type="info" showIcon message="选择运行方案后拉取引擎实装的类层次（实例以计数呈现）" style={{ marginBottom: 8 }} />
        )}
        <Graph3D
          spec={src === 'runtime' ? rt.spec : spec}
          sparqlProfile={src === 'runtime' ? null : sparqlProfile}
          onRequest2D={(name) => {
            setFocus2d(name)
            setSrc('repo')
            setMode('2d')
          }}
        />
        {src === 'runtime' && (
          <Typography.Text type="secondary" style={{ fontSize: 11, display: 'block', marginTop: 6 }}>
            运行态视角为引擎实装 TBox（实例以计数后缀呈现）；全量实例浏览请切回仓库源（渐进通道可用）
          </Typography.Text>
        )}
      </div>
    ) : mode === 'webvowl' ? (
      <WebVowlView ontologyId={ontologyId} />
    ) : (
      <OntologyCompanionGraph ontologyId={ontologyId} />
    )

  return (
    <Maximizeable label="最大化">
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8, flexWrap: 'wrap' }}>
        <Segmented
          size="small"
          value={mode}
          onChange={(v) => setMode(v as '2d' | '3d' | 'webvowl' | 'companion')}
          options={[
            { value: '2d', label: '2D 结构（React Flow）' },
            { value: '3d', label: '三维浏览' },
            { value: 'webvowl', label: 'WebVOWL 对照（OWL 视觉语言）' },
            ...(isCompanion ? [{ value: 'companion' as const, label: '伴生成长图（对话生长）' }] : []),
          ]}
        />
      </div>
      {body}
    </Maximizeable>
  )
}
