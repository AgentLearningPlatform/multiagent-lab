import { useEffect, useState } from 'react'
import { Alert, Button, Select, Space, Tabs, Tag, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { api } from '../../api/client'
import type { OntoBuildSelectableKB } from '../../api/types'
import AuditGraphTab, { RebuildButton } from './components/audit/AuditGraphTab'
import AuditQueryTab from './components/audit/AuditQueryTab'
import AuditDecisionTab from './components/audit/AuditDecisionTab'
import AuditHomeTab from './components/audit/AuditHomeTab'

// ---------------------------------------------------------------------------
// 消费与审计（第五栏，D-O15/REQ-110：去-semantica 化改造复用）
// 原 Semantica 独立栏骨架保留（页头 + 状态条 + 页签），数据源切换为自研后端：
//   KG 图谱 = GET /api/kg/{kbID}（SQLite 自存三表）
//   GraphRAG 试查 = POST /api/kb/{id}/graphrag-search（向量命中 → KG 一跳扩展）
//   决策审计 = /api/audit/decisions*（SQLite 决策表 + derived_from 溯源链）
//   PROV-O 导出 = GET /api/audit/prov-export（Go 原生 Turtle 模板，零 Python）
// B1（REQ-145/M22）：四个页签拆至 components/audit/（图谱/试查/决策/学习引导）。
// ---------------------------------------------------------------------------

/** 跨栏跳转到本栏（与 BuildPage/RuntimePage 的 onto-sidebar-change 机制一致） */
export function gotoAuditPane() {
  localStorage.setItem('eino.onto.sidebar', 'audit')
  window.dispatchEvent(new CustomEvent('onto-sidebar-change'))
}

export default function AuditPage() {
  const [kbs, setKbs] = useState<OntoBuildSelectableKB[]>([])
  const [kbsErr, setKbsErr] = useState<string | null>(null)
  const [kbId, setKbId] = useState<string | undefined>(undefined)
  const [tab, setTab] = useState('graph')

  const loadKbs = () => {
    api
      .selectableKBsForBuild()
      .then((ls) => {
        setKbs(ls)
        setKbsErr(null)
        setKbId((cur) => cur ?? ls.find((k) => k.kg_ready)?.id ?? ls[0]?.id)
      })
      .catch((e: any) => {
        setKbs([])
        setKbsErr(e?.message ?? '知识库列表加载失败')
      })
  }

  useEffect(() => {
    loadKbs()
  }, [])

  const cur = kbs.find((k) => k.id === kbId)

  return (
    <div className="main">
      <div className="work-main sema-main">
        <div className="work-head">
          <div className="work-head-text">
            <div className="work-head-title">
              <Typography.Title level={4} style={{ margin: 0 }}>
                消费与审计
              </Typography.Title>
              <Tag color="purple" style={{ margin: 0 }}>
                消费 + 审计
              </Tag>
              <Tag style={{ margin: 0 }}>自研 KG · 零外部进程</Tag>
            </div>
            <p className="work-head-desc">
              本体叙事的「构建 → 运行 → <strong>消费</strong> → <strong>审计</strong>」环节载体（D-O15 改造自原
              Semantica 独立栏）：知识库语料抽取为自存 KG 供图谱浏览与 GraphRAG 检索，构建/抽取决策全程留痕可溯源。
            </p>
          </div>
          <Space size={8} wrap>
            <Select
              style={{ width: 300 }}
              value={kbId}
              onChange={setKbId}
              placeholder={kbsErr ? '知识库不可达' : '选择知识库'}
              options={kbs.map((k) => ({
                value: k.id,
                label: `${k.name}（KG ${k.kg_entities}/${k.kg_relationships}${k.kg_ready ? '' : ' · 未建'}）`,
              }))}
              notFoundContent={kbsErr ? '知识库接口未就绪' : '暂无知识库'}
            />
            <Button icon={<ReloadOutlined />} onClick={loadKbs}>
              刷新
            </Button>
          </Space>
        </div>

        {/* 状态条：当前库 KG 规模 + 重建入口 */}
        <div className="sema-status">
          {kbsErr ? (
            <Alert type="warning" showIcon message="知识库列表不可用" description={kbsErr} />
          ) : !cur ? (
            <Alert type="info" showIcon message="先选择一个知识库" description="没有合适的库？先到「知识库」页创建并导入文档。" />
          ) : (
            <Alert
              type={cur.kg_ready ? 'success' : 'warning'}
              showIcon
              message={
                <Space size={8} wrap>
                  <span>{cur.name}</span>
                  <Tag color="blue" style={{ margin: 0 }}>
                    {cur.mode === 'graphrag' ? 'graphrag 模式' : 'rag 模式'}
                  </Tag>
                  <Tag color={cur.kg_ready ? 'green' : 'default'} style={{ margin: 0 }}>
                    {cur.kg_ready ? 'KG 已建' : 'KG 未建'}
                  </Tag>
                </Space>
              }
              description={
                <Space size={16} wrap>
                  <span>文档 <b>{cur.doc_count}</b></span>
                  <span>chunk <b>{cur.chunk_count}</b></span>
                  <span>实体 <b>{cur.kg_entities}</b></span>
                  <span>关系 <b>{cur.kg_relationships}</b></span>
                  <RebuildButton kbId={cur.id} onDone={loadKbs} />
                </Space>
              }
            />
          )}
        </div>

        <Tabs
          activeKey={tab}
          onChange={setTab}
          items={[
            { key: 'graph', label: 'KG 图谱', children: <AuditGraphTab kbId={kbId} /> },
            { key: 'query', label: 'GraphRAG 试查', children: <AuditQueryTab kbId={kbId} /> },
            { key: 'audit', label: '决策审计', children: <AuditDecisionTab kbId={kbId} /> },
            { key: 'home', label: '学习引导', children: <AuditHomeTab /> },
          ]}
        />

        <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 4 }}>
          D-O15/REQ-110：KG 与审计数据全部落主平台 SQLite（010 迁移），semantica worker 已归档休眠（tools/semantica-worker/ 代码保留、不进启动链路）。
        </Typography.Text>
      </div>
    </div>
  )
}
