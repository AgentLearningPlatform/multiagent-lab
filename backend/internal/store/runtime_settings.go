// 平台运行环境配置（REQ-191/M31）：runtime_settings 单行表（id=1）。
// 智能体沙箱运行方式（进程内嵌/docker/k8s/auto）与 K8s 访问认证集中存储；
// 空字段语义=「跟随启动环境」（env 兜底），合并逻辑见 MergeOver。
// 本体引擎执行方式不在本表——仍由 runtime-manager runtime_config 承载（REQ-179/M-O16，API 不变）。
package store

import "strings"

// RuntimeSettings 运行环境配置（单例）。
type RuntimeSettings struct {
	SandboxMode          string `json:"sandbox_mode"`            // ''|inprocess|docker|k8s|auto
	SandboxImage         string `json:"sandbox_image"`           // agentd 镜像
	SandboxScope         string `json:"sandbox_scope"`           // ''|agent|run
	DockerBin            string `json:"docker_bin"`              // docker CLI 路径
	KubectlBin           string `json:"kubectl_bin"`             // kubectl CLI 路径
	K8sKubeconfig        string `json:"k8s_kubeconfig"`          // kubeconfig 路径（认证跟随 kubeconfig）
	K8sContext           string `json:"k8s_context"`             // kubectl --context
	K8sNamespace         string `json:"k8s_namespace"`           // Pod namespace
	K8sEndpointMode      string `json:"k8s_endpoint_mode"`       // ''|port-forward|pod-ip
	PlatformURLInCluster string `json:"platform_url_in_cluster"` // Pod 回访平台地址
	PlatformURLExternal  string `json:"platform_url_external"`   // 容器回访平台地址
	UpdatedAt            string `json:"updated_at,omitempty"`
}

const runtimeSettingsCols = `sandbox_mode,sandbox_image,sandbox_scope,docker_bin,kubectl_bin,
k8s_kubeconfig,k8s_context,k8s_namespace,k8s_endpoint_mode,platform_url_in_cluster,platform_url_external`

// GetRuntimeSettings 读取配置（无行返回零值，不视为错误——迁移已种子 id=1 行）。
func (s *Store) GetRuntimeSettings() (*RuntimeSettings, error) {
	row := s.DB.QueryRow(`SELECT ` + runtimeSettingsCols + `,updated_at FROM runtime_settings WHERE id=1`)
	var c RuntimeSettings
	if err := row.Scan(&c.SandboxMode, &c.SandboxImage, &c.SandboxScope, &c.DockerBin, &c.KubectlBin,
		&c.K8sKubeconfig, &c.K8sContext, &c.K8sNamespace, &c.K8sEndpointMode,
		&c.PlatformURLInCluster, &c.PlatformURLExternal, &c.UpdatedAt); err != nil {
		return &RuntimeSettings{}, nil
	}
	return &c, nil
}

// SaveRuntimeSettings 全量覆盖保存（前端表单整单提交；校验在 API 层做枚举白名单）。
func (s *Store) SaveRuntimeSettings(c *RuntimeSettings) error {
	_, err := s.DB.Exec(`UPDATE runtime_settings SET sandbox_mode=?, sandbox_image=?, sandbox_scope=?,
		docker_bin=?, kubectl_bin=?, k8s_kubeconfig=?, k8s_context=?, k8s_namespace=?, k8s_endpoint_mode=?,
		platform_url_in_cluster=?, platform_url_external=?, updated_at=? WHERE id=1`,
		c.SandboxMode, c.SandboxImage, c.SandboxScope, c.DockerBin, c.KubectlBin,
		c.K8sKubeconfig, c.K8sContext, c.K8sNamespace, c.K8sEndpointMode,
		c.PlatformURLInCluster, c.PlatformURLExternal, now())
	return err
}

// MergeOver DB 配置覆盖 base（env 兜底快照）：DB 空字段回落 base 值——「跟随启动环境」语义。
func (rs *RuntimeSettings) MergeOver(base RuntimeSettings) RuntimeSettings {
	m := base
	if v := strings.TrimSpace(rs.SandboxMode); v != "" {
		m.SandboxMode = v
	}
	if v := strings.TrimSpace(rs.SandboxImage); v != "" {
		m.SandboxImage = v
	}
	if v := strings.TrimSpace(rs.SandboxScope); v != "" {
		m.SandboxScope = v
	}
	if v := strings.TrimSpace(rs.DockerBin); v != "" {
		m.DockerBin = v
	}
	if v := strings.TrimSpace(rs.KubectlBin); v != "" {
		m.KubectlBin = v
	}
	if v := strings.TrimSpace(rs.K8sKubeconfig); v != "" {
		m.K8sKubeconfig = v
	}
	if v := strings.TrimSpace(rs.K8sContext); v != "" {
		m.K8sContext = v
	}
	if v := strings.TrimSpace(rs.K8sNamespace); v != "" {
		m.K8sNamespace = v
	}
	if v := strings.TrimSpace(rs.K8sEndpointMode); v != "" {
		m.K8sEndpointMode = v
	}
	if v := strings.TrimSpace(rs.PlatformURLInCluster); v != "" {
		m.PlatformURLInCluster = v
	}
	if v := strings.TrimSpace(rs.PlatformURLExternal); v != "" {
		m.PlatformURLExternal = v
	}
	return m
}

// Hash 配置指纹（resolver 缓存键——任意字段变化即重建后端实例，auto 探测缓存随之重置）。
func (rs *RuntimeSettings) Hash() string {
	return strings.Join([]string{rs.SandboxMode, rs.SandboxImage, rs.SandboxScope, rs.DockerBin,
		rs.KubectlBin, rs.K8sKubeconfig, rs.K8sContext, rs.K8sNamespace, rs.K8sEndpointMode,
		rs.PlatformURLInCluster, rs.PlatformURLExternal}, "\x1f")
}
