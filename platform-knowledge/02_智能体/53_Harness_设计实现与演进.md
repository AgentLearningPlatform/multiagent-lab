---
module: 智能体
topic: Harness 域事实源（设计总纲·实现现状·演进跟踪）
desc: harness 技术沉淀单一事实点——层职责与代码地图、已交付能力表（REQ/M58 里程碑映射）、演进跟踪（触发驱动项）、开放问题与诚实边界；域内文档导航
req: [REQ-14, REQ-190, REQ-191, REQ-201, REQ-202, REQ-203, REQ-204, REQ-206, REQ-214, REQ-219, REQ-224, REQ-231, REQ-232, REQ-225]
docs: ["02 §6 五层总纲", "38 号 B 阶段", "51 号体检", "17 §4.3"]
decisions: [D-O13]
synced: 2026-10-01
---

# Harness：设计、实现与演进（2026-10-01 沉淀）

> **定位**：harness 域（工具装配/防护 hooks/审批 gating/验证背压/重试/沙箱运行后端/事件契约）的**单一沉淀与演进跟踪点**——把散在 38/51/02/17/31/41 各档的 harness 内容收敛为一处导航与一张现状表。技术总纲以 `docs/02 §6 五层总纲` 为准（本档引用不重复）；需求行以 `docs/01 §3.9` / `docs/18` 为准；本文维护「实现到哪了、下一步什么在等触发」。
> **文档地图**（harness 相关内容分布在哪）：
> | 档 | harness 相关职责 |
> | --- | --- |
> | [38 号](38_智能体演进路线建议_HarnessLoopGraph.md) | 分层理论与 B 阶段执行面补齐（**历史路线档**，B1~B5 已全部交付，不再更新） |
> | [51 号（调研预研）](../08_调研预研/51_Harness产品与架构体检与演进分析_20261001.md) | harness 专项体检（产品六项/架构六项不足+W1~W6 演进建议，✅ 已全部拍板归并） |
> | docs/02 §6 五层总纲 | 技术方案事实源（层定义+代码承载对照；「配置聚合呈现≠层职责混淆」） |
> | docs/01 §3.9 + docs/18 | 需求行：REQ-202/206/214/219/224/231/232/225 |
> | [17 号 §4.3](../01_整体设计/17_产品_信息架构与界面设计.md) | 侧板 Harness 页签 IA 定案（五层视角并列、审批开关迁侧板落点） |
> | [31 号](31_智能体对接DeepSeek-Harness_可行性及方案分析.md) / [41 号](99_开源项目分析/41_DeepSeek-Harness_实现方案深度调研.md) | 外部 harness（dsh）对接方案与源码级参照 |
> | docs/20 | 冒烟检查行：S2.18（执行面）/2.24（分层侧板）/2.26（审批面） |
> | [54 号 Context](54_Context_设计实现与演进.md) / [55 号 Loop](55_Loop_设计实现与演进.md) / [56 号 Graph](56_Graph_设计实现与演进.md) | **姊妹域档**（2026-10-01 同构沉淀）——五层各域事实源并列，五层总纲的域内展开 |

## 一、设计总纲：harness 层是什么

harness = 模型之外「怎么把事做安全、做扎实」的一层。五层视角（Model/Context/Harness/Loop/Graph）中，**归 harness**：工具面（装配/来源/前缀）、防护（hooks/SSRF/安全根）、审批 gating、验证背压、重试、沙箱执行后端；**不归**：Context（预算/压缩/剪枝/召回）、Loop（迭代上限/checkpoint/续跑）、Model（连接/采样/推理后端选择）。侧板配置页签跨层聚合呈现，层职责以本口径为准。

**组件与代码地图**（backend/）：

