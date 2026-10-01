package companion

import (
	"context"
	"fmt"
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

// REQ-227~229 单测：印证聚合 / 批内分位 / 同名异义 / 快照对账头。

// TestConfirmAggregation REQ-227①：同事实两候选先后确认 → 图内单边 confirmCount=2、
// 旧边不失效化；object 不同 → 走失效化+新边既有路径。
func TestConfirmAggregation(t *testing.T) {
	base := smokeBase(t)
	t.Setenv("COMPANION_SNAPSHOT_DIR", filepath.Join(t.TempDir(), "snaps"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st, err := openTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAgent(&store.Agent{ID: "agg-agt", Name: "agg", CompanionOntologyID: "ont_smoke"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(st, nil, smokePlans(base))
	cands := []*store.CompanionCandidate{
		{AgentID: "agg-agt", ConversationID: "c1", Kind: "relation", Name: "滚动更新", RelName: "引发", RelTarget: "HPA", Confidence: 0.9, SourceMessageID: "m1", Status: "pending"},
		{AgentID: "agg-agt", ConversationID: "c2", Kind: "relation", Name: "滚动更新", RelName: "引发", RelTarget: "HPA", Confidence: 0.85, SourceMessageID: "m2", Status: "pending"},
		{AgentID: "agg-agt", ConversationID: "c3", Kind: "relation", Name: "滚动更新", RelName: "引发", RelTarget: "回滚", Confidence: 0.8, SourceMessageID: "m3", Status: "pending"},
	}
	if err := st.CreateCompanionCandidates(cands); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmCandidate(ctx, cands[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmCandidate(ctx, cands[1].ID); err != nil {
		t.Fatal(err)
	}
	// 同事实：图内应单边 confirmCount=2
	raw, err := svc.graphQuery(ctx, "ont_smoke", SelectEdges("ont_smoke"))
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Results struct {
			Bindings []map[string]struct{ Value string `json:"value"` } `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	activeHPA := 0
	count := 0
	for _, b := range res.Results.Bindings {
		if b["dst"].Value == "HPA" {
			activeHPA++
			fmt.Sscanf(b["count"].Value, "%d", &count)
		}
	}
	if activeHPA != 1 || count != 2 {
		t.Fatalf("同事实应单边 confirmCount=2，got 边数=%d count=%d", activeHPA, count)
	}
	// object 不同：失效化旧边+新边（既有矛盾路径）
	if _, err := svc.ConfirmCandidate(ctx, cands[2].ID); err != nil {
		t.Fatal(err)
	}
	raw2, _ := svc.graphQuery(ctx, "ont_smoke", SelectEdges("ont_smoke"))
	var res2 struct {
		Results struct {
			Bindings []map[string]struct{ Value string `json:"value"` } `json:"bindings"`
		} `json:"results"`
	}
	_ = json.Unmarshal(raw2, &res2)
	dstSet := map[string]int{}
	for _, b := range res2.Results.Bindings {
		dstSet[b["dst"].Value]++
	}
	if dstSet["HPA"] != 0 || dstSet["回滚"] != 1 {
		t.Fatalf("异客体应失效化旧边仅剩新边: %v", dstSet)
	}
	// 审计落 onto_decision
	decs, err := st.ListDecisions("", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, d := range decs {
		if d.SubjectKind == "manual" && (containsSub(d.Title, "印证聚合") || containsSub(d.Title, "伴生确认入图")) {
			found++
		}
	}
	if found < 2 {
		t.Fatalf("审计应含聚合与确认决策行: %d", found)
	}
	svc.ResetOntology(ctx, "ont_smoke")
}

// TestMarkBatchRank REQ-227②：批内分位归一化 + 门控语义。
func TestMarkBatchRank(t *testing.T) {
	cands := []*store.CompanionCandidate{
		{Confidence: 0.9}, {Confidence: 0.5}, {Confidence: 0.7},
	}
	markBatchRank(cands)
	// 排序后 0.5→0, 0.7→0.5, 0.9→1
	for _, c := range cands {
		switch c.Confidence {
		case 0.5:
			if c.BatchRank != 0 {
				t.Fatalf("最低分位应为 0: %+v", c)
			}
		case 0.7:
			if c.BatchRank != 0.5 {
				t.Fatalf("中位分位应为 0.5: %+v", c)
			}
		case 0.9:
			if c.BatchRank != 1 {
				t.Fatalf("最高分位应为 1: %+v", c)
			}
		}
	}
	single := []*store.CompanionCandidate{{Confidence: 1.0}}
	markBatchRank(single)
	if single[0].BatchRank != 1 {
		t.Fatalf("单条应为 1")
	}
	// 门控：自评全虚高（都 1.0）时低分位被挡
	threshold := 0.9
	auto := 0
	for _, c := range []*store.CompanionCandidate{{Confidence: 1.0, BatchRank: 0}, {Confidence: 1.0, BatchRank: 0.5}, {Confidence: 1.0, BatchRank: 1}} {
		if c.Confidence >= threshold && c.BatchRank >= 0.5 {
			auto++
		}
	}
	if auto != 2 {
		t.Fatalf("分位门控应只放行一半: %d", auto)
	}
}

// TestDisambiguationNote REQ-229①：同名实体定义相似度低 → 候选注记。
func TestDisambiguationNote(t *testing.T) {
	base := smokeBase(t)
	t.Setenv("COMPANION_SNAPSHOT_DIR", filepath.Join(t.TempDir(), "snaps"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st, err := openTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAgent(&store.Agent{ID: "dis-agt", Name: "dis", CompanionOntologyID: "ont_smoke"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(st, nil, smokePlans(base))
	// 图内已有同名实体（定义 A）
	if err := svc.graphUpdate(ctx, "ont_smoke", InsertNodeTriples("ont_smoke", "k0", "concept", "部署", "将应用发布到集群运行的过程", "", 0.9, "m0", testTime())); err != nil {
		t.Fatal(err)
	}
	// 新候选同名但定义完全不同
	c := &store.CompanionCandidate{AgentID: "dis-agt", ConversationID: "c1", Kind: "concept", Name: "部署", Definition: "军队调动安排兵力分布", Confidence: 0.8, SourceMessageID: "m1", Status: "pending"}
	if err := st.CreateCompanionCandidates([]*store.CompanionCandidate{c}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmCandidate(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetCompanionCandidate(c.ID)
	if !containsSub(got.Note, "同名异义疑似") {
		t.Fatalf("应带同名异义注记: %q", got.Note)
	}
	// 纯函数自检：相似定义不触发
	if jaccardBigram("将应用发布到集群运行的过程", "把应用发布到集群里运行的过程") < 0.2 {
		t.Fatal("近似定义相似度不应低于阈值")
	}
	svc.ResetOntology(ctx, "ont_smoke")
}

// TestSnapshotEnvelopeCorruptDetect REQ-227④：对账不一致的快照不回灌且可识别错误。
func TestSnapshotEnvelopeCorruptDetect(t *testing.T) {
	// 信封 count=3 但 results 只有 2 绑定 → 解析报对账错误
	env := []byte(`{"version":1,"count":3,"results":{"results":{"bindings":[
		{"s":{"type":"uri","value":"urn:a"},"p":{"type":"uri","value":"urn:b"},"o":{"type":"literal","value":"x"}},
		{"s":{"type":"uri","value":"urn:c"},"p":{"type":"uri","value":"urn:d"},"o":{"type":"literal","value":"y"}}
	]}}}`)
	_, err := parseSnapshotFile(env)
	if err == nil || !containsSub(err.Error(), "对账不一致") {
		t.Fatalf("对账不一致应报错: %v", err)
	}
	// 旧裸数组格式兼容
	legacy := []byte(`{"results":{"bindings":[{"s":{"type":"uri","value":"urn:a"},"p":{"type":"uri","value":"urn:b"},"o":{"type":"literal","value":"x"}}]}}`)
	triples, err := parseSnapshotFile(legacy)
	if err != nil || len(triples) != 1 {
		t.Fatalf("旧格式应兼容: %v %d", err, len(triples))
	}
}

func containsSub(hay, needle string) bool {
	return len(hay) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(hay); i++ {
			if hay[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
