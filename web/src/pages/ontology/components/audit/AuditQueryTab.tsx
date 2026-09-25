import { useState } from 'react'
import { Alert, Button, Card, Empty, Input, InputNumber, Space, Tag, Typography } from 'antd'
import { SearchOutlined } from '@ant-design/icons'
import { api } from '../../../../api/client'
import { useUI } from '../../../../store/ui'

// ---------------------------------------------------------------------------
// 消费与审计 · GraphRAG 试查页签（向量命中 → KG 一跳扩展，教学口径）。
// B1 拆分（REQ-145）。
// ---------------------------------------------------------------------------

export default function AuditQueryTab({ kbId }: { kbId?: string }) {
  const { showToast } = useUI()
  const [q, setQ] = useState('')
  const [maxResults, setMaxResults] = useState<number>(5)
  const [querying, setQuerying] = useState(false)
  const [result, setResult] = useState<{ degraded?: boolean; error?: string; hits: import('../../../../api/types').KBHit[] } | null>(null)

  const doQuery = async () => {
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
            <span>GraphRAG 检索（向量 + KG 一跳扩展）</span>
          </Space>
        }
      >
        <Input.TextArea
          value={q}
          onChange={(e) => setQ(e.target.value)}
          autoSize={{ minRows: 2, maxRows: 5 }}
          placeholder="用自然语言提问，命中 chunk 的 claim 实体会沿 KG 关系一跳扩展拼入上下文"
        />
        <Space size={10} wrap style={{ marginTop: 10 }}>
          <Button type="primary" icon={<SearchOutlined />} loading={querying} onClick={doQuery} disabled={!kbId}>
            试查
          </Button>
          <InputNumber
            min={1}
            max={50}
            value={maxResults}
            onChange={(v) => setMaxResults(typeof v === 'number' ? v : 5)}
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
                  <Tag style={{ margin: 0 }}>
                    {h.doc} · #{h.seq}
                  </Tag>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">2</span>
            <span>教学对照 · SPARQL 精确查询 vs GraphRAG 语义检索</span>
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
              <li>入口：本体运行栏 → SPARQL 工作台（REQ-92）。</li>
            </ul>
          </div>
          <div className="sema-compare-col">
            <div className="sema-compare-head">
              <span className="sema-compare-title">本栏 · GraphRAG 语义检索</span>
              <Tag color="purple" style={{ margin: 0 }}>
                语义近似 + 一跳
              </Tag>
            </div>
            <ul className="sema-compare-list">
              <li>面向模糊意图：向量命中后沿 KG 关系扩展，容忍同义 / 近义表述。</li>
              <li>教学口径三步：向量命中 → KG 一跳 → 拼上下文；多跳推理留待扩展。</li>
              <li>入口：本页试查（D-O15 自研，命中可回溯 chunk）。</li>
            </ul>
          </div>
        </div>
      </Card>
    </div>
  )
}
