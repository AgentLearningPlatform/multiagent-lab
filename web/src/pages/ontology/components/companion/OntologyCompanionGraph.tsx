import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Empty, Spin, Tooltip, Typography } from 'antd'
import { companionApi } from '../../../../api/companion'
import type { CompanionGraph } from '../../../../api/companion'
import type { Agent } from '../../../../api/types'
import CompanionGraph3D from './CompanionGraph3D'
import LoadErrorAlert from '../../../../components/LoadErrorAlert'

// ---------------------------------------------------------------------------
// REQ-216⑧：本体伴生成长图（对话生长）——伴生图作为本体资产可视化形态之一，与 Spec
// 2D/3D/WebVOWL 并列（VizTabs 第 4 形态）。数据经首个绑定 agent 解析（多 agent 绑同一
// 本体 = 同一伴生子图，任一绑定者视角等价）；宿主方案不可达时空态诚实透出 plan_error。
// ---------------------------------------------------------------------------

export default function OntologyCompanionGraph({ ontologyId }: { ontologyId: string }) {
  const [agents, setAgents] = useState<Agent[]>([])
  const [loading, setLoading] = useState(false)
  const [graph, setGraph] = useState<CompanionGraph | null>(null)
  const [graphErr, setGraphErr] = useState<string | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    companionApi
      .listOntologyAgents(ontologyId)
      .then(async (ls) => {
        const bound = ls ?? []
        setAgents(bound)
        if (bound.length === 0) {
          setGraph(null)
          return
        }
        // 多 agent 绑同一本体 = 同一伴生子图：任取首个绑定者解析图数据
        const g = await companionApi.graph(bound[0].id)
        setGraph(g)
        setGraphErr(null)
      })
      .catch((e: any) => {
        setGraph(null)
        setGraphErr(e?.message ?? '伴生成长图加载失败')
      })
      .finally(() => setLoading(false))
  }, [ontologyId])

  useEffect(() => {
    load()
  }, [load])

  const boundNames = useMemo(() => agents.map((a) => a.name).join('、'), [agents])

  if (loading) {
    return (
      <div style={{ padding: '32px 0', textAlign: 'center' }}>
        <Spin />
      </div>
    )
  }
  if (graphErr) {
    return <LoadErrorAlert title="伴生成长图加载失败" message={graphErr} onRetry={load} />
  }
  if (agents.length === 0) {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description={
          <span style={{ fontSize: 12 }}>
            暂无智能体绑定该本体——在「智能体」侧板伴生配置中绑定后，对话确认入图的知识在此呈现成长图
          </span>
        }
      />
    )
  }
  if (!graph || (graph.nodes?.length ?? 0) === 0) {
    return (
      <div>
        {graph?.plan_error && (
          <Alert type="warning" showIcon style={{ marginBottom: 8 }} message="宿主方案不可达" description={graph.plan_error} />
        )}
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={<span style={{ fontSize: 12 }}>伴生子图暂无确认入图内容——候选确认后成长图在此呈现</span>}
        />
      </div>
    )
  }
  return (
    <div>
      <div style={{ marginBottom: 8 }}>
        <Tooltip title={`伴生子图 ${graph.graph}（绑定者：${boundNames}）`}>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            绑定者：{boundNames} · 节点 {graph.nodes?.length ?? 0} · 边 {graph.edges?.length ?? 0}
            {!graph.engine_running && ' · 宿主方案未运行（空图快照）'}
          </Typography.Text>
        </Tooltip>
      </div>
      <CompanionGraph3D data={graph} />
    </div>
  )
}
