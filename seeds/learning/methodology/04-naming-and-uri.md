# 命名与 URI 规范

> 学习中心方法论卡片 · REQ-90 模块四（深度版，v0.2）。

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

## 深度版：命名之外的标识符工程

### name/label/URI 三层模型

- **name**：机器标识符，spec_json 内部引用键——稳定、ASCII 友好、永不改。
- **label**：人类显示名，可多语言、可随时改。
- **URI**：导出 TTL 后的全局标识，由命名空间 + name 构成——跨本体引用的凭据。

三层各司其职。最常见的混乱是把中文名直接当 name 用：本平台 spec_json 兼容中文 name（设备故障示例即如此），但导出 TTL 后 URI 会含非 ASCII 字符，跨工具兼容性下降。**工程实践：name 用英文、label 放中文**；中文 name 仅用于快速原型。

### 命名空间纪律

导出后检查三件事：①本体的命名空间前缀唯一且有意义（`http://example.org/k8s#` 而非 `http://example.org/ns1#`）；②不与已挂接词表的命名空间冲突；③同一本体跨版本 URI 不变（URI 里不要带版本号）。

### 反例辨析

- **缩写成瘾**：`usr`/`user`/`u` 三个概念并存是灾难前兆；命名规范要写进团队 checklist。
- **复数/单数混用**：`Defects` 与 `Defect` 在推理侧是两个类——校验拦不住（都合法），只有命名纪律能防。
- **关系名名词化**：`ownership`（名词）vs `ownedBy`（动词短语）——关系名保持动词性，读三元组时主谓宾才通顺。

### 在本平台的落点

- 版本 diff（REQ-95）里 changed 集合若出现 name 变更，就是命名纪律失守的信号——name 应该永不进 changed。
- 任务卡 `task_s3_break_references` 的进阶版：改名后跑一次 diff，观察引用断裂如何被校验与 diff 双重捕获。
