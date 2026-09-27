import { useEffect, useState } from 'react'
import { Alert, Button, Card, Empty, Input, InputNumber, Segmented, Select, Space, Tag, Typography } from 'antd'
import { SearchOutlined } from '@ant-design/icons'
import LoadErrorAlert from '../../../../components/LoadErrorAlert'
import { api } from '../../../../api/client'
import { useUI } from '../../../../store/ui'
import type { RuntimeProfile } from '../../../../api/types'

// ---------------------------------------------------------------------------
// 消费与审计 · KG 检索页签（D-O19/REQ-163：第五栏 = 本体消费侧观测台）。
// 数据源双轨且**永不混排**（D-O19 边界规则①③）：
//   · TTL（默认）——本体 TTL 装载链路（运行平面 SPARQL），结果带「TTL」徽标；
//   · 文本——KB 文本抽取 KG（GraphRAG 试查，D-O15 自研），结果带「文本」徽标；
// 知识库侧 KG 管理与展示归 GraphRAG 子模块（第五栏只做消费侧试查）。B1 拆分（REQ-145）。
// ---------------------------------------------------------------------------

type Source = 'ttl' | 'text'

/** TTL 链路：跨谓词字面量包含检索（不绑定具体词表——装载 TTL 的谓词形态由导入器决定） */
const TTL_SEARCH = (kw: string, limit: number) =>
  `SELECT DISTINCT ?s ?label WHERE { ?s ?p ?label . FILTER(isLiteral(?label) && CONTAINS(LCASE(STR(?label)), LCASE("${kw.replace(/"/g, '')}"))) } LIMIT ${limit}`

