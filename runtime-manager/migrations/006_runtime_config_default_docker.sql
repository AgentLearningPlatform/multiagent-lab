-- 006 REQ-236④/M63（D-O20 v0.73 变更，2026-10-01 开发者拍板）：单机默认执行方式
-- 自 k8s 改为 docker（未安装 docker 时启动期降级进程内 native——降级在 startEngine
-- 运行期探测，本迁移只翻默认值）。仅翻转「仍是出厂默认 k8s」的行；用户显式配置的
-- native/docker 不动；显式 k8s 一并翻转为 docker（k8s 为纯桩必报错，翻转只会更可用，
-- 用户可随时在设置页改回）。幂等：非 k8s 行零影响。
UPDATE runtime_config SET execution_method = 'docker', updated_at = CURRENT_TIMESTAMP
WHERE execution_method = 'k8s';
