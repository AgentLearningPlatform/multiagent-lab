import { useCallback, useEffect, useState } from 'react'
import { Badge, Button, Card, Empty, Popconfirm, Space, Spin, Tag, Tooltip, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'

// ---------------------------------------------------------------------------
// REQ-207/M43：本体自进化「进化」页签——候选 vN-cK 列表（proposed/accepted/rejected
// 三桶）+ 配对门控（gate 后展示双信号对照）+ 采纳/拒绝。一期人工触发（无自动循环）；
// 提交候选由「诊断→归因→补丁」LLM 侧产出（后续轮接线 ai-draft 同类端点），本页签先
// 承载门控-采纳-回溯的治理闭环（验收：候选未经门控不得转正）。
// ---------------------------------------------------------------------------

interface EvoCandidate {
  id: string
  label: string
  base_version: number
  layer: 'content' | 'tool' | 'schema'
  summary: string
  evidence: string
  gate_report: string
  status: 'proposed' | 'accepted' | 'rejected'
  round: number
  created_at: string
}

const req = async (url: string, init?: RequestInit) => {
  const res = await fetch(url, { headers: { 'Content-Type': 'application/json' }, ...init })
  const text = await res.text()
  let data: any = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    throw new Error(`HTTP ${res.status}`)
  }
  if (!res.ok) throw new Error((data && data.error) || `HTTP ${res.status}`)
  return data
}

export default function EvolutionPane({ ontologyId }: { ontologyId: string }) {
  const [cands, setCands] = useState<EvoCandidate[]>([])
  const [loading, setLoading] = useState(false)
  const [bucket, setBucket] = useState<'proposed' | 'accepted' | 'rejected'>('proposed')
  const [busy, setBusy] = useState<string | null>(null)
  const [actionErr, setActionErr] = useState<string | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    fetch(`/api/ontologies/${ontologyId}/evolution/candidates`)
      .then((r) => r.json())
      .then((ls) => setCands(ls ?? []))
      .catch(() => setCands([]))
      .finally(() => setLoading(false))
  }, [ontologyId])

  useEffect(() => {
    load()
  }, [load])

  const gate = async (id: string) => {
    setBusy(id)
    setActionErr(null)
    try {
      await req(`/api/ontologies/${ontologyId}/evolution/candidates/${id}/gate`, { method: 'POST', body: '{}' })
      load()
    } catch (e: any) {
      setActionErr(e.message)
    } finally {
      setBusy(null)
    }
  }

  const accept = async (id: string) => {
    setBusy(id)
    setActionErr(null)
    try {
      const r = await req(`/api/ontologies/${ontologyId}/evolution/candidates/${id}/accept`, { method: 'POST', body: '{}' })
      window.dispatchEvent(new CustomEvent('onto-spec-saved', { detail: { ontologyId } }))
      alert(`已采纳并升正式版本 v${r.new_version}`)
      load()
    } catch (e: any) {
      setActionErr(e.message)
    } finally {
      setBusy(null)
    }
  }

  const reject = async (id: string) => {
    setBusy(id)
    setActionErr(null)
    try {
      await req(`/api/ontologies/${ontologyId}/evolution/candidates/${id}/reject`, { method: 'POST', body: '{}' })
      load()
    } catch (e: any) {
      setActionErr(e.message)
    } finally {
      setBusy(null)
    }
  }

  const rows = cands.filter((c) => c.status === bucket)
  const gateInfo = (c: EvoCandidate) => {
    if (!c.gate_report) return null
    try {
      const r = JSON.parse(c.gate_report)
      return {
        passed: r.passed as boolean,
        base: r.base?.overall,
        patched: r.patched?.overall,
      }
    } catch {
      return null
    }
  }

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
        <Space size={4}>
          {(['proposed', 'accepted', 'rejected'] as const).map((b) => (
            <Button key={b} size="small" type={bucket === b ? 'primary' : 'default'} onClick={() => setBucket(b)}>
              {b === 'proposed' ? '待裁决' : b === 'accepted' ? '已采纳' : '已拒绝'}
              {cands.filter((c) => c.status === b).length > 0 && (
                <Badge count={cands.filter((c) => c.status === b).length} size="small" style={{ marginLeft: 4 }} />
              )}
            </Button>
          ))}
        </Space>
        <span style={{ flex: 1 }} />
        <Button size="small" icon={<ReloadOutlined />} onClick={load} aria-label="刷新进化候选" />
      </div>

      {actionErr && (
        <Typography.Text type="danger" style={{ fontSize: 12, display: 'block', marginBottom: 8 }}>{actionErr}</Typography.Text>
      )}

      {loading ? (
        <Spin size="small" />
      ) : rows.length === 0 ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={<span style={{ fontSize: 12 }}>暂无{bucket === 'proposed' ? '待裁决' : bucket === 'accepted' ? '已采纳' : '已拒绝'}候选——进化建议由「诊断→归因→补丁」产出后在此经配对门控人工采纳（REQ-207）</span>}
        />
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {rows.map((c) => {
            const gi = gateInfo(c)
            return (
              <Card key={c.id} size="small" style={{ border: '1px solid var(--ant-color-border, #eee)' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
                  <Tag color="geekblue" style={{ margin: 0 }}>{c.label}</Tag>
                  <Tag style={{ margin: 0 }}>{c.layer}</Tag>
                  <Typography.Text strong style={{ fontSize: 12, flex: 1, minWidth: 200 }}>{c.summary}</Typography.Text>
                  {c.status === 'proposed' && (
                    <Space size={4}>
                      <Button size="small" loading={busy === c.id} onClick={() => gate(c.id)}>
                        {gi ? '重跑门控' : '跑配对门控'}
                      </Button>
                      <Popconfirm title="采纳该候选？补丁将应用并升正式版本（不可撤销，可再进化修正）。" okText="采纳" onConfirm={() => accept(c.id)}>
                        <Button size="small" type="primary" disabled={!gi?.passed} loading={busy === c.id}>
                          采纳
                        </Button>
                      </Popconfirm>
                      <Popconfirm title="拒绝该候选？将留档冻结（可追溯）。" okText="拒绝" okButtonProps={{ danger: true }} onConfirm={() => reject(c.id)}>
                        <Button size="small" danger loading={busy === c.id}>拒绝</Button>
                      </Popconfirm>
                    </Space>
                  )}
                  {c.status !== 'proposed' && (
                    <Tag color={c.status === 'accepted' ? 'green' : 'default'} style={{ margin: 0 }}>
                      {c.status === 'accepted' ? '已采纳' : '已拒绝'}
                    </Tag>
                  )}
                </div>
                <div style={{ fontSize: 11, color: 'var(--ant-color-text-tertiary, #999)', marginTop: 4 }}>
                  基于 v{c.base_version} · 第 {c.round} 轮 · {c.created_at?.slice(5, 16).replace('T', ' ')}
                  {c.evidence && (
                    <Tooltip title={c.evidence}>
                      <Tag color="cyan" style={{ margin: '0 0 0 8px', fontSize: 10 }}>Evidence 锚定</Tag>
                    </Tooltip>
                  )}
                </div>
                {gi && (
                  <div style={{ fontSize: 11, marginTop: 4 }}>
                    <Tag color={gi.passed ? 'green' : 'red'} style={{ margin: 0 }}>
                      门控{gi.passed ? '通过' : '未通过'}
                    </Tag>
                    <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                      质量分 {gi.base?.toFixed(2)} → {gi.patched?.toFixed(2)}
                    </Typography.Text>
                  </div>
                )}
              </Card>
            )
          })}
        </div>
      )}
    </div>
  )
}
