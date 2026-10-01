// ---------------------------------------------------------------------------
// 全站布局常量与助手（REQ-237，57 号《前端_页面布局体检与优化分析》P2 整改落地）
//   F4 左栏宽度三分天下 → 单一约定：同 key/默认/边界，七页共用；
//   F10 一屏四套调宽度交互 → Drawer 收敛为三档默认 + 原生 resizable 拖拽 + 宽度记忆。
// ---------------------------------------------------------------------------

/** 模块左栏宽度（Splitter 左面板）：七页共用同一 key/默认/边界——「全站左栏同宽」为产品意图 */
export const SIDEBAR_WIDTH = {
  storageKey: 'eino.sidebar.width',
  default: 280,
  min: 220,
  max: 480,
} as const

export function sidebarDefaultSize(): number {
  return Number(localStorage.getItem(SIDEBAR_WIDTH.storageKey)) || SIDEBAR_WIDTH.default
}

export function sidebarRemember(sizes: number[]): void {
  localStorage.setItem(SIDEBAR_WIDTH.storageKey, String(Math.round(sizes[0])))
}

/** Drawer 三档默认宽（57 号 F10：原 560/640/680/820/860/880 六种取值收敛） */
export const DRAWER_SIZES = { small: 480, medium: 640, large: 820 } as const

/**
 * Drawer 档位 + 原生可拖拽 + 宽度记忆（antd 6 Drawer `size` 数字 + `resizable`）。
 * 用法：<Drawer {...drawerSizeProps('trace', DRAWER_SIZES.medium)} .../>
 */
export function drawerSizeProps(key: string, def: number, min = 400, max = 1080) {
  const storageKey = `eino.drawer.${key}`
  const saved = Number(localStorage.getItem(storageKey))
  const initial = saved >= min && saved <= max ? saved : def
  let latest = initial
  return {
    size: initial,
    resizable: true,
    onResize: (s: number) => {
      latest = s
    },
    onResizeEnd: () => localStorage.setItem(storageKey, String(latest)),
  }
}
