-- 007 REQ-234①/M61：装载质量快照列（Start 拉取本体后经构建平面 quality/check save=false
-- 快评聚合，JSON {oid:{overall,error_count,warning_count}}；失败跳过不阻断启动）。
-- 沿 003 loaded_versions 先例（IFNULL 兼容存量行）。
-- 注：004 为历史跳号（无内容遗失），迁移按文件名排序执行不受影响。
ALTER TABLE runtime_profile ADD COLUMN loaded_quality TEXT;
