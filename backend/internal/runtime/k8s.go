package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// K8sBackend K8s Pod 沙箱后端（M10 10d，§6.3 同接口扩展）。
// 与 DockerBackend 同语义：每 Agent 一个 agentd Pod（agt-{agentID}），manifest 启动时经环境
// 变量下发（agentd 拉 manifest），主平台 POST {endpoint}/run SSE 透传；进程重启后按注册表对账
// （Pod 存在且 Running 则复用）。经 kubectl CLI 操作，不引入 client-go 依赖（与 docker CLI 同
// 学习尺度口径）。
//
// 端点模式（EndpointMode）：
//   - "port-forward"（默认）：本机 `kubectl port-forward pod/{name} {port}:8080`，端点
//     http://127.0.0.1:{port}——平台在集群外（本地 dev）时唯一可用形态；转发进程随 Stop 回收；
//   - "pod-ip"：直接用 Pod IP（平台与集群同网/平台在集群内时）。
//
// 诚实边界（2026-09-27 交付）：本机无 K8s 集群——机制经 stub kubectl 桩测试验证（manifest
// 内容/状态映射/对账/资源限制），真机（真实集群）验证待主人侧环境，见 02 §12 M10 行。
type K8sBackend struct {
	Image         string // agentd 镜像（集群内可见，如 agentd:dev 或 registry 路径）
	Bin           string // kubectl 路径（空 = PATH）
	Namespace     string // 缺省 = kubeconfig 当前 namespace
	Context       string // kubectl --context（空 = 当前 context）
	PlatformURL   string // Pod 内回访主平台地址（集群内为 service DNS 或节点地址）
	TokenIssue    func(agentID string) (string, error)
	HealthzWait   time.Duration // 启动后等待 healthz 就绪上限（默认 90s，含拉镜像）
	EndpointMode  string        // port-forward（默认）| pod-ip
	ContainerPort int           // agentd 容器端口（缺省 8080；测试桩可注入）

	mu       sync.Mutex
	forwards map[string]*exec.Cmd // agentID -> port-forward 进程（Stop 回收）
	ports    map[string]string    // agentID -> 本地端口
}

func (k *K8sBackend) Name() string { return "k8s" }

func (k *K8sBackend) podName(agentID string) string { return "agt-" + agentID }

func (k *K8sBackend) containerPort() int {
	if k.ContainerPort > 0 {
		return k.ContainerPort
	}
	return 8080
}

func (k *K8sBackend) kubectl(ctx context.Context, args ...string) (string, error) {
	bin := k.Bin
	if bin == "" {
		if p := envOf("KUBECTL_BIN"); p != "" {
			bin = p
		} else {
			bin = "kubectl"
		}
	}
	if k.Context != "" {
		args = append([]string{"--context", k.Context}, args...)
	}
	if k.Namespace != "" {
		args = append([]string{"-n", k.Namespace}, args...)
	}
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("kubectl %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("kubectl %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// podManifest 生成 agentd Pod 清单（JSON，经 stdin apply）。资源限制映射 10b 参数：
// memory 512m → 512Mi，cpus 1 → "1"。
func (k *K8sBackend) podManifest(spec StartSpec, token string) map[string]any {
	mem := spec.Memory
	if mem == "" {
		mem = "512m"
	}
	mem = strings.Replace(mem, "m", "Mi", 1)
	cpus := spec.CPUs
	if cpus <= 0 {
		cpus = 1
	}
	env := []map[string]string{
		{"name": "AGENT_ID", "value": spec.AgentID},
		{"name": "PLATFORM_URL", "value": k.PlatformURL},
		{"name": "MANIFEST_TOKEN", "value": token},
	}
	for _, key := range []string{"ONTOLOGY_MCP_URL", "ONTOLOGY_RUNTIME_MGR_URL", "ONTOLOGY_BUILD_SVC_URL", "ONTOLOGY_DIAL_TIMEOUT"} {
		if v := envOf(key); v != "" {
			env = append(env, map[string]string{"name": key, "value": v})
		}
	}
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]any{"name": k.podName(spec.AgentID), "labels": map[string]string{"app": "agentd", "agent": spec.AgentID}},
		"spec": map[string]any{
			"restartPolicy": "Always",
			"containers": []any{map[string]any{
				"name":  "agentd",
				"image": k.Image,
				"env":   env,
				"resources": map[string]any{
					"requests": map[string]string{"memory": mem, "cpu": fmt.Sprintf("%g", cpus)},
					"limits":   map[string]string{"memory": mem, "cpu": fmt.Sprintf("%g", cpus)},
				},
			}},
		},
	}
}

