-- 024_runtime_settings.sql REQ-191/M31：运行环境统一配置（设置页「运行环境」分区）。
-- 平台级运行方式配置单行表（id=1）：DB 覆盖启动期 env（env 为初始值兜底）——
-- 全部字段默认空串=「跟随启动环境」，存量部署（仅 env 配置）零破坏。
-- 生效语义：保存后新 Run/新方案启动生效（resolver 按配置重建后端），运行中实例不受影响。
CREATE TABLE IF NOT EXISTS runtime_settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  sandbox_mode TEXT NOT NULL DEFAULT '',            -- ''(跟随env) | inprocess | docker | k8s | auto
  sandbox_image TEXT NOT NULL DEFAULT '',           -- agentd 镜像（docker/k8s/auto 共用；空=未启用沙箱）
  sandbox_scope TEXT NOT NULL DEFAULT '',           -- ''(跟随env) | agent | run
  docker_bin TEXT NOT NULL DEFAULT '',              -- docker CLI 路径（空=PATH）
  kubectl_bin TEXT NOT NULL DEFAULT '',             -- kubectl CLI 路径（空=PATH）
  k8s_kubeconfig TEXT NOT NULL DEFAULT '',          -- kubeconfig 路径（空=默认 ~/.kube/config）；认证跟随 kubeconfig
  k8s_context TEXT NOT NULL DEFAULT '',             -- kubectl --context（空=当前 context）
  k8s_namespace TEXT NOT NULL DEFAULT '',           -- Pod 目标 namespace（空=context 默认）
  k8s_endpoint_mode TEXT NOT NULL DEFAULT '',       -- ''(跟随env) | port-forward | pod-ip
  platform_url_in_cluster TEXT NOT NULL DEFAULT '', -- Pod 内回访主平台地址（k8s 优先）
  platform_url_external TEXT NOT NULL DEFAULT '',   -- 容器回访主平台地址（docker 用）
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT OR IGNORE INTO runtime_settings (id) VALUES (1);
