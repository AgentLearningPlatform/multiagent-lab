import { useEffect, useState } from 'react'
import { Empty, Input, Tag } from 'antd'
import { ontoStatus, stageDoneFlags, type ValidationState } from '../../shared'
import type { Ontology, RuntimeProfile } from '../../../../api/types'
import { companionApi } from '../../../../api/companion'
import EmptyGuide from '../../../../components/EmptyGuide'

// ---------------------------------------------------------------------------
// REQ-181/M-O17：资产左列表（平台统一侧栏形态）——来源分组（自建 / 对话生长 / 种子 / fork）
// + fork 徽标（forked_from 语义，v1 零迁移派生：original 工件 / 语义 ID / forked_from 字段）
// + 构建段完成度 dots / 版本 / 运行状态标注。选中高亮。
// REQ-216⑦：来源分组扩「对话生长」——有智能体绑定该本体为伴生归属（companion_ontology_id
// 指向它）即归入（作为一个类型；伴生产物归属容器化的资产可见性落点）。
// REQ-233①/M60：列表搜索（名称/描述/ID/来源分组名子串过滤，前端过滤零接口变更）
// + 空态 EmptyGuide 接入（跳「本体构建」动线，治纯文案空态）。
// ---------------------------------------------------------------------------

/** 来源分组（v1 零迁移派生口径，D-O21；REQ-216 增 grown 组，优先于既有判定）：
 *  绑定为伴生归属 → 对话生长；seed_* 语义 ID → 种子；forked_from 非空 → fork（徽标）；其余 → 自建。 */
function groupOf(o: Ontology, bound: Set<string>): 'seed' | 'fork' | 'built' | 'grown' {
  if (bound.has(o.id)) return 'grown'
  if (/^onto_(seed|k8s_ops|med_common|gene_core)/.test(o.id) || o.forked_from === '') {
    if (o.id.startsWith('onto_seed') || ['onto_k8s_ops', 'onto_med_common', 'onto_gene_core'].includes(o.id)) return 'seed'
  }
  if (o.forked_from) return 'fork'
  return 'built'
}

const GROUP_META: Record<string, { label: string; order: number }> = {
  built: { label: '自建', order: 0 },
  grown: { label: '对话生长', order: 1 },
  seed: { label: '种子', order: 2 },
  fork: { label: 'Fork', order: 3 },
}

export default function AssetList({
  ontos,
  profiles,
  activeId,
  onSelect,
  validations,
}: {
  ontos: Ontology[]
  profiles: RuntimeProfile[]
  activeId: string | null
  onSelect: (id: string) => void
  validations: Record<string, ValidationState>
}) {
  // REQ-216⑦：伴生绑定本体 id 清单（后端 agent 表派生；加载失败静默降级为无分组）
  const [bound, setBound] = useState<Set<string>>(new Set())
  useEffect(() => {
    companionApi
      .boundOntologies()
      .then((r) => setBound(new Set(r.ontology_ids ?? [])))
      .catch(() => setBound(new Set()))
  }, [ontos])

  // REQ-233①：搜索（名称/描述/ID/来源分组名子串，大小写不敏感；前端过滤零接口变更）
  const [q, setQ] = useState('')
  const ql = q.trim().toLowerCase()
  const hit = (o: Ontology, g: string) =>
    !ql ||
    [o.name, o.description ?? '', o.id, GROUP_META[g].label].some((s) => s.toLowerCase().includes(ql))

  // 分组：自建 → 对话生长 → 种子 → fork（组内按 updated_at 已有排序保持）
  const groups = new Map<string, Ontology[]>()
  for (const o of ontos) {
    const g = groupOf(o, bound)
    if (!hit(o, g)) continue
    if (!groups.has(g)) groups.set(g, [])
    groups.get(g)!.push(o)
  }
  const ordered = [...groups.entries()].sort((a, b) => GROUP_META[a[0]].order - GROUP_META[b[0]].order)

  const goBuild = () => {
    localStorage.setItem('eino.onto.sidebar', 'build')
    window.dispatchEvent(new CustomEvent('onto-sidebar-change'))
  }

  if (ontos.length === 0) {
    return (
      <EmptyGuide
        title="暂无本体"
        steps={['到「本体构建」栏选择一条构建路径（自定义 / OntoChat / 由知识库构建等）', '构建产物统一进入本资产列表管理']}
        actionLabel="前往本体构建"
        onAction={goBuild}
      />
    )
  }

  return (
    <div className="asset-list">
      <Input
        allowClear
        size="small"
        placeholder="搜索名称 / 描述 / 来源"
        value={q}
        onChange={(e) => setQ(e.target.value)}
        style={{ marginBottom: 10 }}
        data-testid="asset-search"
      />
      {ordered.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={`无匹配「${q.trim()}」的本体`} style={{ marginTop: 16 }} />
      ) : (
        ordered.map(([g, items]) => (
        <div key={g} className="asset-group">
          <div className="asset-group-title">{GROUP_META[g].label} <span className="side-count">{items.length}</span></div>
          {items.map((o) => {
            const f = stageDoneFlags(o, validations[o.id], profiles, null, false)
            const st = ontoStatus(o, profiles)
            const active = o.id === activeId
            return (
              <button
                key={o.id}
                type="button"
                className={`asset-list-item${active ? ' active' : ''}`}
                onClick={() => onSelect(o.id)}
                title={`${o.name} · v${o.version ?? '—'} · ${st.text}${g === 'grown' ? ' · 对话生长（伴生归属）' : ''}`}
              >
                <span className="asset-item-name" title={o.name}>{o.name}</span>
                <span className="asset-item-meta">
                  {g === 'fork' && <Tag color="purple" style={{ margin: 0, fontSize: 10, lineHeight: '15px', padding: '0 4px' }}>fork</Tag>}
                  {g === 'grown' && <Tag color="geekblue" style={{ margin: 0, fontSize: 10, lineHeight: '15px', padding: '0 4px' }}>对话生长</Tag>}
                  <span className="onto-dots" title={`S1~S4 构建段 ${f.slice(0, 4).filter(Boolean).length}/4`}>
                    {f.slice(0, 4).map((done, i) => (
                      <i key={i} className={`onto-dot${done ? ' on' : ''}`} />
                    ))}
                  </span>
                  <span style={{ fontSize: 10.5, color: 'var(--c-ink-3)' }}>v{o.version ?? '—'}</span>
                  <span className="onto-dot" style={{ background: st.color === 'green' ? '#16a34a' : st.color === 'default' ? '#c3c8da' : st.color, opacity: st.text.includes('运行中') ? 1 : 0.5 }} title={st.text} />
                </span>
              </button>
            )
          })}
        </div>
        ))
      )}
    </div>
  )
}
