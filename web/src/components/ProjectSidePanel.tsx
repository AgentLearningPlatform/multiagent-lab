import { useEffect, useState } from 'react'
import { Alert, Button, Empty, Spin, Tag, Tooltip, Typography } from 'antd'
import {
  BranchesOutlined,
  CloseOutlined,
  FileOutlined,
  FolderOpenOutlined,
  FolderOutlined,
  LeftOutlined,
  ReloadOutlined,
  SettingOutlined,
} from '@ant-design/icons'
import { api } from '../api/client'
import type { DirValidation, Project, ProjectDirEntry } from '../api/types'

type PanelView = 'files' | 'git' | 'config'

/** 单文件预览体积上限（与后端 dir-file ≤1MB 对齐） */
const LARGE_FILE = 1_000_000

function fmtSize(n?: number): string {
  if (typeof n !== 'number' || n < 0) return ''
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

/** git 状态 → 彩色 Tag（兼容 porcelain 单字母与语义词） */
function gitStatusTag(s?: string | null) {
  if (!s) return null
  const k = s.toLowerCase()
  const map: Record<string, { color: string; text: string }> = {
    modified: { color: 'orange', text: '修改' },
    m: { color: 'orange', text: '修改' },
    added: { color: 'green', text: '新增' },
    a: { color: 'green', text: '新增' },
    untracked: { color: 'default', text: '未跟踪' },
    '??': { color: 'default', text: '未跟踪' },
    deleted: { color: 'red', text: '删除' },
    d: { color: 'red', text: '删除' },
  }
  const hit = map[k] ?? { color: 'default', text: s }
  return (
    <Tag color={hit.color} style={{ margin: 0 }}>
      {hit.text}
    </Tag>
  )
}

/**
 * 项目右侧侧边栏（REQ-102，§VSCode activity bar 范式）：
 * 左侧 ~44px 小图标条（文件 / Git / 配置）+ 右侧视图区（~320px）。
 * 由 ProjectsPage 渲染为内容区 flex 兄弟节点，ChatWindow 头部提供开合入口。
 */
export default function ProjectSidePanel({
  project,
  open,
  onClose,
  onEditProject,
}: {
  project: Project
  open: boolean
  onClose: () => void
  /** 配置视图「编辑项目」→ 打开 ProjectModal（由页面提供） */
  onEditProject?: () => void
}) {
  const [view, setView] = useState<PanelView>('files')

  // 切换项目时回到文件视图
  useEffect(() => {
    setView('files')
  }, [project.id])

  return (
    <aside className={`proj-panel${open ? ' open' : ''}`}>
      <div className="proj-panel-bar" role="tablist" aria-label="项目侧边栏视图">
        <Tooltip title="文件视图" placement="left">
          <button
            type="button"
            className={`proj-bar-btn${view === 'files' ? ' active' : ''}`}
            aria-label="文件视图"
            aria-selected={view === 'files'}
            role="tab"
            onClick={() => setView('files')}
          >
            <FolderOutlined />
          </button>
        </Tooltip>
        <Tooltip title="Git 视图" placement="left">
          <button
            type="button"
            className={`proj-bar-btn${view === 'git' ? ' active' : ''}`}
            aria-label="Git 视图"
            aria-selected={view === 'git'}
            role="tab"
            onClick={() => setView('git')}
          >
            <BranchesOutlined />
          </button>
        </Tooltip>
        <Tooltip title="配置视图" placement="left">
          <button
            type="button"
            className={`proj-bar-btn${view === 'config' ? ' active' : ''}`}
            aria-label="配置视图"
            aria-selected={view === 'config'}
            role="tab"
            onClick={() => setView('config')}
          >
            <SettingOutlined />
          </button>
        </Tooltip>
        <span className="proj-bar-spacer" />
        <Tooltip title="收起侧边栏" placement="left">
          <button type="button" className="proj-bar-btn" aria-label="收起侧边栏" onClick={onClose}>
            <CloseOutlined />
          </button>
        </Tooltip>
      </div>

      <div className="proj-panel-view">
        {view === 'files' && <FilesView project={project} onOpenConfig={() => setView('config')} />}
        {view === 'git' && <GitView project={project} />}
        {view === 'config' && <ConfigView project={project} onEdit={onEditProject} />}
      </div>
    </aside>
  )
}

// ---------------------------------------------------------------------------
// 文件视图（REQ-102/103：目录浏览 + 文件预览 = 对话产物视图）
// ---------------------------------------------------------------------------

function FilesView({ project, onOpenConfig }: { project: Project; onOpenConfig: () => void }) {
  const bound = !!project.local_dir
  const [path, setPath] = useState('')
  const [entries, setEntries] = useState<ProjectDirEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [preview, setPreview] = useState<{ path: string; content: string } | null>(null)
  const [previewErr, setPreviewErr] = useState<string | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)

  const load = (sub: string) => {
    setLoading(true)
    setErr(null)
    api
      .listProjectDirFiles(project.id, sub || undefined)
      .then((r) => {
        setEntries(r.entries ?? [])
        setPath(r.path ?? sub)
      })
      .catch((e: any) => {
        setEntries([])
        setErr(e?.message ?? '目录读取失败')
      })
      .finally(() => setLoading(false))
  }

  // 项目 / 绑定目录变化：重置并回到根目录
  useEffect(() => {
    setPreview(null)
    setPreviewErr(null)
    setPath('')
    if (project.local_dir) load('')
    else setEntries([])
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project.id, project.local_dir])

  const openFile = (e: ProjectDirEntry) => {
    const rel = path ? `${path}/${e.name}` : e.name
    if (e.size > LARGE_FILE) {
      setPreview(null)
      setPreviewErr(`文件「${e.name}」超过 1MB（${fmtSize(e.size)}），请下载后查看`)
      return
    }
    setPreviewLoading(true)
    setPreviewErr(null)
    setPreview(null)
    api
      .getProjectDirFile(project.id, rel)
      .then((t) => setPreview({ path: rel, content: t }))
      .catch((e2: any) => setPreviewErr(e2?.message ?? '文件读取失败'))
      .finally(() => setPreviewLoading(false))
  }

  if (!bound) {
    return (
      <div className="proj-view-body">
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={
            <span>
              未绑定本地目录
              <br />
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                绑定后，对话生成的文档（save_file）将写入此目录并在此可见。
              </Typography.Text>
            </span>
          }
        />
        <div className="proj-view-actions">
          <Button type="primary" size="small" onClick={onOpenConfig}>
            在项目配置中绑定本地目录
          </Button>
        </div>
      </div>
    )
  }

  const segs = path ? path.split('/').filter(Boolean) : []
  const sorted = [...entries].sort((a, b) => (a.is_dir === b.is_dir ? a.name.localeCompare(b.name) : a.is_dir ? -1 : 1))

  return (
    <div className="proj-view-body">
      <div className="proj-view-note">对话生成的文档将写入此目录</div>

      <div className="proj-view-toolbar">
        <span className="proj-crumbs">
          <button type="button" className="proj-crumb" onClick={() => load('')}>
            根
          </button>
          {segs.map((s, i) => (
            <span key={i} className="proj-crumb-seg">
              <span className="proj-crumb-sep">/</span>
              <button type="button" className="proj-crumb" onClick={() => load(segs.slice(0, i + 1).join('/'))}>
                {s}
              </button>
            </span>
          ))}
        </span>
        <Tooltip title="刷新">
          <Button size="small" type="text" icon={<ReloadOutlined />} loading={loading} aria-label="刷新" onClick={() => load(path)} />
        </Tooltip>
      </div>

      {err ? (
        <Alert type="warning" showIcon message="目录读取失败" description={err} />
      ) : previewLoading ? (
        <Spin size="small" />
      ) : previewErr ? (
        <>
          <div className="proj-view-toolbar">
            <Button size="small" type="text" icon={<LeftOutlined />} onClick={() => setPreviewErr(null)}>
              返回
            </Button>
          </div>
          <Alert type="info" showIcon message="无法预览" description={previewErr} />
        </>
      ) : preview ? (
        <>
          <div className="proj-view-toolbar">
            <Button size="small" type="text" icon={<LeftOutlined />} onClick={() => setPreview(null)}>
              返回
            </Button>
            <span className="proj-preview-path" title={preview.path}>
              {preview.path}
            </span>
          </div>
          <pre className="proj-preview">{preview.content || '（空文件）'}</pre>
        </>
      ) : (
        <ul className="proj-entries">
          {sorted.length === 0 && <li className="proj-entry-empty">（空目录）</li>}
          {sorted.map((e) => (
            <li
              key={e.name}
              className="proj-entry"
              onClick={() => (e.is_dir ? load(path ? `${path}/${e.name}` : e.name) : openFile(e))}
            >
              <span className="proj-entry-icon">{e.is_dir ? <FolderOpenOutlined /> : <FileOutlined />}</span>
              <span className="proj-entry-name" title={e.name}>
                {e.name}
              </span>
              {gitStatusTag(e.git_status)}
              {!e.is_dir && <span className="proj-entry-size">{fmtSize(e.size)}</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Git 视图（REQ-102：状态概览，深度 Graph 为后续迭代）
// ---------------------------------------------------------------------------

function GitView({ project }: { project: Project }) {
  const bound = !!project.local_dir
  const [info, setInfo] = useState<DirValidation | null>(null)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const load = () => {
    setLoading(true)
    setErr(null)
    api
      .validateProjectDir(project.local_dir)
      .then(setInfo)
      .catch((e: any) => {
        setInfo(null)
        setErr(e?.message ?? '检测失败')
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    setInfo(null)
    if (project.local_dir) load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project.id, project.local_dir])

  if (!bound) {
    return (
      <div className="proj-view-body">
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="未绑定本地目录；绑定后此处显示 Git 状态概览" />
      </div>
    )
  }
  if (loading) {
    return (
      <div className="proj-view-body">
        <Spin size="small" />
      </div>
    )
  }
  if (err) {
    return (
      <div className="proj-view-body">
        <Alert type="warning" showIcon message="检测失败" description={err} />
      </div>
    )
  }
  if (!info) return <div className="proj-view-body" />
  if (info.error) {
    return (
      <div className="proj-view-body">
        <Alert type="error" showIcon message="目录不可用" description={info.error} />
      </div>
    )
  }
  if (!info.is_git) {
    return (
      <div className="proj-view-body">
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="该目录不是 Git 仓库（无 .git）" />
      </div>
    )
  }

  return (
    <div className="proj-view-body">
      <div className="proj-view-note">Git 状态概览</div>
      <div className="proj-kv">
        <span className="proj-k">分支</span>
        <span className="proj-mono">{info.git_branch || '—'}</span>
      </div>
      <div className="proj-kv">
        <span className="proj-k">最新提交</span>
        <span className="proj-mono">{(info.git_commit || '').slice(0, 10) || '—'}</span>
      </div>
      <div className="proj-kv">
        <span className="proj-k">工作区</span>
        <span>
          {info.git_dirty ? (
            <Tag color="orange" style={{ margin: 0 }}>
              已修改
            </Tag>
          ) : (
            <Tag color="green" style={{ margin: 0 }}>
              干净
            </Tag>
          )}
        </span>
      </div>
      <Alert
        type="info"
        showIcon
        style={{ marginTop: 10 }}
        message="完整 Git Graph 视图规划中（REQ-102 深度版）"
        description="本轮交付状态概览；提交历史 / 分支图 / diff 属后续迭代。"
      />
    </div>
  )
}

// ---------------------------------------------------------------------------
// 配置视图（REQ-103：配置作为侧边栏一页入口；完整迁入后续迭代）
// ---------------------------------------------------------------------------

function ConfigView({ project, onEdit }: { project: Project; onEdit?: () => void }) {
  return (
    <div className="proj-view-body">
      <div className="proj-view-note">项目配置摘要（只读）</div>
      <div className="proj-kv">
        <span className="proj-k">名称</span>
        <span>{project.name}</span>
      </div>
      <div className="proj-kv">
        <span className="proj-k">本地目录</span>
        <span className="proj-mono">{project.local_dir || '未绑定'}</span>
      </div>
      <div className="proj-kv">
        <span className="proj-k">协作模式</span>
        <span>{project.collab_mode || '—'}</span>
      </div>
      <div className="proj-kv">
        <span className="proj-k">工作流模式</span>
        <span>{project.workflow_mode || '—'}</span>
      </div>
      <div className="proj-kv">
        <span className="proj-k">成员智能体</span>
        <span>
          {project.agent_ids?.length ?? 0} 个{project.coordinator ? ` · 主 ${project.coordinator}` : ''}
        </span>
      </div>
      <div className="proj-kv">
        <span className="proj-k">项目级约束</span>
        <span className="proj-pre">{project.constraints || '—'}</span>
      </div>
      <div className="proj-view-actions">
        <Button type="primary" size="small" icon={<SettingOutlined />} disabled={!onEdit} onClick={onEdit}>
          编辑项目
        </Button>
      </div>
    </div>
  )
}
