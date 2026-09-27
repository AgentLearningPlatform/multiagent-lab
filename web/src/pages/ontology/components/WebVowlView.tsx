import { useEffect, useRef, useState } from 'react'
import { Alert, Button, Space, Spin, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { api } from '../../../api/client'

// ---------------------------------------------------------------------------
// M21/VIZ-3（REQ-154）：WebVOWL 对照视图——本体语义原生的可视化视觉语言（D-O9 对照项激活）。
// 集成方式：webvowl 浏览器构建（UMD 全量含 d3）经 public/vendor 静态加载，prepare-vendor
// 自 angular-webvowl npm 包自动复制（克隆/重装依赖即自愈）。
// 数据源（2026-09-27 转换链重构，原 owl2vowl.js 为无效链接从未可用——owl2vowl 是 Java-only
// 转换器无浏览器分发）：平台原生 VOWL JSON 导出 /api/ontologies/{id}/export?format=vowljson，
// 前端零转换直接渲染；本视图为 2D SVG，与 WebGL 无关。
// ---------------------------------------------------------------------------

/** webvowl 1.1.x graph 实例（只声明用到的子集） */
interface WebVowlGraph {
  options: () => WebVowlOpts
  start: () => void // 只建空骨架（内部 loadGraphData(true) 跳过解析）
  load: () => void // 解析 options.data 并渲染（数据装载的唯一生效路径）
  updateCanvasContainerSize: () => void // 按当前 options 宽高刷新 svg 画布（容器尺寸变化时调用）
}

/** webvowl 1.1.x options 链式设置器（只声明用到的子集） */
interface WebVowlOpts {
  data: (json: unknown) => void
  width: (v?: number) => WebVowlOpts
  height: (v?: number) => WebVowlOpts
  graphContainerSelector: (v?: string) => WebVowlOpts
  /** 空 sidebar 模块注入（空白修复）：内部 forceRelocationEvent 会读 leftSidebar().isSidebarVisible() */
  leftSidebar: (v?: unknown) => void
  [key: string]: unknown
}

declare global {
  interface Window {
    webvowl?: {
      graph: (containerSelector?: string) => WebVowlGraph
      util?: unknown
    }
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
  const graphRef = useRef<WebVowlGraph | null>(null)
  // webvowl 1.1.x 的 graph() 只接受「选择器字符串」——内部 d3.selectAll(选择器) 对单个 DOM
  // 元素会解出空集（redrawGraph 建 svg 失败 → 随后 .on("dblclick.zoom") 读空节点崩溃），
  // 故给容器生成唯一 id、以 #id 传参（2026-09-27 修复）
  const selRef = useRef<string>('')
  if (!selRef.current) selRef.current = `wv_${Math.random().toString(36).slice(2, 9)}`
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState<string | null>(null)
  const [ttlBytes, setTtlBytes] = useState(0)
  // 力布局收敛期提示（2026-09-27 空白修复配套）：数据装载后 webvowl 力导向需 ~2s 收敛展开，
  // 期间画布近似空白——显式提示「计算中」避免被误判为渲染失败（此前报障的观感来源之一）
  const [layouting, setLayouting] = useState(false)

  const render = async () => {
    setLoading(true)
    setErr(null)
    try {
      // 1) 取平台原生 VOWL JSON 导出（spec 直转，后端零依赖完成原 owl2vowl 的转换职责）
      const res = await fetch(api.ontologyExportUrl(ontologyId, 'vowljson'))
      if (!res.ok) throw new Error(`VOWL JSON 导出失败: HTTP ${res.status}`)
      const json = await res.json()
      setTtlBytes(JSON.stringify(json).length)
      // 2) 注入 webvowl 分发脚本（public/vendor，prepare-vendor 自 npm 包复制）
      loadCss('/vendor/webvowl/webvowl.css')
      await loadScript('/vendor/webvowl/webvowl.js')
      if (!window.webvowl) {
        throw new Error('webvowl 分发文件缺失（public/vendor/webvowl/）——请重新 npm install 并构建（prepare-vendor 自动补齐）')
      }
      // 3) WebVOWL 渲染——严格按官方 app.js 序列：start() 只建空骨架（其 loadGraphData(true)
      //    会跳过解析），数据装载必须走 data() + load()（loadGraphData 内才调用 parser.parse）
      if (containerRef.current) {
        containerRef.current.innerHTML = ''
        const graph = window.webvowl.graph()
        // 空 sidebar 模块（2026-09-27 空白修复）：webvowl 内部 forceRelocationEvent（力布局
        // 收敛后的重定位/适配）会读 options().leftSidebar().isSidebarVisible()——未配置时该处
        // 抛 TypeError，重定位中断，节点停留在布局期 hidden 态不被揭示 → 画布全白（全屏尺寸
        // 变化 + 视图重挂后必现）。本视图无侧栏，isSidebarVisible=false 语义即「按全宽适配」。
        graph.options().leftSidebar({ isSidebarVisible: () => false, showSidebar: () => {}, hideCollapseButton: () => {} })
        graph
          .options()
          .graphContainerSelector(`#${selRef.current}`)
          .width(containerRef.current.clientWidth)
          .height(containerRef.current.clientHeight)
        graph.start()
        graph.options().data(json)
        graph.load()
        graphRef.current = graph
        setLayouting(true)
        window.setTimeout(() => setLayouting(false), 2500)
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

  // 全屏/窗口尺寸变化：保住图实例与力布局状态，仅刷新画布尺寸（不重挂载——重挂载会让
  // 力布局从零重跑，节点全部堆回原点）
  // 2026-09-27 空白修复②：退出全屏瞬间读到的是过渡前布局（:fullscreen 类移除有过渡），
  // 单次刷新会把 svg 留在全屏尺寸 → 内容被 overflow 裁剪成空白。补 rAF + 350ms 二次刷新。
  useEffect(() => {
    const onResize = () => {
      const g = graphRef.current
      const c = containerRef.current
      if (!g || !c) return
      const apply = () => {
        if (!containerRef.current) return
        g.options().width(containerRef.current.clientWidth).height(containerRef.current.clientHeight)
        g.updateCanvasContainerSize()
      }
      apply()
      requestAnimationFrame(apply)
      const t = window.setTimeout(apply, 350)
      return () => window.clearTimeout(t)
    }
    const wrap = () => void onResize()
    document.addEventListener('fullscreenchange', wrap)
    window.addEventListener('resize', wrap)
    return () => {
      document.removeEventListener('fullscreenchange', wrap)
      window.removeEventListener('resize', wrap)
    }
  }, [])

  return (
    <div style={{ position: 'relative' }}>
      <Space style={{ marginBottom: 8 }}>
        <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={render}>
          重新转换并渲染
        </Button>
        <Typography.Text type="secondary" style={{ fontSize: 11 }}>
          WebVOWL（本体语义原生视觉语言，D-O9 对照视图）· 数据源：平台 VOWL JSON 原生导出（spec 直转）
          {ttlBytes > 0 ? ` · ${Math.round(ttlBytes / 1024)}KB JSON` : ''}
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
          <Spin tip="加载 VOWL JSON 并渲染中…" />
        </div>
      )}
      {!loading && layouting && !err && (
        <div style={{ position: 'absolute', left: 0, right: 0, top: 60, display: 'flex', justifyContent: 'center', pointerEvents: 'none', zIndex: 3, background: 'rgba(255,255,255,0.72)', borderRadius: 8, padding: '6px 0' }}>
          <Spin tip="力布局计算中（百级本体约需数秒）…" />
        </div>
      )}
      <div
        ref={containerRef}
        id={selRef.current}
        className="webvowl-box"
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
