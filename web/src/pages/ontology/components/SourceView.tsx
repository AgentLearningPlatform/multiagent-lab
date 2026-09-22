import { useEffect, useState } from 'react'
import { Alert, Button, Empty, Select, Spin, Tabs, Tag, Typography } from 'antd'
import { CopyOutlined, DownloadOutlined } from '@ant-design/icons'
import CodeMirror from '@uiw/react-codemirror'
import { EditorView } from '@codemirror/view'
import { api } from '../../../api/client'
import type { Spec, VersionMeta } from '../../../api/types'
import { useUI } from '../../../store/ui'

// ---------------------------------------------------------------------------
// 源码视图（REQ-93）：版本选择 + CodeMirror 只读渲染 + Spec JSON 格式化视图
// ---------------------------------------------------------------------------

/** 源码视图：超过此体积不渲染，改为下载查看（REQ-93） */
const LARGE_SOURCE = 1_000_000

/** 版本原始源文件格式 → 展示名（与构建平面 original_format 口径一致） */
const FORMAT_LABEL: Record<string, string> = {
  turtle: 'Turtle',
  owl_rdfxml: 'OWL / RDF-XML',
  spec_json: 'Spec JSON',
  csv: 'CSV',
  graphml: 'GraphML',
}

export default function SourceView({ ontologyId, currentVersion, spec }: { ontologyId: string; currentVersion?: number; spec: Spec | null }) {
  const { showToast } = useUI()
  const [list, setList] = useState<VersionMeta[] | null>(null)
  const [listErr, setListErr] = useState<string | null>(null)
  const [listLoading, setListLoading] = useState(false)
  const [version, setVersion] = useState<number | null>(null)
  const [original, setOriginal] = useState<string | null>(null)
  const [origErr, setOrigErr] = useState<string | null>(null)
  const [origLoading, setOrigLoading] = useState(false)
  const [tooLarge, setTooLarge] = useState(false)
  const [sub, setSub] = useState<'original' | 'spec'>('original')

  // 版本列表（失败 → 回退仅当前版本，隐藏选择器）
  useEffect(() => {
    let alive = true
    setListLoading(true)
    setListErr(null)
    api
      .listVersions(ontologyId)
      .then((r) => {
        if (alive) setList(r.versions ?? [])
      })
      .catch((e: any) => {
        if (alive) {
          setList(null)
          setListErr(e.message)
        }
      })
      .finally(() => {
        if (alive) setListLoading(false)
      })
    return () => {
      alive = false
    }
  }, [ontologyId])

  // 默认选中最新含原始源文件的版本，否则最后一版
  useEffect(() => {
    if (!list || list.length === 0) {
      setVersion(null)
      return
    }
    const pick = [...list].reverse().find((v) => v.has_original) ?? list[list.length - 1]
    setVersion(pick.version)
  }, [list])

  // 版本列表不可用 → 回退当前版本
  useEffect(() => {
    if (listErr && currentVersion) setVersion(currentVersion)
  }, [listErr, currentVersion])

  const meta = list?.find((v) => v.version === version) ?? null

  // 原始源文件（按体积与 has_original 决定是否拉取）
  useEffect(() => {
    if (version == null) {
      setOriginal(null)
      setTooLarge(false)
      return
    }
    if (meta && !meta.has_original) {
      setOriginal(null)
      setOrigErr(null)
      setTooLarge(false)
      return
    }
    if (meta && (meta.original_size ?? 0) > LARGE_SOURCE) {
      setOriginal(null)
      setTooLarge(true)
      return
    }
    let alive = true
    setOrigLoading(true)
    setOrigErr(null)
    setTooLarge(false)
    api
      .getVersionOriginal(ontologyId, version)
      .then((t) => {
        if (!alive) return
        if (t.length > LARGE_SOURCE) {
          setOriginal(null)
          setTooLarge(true)
        } else {
          setOriginal(t)
        }
      })
      .catch((e: any) => {
        if (alive) {
          setOriginal(null)
          setOrigErr(e.message)
        }
      })
      .finally(() => {
        if (alive) setOrigLoading(false)
      })
    return () => {
      alive = false
    }
  }, [ontologyId, version, meta?.has_original, meta?.original_size])

  const copyOriginal = () => {
    navigator.clipboard
      ?.writeText(original ?? '')
      .then(() => showToast('源码已复制'))
      .catch(() => showToast('复制失败', 'err'))
  }

  const specText = spec ? JSON.stringify(spec, null, 2) : ''
  const dlUrl = api.versionOriginalUrl(ontologyId, version ?? currentVersion ?? 1)

  return (
    <Tabs
      size="small"
      activeKey={sub}
      onChange={(k) => setSub(k as 'original' | 'spec')}
      items={[
        {
          key: 'original',
          label: '原始源文件',
          children: (
            <>
              <div className="onto-sec" style={{ marginTop: 4 }}>
                {listErr ? (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    版本列表不可用（{listErr}），仅显示当前版本 v{currentVersion ?? '—'}
                  </Typography.Text>
                ) : (
                  <>
                    <span className="onto-sec-title">版本</span>
                    <Select
                      size="small"
                      value={version ?? undefined}
                      loading={listLoading}
                      onChange={setVersion}
                      style={{ width: 260 }}
                      placeholder="选择版本"
                      options={(list ?? []).map((v) => ({
                        value: v.version,
                        label: `v${v.version} · ${v.created_at}${v.has_original ? '' : '（无源文件）'}`,
                      }))}
                    />
                  </>
                )}
                <span className="hit-spacer" />
                {meta?.original_format && (
                  <Tag color="blue" style={{ margin: 0 }}>
                    {FORMAT_LABEL[meta.original_format] ?? meta.original_format}
                  </Tag>
                )}
                {original != null && (
                  <Button size="small" icon={<CopyOutlined />} onClick={copyOriginal}>
                    复制
                  </Button>
                )}
              </div>
              {origErr ? (
                <Alert type="warning" showIcon message="原始源文件获取失败" description={origErr} />
              ) : tooLarge ? (
                <Alert
                  type="info"
                  showIcon
                  message="源文件超过 1MB，已切换为下载查看"
                  description={
                    <Button size="small" icon={<DownloadOutlined />} href={dlUrl} download target="_blank" rel="noreferrer">
                      下载查看
                    </Button>
                  }
                />
              ) : origLoading ? (
                <Spin size="small" />
              ) : original != null ? (
                <div className="onto-cm-wrap">
                  <CodeMirror
                    value={original}
                    readOnly
                    editable={false}
                    height="360px"
                    basicSetup={{ lineNumbers: true, foldGutter: true, highlightActiveLine: false }}
                    extensions={[EditorView.lineWrapping]}
                  />
                </div>
              ) : meta && !meta.has_original ? (
                <Typography.Text type="secondary">
                  该版本无原始源文件（由编辑 / 灌装产生，仅存 Spec 快照）
                </Typography.Text>
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="选择版本查看原始源文件" />
              )}
            </>
          ),
        },
        {
          key: 'spec',
          label: 'Spec JSON',
          children: spec ? (
            <div className="onto-cm-wrap">
              <CodeMirror
                value={specText}
                readOnly
                editable={false}
                height="360px"
                basicSetup={{ lineNumbers: true, foldGutter: true, highlightActiveLine: false }}
                extensions={[EditorView.lineWrapping]}
              />
            </div>
          ) : (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚未保存 Spec（保存后可在此查看格式化 JSON）" />
          ),
        },
      ]}
    />
  )
}
