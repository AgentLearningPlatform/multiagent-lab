import { useEffect, useMemo, useState } from 'react'
import { Alert, Badge, Button, Card, Collapse, Progress, Space, Spin, Tabs, Tag, Typography } from 'antd'
import {
  BookOutlined,
  CheckCircleOutlined,
  CloudServerOutlined,
  RightOutlined,
} from '@ant-design/icons'
import { api } from '../../api/client'
import { useUI } from '../../store/ui'
import { STAGE_DEFS } from './shared'
import PipelinePane from './components/PipelinePane'
import XMarkdown from '@ant-design/x-markdown'
// REQ-109 外部资源导航：仓库随版本维护的内容资产（构建期内联，改 md 即生效无需改代码）
import RESOURCES_MD from '../../../../seeds/learning/external-resources.md?raw'

// ---------------------------------------------------------------------------
// 学习中心（LearnPage，REQ-104 ①，默认页）：本体模块 = 学习各种本体构建、运行方式的模块
//   七阶段学习路径主视图（方法论卡片 + 任务卡打卡 + 工具入口跳转）
//   构建方式对照（五路径卡）| 运行方式对照（引擎差异卡）
//   打卡：checklist `task:` key 空间（REQ-76 机制复用；localStorage P1 尾实现）
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// 方法论卡片（REQ-90 五模块，v0.2 深度版：body 精简骨架 + deep 深度增量；全文见 seeds/learning/methodology/）
// ---------------------------------------------------------------------------

