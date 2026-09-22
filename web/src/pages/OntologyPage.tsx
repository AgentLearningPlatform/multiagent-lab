/**
 * 本体模块入口（D-O11 改版后的薄壳）。
 * 原单页七阶段流水线已按 14 v0.3 §3 拆分为四栏模块（web/src/pages/ontology/）：
 *   OntologyModule（壳）+ LearnPage / BuildPage / AssetsPage / RuntimePage + components/*
 * 本文件仅为 App.tsx 的稳定导入点，避免路由层感知内部结构。
 */
import OntologyModule from './ontology/OntologyModule'

export default function OntologyPage() {
  return <OntologyModule />
}