// Start 启动（或复用）agentd Pod 并等待就绪（对账：Running 且健康则直接复用端点）。
func (k *K8sBackend) Start(ctx context.Context, spec StartSpec) (Endpoint, error) {
	agentID := spec.AgentID
	if st, _ := k.Status(ctx, agentID); st.State == "running" {
		if ep, perr := k.endpointOf(ctx, agentID); perr == nil {
			return ep, nil
		}
	}
	if k.TokenIssue == nil {
		return Endpoint{}, fmt.Errorf("k8s backend: token issuer not configured")
	}
	token, err := k.TokenIssue(agentID)
	if err != nil {
		return Endpoint{}, fmt.Errorf("issue manifest token: %w", err)
	}
	manifest, err := json.Marshal(k.podManifest(spec, token))
	if err != nil {
		return Endpoint{}, err
	}
	// 覆盖式重建：删旧 Pod（含 Nonexistent 容错）再 apply，保证镜像/env 变更生效
	_, _ = k.kubectl(ctx, "delete", "pod", k.podName(agentID), "--ignore-not-found=true", "--wait=false")
	if err := k.applyStdin(ctx, manifest); err != nil {
		return Endpoint{}, err
	}

	wait := k.HealthzWait
	if wait <= 0 {
		wait = 90 * time.Second
	}
	deadline := time.Now().Add(wait)
	for {
		ep, perr := k.endpointOf(ctx, agentID)
		if perr == nil {
			cli := &http.Client{Timeout: 2 * time.Second}
			resp, herr := cli.Get(ep.URL + "/healthz")
			if herr == nil {
				_ = resp.Body.Close()
				if resp.StatusCode < 500 {
					return ep, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return Endpoint{}, fmt.Errorf("agentd pod %s not healthy in %s", k.podName(agentID), wait)
		}
		time.Sleep(1 * time.Second)
	}
}

// applyStdin 经 stdin `kubectl apply -f -` 下发清单（免临时文件）。
func (k *K8sBackend) applyStdin(ctx context.Context, manifest []byte) error {
	bin := k.Bin
	if bin == "" {
		if p := envOf("KUBECTL_BIN"); p != "" {
			bin = p
		} else {
			bin = "kubectl"
		}
	}
	args := []string{"apply", "-f", "-"}
	if k.Context != "" {
		args = append([]string{"--context", k.Context}, args...)
	}
	if k.Namespace != "" {
		args = append([]string{"-n", k.Namespace}, args...)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(string(manifest))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("kubectl apply: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// endpointOf 按端点模式组装 endpoint（port-forward 模式懒建转发进程）。
func (k *K8sBackend) endpointOf(ctx context.Context, agentID string) (Endpoint, error) {
	ip, err := k.kubectl(ctx, "get", "pod", k.podName(agentID),
		"-o", "jsonpath={.status.podIP}")
	if err != nil || ip == "" {
		return Endpoint{}, fmt.Errorf("pod %s 无 podIP: %v", k.podName(agentID), err)
	}
	if k.endpointMode() == "pod-ip" {
		return Endpoint{URL: fmt.Sprintf("http://%s:%d", ip, k.containerPort())}, nil
	}
	return k.portForwardEndpoint(ctx, agentID)
}

// portForwardEndpoint 建立本机端口转发（幂等：已有转发且进程存活则复用）。
func (k *K8sBackend) portForwardEndpoint(ctx context.Context, agentID string) (Endpoint, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.forwards == nil {
		k.forwards = map[string]*exec.Cmd{}
		k.ports = map[string]string{}
	}
	if cmd := k.forwards[agentID]; cmd != nil && cmd.Process != nil {
		if cmd.ProcessState == nil {
			return Endpoint{URL: "http://127.0.0.1:" + k.ports[agentID]}, nil
		}
	}
	port, err := freePort()
	if err != nil {
		return Endpoint{}, fmt.Errorf("alloc local port: %w", err)
	}
	bin := k.Bin
	if bin == "" {
		if p := envOf("KUBECTL_BIN"); p != "" {
			bin = p
		} else {
			bin = "kubectl"
		}
	}
	args := []string{"port-forward", "pod/" + k.podName(agentID), fmt.Sprintf("%s:%d", port, k.containerPort())}
	if k.Context != "" {
		args = append([]string{"--context", k.Context}, args...)
	}
	if k.Namespace != "" {
		args = append([]string{"-n", k.Namespace}, args...)
	}
	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		return Endpoint{}, fmt.Errorf("kubectl port-forward: %w", err)
	}
	k.forwards[agentID] = cmd
	k.ports[agentID] = port
	return Endpoint{URL: "http://127.0.0.1:" + port}, nil
}

func (k *K8sBackend) endpointMode() string {
	if m := envOf("SANDBOX_K8S_ENDPOINT_MODE"); m != "" {
		return m
	}
	if k.EndpointMode != "" {
		return k.EndpointMode
	}
	return "port-forward"
}

// Stop 删除 Pod 并回收本机转发进程。
func (k *K8sBackend) Stop(ctx context.Context, agentID string) error {
	k.mu.Lock()
	if cmd := k.forwards[agentID]; cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait() // 同步回收（SIGKILL 即刻退出；避免僵尸进程干扰状态断言）
	}
	delete(k.forwards, agentID)
	delete(k.ports, agentID)
	k.mu.Unlock()
	_, err := k.kubectl(ctx, "delete", "pod", k.podName(agentID), "--ignore-not-found=true", "--wait=false")
	return err
}

// Status 查询 Pod phase → 后端状态（对账口径与 DockerBackend 一致）。
func (k *K8sBackend) Status(ctx context.Context, agentID string) (BackendStatus, error) {
	phase, err := k.kubectl(ctx, "get", "pod", k.podName(agentID), "-o", "jsonpath={.status.phase}")
	if err != nil || strings.TrimSpace(phase) == "" {
		return BackendStatus{State: "stopped", Detail: "pod not found"}, nil
	}
	switch strings.TrimSpace(phase) {
	case "Running":
		return BackendStatus{State: "running", Detail: phase}, nil
	case "Pending", "ContainerCreating":
		return BackendStatus{State: "stopped", Detail: phase}, nil
	case "Failed", "Unknown":
		return BackendStatus{State: "error", Detail: phase}, nil
	default:
		return BackendStatus{State: "stopped", Detail: phase}, nil
	}
}

func freePort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port), nil
}
