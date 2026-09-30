package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// REQ-216 增量轮（2026-09-30 复查）单测：快照持久化与重建回灌 / Ensure 单飞化。

// TestSnapshotReinflationAfterWipe 增量①决定性验证：确认入图 → 快照落盘 → 模拟方案重建
// （DROP 子图 = Start RemoveAll 的等价结果）→ 读路径访问自快照无损回灌（含 rdfs:label
// 等全部三元组）。快照目录走 env 覆盖（不污染仓库 data/）。
func TestSnapshotReinflationAfterWipe(t *testing.T) {
	base := smokeBase(t)
	t.Setenv("COMPANION_SNAPSHOT_DIR", filepath.Join(t.TempDir(), "snaps"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st, err := openTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAgent(&store.Agent{ID: "snap-agt", Name: "snap", CompanionOntologyID: "ont_smoke"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(st, nil, smokePlans(base))

	// ① 确认入图（写路径含矛盾检测查询与 INSERT）
	cands := []*store.CompanionCandidate{
		{AgentID: "snap-agt", ConversationID: "c1", Kind: "concept", Name: "滚动更新", Definition: "逐批替换", Confidence: 0.9, SourceMessageID: "m1", Status: "pending"},
		{AgentID: "snap-agt", ConversationID: "c1", Kind: "relation", Name: "滚动更新", RelName: "引发", RelTarget: "HPA", Confidence: 0.8, SourceMessageID: "m1", Status: "pending"},
	}
	if err := st.CreateCompanionCandidates(cands); err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		if _, err := svc.ConfirmCandidate(ctx, c.ID); err != nil {
			t.Fatalf("确认入图失败: %v", err)
		}
	}
	// 写路径已刷快照
	if _, err := os.Stat(snapshotPath("ont_smoke")); err != nil {
		t.Fatalf("确认入图后应落快照: %v", err)
	}

	// ② 模拟方案重建：引擎数据目录被 RemoveAll 后伴生子图为空（DROP 等价）
	if err := svc.graphUpdate(ctx, "ont_smoke", DropGraph("ont_smoke")); err != nil {
		t.Fatal(err)
	}
	svc.infMu.Lock()
	svc.inflatedBase = map[string]string{} // 模拟 backend 进程重启（标记清零）
	svc.infMu.Unlock()

	// ③ 读路径访问 → 重建检测 → 自快照回灌
	labels, err := svc.graphQuery(ctx, "ont_smoke", SelectLabels("ont_smoke"))
	if err != nil {
		t.Fatalf("回灌后查询失败: %v", err)
	}
	got := extractLabelsJSON(labels)
	for _, want := range []string{"滚动更新", "HPA"} {
		if !containsStr(got, want) {
			t.Fatalf("回灌后缺实体 %q: %s", want, got)
		}
	}
	// 二次访问不重复回灌（集合语义幂等，计数应稳定）
	n1 := svc.countGraphTriples(ctx, "ont_smoke", base)
	n2 := svc.countGraphTriples(ctx, "ont_smoke", base)
	if n1 != n2 {
		t.Fatalf("回灌应幂等（集合语义），计数漂移 %d → %d", n1, n2)
	}

	// ④ 本体级清空 → 快照同删（清空后的重建不再复活数据）
	if err := svc.ResetOntology(ctx, "ont_smoke"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(snapshotPath("ont_smoke")); !os.IsNotExist(err) {
		t.Fatalf("本体级清空后快照应删除: %v", err)
	}
}

func containsStr(hay, needle string) bool {
	return len(hay) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(hay); i++ {
			if hay[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// TestPlanEnginesSingleflight 增量④：同本体并发 Ensure 只建一个宿主方案（每本体串行锁）。
func TestPlanEnginesSingleflight(t *testing.T) {
	var createCount, listCount atomic.Int64
	var mu sync.Mutex
	created := false
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/runtime-profiles", func(w http.ResponseWriter, r *http.Request) {
		listCount.Add(1)
		mu.Lock()
		defer mu.Unlock()
		profiles := []any{}
		if created {
			profiles = append(profiles, map[string]any{"id": "rt_x", "name": "伴生·X", "engine": "oxigraph", "ontology_ids": []string{"ont_x"}, "port": 9331, "status": "running"})
		}
		_ = json.NewEncoder(w).Encode(profiles)
	})
	mux.HandleFunc("POST /api/runtime-profiles", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if created {
			mu.Unlock()
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"name conflict"}`))
			return
		}
		created = true
		mu.Unlock()
		createCount.Add(1)
		time.Sleep(50 * time.Millisecond) // 放大竞态窗口
		_, _ = w.Write([]byte(`{"id":"rt_x","name":"伴生·X","engine":"oxigraph","ontology_ids":["ont_x"],"port":0,"status":"created"}`))
	})
	mux.HandleFunc("GET /api/ontologies/ont_x", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"ont_x","name":"X"}`))
	})
	mux.HandleFunc("POST /api/runtime-profiles/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"rt_x","status":"running","port":9331}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := NewPlanEngines(srv.URL, srv.URL)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = p.EnsureHostPlan(context.Background(), "ont_x")
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("并发 Ensure 第 %d 路失败: %v", i, err)
		}
	}
	if n := createCount.Load(); n != 1 {
		t.Fatalf("同本体并发 Ensure 应只创建 1 个宿主方案，got %d", n)
	}
}

// TestOnOntologyDeletedCleansSnapshot 增量②b：本体删除后快照与缓存清理。
func TestOnOntologyDeletedCleansSnapshot(t *testing.T) {
	t.Setenv("COMPANION_SNAPSHOT_DIR", filepath.Join(t.TempDir(), "snaps"))
	st, err := openTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(st, nil, NewPlanEngines("http://127.0.0.1:1", "http://127.0.0.1:1"))
	if err := os.MkdirAll(snapshotDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshotPath("ont_del"), []byte(`{"results":{"bindings":[]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	svc.Plans.mu.Lock()
	svc.Plans.eps["ont_del"] = "http://127.0.0.1:1"
	svc.Plans.mu.Unlock()
	svc.OnOntologyDeleted("ont_del")
	if _, err := os.Stat(snapshotPath("ont_del")); !os.IsNotExist(err) {
		t.Fatalf("删除后快照应清理: %v", err)
	}
	svc.Plans.mu.Lock()
	_, cached := svc.Plans.eps["ont_del"]
	svc.Plans.mu.Unlock()
	if cached {
		t.Fatal("删除后端点缓存应失效")
	}
}
