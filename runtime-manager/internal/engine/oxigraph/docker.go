package oxigraph

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/engine"
)

// ---------------------------------------------------------------------------
// REQ-179/M-O16：oxigraph Docker 执行方式（D-O20 反转 NFR-O-4「子进程为主」口径——
// 有 Docker 时容器执行为默认推荐形态）。
//   装载：数据目录准备与 native 同源（写 load_*.ttl → 容器内 oxigraph load，数据目录挂载）；
//   服务：官方镜像 oxigraph/oxigraph serve --location /data（端口映射宿主 port → 容器 8080）；
//   生命周期：docker rm -f 回收（Process.Stop）；容器名 rt-{profileID} 便于对账与排查。
// Fuseki docker 后置（REQ-146 口径：fuseki 仅手动指引，docker 化随需求推进）。
// ---------------------------------------------------------------------------

const (
	// DefaultImage oxigraph 官方镜像（Docker Hub；可用 OXIGRAPH_DOCKER_IMAGE 覆盖 pin 版本）。
	DefaultImage = "oxigraph/oxigraph"
	// ContainerPrefix 运行容器名前缀（对账与排查锚点）。
	ContainerPrefix = "rt-"
	dockerBinEnv    = "DOCKER_BIN"
)

// DockerImage 执行镜像（env 可覆盖）。
func DockerImage() string {
	if v := strings.TrimSpace(os.Getenv("OXIGRAPH_DOCKER_IMAGE")); v != "" {
		return v
	}
	return DefaultImage
}

// DockerAvailable docker CLI 可用性探测（/api/engines 自检与表单默认值联动用）。
func DockerAvailable() bool {
	bin := dockerBin()
	if bin == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "info", "--format", "{{.ServerVersion}}").CombinedOutput()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func dockerBin() string {
	if v := strings.TrimSpace(os.Getenv(dockerBinEnv)); v != "" {
		return v
	}
	if p, err := exec.LookPath("docker"); err == nil {
		return p
	}
	return ""
}

func containerName(profileID string) string { return ContainerPrefix + profileID }

// DockerStart 以容器方式装载并启动（准备与 native 同源；load/serve 均经官方镜像）。
func (r *Runtime) DockerStart(ctx context.Context, profileID string, port int, ttls map[string]string) (*engine.Process, error) {
	bin := dockerBin()
	if bin == "" {
		return nil, fmt.Errorf("docker 执行方式不可用：PATH 中未找到 docker CLI")
	}
	dir := filepath.Join(r.DataDir, profileID)
	// 幂等：重建数据目录（与 native 同语义）
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	image := DockerImage()
	run := func(args ...string) (string, error) {
		c := exec.CommandContext(ctx, bin, args...)
		out, err := c.CombinedOutput()
		if err != nil {
			return string(out), fmt.Errorf("docker %s: %v: %s", strings.Join(args, " "), err, tail(out, 300))
		}
		return string(out), nil
	}
	// 容器名清理（重启用同名冲突）
	_, _ = run("rm", "-f", containerName(profileID))
	// 装载：容器内 oxigraph load（数据目录挂载 /data）
	for oid, ttl := range ttls {
		f := filepath.Join(dir, "load_"+sanitizeID(oid)+".ttl")
		if err := os.WriteFile(f, []byte(ttl), 0o644); err != nil {
			return nil, err
		}
		containerFile := "/data/" + filepath.Base(f)
		if _, lerr := run("run", "--rm", "-v", abs+":/data", image,
			"load", "--location", "/data", "--file", containerFile); lerr != nil {
			return nil, fmt.Errorf("容器内装载 %s 失败: %s", oid, lerr)
		}
		_ = os.Remove(f)
	}
	// serve：-p 宿主 port → 容器 8080
	if _, serr := run("run", "-d", "--name", containerName(profileID),
		"-p", fmt.Sprintf("%d:8080", port),
		"-v", abs+":/data",
		image, "serve", "--location", "/data", "--bind", "0.0.0.0:8080"); serr != nil {
		return nil, fmt.Errorf("oxigraph 容器启动失败: %s", serr)
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/query", port)
	return engine.NewProcess(endpoint, 0, func() error {
		c := exec.Command(bin, "rm", "-f", containerName(profileID))
		_, _ = c.CombinedOutput()
		return nil
	}), nil
}

// DockerRunning 容器对账：容器存在且运行中 → 返回 endpoint（服务重启后领用，与 native 孤儿进程对账同思路）。
func DockerRunning(profileID string, port int) (string, bool) {
	bin := dockerBin()
	if bin == "" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "inspect", "-f", "{{.State.Running}}", containerName(profileID)).Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		return "", false
	}
	return fmt.Sprintf("http://127.0.0.1:%d/query", port), true
}
