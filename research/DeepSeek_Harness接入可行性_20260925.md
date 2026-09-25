# DeepSeek Harness 内置接入可行性调研（推理后端扩展提案，2026-09-25）

> 任务来源：主人指示「研究当前项目 agent 后端内置接入 DeepSeek Harness 的可行性和方案」。
> 语境：DeepSeek Harness（`dsh`）= 深度求索官方开源 agent harness（MIT，TypeScript/Cordis 微内核，理念 `Agent = Model + Harness`，2026-08-13 开源 developer preview）——本项目 2026-09-15 技术选型时已评估过（《本体对话Agent技术选型_Eino_vs_DeepSeekHarness_20260915.md》，当时结论"Eino 为主壳"，dsh 未淘汰而是作为对照位）。
> 性质：可行性分析 + 集成方案（进 research/ 单源）；结论回写 02 §6.16 / 18 / 15 / AGENTS。

## 0. 结论速览

1. **可行，且架构匹配度极高**：M13/D-O13 已建成的推理后端可插拔体系（`AgentInferenceBackend` 接口 + cliAdapter 公共骨架 + PATH 探测 + 能力降级语义）就是为"接入外部执行外壳"设计的——dsh 与已交付的 claude-code/opencode/aider 三个适配器**完全同类**，新增第 4 个适配器约 60~100 行，零架构改动。
2. **关键不确定性只有一个**：dsh 的**非交互（headless）CLI 形态**未有一手确认（选型档 H2/H4 明确标注"developer preview、迭代极快、二手资料需复核"）——PoC 第一交付就是验证 `dsh` 的 headless 运行方式与输出格式。
3. **推荐**：以 M13 外部 CLI 适配器模式接入（P3/P2，PoC 先行，成本约 1~2 人日 + PoC 0.5 天）；锁版本安装规避 developer preview 破坏性变更；Minimal 预设裁剪编码工具对齐平台安全边界；远期与沙箱线合流（agentd 镜像内置 Node+dsh）。

## 1. 为什么"内置接入"是低成本的——架构对照

当前推理后端体系（02 §6.16 实测交付）：

| 构件 | 现状 | dsh 接入时的角色 |
| --- | --- | --- |
| `Backend` 接口（Name/Probe/Capabilities/Run） | ✅ 已稳定（inference.go） | dsh 适配器实现同一接口 |
| `cliAdapter` 公共骨架（PATH 探测/--version/buildArgs/stdout 行解析/Debug.cli 透传） | ✅ 三个适配器共用 | **直接复用**：新 cliAdapter{name:"deepseek-harness", bin:["dsh"], ...} |
| 能力降级语义（技能/MCP→instruction 注入，不支持多 Agent 编排） | ✅ Capabilities() 矩阵 | dsh 声明同等降级（其插件体系对外不可达） |
| 前端（AgentModal 推理后端下拉 / 设置页探测面板） | ✅ 通用渲染 | dsh 自动出现在下拉与面板，零前端改动 |
| 事件转译（message.delta/tool.call → 平台 SSE） | ✅ parseLine 管道 | 按 dsh 输出格式写 parser（PoC 定格式） |

对比：如果走"深集成"路线（把 dsh 的 Cordis 插件体系与平台工具系统对接），成本与维护面会大一个量级且与 M13"仅协议桥接、不研发推理实现"的边界冲突——**不推荐**。

## 2. dsh 关键事实（选型档 H1~H5 + 本次复核）

| 编号 | 事实 | 对接入的影响 |
| --- | --- | --- |
| H1 | TypeScript 96.9%，Cordis 微内核，everything-is-a-plugin | 外壳自成体系；平台只做"提示进/回复出"的壳间桥接 |
| H2 | MIT；developer preview v0.1；**迭代极快、有兼容性破坏性变更**（1.6 万+ commits） | 锁版本安装（npm i -g @deepseek-ai/dsh@固定版）；Probe 版本探测 + 适配器注记 pin 范围 |
| H3 | 自带 Web UI（`dsh web`，:3080）与 Trajectory 审计 | 平台不接管其 Web UI；`--headless`/非交互形态 **PoC 确认** |
| H4 | 模型不绑定 DeepSeek（近 40 家）；预设 Standard/PTC/Minimal/Creator | Minimal 预设 = 平台裁剪编码工具的天然抓手；模型由 dsh 侧配置（Agent 模型连接不生效，与外部 CLI 后端口径一致） |
| H5 | 编码型 harness（read_file/write_file/edit/bash/grep/git/subagent） | **安全边界主战场**：Minimal 裁剪 + 沙箱运行（M10） |

