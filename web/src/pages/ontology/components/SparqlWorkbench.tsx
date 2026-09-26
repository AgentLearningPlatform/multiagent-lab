import { useMemo, useRef, useState } from 'react'
import CodeMirror from '@uiw/react-codemirror'
import { EditorView, keymap } from '@codemirror/view'
import { Prec } from '@codemirror/state'
import { autocompletion, type CompletionContext } from '@codemirror/autocomplete'
import { sparql } from 'codemirror-lang-sparql'
import { Alert, Button, Select, Space, Table, Tag, Typography } from 'antd'
import { ClearOutlined, CaretRightOutlined, DownloadOutlined, UndoOutlined } from '@ant-design/icons'
import { api } from '../../../api/client'

// ---------------------------------------------------------------------------
// SPARQL 工作台 v2（REQ-92 实现升级，2026-09-26 主人授权）：
// 原 Yasgui 内嵌因自带样式未随包生效而杂乱（裸表单形态），且其 UI 体系与平台
// AntD 口径割裂。改用开源组件自组装：CodeMirror 6（@uiw/react-codemirror + 
// codemirror-lang-sparql 语法高亮）+ AntD 表格/告警 + api.runSparql（既有零调用
// 方法首次启用）。查询执行仍走原端点 /api/runtime-profiles/{id}/sparql，后端零改动。
// ---------------------------------------------------------------------------

/** 默认模板：通用类型总览（对任何本体可跑） */
const DEFAULT_SPARQL = `PREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT ?s ?o WHERE { ?s rdf:type ?o } LIMIT 20`

/** 查询模板（REQ-153 种子本体配套教学查询；本体 ID 按所挂方案替换） */
const TEMPLATES: { key: string; label: string; sql: string }[] = [
  { key: 'default', label: '类型总览（通用）', sql: DEFAULT_SPARQL },
  {
    key: 'tabular',
    label: '层级聚合：顶层概念的实例计数',
    sql: `PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX o: <urn:o:onto_org_person:concept:>
SELECT ?概念 (COUNT(?x) AS ?实例数) WHERE {
  ?c rdfs:subClassOf* o:概念 .
  ?x rdf:type ?c .
} GROUP BY ?概念 ORDER BY DESC(?实例数)`,
  },
  {
    key: 'contraindication',
    label: '禁忌关系网：哪些药物禁忌于胃溃疡（med_common）',
    sql: `PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX rel: <urn:o:onto_med_common:relation:>
SELECT ?药物 WHERE {
  ?d rdfs:label "胃溃疡" .
  ?药物 rel:禁忌于 ?d .
}`,
  },
  {
    key: 'central-dogma',
    label: '中心法则链：TP53 → 转录本 → 蛋白（gene_core）',
    sql: `PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX g: <urn:o:onto_gene_core:>
SELECT ?基因 ?转录本 ?蛋白 WHERE {
  ?gene rdfs:label "TP53" .
  ?gene g:relation:转录为 ?转录本 .
  ?转录本 g:relation:翻译为 ?蛋白 .
}`,
  },
  {
    key: 'unannotated',
    label: '开放问题：哪些概念没有任何实例注释',
    sql: `PREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT ?概念 WHERE {
  ?概念 rdf:type rdfs:Class .
  FILTER NOT EXISTS { ?x rdf:type ?概念 }
} ORDER BY ?概念`,
  },
]

/** SPARQL 关键字与常用词补全（codemirror-lang-sparql 只含高亮，补全自组） */
const COMPLETION_WORDS = [
  'SELECT', 'WHERE', 'PREFIX', 'BASE', 'DISTINCT', 'LIMIT', 'OFFSET', 'FILTER',
  'OPTIONAL', 'UNION', 'ORDER BY', 'ASC', 'DESC', 'GROUP BY', 'HAVING', 'AS',
  'ASK', 'CONSTRUCT', 'DESCRIBE', 'BIND', 'VALUES', 'a',
  'rdf:type', 'rdfs:label', 'rdfs:comment', 'rdfs:subClassOf', 'rdfs:domain', 'rdfs:range',
  'owl:Class', 'owl:ObjectProperty', 'owl:NamedIndividual',
]