const METHODOLOGY: { key: string; stage: string; title: string; tag: string; body: string; deep: string }[] = [
  {
    key: 'cq',
    stage: 's1',
    title: '能力问题法（Competency Questions）',
    tag: '问题驱动',
    body: '动手建模之前，先回答一个问题："这个本体要帮我回答什么问题？"这些提问就叫能力问题（CQ），是本体工程的第一件武器。好的 CQ 有三个特征：涉及多个概念的关系（"某缺陷源于哪个需求"而不是"缺陷有哪些"）、能用当前领域的词汇回答、答案可以被验证。实践上先写 3~5 条 CQ，再从每条 CQ 里提取名词（候选概念）与动词（候选关系）——CQ 是概念抽取的脚手架，也是日后验收本体的测试用例：建完的本体若答不上最初的 CQ，说明建模跑偏了。',
    deep: 'CQ 分级给建模深度提供客观依据：L1 检索型（只验证概念与属性）、L2 关系型（逼出对象属性，主力档位）、L3 推理型（答案依赖 subClassOf 传导，需带推理的运行方式）、L4 约束型（逼出 disjointWith/基数，进入 OWL 层的信号）。写 CQ 时标注 L 级别：L1~L2 用 RDFS 就够，L3 需要推理方案，L4 才值得引入 OWL 公理。反例：把功能清单当 CQ（"支持导出 Excel"不是 CQ）；CQ 过载（50 条意味着领域边界没划清）；只写不验（推理对照 REQ-94 就是 L3 类 CQ 的验证工具）。',
  },
  {
    key: 'domain',
    stage: 's1',
    title: '领域分析与概念抽取',
    tag: '方法论',
    body: '领域分析的输入是文档、访谈、流程图与既有数据表；输出是一份"候选概念清单 + 关系草案"。三个实用技法：①名词/动词扫描——在需求文本里圈出名词（候选概念或实例）与动词（候选关系）；②上下位追问——对每个候选问"它是什么的一种？"（得到父类）与"它有哪几种？"（得到子类），层次自然浮现；③边界测试——两个概念若属性完全相同则合并，若只在某个属性上不同则考虑保留父子而非平级。注意区分"类"与"实例"：Pod 是类，pod-nginx-7f9 是实例；一个词条在 CQ 里被"逐个列举"时往往是实例。',
    deep: '三条来源路径差异：文档要警惕"流程步骤被误抽为概念"；数据表是现成草案（表→概念、外键→关系），但连接表是多对多关系的物化、不该抽成概念；词表先查重再自造。层次泛滥反例："缺陷→软件缺陷→在线缺陷→支付在线缺陷"——每多一层必须能说出该层独有的属性或关系，说不出就合并。属性 vs 关系的判定：问"这个值以后要不要当查询主体？"——要就是关系（可导航的连接），不要就是属性（描述）。',
  },
  {
    key: 'reuse',
    stage: 's2',
    title: '复用既有词表与顶层本体',
    tag: '复用优先',
    body: '建模前先找现成词表：FOAF（人物与组织）、SKOS（分类体系与主题词表）、schema.org（通用实体）、Dublin Core（文献元数据）、BFO / DOLCE（顶层本体，提供" continuant / occurrent"等最高层区分）。复用的收益是互操作——你的"组织"与外部世界的"组织"对得上号，Agent 检索时能吃到通用语义。实操建议：自建概念若与词表条目语义一致，用注记（如 rdfs:seeAlso 或等价声明）挂接而非重造；顶层本体不必全盘采用，但"物质/过程""抽象/具体"这类最高层区分值得借鉴。本平台的「组织与人员」示例演示了对照 FOAF 术语的复用路径。',
    deep: '三种挂接强度：注记级（rdfs:seeAlso，最弱，语义只是相近时用）、等价级（owl:equivalentClass，最强，语义完全一致且词表可信时用）、子类级（自建 ⊑ 词表概念，中间档，多数场景的最优解）——选错强度是常见事故，等价声明挂在语义相近但不同的概念上，推理结果会悄悄变错。顶层本体的价值不在条目而在"第一刀怎么切"：continuant（设备、人员）/ occurrent（故障、维护）。反例：为复用而复用（别把 schema.org Person 的 30+ 属性搬进来）；skos:Concept ≠ owl:Class（"缺陷类型"用 SKOS 分类，"缺陷"本身用 OWL 类）。',
  },
  {
    key: 'naming',
    stage: 's2',
    title: '命名与 URI 规范',
    tag: '工程规范',
    body: '命名是本体的"公共接口"，坏了最难补救。四条底线：①概念用单数名词（Pod 而非 Pods），关系用动词或动词短语（exposes、belongsTo）；②大小写惯例全库一致（常见 PascalCase 类名 + camelCase 属性名）；③避免缩写歧义——svc 到底是 Service 还是 supervisor？写全称，label 里放别名；④spec_json 的 name 字段是标识符，一旦被关系/实例引用就不要改（改了等于全体引用断裂），显示名放 label。URI 场景（导出 TTL 后）还要保证同一实体全域唯一、可解引用。',
    deep: 'name/label/URI 三层模型各司其职：name 是机器标识符（稳定、永不改）、label 是人类显示名（可多语言可改）、URI 是导出后的全局标识（跨本体引用的凭据）。本平台 spec_json 兼容中文 name，但导出 TTL 后 URI 含非 ASCII 字符、跨工具兼容性下降——工程实践：name 用英文、label 放中文，中文 name 仅用于快速原型。命名空间纪律：前缀唯一且有意义、不与已挂接词表冲突、跨版本 URI 不变（URI 里不要带版本号）。版本 diff（REQ-95）里 changed 集合若出现 name 变更，就是命名纪律失守的信号。',
  },
  {
    key: 'patterns',
    stage: 's3',
    title: '常见 OWL 建模模式',
    tag: '进阶',
    body: '五个高频模式：①子类分层（rdfs:subClassOf）表达"is-a"，继承父类的全部属性与关系约束；②互斥（owl:disjointWith）让矛盾在推理时暴露而非沉默——"故障"与"正常"应互斥；③部分-整体用专门关系（hasPart）而非子类，引擎（Engine）不是车（Car）的子类，是车的组成部分；④属性域/值域（rdfs:domain/rdfs:range）让错误断言可校验；⑤反属性（如 causes / causedBy）成对声明，方便双向查询。在 SPARQL 型方案里这些公理不参与推理（精确匹配优先），带推理的方案（Fuseki/oo）才会让 subClassOf 传导生效——这正是「运行方式对照」要演示的对照点。',
    deep: '每条公理都是一份推理承诺：声明 disjointWith 前想清楚它帮验证什么（对应一条 L4 级 CQ），答不上就别加。适用边界：subClassOf 最安全但多继承让结果难预期（继承链超 3 层该警惕）；domain/range 既是约束也是推断器——推理机会从属性断言反推实例类型（"D1 exposes S1" ⇒ D1 是 Service），这个副作用常被忽略；transitive 属性查询方便但要做环检测。spec_json 与 OWL 层分工：spec_json 表达结构、OWL 公理表达约束与语义细节——先让结构对，再让约束严。同一本体在 Fuseki（OWL-FB 规则）与 oo（OWL-RL）下推理结果可能不同，这本身就是一个值得做的对照实验。',
  },
]

