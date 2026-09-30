import type { ReactNode } from 'react'

/**
 * B1 引用溯源（M36）：按 rune 偏移区间高亮文本片段（<mark>）。
 * 后端 span 偏移按 Go rune 计，前端用 Array.from（码点对齐）切分后按区间打标。
 * 无区间时原样返回（诚实降级）。
 */
export default function HighlightSpans({
  text,
  spans,
  color,
}: {
  text: string
  spans?: { start: number; end: number }[]
  color?: string
}) {
  if (!text || !spans?.length) return <>{text}</>
  const chars = Array.from(text)
  const mark = new Array<boolean>(chars.length).fill(false)
  let any = false
  for (const s of spans) {
    for (let i = Math.max(0, s.start); i < s.end && i < chars.length; i++) {
      mark[i] = true
      any = true
    }
  }
  if (!any) return <>{text}</>
  const out: ReactNode[] = []
  let buf: string[] = []
  let cur: boolean | null = null
  const flush = () => {
    if (buf.length === 0) return
    if (cur) {
      out.push(
        <mark key={out.length} style={{ background: color ?? '#ffe58f', padding: '0 1px', borderRadius: 2 }}>
          {buf.join('')}
        </mark>,
      )
    } else {
      out.push(buf.join(''))
    }
    buf = []
  }
  chars.forEach((c, i) => {
    if (mark[i] !== cur) {
      flush()
      cur = mark[i]
    }
    buf.push(c)
  })
  flush()
  return <>{out}</>
}

/** 在 hay 中定位 needle 首次出现（码点偏移区间；未命中返回 undefined——前端定位 claim 出处用） */
export function locateText(hay: string, needle: string): { start: number; end: number }[] | undefined {
  if (!hay || !needle) return undefined
  const hc = Array.from(hay)
  const nc = Array.from(needle)
  if (nc.length === 0 || nc.length > hc.length) return undefined
  outer: for (let i = 0; i + nc.length <= hc.length; i++) {
    for (let j = 0; j < nc.length; j++) {
      if (hc[i + j] !== nc[j]) continue outer
    }
    return [{ start: i, end: i + nc.length }]
  }
  return undefined
}
