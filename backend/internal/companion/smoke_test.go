package companion

import (
	"context"
	"encoding/json"
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

func smokeEngine(t *testing.T) *Engine {
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
	return NewEngine(bin, dir, 9198)
}

func TestGraphEngineSmoke(t *testing.T) {
	e := smokeEngine(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// ① 种子 schema 预置（幂等语义）
	if err := e.Update(ctx, SeedSchema()); err != nil {
		t.Fatalf("种子 schema 写入失败: %v", err)
	}
	if err := e.Update(ctx, SeedSchema()); err != nil {
		t.Fatalf("种子 schema 幂等重写失败: %v", err)
	}

	// ② 概念入图
	if err := e.Update(ctx, InsertNodeTriples("smoke1", "k1", "concept", "Pod 扩容", "副本水平伸缩", 0.86, "m1", testTime())); err != nil {
		t.Fatalf("概念入图失败: %v", err)
	}

	// ③ 关系入图（两端实体薄建）
	if err := e.Update(ctx, InsertRelationTriples("smoke1", "k2", "引发", "Pod 扩容", "HPA 调整", "", 0.8, "m2", testTime())); err != nil {
		t.Fatalf("关系入图失败: %v", err)
	}

	// ④ 图内容回读（triples + labels）
	raw, err := e.Query(ctx, SelectLabels("smoke1"))
	if err != nil {
		t.Fatalf("标签查询失败: %v", err)
	}
	// 种子 schema 在全局默认图（跨会话共享），会话 named graph 只含实例数据——labels 不应含种子类名
	// REQ-170 P2 起 SelectLabels 限定 bot:Concept：实体标签不含关系边名（引发=bot:Relation 的 rdfs:label）
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
		t.Fatalf("种子类不应落在会话图: %s", labels)
	}
	// 默认图 schema 存在性（幂等预置的目标位置）
	schemaRaw, err := e.Query(ctx, "SELECT ?c WHERE { ?c a <http://www.w3.org/2002/07/owl#Class> }")
	if err != nil {
		t.Fatalf("默认图 schema 查询失败: %v", err)
	}
	for _, cls := range []string{"thin/Concept", "thin/Relation", "thin/Event", "thin/Source", "thin/Agent"} {
		if !strings.Contains(string(schemaRaw), cls) {
			t.Fatalf("默认图缺种子类 %s: %s", cls, string(schemaRaw))
		}
	}

	// ④b REQ-195：引擎加载路径可观测——ResolvedBinary 登记 + Service.Status 透出 engine_detail
	if e.ResolvedBinary() == "" {
		t.Fatalf("写侧拉起后应登记实际二进制路径（REQ-195）")
	}
	stStore, err := openTestStore(t)
	if err != nil {
		t.Fatalf("建临时库失败: %v", err)
	}
	svc := NewService(stStore, nil, e)
	stMap, err := svc.StatusByAgent(ctx, "smoke1") // REQ-211：状态按 agent 图（此处 smoke1 即图键）
	if err != nil {
		t.Fatalf("Status 失败: %v", err)
	}
	ed, ok := stMap["engine_detail"].(map[string]any)
	if !ok || ed["binary"] == "" || ed["data_dir"] == "" || ed["endpoint"] == "" {
		t.Fatalf("Status 应透出 engine_detail(binary/data_dir/endpoint): %v", stMap["engine_detail"])
	}
	t.Logf("伴生引擎可观测: binary=%s data_dir=%s endpoint=%s", ed["binary"], ed["data_dir"], ed["endpoint"])

	// ⑤ 矛盾失效化：同主体+同关系名新边出现 → 旧边 invalidAt
	findRaw, err := e.Query(ctx, FindActiveEdge("smoke1", "Pod 扩容", "引发"))
	if err != nil {
		t.Fatalf("矛盾检测查询失败: %v", err)
	}
	oldEdge := parseEdgeURI(findRaw)
	if oldEdge == "" {
		t.Fatalf("应找到旧边（k2），结果为空: %s", string(findRaw))
	}
	if err := e.Update(ctx, InvalidateEdge("smoke1", oldEdge, testTime())); err != nil {
		t.Fatalf("旧边失效化失败: %v", err)
	}
	// 失效后不应再检出活动旧边
	findRaw2, err := e.Query(ctx, FindActiveEdge("smoke1", "Pod 扩容", "引发"))
	if err != nil {
		t.Fatalf("二次矛盾检测失败: %v", err)
	}
	if again := parseEdgeURI(findRaw2); again != "" {
		t.Fatalf("失效后不应再检出活动边: %s", again)
	}

	// ⑥ 整体摘除
	if err := e.Update(ctx, DropGraph("smoke1")); err != nil {
		t.Fatalf("DROP GRAPH 失败: %v", err)
	}
	raw2, err := e.Query(ctx, SelectGraphTriples("smoke1"))
	if err != nil {
		t.Fatalf("摘除后查询失败: %v", err)
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
		t.Fatalf("摘除后应无三元组: %d", len(res.Results.Bindings))
	}
	e.Stop()
}

// TestRetrievalVectorFallbackSmoke REQ-194②降级链真机冒烟：无 embedding 连接（Box=nil、
// 临时库无 embedding 默认连接）时向量路静默失败 → 词法兜底生效，match=lexical 如实标注；
// 2 跳邻域边（hop=2 链式文本）随命中实体注入上下文。
func TestRetrievalVectorFallbackSmoke(t *testing.T) {
	e := smokeEngine(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := e.Update(ctx, SeedSchema()); err != nil {
		t.Fatalf("种子 schema 失败: %v", err)
	}
	if err := e.Update(ctx, InsertNodeTriples("s1", "k1", "concept", "阿司匹林", "非甾体抗炎药", 0.9, "m1", testTime())); err != nil {
		t.Fatalf("概念入图失败: %v", err)
	}
	if err := e.Update(ctx, InsertRelationTriples("s1", "k2", "抑制", "阿司匹林", "前列腺素", "", 0.85, "m1", testTime())); err != nil {
		t.Fatalf("关系入图失败: %v", err)
	}
	if err := e.Update(ctx, InsertRelationTriples("s1", "k3", "预防", "前列腺素", "血栓形成", "", 0.8, "m2", testTime())); err != nil {
		t.Fatalf("二级关系入图失败: %v", err)
	}
	st, err := openTestStore(t)
	if err != nil {
		t.Fatalf("建临时库失败: %v", err)
	}
	svc := NewService(st, nil, e) // Box=nil → embedding 必失败 → 降级链
	conv := &store.Conversation{ID: "s1", Scope: "agent"}
	agt := &store.Agent{ID: "s1"} // REQ-211：召回作用域=agent 图
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
	svc.invalidateLabelCache("s1")
	e.Stop()
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
