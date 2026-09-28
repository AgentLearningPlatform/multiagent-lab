---
module: DeepSeek Harness（推理后端）
req: [REQ-160]
docs: ["02 §6.16"]
decisions: [D-O13]
synced: 2026-09-25
---

# DeepSeek Harness（dsh）推理后端

## 产品定位

DeepSeek Harness（`dsh`）是深度求索官方开源的 **agent harness**（MIT，TypeScript/Cordis 微内核，理念 `Agent = Model + Harness`）——一个"成品运行外壳"：自带 ReAct 主循环、工具集、Web UI 与 Trajectory 审计，模型不绑定 DeepSeek（近 40 家厂商）。

平台将其接入为**第 4 个外部推理后端**（M24/REQ-160，阶段一/二已交付）：Agent 配置里把「推理后端」切到 `deepseek-harness`，本次运行即由 dsh 外壳执行——与自研 `eino-adk` 后端形成 **A/B 对照**（同一 Agent 配置、两种执行外壳），是"可插拔推理后端"教学目标（D-O13/LG-13）的最生动案例。

## 设计原理

### 接入架构：cliAdapter 骨架复用

M13/D-O13 的推理后端可插拔体系（`AgentInferenceBackend` 接口 + cliAdapter 公共骨架）为本类接入而生。dsh 适配器 = 第 4 个 `cliAdapter`（~15 行），复用既有管道：

- **PATH 探测**：`dsh --version`（未安装 → 设置页面板标"不可用"，Agent 保存不受影响，运行时报错降级）；
- **调用契约（PoC 确认）**：`dsh --profile headless <task>`——官方示例形态，answer-once-and-exit（一次任务 → 打印结果 → 退出），纯文本输出；
- **事件映射**：parsePlainLine 降级（整段输出一条 message.delta）——dsh 暂无结构化流式输出，tool 事件缺失如实标注于能力矩阵；
- **错误透出**：dsh 结构化错误码（如 `MISSING_CREDENTIAL`）原样透出到 run.error，不静默。

### 能力降级矩阵（外部 CLI 口径）

| 能力 | dsh 后端 | 说明 |
| --- | --- | --- |
| 对话/流式 | ✅（行级转译） | 整段输出一条 delta（降级形态） |
| 技能 / MCP servers | ⚠️ → instruction 注入 | dsh 插件体系对外不可达，平台侧降级为提示词注入 |
| AgentAsTool / 工作流 | ❌ | 多 Agent 编排不暴露（切 dsh 后自动回退单 Agent 对话，带告警事件） |
| 中断恢复 | ❌ 仅 Cancel | 外部 CLI 口径 |

### 安全边界

- **Minimal 裁剪**：dsh 是编码型 harness（默认工具 read_file/write_file/edit/bash/grep/git…）。接入采用 Minimal 方向裁剪编码工具，避免 Agent 跑偏操作文件系统；
- **锁版本**：dsh 为 developer preview、迭代极快有破坏性变更——平台按锁定版本（0.1.5-rc.3）适配，升级需手动（Probe 显示版本）；
- **凭据自管**：模型 Key 由 dsh 侧保管（`DEEPSEEK_API_KEY` 环境变量或 dsh web Models 页写入 DSH_HOME），平台不经手；缺失时运行报结构化 `MISSING_CREDENTIAL` 并给出补配指引；
- **D-O15 合规**：dsh 按"外部 CLI、PATH 探测、未装即降级"处理，**不进 run-dev 主链**——与 claude-code/opencode/aider 同待遇。

### 阶段三（规划）：agentd 镜像内置

与 M10 沙箱线合流——agentd 镜像增加 Node.js + 锁版本 dsh 层，"DeepSeek Harness in sandbox"：编码工具被容器边界兜底（Minimal 裁剪 + 容器隔离双保险），同时作为 REQ-142（多类型智能体研究）的首个形态实证。

## 使用指南

1. **安装 dsh**（宿主机或沙箱镜像内）：

   ```bash
   npm i -g @deepseek-ai/dsh@0.1.5-rc.3   # 平台适配锁定的版本
   ```

2. **配置凭据**（二选一）：
   - 环境变量：`export DEEPSEEK_API_KEY=sk-…`（启动主平台前）；
   - 或运行一次 `dsh web`，在其 Models 页录入（写入 DSH_HOME 凭据服务）。

3. **切换后端**：智能体配置 → 「模型与参数」→ 推理后端 → 选 `deepseek-harness`（设置页「推理后端」面板可见探测状态与版本）；

4. **发起对话**：run.started 事件标注 `backend: deepseek-harness`；回复由 dsh 产出。可与 eino-adk 开两次对话对照行为差异（A/B 教学）。

> 注意：dsh 后端下，Agent 的模型连接、技能、MCP 配置**不直接生效**（降级为 instruction 注入或由 dsh 自身配置决定）——这是外部 CLI 口径，非缺陷。

## 相关资料

- ~~《本体对话Agent技术选型_Eino_vs_DeepSeekHarness_20260915.md》~~ —— 2026-09-15 选型对照（Eino vs dsh vs Python 自研）；原档已删除（2026-09-28 文档整理），结论保留于本档附录
- [02_智能体_技术方案设计](../../docs/02_智能体_技术方案设计.md) §6.16 —— 推理后端可插拔契约与能力矩阵
- [DeepSeek Harness 官方仓库](https://github.com/deepseek-ai/deepseek-harness)（MIT，developer preview）
- [harness vs framework 概念辨析（freeCodeCamp）](https://www.freecodecamp.org/news/what-is-an-agent-harness/)

---

## 附：内置接入可行性调研全文（2026-09-25，REQ-160 立项决策依据）

> 原《DeepSeek_Harness接入可行性_20260925》单档已并入本节（2026-09-27 平台知识内容去重，G-5 增补）；结论已兑现为 M24 阶段一/二交付。

> 任务来源：开发者指示「研究当前项目 agent 后端内置接入 DeepSeek Harness 的可行性和方案」。
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

