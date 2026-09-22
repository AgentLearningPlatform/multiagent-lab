// OntoChatFlow 对话式多轮本体引导（REQ-103 模式 A）。
// 交互流：cq（领域描述+CQ）→ domain（逐轮补全，模型归纳+引导）→ 生成草稿（校验循环后端内聚）
// → 预览确认入库（REQ-82 门控）或回复修改意见进入 refine。会话留痕可切换/删除。
import { useCallback, useEffect, useRef, useState } from 'react'
import { Alert, Button, Card, Empty, Input, List, Popconfirm, Space, Spin, Tag, Typography } from 'antd'
import { DeleteOutlined, PlusOutlined, SendOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { api, ApiError } from '../../api/client'
import type { OntoChatSession, Spec } from '../../api/types'
import { useUI } from '../../store/ui'

const STAGE_TAG: Record<string, { color: string; text: string }> = {
  cq: { color: 'default', text: '① 能力问题' },
  domain: { color: 'processing', text: '② 领域补全' },
  draft: { color: 'blue', text: '③ 草稿就绪' },
  refine: { color: 'warning', text: '③ 草稿待修正' },
  done: { color: 'green', text: '④ 已入库' },
}

/** 首轮输入模板（cq 阶段占位提示） */
const CQ_TEMPLATE = '软件缺陷管理系统\n缺陷源于哪个需求？\n缺陷影响哪些模块？'

export default function OntoChatFlow({ onSaved }: { onSaved: (ontologyId: string) => void }) {
  const { showToast } = useUI()
  const [sessions, setSessions] = useState<OntoChatSession[]>([])
  const [active, setActive] = useState<OntoChatSession | null>(null)
  const [loading, setLoading] = useState(true)
  const [turning, setTurning] = useState(false)
  const [input, setInput] = useState('')
  const [draft, setDraft] = useState<Spec | null>(null)
  const [draftWarning, setDraftWarning] = useState('')
  const [saveName, setSaveName] = useState('')
  const listEndRef = useRef<HTMLDivElement>(null)

  const refreshList = useCallback(async () => {
    try {
      const list = await api.listOntoChatSessions()
      setSessions(list)
      return list
    } catch {
      return []
    }
  }, [])

  const openSession = useCallback(async (id: string) => {
    setLoading(true)
    try {
      const s = await api.getOntoChatSession(id)
      setActive(s)
      // 从上下文恢复草稿预览（refine/done 阶段重进不丢）
      if (s.context?.draft_spec) {
        try {
          setDraft(s.context.draft_spec as Spec)
        } catch {
          setDraft(null)
        }
      } else {
        setDraft(null)
      }
      setDraftWarning('')
      setSaveName(s.title || '')
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setLoading(false)
    }
  }, [showToast])

  useEffect(() => {
    ;(async () => {
      const list = await refreshList()
      if (list.length > 0) await openSession(list[0].id)
      else setLoading(false)
    })()
  }, [refreshList, openSession])

  useEffect(() => {
    listEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [active?.messages?.length, turning])

  const newSession = async () => {
    try {
      const s = await api.createOntoChatSession(`引导 ${new Date().toLocaleString()}`)
      await refreshList()
      setActive(s)
      setDraft(null)
      setDraftWarning('')
      setSaveName('')
      setInput('')
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  const removeSession = async (id: string) => {
    try {
      await api.deleteOntoChatSession(id)
      const list = await refreshList()
      if (active?.id === id) {
        setActive(null)
        setDraft(null)
        if (list.length > 0) await openSession(list[0].id)
      }
      showToast('会话已删除')
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  const send = async (text: string, feedback?: string) => {
    if (!active) return
    if (!text.trim() && !feedback?.trim()) {
      showToast('请输入内容', 'err')
      return
    }
    setTurning(true)
    try {
      const r = await api.ontoChatTurn(active.id, text, feedback)
      setActive(r.session)
      setInput('')
      if (r.draft) {
        setDraft(r.draft)
        setSaveName(r.draft.name || '')
      }
      if (r.warning) setDraftWarning(r.warning)
      else if (r.draft) setDraftWarning('')
      await refreshList()
    } catch (e: any) {
      if (e instanceof ApiError && e.status === 503) showToast('LLM 未配置：请在「设置-模型连接」配置默认 chat 连接', 'err')
      else showToast(e.message, 'err')
    } finally {
      setTurning(false)
    }
  }

  const saveDraft = async () => {
    if (!active || !draft) return
    const nm = saveName.trim() || draft.name || ''
    if (!nm) {
      showToast('请输入本体名称', 'err')
      return
    }
    setTurning(true)
    try {
      const r = await api.ontoChatSave(active.id, nm)
      showToast(`已创建本体「${r.ontology.name}」`)
      await refreshList()
      setActive(r.session)
      onSaved(r.ontology.id)
    } catch (e: any) {
      if (e instanceof ApiError && e.validationErrors?.length) showToast(`草稿校验未通过：${e.validationErrors[0].message}`, 'err')
      else showToast(e.message, 'err')
    } finally {
      setTurning(false)
    }
  }

  const stage = active?.stage ?? 'cq'
  const stageTag = STAGE_TAG[stage] ?? STAGE_TAG.cq
  const isDone = stage === 'done'
  const placeholder =
    stage === 'cq'
      ? `第一行领域描述，其后每行一条能力问题（CQ），如：\n${CQ_TEMPLATE}`
      : stage === 'domain'
        ? '补充领域信息（概念/层级/关系/实例来源）…或回复「生成草稿」直接产出'
        : '回复修改意见进入修正轮（如：给 Deployment 增加副本数属性）…'

  return (
    <Card className="work-card" size="small">
      <Alert
        type="success"
        showIcon
        style={{ marginBottom: 12 }}
        message="对话式本体构建（OntoChat 流程，REQ-103 模式 A 已交付）"
        description="对话式 CQ 引导 → 逐轮补全领域信息 → spec_json 草稿（生成-校验循环后端内聚）→ 预览确认入库。复用主平台模型代理，无新服务。"
      />
      <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start' }}>
        {/* 会话列表 */}
        <div style={{ width: 220, flexShrink: 0 }}>
          <Space style={{ marginBottom: 8, width: '100%', justifyContent: 'space-between' }}>
            <Typography.Text strong>会话</Typography.Text>
            <Button size="small" icon={<PlusOutlined />} onClick={newSession}>
              新建
            </Button>
          </Space>
          <List
            size="small"
            dataSource={sessions}
            locale={{ emptyText: '暂无会话' }}
            renderItem={(s) => (
              <List.Item
                style={{
                  cursor: 'pointer',
                  padding: '6px 8px',
                  background: active?.id === s.id ? 'var(--ant-color-primary-bg, #e6f4ff)' : undefined,
                  borderRadius: 6,
                }}
                onClick={() => openSession(s.id)}
                actions={[
                  <Popconfirm key="del" title="删除该会话？" onConfirm={(e) => { e?.stopPropagation(); removeSession(s.id) }}>
                    <Button type="text" size="small" icon={<DeleteOutlined />} onClick={(e) => e.stopPropagation()} />
                  </Popconfirm>,
                ]}
              >
                <div style={{ minWidth: 0 }}>
                  <div style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: 13 }}>{s.title || s.id}</div>
                  <Tag {...(STAGE_TAG[s.stage] ?? {})} style={{ margin: 0, fontSize: 11 }}>
                    {STAGE_TAG[s.stage]?.text ?? s.stage}
                  </Tag>
                </div>
              </List.Item>
            )}
          />
        </div>

        {/* 对话区 */}
        <div style={{ flex: 1, minWidth: 0 }}>
          {loading ? (
            <div style={{ textAlign: 'center', padding: 40 }}><Spin /></div>
          ) : !active ? (
            <Empty description="新建或选择一个会话开始" style={{ padding: 40 }} />
          ) : (
            <>
              <Space size={8} style={{ marginBottom: 8 }}>
                <Tag {...stageTag} style={{ margin: 0 }}>{stageTag.text}</Tag>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>轮数 {active.round}</Typography.Text>
                {active.ontology_id && (
                  <Tag color="green" style={{ margin: 0 }}>产物 {active.ontology_id}</Tag>
                )}
              </Space>
              <div
                className="onto-chat-msgs"
                style={{ maxHeight: 380, overflowY: 'auto', padding: '4px 2px', display: 'flex', flexDirection: 'column', gap: 8 }}
              >
                {(active.messages ?? []).map((m, i) => (
                  <div
                    key={i}
                    style={{
                      alignSelf: m.role === 'user' ? 'flex-end' : 'flex-start',
                      maxWidth: '86%',
                      background: m.role === 'user' ? 'var(--ant-color-primary-bg, #e6f4ff)' : 'var(--ant-color-bg-layout, #f5f5f5)',
                      borderRadius: 8,
                      padding: '6px 10px',
                      whiteSpace: 'pre-wrap',
                      fontSize: 13,
                      lineHeight: 1.6,
                    }}
                  >
                    {m.content}
                  </div>
                ))}
                {turning && (
                  <div style={{ alignSelf: 'flex-start' }}>
                    <Spin size="small" /> <Typography.Text type="secondary" style={{ fontSize: 12 }}>思考中…</Typography.Text>
                  </div>
                )}
                <div ref={listEndRef} />
              </div>

              {/* 草稿预览 + 入库（REQ-82 门控：必须经用户确认） */}
              {draft && !isDone && (
                <Card size="small" style={{ marginTop: 10 }} title="草稿预览（确认后入库）">
                  <Space size={6} wrap>
                    <Tag color="blue" style={{ margin: 0 }}>概念 {draft.concepts?.length ?? 0}</Tag>
                    <Tag color="geekblue" style={{ margin: 0 }}>关系 {draft.relations?.length ?? 0}</Tag>
                    <Tag color="purple" style={{ margin: 0 }}>实例 {draft.instances?.length ?? 0}</Tag>
                  </Space>
                  {draftWarning && <Alert type="warning" showIcon style={{ marginTop: 8 }} message={draftWarning} />}
                  <Space style={{ marginTop: 10 }} size={8}>
                    <Input value={saveName} onChange={(e) => setSaveName(e.target.value)} placeholder="本体名称" style={{ width: 240 }} />
                    <Button type="primary" icon={<ThunderboltOutlined />} loading={turning} onClick={saveDraft}>
                      创建并保存
                    </Button>
                  </Space>
                </Card>
              )}
              {isDone && active.ontology_id && (
                <Alert type="success" showIcon style={{ marginTop: 10 }} message={`草稿已入库为「${active.title}」关联的本体 ${active.ontology_id}，可到「本体资产」栏继续编辑与部署`} />
              )}

              {/* 输入区 */}
              {!isDone && (
                <div style={{ marginTop: 10, display: 'flex', gap: 8 }}>
                  <Input.TextArea
                    value={input}
                    onChange={(e) => setInput(e.target.value)}
                    placeholder={placeholder}
                    autoSize={{ minRows: 2, maxRows: 5 }}
                    disabled={turning}
                    onPressEnter={(e) => {
                      if (!e.shiftKey) {
                        e.preventDefault()
                        send(input)
                      }
                    }}
                  />
                  <Button type="primary" icon={<SendOutlined />} loading={turning} onClick={() => send(input)}>
                    发送
                  </Button>
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </Card>
  )
}