// ---------------------------------------------------------------------------
// 任务卡（REQ-91 结构：阶段/标题/目标/前置/步骤/验收问题/难度；`task:` 打卡）
// ---------------------------------------------------------------------------

interface TaskCardDef {
  id: string
  stage: string
  title: string
  goal: string
  prereq: string[]
  steps: string[]
  acceptance: string
  link: { text: string; sidebar: 'build' | 'assets' | 'runtime' }
  difficulty: number
}

const TASKS: TaskCardDef[] = [
  {
    id: 'task_s1_first_ontology',
    stage: 's1',
    title: '建立第一个本体（三条来源路径各走一遍）',
    goal: '体会 S1 三条来源路径的差异与产物形态',
    prereq: [],
    steps: ['创建内置示例（seed-sample）', '用「AI 创建」生成一个小领域草稿并确认入库', '粘贴一段 TTL 走「导入文件」（观察导入报告与有损警告）'],
    acceptance: '三条路径的产物分别是什么形态？哪条有损、为什么？',
    link: { text: '前往自定义构建 S1', sidebar: 'build' },
    difficulty: 1,
  },
  {
    id: 'task_s2_model_cq',
    stage: 's2',
    title: '从 CQ 到 Spec：为一个问题建模',
    goal: '体验"问题驱动建模"',
    prereq: ['task_s1_first_ontology'],
    steps: ['写下 3 条能力问题', '在 Spec 编辑器为每条 CQ 补概念与关系', '保存并观察版本号递增'],
    acceptance: '你的哪条 CQ 需要跨概念的关系才能回答？',
    link: { text: '前往本体资产编辑', sidebar: 'assets' },
    difficulty: 1,
  },
  {
    id: 'task_s3_break_references',
    stage: 's3',
    title: '故意制造一次校验失败',
    goal: '理解引用完整性校验拦住了什么',
    prereq: ['task_s2_model_cq'],
    steps: ['在 Spec 里让某关系指向未定义的概念', '尝试保存（观察校验错误表）', '修正后重新保存'],
    acceptance: '校验在构建平面还是运行平面执行？为什么不允许保存坏本体？',
    link: { text: '前往本体资产校验', sidebar: 'assets' },
    difficulty: 1,
  },
  {
    id: 'task_s4_read_graph',
    stage: 's4',
    title: '读图：从可视化找一条继承链',
    goal: '用 S4 图谱理解层次结构',
    prereq: ['task_s2_model_cq'],
    steps: ['打开资产详情「可视化」', '找到最深的一条继承链', '点击节点查看实例数徽标'],
    acceptance: '虚线与实线分别代表什么？',
    link: { text: '前往本体资产可视化', sidebar: 'assets' },
    difficulty: 1,
  },
  {
    id: 'task_s5_run_profile',
    stage: 's5',
    title: '把本体部署为运行方案',
    goal: '理解"构建/运行解耦"与显式重载',
    prereq: ['task_s3_break_references'],
    steps: ['在运行栏新建方案（勾选本体）', '启动并确认 running', '回资产栏保存一个新版本，再回方案页执行「重载」'],
    acceptance: '为什么仓库更新后方案不会自动生效？（REQ-87 解耦语义）',
    link: { text: '前往本体运行', sidebar: 'runtime' },
    difficulty: 2,
  },
  {
    id: 'task_s6_facade',
    stage: 's6',
    title: '认识 facade：4 个固定工具',
    goal: '理解统一 MCP 契约对所有运行时类型一致',
    prereq: ['task_s5_run_profile'],
    steps: ['打开方案详情「facade 与工具」', '阅读 4 个 onto_* 工具签名', '展开「翻译透视」看一次调用的 SPARQL 原文'],
    acceptance: 'onto_neighbors 返回什么？翻译透视图里 SPARQL 是谁生成的？',
    link: { text: '前往方案详情', sidebar: 'runtime' },
    difficulty: 2,
  },
  {
    id: 'task_s7_agent_query',
    stage: 's7',
    title: '让 Agent 用本体回答跨概念问题',
    goal: '验证对话挂载与查询全链路',
    prereq: ['task_s6_facade'],
    steps: ['在对话输入框「本体增强」chip 选择 running 方案', '提问一个需要 neighbors 的问题', '在时间线观察工具调用卡片，回方案详情看透视记录'],
    acceptance: 'Agent 调用了哪个工具？方案的哪个端点承接了这次查询？',
    link: { text: '前往方案详情', sidebar: 'runtime' },
    difficulty: 2,
  },
  {
    id: 'task_s5_reload_semantics',
    stage: 's5',
    title: '多方案并存对照',
    goal: '体会同一本体的多套部署视图',
    prereq: ['task_s5_run_profile'],
    steps: ['用不同端口再建一套方案（加载同一本体）', '分别启动', '停止其中一套，确认另一套与普通对话不受影响（NFR-O-2 故障隔离）'],
    acceptance: '两套方案的查询结果一致吗？停止一套后挂载它的对话看到什么提示？',
    link: { text: '前往本体运行', sidebar: 'runtime' },
    difficulty: 2,
  },
]

