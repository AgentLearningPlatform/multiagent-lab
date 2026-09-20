import { useEffect, useMemo, useState } from 'react'
import { Button, Collapse, Empty, Space } from 'antd'
import { PlusOutlined, SettingOutlined, DeleteOutlined } from '@ant-design/icons'
import { Conversations } from '@ant-design/x'
import { api } from '../api/client'
import type { Agent, Conversation, Project } from '../api/types'
import { useUI } from '../store/ui'
import { confirmAction } from '../lib/antd'

interface TreeNode {
  key: string
  name: string
  convs: Conversation[]
}

/**
 * 左栏树（原型 06 §3.1 / §3.2，Ant Design X Conversations + antd Collapse）：
 * - mode=agent：智能体折叠列表，每个智能体节点常驻「＋新对话」「⚙配置」，其下挂该智能体的对话
 * - mode=project：项目折叠列表，每个项目节点同样入口，其下挂项目对话
 */
export default function Sidebar({
  mode,
  agents,
  projects,
  conversations,
  activeAgentId,
  activeProjectId,
  onSelectAgent,
  onSelectProject,
  onNewAgent,
  onNewProject,
  onNewConversation,
  onConfigureAgent,
  onConfigureProject,
}: {
  mode: 'agent' | 'project'
  agents: Agent[]
  projects: Project[]
  conversations: Conversation[]
  activeAgentId: string | null
  activeProjectId: string | null
  onSelectAgent: (id: string) => void
  onSelectProject: (id: string) => void
  onNewAgent: () => void
  onNewProject: () => void
  onNewConversation: (nodeId: string) => void
  onConfigureAgent: (id: string) => void
  onConfigureProject: (id: string) => void
}) {
  const { currentConvId, setCurrentConv, bumpData, showToast } = useUI()
  const [open, setOpen] = useState<Record<string, boolean>>({})

  const nodes = useMemo<TreeNode[]>(() => {
    const byCreated = (a: Conversation, b: Conversation) => a.created_at.localeCompare(b.created_at)
    if (mode === 'agent') {
      return agents.map((a) => ({
        key: a.id,
        name: a.name,
        convs: conversations.filter((c) => c.scope === 'agent' && c.agent_id === a.id).sort(byCreated),
      }))
    }
    return projects.map((p) => ({
      key: p.id,
      name: p.name,
      convs: conversations.filter((c) => c.scope === 'project' && c.project_id === p.id).sort(byCreated),
    }))
  }, [mode, agents, projects, conversations])

  const activeId = mode === 'agent' ? activeAgentId : activeProjectId

  // 选中节点自动展开
  useEffect(() => {
    if (activeId) setOpen((o) => (o[activeId] ? o : { ...o, [activeId]: true }))
  }, [activeId])

  const selectNode = (id: string) => {
    if (mode === 'agent') onSelectAgent(id)
    else onSelectProject(id)
  }

  const removeConversation = (id: string) => {
    confirmAction('删除该对话及其全部消息？', '删除后不可恢复。', async () => {
      try {
        await api.deleteConversation(id)
        if (currentConvId === id) setCurrentConv(null)
        bumpData()
      } catch (e: any) {
        showToast(e.message, 'err')
      }
    })
  }

  const isAgent = mode === 'agent'
  const configure = (id: string) => {
    if (isAgent) onConfigureAgent(id)
    else onConfigureProject(id)
  }

  return (
    <div className="sidebar">
      <div className="side-head">
        <span className="side-title">{isAgent ? '智能体' : '项目'}</span>
      </div>
      <Button block type="dashed" icon={<PlusOutlined />} onClick={isAgent ? onNewAgent : onNewProject} style={{ margin: '0 12px 8px', width: 'calc(100% - 24px)' }}>
        新建{isAgent ? '智能体' : '项目'}
      </Button>
      <div className="conv-list">
        {nodes.length === 0 && (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={isAgent ? '还没有智能体，点击上方创建' : '还没有项目，点击上方创建'}
            style={{ marginTop: 40 }}
          />
        )}
        <Collapse
          ghost
          size="small"
          activeKey={nodes.filter((n) => open[n.key]).map((n) => n.key)}
          onChange={(keys) => {
            const arr = Array.isArray(keys) ? keys : [keys]
            setOpen(Object.fromEntries(nodes.map((n) => [n.key, arr.includes(n.key)])))
            // 点击标题即选中该智能体/项目
            const last = arr[arr.length - 1]
            if (last) selectNode(last)
          }}
          items={nodes.map((n) => ({
            key: n.key,
            label: (
              <span className="node-title" aria-current={activeId === n.key ? 'true' : undefined}>
                {n.name}
              </span>
            ),
            extra: (
              <Space size={0} onClick={(e) => e.stopPropagation()}>
                <Button type="text" size="small" icon={<PlusOutlined />} title="新建对话" onClick={() => onNewConversation(n.key)} />
                <Button type="text" size="small" icon={<SettingOutlined />} title="配置" onClick={() => configure(n.key)} />
              </Space>
            ),
            children: (
              <Conversations
                items={n.convs.map((c) => ({ key: c.id, label: c.title || '未命名对话' }))}
                activeKey={currentConvId ?? undefined}
                onActiveChange={setCurrentConv}
                menu={(c) => ({
                  items: [{ key: 'delete', label: '删除', icon: <DeleteOutlined />, danger: true }],
                  onClick: ({ key }) => {
                    if (key === 'delete') removeConversation(c.key)
                  },
                })}
              />
            ),
          }))}
        />
        {nodes.some((n) => open[n.key] && n.convs.length === 0) && null}
        {nodes.length > 0 && (
          <div className="empty-hint">展开节点后点 ＋ 新建对话</div>
        )}
      </div>
    </div>
  )
}
