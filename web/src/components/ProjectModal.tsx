import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { Alert, Button, Checkbox, Col, Divider, Form, Input, Modal, Row, Select, Space, Spin, Tag } from 'antd'
import { api } from '../api/client'
import type { Agent, DirValidation } from '../api/types'
import { useUI } from '../store/ui'

/** 弹窗小节标题（左对齐小标题；inline 边距覆盖 antd Divider 默认间距） */
function Section({ children, first }: { children: ReactNode; first?: boolean }) {
  return (
    <Divider titlePlacement="left" plain style={{ margin: first ? '0 0 14px' : '6px 0 14px' }}>
      {children}
    </Divider>
  )
}

/**
 * 新建项目弹窗（REQ-103：编辑用途已迁至右侧边栏配置视图，本弹窗仅用于新建）。
 * 布局分组：基本信息 → 本地目录 → 协作模式 → 成员智能体 → 项目级约束；
 * 短字段走两列 Row/Col，长文本整行 autoSize。
 */
export default function ProjectModal({
  agents,
  onClose,
  onCreated,
}: {
  agents: Agent[]
  onClose: () => void
  onCreated: (id: string) => void
}) {
  const { showToast, bumpData } = useUI()
  const [form] = Form.useForm()
  const [selected, setSelected] = useState<Record<string, 'coordinator' | 'member'>>({})
  const [saving, setSaving] = useState(false)

  // REQ-101：本地目录绑定 + 检测
  const localDir = Form.useWatch('local_dir', form)
  const [dirCheck, setDirCheck] = useState<DirValidation | null>(null)
  const [checking, setChecking] = useState(false)

  useEffect(() => {
    form.setFieldsValue({ collab_mode: 'agent_as_tool', workflow_mode: 'free' })
  }, [form])

  const checkDir = async () => {
    const dir = (localDir ?? '').trim()
    if (!dir) return
    setChecking(true)
    setDirCheck(null)
    try {
      setDirCheck(await api.validateProjectDir(dir))
    } catch (e: any) {
      setDirCheck({ exists: false, is_dir: false, is_git: false, error: e?.message ?? '检测失败' })
    } finally {
      setChecking(false)
    }
  }

  const save = async () => {
    let v: any
    try {
      v = await form.validateFields()
    } catch {
      return
    }
    const members = Object.entries(selected).map(([agent_id, role]) => ({ agent_id, role }))
    setSaving(true)
    try {
      const p = await api.createProject({
        name: v.name,
        description: v.description ?? '',
        collab_mode: v.collab_mode ?? 'agent_as_tool',
        workflow_mode: v.workflow_mode ?? 'free',
        constraints: v.constraints ?? '',
        local_dir: v.local_dir ?? '',
      })
      if (members.length) await api.setProjectAgents(p.id, members)
      showToast('项目已创建')
      bumpData()
      onCreated(p.id)
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open
      onCancel={onClose}
      title="新建项目"
      width={680}
      centered
      styles={{ body: { maxHeight: 'calc(100vh - 220px)', overflowY: 'auto', paddingRight: 8 } }}
      footer={
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={saving} onClick={save}>
            创建
          </Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        <Section first>基本信息</Section>
        <Form.Item name="name" label="名称" rules={[{ required: true, message: '名称必填' }]}>
          <Input placeholder="项目名称" />
        </Form.Item>
        <Form.Item name="description" label="描述">
          <Input.TextArea autoSize={{ minRows: 2, maxRows: 5 }} />
        </Form.Item>

        <Section>本地目录（可选）</Section>
        <Form.Item
          label="本地目录（绝对路径）"
          extra="支持 Windows 盘符路径（C:\Users\…）与 POSIX 路径；绑定后，对话生成的文档（save_file）与文件列表将落在该目录；留空表示不绑定。"
        >
          <Space.Compact style={{ width: '100%' }}>
            <Form.Item name="local_dir" noStyle>
              <Input placeholder={'如 /home/me/project 或 C:\\Users\\me\\project'} allowClear />
            </Form.Item>
            <Button onClick={checkDir} loading={checking} disabled={!(localDir ?? '').trim()}>
              检测
            </Button>
          </Space.Compact>
        </Form.Item>
        {dirCheck && (
          <div className="dir-check">
            {dirCheck.error ? (
              <Alert type="error" showIcon message="目录检测失败" description={dirCheck.error} />
            ) : (
              <Space size={6} wrap>
                <Tag color={dirCheck.exists ? 'green' : 'red'} style={{ margin: 0 }}>
                  {dirCheck.exists ? '存在' : '不存在'}
                </Tag>
                <Tag color={dirCheck.is_dir ? 'green' : 'red'} style={{ margin: 0 }}>
                  {dirCheck.is_dir ? '目录' : '非目录'}
                </Tag>
                {dirCheck.is_git ? (
                  <>
                    <Tag color="blue" style={{ margin: 0 }}>
                      分支 {dirCheck.git_branch || '—'}
                    </Tag>
                    <Tag style={{ margin: 0 }}>{(dirCheck.git_commit || '').slice(0, 10) || '—'}</Tag>
                    {dirCheck.git_dirty ? (
                      <Tag color="orange" style={{ margin: 0 }}>
                        已修改
                      </Tag>
                    ) : (
                      <Tag color="green" style={{ margin: 0 }}>
                        干净
                      </Tag>
                    )}
                  </>
                ) : (
                  <Tag style={{ margin: 0 }}>非 Git 仓库</Tag>
                )}
              </Space>
            )}
          </div>
        )}
        {checking && !dirCheck && (
          <div className="dir-check">
            <Spin size="small" />
          </div>
        )}

        <Section>协作模式</Section>
        <Row gutter={16}>
          <Col span={12}>
            <Form.Item name="collab_mode" label="协作模式（M4 生效）">
              <Select
                options={[
                  { value: 'agent_as_tool', label: 'agent_as_tool（主智能体调度）' },
                  { value: 'transfer', label: 'transfer（路由移交）' },
                  { value: 'single', label: 'single（单智能体）' },
                ]}
              />
            </Form.Item>
          </Col>
          <Col span={12}>
            <Form.Item name="workflow_mode" label="工作流模式（M4 生效）">
              <Select
                options={[
                  { value: 'free', label: 'free（自由协作）' },
                  { value: 'sequential', label: 'sequential（顺序）' },
                  { value: 'parallel', label: 'parallel（并行）' },
                  { value: 'loop', label: 'loop（循环）' },
                ]}
              />
            </Form.Item>
          </Col>
        </Row>

        <Section>成员智能体</Section>
        <div className="member-block">
          <div className="member-hint">项目会话由主智能体调度（M4 生效）；勾选成员并指定主智能体。</div>
          {agents.length === 0 && <div className="empty-hint">还没有智能体，请先到「智能体」页创建</div>}
          {agents.map((a) => (
            <div key={a.id} className="member-row">
              <Checkbox
                checked={!!selected[a.id]}
                onChange={(e) =>
                  setSelected((s) => {
                    const next = { ...s }
                    if (e.target.checked) next[a.id] = 'member'
                    else delete next[a.id]
                    return next
                  })
                }
              >
                {a.name}
              </Checkbox>
              {selected[a.id] && (
                <Select
                  size="small"
                  style={{ width: 130 }}
                  value={selected[a.id]}
                  onChange={(role) => setSelected((s) => ({ ...s, [a.id]: role }))}
                  options={[
                    { value: 'member', label: '成员' },
                    { value: 'coordinator', label: '主智能体' },
                  ]}
                />
              )}
            </div>
          ))}
        </div>

        <Section>项目级约束</Section>
        <Form.Item name="constraints" label="项目级约束（统一注入成员提示词，P1）">
          <Input.TextArea autoSize={{ minRows: 2, maxRows: 6 }} />
        </Form.Item>
      </Form>
    </Modal>
  )
}
