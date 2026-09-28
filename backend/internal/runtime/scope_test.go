// 10c per-run 作用域单测（stub docker CLI，零外部依赖）：
// run 域容器命名（agt-{agent}-r-{run8}）、Start 不复用（对账跳过）、Stop 定位 run 实例、
// agent 域复用回归、SANDBOX_SCOPE 环境解析。
package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubDockerCLI 写一个假 docker 脚本：记录全部调用到 REC，inspect 恒报 running、
// port 恒回 STUB_PORT（指向 httptest healthz）。返回 bin 路径与 REC 内容读取函数。
func stubDockerCLI(t *testing.T) (bin string, rec func() string) {
	t.Helper()
	dir := t.TempDir()
	recPath := filepath.Join(dir, "calls.log")
	portFile := filepath.Join(dir, "port")
	script := filepath.Join(dir, "docker")
	code := "#!/bin/sh\necho \"$@\" >> " + recPath + "\n" +
		"if [ \"$1\" = \"inspect\" ]; then echo running; exit 0; fi\n" +
		"if [ \"$1\" = \"port\" ]; then cat " + portFile + "; exit 0; fi\n" +
		"if [ \"$1\" = \"rm\" ]; then exit 0; fi\n" +
		"echo stubcid; exit 0\n"
	if err := os.WriteFile(script, []byte(code), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, func() string {
		b, _ := os.ReadFile(recPath)
		return string(b)
	}
}

func TestScopeEnv(t *testing.T) {
	t.Setenv("SANDBOX_SCOPE", "")
	if got := Scope(); got != "agent" {
		t.Errorf("缺省应 agent，得到 %s", got)
	}
	t.Setenv("SANDBOX_SCOPE", "run")
	if got := Scope(); got != "run" {
		t.Errorf("run 应生效，得到 %s", got)
	}
	t.Setenv("SANDBOX_SCOPE", "garbage")
	if got := Scope(); got != "agent" {
		t.Errorf("非法值应回退 agent，得到 %s", got)
	}
}

func TestDockerInstanceName(t *testing.T) {
	d := &DockerBackend{}
	t.Setenv("SANDBOX_SCOPE", "run")
	if got := d.instanceName(StartSpec{AgentID: "a1", RunID: "0123456789abcdef"}); got != "agt-a1-r-01234567" {
		t.Errorf("run 域命名 = %s", got)
	}
	if got := d.instanceName(StartSpec{AgentID: "a1"}); got != "agt-a1" {
		t.Errorf("run 域但无 RunID 应回退 agent 域，得到 %s", got)
	}
	t.Setenv("SANDBOX_SCOPE", "agent")
	if got := d.instanceName(StartSpec{AgentID: "a1", RunID: "0123456789"}); got != "agt-a1" {
		t.Errorf("agent 域应忽略 RunID，得到 %s", got)
	}
}

// run 域 Start：对账不复用（inspect 报 running 仍建新容器）；Stop 定位 run 实例。
func TestDockerRunScopeLifecycle(t *testing.T) {
	// httptest 充当 agentd healthz（stub port 输出指向它，让 Start 走完健康检查）
	hz := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer hz.Close()
	port := strings.TrimPrefix(hz.URL, "http://127.0.0.1:")

	bin, rec := stubDockerCLI(t)
	t.Setenv("DOCKER_BIN", bin) // bin 解析走 env（Podman 兼容同路径）
	if err := os.WriteFile(filepath.Join(filepath.Dir(bin), "port"), []byte("0.0.0.0:"+port), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SANDBOX_SCOPE", "run")
	d := &DockerBackend{PlatformURL: "http://platform", TokenIssue: func(string) (string, error) { return "tok", nil }}

	spec := StartSpec{AgentID: "a1", RunID: "0123456789abcdef"}
	ep, err := d.Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("run 域 Start 失败: %v", err)
	}
	if ep.URL != "http://127.0.0.1:"+port {
		t.Errorf("端点 = %s", ep.URL)
	}
	calls := rec()
	if !strings.Contains(calls, "run -d --name agt-a1-r-01234567") {
		t.Errorf("run 域应建新容器（不复用），调用记录: %s", calls)
	}
	if err := d.Stop(context.Background(), StopSpec{AgentID: "a1", RunID: "0123456789abcdef"}); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !strings.Contains(rec(), "rm -f agt-a1-r-01234567") {
		t.Errorf("Stop 应清理 run 实例: %s", rec())
	}
}

// agent 域回归：running 即复用（不 run -d）；Stop 清 agent 实例。
func TestDockerAgentScopeReuseRegression(t *testing.T) {
	hz := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer hz.Close()
	port := strings.TrimPrefix(hz.URL, "http://127.0.0.1:")

	bin, rec := stubDockerCLI(t)
	t.Setenv("DOCKER_BIN", bin)
	if err := os.WriteFile(filepath.Join(filepath.Dir(bin), "port"), []byte("0.0.0.0:"+port), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SANDBOX_SCOPE", "agent")
	d := &DockerBackend{PlatformURL: "http://platform", TokenIssue: func(string) (string, error) { return "tok", nil }}

	ep, err := d.Start(context.Background(), StartSpec{AgentID: "a1"})
	if err != nil {
		t.Fatalf("agent 域 Start: %v", err)
	}
	if ep.URL != "http://127.0.0.1:"+port {
		t.Errorf("端点 = %s", ep.URL)
	}
	if strings.Contains(rec(), " run ") {
		t.Errorf("复用路径不应建新容器: %s", rec())
	}
	if err := d.Stop(context.Background(), StopSpec{AgentID: "a1"}); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !strings.Contains(rec(), "rm -f agt-a1\n") {
		t.Errorf("Stop 应清理 agent 实例: %s", rec())
	}
}

// K8s 后端同名规则（10d 桩测试的 10c 增量）：run 域 pod 名。
func TestK8sInstanceName(t *testing.T) {
	k := &K8sBackend{}
	t.Setenv("SANDBOX_SCOPE", "run")
	if got := k.instanceName(StartSpec{AgentID: "a1", RunID: "0123456789abcdef"}); got != "agt-a1-r-01234567" {
		t.Errorf("k8s run 域 pod 名 = %s", got)
	}
	if got := k.instanceName(StartSpec{AgentID: "a1"}); got != "agt-a1" {
		t.Errorf("k8s 无 RunID 应回退 agent 域，得到 %s", got)
	}
}
