/**
 * 平台知识/文档互引的仓库内 .md 路径解析（REQ-161/169）。
 * 从 ReferencePage 抽出共享：正文相对引用点击（主题页与 DocViewerModal 抽屉内）共用同一套解析口径。
 */

/** 文档编号 → 仓库相对路径（frontmatter docs/decisions 指针用；新编号在此补行） */
export const DOC_FILE_BY_NO: Record<string, string> = {
  '01': 'docs/01_智能体_需求文档_PRD.md',
  '02': 'docs/02_智能体_技术方案设计.md',
  '03': 'docs/03_本体_需求文档.md',
  '04': 'docs/04_本体_方案设计.md',
  '11': 'docs/11_知识库_需求文档.md',
  '12': 'docs/12_知识库_方案设计.md',
  '14': 'docs/14_本体_前端改造方案.md',
  '16': 'docs/16_部署与运行.md',
  '17': 'platform-knowledge/整体设计/17_产品_信息架构与界面设计.md',
  '18': 'docs/18_REQ编号注册表.md',
  '19': 'platform-knowledge/本体/开源项目分析/19_本体_semantica集成方案.md',
  '20': 'docs/20_回归冒烟清单.md',
  '21': 'platform-knowledge/知识库/21_知识库能力增强调研.md',
  '22': 'platform-knowledge/智能体/22_多类型智能体方案研究.md',
  '23': 'platform-knowledge/本体/23_本体_开源实现方案借鉴研究.md',
  '24': 'platform-knowledge/本体/24_本体_本地工程化落地方案调研.md',
  '25': 'platform-knowledge/知识库/25_Agent知识库与知识图谱构建接入方案.md',
}

/** 百分号编码防御性解码（XMarkdown 部分渲染路径会对中文 href 做 encodeURI；畸形序列原样返回） */
function safeDecode(s: string): string {
  if (!s.includes('%')) return s
  try {
    return decodeURIComponent(s)
  } catch {
    return s
  }
}

/** 相对引用解析（REQ-161 补充要求：文档互引用相对路径）——按所在仓库目录解析，返回仓库相对 .md 路径或 null */
export function resolveRef(href: string, base: string): string | null {
  const h = safeDecode(href.trim())
  if (!h || h.startsWith('http://') || h.startsWith('https://') || h.startsWith('#') || h.startsWith('mailto:')) return null
  if (h.startsWith('/')) return null
  const joined = /^(platform-knowledge|docs|research|seeds)\//.test(h) ? h : `${base}/${h}`
  const parts: string[] = []
  for (const seg of joined.split('/')) {
    if (seg === '' || seg === '.') continue
    if (seg === '..') parts.pop()
    else parts.push(seg)
  }
  const out = parts.join('/')
  return out.endsWith('.md') ? out : null
}

export function docFileOf(ptr: string): string | null {
  const s = ptr.trim()
  // 完整路径直传（docs/ platform-knowledge/ research/ 下的 .md）
  if (s.endsWith('.md') && (s.startsWith('docs/') || s.startsWith('research/') || s.startsWith('platform-knowledge/'))) return s
  // 编号指针（如 "02" / "02 §6.4" / "17 §2.2"）：取前两位编号映射
  const no = s.slice(0, 2)
  return DOC_FILE_BY_NO[no] ?? null
}
