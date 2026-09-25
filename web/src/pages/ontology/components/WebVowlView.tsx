import { useEffect, useRef, useState } from 'react'
import { Alert, Button, Space, Spin, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { api } from '../../../api/client'

// ---------------------------------------------------------------------------
// M21/VIZ-3（REQ-154）：WebVOWL 对照视图——本体语义原生的可视化视觉语言（D-O9 对照项激活）。
// 集成方式：webvowl 独立分发包（dist js/css）经 public/vendor 静态加载；数据源 = 平台 TTL 导出
// （/api/ontologies/{id}/export?format=turtle，即本体的 Turtle 形态）经 owl2vowl 转换为 webvowl json。
// 转换链落地：sidecar rdflib 不可用时降级提示（诚实边界）；Safari 等 WebGL 无关（本视图为 2D SVG）。
// ---------------------------------------------------------------------------

declare global {
  interface Window {
    webvowl?: { graph: (container: HTMLElement) => { options: () => { data: (json: unknown) => void } }; util?: unknown }
    owl2vowl?: { convert: (opts: { data?: string; fileContents?: string }) => { json: () => unknown } }
  }
}

/** 动态注入脚本/样式（幂等） */
function loadScript(src: string): Promise<void> {
  return new Promise((resolve, reject) => {
    if (document.querySelector(`script[src="${src}"]`)) return resolve()
    const el = document.createElement('script')
    el.src = src
    el.onload = () => resolve()
    el.onerror = () => reject(new Error(`加载失败: ${src}`))
    document.head.appendChild(el)
  })
}
function loadCss(href: string): void {
  if (document.querySelector(`link[href="${href}"]`)) return
  const el = document.createElement('link')
  el.rel = 'stylesheet'
  el.href = href
  document.head.appendChild(el)
}

export default function WebVowlView({ ontologyId }: { ontologyId: string }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState<string | null>(null)
  const [ttlBytes, setTtlBytes] = useState(0)

  const render = async () => {
    setLoading(true)
    setErr(null)
    try {
      // 1) 取平台 TTL 导出（本体的 Turtle 形态）
      const res = await fetch(api.ontologyExportUrl(ontologyId, 'turtle'))
      if (!res.ok) throw new Error(`TTL 导出失败: HTTP ${res.status}`)
      const ttl = await res.text()
      setTtlBytes(ttl.length)
      // 2) 注入 webvowl + owl2vowl 分发脚本（public/vendor，随镜像分发）
      loadCss('/vendor/webvowl/webvowl.css')
      await loadScript('/vendor/webvowl/webvowl.js')
      await loadScript('/vendor/webvowl/owl2vowl.js')
      if (!window.owl2vowl || !window.webvowl) {
        throw new Error('webvowl/owl2vowl 分发文件缺失（public/vendor/webvowl/）——请运行 tools/fetch-webvowl.sh 或参考 15 号登记簿部署')
      }
      // 3) OWL2VOWL：TTL → webvowl json（预转换；结果仅内存态，转换缓存随版本 TTL 导出）
      const json = window.owl2vowl.convert({ data: ttl }).json()
      // 4) WebVOWL 渲染
      if (containerRef.current) {
        containerRef.current.innerHTML = ''
        const graph = window.webvowl.graph(containerRef.current)
        graph.options().data(json)
      }
    } catch (e: any) {
      setErr(e?.message ?? String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (ontologyId) void render()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ontologyId])

  return (
    <div>
      <Space style={{ marginBottom: 8 }}>
        <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={render}>
          重新转换并渲染
        </Button>
        <Typography.Text type="secondary" style={{ fontSize: 11 }}>
          WebVOWL（本体语义原生视觉语言，D-O9 对照视图）· 数据源：平台 TTL 导出 → owl2vowl 预转换
          {ttlBytes > 0 ? ` · ${Math.round(ttlBytes / 1024)}KB TTL` : ''}
        </Typography.Text>
      </Space>
      {err && (
        <Alert
          type="warning"
          showIcon
          message="WebVOWL 对照视图不可用"
          description={err}
          style={{ marginBottom: 8 }}
        />
      )}
      {loading && !err && (
        <div style={{ textAlign: 'center', padding: 40 }}>
          <Spin tip="转换并渲染中（TTL → owl2vowl → webvowl）…" />
        </div>
      )}
      <div
        ref={containerRef}
        style={{ width: '100%', height: 560, border: '1px solid var(--ant-color-border, #e3e6f0)', borderRadius: 8, background: '#fff', overflow: 'hidden' }}
      />
      {!err && !loading && (
        <Typography.Text type="secondary" style={{ fontSize: 11, display: 'block', marginTop: 6 }}>
          对照说明：WebVOWL 为 OWL 视觉语言的事实标准（类区块/对象属性/特征标记，可折叠展开、过滤器、搜索）；
          与 2D React Flow（结构视图）和三维浏览（沉浸视图）构成三重视角。编辑仍归「图形编辑」（REQ-71）。
        </Typography.Text>
      )}
    </div>
  )
}
