import { create } from 'zustand'

export type PageKey = 'agents' | 'projects' | 'ontology' | 'knowledge' | 'skills' | 'settings'

interface UIState {
  page: PageKey
  setPage: (p: PageKey) => void

  /** 当前选中的对话 */
  currentConvId: string | null
  setCurrentConv: (id: string | null) => void

  /** 数据版本号：增删改后 +1 触发刷新 */
  dataVersion: number
  bumpData: () => void

  toast: { msg: string; kind: 'ok' | 'err' } | null
  showToast: (msg: string, kind?: 'ok' | 'err') => void
}

export const useUI = create<UIState>((set) => ({
  page: 'agents',
  setPage: (p) => set({ page: p }),
  currentConvId: null,
  setCurrentConv: (id) => set({ currentConvId: id }),
  dataVersion: 0,
  bumpData: () => set((s) => ({ dataVersion: s.dataVersion + 1 })),
  toast: null,
  showToast: (msg, kind = 'ok') => {
    set({ toast: { msg, kind } })
    setTimeout(() => set({ toast: null }), 2600)
  },
}))
