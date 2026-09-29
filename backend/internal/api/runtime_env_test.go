// REQ-191/M31 运行环境统一配置——api 层单测：
// env 兜底/DB 覆盖合并、Build 各形态字段、resolver 缓存重建、GET/PUT/test handlers。
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/runtime"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func newRuntimeEnvFixture(t *testing.T) (*store.Store, *RuntimeEnv) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	env := &RuntimeEnv{
		Store:      st,
		TokenIssue: func(string) (string, error) { return "tok", nil },
		Defaults: store.RuntimeSettings{
			SandboxMode:          "docker",
			SandboxImage:         "agentd:dev",
			SandboxScope:         "agent",
			DockerBin:            "",
			K8sContext:           "env-ctx",
			PlatformURLExternal:  "http://host.docker.internal:8080",
			PlatformURLInCluster: "",
		},
	}
	return st, env
}

func TestRuntimeEnvMergeOverEnv(t *testing.T) {
	st, env := newRuntimeEnvFixture(t)
	// ① DB 全空 → env 兜底（mode/image/scope 来自 Defaults）
	eff := env.Effective()
	if eff.SandboxMode != "docker" || eff.SandboxImage != "agentd:dev" || eff.K8sContext != "env-ctx" {
		t.Fatalf("空表应回落 env：%+v", eff)
	}
	// ② DB 覆盖：mode=k8s、context 覆盖，未配置字段仍回落 env
	if err := st.SaveRuntimeSettings(&store.RuntimeSettings{SandboxMode: "k8s", K8sContext: "db-ctx", K8sKubeconfig: "/kc"}); err != nil {
		t.Fatal(err)
	}
	eff = env.Effective()
	if eff.SandboxMode != "k8s" || eff.K8sContext != "db-ctx" || eff.K8sKubeconfig != "/kc" {
		t.Fatalf("DB 应覆盖 env：%+v", eff)
	}
	if eff.SandboxImage != "agentd:dev" || eff.PlatformURLExternal != "http://host.docker.internal:8080" {
		t.Fatalf("未配置字段应回落 env：%+v", eff)
	}
	// ③ inprocess 覆盖 → 未启用
	if err := st.SaveRuntimeSettings(&store.RuntimeSettings{SandboxMode: "inprocess"}); err != nil {
		t.Fatal(err)
	}
	if env.Effective().SandboxMode != "inprocess" {
		t.Fatal("inprocess 应生效")
	}
}

func TestRuntimeEnvBuildShapes(t *testing.T) {
	_, env := newRuntimeEnvFixture(t)
	// inprocess / 空镜像 → nil（未启用沙箱）
	if b := env.Build(store.RuntimeSettings{SandboxMode: "inprocess", SandboxImage: "agentd:dev"}); b != nil {
		t.Fatal("inprocess 应返回 nil")
	}
	if b := env.Build(store.RuntimeSettings{SandboxMode: "docker"}); b != nil {
		t.Fatal("镜像空应返回 nil")
	}
	// docker 形态字段
	b := env.Build(store.RuntimeSettings{SandboxMode: "docker", SandboxImage: "img", SandboxScope: "run", DockerBin: "/bin/docker", PlatformURLExternal: "http://p:8080"})
	d, ok := b.(*runtime.DockerBackend)
	if !ok {
		t.Fatalf("应构造 DockerBackend，得到 %T", b)
	}
	if d.Image != "img" || d.Scope != "run" || d.Bin != "/bin/docker" || d.PlatformURL != "http://p:8080" {
		t.Fatalf("docker 字段不符：%+v", d)
	}
	// k8s 形态字段（含 kubeconfig；in-cluster 缺省回退 external）
	b = env.Build(store.RuntimeSettings{SandboxMode: "k8s", SandboxImage: "img", K8sKubeconfig: "/kc", K8sContext: "c", K8sNamespace: "n", K8sEndpointMode: "pod-ip", PlatformURLExternal: "http://p:8080"})
	k, ok := b.(*runtime.K8sBackend)
	if !ok {
		t.Fatalf("应构造 K8sBackend，得到 %T", b)
	}
	if k.Kubeconfig != "/kc" || k.Context != "c" || k.Namespace != "n" || k.EndpointMode != "pod-ip" || k.PlatformURL != "http://p:8080" || k.Scope != "" {
		t.Fatalf("k8s 字段不符：%+v", k)
	}
	// auto 形态：双候选 k8s→docker
	b = env.Build(store.RuntimeSettings{SandboxMode: "auto", SandboxImage: "img"})
	a, ok := b.(*runtime.AutoBackend)
	if !ok || len(a.Candidates) != 2 {
		t.Fatalf("auto 应构造双候选 AutoBackend：%T", b)
	}
	if _, isK := a.Candidates[0].(*runtime.K8sBackend); !isK {
		t.Fatalf("候选 0 应为 k8s：%T", a.Candidates[0])
	}
	if _, isD := a.Candidates[1].(*runtime.DockerBackend); !isD {
		t.Fatalf("候选 1 应为 docker：%T", a.Candidates[1])
	}
	// env 缺省语义：mode 空 + 镜像非空 = docker（存量 SANDBOX_BACKEND 缺省行为）
	if b := env.Build(store.RuntimeSettings{SandboxImage: "img"}); b == nil {
		t.Fatal("mode 空且镜像非空应缺省 docker 启用")
	}
}

