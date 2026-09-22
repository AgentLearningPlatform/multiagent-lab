# K8s 迷你运维本体 · 建模说明

> 示例本体建模说明（REQ-90 ②）。本体 id=onto_k8s_ops，seed-sample 一键创建。

## 背景与目标

面向 K8s 运维场景的最小教学本体：让学习者用最少的概念体会 spec_json 三要素（concepts/relations/instances）与"导入→编辑→校验→可视化→运行→对话"全链路。

## 目标 CQ 列表

1. 某 Pod 部署在哪个 Node 上？（belongsTo）
2. 某 Service 暴露了哪些 Pod？（exposes）
3. 某节点上的所有工作负载有哪些？（跨概念：Node ← Pod）
4. 某实例的邻居有哪些？（onto_neighbors）

## 建模决策记录

| 决策 | 理由 |
| --- | --- |
| Pod/Node/Service 三个概念平级，不设父类 | 教学最小化；层次留给学习者练习（可加 Workload 父类） |
| belongsTo / exposes 两个关系 | 恰好覆盖"跨概念查询"与"邻居查询"两个 facade 工具演示 |
| 实例少量手写 | 数据规模不是本示例的重点（灌装路径见设备故障示例） |
| 属性只留 status / ip 两个 | 够演示 onto_get_instance 的 attributes 返回即可 |

## 局限与反例

- **没有互斥与层次**：真实 K8s 本体应有 Workload 层次与 Running/Pending 互斥——本示例刻意省略，作为学习者的第一个扩展练习。
- **没有属性域/值域**：exposes 的主客体约束未声明，错误断言不会在构建平面被拦截（引用完整性只查"概念是否定义"，不查"方向是否合理"）。
- **不适合推理对照**：无 subClassOf 结构，在带推理方案里不会有新增实例——推理对照请用有层次的示例。
