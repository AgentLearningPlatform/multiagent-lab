package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type netListener net.Listener

func netListen(addr string) (netListener, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// M10 10d 桩级单测（本机无 K8s 集群）：stub kubectl 脚本驱动 manifest 生成/对账/状态映射/Stop 回收。
// 真机（真实集群）验证待开发者侧环境——边界注记见 02 §12 M10 行。

// stubKubectl 生成记录调用的伪 kubectl，返回二进制路径与日志文件路径。
func stubKubectl(t *testing.T, phase, podIP string) (bin, logPath, manifestPath string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "kubectl")
	logPath = filepath.Join(dir, "calls.log")
	manifestPath = filepath.Join(dir, "manifest.json")
	script := "#!/bin/bash\necho \"$@\" >> \"" + logPath + "\"\n" +
		"case \"$*\" in\n" +
		"  *\"jsonpath={.status.phase}\"*) echo \"" + phase + "\" ;;\n" +
		"  *\"jsonpath={.status.podIP}\"*) echo \"" + podIP + "\" ;;\n" +
		"  *\" apply \"*|*\"apply -f -\"*) cat > \"" + manifestPath + "\" ;;\n" +
		"  *\"port-forward\"*) sleep 60 ;;\n" +
		"  *) exit 0 ;;\n" +
		"esac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, logPath, manifestPath
}

func callsOf(t *testing.T, logPath string) []string {
	t.Helper()
	b, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestK8sStatusMapping(t *testing.T) {
	cases := map[string]BackendStatus{
		"Running": {State: "running", Detail: "Running"},
		"Pending": {State: "stopped", Detail: "Pending"},
		"Failed":  {State: "error", Detail: "Failed"},
	}
	for phase, want := range cases {
		bin, _, _ := stubKubectl(t, phase, "10.244.0.5")
		k := &K8sBackend{Bin: bin}
		st, err := k.Status(context.Background(), "a1")
		if err != nil {
			t.Fatalf("%s: %v", phase, err)
		}
		if st.State != want.State || st.Detail != want.Detail {
			t.Fatalf("phase %s → %+v, want %+v", phase, st, want)
		}
	}
	// Pod 不存在（kubectl 报错）→ stopped
	bin, _, _ := stubKubectl(t, "", "")
	// 空 phase 脚本会命中 *) exit 0 → 空 stdout → not found
	k := &K8sBackend{Bin: bin, Context: "c1", Namespace: "ns1"}
	st, err := k.Status(context.Background(), "a1")
	if err != nil || st.State != "stopped" {
		t.Fatalf("不存在 Pod 应 stopped: %+v %v", st, err)
	}
	if !strings.Contains(strings.Join(callsOf(t, logFor(t, bin)), ""), "--context c1") {
		t.Fatal("Context 应注入 --context 参数")
	}
}

func logFor(t *testing.T, bin string) string {
	t.Helper()
	return filepath.Join(filepath.Dir(bin), "calls.log")
}

func TestK8sStartReconcile(t *testing.T) {
	// Pod 已 Running：直接领用端点，不 apply/不 delete（对账语义）
	bin, logPath, manifestPath := stubKubectl(t, "Running", "127.0.0.1")
	k := &K8sBackend{Bin: bin, EndpointMode: "pod-ip", TokenIssue: func(string) (string, error) {
		t.Fatal("复用路径不应签发 token")
		return "", nil
	}}
	ep, err := k.Start(context.Background(), StartSpec{AgentID: "a1"})
	if err != nil {
		t.Fatal(err)
	}
	if ep.URL != "http://127.0.0.1:8080" {
		t.Fatalf("pod-ip 模式端点不符: %s", ep.URL)
	}
	joined := strings.Join(callsOf(t, logPath), "\n")
	if strings.Contains(joined, "apply") || strings.Contains(joined, "delete") {
		t.Fatalf("复用路径不应 apply/delete:\n%s", joined)
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatal("复用路径不应写 manifest")
	}
}

func TestK8sStartAppliesManifest(t *testing.T) {
	// healthz 假服务起在随机空闲端口（ContainerPort 注入，免与真实 :8080 冲突）
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无可用本地端口: %v", err)
	}
	defer ln.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	go func() { _ = http.Serve(ln, mux) }()

	bin, logPath, manifestPath := stubKubectl(t, "Pending", "127.0.0.1")
	port := ln.Addr().(*net.TCPAddr).Port
	k := &K8sBackend{Bin: bin, Image: "agentd:dev", PlatformURL: "http://platform:8080",
		EndpointMode: "pod-ip", HealthzWait: 5 * time.Second, ContainerPort: port,
		TokenIssue: func(agentID string) (string, error) { return "tok-" + agentID, nil }}
	t.Setenv("ONTOLOGY_MCP_URL", "http://onto:8091")
	ep, err := k.Start(context.Background(), StartSpec{AgentID: "a2", Memory: "512m", CPUs: 1})
	if err != nil {
		t.Fatal(err)
	}
	wantEP := fmt.Sprintf("http://127.0.0.1:%d", port)
	if ep.URL != wantEP {
		t.Fatalf("端点不符: %s, want %s", ep.URL, wantEP)
	}
	// manifest 内容断言：镜像/env/资源限制/标签
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("apply 未收到 manifest: %v", err)
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		t.Fatalf("manifest 非法 JSON: %s", raw)
	}
	meta := m["metadata"].(map[string]any)
	if meta["name"] != "agt-a2" {
		t.Fatalf("pod 名不符: %v", meta)
	}
	spec := m["spec"].(map[string]any)
	container := spec["containers"].([]any)[0].(map[string]any)
	if container["image"] != "agentd:dev" {
		t.Fatalf("镜像不符: %v", container["image"])
	}
	envs := map[string]string{}
	for _, e := range container["env"].([]any) {
		em := e.(map[string]any)
		envs[em["name"].(string)] = em["value"].(string)
	}
	for key, want := range map[string]string{"AGENT_ID": "a2", "PLATFORM_URL": "http://platform:8080", "MANIFEST_TOKEN": "tok-a2", "ONTOLOGY_MCP_URL": "http://onto:8091"} {
		if envs[key] != want {
			t.Fatalf("env %s=%q 不符（want %q）", key, envs[key], want)
		}
	}
	res := container["resources"].(map[string]any)
	limits := res["limits"].(map[string]any)
	if limits["memory"] != "512Mi" || limits["cpu"] != "1" {
		t.Fatalf("资源限制不符: %v", limits)
	}
	joined := strings.Join(callsOf(t, logPath), "\n")
	if !strings.Contains(joined, "delete") || !strings.Contains(joined, "apply") {
		t.Fatalf("启动路径应先 delete（覆盖式重建）再 apply:\n%s", joined)
	}
}

