// ---------------------------------------------------------------------------
// 官网内容单源（REQ-238）：模块介绍 = platform-knowledge/ 各模块 00 导读页（构建期内联，
// 与平台知识页 ReferencePage 同手法——内容只在 platform-knowledge 维护一处）；
// 演示数据 = ontology-service/internal/seed/examples/ 种子 spec（后端启动播种同一份）。
// ---------------------------------------------------------------------------

// 模块导读页（?raw 构建期内联）
import agentsMd from '../../platform-knowledge/02_智能体/00_智能体模块.md?raw'
import projectsMd from '../../platform-knowledge/03_项目/00_项目模块.md?raw'
import ontologyMd from '../../platform-knowledge/04_本体/00_本体模块.md?raw'
import knowledgeMd from '../../platform-knowledge/05_知识库/00_知识库模块.md?raw'
import skillsMd from '../../platform-knowledge/06_技能/00_技能模块.md?raw'
import settingsMd from '../../platform-knowledge/07_设置/00_设置模块.md?raw'
import designMd from '../../platform-knowledge/01_整体设计/00_整体设计.md?raw'

// 演示种子（与后端启动播种同源；?raw + JSON.parse，避免 TS 深层 JSON 类型推断）
import orgsRaw from '../../ontology-service/internal/seed/examples/orgs.json?raw'
import defectsRaw from '../../ontology-service/internal/seed/examples/defects.json?raw'
import failureRaw from '../../ontology-service/internal/seed/examples/failure.json?raw'
import geneCoreRaw from '../../ontology-service/internal/seed/examples/gene_core.json?raw'
import medCommonRaw from '../../ontology-service/internal/seed/examples/med_common.json?raw'
import type { Spec } from '../../web/src/api/types'

/** 仓库地址：构建期注入（CI=触发仓库 github.repository，随迁移/fork 自动跟随；缺省=组织仓库），见 vite.config.ts define */
export const REPO_SLUG: string = __REPO_SLUG__
export const REPO_URL = `https://github.com/${REPO_SLUG}`
const REPO_RAW_BASE = `${REPO_URL}/blob/main`

export interface ModuleInfo {
  key: string
  /** 仓库内相对路径（GitHub 源文档链接与站内相对链接解析用） */
  path: string
  title: string
  tagline: string
  md: string
}

/** 功能模块（目录序=导航序；整体设计放最后，作为深入了解入口） */
export const MODULES: ModuleInfo[] = [
  { key: 'agents', path: 'platform-knowledge/02_智能体/00_智能体模块.md', title: '智能体', tagline: '对话 · 多智能体协作 · 知识/技能/本体增强', md: agentsMd },
  { key: 'ontology', path: 'platform-knowledge/04_本体/00_本体模块.md', title: '本体', tagline: '构建 → 资产 → 运行 → 消费审计 全流程闭环', md: ontologyMd },
  { key: 'knowledge', path: 'platform-knowledge/05_知识库/00_知识库模块.md', title: '知识库', tagline: 'RAG / GraphRAG 双子模块（KG 自研内置）', md: knowledgeMd },
  { key: 'projects', path: 'platform-knowledge/03_项目/00_项目模块.md', title: '项目', tagline: '多智能体协作工作空间（目录绑定 / 文件 / Git）', md: projectsMd },
  { key: 'skills', path: 'platform-knowledge/06_技能/00_技能模块.md', title: '技能', tagline: '智能体能力扩展（提示词 / 工具包装）', md: skillsMd },
  { key: 'settings', path: 'platform-knowledge/07_设置/00_设置模块.md', title: '设置', tagline: '模型连接 · 连接器 · 运行环境 · 使用统计', md: settingsMd },
  { key: 'design', path: 'platform-knowledge/01_整体设计/00_整体设计.md', title: '整体设计', tagline: '产品定位 / 架构分层 / 技术栈 / 协议面 / 部署形态', md: designMd },
]

/** 剥 frontmatter（DocViewerModal 同款正则） */
export function stripFrontmatter(md: string): string {
  return md.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n?/, '')
}

/** md 相对链接 → 仓库源文档（导读页互引在静态站不落盘，统一指到 GitHub 源文件） */
export function resolveMdLink(href: string, fromPath: string): string | null {
  if (!href || /^(https?:|mailto:|#|\/\/)/.test(href)) return null
  const clean = href.split('#')[0]
  if (!clean.endsWith('.md')) return null
  const dir = fromPath.split('/').slice(0, -1).join('/')
  const parts = (dir ? `${dir}/${clean}` : clean).split('/')
  const stack: string[] = []
  for (const seg of parts) {
    if (seg === '..') stack.pop()
    else if (seg !== '.') stack.push(seg)
  }
  return `${REPO_RAW_BASE}/${stack.join('/')}`
}

/** 演示种子（后端 internal/seed 启动播种的同一批示例本体） */
export interface SeedSpec {
  key: string
  file: string
  spec: Spec
}

function parseSeed(key: string, file: string, raw: string): SeedSpec {
  return { key, file, spec: JSON.parse(raw) as Spec }
}

export const SEEDS: SeedSpec[] = [
  parseSeed('orgs', 'orgs.json', orgsRaw),
  parseSeed('defects', 'defects.json', defectsRaw),
  parseSeed('failure', 'failure.json', failureRaw),
  parseSeed('gene_core', 'gene_core.json', geneCoreRaw),
  parseSeed('med_common', 'med_common.json', medCommonRaw),
]

export const seedCounts = (s: Spec) => ({
  concepts: s.concepts?.length ?? 0,
  relations: s.relations?.length ?? 0,
  instances: s.instances?.length ?? 0,
})
