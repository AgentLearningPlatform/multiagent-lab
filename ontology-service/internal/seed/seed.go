// Package seed 内置示例本体（REQ-62）：最小 K8s 运维本体。
package seed

import pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"

// K8sOps 返回最小 K8s 运维本体（Pod/Node/Service 概念、belongsTo/exposes/runs 关系、实例）。
func K8sOps() *pkgspec.Spec {
	return &pkgspec.Spec{
		ID:          "onto_k8s_ops",
		Name:        "K8s 运维本体",
		Description: "内置示例：Kubernetes 集群运维领域最小本体（Pod/Node/Service 与运维关系）",
		Concepts: []pkgspec.Concept{
			{Name: "计算节点", Label: "Node", Definition: "Kubernetes 集群中的工作节点，承载 Pod 运行"},
			{Name: "工作负载", Label: "Workload", Definition: "集群上运行的应用负载抽象"},
			{Name: "Pod", Definition: "最小调度单元，包含一个或多个容器", Parents: []string{"工作负载"}},
			{Name: "Deployment", Definition: "无状态应用的声明式管理器", Parents: []string{"工作负载"}},
			{Name: "Service", Definition: "一组 Pod 的稳定访问入口", Parents: []string{"工作负载"}},
			{Name: "命名空间", Label: "Namespace", Definition: "集群内资源的逻辑隔离分区"},
			{Name: "事件", Label: "Event", Definition: "集群中发生的值得关注的运维事件"},
		},
		Relations: []pkgspec.Relation{
			{Name: "调度于", Definition: "Pod 被调度到某个节点上运行", From: "Pod", To: "计算节点"},
			{Name: "暴露", Definition: "Service 暴露一组 Pod 作为访问入口", From: "Service", To: "Pod"},
			{Name: "管理", Definition: "Deployment 管理其副本 Pod", From: "Deployment", To: "Pod"},
			{Name: "属于", Definition: "资源归属于某命名空间", From: "工作负载", To: "命名空间"},
			{Name: "发生于", Definition: "事件发生于某资源上", From: "事件", To: "工作负载"},
		},
		Instances: []pkgspec.Instance{
			{Name: "node-worker-01", Concept: "计算节点", Attributes: map[string]any{"status": "Ready", "cpu": "8", "memory_gi": "32"}},
			{Name: "node-worker-02", Concept: "计算节点", Attributes: map[string]any{"status": "Ready", "cpu": "8", "memory_gi": "32"}},
			{Name: "ns-production", Concept: "命名空间", Attributes: map[string]any{"env": "prod"}},
			{Name: "deploy-api", Concept: "Deployment", Attributes: map[string]any{"replicas": 3, "image": "api:v1.4.2"}},
			{Name: "pod-api-7f9c", Concept: "Pod", Attributes: map[string]any{"phase": "Running", "restarts": 0}},
			{Name: "pod-api-8d1e", Concept: "Pod", Attributes: map[string]any{"phase": "CrashLoopBackOff", "restarts": 17}},
			{Name: "svc-api", Concept: "Service", Attributes: map[string]any{"type": "ClusterIP", "port": 8080}},
			{Name: "ev-api-oom", Concept: "事件", Attributes: map[string]any{"reason": "OOMKilled", "message": "内存超限被杀死"}},
		},
	}
}

// K8sOpsRelations 补实例关系（单独组装避免上面初始化块过长）。
func K8sOpsWithRelations() *pkgspec.Spec {
	s := K8sOps()
	byName := func(n string) *pkgspec.Instance {
		for i := range s.Instances {
			if s.Instances[i].Name == n {
				return &s.Instances[i]
			}
		}
		return nil
	}
	link := func(src, rel, tgt string) {
		if a, b := byName(src), tgt; a != nil {
			a.Relations = append(a.Relations, pkgspec.InstanceRel{Rel: rel, Target: b})
		}
	}
	link("pod-api-7f9c", "调度于", "node-worker-01")
	link("pod-api-8d1e", "调度于", "node-worker-02")
	link("deploy-api", "管理", "pod-api-7f9c")
	link("deploy-api", "管理", "pod-api-8d1e")
	link("svc-api", "暴露", "pod-api-7f9c")
	link("deploy-api", "属于", "ns-production")
	link("ev-api-oom", "发生于", "pod-api-8d1e")
	return s
}
