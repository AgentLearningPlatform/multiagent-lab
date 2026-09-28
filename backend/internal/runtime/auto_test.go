package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBackend AutoBackend 单测桩：可控行为的最小 Backend+Prober 实现。
type fakeBackend struct {
	name     string
	avail    bool
	startErr error
	starts   int
	stops    int
}

func (f *fakeBackend) Name() string { return f.name }
func (f *fakeBackend) Available(ctx context.Context) bool {
	return f.avail
}
func (f *fakeBackend) Start(ctx context.Context, spec StartSpec) (Endpoint, error) {
	f.starts++
	if f.startErr != nil {
		return Endpoint{}, f.startErr
	}
	return Endpoint{URL: "http://" + f.name}, nil
}
func (f *fakeBackend) Stop(ctx context.Context, spec StopSpec) error { f.stops++; return nil }
func (f *fakeBackend) Status(ctx context.Context, agentID string) (BackendStatus, error) {
	return BackendStatus{State: "running", Detail: "ok"}, nil
}

func TestAutoPickPriority(t *testing.T) {
	k8s := &fakeBackend{name: "k8s", avail: true}
	docker := &fakeBackend{name: "docker", avail: true}
	a := &AutoBackend{Candidates: []Backend{k8s, docker}}
	ep, err := a.Start(context.Background(), StartSpec{AgentID: "a1"})
	if err != nil {
		t.Fatal(err)
	}
	if ep.URL != "http://k8s" {
		t.Fatalf("k8s 优先：应委派 k8s，got %s", ep.URL)
	}
	if a.Name() != "k8s" || k8s.starts != 1 || docker.starts != 0 {
		t.Fatalf("粘滞与委派不符: name=%s k8s=%d docker=%d", a.Name(), k8s.starts, docker.starts)
	}
}

func TestAutoFallbackToDocker(t *testing.T) {
	k8s := &fakeBackend{name: "k8s", avail: false}
	docker := &fakeBackend{name: "docker", avail: true}
	a := &AutoBackend{Candidates: []Backend{k8s, docker}}
	if !a.Available(context.Background()) {
		t.Fatal("docker 可用时应 Available=true")
	}
	if a.Name() != "docker" {
		t.Fatalf("k8s 不可用应换档 docker，got %s", a.Name())
	}
}

func TestAutoNoSandboxFallsToInprocess(t *testing.T) {
	a := &AutoBackend{Candidates: []Backend{
		&fakeBackend{name: "k8s", avail: false},
		&fakeBackend{name: "docker", avail: false},
	}}
	if a.Available(context.Background()) {
		t.Fatal("均不可用时应 Available=false")
	}
	if _, err := a.Start(context.Background(), StartSpec{AgentID: "a1"}); !errors.Is(err, ErrNoSandbox) {
		t.Fatalf("应返回 ErrNoSandbox，got %v", err)
	}
}

func TestAutoStickySwapOnFailure(t *testing.T) {
	k8s := &fakeBackend{name: "k8s", avail: true}
	docker := &fakeBackend{name: "docker", avail: true}
	a := &AutoBackend{Candidates: []Backend{k8s, docker}}
	if b := a.pick(context.Background()); b.Name() != "k8s" {
		t.Fatalf("初选应 k8s，got %s", b.Name())
	}
	k8s.avail = false // 集群下线
	if b := a.pick(context.Background()); b.Name() != "docker" {
		t.Fatalf("粘滞失效应换档 docker，got %s", b.Name())
	}
	k8s.avail = true
	docker.avail = false
	if b := a.pick(context.Background()); b.Name() != "k8s" {
		t.Fatalf("docker 失效应换回 k8s，got %s", b.Name())
	}
}

func TestAutoStatusPrefixesBackendName(t *testing.T) {
	a := &AutoBackend{Candidates: []Backend{&fakeBackend{name: "k8s", avail: true}}}
	st, err := a.Status(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(st.Detail, "k8s: ") {
		t.Fatalf("detail 应带形态前缀，got %q", st.Detail)
	}
}

func TestAutoStopDelegatesAllCandidates(t *testing.T) {
	k8s := &fakeBackend{name: "k8s", avail: true}
	docker := &fakeBackend{name: "docker", avail: false}
	a := &AutoBackend{Candidates: []Backend{k8s, docker}}
	if err := a.Stop(context.Background(), StopSpec{AgentID: "a1"}); err != nil {
		t.Fatal(err)
	}
	if k8s.stops != 1 || docker.stops != 1 {
		t.Fatalf("Stop 应双删（幂等）：k8s=%d docker=%d", k8s.stops, docker.stops)
	}
}

// scriptBin 生成一个按调用参数输出固定内容的桩脚本（kubectl/docker 探测用）。
func scriptBin(t *testing.T, name, match, out string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	script := "#!/bin/sh\nfor a in \"$@\"; do if [ \"$a\" = \"" + match + "\" ]; then echo " + out + "; exit 0; fi; done\nexit 1\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestK8sAvailableProbe(t *testing.T) {
	okBin := scriptBin(t, "kubectl", "--raw=/readyz", "ok")
	if !(&K8sBackend{Bin: okBin}).Available(context.Background()) {
		t.Fatal("readyz ok 应 Available=true")
	}
	badBin := scriptBin(t, "kubectl", "nothing-matches", "")
	if (&K8sBackend{Bin: badBin}).Available(context.Background()) {
		t.Fatal("集群不可达应 Available=false")
	}
}

func TestDockerAvailableProbe(t *testing.T) {
	okBin := scriptBin(t, "docker", "--format", "29.8.0")
	if !(&DockerBackend{Bin: okBin}).Available(context.Background()) {
		t.Fatal("daemon ping ok 应 Available=true")
	}
	badBin := scriptBin(t, "docker", "nothing-matches", "")
	if (&DockerBackend{Bin: badBin}).Available(context.Background()) {
		t.Fatal("daemon 不可达应 Available=false")
	}
}
