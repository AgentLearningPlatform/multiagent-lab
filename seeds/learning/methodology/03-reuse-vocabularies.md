# 复用既有词表与顶层本体

> 学习中心方法论卡片 · REQ-90 模块三（精简版）。

## 为什么复用优先

建模前先找现成词表。复用的收益是**互操作**——你的"组织"与外部世界的"组织"对得上号，Agent 检索时能吃到通用语义；自造词则是一座孤岛。

## 常用词表速查

| 词表 | 领域 | 典型条目 |
| --- | --- | --- |
| FOAF | 人物与组织 | foaf:Person / foaf:Organization / foaf:member |
| SKOS | 分类体系与主题词表 | skos:Concept / skos:broader / skos:prefLabel |
| schema.org | 通用实体 | Person / Organization / Product |
| Dublin Core | 文献元数据 | dc:title / dc:creator / dc:date |
| BFO / DOLCE | 顶层本体 | continuant（持续体）/ occurrent（发生体） |

## 实操建议

1. 自建概念若与词表条目语义一致，用注记（如 `rdfs:seeAlso` 或等价声明）挂接而非重造。
2. 顶层本体不必全盘采用，但"物质/过程""抽象/具体"这类最高层区分值得借鉴——它们决定你的概念树第一层怎么分。
3. 复用不等于照搬：词表里 80% 的条目你用不上，只挂接真正回答 CQ 的那部分。

## 在本平台的落点

- 「学习中心 → 示例本体库」的「组织与人员」示例演示了对照 FOAF 术语的复用路径。
- 双轨（Open Ontologies 流程）可对同一本体做 FOAF 对齐实验。
