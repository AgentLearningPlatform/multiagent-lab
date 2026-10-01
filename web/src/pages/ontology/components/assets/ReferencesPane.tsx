import { useEffect, useState } from 'react'
import { Alert, Button, Empty, List, Spin, Tag, Typography } from 'antd'
import { api } from '../../../../api/client'
import type { OntologyReferences, OntologyPlanRef } from '../../../../api/types'

// ---------------------------------------------------------------------------
// REQ-233②/M60：本体详情「被引用」聚合区块——运行方案挂载 / KB 约束词表（kg_ontology_id）
// / 智能体伴生绑定（companion_ontology_id）三查询拼装一处可见（主后端 GET
// /api/ontologies/{id}/references 聚合端点；删除确认预检与 DELETE 守卫同源）。
// 正交红线：只展示引用事实与状态，启停/解绑/换绑操作不在本页（指向对应管理面）。
// ---------------------------------------------------------------------------

function planStatusTag(p: OntologyPlanRef) {
  const color = p.status === 'running' ? 'green' : p.status === 'error' ? 'volcano' : p.status === 'starting' ? 'gold' : 'default'
  const text = { running: '运行中', starting: '启动中', stopped: '已停止', error: '异常', created: '未启动' }[p.status] ?? p.status
  return <Tag color={color} style={{ margin: 0 }}>{text}</Tag>
}

function Section({ title, count, children }: { title: string; count: number; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: 20 }}>
      <Typography.Text strong style={{ fontSize: 13 }}>
        {title} <Tag style={{ marginInlineStart: 6 }}>{count}</Tag>
      </Typography.Text>
      <div style={{ marginTop: 8 }}>{children}</div>
    </div>
  )
}

export default function ReferencesPane({ ontologyId }: { ontologyId: string }) {
  const [refs, setRefs] = useState<OntologyReferences | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const reload = () => {
    setLoading(true)
    api
      .ontologyReferences(ontologyId)
      .then((r) => {
        setRefs(r)
        setErr(null)
      })
      .catch((e: any) => setErr(e?.message ?? '加载失败'))
      .finally(() => setLoading(false))
  }
  useEffect(() => {
    reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ontologyId])

  if (loading && !refs) return <Spin style={{ display: 'block', margin: '32px auto' }} />
  if (err)
    return (
      <Alert
        type="warning"
        showIcon
        title="引用清单加载失败"
        description={err}
        action={
          <Button size="small" onClick={reload}>
            重试
          </Button>
        }
      />
    )
  if (!refs) return null

  return (
    <div data-testid="onto-references" style={{ maxWidth: 640 }}>
      {(refs.warnings ?? []).map((w, i) => (
        <Alert key={i} type="warning" showIcon title={w} style={{ marginBottom: 12 }} />
      ))}

      <Section title="运行方案挂载" count={refs.runtime_plans.length}>
        {refs.runtime_plans.length === 0 ? (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>未被任何运行方案挂载（方案启停与挂载在「本体运行」栏管理）</Typography.Text>
        ) : (
          <List
            size="small"
            dataSource={refs.runtime_plans}
            renderItem={(p) => (
              <List.Item style={{ padding: '6px 0' }}>
                <span style={{ flex: 1, minWidth: 0 }}>
                  <Typography.Text ellipsis style={{ maxWidth: 360 }}>{p.name || p.id}</Typography.Text>
                  {p.engine && <Tag style={{ marginInlineStart: 8 }}>{p.engine}</Tag>}
                </span>
                {planStatusTag(p)}
              </List.Item>
            )}
          />
        )}
      </Section>

      <Section title="KB 约束词表" count={refs.kb_vocabs.length}>
        {refs.kb_vocabs.length === 0 ? (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>未被知识库用作 KG 约束抽取词表（挂载点在知识库 GraphRAG 配置）</Typography.Text>
        ) : (
          <List
            size="small"
            dataSource={refs.kb_vocabs}
            renderItem={(k) => (
              <List.Item style={{ padding: '6px 0' }}>
                <span style={{ flex: 1, minWidth: 0 }}>
                  <Typography.Text ellipsis style={{ maxWidth: 360 }}>{k.name}</Typography.Text>
                  <Typography.Text type="secondary" style={{ marginInlineStart: 8, fontSize: 11 }}>{k.id}</Typography.Text>
                </span>
                {k.mode && <Tag color="blue" style={{ margin: 0 }}>{k.mode === 'graphrag' ? 'GraphRAG' : 'RAG'}</Tag>}
              </List.Item>
            )}
          />
        )}
      </Section>

      <Section title="智能体伴生绑定" count={refs.companion_agents.length}>
        {refs.companion_agents.length === 0 ? (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>无智能体绑定该本体为伴生归属（绑定入口在智能体侧板「伴生本体」）</Typography.Text>
        ) : (
          <List
            size="small"
            dataSource={refs.companion_agents}
            renderItem={(a) => (
              <List.Item style={{ padding: '6px 0' }}>
                <span style={{ flex: 1, minWidth: 0 }}>
                  <Typography.Text ellipsis style={{ maxWidth: 360 }}>{a.name}</Typography.Text>
                  <Typography.Text type="secondary" style={{ marginInlineStart: 8, fontSize: 11 }}>{a.id}</Typography.Text>
                </span>
                <Tag color="geekblue" style={{ margin: 0 }}>伴生归属</Tag>
              </List.Item>
            )}
          />
        )}
      </Section>

      {refs.runtime_plans.length + refs.kb_vocabs.length + refs.companion_agents.length === 0 && (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="该本体当前无任何消费方引用" />
      )}
    </div>
  )
}
