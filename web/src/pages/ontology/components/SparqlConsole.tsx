import { useEffect, useRef } from 'react'
import Yasgui from '@triply/yasgui'

// ---------------------------------------------------------------------------
// Yasgui 实例挂载（vanilla JS → React 桥，REQ-92）
// ---------------------------------------------------------------------------

/** SPARQL 工作台默认模板（REQ-92：/api/runtime-profiles/{id}/sparql，引擎直连） */
const DEFAULT_SPARQL = `PREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT ?s ?o WHERE { ?s rdf:type ?o } LIMIT 20`

/**
 * Yasgui 桥：
 *  - 以 endpoint / persistenceId 为生命周期边界，绑定方案变更即销毁重建；查询历史走 Yasgui 自带 localStorage；
 *  - StrictMode 双挂载由 cleanup 的 destroy() 兜底（destroy 会移除其 rootEl 与全局监听）；
 *  - method 用 GET（?query=）匹配运行平面反代：POST 侧要求 application/sparql-query 原文，
 *    而 Yasgui 的 POST 会发 x-www-form-urlencoded，故改用 GET（两端点均支持）。
 */
export default function YasguiPane({ endpoint, persistenceId }: { endpoint: string; persistenceId: string }) {
  const hostRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const y = new Yasgui(host, {
      // Yasgui 对顶层 config 为浅合并：requestConfig 需给全量，否则会丢失 Accept / 参数默认值
      requestConfig: {
        endpoint,
        method: 'GET',
        acceptHeaderSelect: 'application/sparql-results+json,*/*;q=0.9',
        acceptHeaderGraph: 'application/n-triples,*/*;q=0.9',
        acceptHeaderUpdate: 'text/plain,*/*;q=0.9',
        namedGraphs: [],
        defaultGraphs: [],
        args: [],
        headers: {},
        withCredentials: false,
        adjustQueryBeforeRequest: false,
      },
      persistenceId,
      autoAddOnInit: true,
      copyEndpointOnNewTab: false,
    })
    const tab = y.getTab()
    // 仅在空白新标签页填默认模板；已从 localStorage 恢复的查询保持原样
    if (tab && !tab.getQuery().trim()) tab.setQuery(DEFAULT_SPARQL)
    return () => {
      try {
        y.destroy()
      } catch {
        /* 已销毁 */
      }
      host.innerHTML = ''
    }
  }, [endpoint, persistenceId])

  return <div ref={hostRef} className="onto-yasgui" />
}
