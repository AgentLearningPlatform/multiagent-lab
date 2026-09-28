// 本地 UMD 分发准备（M21/VIZ-1 配套）：web/public/vendor/ 按 14 号 v0.18 约定不入库
// （.gitignore 覆盖），克隆或重装依赖后目录为空——Graph3D 经 /vendor/3d-force-graph.min.js
// 注入会 404→SPA fallback 回 HTML→「三维视图初始化失败」。本脚本自 node_modules 的
// 3d-force-graph npm 包复制 UMD，挂接在 npm run dev / build 前自动执行（免手工放置）。
import { copyFileSync, existsSync, mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = dirname(dirname(fileURLToPath(import.meta.url)))
const dest = join(root, 'public', 'vendor')
mkdirSync(dest, { recursive: true })

// WebVOWL 资源（2026-09-27 修复）：自 angular-webvowl npm 包复制 WebVOWL 1.1.x 浏览器构建
// （UMD 全量含 d3，挂 window.webvowl）。渲染端由此自愈；TTL→VOWL JSON 转换已移至
// ontology-service 原生导出（?format=vowljson）——owl2vowl 为 Java-only 无浏览器分发，
// 原工具链的 owl2vowl.js 引用从未可用。tools/fetch-webvowl.sh 保留为离线手动兜底。
const jobs = [
  { from: '3d-force-graph/dist/3d-force-graph.min.js', to: '3d-force-graph.min.js' },
  { from: 'angular-webvowl/dist/webvowl.js', to: 'webvowl/webvowl.js' },
  { from: 'angular-webvowl/dist/webvowl.css', to: 'webvowl/webvowl.css' },
]

// 依赖包缺失（典型场景：拉取了新增 devDependency 的代码但未重新 npm install）时给出可行动
// 指引——2026-09-27 他机报障「npm run build 报 ENOENT copyfile angular-webvowl」：脚本路径
// 均经 import.meta.url 相对解析非绝对路径硬编码，实为其 node_modules 落后于 package.json。
const missingPkgs = [...new Set(jobs.map((j) => j.from.split('/')[0]))].filter(
  (pkg) => !existsSync(join(root, 'node_modules', pkg)),
)
if (missingPkgs.length > 0) {
  console.warn(
    `[prepare-vendor] 提示: 依赖包未安装（${missingPkgs.join(', ')}）——请在 web/ 目录执行 npm install 后重新构建；` +
      '本次构建继续，缺失的可视化资源将在运行时诚实降级报错',
  )
}

for (const { from, to } of jobs) {
  try {
    const target = join(dest, to)
    mkdirSync(dirname(target), { recursive: true })
    copyFileSync(join(root, 'node_modules', from), target)
    console.log(`[prepare-vendor] ${to} ← node_modules/${from}`)
  } catch (e) {
    console.warn(`[prepare-vendor] 警告: ${to} 复制失败（${e?.message}）——对应视图将诚实降级报错`)
  }
}