| 组件 | 代码 | 职责 |
| --- | --- | --- |
| 装配链 assembleTools | `internal/chat/assembler.go` | 8 步管线：内置勾选→技能白名单→连接器（mcp 直通/k8s·ssh 经插件服务 :8093）→本体 facade→项目文件→执行面原语→hook 包装→审批包装；`{连接器实例名}__{tool}` 前缀改名；先到先得+重名告警 |
| HookChain | `internal/tool/hooks.go` | PreHook（带 guard 名，拒绝=结构化回执回喂+hook.denied 审计事件）/PostHook；fetchGuard（http_fetch 内网预检，启用）/cmdGuard（危险命令模式表，预置未启用） |
| 审批 gating | `internal/tool/approval.go` | 三档 off/danger/all（危险清单=写类内置+http_fetch+连接器/MCP 非 read-only 前缀，ontology/oo 免审）；豁免清单 approval_exempt；挂起经 checkpoint（SuspenseAt 持久化），恢复重入超时自动 deny |
| 验证背压 | `internal/chat/verify.go` | verify_on_stop：运行收尾执行验证命令，失败→run.finished reason=verify_failed+独立 verify.failed 事件 |
| 重试 | `internal/chat/retry.go` | 幂等错误白名单 N=2 指数退避（当前仅装配期 MCP 拉取） |
| 沙箱后端 | `internal/runtime/` | inprocess/docker/k8s/auto（lastGood 粘滞）+agent/run 两作用域；runtime_settings DB 覆盖 env（REQ-191）；启动对账/确定性路由/端口探测/回环绑定（REQ-236） |
| 事件契约 | `internal/chat/runner.go` | run_event 全集（tool.call/result 32KB 头尾截断/verify 独立事件/connector.degraded/hook.denied/approval.granted·denied 带 decision_source/run.started 生效策略透出 tool_approval {mode,source}） |
| 装配预览 | `internal/api/handlers_toolpreview.go` | GET /api/agents/{id}/tool-preview（四源合并确定性预览+遮蔽告警，零 MCP 拨号）+ GET /api/hooks |
| 分层侧板 | `web/src/components/AgentSidePanel.tsx` | Harness 页签：运行后端/沙箱资源/工作目录/验证命令/审批策略（三档+豁免+超时）/工具预览卡/hooks 卡（后端读取） |
| 外部 harness 接口 | `internal/inference/acp.go` | dsh 通道面（ACP 长驻 stdio/session 语义转译/权限默认 deny；外部 CLI 后端不走装配链——harness 配置对其不生效） |

## 二、实现现状（已交付能力表）

| 能力 | REQ | 里程碑 | 状态 | 验证 |
| --- | --- | --- | --- | --- |
| 上下文工程（相邻层前置） | REQ-201 | M37 | ✅ | 真机 11/11 |
| 执行面原语+hooks 骨架+verify 背压+重试 | REQ-202/203 | M38 | ✅ | 真机 12 断言 |
| Loop 持久化 checkpoint/续跑/usage | REQ-204 | M39 | ✅（核心） | 真机 9/9 |
| 连接器统一抽象+插件服务+凭据加密 | REQ-214 | M46 | ✅ | 真机 GLM run |
| 分层侧板 Harness/Context/Loop/Graph 页签 | REQ-219 | M50 | ✅ | headless 16/16 |
| dsh 通道面 S1+S2（外部 harness） | REQ-206 | M41 | ✅（S3 可选待领取） | stub ACP 单测 |
| 事件契约（审计事件/截断/独立 verify） | REQ-224 | M52 | ✅ | 单测+真机 |
| 审批三档+豁免+挂起超时+生效透出+装配预览 | REQ-231 | M58 | ✅ | 真机 danger 全链（挂起→approve→granted 审计→文件写出；豁免免审）+headless 7/7 |
| 沙箱纵深（work_dir volume/run_command/出网） | REQ-225 | M53 | 📋 触发驱动 | — |
| hooks 可配置化（agent 级开关/自定义 deny 规则） | REQ-232 | M59 | 📋 触发驱动（cmdGuard 生效绑定 REQ-225②） | — |
| 外部 harness 治理面（页签诚实降维+dsh 权限转审批） | REQ-206 余项 | M41 真机联调轮 | 📋 归 REQ-206 扩注 | — |
| 执行期超时统一入口+连接器周期探测 | 51 号 W6 | — | ⏳ 需求池观察条目 | — |

## 三、开放问题与诚实边界

1. **mcp 直通连接器保守全审**——danger 档无法静态判定外部 MCP 工具读写性，全部视为危险（read-only 免审仅覆盖内置与 oo/ontology 前缀）。
2. **挂起超时仅在恢复重入时判定**——挂起中无后台时钟（免常驻进程），后端重启后超时视角丢失。
3. **work_dir 零 volume 绑定**——inprocess 与 docker/k8s 两执行世界文件语义割裂，待 REQ-225/M53 触发（触发条件=出现真实「跑命令改代码并验证」本地任务诉求）。
4. **hooks 不可配置**——进程内固定注册表，agent 级开关与自定义 deny 规则待 REQ-232/M59 触发。
5. **外部 CLI 后端旁路**——inference_backend 为外部 CLI 时 harness 配置全部不生效，侧板降维标注随 M41 真机联调轮（51 号 W5 定案：UI 标注+保存校验都做）。
6. **装配预览不含本体 facade 与会话形态工具**——onto_* 随对话挂载、todo_write 仅会话内，预览为能力面口径（诚实注记在端点响应）。

## 四、维护约定

- 本档为 harness 域**新增演进的默认落点**：新 REQ 立项/交付后更新「实现现状」表与「开放问题」；里程碑状态以 docs/02 §12 为权威，本表只做域内速览。
- 51 号体检档内容已由本档与 REQ-231/224 交付吸收，按调研预研收录规则待整理轮归档；38 号保持历史档不动。
- 理论补充（五层模型、外部 harness 对比）继续沉淀 36/37/38 号体系，不在本档重复。
