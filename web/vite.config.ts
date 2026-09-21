import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'

// 剥除第三方 CSS（Yasgui 打包的 DataTables 旧样式）中的 IE 星号 hack（*zoom/*cursor 等）：
// 现代浏览器无需这些声明，且 esbuild 压缩时会逐条产生 css-syntax-error 告警噪音。
// 用 transform 插件在文件级处理（postcss 不作用于 node_modules CSS，实测无效）。
const stripIEHacks = (): Plugin => ({
  name: 'strip-ie-hacks',
  enforce: 'pre',
  transform(code, id) {
    if (id.includes('yasgui') && id.endsWith('.css')) {
      // 移除 IE 星号 hack 声明（*prop:value;）——仅匹配声明起始位置的星号
      return { code: code.replace(/(^|[;{}])\*[a-z-]+\s*:[^;{}]*;?/gi, '$1'), map: null }
    }
    return null
  },
})

// 前端 dev server：:5173，API 代理到本地 backend :8080
export default defineConfig({
  plugins: [react(), stripIEHacks()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
  build: { outDir: 'dist' },
})