## 3. 集成方案（M24 提案，三阶段）

### 阶段一：PoC——headless 形态确认（0.5 天）

1. `npm i -g @deepseek-ai/dsh@<锁定版>`；`dsh --help` / 文档确认非交互运行方式（候选：`dsh run <prompt>` / `dsh -p` / stdin 管道；参照 claude-code `-p` 与 opencode `run` 的既有先例）；
2. 确认输出格式（纯文本 / JSONL / trajectory 文件路径）与退出码语义；
3. 确认预设/工具裁剪的配置面（Minimal 模式如何以 CLI 参数或配置文件生效）；
4. 产出：PoC 记录（本档追加或 15 号注记）+ go/no-go。

### 阶段二：适配器交付（1~2 人日）

- `internal/inference/adapters.go` 增第 4 个 `cliAdapter{name:"deepseek-harness", bin:["dsh"], versionArgs, buildArgs, parseLine}`——首版 parsePlainLine 降级（整段输出一条 delta），格式确认后升级逐行映射（对齐 claude-code 的 JSONL→delta/tool.call 先例）；
- Capabilities：同外部 CLI 口径（对话/流式；技能/MCP→instruction 注入；无多 Agent 编排）；
- 安全默认：buildArgs 固定携带 Minimal/裁剪预设（bash/edit/glob 禁用，工作区只读），Agent 级不暴露放宽开关；
- 前端零改动（下拉/面板自动出现）；设置页探测面板出现"deepseek-harness（未安装则不可用）"。

### 阶段三（可选，与沙箱合流）：agentd 镜像内置 dsh

agentd 镜像（M10）加 Node.js + 锁版本 dsh 层 → 外部 CLI 后端在**沙箱内**运行（bash/edit 工具被容器边界兜底）——"DeepSeek Harness in sandbox" 与 REQ-142（多类型智能体研究：编码型 harness 形态）直接呼应，作为该研究待办的首个实证。

## 4. 价值评估

| 维度 | 说明 |
| --- | --- |
| 学习价值 | **同一 Agent 配置在 eino-adk 与 dsh 下行为 A/B 对照**——可插拔推理后端教学目标（D-O13）的最生动案例；harness vs framework 概念辨析（freecodecamp 综述）进任务卡 |
| 模型亲和 | 平台默认连接即 DeepSeek；dsh 对 DeepSeek 模型的调优配合度预期最佳（官方同源） |
| 生态信号 | 引入官方 harness 作为对照壳，丰富"多类型智能体"（REQ-142）的形态样本 |
| 成本 | 阶段二 ~1~2 人日（复用骨架）；阶段三随沙箱镜像迭代顺带 |

## 5. 风险与对策

| 风险 | 对策 |
| --- | --- |
| developer preview 破坏性变更（H2） | 锁版本安装 + Probe 版本探测 + 适配器注记 pin 范围；升级需手动 |
| headless 形态不存在/不稳（H3 二手资料） | PoC 先行是 go/no-go 门；无 headless 则降级为"dsh web 人工对照演示"（不进推理后端注册表） |
| 编码工具安全（bash/edit） | 默认 Minimal 裁剪 + 只读 CWD + 沙箱运行（阶段三）；不禁用则不可上生产路径 |
| Node 运行时与 D-O15 | D-O15 禁令仅限 Python venv；dsh 按"外部 CLI、PATH 探测、未装即降级"处理，**不进 run-dev 主链**（与 claude-code 同待遇） |
| 事件语义缺失（无结构化输出时） | parsePlainLine 降级（整段 delta）；tool 事件缺失如实标注于能力矩阵（与 opencode 同级） |

## 6. 待决问题（开发会话领取时确认）

1. dsh 当前版本的 headless 子命令与输出契约（PoC）；
2. Minimal 预设在 CLI 面的生效方式（参数 vs 配置文件 vs 环境变量）；
3. 是否在 AgentModal 暴露"预设选择"（默认锁 Minimal）；
4. 与 REQ-142（多类型智能体研究）合并推进还是独立交付（建议：适配器独立小交付，研究结论回填 REQ-142）。
