package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// 真机集成冒烟（gated：本机 data/bin/oxigraph 存在时运行；独立数据目录+端口，不碰运行平面）。
// 覆盖：懒启动 → 种子 schema → 概念/关系入图 → 矛盾失效化 → 图查询 → 整体摘除。

// smokeBase 启动独立 oxigraph 实例冒充「方案引擎」端点（REQ-216：伴生读写面已归一为
// SPARQL 标准 /query + /update——PlanEngines 只需 base URL，无需真跑 runtime-manager）。
func smokeBase(t *testing.T) string {
	t.Helper()
	bin := filepath.Join("..", "..", "..", "data", "bin", "oxigraph")
	if _, err := os.Stat(bin); err != nil {
		if _, err2 := os.Stat("data/bin/oxigraph"); err2 == nil {
			bin = "data/bin/oxigraph"
		} else {
			t.Skip("本机无 oxigraph 二进制，跳过真机冒烟")
		}
	}
	// 固定冒烟目录（非 TempDir——oxigraph 进程跨测试 run 残留时，TempDir 已被清理会致 IO 错）；
	// 启动前清掉同目录残留进程（孤儿复用会指向被删数据）与目录本身。
	dir := filepath.Join("..", "..", "..", "data", "companion-graph-smoke")
	_ = exec.Command("pkill", "-f", "companion-graph-smoke").Run()
	time.Sleep(300 * time.Millisecond)
	_ = os.RemoveAll(dir)
	t.Cleanup(func() {
		_ = exec.Command("pkill", "-f", "companion-graph-smoke").Run()
	})
	cmd := exec.Command(bin, "serve", "--location", dir, "--bind", "127.0.0.1:9198")
	if err := cmd.Start(); err != nil {
		t.Fatalf("冒烟 oxigraph 启动失败: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	base := "http://127.0.0.1:9198"
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		tr := &http.Client{Timeout: 2 * time.Second}
		req, _ := http.NewRequest("POST", base+"/query", strings.NewReader("SELECT * WHERE {} LIMIT 1"))
		req.Header.Set("Content-Type", "application/sparql-query")
		if resp, err := tr.Do(req); err == nil {
			_ = resp.Body.Close()
			return base
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("冒烟 oxigraph 健康等待超时")
	return ""
}

// smokePlans 构造指向冒烟引擎的 PlanEngines（预置端点缓存命中即不触达运行平面 REST）。
func smokePlans(base string) *PlanEngines {
	p := NewPlanEngines("http://127.0.0.1:1", "http://127.0.0.1:1")
	p.mu.Lock()
	p.eps["ont_smoke"] = base
	p.hostIDs["ont_smoke"] = "rt_smoke"
	p.mu.Unlock()
	return p
}

func TestGraphEngineSmoke(t *testing.T) {
	base := smokeBase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stStore, err := openTestStore(t)
	if err != nil {
		t.Fatalf("建临时库失败: %v", err)
	}
	if _, err := stStore.CreateAgent(&store.Agent{ID: "smoke-agt", Name: "smoke", CompanionOntologyID: "ont_smoke"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(stStore, nil, smokePlans(base))

	// ① 种子 schema 预置（幂等语义）
	if err := svc.graphUpdate(ctx, "ont_smoke", SeedSchema()); err != nil {
		t.Fatalf("种子 schema 写入失败: %v", err)
	}
	if err := svc.graphUpdate(ctx, "ont_smoke", SeedSchema()); err != nil {
		t.Fatalf("种子 schema 幂等重写失败: %v", err)
	}

	// ② 概念入图
	if err := svc.graphUpdate(ctx, "ont_smoke", InsertNodeTriples("ont_smoke", "k1", "concept", "Pod 扩容", "副本水平伸缩", "", 0.86, "m1", testTime())); err != nil {
		t.Fatalf("概念入图失败: %v", err)
	}

	// ③ 关系入图（两端实体薄建）
	if err := svc.graphUpdate(ctx, "ont_smoke", InsertRelationTriples("ont_smoke", "k2", "引发", "Pod 扩容", "HPA 调整", "", 0.8, "m2", testTime())); err != nil {
		t.Fatalf("关系入图失败: %v", err)
	}

	// ④ 图内容回读（triples + labels）
	raw, err := svc.graphQuery(ctx, "ont_smoke", SelectLabels("ont_smoke"))
	if err != nil {
		t.Fatalf("标签查询失败: %v", err)
	}
	// 种子 schema 在默认图（方案内共享），伴生 named graph 只含实例数据——labels 不应含种子类名
	// SelectLabels 限定 bot:Concept：实体标签不含关系边名（引发=bot:Relation 的 rdfs:label）
	labels := extractLabelsJSON(raw)
	for _, want := range []string{"Pod 扩容", "HPA 调整"} {
		if !strings.Contains(labels, want) {
			t.Fatalf("图中缺实体标签 %q: %s", want, labels)
		}
	}
	if strings.Contains(labels, "引发") {
		t.Fatalf("关系边名不应出现在实体标签: %s", labels)
	}
	if strings.Contains(labels, "概念") {
		t.Fatalf("种子类不应落在伴生子图: %s", labels)
	}

	// ④b REQ-216：Status 透出宿主方案可观测（plan id/端点）
	stMap, err := svc.StatusByAgent(ctx, "smoke-agt")
	if err != nil {
		t.Fatalf("Status 失败: %v", err)
	}
	if stMap["engine_running"] != true || stMap["graph"] != GraphURI("ont_smoke") {
		t.Fatalf("Status 应透出宿主方案运行态与本体伴生子图: %v", stMap)
	}
	plan, ok := stMap["plan"].(map[string]any)
	if !ok || plan["id"] != "rt_smoke" {
		t.Fatalf("Status 应透出宿主方案 plan.id: %v", stMap["plan"])
	}

	// ⑤ 矛盾失效化：同主体+同关系名新边出现 → 旧边 invalidAt
	findRaw, err := svc.graphQuery(ctx, "ont_smoke", FindActiveEdge("ont_smoke", "Pod 扩容", "引发"))
	if err != nil {
		t.Fatalf("矛盾检测查询失败: %v", err)
	}
	oldEdge := parseEdgeURI(findRaw)
	if oldEdge == "" {
		t.Fatalf("应找到旧边（k2），结果为空: %s", string(findRaw))
	}
	if err := svc.graphUpdate(ctx, "ont_smoke", InvalidateEdge("ont_smoke", oldEdge, testTime())); err != nil {
		t.Fatalf("旧边失效化失败: %v", err)
	}
	findRaw2, err := svc.graphQuery(ctx, "ont_smoke", FindActiveEdge("ont_smoke", "Pod 扩容", "引发"))
	if err != nil {
		t.Fatalf("二次矛盾检测失败: %v", err)
	}
	if again := parseEdgeURI(findRaw2); again != "" {
		t.Fatalf("失效后不应再检出活动边: %s", again)
	}

	// ⑥ 整体清空（本体级）
	if err := svc.ResetOntology(ctx, "ont_smoke"); err != nil {
		t.Fatalf("本体伴生子图清空失败: %v", err)
	}
	raw2, err := svc.graphQuery(ctx, "ont_smoke", SelectGraphTriples("ont_smoke"))
	if err != nil {
		t.Fatalf("清空后查询失败: %v", err)
	}
	var res struct {
		Results struct {
			Bindings []any `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw2, &res); err != nil {
		t.Fatalf("结果解析失败: %v", err)
	}
	if len(res.Results.Bindings) != 0 {
		t.Fatalf("清空后应无三元组: %d", len(res.Results.Bindings))
	}
	// ⑦ agent 解绑：绑定清空 + 候选游标清理，图不动（此处图已清，仅验绑定面）
	if err := svc.ResetAgent(ctx, "smoke-agt"); err != nil {
		t.Fatalf("agent 解绑失败: %v", err)
	}
	a, _ := stStore.GetAgent("smoke-agt")
	if a.CompanionOntologyID != "" || a.CompanionOntology {
		t.Fatalf("解绑后绑定应为空（派生开关随之关闭）: %+v", a)
	}
}

// TestRetrievalVectorFallbackSmoke REQ-194②降级链真机冒烟：无 embedding 连接（Box=nil、
// 临时库无 embedding 默认连接）时向量路静默失败 → 词法兜底生效，match=lexical 如实标注；
// 2 跳邻域边（hop=2 链式文本）随命中实体注入上下文。REQ-216：召回走本体伴生子图。
func TestRetrievalVectorFallbackSmoke(t *testing.T) {
	base := smokeBase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st, err := openTestStore(t)
	if err != nil {
		t.Fatalf("建临时库失败: %v", err)
	}
	if _, err := st.CreateAgent(&store.Agent{ID: "s-agt", Name: "s", CompanionOntologyID: "ont_smoke"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(st, nil, smokePlans(base)) // Box=nil → embedding 必失败 → 降级链
	if err := svc.graphUpdate(ctx, "ont_smoke", SeedSchema()); err != nil {
		t.Fatalf("种子 schema 失败: %v", err)
	}
	if err := svc.graphUpdate(ctx, "ont_smoke", InsertNodeTriples("ont_smoke", "k1", "concept", "阿司匹林", "非甾体抗炎药", "", 0.9, "m1", testTime())); err != nil {
		t.Fatalf("概念入图失败: %v", err)
	}
	if err := svc.graphUpdate(ctx, "ont_smoke", InsertRelationTriples("ont_smoke", "k2", "抑制", "阿司匹林", "前列腺素", "", 0.85, "m1", testTime())); err != nil {
		t.Fatalf("关系入图失败: %v", err)
	}
	if err := svc.graphUpdate(ctx, "ont_smoke", InsertRelationTriples("ont_smoke", "k3", "预防", "前列腺素", "血栓形成", "", 0.8, "m2", testTime())); err != nil {
		t.Fatalf("二级关系入图失败: %v", err)
	}
	conv := &store.Conversation{ID: "conv-s", Scope: "agent", AgentID: strPtr("s-agt")}
	agt, err := st.GetAgent("s-agt")
	if err != nil {
		t.Fatal(err)
	}
	text, entities, err := svc.RetrievalContext(ctx, conv, agt, "阿司匹林有什么作用？")
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if text == "" || len(entities) == 0 {
		t.Fatalf("词法兜底应命中阿司匹林: %q %v", text, entities)
	}
	if entities[0]["label"] != "阿司匹林" {
		t.Fatalf("命中实体不符: %v", entities[0])
	}
	if m, _ := entities[0]["match"].(string); m != "lexical" {
		t.Fatalf("降级链 match 应=lexical，got %v", entities[0]["match"])
	}
	// 2 跳链式边：阿司匹林 —抑制→ 前列腺素 —预防→ 血栓形成
	if !strings.Contains(text, "血栓形成") || !strings.Contains(text, "（2跳）") {
		t.Fatalf("2 跳邻域应注入链式边（血栓形成/2跳标注）: %s", text)
	}
	// 邻域限流 ≤8 边
	if rels, _ := entities[0]["relations"].([]map[string]any); len(rels) > 8 {
		t.Fatalf("邻域边应限流 ≤8，got %d", len(rels))
	}
	// 缓存失效防御：confirm 路径调 invalidateLabelCache 不 panic（图写入钩子）
	svc.invalidateLabelCache("ont_smoke")
}

func TestStoreCompanionRoundTrip(t *testing.T) {
	// 候选/游标存储回环（内存 SQLite）
	st, err := openTestStore(t)
	if err != nil {
		t.Fatalf("建临时库失败: %v", err)
	}
	cands := []*store.CompanionCandidate{
		{AgentID: "agt-a", Kind: "concept", Name: "滚动更新", Definition: "逐批替换实例", Confidence: 0.9, SourceMessageID: "m1"},
		{AgentID: "agt-a", Kind: "relation", Name: "部署", RelName: "部署", RelTarget: "Deployment", Confidence: 0.7, SourceMessageID: "m1"},
		{AgentID: "agt-b", Kind: "concept", Name: "金丝雀发布", Confidence: 0.6, SourceMessageID: "m2"},
	}
	if err := st.CreateCompanionCandidates(cands); err != nil {
		t.Fatalf("批量落库失败: %v", err)
	}
	if cands[0].ID == "" || cands[0].Status != "pending" {
		t.Fatalf("落库应补 ID 与 pending: %+v", cands[0])
	}
	list, err := st.ListCompanionCandidates("conv-x", "", "pending")
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	_ = list
	// 未过滤会话（上例 conv 未传）——直接按全量再查一次
	all, err := st.ListCompanionCandidates("", "", "")
	if err != nil {
		t.Fatalf("全量列表失败: %v", err)
	}
	if len(all) < 3 {
		t.Fatalf("应至少 3 条候选，got %d", len(all))
	}
	// REQ-193/M33：agent 维度过滤——跨会话铺平视图按 agent 拉取
	byA, err := st.ListCompanionCandidates("", "agt-a", "")
	if err != nil || len(byA) != 2 {
		t.Fatalf("agent 过滤应得 2 条: %v %d", err, len(byA))
	}
	byB, err := st.ListCompanionCandidates("", "agt-b", "pending")
	if err != nil || len(byB) != 1 || byB[0].Name != "金丝雀发布" {
		t.Fatalf("agent+status 复合过滤不符: %v %+v", err, byB)
	}
	got, err := st.DecideCompanionCandidate(cands[0].ID, "confirmed")
	if err != nil || got.Status != "confirmed" {
		t.Fatalf("裁决失败: %v %+v", err, got)
	}
	if _, err := st.DecideCompanionCandidate(cands[0].ID, "rejected"); err == nil {
		t.Fatalf("已裁决候选不应二次裁决")
	}
	if err := st.AdvanceCompanionCursor("conv-x", "agt-a", "m1"); err != nil {
		t.Fatalf("游标推进失败: %v", err)
	}
	cur, err := st.GetCompanionCursor("conv-x", "agt-a")
	if err != nil || cur.LastMessageID != "m1" {
		t.Fatalf("游标回读不符: %+v %v", cur, err)
	}
	if err := st.DeleteConversationCompanionData("conv-x"); err != nil {
		t.Fatalf("会话摘除失败: %v", err)
	}
	if cur2, _ := st.GetCompanionCursor("conv-x", "agt-a"); cur2.LastMessageID != "" {
		t.Fatalf("摘除后游标应清空: %+v", cur2)
	}
}

func openTestStore(t *testing.T) (*store.Store, error) {
	t.Helper()
	return store.Open(filepath.Join(t.TempDir(), "test.db"))
}