/** `task:` 打卡空间（localStorage；REQ-75 配置页 P2 交付后迁服务端 pipeline_profile.checklist） */
const CHECKLIST_KEY = 'eino.onto.checklist.tasks'

function readChecklist(): Record<string, string> {
  try {
    return JSON.parse(localStorage.getItem(CHECKLIST_KEY) ?? '{}')
  } catch {
    return {}
  }
}

function writeChecklist(c: Record<string, string>) {
  localStorage.setItem(CHECKLIST_KEY, JSON.stringify(c))
}

const STAGE_COLORS: Record<string, string> = {
  s1: 'purple', s2: 'blue', s3: 'geekblue', s4: 'cyan', s5: 'green', s6: 'orange', s7: 'magenta',
}

// ---------------------------------------------------------------------------
// 构建方式对照（六路径卡，D-O11 分层 + D-O14 第六路径）
// ---------------------------------------------------------------------------

const BUILD_PATH_CARDS: { key: string; title: string; scene: string; points: string; state: { color: string; text: string }; example: string }[] = [
  {
    key: 'custom',
    title: '自定义构建',
    scene: '从零手写 / 导入 / AI 单轮生成',
    points: 'S1~S4 全程亲手操作，理解 spec_json 三要素',
    state: { color: 'green', text: '可用' },
    example: 'K8s 迷你运维（内置示例）',
  },
  {
    key: 'ontochat',
    title: 'OntoChat 流程',
    scene: '对话式多轮引导（CQ → 补全 → 草稿 → 校验 → 入库）',
    points: '对话式知识工程方法（OntoChat 论文三部曲）',
    state: { color: 'green', text: '可用' },
    example: '软件缺陷管理（AI 生成路径演示）',
  },
  {
    key: 'kb',
    title: '由知识库构建',
    scene: 'KB chunk→LLM 抽取 / KG→直转 / 混合（KB→本体构建方向）',
    points: '知识资产复用：语料/图谱 → 语义资产的直接通路（D-O14，O13 已工程化）',
    state: { color: 'orange', text: '部分可用' },
    example: '设备故障知识库 → 设备故障本体（第 5 示例，样例语料随 seeds/learning）',
  },
  {
    key: 'kg',
    title: 'KG 消费流程',
    scene: '自研 KG 图谱 / GraphRAG 试查 / 决策溯源（消费与审计栏承载）',
    points: '精确查询 vs 语义检索的消费面对照（D-O15 去-semantica 化）',
    state: { color: 'cyan', text: '引导' },
    example: '—（消费侧演示）',
  },
  {
    key: 'ontoextend',
    title: 'OntoExtend 流程',
    scene: '对话式扩展现有本体（fork + AI 组合）',
    points: '增量建模与本体演化',
    state: { color: 'cyan', text: '引导先行' },
    example: '—（工程化评估中）',
  },
  {
    key: 'oo',
    title: 'Open Ontologies 流程',
    scene: '双轨 TTL 构建（物化推理 / SHACL / 影响分析）',
    points: '推理型本体工程的完整工具面（39 工具）',
    state: { color: 'cyan', text: '引导+回流' },
    example: '设备故障知识（SKOS 分类+灌装路径演示）',
  },
]

