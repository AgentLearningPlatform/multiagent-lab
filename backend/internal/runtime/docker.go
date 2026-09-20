package runtime

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
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
	PlatformURL string                               // 容器内访问主平台的地址（如 http://host.docker.internal:8080）
	TokenIssue  func(agentID string) (string, error) // 一次性 manifest token 签发回调
	HealthzWait time.Duration                        // 启动后等待 healthz 就绪的上限（默认 60s）
}

func (d *DockerBackend) containerName(agentID string) string { return "agt-" + agentID }

// docker 执行 helper：输出 stdout，非零退出返回 error。
func (d *DockerBackend) docker(ctx context.Context, args ...string) (string, error) {
	c := exec.CommandContext(ctx, "docker", args...)
	out, err := c.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("docker %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Start 启动（或复用）agentd 容器并等待其就绪。
func (d *DockerBackend) Start(ctx context.Context, agentID string) (Endpoint, error) {
	name := d.containerName(agentID)
	// 对账：容器已在跑则复用（进程重启后注册表丢失的场景）
	if st, _ := d.Status(ctx, agentID); st.State == "running" {
		if ep, perr := d.endpointOf(ctx, agentID); perr == nil {
			return ep, nil
		}
	}
	// 清理同名的停止容器（容器名冲突）
	_, _ = d.docker(ctx, "rm", "-f", name)

	if d.TokenIssue == nil {
		return Endpoint{}, fmt.Errorf("docker backend: token issuer not configured")
	}
	token, err := d.TokenIssue(agentID)
	if err != nil {
		return Endpoint{}, fmt.Errorf("issue manifest token: %w", err)
	}

	runArgs := []string{
		"run", "-d", "--name", name,
		"--memory=512m", "--cpus=1",
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
		ep, perr := d.endpointOf(ctx, agentID)
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
			return Endpoint{}, fmt.Errorf("agentd container %s not healthy in %s", name, wait)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// endpointOf 查询容器端口映射并组装 endpoint。
func (d *DockerBackend) endpointOf(ctx context.Context, agentID string) (Endpoint, error) {
	out, err := d.docker(ctx, "port", d.containerName(agentID), "8080/tcp")
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

// Stop 停止并移除容器。
func (d *DockerBackend) Stop(ctx context.Context, agentID string) error {
	if _, err := d.docker(ctx, "rm", "-f", d.containerName(agentID)); err != nil {
		log.Printf("[runtime] stop container agt-%s: %v", agentID, err)
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
