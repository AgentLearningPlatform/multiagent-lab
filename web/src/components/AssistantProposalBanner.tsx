import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Space } from 'antd'
import { api } from '../api/client'
import { useUI } from '../store/ui'

/**
 * 平台助手配置提案横幅（REQ-186 阶段三 / REQ-213 自设置页迁入内置行配置侧板）：
 * 助手对话中 propose_assistant_config 产出的暂存提案（10 分钟 TTL）在此确认应用/忽略。
 * 15s 轮询；仅内置行侧板渲染。
 */
export default function AssistantProposalBanner({ onChanged }: { onChanged?: () => void }) {
  const { showToast } = useUI()
  const [proposal, setProposal] = useState<{ proposal_id: string; changes: { field: string; from: string; to: string }[]; created_at: string } | null>(null)

  const loadProposal = useCallback(() => {
    api
      .assistantProposalGet()
      .then((d) => setProposal(d.pending && d.proposal ? d.proposal : null))
      .catch(() => setProposal(null))
  }, [])

  useEffect(() => {
    loadProposal()
    const iv = setInterval(loadProposal, 15000)
    return () => clearInterval(iv)
  }, [loadProposal])

  const applyProposal = async () => {
    if (!proposal) return
    try {
      await api.assistantProposalApply(proposal.proposal_id)
      showToast('提案已应用：平台助手配置已更新')
      setProposal(null)
      onChanged?.()
    } catch (e: any) {
      showToast(e.message, 'err')
      loadProposal()
    }
  }

  const discardProposal = async () => {
    if (!proposal) return
    try {
      await api.assistantProposalDiscard(proposal.proposal_id)
      setProposal(null)
      showToast('提案已忽略')
    } catch (e: any) {
      showToast(e.message, 'err')
    }
  }

  if (!proposal) return null
  return (
    <Alert
      type="warning"
      showIcon
      style={{ marginBottom: 12 }}
      message="平台助手配置提案待确认（对话中 propose_assistant_config 产出，两段式确认前不落库）"
      description={
        <div style={{ fontSize: 12 }}>
          {proposal.changes.map((c, i) => (
            <div key={i}>
              <b>{c.field}</b>：{c.from || '（空）'} → {c.to || '（空）'}
            </div>
          ))}
          <Space style={{ marginTop: 6 }}>
            <Button size="small" type="primary" onClick={applyProposal}>确认应用</Button>
            <Button size="small" onClick={discardProposal}>忽略</Button>
          </Space>
        </div>
      }
    />
  )
}