func TestRuntimeEnvCurrentCache(t *testing.T) {
	st, env := newRuntimeEnvFixture(t)
	b1 := env.Current()
	if b2 := env.Current(); b1 != b2 {
		t.Fatal("配置未变应复用实例（auto 粘滞态保留）")
	}
	if err := st.SaveRuntimeSettings(&store.RuntimeSettings{SandboxMode: "inprocess"}); err != nil {
		t.Fatal(err)
	}
	if b3 := env.Current(); b3 != nil {
		t.Fatalf("配置变更应重建（inprocess→nil），得到 %T", b3)
	}
}

func TestRuntimeEnvHandlers(t *testing.T) {
	st, env := newRuntimeEnvFixture(t)
	s := &Server{Store: st, RuntimeEnv: env}

	// GET：settings(DB 原值)/effective/defaults/sandbox_enabled
	w := httptest.NewRecorder()
	s.runtimeEnvGet(w, httptest.NewRequest("GET", "/api/runtime-env", nil))
	if w.Code != 200 {
		t.Fatalf("GET 应 200：%d", w.Code)
	}
	var got struct {
		Settings       store.RuntimeSettings `json:"settings"`
		Effective      store.RuntimeSettings `json:"effective"`
		SandboxEnabled bool                  `json:"sandbox_enabled"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Settings.SandboxMode != "" || got.Effective.SandboxMode != "docker" || !got.SandboxEnabled {
		t.Fatalf("GET 结构不符：%+v", got)
	}

	// PUT：合法保存 + 非法枚举 400
	body, _ := json.Marshal(store.RuntimeSettings{SandboxMode: "k8s", K8sKubeconfig: "/kc", SandboxScope: "run"})
	w = httptest.NewRecorder()
	s.runtimeEnvPut(w, httptest.NewRequest("PUT", "/api/runtime-env", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("PUT 应 200：%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.runtimeEnvPut(w, httptest.NewRequest("PUT", "/api/runtime-env", bytes.NewReader([]byte(`{"sandbox_mode":"bogus"}`))))
	if w.Code != 400 {
		t.Fatalf("非法 mode 应 400：%d", w.Code)
	}
	w = httptest.NewRecorder()
	s.runtimeEnvPut(w, httptest.NewRequest("PUT", "/api/runtime-env", bytes.NewReader([]byte(`{"sandbox_mode":"docker","k8s_endpoint_mode":"bogus"}`))))
	if w.Code != 400 {
		t.Fatalf("非法 endpoint_mode 应 400：%d", w.Code)
	}

	// test：k8s 目标走合并配置探测（无集群环境应诚实 ok=false，不报 panic/5xx）
	body, _ = json.Marshal(map[string]string{"target": "k8s"})
	w = httptest.NewRecorder()
	s.runtimeEnvTest(w, httptest.NewRequest("POST", "/api/runtime-env/test", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("test 应 200：%d %s", w.Code, w.Body.String())
	}
	var tr struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &tr)
	if tr.Detail == "" {
		t.Fatal("test 应附 detail")
	}
	// 非法 target 400
	body, _ = json.Marshal(map[string]string{"target": "podman"})
	w = httptest.NewRecorder()
	s.runtimeEnvTest(w, httptest.NewRequest("POST", "/api/runtime-env/test", bytes.NewReader(body)))
	if w.Code != 400 {
		t.Fatalf("非法 target 应 400：%d", w.Code)
	}
	_ = context.Background()
}
