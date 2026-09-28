import { useEffect, useState } from 'react'
import { Empty } from 'antd'
import CompanionPane from './ontology/components/companion/CompanionPane'
import CompanionGraph3D from './ontology/components/companion/CompanionGraph3D'
import { api } from '../api/client'
import type { Conversation } from '../api/types'

// ---------------------------------------------------------------------------
// REQ-180/M-O17：伴生本体独立子模块（第六栏，列于资产与运行之间）。
// 从资产栏页签升格——CompanionPane 全功能等价迁移 + CompanionGraph3D 成长图首页化
// + 空态引导卡（指向智能体配置伴生开关）。资产栏撤伴生页签回归仓库资产语义。
// ---------------------------------------------------------------------------

export default function CompanionPage() {
  const [convs, setConvs] = useState<Conversation[]>([])
  const [convId, setConvId] = useState<string | undefined>(undefined)
  const [hasData, setHasData] = useState(false)

  useEffect(() => {
    Promise.all([
      api.listConversations({ scope: 'agent' }),
      api.listConversations({ scope: 'project' }),
    ])
      .then(([agents, projects]) => {
        const merged = [
          ...agents,
          ...projects.map((p) => ({ ...p, title: `${p.title || p.id}（项目）` })),
        ]
        setConvs(merged)
        setConvId((cur) => cur ?? merged[0]?.id)
      })
      .catch(() => {})
  }, [])

  useEffect(() => {
    if (!convId) return
    fetch(`/api/companion/graph?conversation_id=${encodeURIComponent(convId)}`)
      .then((r) => r.json())
      .then((g) => setHasData((g.nodes?.length ?? 0) > 0))
      .catch(() => setHasData(false))
  }, [convId])

  const showEmptyGuide = convs.length === 0 || !hasData

  return (
    <div className="work-main">
      <div className="work-head">
        <div className="work-head-text">
          <div className="work-head-title">伴生本体</div>
          <p className="work-head-desc">
            Agent 开启「伴生本体」后，对话收尾自动抽取领域知识候选，人工确认入图（D-O19 第三来源）。本栏 = 会话伴生图管理与成长可视化。
          </p>
        </div>
      </div>

      {showEmptyGuide && (
        <div className="work-empty" style={{ minHeight: 180, marginBottom: 12 }}>
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={
              <>
                <p>{convs.length === 0 ? '暂无 Agent 会话——先创建智能体并开启「伴生本体」开关' : '选中会话的伴生图尚无确认入图内容'}</p>
                <p style={{ fontSize: 12, color: 'var(--ant-color-text-tertiary, #888)' }}>
                  到「智能体」配置侧边栏开启伴生开关 → 对话一轮 → 候选在此确认入图 → 成长图呈现
                </p>
              </>
            }
          />
        </div>
      )}

      {convId && hasData && (
        <div style={{ marginBottom: 12 }}>
          <CompanionGraph3D convId={convId} />
        </div>
      )}

      <CompanionPane />
    </div>
  )
}
