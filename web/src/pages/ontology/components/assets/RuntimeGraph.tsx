import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Select, Space, Spin, Tag, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { api } from '../../../../api/client'
import type { RuntimeProfile, Spec } from '../../../../api/types'
import Graph3D from '../Graph3D'

// ---------------------------------------------------------------------------
// REQ-234②/M61：资产可视化「运行态」源——running 方案 SPARQL 拉取引擎实装 TBox 实渲
// （REQ-175 渐进通道同语义：浏览对象是运行中的本体）。薄实现：经 facade 同口径工作台
// 端点拉 owl:Class+rdfs:subClassOf+rdf:type 计数 → 组装派生 spec → 复用 Graph3D 全套
// 三维渲染/搜索/图例（实例不拉全量，以 label 计数后缀呈现；全量实例经 3D 渐进通道）。
// ---------------------------------------------------------------------------

/** 单条 SPARQL 查询（工作台反代端点，JSON 绑定结果） */
async function runQ(profileId: string, query: string): Promise<Record<string, string>[]> {
  const res = await fetch(`/api/runtime-profiles/${profileId}/sparql`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/sparql-query', Accept: 'application/sparql-results+json' },
    body: query,
  })
  if (!res.ok) throw new Error((await res.json().catch(() => ({})))?.error ?? `查询失败 ${res.status}`)
  const data = await res.json()
  const bindings = data?.results?.bindings ?? []
  const rows: Record<string, string>[] = []
  for (const b of bindings) {
    const row: Record<string, string> = {}
    for (const k of Object.keys(b)) row[k] = String(b[k]?.value ?? '')
    rows.push(row)
  }
  return rows
}

/** IRI 尾段名（urn:o:{oid}:{name} / http://…/{name}） */
const localName = (iri: string) => decodeURIComponent(iri).split(/[#:]/).pop() ?? iri

export default function RuntimeGraph({ ontologyId }: { ontologyId: string }) {
  const [profiles, setProfiles] = useState<RuntimeProfile[]>([])
  const [profileId, setProfileId] = useState<string | null>(null)
  const [spec, setSpec] = useState<Spec | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    api
      .listRuntimeProfiles()
      .then((ps) => {
        const running = ps.filter((p) => p.status === 'running' && p.ontology_ids?.includes(ontologyId))
        setProfiles(running)
        setProfileId((cur) => (cur && running.some((p) => p.id === cur) ? cur : running[0]?.id ?? null))
      })
      .catch(() => setProfiles([]))
  }, [ontologyId])

  const load = () => {
    if (!profileId) return
    setLoading(true)
    setErr(null)
    ;(async () => {
      // 概念+继承（owl:Class；TTL 导出带 rdfs:label）+ 每概念实例计数（rdf:type 命中 NamedIndividual）
      const classQ = `PREFIX owl: <http://www.w3.org/2002/07/owl#>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT ?c ?label ?parent WHERE { ?c a owl:Class . OPTIONAL { ?c rdfs:label ?label } OPTIONAL { ?c rdfs:subClassOf ?parent } }`
      const countQ = `PREFIX owl: <http://www.w3.org/2002/07/owl#>
PREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#>
SELECT ?c (COUNT(?i) AS ?n) WHERE { ?c a owl:Class . ?i a ?c } GROUP BY ?c`
      const [cls, cnts] = await Promise.all([runQ(profileId, classQ), runQ(profileId, countQ)])
      const counts = new Map(cnts.map((r) => [r.c, Number(r.n) || 0]))
      const byIri = new Map<string, { name: string; label?: string; parents: string[] }>()
      for (const r of cls) {
        const name = localName(r.c)
        const cur = byIri.get(r.c) ?? { name, label: undefined, parents: [] }
        if (r.label) cur.label = r.label
        if (r.parent && r.parent !== r.c) cur.parents.push(localName(r.parent))
        byIri.set(r.c, cur)
      }
      const derived: Spec = {
        name: '运行态实渲',
        description: '',
        concepts: [...byIri.values()].map((c) => {
          const cnt = [...counts.entries()].find(([iri]) => localName(iri) === c.name)?.[1] ?? 0
          return {
            name: c.name,
            label: cnt > 0 ? `${c.label || c.name} (${cnt})` : c.label || c.name,
            definition: '',
            parents: [...new Set(c.parents)],
          }
        }),
        relations: [],
        instances: [],
      }
      setSpec(derived)
    })()
      .catch((e: any) => setErr(e?.message ?? '加载失败'))
      .finally(() => setLoading(false))
  }
  useEffect(() => {
    setSpec(null)
    if (profileId) load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [profileId])

  const total = useMemo(() => spec?.concepts.length ?? 0, [spec])

  if (profiles.length === 0) {
    return (
      <Alert
        type="info"
        showIcon
        title="运行态实渲需要包含该本体的 running 方案"
        description="到「本体运行」栏启动方案后回到此处查看引擎实装的类层次（REQ-81：浏览对象是运行中的本体）。"
      />
    )
  }
  return (
    <div data-testid="runtime-graph">
      <Space style={{ marginBottom: 8 }} wrap>
        <Select
          size="small"
          style={{ minWidth: 220 }}
          value={profileId}
          onChange={setProfileId}
          options={profiles.map((p) => ({ value: p.id, label: `${p.name}（:${p.port}）` }))}
        />
        <Button size="small" icon={<ReloadOutlined />} onClick={load}>
          重新拉取
        </Button>
        <Tag color="geekblue" style={{ margin: 0 }}>
          引擎实装 TBox {total} 类
        </Tag>
        <Typography.Text type="secondary" style={{ fontSize: 11 }}>
          括号内为该类实例计数；全量实例经三维渐进通道按需扩展
        </Typography.Text>
      </Space>
      {loading && !spec && <Spin style={{ display: 'block', margin: '32px auto' }} />}
      {err && <Alert type="warning" showIcon title="运行态拉取失败" description={err} />}
      {spec && <Graph3D key={profileId} spec={spec} />}
    </div>
  )
}
