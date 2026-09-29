# Eino 框架概述

Eino 是字节跳动开源的 Go 大模型应用开发框架，提供链式编排（Chain/Graph）与 ADK 智能体运行时。
Eino 的组件面覆盖 ChatModel、Tool、Retriever 与 Lambda 节点；其中 Retriever 组件对接外部检索后端。
eino-ext 扩展仓库提供 Qdrant 客户端组件：Eino 的 Retriever 经 eino-ext 封装后可直连 Qdrant 集群，
Qdrant 内部使用 HNSW 索引加速 cosine 相似度检索。

# 检索服务缺陷记录

BUG-1024：检索服务在弱网环境下 P99 延迟超过 30 秒，定位为 Qdrant 客户端超时未配置；
BUG-1024 的修复方案是给 Qdrant 客户端显式设置 3 秒拨号超时与 10 秒整体超时。
BUG-1024 由该超时缺失引发，回归用例覆盖弱网注入场景。

# 评测语料说明

本文档是知识库检索评测基准（KB-14/M35）的种子语料：刻意包含编号类（BUG-1024）、
术语类（HNSW、cosine）与依赖链类（Eino→eino-ext→Qdrant）三类可出题素材。
依赖链：Eino 框架封装 eino-ext 组件，eino-ext 组件连接 Qdrant 服务，Qdrant 服务内置 HNSW 索引。
