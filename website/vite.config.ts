import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'

// REQ-238：项目官网（GitHub Pages）。默认部署在仓库子路径 /multiagent-lab/，
// 自定义域名时用 WEBSITE_BASE=/ 覆盖。内容单源：模块介绍构建期内联 platform-knowledge/
// 模块导读页、演示数据内联 ontology-service 种子 spec（web 前端 ReferencePage 同手法）。
const stripIEHacks = (): Plugin => ({
  // 与 web/vite.config.ts 同款：剥第三方 CSS 的 IE 星号 hack（*zoom 等），消构建告警噪音
  name: 'strip-ie-hacks',
  enforce: 'pre',
  transform(code, id) {
    if (id.includes('yasgui') && id.endsWith('.css')) {
      return { code: code.replace(/(^|[;{}])\*[a-z-]+\s*:[^;{}]*;?/gi, '$1'), map: null }
    }
    return null
  },
})

export default defineConfig({
  plugins: [react(), stripIEHacks()],
  base: process.env.WEBSITE_BASE ?? '/multiagent-lab/',
  server: {
    port: 5174,
    // dev 模式跨目录 import：platform-knowledge/、ontology-service/internal/seed/、web/src/ 组件
    fs: { allow: ['..'] },
  },
  build: { outDir: 'dist' },
  // web/src 组件（Graph3D/SpecGraph）由 web/node_modules 解析依赖，站点自身也有一份 react——
  // 强制收敛到单一 react/react-dom 拷贝，否则双 React 实例触发 Invalid hook call
  resolve: { dedupe: ['react', 'react-dom'] },
})
