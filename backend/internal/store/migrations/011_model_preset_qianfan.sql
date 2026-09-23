-- 011 模型管理预设增强（REQ-104/M12 v0.16）：
-- 预置百度千帆 embeddings-v1 向量连接候选（OpenAI 兼容接入点预填，Key 留空待填）。
-- 幂等：仅当库内尚无任何 embedding 连接时插入（迁移兼容——不覆盖用户既有向量配置）。
-- is_default=1 落「默认向量连接候选」标记（呼应 REQ-46）：启用前 GetDefaultConnection
-- 依 enabled=1 过滤不会返回停用连接，知识库仍按「未配置向量连接」引导；填入 Key 并启用即生效。
INSERT INTO model_connection
  (id, name, conn_type, protocol, base_url, model_name, api_key_enc, api_key_hint, enabled, is_default)
SELECT 'conn_qianfan_embedding', '百度千帆（预置）', 'embedding', 'openai_compat',
       'https://qianfan.baidubce.com/v2', 'embeddings-v1', NULL, '', 0, 1
WHERE NOT EXISTS (SELECT 1 FROM model_connection WHERE conn_type = 'embedding');
