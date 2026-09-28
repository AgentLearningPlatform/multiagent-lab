import { Empty, Tag } from 'antd'
import { ontoStatus, stageDoneFlags, type ValidationState } from '../../shared'
import type { Ontology, RuntimeProfile } from '../../../../api/types'

// ---------------------------------------------------------------------------
// REQ-181/M-O17：资产左列表（平台统一侧栏形态）——来源分组（自建 / 导入 / 种子）
// + fork 徽标（forked_from 语义，v1 零迁移派生：original 工件 / 语义 ID / forked_from 字段）
// + 构建段完成度 dots / 版本 / 运行状态标注。选中高亮。
// ---------------------------------------------------------------------------

/** 来源分组（v1 零迁移派生口径，D-O21）：
 *  seed_* 语义 ID → 种子；forked_from 非空 → fork（徽标）；其余 → 自建；
 *  importer 管道产物（TTL 导入等）后续以 artifact original 标记归「导入」（当前并入自建，导入审查交付后细分）。 */
function groupOf(o: Ontology): 'seed' | 'fork' | 'built' {
  if (/^onto_(seed|k8s_ops|med_common|gene_core)/.test(o.id) || o.forked_from === '') {
    if (o.id.startsWith('onto_seed') || ['onto_k8s_ops', 'onto_med_common', 'onto_gene_core'].includes(o.id)) return 'seed'
  }
  if (o.forked_from) return 'fork'
  return 'built'
}

const GROUP_META: Record<string, { label: string; order: number }> = {
  built: { label: '自建', order: 0 },
  seed: { label: '种子', order: 1 },
  fork: { label: 'Fork', order: 2 },
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
  // 分组：自建 → 种子 → fork（组内按 updated_at 已有排序保持）
  const groups = new Map<string, Ontology[]>()
  for (const o of ontos) {
    const g = groupOf(o)
    if (!groups.has(g)) groups.set(g, [])
    groups.get(g)!.push(o)
  }
  const ordered = [...groups.entries()].sort((a, b) => GROUP_META[a[0]].order - GROUP_META[b[0]].order)

  if (ontos.length === 0) {
    return (
      <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无本体——到「本体构建」栏创建" style={{ marginTop: 24 }} />
    )
  }

  return (
    <div className="asset-list">
      {ordered.map(([g, items]) => (
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
                title={`${o.name} · v${o.version ?? '—'} · ${st.text}`}
              >
                <span className="asset-item-name" title={o.name}>{o.name}</span>
                <span className="asset-item-meta">
                  {g === 'fork' && <Tag color="purple" style={{ margin: 0, fontSize: 10, lineHeight: '15px', padding: '0 4px' }}>fork</Tag>}
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
      ))}
    </div>
  )
}
