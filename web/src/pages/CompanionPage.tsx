import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Empty, Select, Spin } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import CompanionPane from './ontology/components/companion/CompanionPane'
import CompanionGraph3D from './ontology/components/companion/CompanionGraph3D'
import { api } from '../api/client'
import type { Agent, Conversation } from '../api/types'
import { companionApi } from '../api/companion'
import type { CompanionGraph } from '../api/companion'
import LoadErrorAlert from '../components/LoadErrorAlert'

// ---------------------------------------------------------------------------
// REQ-180/M-O17：伴生本体独立子模块（第六栏）。REQ-195 布局重构（页级选择器+成长图
// 唯一化+空态单层）。REQ-211/M44：伴生图作用域 agent 化——「伴生对象」从会话升级为
// 智能体（一 agent 一图：其全部会话与参与的项目会话共享沉淀）；成长图/候选/状态均按
// 智能体取数，候选行保留来源会话标注（provenance）。
// ---------------------------------------------------------------------------

export default function CompanionPage() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [agentsLoading, setAgentsLoading] = useState(false)
  const [agentsErr, setAgentsErr] = useState<string | null>(null)
  const [agentId, setAgentId] = useState<string | undefined>(undefined)
  const [convTitles, setConvTitles] = useState<Map<string, string>>(new Map())

  const [graph, setGraph] = useState<CompanionGraph | null>(null)
  const [graphLoading, setGraphLoading] = useState(false)
  const [graphErr, setGraphErr] = useState<string | null>(null)
  const [graphKey, setGraphKey] = useState(0) // confirm/reject/reset 后刷新成长图

  const loadAgents = useCallback(() => {
    setAgentsLoading(true)
    Promise.all([
      api.listAgents(),
      api.listConversations({ scope: 'agent' }).catch(() => []),
      api.listConversations({ scope: 'project' }).catch(() => []),
    ])
      .then(([ags, agentConvs, projectConvs]) => {
        setAgents(ags)
        setAgentsErr(null)
        setAgentId((cur) => cur ?? ags[0]?.id)
        // 来源会话标题映射（候选行 provenance 标注用）
        const m = new Map<string, string>()
        for (const c of [...(agentConvs as Conversation[]), ...(projectConvs as Conversation[])]) {
          m.set(c.id, c.scope === 'project' ? `${c.title || c.id}（项目）` : c.title || c.id)
        }
        setConvTitles(m)
      })
      .catch((e: any) => setAgentsErr(e?.message ?? '智能体列表加载失败'))
      .finally(() => setAgentsLoading(false))
  }, [])

  useEffect(() => {
    loadAgents()
  }, [loadAgents])

  const loadGraph = useCallback((aid: string) => {
    setGraphLoading(true)
    setGraphErr(null)
    companionApi
      .graph(aid)
      .then(setGraph)
      .catch((e: any) => {
        setGraph(null)
        setGraphErr(e?.message ?? '伴生图加载失败')
      })
      .finally(() => setGraphLoading(false))
  }, [])

  useEffect(() => {
    if (agentId) loadGraph(agentId)
    else {
      setGraph(null)
      setGraphErr(null)
    }
  }, [agentId, graphKey, loadGraph])

  const activeAgent = useMemo(() => agents.find((a) => a.id === agentId), [agents, agentId])
  const hasAgents = agents.length > 0

  return (
    <div className="work-main">
      <div className="work-head">
        <div className="work-head-text">
          <div className="work-head-title">伴生本体</div>
          <Alert
            type="info"
            showIcon
            style={{ marginTop: 8 }}
            message="伴生本体（动态薄本体，M28/REQ-170）"
            description="Agent 开启「伴生本体」后，对话收尾自动抽取领域知识候选（旁路管线，不改对话主链路）；确认后写入该智能体的伴生图——其全部会话与参与的项目会话共享沉淀（REQ-211，按智能体隔离），对话检索跨会话可召回；矛盾旧边失效化而非删除。来源徽标「对话」（D-O19 第三来源，不入 KG 检索区）。"
          />
        </div>
      </div>

      {agentsErr && <LoadErrorAlert title="智能体列表加载失败" message={agentsErr} onRetry={loadAgents} style={{ marginBottom: 12 }} />}

      {!agentsErr && !hasAgents && !agentsLoading ? (
        <div className="work-empty" style={{ minHeight: 200 }}>
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={
              <>
                <p>暂无智能体——先创建智能体并开启「伴生本体」开关</p>
                <p style={{ fontSize: 12, color: 'var(--ant-color-text-tertiary, #888)' }}>
                  到「智能体」配置侧边栏开启伴生开关 → 对话一轮 → 候选在此确认入图 → 成长图呈现
                </p>
              </>
            }
          />
        </div>
      ) : (
        <>
          <div className="onto-sec" style={{ marginTop: 12 }}>
            <span className="onto-sec-title">伴生对象（智能体，伴生图按智能体隔离）</span>
            <span className="hit-spacer" />
            <Select
              style={{ minWidth: 300 }}
              showSearch
              optionFilterProp="label"
              value={agentId}
              loading={agentsLoading}
              onChange={setAgentId}
              placeholder={agentsErr ? '智能体列表不可用' : '选择伴生对象（智能体）'}
              notFoundContent={agentsLoading ? <Spin size="small" /> : '暂无智能体'}
              options={agents.map((a) => ({
                value: a.id,
                label: a.companion_ontology ? `${a.name}（伴生已开）` : a.name,
              }))}
            />
            <Button size="small" icon={<ReloadOutlined />} onClick={loadAgents} aria-label="刷新智能体列表">
              刷新
            </Button>
          </div>

          {graphErr ? (
            <LoadErrorAlert title="伴生图加载失败" message={graphErr} onRetry={() => agentId && loadGraph(agentId)} style={{ marginBottom: 12 }} />
          ) : graphLoading ? (
            <div style={{ padding: '24px 0', textAlign: 'center' }}>
              <Spin />
            </div>
          ) : graph && (graph.nodes?.length ?? 0) > 0 ? (
            <div style={{ marginBottom: 12 }}>
              <CompanionGraph3D data={graph} />
            </div>
          ) : (
            agentId && (
              <div className="work-empty" style={{ minHeight: 120, marginBottom: 12 }}>
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description={
                    <span style={{ fontSize: 12 }}>
                      {activeAgent?.companion_ontology
                        ? '该智能体伴生图暂无确认入图内容——下方候选确认流入图后，成长图在此呈现'
                        : '该智能体未开启伴生本体开关——到「智能体」配置侧边栏开启后对话一轮即开始沉淀'}
                  </span>
                }
                />
              </div>
            )
          )}

          <CompanionPane agentId={agentId} convTitles={convTitles} onChangedGraph={() => setGraphKey((k) => k + 1)} />
        </>
      )}
    </div>
  )
}
