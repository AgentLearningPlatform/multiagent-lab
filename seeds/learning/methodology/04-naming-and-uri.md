# 命名与 URI 规范

> 学习中心方法论卡片 · REQ-90 模块四（精简版）。

## 为什么命名最难补救

命名是本体的"公共接口"：概念名一旦被关系/实例引用，改动等于全体引用断裂。spec_json 的 `name` 字段是**标识符**，显示名放 `label`——两者分开是本平台的第一条命名纪律。

## 四条底线

1. **概念用单数名词**（Pod 而非 Pods），关系用动词或动词短语（exposes、belongsTo）。
2. **大小写惯例全库一致**：常见 PascalCase 类名 + camelCase 属性名。
3. **避免缩写歧义**——svc 到底是 Service 还是 supervisor？写全称，label 里放别名。
4. **name 一旦被引用就不要改**；要改名，先改引用（本平台校验会拦截悬空引用）。

## URI 场景（导出 TTL 后）

spec_json 经 O1 sidecar 导出 Turtle 后，概念/实例会获得 URI。要求：

- 同一实体全域唯一；
- 可解引用（HTTP URI 优先）；
- 用命名空间分区你的本体（如 `http://example.org/k8s#Pod`）。

## 在本平台的落点

- 「本体资产 → TTL 导出」后用文本编辑器打开观察 URI 形态。
- 任务卡 `task_s3_break_references`：故意改掉一个被引用的概念名，看校验如何拦截。