function sparqlCompletions(context: CompletionContext) {
  const word = context.matchBefore(/[\w:]*/)
  if (!word || (word.from === word.to && !context.explicit)) return null
  return {
    from: word.from,
    options: COMPLETION_WORDS.map((label) => ({ label, type: 'keyword' })),
  }
}

type SelectResult = { kind: 'select'; vars: string[]; rows: Record<string, string>[]; ms: number }
type AskResult = { kind: 'ask'; value: boolean; ms: number }
type GraphResult = { kind: 'graph'; text: string; ms: number }
type QueryResult = SelectResult | AskResult | GraphResult

function detectQueryForm(q: string): 'select' | 'ask' | 'graph' | 'update' {
  // 跳过注释与 PREFIX/BASE 后取首个有意义的查询词（与 facade 白名单同思路的轻量版）
  const stripped = q.replace(/#[^\n]*/g, ' ')
  const m = stripped.match(/\b(SELECT|ASK|CONSTRUCT|DESCRIBE|INSERT|DELETE|LOAD|CLEAR|CREATE|DROP|MOVE|COPY|ADD)\b/i)
  const kw = (m?.[1] ?? 'SELECT').toUpperCase()
  if (kw === 'SELECT') return 'select'
  if (kw === 'ASK') return 'ask'
  if (kw === 'CONSTRUCT' || kw === 'DESCRIBE') return 'graph'
  return 'update'
}

function toCsv(vars: string[], rows: Record<string, string>[]): string {
  const esc = (v: string) => (/[",\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v)
  const head = vars.map(esc).join(',')
  const body = rows.map((r) => vars.map((v) => esc(r[v] ?? '')).join(',')).join('\n')
  return '\ufeff' + head + '\n' + body
}

export default function SparqlWorkbench({ profileId, persistenceId }: { profileId: string; persistenceId: string }) {
  const [query, setQuery] = useState<string>(() => {
    try {
      return localStorage.getItem(persistenceId) || DEFAULT_SPARQL
    } catch {
      return DEFAULT_SPARQL
    }
  })
  const [running, setRunning] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [result, setResult] = useState<QueryResult | null>(null)
  const queryRef = useRef(query)
  queryRef.current = query
  const runRef = useRef<() => void>(() => {})

  const run = async () => {
    const q = queryRef.current
    const form = detectQueryForm(q)
    if (form === 'update') {
      setErr('工作台仅支持只读查询（SELECT / ASK / CONSTRUCT / DESCRIBE）；变更查询请走构建工具链或对话 facade（受控白名单）。')
      setResult(null)
      return
    }
    setRunning(true)
    setErr(null)
    const t0 = performance.now()
    try {
      const { raw, json } = await api.runSparql(
        profileId,
        q,
        form === 'graph' ? 'text/turtle' : undefined,
      )
      const ms = Math.round(performance.now() - t0)
      if (form === 'ask' && typeof json?.boolean === 'boolean') {
        setResult({ kind: 'ask', value: json.boolean, ms })
      } else if (json?.results?.bindings) {
        const vars: string[] = json.head?.vars ?? Object.keys(json.results.bindings[0] ?? {})
        const rows = json.results.bindings.map((b: Record<string, any>, i: number) => {
          const row: Record<string, string> = { __key: String(i) }
          for (const v of vars) row[v] = b[v]?.value ?? ''
          return row
        })
        setResult({ kind: 'select', vars, rows, ms })
      } else {
        setResult({ kind: 'graph', text: raw, ms })
      }
    } catch (e: any) {
      setErr(e.message)
      setResult(null)
    } finally {
      setRunning(false)
    }
  }
  runRef.current = run

  const extensions = useMemo(
    () => [
      sparql(),
      EditorView.lineWrapping,
      autocompletion({ override: [sparqlCompletions] }),
      Prec.highest(
        keymap.of([
          {
            key: 'Mod-Enter',
            run: () => {
              runRef.current()
              return true
            },
          },
        ]),
      ),
    ],
    [],
  )

  const exportCsv = () => {
    if (result?.kind !== 'select') return
    const blob = new Blob([toCsv(result.vars, result.rows)], { type: 'text/csv;charset=utf-8' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `sparql-result-${profileId}.csv`
    a.click()
    URL.revokeObjectURL(a.href)
  }

  return (
    <div className="onto-sparql-workbench">
      <div className="onto-sparql-toolbar">
        <Button type="primary" icon={<CaretRightOutlined />} loading={running} onClick={run}>
          运行查询
        </Button>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          ⌘/Ctrl + Enter
        </Typography.Text>
        <Select
          size="small"
          style={{ minWidth: 240 }}
          placeholder="查询模板…"
          value={null}
          options={TEMPLATES.map((t) => ({ value: t.key, label: t.label }))}
          onChange={(k) => {
            const t = TEMPLATES.find((x) => x.key === k)
            if (t) {
              setQuery(t.sql)
              setResult(null)
              setErr(null)
            }
          }}
        />
        <Button size="small" icon={<UndoOutlined />} onClick={() => { setQuery(DEFAULT_SPARQL); setResult(null); setErr(null) }}>
          重置
        </Button>
        <Button size="small" icon={<DownloadOutlined />} disabled={result?.kind !== 'select'} onClick={exportCsv}>
          导出 CSV
        </Button>
        <span className="onto-sparql-meta">
          {result && (
            <Tag color="blue" style={{ margin: 0 }}>
              {result.ms}ms
            </Tag>
          )}
          {result?.kind === 'select' && (
            <Tag style={{ margin: 0 }}>{result.rows.length} 行</Tag>
          )}
        </span>
      </div>

      <CodeMirror
        value={query}
        height="190px"
        basicSetup={{ lineNumbers: true, foldGutter: false, highlightActiveLine: true }}
        extensions={extensions}
        onChange={(v) => {
          setQuery(v)
          try {
            localStorage.setItem(persistenceId, v)
          } catch {
            /* 存储不可用则不持久化 */
          }
        }}
      />

      {err && (
        <Alert type="error" showIcon closable style={{ marginTop: 10 }} message="查询执行失败" description={err} onClose={() => setErr(null)} />
      )}

      {result?.kind === 'ask' && (
        <Alert
          type={result.value ? 'success' : 'warning'}
          showIcon
          style={{ marginTop: 10 }}
          message={`ASK 查询结果：${result.value}`}
        />
      )}

      {result?.kind === 'graph' && (
        <pre className="onto-sparql-graph">{result.text}</pre>
      )}

      {result?.kind === 'select' && (
        <Table
          size="small"
          style={{ marginTop: 10 }}
          rowKey="__key"
          dataSource={result.rows}
          pagination={{ pageSize: 10, size: 'small', showSizeChanger: false, showTotal: (n) => `共 ${n} 行` }}
          columns={result.vars.map((v) => ({
            title: `?${v}`,
            dataIndex: v,
            key: v,
            ellipsis: true,
          }))}
          locale={{ emptyText: '查询无结果（空结果集）' }}
        />
      )}

      {!result && !err && (
        <Typography.Text type="secondary" style={{ display: 'block', marginTop: 10, fontSize: 12 }}>
          <Space size={4}>
            提示：SELECT 返回结果表，ASK 返回真值，CONSTRUCT/DESCRIBE 返回 Turtle 文本；查询经平台反代直达运行引擎（REQ-92）。
          </Space>
        </Typography.Text>
      )}

      <div style={{ marginTop: 8 }}>
        <Button type="text" size="small" icon={<ClearOutlined />} onClick={() => { setResult(null); setErr(null) }}>
          清空结果
        </Button>
      </div>
    </div>
  )
}
