# AGENTS.md — AI 编码助手项目引导

> 本文件面向 AI 编码工具（zcode / Claude Code / Cursor 等）。人类用户请看 README.md。

## 项目定位

「学习智能体构建和本体构建的 Web 平台」——Go + CloudWeGo Eino/ADK 迷你版 Coze，五业务模块 + 平台知识 + 设置（智能体 / 项目 / 本体 / 知识库 / 技能 / 📖平台知识 REQ-116/161 / ⚙设置；OpenOntologies 双轨经本体模块入口承载，REQ-111）。定位是**学习平台**，双主线：A 线多智能体工程（01 LG-1~16）+ B 线本体工程（03），能力边界诚实标注（如 KG 增强检索「前期够教学即可」）。

## 仓库结构

- `backend/` 主平台后端（Hertz :8080，Eino ADK 编排、SQLite、SSE 流式）
- `ontology-service/` 本体构建平面（:8091，spec_json 多形态资产、LLM 辅助创建、自研 KG：SQLite 三表 + PROV-O 导出）
- `runtime-manager/` 本体运行平面（:8090，多引擎方案管理：Oxigraph / Fuseki）
- `web/` React 前端（Vite + AntD；本体模块五栏 IA：学习中心 / 构建 / 资产 / 运行 / 消费与审计）
- `tools/` 工具脚本（`tools/semantica-worker/` 已归档休眠——代码保留、**勿重新启用**，D-O15）
- `deploy/` Docker Compose + Helm；`seeds/` 种子数据与学习中心内容包（学习包/方法论/外部资源单源）
- `docs/` 项目文档事实源（开发过程辅助活文档）；`research/` 立项前调研依据；`platform-knowledge/` 平台知识（产品设计/技术原理/模块导读，按模块子目录组织，REQ-161）

## 开发前必读（按需读，不必全读；版本号不在此 pin，以各文档头部为准）

| 文档 | 内容 |
|---|---|
| `docs/18_REQ编号注册表.md` | **REQ 编号唯一分配权威**——新需求立项先查此表；含 90~96 冲突裁决（本体学习增强保留 90~96；智能体侧技能=REQ-120、挂载=121、沙箱=122、Agent 级配置=123、折叠树=124、项目文件=125、产物=126）与文档编号约定 |
| `docs/01_智能体_需求文档_PRD.md` | 智能体平台**唯一需求事实源**：REQ 全表 + 决策 Q-1~20 + 验收（§6）+ 需求池（§9，含触发条件） |
| `docs/03_本体_需求文档.md` | 本体模块**唯一需求事实源**：REQ 全表 + 决策记录 D-O1~16（§5）+ 验收要点（§7）+ 迭代记录（§8） |
| `platform-knowledge/产品设计/17_产品_信息架构与界面设计.md` | **产品/界面口径事实源**（2026-09-25 自 docs/ 迁入平台知识）：北极星与产品原则、五栏/导航 IA、构建路径分类学、用户旅程 J1~J4、REQ-111/112 |
| `docs/04_本体_方案设计.md` | 本体模块方案设计（需求与方案分离） |
| `docs/02_智能体_技术方案设计.md` | 智能体平台技术方案、API 面、里程碑（§12 含状态列） |
| `docs/12_知识库_方案设计.md` / `docs/11_知识库_需求文档.md` | KB 模块（RAG/GraphRAG 双子模块，KG 自研内置 backend） |
| `docs/14_本体_前端改造方案.md` | 前端结构与改造史（当前态以此为准） |
| `docs/16_部署与运行.md` | 本地 / Docker / K8s 三条部署路径 |
| `docs/20_回归冒烟清单.md` | **交付质量资产**：每个独立任务交付后按清单跑核心动线（纪律 #5 的落地载体） |
| `docs/15_开源项目及论文登记簿.md` | 全局台账：**新引入开源项目/论文必须先登记**（`platform-knowledge/本体/19_本体_semantica集成方案.md` 为其存档，原 15 号） |
| `docs/07` 工具链 / `docs/08` 自研边界 / `docs/13` 学习缺口分析 | 决策依据 |
| `research/` | 13 份立项前调研——各决策的 why（为何选 Eino、为何不自研 GraphRAG 等），映射表见 `research/README.md` |
| `platform-knowledge/` | **平台知识目录**（REQ-161）：产品设计/技术原理/调研研究 + 各模块学习导读，按模块子目录组织（本体/智能体/知识库/产品设计等，不平铺）；**目录结构即平台知识页结构**；维护约定见 `platform-knowledge/README.md` |