// ---------------------------------------------------------------------------
// 运行方式对照（引擎差异卡；O6 已交付，推理对照已激活）
// ---------------------------------------------------------------------------

const RUNTIME_CARDS: { engine: string; tag: { color: string; text: string };推理: string; points: string; entry: string }[] = [
  {
    engine: 'Oxigraph',
    tag: { color: 'green', text: '可用' },
    推理: '无推理（精确匹配优先）',
    points: 'SPARQL 毫秒级查询；spec_json→TTL 导出后装载；显式重载语义',
    entry: '本体运行 → Oxigraph',
  },
  {
    engine: 'Fuseki',
    tag: { color: 'green', text: '可用' },
    推理: '带推理（RDFS/OWL 规则推理，方案级开关）',
    points: '推理对照（REQ-94）已激活：同本体建两套方案一开一关推理，同一 SPARQL 对照结果差异（如 subClassOf 实例类型传导）；对照实验建议见方案向导',
    entry: '本体运行 → Fuseki',
  },
  {
    engine: 'Open Ontologies',
    tag: { color: 'cyan', text: '引导页' },
    推理: '物化推理（RDFS/OWL-RL 建库即物化）',
    points: '39 工具 MCP；SHACL 校验；变更影响分析；双轨数据不进主线仓库',
    entry: '本体运行 → Open Ontologies',
  },
]

