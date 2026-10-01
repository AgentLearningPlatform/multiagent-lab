import {
  ApartmentOutlined,
  CloudServerOutlined,
  GithubOutlined,
  RocketOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { Button, Tag } from 'antd'
import { MODULES, REPO_URL, SEEDS } from '../content'

// ---------------------------------------------------------------------------
// 首页（REQ-238）：项目概览。内容源=README / AGENTS 项目定位 / platform-knowledge/
// 01_整体设计/00_整体设计.md（摘编口径，深入阅读走模块页导读档，避免双源）。
// ---------------------------------------------------------------------------

const CORES: { title: string; desc: string; points: string[] }[] = [
  {
    title: '智能体工程',
    desc: '对话工作台 × 多智能体协作 × 能力增强，过程完全可观测',
    points: [
      '流式对话与调用轨迹面板（工具 / 子智能体 / 检索 / 审批事件全留痕）',
      '多智能体协作：项目工作空间、成员编排、委派与切换事件可见',
      '上下文工程 / Harness 沙箱与审批 / Loop 持久化与续跑（五层视角）',
      '评估驱动质量：agenteval 种子任务集 + LLM Judge 门控',
    ],
  },
  {
    title: '本体工程',
    desc: '构建 → 资产 → 运行 → 消费审计 的完整闭环',
    points: [
      '六条构建路径：手工向导 / AI 对话 / 由知识库构建 / OO 双轨 / OntoExtend 等',
      '资产治理：质量门禁与质量卡、版本与 diff、进化候选受控生长、被引用聚合',
      '多引擎运行平面：Oxigraph / Fuseki，MCP facade（onto_* 工具）供智能体消费',
      '消费与审计观测台：KG 检索来源徽标、决策溯源、PROV-O 导出',
    ],
  },
]

const FEATURES: { icon: React.ReactNode; title: string; desc: string }[] = [
  { icon: <ApartmentOutlined />, title: '知识库 RAG / GraphRAG', desc: '向量检索 + 自研 KG 抽取（LLM 主路径 + 规则回退），本体 TTL 直接装载为 KG 源，与本体双向打通' },
  { icon: <CloudServerOutlined />, title: '外部连接器', desc: 'Kubernetes / SSH / 自定义 MCP 三类连接器，凭据加密托管，agent 级授权白名单' },
  { icon: <SafetyCertificateOutlined />, title: '沙箱执行与审批', desc: 'docker / k8s 执行后端，danger 级工具三档审批、挂起超时与结构化审计事件' },
  { icon: <ThunderboltOutlined />, title: '技能与 MCP 服务化', desc: '技能封装复用，智能体一键暴露为 MCP Server（/mcp Streamable HTTP）' },
  { icon: <RobotOutlined />, title: '平台助手', desc: '内置 AI 助手：平台答疑 / 知识检索 / 配置提案（人工确认后落库），随开随用' },
  { icon: <RocketOutlined />, title: '单机可部署', desc: 'run-dev.sh 一条命令启动全栈（零 venv）；Docker Compose / Helm 交付；SQLite 单文件存储' },
]

const TECH = ['Go', 'CloudWeGo Eino / ADK', 'Hertz', 'SQLite', 'SSE', 'React 18', 'AntD 6', 'Vite', 'three.js', '@xyflow/react']

export default function HomePage() {
  return (
    <main>
      {/* Hero */}
      <section className="hero">
        <div className="page-inner">
          <Tag color="geekblue" style={{ marginBottom: 14 }}>
            开源 · Apache License 2.0
          </Tag>
          <h1>
            AgentLab
            <span className="hero-sub">智能体构建平台</span>
          </h1>
          <p className="hero-desc">
            基于 Go + CloudWeGo Eino/ADK 的智能体构建平台，<strong>单机可部署</strong>。
            <strong>智能体工程</strong>（对话 / 多智能体协作 / 知识·技能·本体增强）与
            <strong>本体工程</strong>（构建 → 资产 → 运行 → 消费审计闭环）双核心，
            内置学习中心与示例本体，能力边界诚实标注。
          </p>
          <div className="hero-actions">
            <Button type="primary" size="large" href="#/demo">
              在线演示 →
            </Button>
            <Button size="large" icon={<GithubOutlined />} href={REPO_URL} target="_blank" rel="noreferrer">
              GitHub 仓库
            </Button>
            <Button type="text" size="large" href="#quickstart">
              快速开始 ↓
            </Button>
          </div>
        </div>
      </section>

      <div className="page-inner">
        {/* 双核心 */}
        <section className="sec">
          <h2>双核心</h2>
          <div className="core-grid">
            {CORES.map((c) => (
              <div className="core-card" key={c.title}>
                <h3>{c.title}</h3>
                <p className="core-desc">{c.desc}</p>
                <ul>
                  {c.points.map((p) => (
                    <li key={p}>{p}</li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        </section>

        {/* 功能模块 */}
        <section className="sec">
          <h2>
            功能模块 <a className="sec-more" href="#/module/agents">查看详细介绍 →</a>
          </h2>
          <div className="mod-grid">
            {MODULES.map((m) => (
              <a className="mod-card" key={m.key} href={`#/module/${m.key}`}>
                <span className="mod-title">{m.title}</span>
                <span className="mod-tagline">{m.tagline}</span>
              </a>
            ))}
          </div>
        </section>

        {/* 特性 */}
        <section className="sec">
          <h2>平台特性</h2>
          <div className="feat-grid">
            {FEATURES.map((f) => (
              <div className="feat-card" key={f.title}>
                <span className="feat-icon">{f.icon}</span>
                <div>
                  <div className="feat-title">{f.title}</div>
                  <div className="feat-desc">{f.desc}</div>
                </div>
              </div>
            ))}
          </div>
        </section>

        {/* 技术栈 */}
        <section className="sec">
          <h2>技术栈</h2>
          <div className="tech-strip">
            {TECH.map((t) => (
              <Tag key={t} style={{ margin: 0, padding: '4px 12px', fontSize: 13 }}>
                {t}
              </Tag>
            ))}
          </div>
        </section>

        {/* 快速开始 */}
        <section className="sec" id="quickstart">
          <h2>快速开始</h2>
          <div className="qs-grid">
            <div className="qs-card">
              <div className="qs-title">一条命令启动（开发/演示）</div>
              <pre className="qs-code">{`git clone https://github.com/<owner>/multiagent-lab.git
cd multiagent-lab
./run-dev.sh
# 打开 http://localhost:8080（前端 :5173 由反代同源承载）`}</pre>
              <p className="qs-note">
                仓库地址见本页右上角「GitHub」链接；零 Python venv 依赖；首次启动自动建库（SQLite）并播种示例本体与学习中心内容包。
              </p>
            </div>
            <div className="qs-card">
              <div className="qs-title">Docker Compose / Helm</div>
              <pre className="qs-code">{`cd deploy
docker compose up -d
# 或 K8s：helm install agentlab deploy/helm`}</pre>
              <p className="qs-note">
                部署形态与远程访问凭证见仓库 <a href={`${REPO_URL}/blob/main/platform-knowledge/01_整体设计/16_部署与运行.md`} target="_blank" rel="noreferrer">《部署与运行》</a>。
              </p>
            </div>
          </div>
          <p className="learn-hint">
            首次使用建议从内置<strong>学习中心</strong>开始：七阶段学习路径 + 方法论卡 + 任务卡打卡，配套{' '}
            {SEEDS.length} 个示例本体（{SEEDS.map((s) => s.spec.name).join(' / ')}）。
          </p>
        </section>
      </div>
    </main>
  )
}