func TestK8sStopCleansForward(t *testing.T) {
	// port-forward 模式：Start 复用路径建立转发进程，Stop 应杀进程并删除 Pod
	bin, logPath, _ := stubKubectl(t, "Running", "127.0.0.1")
	k := &K8sBackend{Bin: bin} // 默认 port-forward 模式
	ep, err := k.Start(context.Background(), StartSpec{AgentID: "a3"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ep.URL, "http://127.0.0.1:") {
		t.Fatalf("port-forward 端点不符: %s", ep.URL)
	}
	port := strings.TrimPrefix(ep.URL, "http://127.0.0.1:")
	k.mu.Lock()
	cmd := k.forwards["a3"]
	k.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		t.Fatal("转发进程应存在")
	}
	time.Sleep(100 * time.Millisecond) // 给 stub 启动时间
	if err := k.Stop(context.Background(), "a3"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if cmd.ProcessState == nil {
		// ProcessState 在 Wait 后才非 nil；此处以信号成功为证——尝试再 Kill 报错即已退出
		if err := cmd.Process.Signal(os.Interrupt); err == nil {
			t.Fatal("Stop 后转发进程应已退出")
		}
	}
	joined := strings.Join(callsOf(t, logPath), "\n")
	if !strings.Contains(joined, "delete pod agt-a3") {
		t.Fatalf("Stop 应删除 Pod:\n%s", joined)
	}
	if strings.Contains(joined, port+":8080") == false {
		t.Fatalf("应建立 %s:8080 转发:\n%s", port, joined)
	}
}

// netListen8080 占用 127.0.0.1:8080（healthz 假服务用），被占用返回错误。
func netListen8080() (netListener, error) {
	return netListen("127.0.0.1:8080")
}
