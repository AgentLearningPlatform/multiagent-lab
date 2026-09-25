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

- [DeepSeek_Harness接入可行性_20260925](../../research/DeepSeek_Harness接入可行性_20260925.md) —— 接入可行性调研全文（架构对照/dsh 事实 H1~H5/风险对策）
- 《本体对话Agent技术选型_Eino_vs_DeepSeekHarness_20260915.md》（research/）—— 2026-09-15 选型对照（Eino vs dsh vs Python 自研）
- [02_智能体_技术方案设计](../../docs/02_智能体_技术方案设计.md) §6.16 —— 推理后端可插拔契约与能力矩阵
- [DeepSeek Harness 官方仓库](https://github.com/deepseek-ai/deepseek-harness)（MIT，developer preview）
- [harness vs framework 概念辨析（freeCodeCamp）](https://www.freecodecamp.org/news/what-is-an-agent-harness/)
