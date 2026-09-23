import { useEffect, useMemo, useState } from 'react'
import { Button } from 'antd'
import {
  PlusOutlined,
  SettingOutlined,
  DeleteOutlined,
  EditOutlined,
  DownOutlined,
  RightOutlined,
  ProjectOutlined,
} from '@ant-design/icons'
import { Conversations } from '@ant-design/x'
import { api } from '../api/client'
import EmptyGuide from './EmptyGuide'
import type { Agent, Conversation, Project } from '../api/types'
import { useUI } from '../store/ui'
import { confirmAction } from '../lib/antd'
import NameModal from './NameModal'

interface TreeNode {
  key: string
  name: string
  convs: Conversation[]
}

/**
 * 左栏树（原型 06 §3.1 / §3.2）：
 * - mode=agent：智能体节点（头像点 + 对话数徽标 + 常驻「＋新对话」「⚙配置」），其下挂该智能体的对话；
 * - mode=project：项目节点同样结构，其下挂项目对话；
 * - 自绘节点头部 + 子树缩进导轨（替代 antd Collapse），统一行高与间距；会话行仍用 X Conversations（含重命名/删除菜单）。
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
  const [renaming, setRenaming] = useState<Conversation | null>(null)

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

  // 点击节点头部：展开/收起；展开时同时选中该节点
  const toggleNode = (id: string) => {
    const next = !open[id]
    setOpen((o) => ({ ...o, [id]: next }))
    if (next) selectNode(id)
  }

  const isAgent = mode === 'agent'
  const configure = (id: string) => {
    if (isAgent) onConfigureAgent(id)
    else onConfigureProject(id)
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

  const renameConversation = async (name: string) => {
    const target = renaming
    setRenaming(null)
    if (!target) return
    try {
      await api.updateConversation(target.id, { ...target, title: name })
      bumpData()
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  return (
    <div className="sidebar">
      <div className="side-head">
        <span className="side-title">{isAgent ? '智能体' : '项目'}</span>
        <span className="side-count">{nodes.length}</span>
      </div>
      <div className="side-actions">
        <Button block type="dashed" icon={<PlusOutlined />} onClick={isAgent ? onNewAgent : onNewProject}>
          新建{isAgent ? '智能体' : '项目'}
        </Button>
      </div>

      <div className="conv-list">
        {nodes.length === 0 && (
          isAgent ? (
            <EmptyGuide
              title="创建第一个智能体"
              steps={[
                '起个名称，写一句话描述它的职责（系统提示词）',
                '模型连接可留空 = 跟随全局默认（设置页已预置 DeepSeek）',
                '需要执行动作就勾选工具（current_time / ask_human 等）',
              ]}
              actionLabel="＋ 新建智能体"
              onAction={onNewAgent}
              footer="创建后点节点上的 ＋ 新建对话即可开始。"
            />
          ) : (
            <EmptyGuide
              title="创建第一个项目"
              steps={[
                '起个名称，选择协作模式（默认 agent_as_tool）',
                '可绑定本地目录：对话产物写入该目录，文件/Git 侧边栏可见（绑定即授予模型该目录读写权限）',
                '在项目配置中勾选成员智能体并指定主智能体',
              ]}
              actionLabel="＋ 新建项目"
              onAction={onNewProject}
              footer="项目会话由成员智能体协作处理。"
            />
          )
        )}

        {nodes.map((n) => {
          const expanded = !!open[n.key]
          const active = activeId === n.key
          return (
            <div key={n.key} className={`side-node${active ? ' active' : ''}`}>
              <div className="side-node-row">
                <button
                  type="button"
                  className="side-node-head"
                  aria-expanded={expanded}
                  onClick={() => toggleNode(n.key)}
                >
                  <span className="side-node-chev">{expanded ? <DownOutlined /> : <RightOutlined />}</span>
                  {/* 智能体用品牌同源的三节点标记；项目保留各自图标，一眼可辨 */}
                  <span className="side-node-dot">{isAgent ? <span className="agent-glyph" /> : <ProjectOutlined />}</span>
                  <span className="side-node-name" title={n.name}>{n.name}</span>
                  <span className="side-node-count">{n.convs.length}</span>
                </button>
                <span className="side-node-ops">
                  <Button type="text" size="small" icon={<PlusOutlined />} title="新建对话" onClick={() => onNewConversation(n.key)} />
                  <Button type="text" size="small" icon={<SettingOutlined />} title="配置" onClick={() => configure(n.key)} />
                </span>
              </div>

              {expanded && (
                <div className="side-node-children">
                  {n.convs.length === 0 ? (
                    <div className="side-node-empty">暂无对话 · 点 ＋ 新建</div>
                  ) : (
                    <Conversations
                      items={n.convs.map((c) => ({ key: c.id, label: c.title || '未命名对话' }))}
                      activeKey={currentConvId ?? undefined}
                      onActiveChange={setCurrentConv}
                      menu={(c) => ({
                        items: [
                          { key: 'rename', label: '重命名', icon: <EditOutlined /> },
                          { key: 'delete', label: '删除', icon: <DeleteOutlined />, danger: true },
                        ],
                        onClick: ({ key }) => {
                          const conv = n.convs.find((x) => x.id === c.key)
                          if (!conv) return
                          if (key === 'rename') setRenaming(conv)
                          else if (key === 'delete') removeConversation(conv.id)
                        },
                      })}
                    />
                  )}
                </div>
              )}
            </div>
          )
        })}

        {nodes.length > 0 && <div className="empty-hint">展开节点后点 ＋ 新建对话</div>}
      </div>

      {renaming && (
        <NameModal
          open
          title="重命名对话"
          placeholder="对话名称"
          okText="保存"
          onCancel={() => setRenaming(null)}
          onSubmit={renameConversation}
        />
      )}
    </div>
  )
}
