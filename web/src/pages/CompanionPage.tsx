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
// REQ-180/M-O17：伴生本体独立子模块（第六栏，列于资产与运行之间）。
// REQ-195 布局重构（开发者报障四问题：上下两个图界面重复 / 没有切换本体所属对象 /
// 显示界面没有内容 / 引擎加载路径与实际二进制路径不匹配）：
// ①「伴生对象」选择器上提页级且按所属智能体分组（项目会话独立组）——此前页首图由
//   首个会话静默驱动无任何切换 UI，与面板内会话下拉两套状态互不相通；
// ②成长图 3D 唯一化于页首（REQ-154/180「成长图首页化」保留），面板内第二入口退役，
//   数据页级统一获取下发（confirm/reset 后经回调刷新，入图即刻可见）；
// ③空态收敛为单层——选中会话无图时页首局部空态卡一句带过，不再整页空态与面板
//   空态双层堆叠；整页引导空态仅剩「无任何会话」场景。
// ---------------------------------------------------------------------------

export default function CompanionPage() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [convs, setConvs] = useState<Conversation[]>([])
  const [convsLoading, setConvsLoading] = useState(false)
  const [convsErr, setConvsErr] = useState<string | null>(null)
  const [convId, setConvId] = useState<string | undefined>(undefined)

  const [graph, setGraph] = useState<CompanionGraph | null>(null)
  const [graphLoading, setGraphLoading] = useState(false)
  const [graphErr, setGraphErr] = useState<string | null>(null)
  const [graphKey, setGraphKey] = useState(0) // confirm/reject/reset 后刷新成长图

  const loadConvs = useCallback(() => {
    setConvsLoading(true)
    Promise.all([
      api.listAgents().catch(() => [] as Agent[]),
      api.listConversations({ scope: 'agent' }),
      api.listConversations({ scope: 'project' }),
    ])
      .then(([ags, agentConvs, projectConvs]) => {
        setAgents(ags)
        const merged = [
          ...agentConvs,
          ...projectConvs.map((p) => ({ ...p, title: `${p.title || p.id}（项目）` })),
        ]
        setConvs(merged)
        setConvsErr(null)
        setConvId((cur) => cur ?? merged[0]?.id)
      })
      .catch((e: any) => setConvsErr(e?.message ?? '会话列表加载失败'))
      .finally(() => setConvsLoading(false))
  }, [])

  useEffect(() => {
    loadConvs()
  }, [loadConvs])

  const loadGraph = useCallback((cid: string) => {
    setGraphLoading(true)
    setGraphErr(null)
    companionApi
      .graph(cid)
      .then(setGraph)
      .catch((e: any) => {
        setGraph(null)
        setGraphErr(e?.message ?? '伴生图加载失败')
      })
      .finally(() => setGraphLoading(false))
  }, [])

  useEffect(() => {
    if (convId) loadGraph(convId)
    else {
      setGraph(null)
      setGraphErr(null)
    }
  }, [convId, graphKey, loadGraph])

  // 所属对象分组选项：agent 会话按所属智能体 OptGroup，项目会话独立组（REQ-187 项目伴生口径）
  const convOptions = useMemo(() => {
    const agentName = new Map(agents.map((a) => [a.id, a.name]))
    const groups = new Map<string, { label: string; value: string }[]>()
    for (const c of convs) {
      const g = c.scope === 'project' ? '项目会话' : agentName.get(c.agent_id ?? '') ?? '智能体会话'
      if (!groups.has(g)) groups.set(g, [])
      groups.get(g)!.push({ label: c.title || c.id, value: c.id })
    }
    return [...groups.entries()].map(([g, options]) => ({ label: g, options }))
  }, [agents, convs])

  const hasConvs = convs.length > 0

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
            description="Agent 开启「伴生本体」后，对话收尾自动抽取领域知识候选（旁路管线，不改对话主链路）；在此人工确认后写入该会话的伴生图（独立 Oxigraph 引擎，按会话 named graph 隔离，矛盾旧边失效化而非删除）。来源徽标「对话」（D-O19 第三来源，不入 KG 检索区）。"
          />
        </div>
      </div>

      {convsErr && <LoadErrorAlert title="会话列表加载失败" message={convsErr} onRetry={loadConvs} style={{ marginBottom: 12 }} />}

      {!convsErr && !hasConvs && !convsLoading ? (
        <div className="work-empty" style={{ minHeight: 200 }}>
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={
              <>
                <p>暂无 Agent 会话——先创建智能体并开启「伴生本体」开关</p>
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
            <span className="onto-sec-title">伴生对象（会话，伴生图按会话隔离）</span>
            <span className="hit-spacer" />
            <Select
              style={{ minWidth: 340 }}
              showSearch
              optionFilterProp="label"
              value={convId}
              loading={convsLoading}
              onChange={setConvId}
              placeholder={convsErr ? '会话列表不可用' : '选择伴生对象（会话）'}
              notFoundContent={convsLoading ? <Spin size="small" /> : '暂无 Agent 会话'}
              options={convOptions}
            />
            <Button size="small" icon={<ReloadOutlined />} onClick={loadConvs} aria-label="刷新会话列表">
              刷新
            </Button>
          </div>

          {graphErr ? (
            <LoadErrorAlert title="伴生图加载失败" message={graphErr} onRetry={() => convId && loadGraph(convId)} style={{ marginBottom: 12 }} />
          ) : graphLoading ? (
            <div style={{ padding: '24px 0', textAlign: 'center' }}>
              <Spin />
            </div>
          ) : graph && (graph.nodes?.length ?? 0) > 0 ? (
            <div style={{ marginBottom: 12 }}>
              <CompanionGraph3D data={graph} />
            </div>
          ) : (
            convId && (
              <div className="work-empty" style={{ minHeight: 120, marginBottom: 12 }}>
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description={
                    <span style={{ fontSize: 12 }}>
                      该会话伴生图暂无确认入图内容——下方候选确认流入图后，成长图在此呈现
                    </span>
                  }
                />
              </div>
            )
          )}

          <CompanionPane convId={convId} onChangedGraph={() => setGraphKey((k) => k + 1)} />
        </>
      )}
    </div>
  )
}
