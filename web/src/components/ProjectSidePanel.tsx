import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { Alert, Button, Checkbox, Divider, Empty, Form, Input, Popconfirm, Select, Space, Spin, Tag, Tooltip, Typography } from 'antd'
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
import type { Agent, DirValidation, GitBranch, GitCommit, GitFileChange, GitWorkingFile, Project, ProjectDirEntry } from '../api/types'
import { useUI } from '../store/ui'

export type PanelView = 'files' | 'git' | 'config'

/** 侧边栏小节标题（左对齐小标题；窄面板收紧边距） */
function Section({ children, first }: { children: ReactNode; first?: boolean }) {
  return (
    <Divider titlePlacement="left" plain style={{ margin: first ? '0 0 12px' : '4px 0 12px' }}>
      {children}
    </Divider>
  )
}

/** 项目成员选中态（与 ProjectModal 一致：默认 member，coordinator 覆盖） */
function initMembers(project: Project): Record<string, 'coordinator' | 'member'> {
  const init: Record<string, 'coordinator' | 'member'> = {}
  for (const id of project.agent_ids) init[id] = 'member'
  if (project.coordinator) init[project.coordinator] = 'coordinator'
  return init
}

const COLLAB_OPTIONS = [
  { value: 'agent_as_tool', label: 'agent_as_tool（主智能体调度）' },
  { value: 'transfer', label: 'transfer（路由移交）' },
  { value: 'single', label: 'single（单智能体）' },
]
const WORKFLOW_OPTIONS = [
  { value: 'free', label: 'free（自由协作）' },
  { value: 'sequential', label: 'sequential（顺序）' },
  { value: 'parallel', label: 'parallel（并行）' },
  { value: 'loop', label: 'loop（循环）' },
]

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
  agents,
  open,
  view,
  onViewChange,
  onClose,
  onChanged,
}: {
  project: Project
  agents: Agent[]
  open: boolean
  view: PanelView
  onViewChange: (v: PanelView) => void
  onClose: () => void
  onChanged?: () => void
}) {
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
            onClick={() => onViewChange('files')}
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
            onClick={() => onViewChange('git')}
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
            onClick={() => onViewChange('config')}
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
        {view === 'files' && <FilesView project={project} onOpenConfig={() => onViewChange('config')} />}
        {view === 'git' && <GitView project={project} />}
        {view === 'config' && <ConfigView project={project} agents={agents} onChanged={onChanged} />}
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
// Git 视图（REQ-102 深度版：分支切换 + 提交历史时间线 + 每提交变更明细/patch + 工作区变更）
// ---------------------------------------------------------------------------

/** git iso-strict 日期 → 同年 "MM-DD HH:mm"，跨年 "YY-MM-DD HH:mm" */
function fmtGitDate(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const p = (n: number) => String(n).padStart(2, '0')
  const now = new Date()
  const hm = `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
  return d.getFullYear() === now.getFullYear() ? hm : `${String(d.getFullYear()).slice(2)}-${hm}`
}

/** porcelain 状态码 → Tag */
function gitCodeTag(code: string) {
  const map: Record<string, { color: string; text: string }> = {
    M: { color: 'orange', text: '改' },
    A: { color: 'green', text: '增' },
    D: { color: 'red', text: '删' },
    R: { color: 'blue', text: '移' },
    C: { color: 'blue', text: '拷' },
    '??': { color: 'default', text: '未跟踪' },
  }
  const hit = map[code] ?? { color: 'default', text: code || '—' }
  return (
    <Tag color={hit.color} style={{ margin: 0, fontSize: 10, lineHeight: '16px', padding: '0 4px' }}>
      {hit.text}
    </Tag>
  )
}

/** numstat 数字渲染：-1=二进制，undefined=无统计 */
function numStat(add?: number | null, del?: number | null) {
  const one = (v?: number | null, cls?: string) => {
    if (v === undefined || v === null) return <span className="proj-entry-size">—</span>
    if (v < 0) return <span className="proj-entry-size">二进制</span>
    return <span className={cls}>{v}</span>
  }
  return (
    <span className="git-numstat">
      {one(add, 'git-add')}
      {one(del, 'git-del')}
    </span>
  )
}

function GitView({ project }: { project: Project }) {
  const bound = !!project.local_dir
  const [branches, setBranches] = useState<GitBranch[]>([])
  const [curRef, setCurRef] = useState<string | undefined>(undefined) // undefined = 当前 HEAD
  const [commits, setCommits] = useState<GitCommit[]>([])
  const [working, setWorking] = useState<GitWorkingFile[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  // 展开的提交 → 变更文件明细
  const [expanded, setExpanded] = useState<string | null>(null)
  const [files, setFiles] = useState<GitFileChange[] | null>(null)
  const [filesLoading, setFilesLoading] = useState(false)
  // patch 预览
  const [patch, setPatch] = useState<{ commit: string; path?: string; text: string } | null>(null)
  const [patchLoading, setPatchLoading] = useState(false)
  const [patchErr, setPatchErr] = useState<string | null>(null)

  const load = (ref?: string) => {
    setLoading(true)
    setErr(null)
    setExpanded(null)
    setFiles(null)
    setPatch(null)
    setPatchErr(null)
    Promise.all([api.gitBranches(project.id), api.gitLog(project.id, ref), api.gitWorking(project.id)])
      .then(([b, l, w]) => {
        setBranches(b.branches ?? [])
        setCommits(l.commits ?? [])
        setWorking(w.files ?? [])
        setCurRef(ref)
      })
      .catch((e: any) => setErr(e?.message ?? 'Git 信息读取失败'))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    setBranches([])
    setCommits([])
    setWorking([])
    setCurRef(undefined)
    if (project.local_dir) load(undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project.id, project.local_dir])

  const openCommit = (c: GitCommit) => {
    if (expanded === c.hash) {
      setExpanded(null)
      setFiles(null)
      return
    }
    setExpanded(c.hash)
    setFiles(null)
    setFilesLoading(true)
    api
      .gitCommitFiles(project.id, c.hash)
      .then((r) => setFiles(r.files ?? []))
      .catch(() => setFiles([]))
      .finally(() => setFilesLoading(false))
  }

  const openPatch = (c: GitCommit, path?: string) => {
    setPatchLoading(true)
    setPatchErr(null)
    setPatch(null)
    api
      .gitCommitPatch(project.id, c.hash, path)
      .then((t) => setPatch({ commit: c.hash, path, text: t }))
      .catch((e: any) => setPatchErr(e?.message ?? 'patch 读取失败'))
      .finally(() => setPatchLoading(false))
  }

  if (!bound) {
    return (
      <div className="proj-view-body">
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="未绑定本地目录；绑定后此处显示 Git 状态与提交历史" />
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
        <Alert type="warning" showIcon message="Git 信息读取失败" description={err} />
      </div>
    )
  }

  const locals = branches.filter((b) => !b.is_remote)
  const remotes = branches.filter((b) => b.is_remote)
  const current = locals.find((b) => b.current)

  // patch 预览态
  if (patch || patchErr || patchLoading) {
    const backTo = expanded
    return (
      <div className="proj-view-body">
        <div className="proj-view-toolbar">
          <Button
            size="small"
            type="text"
            icon={<LeftOutlined />}
            onClick={() => {
              setPatch(null)
              setPatchErr(null)
              void backTo
            }}
          >
            返回
          </Button>
          <span className="proj-preview-path" title={patch?.path || patch?.commit}>
            {patch?.path || '提交完整 diff'}
          </span>
        </div>
        {patchLoading ? <Spin size="small" /> : patchErr ? <Alert type="info" showIcon message="无法查看 diff" description={patchErr} /> : <pre className="proj-preview">{patch?.text || '（空 diff）'}</pre>}
      </div>
    )
  }

  return (
    <div className="proj-view-body">
      <div className="proj-view-note">分支（点击切换提交历史范围）</div>
      <div className="git-branch-chips">
        {locals.map((b) => (
          <button key={b.name} type="button" className={`git-chip${b.current ? ' active' : ''}`} onClick={() => load(b.current ? undefined : b.name)}>
            {b.name}
            {b.current ? ' ·' : ''}
          </button>
        ))}
      </div>
      {remotes.length > 0 && (
        <div className="git-branch-chips">
          {remotes.map((b) => (
            <button key={b.name} type="button" className="git-chip remote" onClick={() => load(b.name)}>
              {b.name}
            </button>
          ))}
        </div>
      )}

      {working.length > 0 && (
        <>
          <Section>未提交变更（{working.length}）</Section>
          <ul className="git-files">
            {working.map((f) => (
              <li key={f.code + f.path} className="git-file-row">
                {gitCodeTag(f.code)}
                <span className="git-file-path" title={f.path}>
                  {f.path}
                </span>
                {numStat(f.add, f.del)}
              </li>
            ))}
          </ul>
        </>
      )}

      <Section>
        提交历史{curRef ? ` · ${curRef}` : current ? ` · ${current.name}` : ''}（{commits.length}）
      </Section>
      {commits.length === 0 ? (
        <div className="proj-entry-empty">（该分支暂无提交）</div>
      ) : (
        <ul className="git-timeline">
          {commits.map((c) => (
            <li key={c.hash} className="git-commit-row" onClick={() => openCommit(c)}>
              <span className={`git-dot${c.merge ? ' merge' : ''}`} />
              <div className="git-commit-subject" title={c.subject}>
                {c.subject}
              </div>
              <div className="git-commit-meta">
                <code>{c.short}</code> · {c.author} · {fmtGitDate(c.date)}
                {(c.refs ?? []).length > 0 && (
                  <span className="git-refs">
                    {(c.refs ?? []).map((rf) => (
                      <Tag key={rf} color={rf === 'HEAD' ? 'gold' : 'blue'} style={{ marginInlineStart: 4, fontSize: 10, lineHeight: '16px', padding: '0 4px' }}>
                        {rf}
                      </Tag>
                    ))}
                  </span>
                )}
              </div>
              {expanded === c.hash && (
                <div className="git-commit-files" onClick={(e) => e.stopPropagation()}>
                  {filesLoading ? (
                    <Spin size="small" />
                  ) : (files ?? []).length === 0 ? (
                    <div className="proj-entry-empty">（无文件变更）</div>
                  ) : (
                    <ul className="git-files">
                      {(files ?? []).map((f) => (
                        <li key={f.path} className="git-file-row clickable" title="查看 diff" onClick={() => openPatch(c, f.path)}>
                          <FileOutlined className="git-file-icon" />
                          <span className="git-file-path" title={f.path}>
                            {f.path}
                          </span>
                          {numStat(f.add, f.del)}
                        </li>
                      ))}
                    </ul>
                  )}
                  <Button size="small" type="link" onClick={() => openPatch(c)}>
                    查看完整 diff
                  </Button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// 配置视图（REQ-103：原 ProjectModal 编辑表单迁入；仅承载容器变化，字段/校验/提交不变）
// ---------------------------------------------------------------------------

function ConfigView({ project, agents, onChanged }: { project: Project; agents: Agent[]; onChanged?: () => void }) {
  const { showToast, bumpData } = useUI()
  const [form] = Form.useForm()
  const [selected, setSelected] = useState<Record<string, 'coordinator' | 'member'>>(() => initMembers(project))
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)

  // REQ-101：本地目录绑定 + 检测
  const localDir = Form.useWatch('local_dir', form)
  const [dirCheck, setDirCheck] = useState<DirValidation | null>(null)
  const [checking, setChecking] = useState(false)

  useEffect(() => {
    form.setFieldsValue({
      name: project.name,
      description: project.description,
      collab_mode: project.collab_mode ?? 'agent_as_tool',
      workflow_mode: project.workflow_mode ?? 'free',
      constraints: project.constraints,
      local_dir: project.local_dir ?? '',
    })
    setSelected(initMembers(project))
    setDirCheck(null)
  }, [project.id, form])

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
      await api.updateProject(project.id, v)
      await api.setProjectAgents(project.id, members)
      showToast('已保存')
      bumpData()
      onChanged?.()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    setDeleting(true)
    try {
      await api.deleteProject(project.id)
      showToast('已删除')
      bumpData()
      onChanged?.()
    } catch (e: any) {
      showToast(e.message, 'err')
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="proj-view-body">
      <Form form={form} layout="vertical" requiredMark={false} size="small">
        <Section first>基本信息</Section>
        <Form.Item name="name" label="名称" rules={[{ required: true, message: '名称必填' }]}>
          <Input />
        </Form.Item>
        <Form.Item name="description" label="描述">
          <Input.TextArea autoSize={{ minRows: 2, maxRows: 5 }} />
        </Form.Item>

        <Section>本地目录（可选）</Section>
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 8 }}
          message="绑定即授权"
          description="保存后，项目对话中的智能体即获得该目录范围内的文件读写权限（list_files / read_file / save_file + Git 只读展示）；路径越界由系统强制防护（无法访问目录之外）。请仅绑定可信目录；每次工具调用在对话时间线可审计。"
        />
        <Form.Item
          label="本地目录"
          extra="支持 Windows 盘符路径（C:\Users\…）与 POSIX 路径；绑定后，对话生成的文档（save_file）与文件列表将落在该目录。"
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
        <Form.Item name="collab_mode" label="协作模式（M4 生效）">
          <Select options={COLLAB_OPTIONS} />
        </Form.Item>
        <Form.Item name="workflow_mode" label="工作流模式（M4 生效）">
          <Select options={WORKFLOW_OPTIONS} />
        </Form.Item>

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
                  style={{ width: 120 }}
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

      <div className="proj-view-actions">
        <Button type="primary" size="small" loading={saving} onClick={save}>
          保存
        </Button>
        <Popconfirm
          title={`删除项目「${project.name}」？`}
          description="其对话与消息将一并删除。"
          okText="删除"
          okButtonProps={{ danger: true }}
          cancelText="取消"
          onConfirm={remove}
        >
          <Button danger size="small" loading={deleting}>
            删除项目
          </Button>
        </Popconfirm>
      </div>
    </div>
  )
}
