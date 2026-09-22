import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Empty,
  Input,
  Modal,
  Popconfirm,
  Space,
  Tag,
  Typography,
} from 'antd'
import { CopyOutlined, DeleteOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { api } from '../../../api/client'
import type {
  PipelineCatalogResponse,
  PipelineChecklistItem,
  PipelineProfile,
  PipelineStageSelection,
  PipelineStageTool,
} from '../../../api/types'
import { useUI } from '../../../store/ui'

// ---------------------------------------------------------------------------
// 工具链配置页（REQ-75/76，04 §4.6）：路径级工具链配置 + guided 引导打卡
//   七阶段纵向步骤条 × 候选卡片（名称/许可/模式/形态/学习要点）；选中即保存。
//   默认工具链每阶段预置 builtin 项，打开即可用；复制/重置/删除。
//   guided 打卡走 checklist 的 tool:<stage>:<tool_id> key（与学习中心 task: 分列）。
// ---------------------------------------------------------------------------

const STAGE_NAMES: Record<string, string> = {
  s1: 'S1 本体来源',
  s2: 'S2 编辑管理',
  s3: 'S3 校验',
  s4: 'S4 可视化浏览',
  s5: 'S5 运行时引擎',
  s6: 'S6 服务化接口',
  s7: 'S7 对接 Agent',
}

const MODE_TAG: Record<string, { color: string; text: string }> = {
  builtin: { color: 'blue', text: '内置' },
  guided: { color: 'orange', text: '引导执行' },
  managed: { color: 'purple', text: '自动编排' },
}

const STAGE_TIPS: Record<string, string> = {
  s1: '从 0 获得本体（无 → spec_json / original）',
  s2: '修改完善本体（spec_json ⇄ spec_json）',
  s3: '保证正确性（→ 校验报告）：结构 → 逻辑 → 业务约束三层',
  s4: '理解结构（→ 交互视图）',
  s5: '让本体"活"起来（→ 运行中的数据服务；managed 归运行栏）',
  s6: '标准化访问（→ SPARQL endpoint / MCP / REST）',
  s7: '被智能体消费（MCP endpoint → 对话能力）',
}

const DEFAULT_STAGES: Record<string, PipelineStageSelection> = {
  s1: { tool: 'builtin_import', mode: 'builtin' },
  s2: { tool: 'builtin_editor', mode: 'builtin' },
  s3: { tool: 'builtin_validate', mode: 'builtin' },
  s4: { tool: 'builtin_reactflow', mode: 'builtin' },
  s5: { tool: 'builtin_oxigraph', mode: 'managed' },
  s6: { tool: 'builtin_sparql', mode: 'builtin' },
  s7: { tool: 'builtin_agent_mount', mode: 'builtin' },
}

export default function PipelinePane() {
  const { showToast } = useUI()
  const [catalog, setCatalog] = useState<PipelineCatalogResponse | null>(null)
  const [pipelines, setPipelines] = useState<PipelineProfile[] | null>(null)
  const [activeId, setActiveId] = useState<string | null>(null)
  const [checklist, setChecklist] = useState<PipelineChecklistItem[]>([])
  const [busy, setBusy] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [newName, setNewName] = useState('')

  const active = useMemo(() => pipelines?.find((p) => p.id === activeId) ?? null, [pipelines, activeId])

  const reload = (selectId?: string) => {
    api
      .listPipelineCatalog()
      .then(setCatalog)
      .catch((e) => showToast(`候选清单加载失败：${e.message}`, 'err'))
    api
      .listPipelines()
      .then((ps) => {
        setPipelines(ps)
        const target = selectId ?? activeId
        setActiveId(target && ps.some((p) => p.id === target) ? target : (ps[0]?.id ?? null))
      })
      .catch((e) => showToast(`配置列表加载失败：${e.message}`, 'err'))
  }

  useEffect(() => {
    reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 选中配置 → 拉引导清单
  useEffect(() => {
    if (!activeId) {
      setChecklist([])
      return
    }
    let alive = true
    api
      .getPipeline(activeId)
      .then((d) => {
        if (alive) setChecklist(d.checklist)
      })
      .catch((e) => {
        if (alive) showToast(e.message, 'err')
      })
    return () => {
      alive = false
    }
  }, [activeId, pipelines])

  const selectTool = async (stage: string, tool: PipelineStageTool) => {
    if (!active) return
    setBusy(true)
    try {
      const stages = { ...active.stages, [stage]: { tool: tool.id, mode: tool.mode } }
      const updated = await api.updatePipeline(active.id, { stages })
      setPipelines((ps) => (ps ?? []).map((p) => (p.id === updated.id ? updated : p)))
      showToast(`${STAGE_NAMES[stage]} 已切换为「${tool.name}」`)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const toggleCheck = async (key: string, done: boolean) => {
    if (!active) return
    try {
      await api.checkPipeline(active.id, key, done)
      const d = await api.getPipeline(active.id)
      setChecklist(d.checklist)
      setPipelines((ps) => (ps ?? []).map((p) => (p.id === d.profile.id ? d.profile : p)))
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  const createPipeline = async () => {
    const name = newName.trim()
    if (!name) {
      showToast('请输入工具链名称', 'err')
      return
    }
    setBusy(true)
    try {
      const p = await api.createPipeline({ name })
      showToast(`已创建「${p.name}」（默认工具链）`)
      setCreateOpen(false)
      setNewName('')
      reload(p.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const cloneActive = async () => {
    if (!active) return
    setBusy(true)
    try {
      const p = await api.clonePipeline(active.id)
      showToast(`已复制为「${p.name}」`)
      reload(p.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const resetActive = async () => {
    if (!active) return
    setBusy(true)
    try {
      const updated = await api.updatePipeline(active.id, { stages: DEFAULT_STAGES })
      setPipelines((ps) => (ps ?? []).map((p) => (p.id === updated.id ? updated : p)))
      showToast('已重置为默认工具链')
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const removeActive = async () => {
    if (!active) return
    try {
      await api.deletePipeline(active.id)
      showToast('已删除工具链配置')
      setActiveId(null)
      reload()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  const doneCount = Object.keys(active?.checklist ?? {}).length

  return (
    <>
      <div className="onto-sec">
        <span className="onto-sec-title">工具链配置（REQ-75：路径级 pipeline_profile）</span>
        <span className="hit-spacer" />
        <Space>
          <Button size="small" icon={<ReloadOutlined />} onClick={() => reload()} />
          <Button size="small" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
            新建
          </Button>
          <Button size="small" icon={<CopyOutlined />} onClick={cloneActive} disabled={!active}>
            复制
          </Button>
          <Button size="small" onClick={resetActive} disabled={!active}>
            重置默认
          </Button>
          <Popconfirm title="删除该工具链配置？" onConfirm={removeActive} disabled={!active}>
            <Button size="small" danger icon={<DeleteOutlined />} disabled={!active} />
          </Popconfirm>
        </Space>
      </div>

      {pipelines !== null && pipelines.length === 0 ? (
        <Empty description="暂无工具链配置（新建一个，默认模板打开即可用）" style={{ margin: '32px 0' }} />
      ) : (
        <Space size={6} wrap style={{ marginBottom: 12 }}>
          {(pipelines ?? []).map((p) => (
            <Tag.CheckableTag key={p.id} checked={p.id === activeId} onChange={() => setActiveId(p.id)} style={{ fontSize: 13 }}>
              {p.name}
              {p.ontology_id ? ` · ${p.ontology_id.replace('onto_', '')}` : ''}
            </Tag.CheckableTag>
          ))}
        </Space>
      )}

      {active && checklist.length > 0 && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message={`引导执行清单（${doneCount}/${checklist.length} 已打卡）`}
          description={
            <ul style={{ margin: 0, paddingLeft: 18 }}>
              {checklist.map((c) => {
                const done = Boolean((active.checklist ?? {})[c.key])
                return (
                  <li key={c.key} style={{ marginBottom: 4 }}>
                    <Button
                      size="small"
                      type={done ? 'primary' : 'default'}
                      ghost={done}
                      onClick={() => toggleCheck(c.key, !done)}
                      style={{ marginRight: 8 }}
                    >
                      {done ? '已完成' : '打卡'}
                    </Button>
                    <b>{c.title}</b>
                    {c.detail && <span style={{ fontSize: 12 }}> — {c.detail}</span>}
                    {c.entry_url && (
                      <a href={c.entry_url} target="_blank" rel="noreferrer" style={{ fontSize: 12, marginLeft: 6 }}>
                        入口 ↗
                      </a>
                    )}
                  </li>
                )
              })}
            </ul>
          }
        />
      )}

      {active &&
        (catalog?.stages ?? []).map((sid) => {
          const sel = active.stages?.[sid]
          const tools = catalog?.catalog?.[sid] ?? []
          return (
            <Card
              key={sid}
              size="small"
              title={
                <Space size={8}>
                  <span>{STAGE_NAMES[sid] ?? sid}</span>
                  {sel && (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      当前：{tools.find((t) => t.id === sel.tool)?.name ?? sel.tool}
                    </Typography.Text>
                  )}
                </Space>
              }
              style={{ marginBottom: 8 }}
            >
              <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
                {STAGE_TIPS[sid]}
              </Typography.Paragraph>
              <Space size={6} wrap>
                {tools.map((t) => {
                  const chosen = sel?.tool === t.id
                  const mt = MODE_TAG[t.mode] ?? { color: 'default', text: t.mode }
                  return (
                    <Button
                      key={t.id}
                      size="small"
                      type={chosen ? 'primary' : 'default'}
                      disabled={busy}
                      onClick={() => selectTool(sid, t)}
                      title={t.learning}
                    >
                      {t.name}
                      <Tag color={mt.color} style={{ marginInlineStart: 6, marginInlineEnd: 0, fontSize: 11 }}>
                        {mt.text}
                      </Tag>
                    </Button>
                  )
                })}
              </Space>
            </Card>
          )
        })}

      <Modal
        title="新建工具链配置"
        open={createOpen}
        onOk={createPipeline}
        onCancel={() => setCreateOpen(false)}
        okText="创建"
        okButtonProps={{ disabled: !newName.trim() }}
      >
        <Input
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          placeholder="名称（如「推理对照工具链」）"
          onPressEnter={createPipeline}
        />
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          创建后每阶段预置 builtin 默认项，打开即可用；配置只是"换工具"。
        </Typography.Text>
      </Modal>
    </>
  )
}
