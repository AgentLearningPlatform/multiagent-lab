package runtime

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DockerBackend Docker 沙箱后端（§6.3，P1）。
// 每个一个容器跑 agent-runtime 镜像（backend/agentd）：
//
//	docker run -d --name agt-{agentID} --memory=512m --cpus=1 \
//	  -e AGENT_ID=... -e PLATFORM_URL=... -e MANIFEST_TOKEN=... -P {image}
//
// 配置经启动时下发（agentd 拉 manifest）；对话请求主平台 POST {endpoint}/run SSE 透传。
// 主平台进程重启后按注册表对账：容器存在且 running 则复用，不存在重建。
// 通过 docker CLI（os/exec）操作，不引入 SDK 依赖（学习尺度：本地单机）。
type DockerBackend struct {
	Image       string                               // agentd 镜像，如 agentd:dev
	Bin         string                               // docker CLI 路径（空 = PATH 查找 + 常见安装位置回退，10a）
	PlatformURL string                               // 容器内访问主平台的地址（如 http://host.docker.internal:8080）
	TokenIssue  func(agentID string) (string, error) // 一次性 manifest token 签发回调
	HealthzWait time.Duration                        // 启动后等待 healthz 就绪的上限（默认 60s）
}

func (d *DockerBackend) Name() string { return "docker" }

