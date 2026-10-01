package manager

// REQ-236/M63 运行平面健壮化——单测：
// ①迁移 006 默认执行方式翻转（D-O20 v0.73：出厂默认 k8s → docker）；
// ②nextPort 端口占用探测（被占端口跳过）；
// ③Reconcile 启动对账（存活引擎领养重建句柄 / 已死收敛 stopped）；
// ④RunningByOntology 确定性路由（同本体多方案取最早创建，created_at 并列按 id 兜底）。

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/engine"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/engine/oxigraph"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "rt.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// stubEngine HealthCheck 对任意 endpoint HTTP GET 200 即健康的假适配器。
type stubEngine struct{}

func (stubEngine) Start(_ context.Context, _ string, _ int, _ map[string]string) (*engine.Process, error) {
	return nil, fmt.Errorf("stub 不支持真实启动")
}
func (stubEngine) HealthCheck(_ context.Context, endpoint string) error {
	resp, err := http.Get(endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("status %s", resp.Status)
	}
	return nil
}

func TestMigrationsDefaultDocker(t *testing.T) {
	st := newStore(t)
	// 005 出厂默认 k8s → 006 应翻转为 docker（D-O20 v0.73：默认 docker，未装降级 native）
	if got := st.GetConfig().ExecutionMethod; got != "docker" {
		t.Fatalf("迁移 006 后默认执行方式应为 docker，得到 %q", got)
	}
}

func TestNextPortSkipsOccupied(t *testing.T) {
	st := newStore(t)
	// 占住 max+1 基线候选端口 → 分配应跳过它取下一个可 bind 端口（19201 系测试冷门段）
	ln, err := net.Listen("tcp", "127.0.0.1:19201")
	if err != nil {
		t.Skipf("19201 不可 bind（环境占用），跳过: %v", err)
	}
	defer ln.Close()
	got := nextPort(st)
	if got == 19201 {
		t.Fatalf("被占用端口 19201 应被探测跳过，得到 %d", got)
	}
	if got < 9201 {
		t.Fatalf("分配端口应递增基线之上，得到 %d", got)
	}
}

func TestReconcileAdoptsAliveConvergesDead(t *testing.T) {
	st := newStore(t)
	// 活引擎：真实 listener（HealthCheck GET 200）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	alive := &store.Profile{ID: "rt_alive", Name: "活方案", Engine: "oxigraph", OntologyIDs: []string{"onto_a"}, Config: "{}", Port: port}
	dead := &store.Profile{ID: "rt_dead", Name: "死方案", Engine: "oxigraph", OntologyIDs: []string{"onto_b"}, Config: "{}", Port: 9299}
	for _, p := range []*store.Profile{alive, dead} {
		if err := st.Create(p); err != nil {
			t.Fatal(err)
		}
		if err := st.SetStatus(p.ID, "running", "", "12345"); err != nil {
			t.Fatal(err)
		}
	}
	m := New(st, "http://127.0.0.1:1", t.TempDir(), t.TempDir())
	m.RegisterEngine("oxigraph", stubEngine{})

	m.Reconcile()

	// 活方案领养：句柄恢复（ProcEndpoint 可用；领养 native 不误触 docker 探测失败路径）
	ep, err := m.ProcEndpoint("rt_alive")
	if err != nil {
		t.Fatalf("存活引擎应被领养重建句柄: %v", err)
	}
	if ep != fmt.Sprintf("http://127.0.0.1:%d/query", port) {
		t.Fatalf("领养 endpoint 不符: %q", ep)
	}
	// 死方案收敛 stopped（非 error——重启不是方案故障）
	p, err := st.Get("rt_dead")
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "stopped" {
		t.Fatalf("失活运行态应收敛 stopped，得到 %q（last_error=%q）", p.Status, p.LastError)
	}
	if p.LastError == "" {
		t.Fatal("收敛应附对账语义 last_error")
	}
	// 未运行方案不受影响
	_ = oxigraph.DockerAvailable // 引用保持 import（DockerRunning 领养分支在无 docker 环境自然走 false）
}

func TestRunningByOntologyDeterministic(t *testing.T) {
	st := newStore(t)
	for _, p := range []*store.Profile{
		{ID: "rt_a_first", Name: "先建", Engine: "oxigraph", OntologyIDs: []string{"onto_x", "onto_y"}, Config: "{}"},
		{ID: "rt_b_later", Name: "后建", Engine: "oxigraph", OntologyIDs: []string{"onto_x"}, Config: "{}"},
	} {
		if err := st.Create(p); err != nil {
			t.Fatal(err)
		}
		if err := st.SetStatus(p.ID, "running", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.RunningByOntology("onto_x")
	if err != nil {
		t.Fatal(err)
	}
	// 同秒创建时按 id 字典序兜底：rt_a_first < rt_b_later，路由确定可复现
	if got.ID != "rt_a_first" {
		t.Fatalf("同本体多方案应路由最早创建（id 兜底序），得到 %q", got.ID)
	}
	if _, err := st.RunningByOntology("onto_y"); err != nil {
		t.Fatalf("第二本体路由失败: %v", err)
	}
}
