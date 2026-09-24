-- 017 非内置后端智能体 logo（REQ-137）：配置登记的图标 URL；
-- 展示侧判断 inference_backend 非内置集合且 logo_url 非空 → 显示原 logo，未配置回退默认 glyph。
ALTER TABLE agent ADD COLUMN logo_url TEXT NOT NULL DEFAULT '';
