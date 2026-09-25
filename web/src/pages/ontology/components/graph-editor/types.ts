// ---------------------------------------------------------------------------
// GraphEditor 共享类型（B1 拆分，REQ-145）
// ---------------------------------------------------------------------------

export type Selection = { kind: 'node'; id: string } | { kind: 'edge'; id: string } | null

export interface ConnDraft {
  source: string
  target: string
  kind: 'parent' | 'relation' | 'instance-relation'
}