## 硬性纪律

1. **需求先行**：新需求/需求变更先在对应需求档落档（01/03/11 按归属；编号先查 `docs/18` 注册表；版本递增 + 迭代表留痕），再实现；实现交付后回写相关文档（回写前先读各文档头部最新版本号，避免撞号）。
2. **git**：只 add 本次实际修改的文件路径（禁止 `git add .` / `git add -A`）；commit message 概述变更要点；commit 后 push origin/main。**任务边界即提交**：每完成一个独立任务、或开始新独立任务前，不留未提交改动。
3. **权威 = origin/main**；本仓库（macOS 正身）是唯一开发地，云盘副本只读。
4. **自研边界**：开源优先、避免重复造轮子（D-O5）；**不接入 Python 运行时依赖**（D-O15：`run-dev.sh` 一条命令启动零 venv，验收 22）。
5. **验收**以 03 §7 / 01 §6 验收要点为准；前端改动用 headless Chrome（CDP）按 **`docs/20_回归冒烟清单`** 冒烟验证（选改动面对应模块节；★ 主链路建议全跑）。
6. **活文档**：已确认需求不删除，变更标注 `（已变更/已废弃，见 vX.X）`；文档正文与实现不符时，以实现为准并回修文档。
7. **知识同步**：需求/方案档发生**语义级变更**（新增/变更 REQ 行、新增/反转 D-* 决策、口径退役）的同一轮交付里，必须同步更新 `sources` 命中的 `platform-knowledge/` 模块导读页（每篇头部有源指针 frontmatter；原 seeds/learning/reference/，2026-09-25 迁入）；纯笔误/版本递增不触发。知识分层职责见 17 §1.4，页面渲染检查在 20 号冒烟清单 S1。

## 当前状态（2026-09-26，由协作 Agent 维护）

