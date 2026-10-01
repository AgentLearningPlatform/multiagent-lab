import { useMemo, useState } from 'react'
import { Alert, Card, Select, Space, Tabs, Tag, Typography } from 'antd'
import Graph3D from '../../../web/src/pages/ontology/components/Graph3D'
import SpecGraph from '../../../web/src/pages/ontology/components/SpecGraph'
import { SEEDS, seedCounts } from '../content'

// ---------------------------------------------------------------------------
// 在线演示页（REQ-238）：内置种子本体（后端 internal/seed 启动播种同一批）构建产物的
// 可视化——三维图谱（Graph3D）与二维图谱（SpecGraph）均为纯前端渲染、零后端依赖，
// 组件直接复用 web/src（平台资产可视化同款）。诚实边界：仅展示静态构建产物，
// 构建/运行/对话/抽取/审计等完整能力需部署平台。
// ---------------------------------------------------------------------------

export default function DemoPage() {
  const [key, setKey] = useState(SEEDS[0].key)
  const seed = useMemo(() => SEEDS.find((s) => s.key === key) ?? SEEDS[0], [key])
  const counts = seedCounts(seed.spec)

  return (
    <main className="page">
      <div className="page-inner">
        <h1>在线演示 · 内置示例本体</h1>
        <p className="module-tagline">
          以下为平台内置种子本体的<strong>静态可视化演示</strong>（与后端启动播种同源数据）——
          三维/二维图谱渲染复用平台资产可视化组件，可拖拽、缩放、搜索、点选节点查看定义。
        </p>
        <Alert
          type="info"
          showIcon
          style={{ margin: '12px 0 16px' }}
          title="这是静态演示数据"
          description="六条构建路径、运行方案、对话挂载、知识抽取、消费审计等完整能力需要本地或服务器部署平台后体验（见「快速开始」）。"
        />

        <div className="demo-bar">
          <Space size={10} wrap>
            <Select
              value={key}
              onChange={setKey}
              style={{ width: 320 }}
              options={SEEDS.map((s) => ({ value: s.key, label: s.spec.name }))}
            />
            <Tag style={{ margin: 0 }}>概念 {counts.concepts}</Tag>
            <Tag style={{ margin: 0 }}>关系 {counts.relations}</Tag>
            <Tag style={{ margin: 0 }}>实例 {counts.instances}</Tag>
            <Tag color="geekblue" style={{ margin: 0 }}>
              seed/{seed.file}
            </Tag>
          </Space>
        </div>
        {seed.spec.description && (
          <Typography.Paragraph type="secondary" className="demo-desc">
            {seed.spec.description}
          </Typography.Paragraph>
        )}

        <Card size="small" className="demo-viz-card">
          <Tabs
            key={seed.key}
            defaultActiveKey="3d"
            items={[
              {
                key: '3d',
                label: '三维浏览',
                children: (
                  <div className="demo-viz">
                    <Graph3D spec={seed.spec} />
                  </div>
                ),
              },
              {
                key: '2d',
                label: '二维图谱',
                children: (
                  <div className="demo-viz">
                    <SpecGraph spec={seed.spec} />
                  </div>
                ),
              },
            ]}
          />
        </Card>
      </div>
    </main>
  )
}