export default function AuditQueryTab({ kbId }: { kbId?: string }) {
  const { showToast } = useUI()
  const [source, setSource] = useState<Source>('ttl') // D-O19：默认本体 TTL 装载来源
  const [profiles, setProfiles] = useState<RuntimeProfile[] | null>(null)
  const [profileId, setProfileId] = useState<string>('')

  // TTL 检索状态
  const [kw, setKw] = useState('')
  const [maxResults, setMaxResults] = useState<number>(20)
  const [querying, setQuerying] = useState(false)
  const [ttlRows, setTtlRows] = useState<{ uri: string; label: string }[] | null>(null)
  const [ttlErr, setTtlErr] = useState<string | null>(null)

  // 文本源状态（既有 GraphRAG 试查）
  const [q, setQ] = useState('')
  const [result, setResult] = useState<{ degraded?: boolean; error?: string; hits: import('../../../../api/types').KBHit[] } | null>(null)

  useEffect(() => {
    api
      .listRuntimeProfiles()
      .then((ps) => {
        const running = ps.filter((p) => p.status === 'running')
        setProfiles(running)
        if (running.length > 0) setProfileId((prev) => prev || running[0].id)
      })
      .catch(() => setProfiles([]))
  }, [])

  const doTtlQuery = async () => {
    if (!profileId) {
      showToast('先选择运行中的本体方案（无运行方案请到「本体运行」栏启动）', 'err')
      return
    }
    if (!kw.trim()) {
      showToast('请输入检索关键词', 'err')
      return
    }
    setQuerying(true)
    setTtlErr(null)
    setTtlRows(null)
    try {
      const { json } = await api.runSparql(profileId, TTL_SEARCH(kw.trim(), maxResults))
      const rows = (json?.results?.bindings ?? []).map((b: Record<string, any>) => ({
        uri: b.s?.value ?? '',
        label: b.label?.value ?? '',
      }))
      setTtlRows(rows)
    } catch (e: any) {
      setTtlErr(e?.message ?? 'SPARQL 查询失败')
    } finally {
      setQuerying(false)
    }
  }

  const doTextQuery = async () => {
    if (!kbId) {
      showToast('先在顶部选择知识库', 'err')
      return
    }
    if (!q.trim()) {
      showToast('请输入查询内容', 'err')
      return
    }
    setQuerying(true)
    setResult(null)
    try {
      const r = await api.graphragSearchKB(kbId, q.trim(), maxResults)
      setResult(r)
    } catch (e: any) {
      showToast(e?.message ?? '查询失败', 'err')
    } finally {
      setQuerying(false)
    }
  }

  return (
    <div className="sema-home">
      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">1</span>
            <span>KG 检索 · 本体消费侧观测台</span>
            <Segmented
              size="small"
              value={source}
              onChange={(v) => setSource(v as Source)}
              options={[
                { label: '本体 TTL 装载', value: 'ttl' },
                { label: 'KB 文本抽取', value: 'text' },
              ]}
            />
          </Space>
        }
      >
        {source === 'ttl' ? (
          <>
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 10 }}
              message="TTL 来源 = 本体 TTL 装载链路（运行平面 SPARQL，D-O19 边界规则①）"
              description="对已装载本体做关键词级实体检索（跨谓词字面量包含匹配）——「本体被消费成什么样」的直观试查；KB 文本抽取 KG 的展示归知识库 GraphRAG 子模块，两源在本页签内可切换、永不混排。"
            />
            <Space size={10} wrap style={{ marginBottom: 10 }}>
              <Select
                style={{ minWidth: 260 }}
                placeholder="选择运行中的本体方案"
                value={profileId || undefined}
                onChange={setProfileId}
                options={(profiles ?? []).map((p) => ({ value: p.id, label: `${p.name}（running）` }))}
                notFoundContent={profiles === null ? '加载中…' : '无运行中的方案（到「本体运行」栏启动）'}
              />
              <Input
                style={{ width: 240 }}
                placeholder="关键词（按标签/注释/名称包含匹配）"
                value={kw}
                onChange={(e) => setKw(e.target.value)}
                onPressEnter={doTtlQuery}
              />
              <Button type="primary" icon={<SearchOutlined />} loading={querying} onClick={doTtlQuery}>
                检索
              </Button>
            </Space>
            {ttlErr && <LoadErrorAlert title="TTL 检索失败" message={ttlErr} onRetry={doTtlQuery} style={{ marginTop: 6 }} />}
            {ttlRows !== null && ttlRows.length === 0 && !ttlErr && (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} style={{ margin: '16px 0' }} description="无命中（换关键词，或确认方案已装载本体）" />
            )}
            {ttlRows !== null && ttlRows.length > 0 && (
              <div className="sema-claims">
                {ttlRows.map((r, i) => (
                  <div className="sema-claim" key={i}>
                    <p className="sema-claim-text">{r.label}</p>
                    <div className="sema-claim-meta">
                      <Tag color="green" style={{ margin: 0 }}>
                        TTL 来源
                      </Tag>
                      <Tag style={{ margin: 0, maxWidth: 420, overflow: 'hidden', textOverflow: 'ellipsis' }}>{r.uri}</Tag>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </>
        ) : (
          <>
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 10 }}
              message="文本来源 = KB 文本抽取 KG（GraphRAG 试查，D-O15 自研）"
              description="向量命中 → KG 一跳扩展（教学口径三步）；该来源的知识图谱管理归知识库 GraphRAG 子模块，本页签仅作消费侧对照试查（D-O19 边界规则②）。"
            />
            <Input.TextArea
              value={q}
              onChange={(e) => setQ(e.target.value)}
              autoSize={{ minRows: 2, maxRows: 5 }}
              placeholder="用自然语言提问，命中 chunk 的 claim 实体会沿 KG 关系一跳扩展拼入上下文"
            />
            <Space size={10} wrap style={{ marginTop: 10 }}>
              <Button type="primary" icon={<SearchOutlined />} loading={querying} onClick={doTextQuery} disabled={!kbId}>
                试查
              </Button>
              <InputNumber
                min={1}
                max={50}
                value={maxResults}
                onChange={(v) => setMaxResults(typeof v === 'number' ? v : 20)}
                addonBefore="max_results"
                style={{ width: 190 }}
              />
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                score=1.0 为 seed 实体命中、0.6 为一跳邻接；KG 无命中自动回退纯向量（degraded 标注）。
              </Typography.Text>
            </Space>
            {result?.degraded && (
              <Alert type="warning" showIcon style={{ marginTop: 10 }} message="已降级为向量检索" description={result.error} />
            )}
            {result && !result.degraded && result.hits.length === 0 && (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} style={{ margin: '16px 0' }} description="无命中（换个问法或先建 KG）" />
            )}
            {result && result.hits.length > 0 && (
              <div className="sema-claims">
                {result.hits.map((h, i) => (
                  <div className="sema-claim" key={i}>
                    <p className="sema-claim-text">{h.excerpt}</p>
                    <div className="sema-claim-meta">
                      <Tag color="blue" style={{ margin: 0 }}>
                        score {h.score.toFixed(3)}
                      </Tag>
                      <Tag color="orange" style={{ margin: 0 }}>
                        文本来源
                      </Tag>
                      <Tag style={{ margin: 0 }}>
                        {h.doc} · #{h.seq}
                      </Tag>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </>
        )}
      </Card>

      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">2</span>
            <span>教学对照 · SPARQL 精确查询 vs 语义检索</span>
          </Space>
        }
      >
        <div className="sema-compare">
          <div className="sema-compare-col">
            <div className="sema-compare-head">
              <span className="sema-compare-title">运行平面 · SPARQL 精确查询</span>
              <Tag color="geekblue" style={{ margin: 0 }}>
                结构精确匹配
              </Tag>
            </div>
            <ul className="sema-compare-list">
              <li>面向已知结构：类 / 属性 / 实例的精确三元组匹配。</li>
              <li>结果可复现、可解释，适合校验与断言（Fuseki 可开推理对照）。</li>
              <li>入口：本体运行栏 → SPARQL 工作台（REQ-92）；本页签 TTL 检索即其轻量封装。</li>
            </ul>
          </div>
          <div className="sema-compare-col">
            <div className="sema-compare-head">
              <span className="sema-compare-title">本栏 · 语义检索（文本抽取 KG）</span>
              <Tag color="purple" style={{ margin: 0 }}>
                语义近似 + 一跳
              </Tag>
            </div>
            <ul className="sema-compare-list">
              <li>面向模糊意图：向量命中后沿 KG 关系扩展，容忍同义 / 近义表述。</li>
              <li>教学口径三步：向量命中 → KG 一跳 → 拼上下文；多跳推理留待扩展。</li>
              <li>入口：本页签「KB 文本抽取」源（D-O15 自研，命中可回溯 chunk）。</li>
            </ul>
          </div>
        </div>
      </Card>
    </div>
  )
}