- **M28 P2a+P2b 已交付（2026-09-26）——伴生图检索接入 + 前端「伴生本体」页签，动态价值链真机打通**：①REQ-151 graph 参数扩展：facade sparql_query 增 graph=companion + conversation_id，转发伴生引擎并以 SPARQL 协议标准参数 **default-graph-uri 零改写限定会话图**（实测 oxigraph 0.5.11 支持；SELECT 白名单/透视/limit 照旧；COMPANION_GRAPH_ENDPOINT 可配；guide 注入文案同步）；②资产栏第九 Tab「伴生本体」（「对话」来源徽标，D-O19 第三来源）——api/companion.ts 独立模块（client.ts 含主人 M27 WIP 按文件避让）+ CompanionPane（会话选择/状态卡 引擎-待确认-游标-实体标签/候选三桶确认流/整体摘除，失败 LoadErrorAlert 可重试）；**真机全链验证**：注入候选→页签确认入图→伴生引擎懒启动（:9199）→labels 回显→MCP tools/call graph=companion 返回图内实体（带 graph 字段）——「对话→候选→确认→agent 可查」打通；**坑：backend 进程 cwd=backend/，伴生引擎二进制候选序补 ../data/bin**。**M28 剩余（P2 尾）**：Agent 配置页开关 UI（AgentSidePanel 含主人 M27 WIP，待提交后补）/ REQ-154 3d-force 成长可视化 / KG 检索源并入；LLM 真实抽取随自然对话覆盖。03 v0.38/02 v0.73/20 v1.22/18 v1.34/方案 v2.2；截图 smoke/m28p2/。
- **M28 首项 REQ-170 P1 伴生本体最小闭环已交付（2026-09-26，主人指令"开始执行伴生本体需求开发"授权）**：迁移 021（agent.companion_ontology 开关**默认关** + companion_candidate 候选表 + companion_cursor 会话游标）；backend/internal/companion **旁路包**——worker（Run/Resume 收尾非阻塞触发 + message 表游标续抽，chat.GenerateStructured 复用 REQ-98 抽取）+ 伴生图引擎（**独立 Oxigraph 实例** data/companion-graph/ :9199 懒启动，二进制候选序复用 REQ-146）+ SPARQL 纯函数层（种子 5 骨架类幂等预置入**全局默认图**、实体 slug 同名归并、关系边=bot:Relation 实例节点、**矛盾旧边 bot:invalidAt 失效化而非删除**、DROP GRAPH 整体摘除）；**API 先行五端点**（GET candidates / POST confirm 入图+失效化 / POST reject / GET status / POST conversations/{id}/reset，server.go 路由）；SSE 契约 0 改动（低侵入三原则达成）。测试：零依赖单测 4 组 + **真机冒烟**（本机 oxigraph 0.5.11 独立端口 9198：懒启动→schema→概念/关系入图→失效化→摘除全链过）；**坑：oxigraph UPDATE 端点与查询分离（/update vs /query）；测试残留引擎进程指向已清数据目录报 IO error——固定冒烟目录+前置 pkill**。**待领取（P2）**：前端「伴生」页签+Agent 配置开关 UI / REQ-151 graph 参数扩展 / 3d-force 成长可视化 / KG 检索源并入；E2E（真实 LLM 抽取）待 dev 栈下次重启实测（本轮未重启运行中主栈——工作树含主人 M27 WIP）。03 v0.37 §2.13/02 v0.72 §12 M28 立项/20 v1.21 S4.11/18 v1.33。
- **M22 二批已交付（2026-09-26）——REQ-145 前端体验优化 P2 五项（A3/A5/B2/B3/B5），M22 A/B 类全清**：A3 版本文本对照（ontology-service 新增 `GET /api/ontologies/{id}/versions/{version}/spec` 快照端点（复用 repo.GetVersionSpec；rest 级 httptest 三例）+ VersionDiff「文本对照」页签——两版 spec 归一格式化后 react-diff-viewer-continued 4.4 分栏对照，15 号 v2.11 登记）；A5 Skeleton 规范（口径：内容占位 Skeleton/过程进行中 Spin；本体模块 6 处内容占位落地，agent 侧顺延）；B2 无障碍（icon-only 按钮 aria-label 补齐 + GraphEditor 画布 role="application"）；B3 决策表操作列 fixed（AntD 6 类名 fix-end）+ TraceTable/CSV 草稿表 scroll.x；B5 共享 LoadErrorAlert 可重试错误卡（KnowledgePage 侧栏/OntoChat 会话列表/RuntimePage；停服实测错误卡→重试→恢复闭环）。**B4 样式 token 化（P3）与 C 类（C1 KnowledgePage 798 行/C2 ChatWindow 1365 行拆分、C3 分页规范）留独立批次评估**。冒烟：20 号 S4.10 新增 + S1.1/S4.9 回归全过，截图 smoke/req145-b2/；14 v0.21/03 v0.36/02 v0.71/20 v1.20/18 v1.32。**交付注记：工作树含主人 REQ-167 WIP（AIOptimizeButton/assistant 后端四件，tsc 半成品错误），本批构建验证经 git worktree 干净树执行、提交严格避让 WIP 文件；其间 dev 栈因并行会话 run-dev.sh 与旧 backend 端口竞争整体退出，已按 run-dev.sh 标准恢复（8080/8090/8091 全活）**。
- **M22 一批已交付（2026-09-26）——REQ-145 前端体验优化 P1 四项（A1/A2/A4/B1）**：A1 Spec 编辑 + A2 GraphEditor 实例属性（选中面板/添加实例弹窗）统一 CodeMirror JSON（JsonEditor 共享组件：语法高亮 + jsonParseLinter 行内错误标记 + 防崩 try/catch 保留）；A4 claim 溯源表 AntD 6 virtual + 决策/claim 表 pageSize 统一 10；B1 三大文件拆分 AssetsPage 329/GraphEditor 353/AuditPage 141 全 ≤400 行（assets/ 三件、graph-editor/ 五件、audit/ 四页签）；**坑沉淀：@uiw/react-codemirror 4.25 value 受控同步在打字锁挂起期吞外部值更新（重新加载/切换本体编辑器不重置），JsonEditor 改显式所有权同步（onCreateEditor 持 view + 显式变更 dispatch 替换）**；@codemirror/lang-json+lint 新依赖登记 15 号 v2.10（顺补登 @uiw/react-codemirror）；冒烟 S1.1/S4.1/S4.2/S4.4 全过 + 截图 smoke/req145/；14 v0.20/02 v0.70/03 v0.35/20 v1.19 S4.9/18 v1.31。
- **M19 阶段三 REQ-150 对比界面系统优化已交付（2026-09-26）——M19 全项完成**：代码 f280135（2026-09-25 主人本地先行，选项承载迁窗格头四徽标/共享区 chips 收起为摘要/Splitter 可拖宽）+ 本轮收尾——窗格头状态徽标五态（待提问/生成中脉冲/完成/出错/已停止）+ **顺修停止断流缺陷**（abort 后 run.finished 收不到致流式游标永挂，stopFlag 标记+收尾兜底置终态，单路/对比两路同修）；CDP 冒烟全过（模型覆盖 flashx 生效/SC-10 双 Agent 同问对照/停止→已停止/拖宽 580→660/刷新配置持久化）+ 截图 smoke/req150/；01 v0.49/02 v0.68（M19 状态列收口+M10 行顺修）/17 v0.20/20 v1.18 S2.7/18 v1.30。
- **M10 阶段 10a+10b 代码与交付回写均已完成（10b 回写 2026-09-26 领取）**：10a 实测交付（96210e6，2026-09-25，主人装 Docker 后领取）；**10b 沙箱生命周期可见化+资源限制参数化代码已交付（9b0d45c，迁移 020——此前 02 §12 误记"进行中"，v0.68 已顺修），交付回写本轮领取完成：01 v0.50（REQ-122 §3.3 行去「调研完成待领取」+ §3.7 补交付注记）/ 17 v0.21（LG-15 行去「M10 未激活」+ §4.3 实现注记）/ 02 v0.69（M10 行收口「已交付含文档回写」）；20 号 S2.8 的 10b 检查项经核对已在 v1.12 随交付当日并入（无缺）**。待领取：10c per-run 模式+Podman（可选）→ 10d K8s Pod 后端（P2；备选运行时：Agent Substrate / K8s Agent Sandbox）→ 10e gVisor/Kata 叠加（可选）→ **10f 沙箱环境分级与执行沙箱选型（REQ-159）**。
- **M10 阶段 10a 最小闭环实测已交付（96210e6，2026-09-25，主人装 Docker 后领取）**：agentd 镜像构建 + SANDBOX_IMAGE 全链实测跑通（manifest/SSE 透传/落库/还原/容器复用），实测修复五问题（端口错配/docker CLI 回退/tool_approval NOT NULL/runDocker 信封解包/agentd 错误静默）+ debug 沙箱透传断链补齐；02 v0.60/20 v1.11 S2.8。10b 状态见上条。
- **M19 对话对比扩展全项完成：阶段一 REQ-143 跨智能体窗格（df25e82，01 v0.41/02 v0.58/20 v1.9）+ 阶段二 REQ-144 配置剖面与复用（f1fdc40，01 v0.42/02 v0.59/20 v1.10，2026-09-25）**：窗格级 Agent/模型/库/方案/温度/提示词改写/技能开关覆盖、不携带历史开关、剖面对话级命名保存与应用（合并序明示）、复制上一窗格、采纳转正（agent 直聊）；窗格级守卫（docker/外部 CLI 独立报错）；顺修 research 审查 B1/B3 两 P0。REQ-150（选项承载迁窗格头）待后续。

