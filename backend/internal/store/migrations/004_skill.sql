-- 004 技能系统（v0.6 §5.2/§6.12，M7 P1）
CREATE TABLE IF NOT EXISTS skill (
  id TEXT PRIMARY KEY,                    -- 如 skill_k8s_triage
  name TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  instruction TEXT NOT NULL,              -- 注入系统提示词的指令段
  tools TEXT NOT NULL DEFAULT '[]',       -- 工具白名单（工具注册表 id 数组）
  resources TEXT NOT NULL DEFAULT '[]',   -- [{name,content}]（P2 激活注入）
  builtin INTEGER NOT NULL DEFAULT 0,     -- 内置示例技能：不可删除，只可停用
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at DATETIME, updated_at DATETIME
);

-- 内置示例技能 seed（INSERT OR IGNORE 保证幂等）
INSERT OR IGNORE INTO skill (id,name,description,instruction,tools,resources,builtin,enabled,created_at,updated_at)
VALUES (
  'skill_structured_output','结构化输出约束',
  '要求模型以严格 JSON 输出结果，适合需要程序化解析的场景。',
  '【输出格式约束】
1. 最终回答必须是一个合法 JSON 对象，不要输出任何 JSON 之外的文字、Markdown 代码块围栏。
2. JSON 顶层结构：{"summary": string, "items": [{"title": string, "detail": string}], "confidence": number}。
3. confidence 取值 0~1；不确定的信息在 items 中保留并降低 confidence。
4. 无法回答时输出 {"summary":"","items":[],"confidence":0}。',
  '[]','[]',1,1,
  datetime('now'), datetime('now')
),
(
  'skill_k8s_triage','K8s 排障指引',
  '按固定排查顺序定位 Kubernetes 集群问题，适合运维/DevOps 助手。',
  '【K8s 排障工作法】
按以下顺序排查并输出每步结论：
1. 命名空间与资源现状：kubectl get events --sort-by=.metadata.creationTimestamp 取最近异常。
2. Pod 状态：kubectl get pods -o wide | grep -v Running，逐个看 kubectl describe pod 的 Events 段。
3. 日志：容器 CrashLoopBackOff 先 kubectl logs --previous；OOMKilled 看 lastState.terminated.reason。
4. 网络与服务：Service Endpoints 是否为空；NetworkPolicy 是否拦截。
5. 输出结论格式：现象 → 根因 → 修复建议（可执行命令）→ 风险提示。',
  '[]','[]',1,1,
  datetime('now'), datetime('now')
);