export default function LearnPage() {
  const { showToast } = useUI()
  const [checklist, setChecklist] = useState<Record<string, string>>(readChecklist)
  const [learningExamples, setLearningExamples] = useState<{ key: string; name: string; description: string }[] | null>(null)

  useEffect(() => {
    api
      .listLearningExamples()
      .then(setLearningExamples)
      .catch(() => setLearningExamples([]))
  }, [])

  const toggleTask = (id: string) => {
    const next = { ...checklist }
    if (next[id]) delete next[id]
    else next[id] = new Date().toISOString()
    setChecklist(next)
    writeChecklist(next)
  }

  /** 按阶段聚合任务完成度（REQ-91 ④） */
  const stageProgress = useMemo(() => {
    const m = new Map<string, { total: number; done: number }>()
    for (const t of TASKS) {
      const e = m.get(t.stage) ?? { total: 0, done: 0 }
      e.total += 1
      if (checklist[t.id]) e.done += 1
      m.set(t.stage, e)
    }
    return m
  }, [checklist])

  const totalDone = TASKS.filter((t) => checklist[t.id]).length
  const goSidebar = (key: string) => {
    localStorage.setItem('eino.onto.sidebar', key)
    window.dispatchEvent(new CustomEvent('onto-sidebar-change'))
  }

  const taskByStage = (stage: string) => TASKS.filter((t) => t.stage === stage)

  const stagePanels = STAGE_DEFS.map((s, i) => {
    const prog = stageProgress.get(s.key) ?? { total: 0, done: 0 }
    const methods = METHODOLOGY.filter((m) => m.stage === s.key)
    const isBuildStage = i <= 3
    return {
      key: s.key,
      label: (
        <Space size={8} wrap>
          <Tag color={STAGE_COLORS[s.key]} style={{ margin: 0 }}>{s.key.toUpperCase()}</Tag>
          <span>{s.short}</span>
          {prog.total > 0 && (
            <Badge
              count={`${prog.done}/${prog.total}`}
              style={{ backgroundColor: prog.done === prog.total ? '#16a34a' : '#8b91a7' }}
            />
          )}
          {!isBuildStage && <Tag style={{ margin: 0, fontSize: 10 }}>运行段 · 见运行栏</Tag>}
        </Space>
      ),
      children: (
        <div className="onto-learn-stage">
          {methods.map((m) => (
            <Collapse
              key={m.key}
              size="small"
              className="onto-learn-method"
              items={[{
                key: m.key,
                label: (
                  <Space size={6} wrap>
                    <BookOutlined style={{ color: 'var(--c-brand)' }} />
                    <span style={{ fontWeight: 600 }}>{m.title}</span>
                    <Tag color="blue" style={{ margin: 0 }}>{m.tag}</Tag>
                    <Typography.Text type="secondary" style={{ fontSize: 11 }}>方法论卡片 · REQ-90</Typography.Text>
                  </Space>
                ),
                children: (
                  <>
                    <p className="onto-learn-method-body">{m.body}</p>
                    <Collapse
                      size="small"
                      ghost
                      items={[
                        {
                          key: 'deep',
                          label: <Typography.Text type="secondary" style={{ fontSize: 12 }}>深度版（REQ-90 P2 补齐）</Typography.Text>,
                          children: <p className="onto-learn-method-body" style={{ fontSize: 12 }}>{m.deep}</p>,
                        },
                      ]}
                    />
                  </>
                ),
              }]}
            />
          ))}
          {methods.length === 0 && (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>本阶段方法论卡片随 P2 深度版补齐。</Typography.Text>
          )}
          {prog.total > 0 && (
            <div className="onto-learn-tasks">
              <div className="onto-learn-tasks-head">
                <span>任务卡（{prog.done}/{prog.total} 已完成）</span>
                <Progress percent={Math.round((prog.done / prog.total) * 100)} size="small" style={{ width: 120, margin: 0 }} showInfo={false} />
              </div>
              {taskByStage(s.key).map((t) => {
                const done = !!checklist[t.id]
                const prereqOk = t.prereq.every((p) => checklist[p])
                return (
                  <div key={t.id} className={`onto-task-card${done ? ' done' : ''}`}>
                    <div className="onto-task-head">
                      <Button
                        size="small"
                        type={done ? 'primary' : 'default'}
                        icon={<CheckCircleOutlined />}
                        disabled={!prereqOk && !done}
                        title={!prereqOk && !done ? `前置任务未完成：${t.prereq.join(', ')}` : undefined}
                        onClick={() => toggleTask(t.id)}
                      >
                        {done ? '已完成' : '打卡'}
                      </Button>
                      <span className="onto-task-title">{t.title}</span>
                      <Tag style={{ margin: 0 }}>难度 {t.difficulty}</Tag>
                    </div>
                    <div className="onto-task-body">
                      <p><b>目标：</b>{t.goal}</p>
                      <ol>
                        {t.steps.map((st, j) => (
                          <li key={j}>{st}</li>
                        ))}
                      </ol>
                      <p className="onto-task-accept"><b>验收问题：</b>{t.acceptance}</p>
                      <Button size="small" type="link" icon={<RightOutlined />} onClick={() => goSidebar(t.link.sidebar)}>
                        {t.link.text}
                      </Button>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      ),
    }
  })

  return (
    <div className="work-main">
      <div className="work-head">
        <div className="work-head-text">
          <div className="work-head-title">
            <Typography.Title level={4} style={{ margin: 0 }}>
              学习中心
            </Typography.Title>
            <Tag color="blue" style={{ margin: 0 }}>默认页</Tag>
            <Tag color="geekblue" style={{ margin: 0 }}>任务进度 {totalDone}/{TASKS.length}</Tag>
          </div>
          <p className="work-head-desc">
            七阶段学习路径（REQ-74 理论骨架）：每阶段有方法论卡片（REQ-90）与动手任务卡（REQ-91，`task:` 打卡）；学完的路径产物进「本体构建」，部署进「本体运行」。
          </p>
        </div>
      </div>

      <Card className="work-card" size="small" title="七阶段学习路径（S1 → S7）">
        <Collapse
          defaultActiveKey={['s1']}
          items={stagePanels}
        />
      </Card>

      <Tabs
        items={[
          {
            key: 'build-paths',
            label: '构建方式对照',
            children: (
              <div className="onto-learn-cards">
                {BUILD_PATH_CARDS.map((c) => (
                  <div className="onto-learn-card" key={c.key}>
                    <div className="onto-learn-card-head">
                      <span className="onto-learn-card-title">{c.title}</span>
                      <Tag color={c.state.color} style={{ margin: 0 }}>{c.state.text}</Tag>
                    </div>
                    <p className="onto-learn-card-line"><b>适用场景：</b>{c.scene}</p>
                    <p className="onto-learn-card-line"><b>学习要点：</b>{c.points}</p>
                    <p className="onto-learn-card-line"><b>关联示例：</b>{c.example}</p>
                    <Button size="small" type="link" icon={<RightOutlined />} onClick={() => { localStorage.setItem('eino.onto.buildPath', c.key); goSidebar('build') }}>
                      进入该路径
                    </Button>
                  </div>
                ))}
              </div>
            ),
          },
          {
            key: 'runtime-paths',
            label: '运行方式对照',
            children: (
              <>
                <Alert
                  type="success"
                  showIcon
                  style={{ marginBottom: 10 }}
                  message="同一份本体在不同引擎下行为不同——这是本体运行环节的核心学习点"
                  description="推理对照已随 Fuseki（O6）激活：对同一本体建两套 Fuseki 方案（推理一开一关），同一 SPARQL 并行发往两套方案对照结果差异（推荐用 subClassOf 子类实例验证类型传导）。"
                />
                <div className="onto-learn-cards">
                  {RUNTIME_CARDS.map((c) => (
                    <div className="onto-learn-card" key={c.engine}>
                      <div className="onto-learn-card-head">
                        <span className="onto-learn-card-title">
                          <CloudServerOutlined style={{ marginRight: 6, color: 'var(--c-brand)' }} />
                          {c.engine}
                        </span>
                        <Tag color={c.tag.color} style={{ margin: 0 }}>{c.tag.text}</Tag>
                      </div>
                      <p className="onto-learn-card-line"><b>推理能力：</b>{c.推理}</p>
                      <p className="onto-learn-card-line"><b>学习要点：</b>{c.points}</p>
                      <p className="onto-learn-card-line"><b>入口：</b>{c.entry}</p>
                      <Button size="small" type="link" icon={<RightOutlined />} onClick={() => goSidebar('runtime')}>
                        前往运行栏
                      </Button>
                    </div>
                  ))}
                </div>
              </>
            ),
          },
          {
            key: 'examples',
            label: '示例本体库',
            children: learningExamples === null ? (
              <Spin size="small" />
            ) : learningExamples.length === 0 ? (
              <Typography.Text type="secondary">构建平面未返回学习示例（需 ontology-service 就绪）。</Typography.Text>
            ) : (
              <div className="onto-learn-cards">
                {learningExamples.map((le) => (
                  <div className="onto-learn-card" key={le.key}>
                    <div className="onto-learn-card-head">
                      <span className="onto-learn-card-title">{le.name}</span>
                      <Tag style={{ margin: 0 }}>{le.key}</Tag>
                    </div>
                    <p className="onto-learn-card-line">{le.description}</p>
                    <Button
                      size="small"
                      type="primary"
                      ghost
                      onClick={() => {
                        api
                          .seedLearningExample(le.key)
                          .then((o) => {
                            showToast((o as unknown as { seeded?: boolean }).seeded === false ? '该学习示例已存在' : `学习示例「${o.name}」已创建`)
                          })
                          .catch((e: any) => showToast(e.message, 'err'))
                      }}
                    >
                      一键灌装到资产
                    </Button>
                  </div>
                ))}
                <Alert type="success" showIcon message="4 示例本体已齐（REQ-91 ③ 交付）" description="覆盖构建路径：K8s 迷你运维（手写，seed-sample）、软件缺陷管理（AI 生成）、组织与人员（复用对照 FOAF）、设备故障知识（分类+灌装路径）。每份示例附建模说明（seeds/learning/examples/*.README.md：背景/CQ/决策记录/局限）。第 5 示例「设备故障知识库→设备故障本体」随 O13（KB 构建路径）。" />
              </div>
            ),
          },
          {
            key: 'pipeline',
            label: '工具链配置',
            children: <PipelinePane />,
          },
          {
            key: 'external-resources',
            label: '外部资源',
            children: (
              <>
                <Alert
                  type="info"
                  showIcon
                  style={{ marginBottom: 10 }}
                  message="可直接访问的平台与典型开源项目导航（REQ-109）"
                  description="内容由仓库随版本维护的 seeds/learning/external-resources.md 渲染——编辑该文件并提交后即生效（构建期内联，无需改代码）。全局台账与登记状态见 docs/15_开源项目及论文登记簿.md。"
                />
                <div className="onto-learn-extres">
                  <XMarkdown content={RESOURCES_MD} openLinksInNewTab />
                </div>
              </>
            ),
          },
        ]}
        style={{ marginTop: 2 }}
      />
    </div>
  )
}
