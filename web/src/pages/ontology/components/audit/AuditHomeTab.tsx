import { Card, Space, Steps, Tag, Typography } from 'antd'

// ---------------------------------------------------------------------------
// 消费与审计 · 学习引导页签（三段式：功能 / 原理 / 使用说明；去-semantica 化口径）。
// B1 拆分（REQ-145）。
// ---------------------------------------------------------------------------

/** ① 功能：它是什么 / 解决什么 / 不做什么（去-semantica 化后口径） */
const FEATURES: { key: string; title: string; tag: string; color: string; body: string }[] = [
  {
    key: 'what',
    title: '它是什么',
    tag: '定位',
    color: 'blue',
    body: '本体叙事「构建 → 运行 → 消费 → 审计」中的消费与审计环节：把知识库语料抽取为自存 KG（SQLite 实体/关系/claim 三表），并以决策留痕 + PROV-O 溯源审计「数据从哪来、结论由何推导」。',
  },
  {
    key: 'why',
    title: '解决什么',
    tag: '痛点',
    color: 'geekblue',
    body: '黑盒决策不可审计：KG 怎么抽出来的、本体为何这样建、依据哪些原文片段，往往散落在日志里。本栏把「chunk → claim → KG → 本体 → 决策」固化为可查询的溯源链。',
  },
  {
    key: 'not',
    title: '不做什么',
    tag: '边界',
    color: 'default',
    body: '不引入外部语义层运行时（D-O15 反转 D-O10：semantica worker 归档休眠）；不替代 SPARQL 精确查询运行平面；KG 抽取用 REQ-98 LLM 能力代理 + 规则回退，前期够教学，多跳推理留待扩展。',
  },
]

/** ② 原理：GraphRAG 检索三步 + 溯源链 */
const RETRIEVE_STEPS = [
  { title: '向量命中', description: 'query embed → chunk 相似检索（M6 建制）' },
  { title: 'KG 一跳扩展', description: '命中 chunk 的 claim → seed 实体 → 邻接关系' },
  { title: '拼上下文', description: '实体 claim 按分合并 → 命中列表（degraded 回退向量）' },
]

/** ② 原理：数据链路（抽取 → 消费 → 审计回流） */
const PLANE_STEPS = [
  { title: '抽取（graphrag 导入）', description: 'chunks → REQ-98 LLM 抽实体/关系/claim → SQLite 自存表' },
  { title: '消费（本栏图谱/试查）', description: 'KG 浏览 + GraphRAG 试查 + 策略 B/C 构建本体' },
  { title: '审计（决策留痕）', description: '每次抽取/构建落 onto_decision，derived_from 串成溯源链 → PROV-O 导出' },
]

/** ③ 使用说明：四步演练 */
const DRILL_STEPS = [
  { title: '选库', description: '顶部选择知识库（graphrag 模式导入文档时自动抽取 KG）。' },
  { title: '看图谱 / 试查', description: '「KG 图谱」页浏览实体/关系/claim；「GraphRAG 试查」体验向量 + 图混合检索。' },
  { title: '重建 KG', description: 'rag 模式库或抽取失败时，点「重建 KG」显式重抽（LLM 主路径，失败自动回退规则抽取）。' },
  { title: '审计与导出', description: '「决策审计」页看留痕与溯源链，导出 PROV-O Turtle 供外部工具检查。' },
]

export default function AuditHomeTab() {
  return (
    <div className="sema-home">
      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">①</span>
            <span>功能 · 它是什么、解决什么、不做什么</span>
          </Space>
        }
      >
        <div className="sema-grid-3">
          {FEATURES.map((f) => (
            <div className="sema-feature" key={f.key}>
              <div className="sema-feature-head">
                <span className="sema-feature-title">{f.title}</span>
                <Tag color={f.color} style={{ margin: 0 }}>
                  {f.tag}
                </Tag>
              </div>
              <p className="sema-feature-body">{f.body}</p>
            </div>
          ))}
        </div>
      </Card>

      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">②</span>
            <span>原理 · GraphRAG 检索三步、PROV-O 溯源与数据链路</span>
          </Space>
        }
      >
        <div className="onto-sec" style={{ marginTop: 0 }}>
          <span className="onto-sec-title">GraphRAG 检索三步（教学口径）</span>
        </div>
        <Steps size="small" orientation="horizontal" titlePlacement="vertical" responsive={false} items={RETRIEVE_STEPS} />

        <div className="onto-sec">
          <span className="onto-sec-title">PROV-O 溯源机制</span>
        </div>
        <p className="sema-p">
          决策按 W3C PROV-O 语义留痕：<Typography.Text code>Activity</Typography.Text>（一次抽取/构建/手工决策）、
          <Typography.Text code>Entity</Typography.Text>（作用主体 kb/本体）、
          <Typography.Text code>wasDerivedFrom</Typography.Text>（前置决策）。「导出 PROV-O」生成 Turtle 供外部工具检查
        </p>

        <div className="onto-sec">
          <span className="onto-sec-title">数据链路（抽取 → 消费 → 审计回流）</span>
        </div>
        <Steps size="small" orientation="horizontal" titlePlacement="vertical" responsive={false} items={PLANE_STEPS} />
        <p className="sema-p sema-p-muted">
          KG 抽取用 REQ-98 LLM 能力代理（chat.GenerateStructured），失败自动回退规则抽取（「A 是 B」→ IS_A 等句式）；
          M14 非阻断降级语义保留：抽取失败不影响导入与向量检索。
        </p>
      </Card>

      <Card
        size="small"
        className="work-card sema-card"
        title={
          <Space size={8}>
            <span className="sema-card-no">③</span>
            <span>使用说明 · 四步演练</span>
          </Space>
        }
      >
        <Steps size="small" orientation="vertical" items={DRILL_STEPS} />
      </Card>
    </div>
  )
}