- **M18 WIP 已提交待续（b5de3ac，REQ-131/132/134）**：/mcp 端点链（initialize→tools/list 通、Bearer 401/200）+ AgentSidePanel 四分类页签 + Modal 分级已交付；~~阻塞：工具执行环回归~~（**2026-09-25 核验：HEAD 复测未复现**——tool.call→tool.result→二轮续跑→completed，疑为其提交前中途 WIP 态，见 02 v0.55，阻塞解除）；REQ-134 pro-components 未引入；/mcp tools/call 成功路径待回归。
- **本会话已转为文档线**（方案/需求文档修改），不再承担开发任务。
- **REQ-146/147 本体运行平面增强已交付（824b063，2026-09-25，03 v0.26/04 v0.22/14 v0.17/20 v1.6 S4.5/S4.6）**：①引擎自检与一键安装——GET /api/engines（oxigraph 候选序 OXIGRAPH_BIN→PATH→data/bin→tools/bin；fuseki 仅手动指引）+ POST install 异步任务（官方 release pin v0.5.11 按 GOOS/GOARCH 映射；downloadTo 原子落盘；oxigraph Start 动态解析装后免重启 runtimed）；运行页引擎缺失显红 Tag + 预检 Alert + 一键安装按钮 + 方案卡缺失标记；②方案停止态编辑——stopped/created/error 方案卡「编辑」改名称与本体集合（既有 PUT 前端入口补齐），running 置灰，引擎/端口只读。环境注：GLM 默认连接 429 余额不足首轮即 run.error（勿误判回归，测试绑可用连接）；沙箱 CDN 受限致 oxigraph 真实下载未在本机走通（机制经 httptest 单测 + 桩二进制动态解析验证），真机可用。
- **REQ-19e/19f 对话对比模式已实现交付（2026-09-24，01 v0.37/02 v0.54）**：P1 基座补齐——头部「对比」开关 + 2~4 列等分窗格 + 窗格独立配置（模型/知识库/运行方案，空=继承）+ 一次提问 N 路（用户消息组级单条/历史快照共享/独立 runID 装配/助手消息 meta）/组级停止；SSE 契约不变；边界：docker/外部 CLI 后端不支持对比。**勘误：02 §6.19 前提"已实现"系误记，实至今日交付**。M19（REQ-143/144）自此有基座可增量开发。
- **18 号 v1.13 治理**：REQ-131 双分配修复——NFR-O-6 前端体验优化改配 REQ-145（131 唯一语义 = Agent 对外 MCP 服务化，M18）。
- **REQ-135~142「方案 only」批次已实现交付（2026-09-24，主人授权转入开发；01 v0.35）**：REQ-135 配置分级/REQ-136 自动命名/REQ-137 logo/REQ-139 卡片优化/REQ-140 文档查看（代码）；REQ-141 调研（21 号）/REQ-142 研究（22 号）文档落档；REQ-143/144（对话对比扩展）已排期 M19（02 v0.53）。
- **规划治理完成（2026-09-23）**：需求编号全局注册表（18 号）+ 产品横切需求 REQ-111~115 立项（01 §3.9）+ 里程碑状态列（02 §12）+ 冒烟清单资产化（20 号）+ 17 号产品文档 v0.2（双主线定位、J4 旅程、三开放问题关闭→D-O16）。
- 智能体侧：M12 全项收尾（02 v0.34 模型预设 + v0.35 Git 深度版）；项目绑定本地目录 bugfix 交付（02 v0.33）；REQ-14(恢复/审批) 中断恢复与工具审批已交付（02 v0.38/0.39）；**REQ-113 数据导出与生命周期已交付（02 v0.40）；REQ-114①/112/115 产品横切已交付（01 v0.26，17 v0.6）；REQ-111~116 全部落地**；**M17 阶段一（REQ-117 调试模式，主人交付）+ 阶段二已交付（02 v0.45：CLI 原始输出透出/调试事件入库开关/事件流导出与重放）**；**REQ-133 本地目录改造已交付（01 v0.31/02 v0.50：validate-dir 分字段直连 format_ok/reachable + DirCheckResult 共享组件 + 同机 pick-dir 三平台系统对话框；跨运行时绝对路径判定 fsutil.IsAbsDir）**。REQ-131/132/134 已立项方案 only 未排期（131 Agent 对外 MCP 服务化 / 132+134 配置页组件化改版，合并实施）。
- 知识库侧：**M16 知识图谱增强已立项排期**（REQ-127~130：图谱浏览/多跳检索/抽取治理/社区摘要全局问答；11 v0.6 + 12 v0.9 §3.5 + 02 §12 M16）——**M16 全项完成：阶段一 REQ-127/128（02 v0.41）+ 阶段二 REQ-129 抽取治理（02 v0.42）+ REQ-130 社区摘要与全局问答（02 v0.43）**。
- 本体侧：D-O15/REQ-110 已交付（验收 22 达成），待主人验收第五栏「消费与审计」；REQ-71 v1.5 实例入画布已交付（03 v0.19）。
- **待领取（已排期 M24，PoC 先行）**：REQ-160 DeepSeek Harness 推理后端适配器（M13 cliAdapter 第 4 例；dsh headless 契约 PoC 为 go/no-go 门；Minimal 裁剪+锁版本）——research/DeepSeek_Harness接入可行性_20260925.md。
- **部分交付（2026-09-25 结构基座）**：REQ-161 平台知识——模块更名（参考资料→平台知识）+ 文档体系整理（docs/ 留开发辅助活文档；产品设计/技术原理/研究 7 篇迁 `platform-knowledge/` 按模块子目录；学习导读 8 篇同步迁入；互引相对路径化）+ 页面目录驱动二级主题树 + 外部资源主题页挂载；内容扩充首批矩阵（14+ 主题页）与构建验证待续（**主人本地开发**，01 v0.48/17 v0.19/18 v1.27/20 v1.15）；REQ-169 互引点击→右侧 Drawer 阅读基座已含（后端白名单+前端解析，主人本地开发）。
- **待领取（REQ-169，主人本地开发）**：平台知识文档互引阅读——文档互引用相对路径；平台知识页正文/导读点击相对路径链接→右侧 Drawer（DocViewerModal）阅读文件内容；后端 docRead 只读白名单扩 platform-knowledge/。
- **待领取（REQ-170，方案 only 待排期）**：智能体运行时动态薄本体——报告轻量档 × D-O15 自研栈旁路落地（伴生 worker 游标抽取/候选人工确认/Oxigraph 会话图 named graph 隔离/种子 5 骨架类/失效化）；对话主链路 0 改动、开关默认关；展示走 D-O19 第三来源「对话」资产栏独立页签；方案 platform-knowledge/智能体/动态薄本体_可行方案.md（03 v0.31/18 v1.28）。
- **待领取（立项待开发）**：REQ-162 学习中心七阶段方块化+外部资源迁平台知识 / REQ-163 消费与审计 KG 边界厘清（D-O19）/ REQ-164 智能体配置补技能 UI+后端移基本分区 / REQ-165 推理后端官方 Logo 自动匹配——2026-09-25 体验细节四批次，18 v1.25。
- **待领取（已排期 M27）**：REQ-166 内置系统级 Agent「平台助手」（设置页配置界面）+ REQ-167 AI 内容优化按钮（复杂输入框通用组件）——01 v0.47；REQ-168 悬浮泡泡 P3 预留。
- ~~待领取（本期排期）M21 本体可视化~~（✅ VIZ-1+VIZ-3 已交付 8d25e17，2026-09-26——Graph3D 三维浏览 Tab（3d-force-graph UMD，聚焦飞入/邻居高亮/搜索过滤/侧栏）+ WebVOWL 对照激活（诚实降级）；03 v0.32/14 v0.18/20 v1.14 S4.5；**坑：3d-force-graph 1.80 必须 new 调用**；VIZ-2/VIZ-4 候选池）。
- **M20 首项 REQ-151 已交付（2026-09-26）**：facade 第 5 工具 `sparql_query`（Q-14 契约 4→5，D-O17）——词法级 SELECT 白名单（字面量/IRI 不误伤）+ 10s 超时 + limit 50/200 诚实截断 + 透视照常；单测 28 例 + httptest 桩集成测试；**本机经 REQ-146 一键安装 oxigraph v0.5.11（macOS 首次走通）后全链真机实测**：tools/list 5 工具/变更查询全拒（QUERY_REJECTED 不触达引擎）/GROUP BY 聚合开放问题/Agent 对话端到端（tool.call source=ontology:facade→正确回答）——**验收 23 前半达成**；guide 注入文案与前端工具表同步 5 工具（03 v0.33/04 v0.23/02 v0.66/20 v1.16 S4.7/18 v1.29）。**环境注**：本机 python3 shim 因 xcrun 缺失损坏，方案启动 sidecar 经 SIDECAR_PYTHON=/usr/local/bin/python3.12（+pip rdflib）绕过；DeepSeek 预置 Key 401（E2E 用 GLM flashx 连接）。M20 剩余：REQ-152 直连验证包 / REQ-153 双种子本体（小成本可先行）。
- **M20 REQ-153 已交付（2026-09-26）**：百级大型种子本体双份——med_common 医学常识（102 概念/170 实例，禁忌关系网）+ gene_core 基因与中心法则（104 概念/94 实例，HGNC 对齐）+ 建模说明 README×2 + seed 单测 3 例；本机运行栈全链冒烟：灌装→oxigraph 方案装载→SPARQL 四查询 1.3~1.7ms 全命中（禁忌网/层级聚合/中心法则链/染色体分组）→Graph3D 272/198 节点三维浏览流畅 + TP53 定位——**验收 23 全达成/24 百级达成**（03 v0.34/04 v0.24/02 v0.67/14 v0.19/15 v2.9/20 v1.17 S4.8）；顺修 03 验收 24 重复行。
- **待领取（已排期 M20 仅剩）**：REQ-152 外部 MCP 客户端直连验证包（Claude/Cursor 配置文档+实测，实测环节需主人侧 GUI 客户端配合）。
~~REQ-148~~（✅ a64178f）/ ~~REQ-149 展示级别作用于历史回放~~（✅ f03c1fe+5ee3ddd，2026-09-25——levelGated 历史与回放过滤 + debug_persist UI 面与联动 + 诚实标注 + 顺修 M17 model.step SSE 双发 bug；01 v0.40/02 v0.57/20 v1.8）；~~REQ-150 对比界面系统优化~~（✅ 2026-09-26 交付，见顶部 M19 阶段三行）——2026-09-25 主人提出。
~~REQ-148 供应商多实例与别名/改名~~（✅ 已交付 a64178f，2026-09-25——provider_group 表与 BaseURL 解耦 + 别名展示层 + provider-groups 端点 + 统计聚合修正，01 v0.39/02 v0.56/20 v1.7）；REQ-149 展示级别作用于历史回放（REQ-117 配套，回放过滤 + debug_persist 联动）——2026-09-25 主人提出，详单见 01 v0.38。
- P2 存量池（03 v0.22 清账后真实剩余）：REQ-78 双轨 TTL 互通（触发条件：oo-worker 实际启用，未满足）/ OWL 映射细则增强（待具体规格）/ Cayley 内存图引擎（可选）/ 方法论深度版内容包（随内容排期）。~~REQ-83 fork / NFR-O-5 方案日志 / REQ-71 画布增强 / REQ-95 diff / REQ-96 灌装 / REQ-75/76 工具链 / REQ-103 模式 A~~ 均已实现（03 v0.22 逐项清账）。
- P3：REQ-79 本体对齐合并（远期）/ REQ-77 开放工具注册 / WebVOWL 嵌入（可选增强）/ 外部 MCP 客户端直连验证 / 本体与知识库联动（实例挂文档）。~~本体热加载~~已实现（/reload + 重载按钮，03 v0.22 补记）。
- **运维注**：重启 dev 栈必须清理 `exe/backend` 编译子进程（pkill 杀 go-run 父进程不杀子进程，旧进程占 :8080 致新代码"重启不生效"）。
- 本节随开发推进更新；状态变化时同步 MEMORY 协作线。
