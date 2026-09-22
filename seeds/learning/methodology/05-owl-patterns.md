# 常见 OWL 建模模式

> 学习中心方法论卡片 · REQ-90 模块五（精简版）。

## 五个高频模式

### ① 子类分层（rdfs:subClassOf）

表达 "is-a"。子类继承父类的全部属性与关系约束。spec_json 里就是概念的 `parents` 字段。

### ② 互斥（owl:disjointWith）

让矛盾在推理时**暴露而非沉默**。"故障"与"正常"应互斥——某实例同时断言两者时，推理机会报不一致。

### ③ 部分-整体用专门关系

引擎（Engine）不是车（Car）的子类，是车的**组成部分**。用 `hasPart` 关系而非 subClassOf——混淆两者是最常见的建模错误。

### ④ 属性域/值域（rdfs:domain / rdfs:range）

声明"exposes 的主体一定是 Service、客体一定是 Pod"，让错误断言可校验。

### ⑤ 反属性成对声明

如 causes / causedBy 成对声明，方便双向查询。

## 与运行方式的联动（关键学习点）

在 SPARQL 型方案（Oxigraph）里这些公理**不参与推理**（精确匹配优先）；带推理的方案（Fuseki / Open Ontologies）才会让 subClassOf 传导生效。同一份本体、不同运行方式、不同查询结果——这正是「学习中心 → 运行方式对照」要演示的对照点。

## 在本平台的落点

- 任务卡 `task_s5_reload_semantics`：多方案并存对照。
- 推理对照入口随 Fuseki（O6）激活（REQ-94）。