// Available 探测 docker 守护进程可达（REQ-190 auto 候选探测；超时由调用方 ctx 控制）。
func (d *DockerBackend) Available(ctx context.Context) bool {
	out, err := exec.CommandContext(ctx, d.dockerBin(), "version", "--format", "{{.Server.Version}}").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func (d *DockerBackend) containerName(agentID string) string { return "agt-" + agentID }

// instanceName 实例名（10c 作用域）：run 域且携带 RunID → agt-{agentID}-r-{run8}（每次 Run
// 独立容器，用后即清）；否则 agent 域常驻实例 agt-{agentID}（跨 Run 复用）。
func (d *DockerBackend) instanceName(spec StartSpec) string {
	if Scope() == "run" && spec.RunID != "" {
		run := spec.RunID
		if len(run) > 8 {
			run = run[:8]
		}
		return "agt-" + spec.AgentID + "-r-" + run
	}
	return "agt-" + spec.AgentID
}

// dockerBin 解析 docker CLI 路径（10a 实测补强）：DOCKER_BIN > PATH > Docker Desktop 常见安装位置。
// macOS Docker Desktop 装于 ~/.docker/bin 且不一定在服务进程 PATH 上，回退避免"找不到可执行文件"。
func (d *DockerBackend) dockerBin() string {
	if d.Bin != "" {
		return d.Bin
	}
	if p := envOf("DOCKER_BIN"); p != "" {
		return p
	}
	if _, err := exec.LookPath("docker"); err == nil {
		return "docker"
	}
	if home, err := os.UserHomeDir(); err == nil {
		if c := filepath.Join(home, ".docker", "bin", "docker"); fileExecutable(c) {
			return c
		}
	}
	for _, c := range []string{"/usr/local/bin/docker", "/opt/homebrew/bin/docker"} {
		if fileExecutable(c) {
			return c
		}
	}
	return "docker" // 兜底：让 exec 报出原始错误
}

func fileExecutable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// docker 执行 helper：输出 stdout，非零退出返回 error。
func (d *DockerBackend) docker(ctx context.Context, args ...string) (string, error) {
	c := exec.CommandContext(ctx, d.dockerBin(), args...)
	out, err := c.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("docker %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Start 启动（或复用）agentd 容器并等待其就绪（资源限制按 spec，空 = 默认 512m/1CPU）。
// agent 域：容器已在跑则复用（进程重启后注册表丢失的场景）；run 域（10c）：每次 Start
// 建新容器（对账复用跳过），Run 收尾由调用方 Stop 清理。
func (d *DockerBackend) Start(ctx context.Context, spec StartSpec) (Endpoint, error) {
	agentID := spec.AgentID
	name := d.instanceName(spec)
	runScoped := Scope() == "run" && spec.RunID != ""
	// 对账：agent 域容器已在跑则复用；run 域不复用（每次全新实例）
	if !runScoped {
		if st, _ := d.Status(ctx, agentID); st.State == "running" {
			if ep, perr := d.endpointOf(ctx, d.containerName(agentID)); perr == nil {
				return ep, nil
			}
		}
	}
	// 清理同名残留（容器名冲突）
	_, _ = d.docker(ctx, "rm", "-f", name)

	if d.TokenIssue == nil {
		return Endpoint{}, fmt.Errorf("docker backend: token issuer not configured")
	}
	token, err := d.TokenIssue(agentID)
	if err != nil {
		return Endpoint{}, fmt.Errorf("issue manifest token: %w", err)
	}

	// M10/10b：资源限制参数化（per Agent；空 = 默认）
	mem := spec.Memory
	if mem == "" {
		mem = "512m"
	}
	cpus := spec.CPUs
	if cpus <= 0 {
		cpus = 1
	}
	runArgs := []string{
		"run", "-d", "--name", name,
		"--memory=" + mem, fmt.Sprintf("--cpus=%g", cpus),
		// Linux 原生 dockerd 无 host.docker.internal DNS（Docker Desktop 特性，10c WSL 实测）——
		// host-gateway 别名三平台通用（Docker 20.10+/Podman），容器内经该名回访主平台
		"--add-host=host.docker.internal:host-gateway",
		"-e", "AGENT_ID=" + agentID,
		"-e", "PLATFORM_URL=" + d.PlatformURL,
		"-e", "MANIFEST_TOKEN=" + token,
		"-P", // 随机映射容器 8080
	}
	// 本体平面地址透传（容器内 facade 不可达时 agentd 自动降级，M8 语义）
	for _, k := range []string{"ONTOLOGY_MCP_URL", "ONTOLOGY_RUNTIME_MGR_URL", "ONTOLOGY_BUILD_SVC_URL", "ONTOLOGY_DIAL_TIMEOUT"} {
		if v := envOf(k); v != "" {
			runArgs = append(runArgs, "-e", k+"="+v)
		}
	}
	runArgs = append(runArgs, d.Image)
	if _, err := d.docker(ctx, runArgs...); err != nil {
		return Endpoint{}, err
	}

	// 等待 agentd healthz 就绪
	wait := d.HealthzWait
	if wait <= 0 {
		wait = 60 * time.Second
	}
	deadline := time.Now().Add(wait)
	for {
		ep, perr := d.endpointOf(ctx, name)
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
			// run 域：启动失败不留残容器（用后即清语义覆盖失败路径）
			if runScoped {
				_, _ = d.docker(ctx, "rm", "-f", name)
			}
			return Endpoint{}, fmt.Errorf("agentd container %s not healthy in %s", name, wait)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// endpointOf 查询容器端口映射并组装 endpoint（10c：name 由调用方按作用域给实例名）。
func (d *DockerBackend) endpointOf(ctx context.Context, name string) (Endpoint, error) {
	out, err := d.docker(ctx, "port", name, "8080/tcp")
	if err != nil {
		return Endpoint{}, err
	}
	// 形如 "0.0.0.0:32771"（多行时取第一行）
	line := strings.SplitN(out, "\n", 2)[0]
	parts := strings.Split(line, ":")
	if len(parts) != 2 {
		return Endpoint{}, fmt.Errorf("unexpected docker port output: %q", out)
	}
	return Endpoint{URL: "http://127.0.0.1:" + parts[1]}, nil
}

// Stop 停止并移除容器（10c：run 域按 (AgentID, RunID) 定位实例）。
func (d *DockerBackend) Stop(ctx context.Context, spec StopSpec) error {
	name := d.instanceName(StartSpec{AgentID: spec.AgentID, RunID: spec.RunID})
	if _, err := d.docker(ctx, "rm", "-f", name); err != nil {
		log.Printf("[runtime] stop container %s: %v", name, err)
		return err
	}
	return nil
}

// Status 查询容器状态。
func (d *DockerBackend) Status(ctx context.Context, agentID string) (BackendStatus, error) {
	out, err := d.docker(ctx, "inspect", "-f", "{{.State.Status}}", d.containerName(agentID))
	if err != nil {
		return BackendStatus{State: "stopped", Detail: "container not found"}, nil
	}
	switch out {
	case "running":
		return BackendStatus{State: "running"}, nil
	case "exited", "dead", "created":
		return BackendStatus{State: "stopped", Detail: out}, nil
	default:
		return BackendStatus{State: "error", Detail: out}, nil
	}
}

func envOf(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
