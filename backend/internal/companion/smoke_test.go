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
	labels := extractLabelsJSON(raw)
	for _, want := range []string{"Pod 扩容", "HPA 调整", "引发"} {
		if !strings.Contains(labels, want) {
			t.Fatalf("图中缺标签 %q: %s", want, labels)
		}
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

func TestStoreCompanionRoundTrip(t *testing.T) {
	// 候选/游标存储回环（内存 SQLite）
	st, err := openTestStore(t)
	if err != nil {
		t.Fatalf("建临时库失败: %v", err)
	}
	cands := []*store.CompanionCandidate{
		{Kind: "concept", Name: "滚动更新", Definition: "逐批替换实例", Confidence: 0.9, SourceMessageID: "m1"},
		{Kind: "relation", Name: "部署", RelName: "部署", RelTarget: "Deployment", Confidence: 0.7, SourceMessageID: "m1"},
	}
	if err := st.CreateCompanionCandidates(cands); err != nil {
		t.Fatalf("批量落库失败: %v", err)
	}
	if cands[0].ID == "" || cands[0].Status != "pending" {
		t.Fatalf("落库应补 ID 与 pending: %+v", cands[0])
	}
	list, err := st.ListCompanionCandidates("conv-x", "pending")
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	_ = list
	// 未过滤会话（上例 conv 未传）——直接按全量再查一次
	all, err := st.ListCompanionCandidates("", "")
	if err != nil {
		t.Fatalf("全量列表失败: %v", err)
	}
	if len(all) < 2 {
		t.Fatalf("应至少 2 条候选，got %d", len(all))
	}
	got, err := st.DecideCompanionCandidate(cands[0].ID, "confirmed")
	if err != nil || got.Status != "confirmed" {
		t.Fatalf("裁决失败: %v %+v", err, got)
	}
	if _, err := st.DecideCompanionCandidate(cands[0].ID, "rejected"); err == nil {
		t.Fatalf("已裁决候选不应二次裁决")
	}
	if err := st.AdvanceCompanionCursor("conv-x", "m1"); err != nil {
		t.Fatalf("游标推进失败: %v", err)
	}
	cur, err := st.GetCompanionCursor("conv-x")
	if err != nil || cur.LastMessageID != "m1" {
		t.Fatalf("游标回读不符: %+v %v", cur, err)
	}
	if err := st.DeleteConversationCompanionData("conv-x"); err != nil {
		t.Fatalf("会话摘除失败: %v", err)
	}
	if cur2, _ := st.GetCompanionCursor("conv-x"); cur2.LastMessageID != "" {
		t.Fatalf("摘除后游标应清空: %+v", cur2)
	}
}

func openTestStore(t *testing.T) (*store.Store, error) {
	t.Helper()
	return store.Open(filepath.Join(t.TempDir(), "test.db"))
}
