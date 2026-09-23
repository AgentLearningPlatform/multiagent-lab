-- M13/D-O13 ⑥：agent 表新增推理后端字段（老数据为空 = eino-adk 自研默认，不需改写）
ALTER TABLE agent ADD COLUMN inference_backend TEXT NOT NULL DEFAULT '';
