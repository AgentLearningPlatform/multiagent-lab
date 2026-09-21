import { useEffect, useMemo, useState } from 'react'
import {
  Button,
  Empty,
  Form,
  Input,
  Menu,
  Modal,
  Popconfirm,
  Result,
  Select,
  Space,
  Splitter,
  Switch,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import { EyeOutlined, MinusCircleOutlined, PlusOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { api } from '../api/client'
import type { Agent, Skill, ToolInfo } from '../api/types'
import { useUI } from '../store/ui'

type FilterKey = 'all' | 'mounted' | 'unmounted'

/**
 * 技能视图（原型 06 §3.5 / 02 文档 §10）：
 * 左栏筛选（全部 / 已挂载 / 未挂载，真实过滤：挂载 = 技能 id 出现在任一智能体 skills 中）
 * + 预留区（分组 / 标签 / 版本历史 / 市场导入，标注「规划中」且 disabled）；
 * 右栏技能卡片列表 + 新建；编辑弹窗含 instruction / tools 白名单 / resources 文本块 + 注入预览。
 */
export default function SkillsPage() {
  const { showToast, bumpData } = useUI()
  const [skills, setSkills] = useState<Skill[]>([])
  const [tools, setTools] = useState<ToolInfo[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [loadErr, setLoadErr] = useState<string | null>(null)
  const [filter, setFilter] = useState<FilterKey>('all')
  const [modal, setModal] = useState<Skill | 'new' | undefined>(undefined)
  const [preview, setPreview] = useState<{ skill: Skill; instruction: string } | null>(null)

  const reload = () => {
    api.listSkills()
      .then((ss) => {
        setSkills(ss)
        setLoadErr(null)
      })
      .catch((e: any) => {
        setSkills([])
        setLoadErr(e?.message ?? '加载失败')
      })
    // 工具注册表 / 智能体仅用于勾选与挂载计算，失败时优雅降级为空集
    api.listTools().then(setTools).catch(() => setTools([]))
    api.listAgents().then(setAgents).catch(() => setAgents([]))
  }

  useEffect(reload, [])

  // 挂载集合：技能 id 出现在任一智能体 skills 中即为已挂载
  const mountedIds = useMemo(() => {
    const s = new Set<string>()
    for (const a of agents) for (const id of a.skills ?? []) s.add(id)
    return s
  }, [agents])

  const counts = useMemo(() => {
    const mounted = skills.filter((s) => mountedIds.has(s.id)).length
    return { all: skills.length, mounted, unmounted: skills.length - mounted }
  }, [skills, mountedIds])

  const filtered = useMemo(() => {
    if (filter === 'mounted') return skills.filter((s) => mountedIds.has(s.id))
    if (filter === 'unmounted') return skills.filter((s) => !mountedIds.has(s.id))
    return skills
  }, [skills, filter, mountedIds])

  const openPreview = async (s: Skill) => {
    try {
      const r = await api.skillPreview(s.id)
      setPreview({ skill: s, instruction: r.instruction_block ?? (r as { instruction?: string }).instruction ?? '' })
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  const remove = async (s: Skill) => {
    try {
      await api.deleteSkill(s.id)
      showToast('已删除技能')
      bumpData()
      reload()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  return (
    <Splitter
      className="main sidebar-splitter"
      onResizeEnd={(sizes) => localStorage.setItem('eino.sidebar.width', String(Math.round(sizes[0])))}
    >
      <Splitter.Panel defaultSize={Number(localStorage.getItem('eino.sidebar.width')) || 280} min={220} max={480} className="sidebar-panel">
        <aside className="sidebar">
          <div className="side-head">
            <span className="side-title">技能筛选</span>
          </div>
          <Menu
            mode="vertical"
            selectedKeys={[filter]}
            onClick={({ key }) => setFilter(key as FilterKey)}
            style={{ padding: '0 10px', background: 'transparent' }}
            items={[
              { key: 'all', label: <Space size={6}>全部<Tag style={{ margin: 0 }}>{counts.all}</Tag></Space> },
              { key: 'mounted', label: <Space size={6}>已挂载<Tag color="green" style={{ margin: 0 }}>{counts.mounted}</Tag></Space> },
              { key: 'unmounted', label: <Space size={6}>未挂载<Tag style={{ margin: 0 }}>{counts.unmounted}</Tag></Space> },
            ]}
          />
          <Menu
            mode="vertical"
            selectable={false}
            style={{ padding: '0 10px', background: 'transparent' }}
            items={[
              {
                key: 'reserve',
                type: 'group',
                label: (
                  <Space size={6}>
                    预留区
                    <Tag color="purple" style={{ margin: 0 }}>
                      规划中
                    </Tag>
                  </Space>
                ),
                children: [
                  { key: 'reserve-group', label: '技能分组 / 标签', disabled: true },
                  { key: 'reserve-version', label: '版本历史', disabled: true },
                  { key: 'reserve-market', label: '技能市场导入', disabled: true },
                ],
              },
            ]}
          />
          <div className="side-reserve-wrap">
            <div className="reserve-note">
              「已挂载 / 未挂载」为真实过滤：挂载 = 技能出现在任一智能体的技能列表中。
            </div>
          </div>
        </aside>
      </Splitter.Panel>
      <Splitter.Panel className="content-panel">

        <div className="work-main">
          <div className="work-head">
            <div className="work-head-text">
              <div className="work-head-title">
                <Typography.Title level={4} style={{ margin: 0 }}>
                  技能
                </Typography.Title>
                <Tag style={{ margin: 0 }}>{filtered.length}</Tag>
              </div>
              <p className="work-head-desc">
                技能是<strong>配置级能力包</strong>（提示词 + 工具白名单），不提供脚本执行环境；装配期按「主指令 + # 启用技能」拼接注入（§6.12）。
              </p>
            </div>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setModal('new')}>
              新建技能
            </Button>
          </div>

          {loadErr ? (
            <div className="work-empty">
              <Result
                status="warning"
                title="技能后端未就绪"
                subTitle={`${loadErr}（M7 后端另行部署）`}
                extra={<Button onClick={reload}>重试</Button>}
              />
            </div>
          ) : filtered.length === 0 ? (
            <div className="work-empty">
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={skills.length === 0 ? '暂无技能，点击右上「新建技能」' : '该筛选下暂无技能'}
              />
            </div>
          ) : (
            <div className="skill-grid">
              {filtered.map((s) => (
                <div className="skill-card" key={s.id}>
                  <div className="skill-card-top">
                    <span className="skill-card-icon">
                      <ThunderboltOutlined />
                    </span>
                    <span className="skill-card-name" title={s.name}>
                      {s.name}
                    </span>
                    {s.builtin && (
                      <Tag color="gold" style={{ margin: 0 }}>
                        内置
                      </Tag>
                    )}
                    <Tag color={s.enabled ? 'blue' : 'default'} style={{ margin: 0 }}>
                      {s.enabled ? '启用' : '停用'}
                    </Tag>
                    {mountedIds.has(s.id) ? (
                      <Tag color="green" style={{ margin: 0 }}>
                        已挂载
                      </Tag>
                    ) : (
                      <Tag style={{ margin: 0 }}>未挂载</Tag>
                    )}
                  </div>
                  <p className="skill-card-desc">{s.description || '未填写描述'}</p>
                  <div className="skill-card-meta">
                    <span>工具 {s.tools?.length ?? 0}</span>
                    <span className="dot">·</span>
                    <span>资源 {s.resources?.length ?? 0}</span>
                  </div>
                  <div className="skill-card-ops">
                    <Button type="link" size="small" icon={<EyeOutlined />} onClick={() => openPreview(s)}>
                      注入预览
                    </Button>
                    <Button type="link" size="small" onClick={() => setModal(s)}>
                      编辑
                    </Button>
                    {s.builtin ? (
                      <Tooltip title="内置技能不可删除">
                        <span>
                          <Button type="link" size="small" danger disabled>
                            删除
                          </Button>
                        </span>
                      </Tooltip>
                    ) : (
                      <Popconfirm
                        title={`删除技能「${s.name}」？`}
                        description="已挂载该技能的智能体将失去对应能力。"
                        okText="删除"
                        okButtonProps={{ danger: true }}
                        cancelText="取消"
                        onConfirm={() => remove(s)}
                      >
                        <Button type="link" size="small" danger>
                          删除
                        </Button>
                      </Popconfirm>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        {modal !== undefined && (
          <SkillModal
            skill={modal}
            tools={tools}
            onClose={() => setModal(undefined)}
            onSaved={() => {
              setModal(undefined)
              bumpData()
              reload()
            }}
            onPreview={openPreview}
          />
        )}
        {preview && <PreviewModal skill={preview.skill} instruction={preview.instruction} onClose={() => setPreview(null)} />}
      </Splitter.Panel>
    </Splitter>
  )
}

/** 技能编辑弹窗：name / description / instruction（必填）/ tools 多选 / resources（Form.List） */
function SkillModal({
  skill,
  tools,
  onClose,
  onSaved,
  onPreview,
}: {
  skill: Skill | 'new'
  tools: ToolInfo[]
  onClose: () => void
  onSaved: () => void
  onPreview: (s: Skill) => void
}) {
  const { showToast } = useUI()
  const [form] = Form.useForm()
  const [busy, setBusy] = useState(false)
  const edit = skill === 'new' ? null : skill

  useEffect(() => {
    if (edit) {
      form.setFieldsValue({
        name: edit.name,
        description: edit.description,
        instruction: edit.instruction,
        tools: edit.tools ?? [],
        resources: edit.resources ?? [],
        enabled: edit.enabled,
      })
    } else {
      form.setFieldsValue({ name: '', description: '', instruction: '', tools: [], resources: [], enabled: true })
    }
  }, [skill, form, edit])

  const save = async () => {
    let v: any
    try {
      v = await form.validateFields()
    } catch {
      return
    }
    const resources = ((v.resources ?? []) as { name?: string; content?: string }[])
      .filter((r) => r && (r.name || r.content))
      .map((r) => ({ name: r.name ?? '', content: r.content ?? '' }))
    const payload = {
      name: v.name,
      description: v.description ?? '',
      instruction: v.instruction,
      tools: v.tools ?? [],
      resources,
      // 缺省视为启用：避免后端 full-replace 因字段缺失把技能停用
      enabled: typeof v.enabled === 'boolean' ? v.enabled : edit ? edit.enabled : true,
    }
    setBusy(true)
    try {
      if (edit) await api.updateSkill(edit.id, payload)
      else await api.createSkill(payload)
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
      title={edit ? `编辑技能 · ${edit.name}` : '新建技能'}
      width={680}
      onCancel={onClose}
      styles={{ body: { maxHeight: 'calc(100vh - 220px)', overflowY: 'auto', paddingRight: 8 } }}
      footer={
        <Space style={{ display: 'flex', justifyContent: 'space-between', width: '100%' }}>
          <Button disabled={!edit} onClick={() => edit && onPreview(edit)}>
            注入预览
          </Button>
          <Space>
            <Button onClick={onClose}>取消</Button>
            <Button type="primary" loading={busy} onClick={save}>
              保存
            </Button>
          </Space>
        </Space>
      }
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        <Form.Item name="name" label="名称" rules={[{ required: true, message: '名称必填' }]}>
          <Input placeholder="如：K8s 排障指引" maxLength={60} />
        </Form.Item>
        <Form.Item name="description" label="描述">
          <Input.TextArea autoSize={{ minRows: 2, maxRows: 3 }} placeholder="用途与触发场景（供智能体理解何时启用）" />
        </Form.Item>
        <Form.Item name="enabled" label="启用" valuePropName="checked" extra="停用后不参与装配注入；保存时后端按 full-replace 处理，缺省视为停用。">
          <Switch />
        </Form.Item>
        <Form.Item
          name="instruction"
          label="指令（instruction）"
          rules={[{ required: true, message: '指令必填' }]}
          extra="装配期按「主指令 + # 启用技能」拼接注入；技能仅配置级注入，不提供脚本执行环境。"
        >
          <Input.TextArea autoSize={{ minRows: 6, maxRows: 16 }} placeholder="技能提示词…" />
        </Form.Item>
        <Form.Item
          name="tools"
          label="工具白名单"
          extra={tools.length === 0 ? '工具注册表为空；请确认 M5 后端已就绪。' : '来自工具注册表；与智能体自身工具合并去重（§6.12）。'}
        >
          <Select
            mode="multiple"
            allowClear
            placeholder="选择该技能可用的工具"
            options={tools.map((t) => ({ value: t.id, label: `${t.name}${t.source ? ` · ${t.source}` : ''}` }))}
          />
        </Form.Item>
        <Form.Item label="资源（resources，可选）" style={{ marginBottom: 0 }} extra="按需拼入上下文（P2）；内容较大时后续演进为 get_skill_resource 工具。">
          <Form.List name="resources">
            {(fields, { add, remove }) => (
              <div className="res-list">
                {fields.map((field) => (
                  <div className="res-row" key={field.key}>
                    <Form.Item name={[field.name, 'name']} noStyle>
                      <Input placeholder="资源名" style={{ width: 180 }} />
                    </Form.Item>
                    <Form.Item name={[field.name, 'content']} noStyle>
                      <Input.TextArea autoSize={{ minRows: 1, maxRows: 4 }} placeholder="资源内容" style={{ flex: 1 }} />
                    </Form.Item>
                    <Button type="text" danger icon={<MinusCircleOutlined />} onClick={() => remove(field.name)} />
                  </div>
                ))}
                <Button type="dashed" block icon={<PlusOutlined />} onClick={() => add({ name: '', content: '' })}>
                  添加资源
                </Button>
              </div>
            )}
          </Form.List>
        </Form.Item>
      </Form>
    </Modal>
  )
}

/** 注入预览：展示后端组合后的完整 instruction（§6.12：主指令 + # 启用技能 段） */
function PreviewModal({ skill, instruction, onClose }: { skill: Skill; instruction: string; onClose: () => void }) {
  return (
    <Modal open centered title={`注入预览 · ${skill.name}`} width={680} onCancel={onClose} footer={<Button onClick={onClose}>关闭</Button>}>
      <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
        装配期将该技能拼入智能体主指令：主指令 + <Typography.Text code># 启用技能</Typography.Text> +{' '}
        <Typography.Text code>{'<skill name="…">…</skill>'}</Typography.Text>（§6.12）。
      </Typography.Paragraph>
      <pre className="preview-pre">{instruction || '（空）'}</pre>
    </Modal>
  )
}
