package companion

import (
	"math"
	"strings"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// REQ-194/M34 零依赖单测：分窗 / 对齐 / 合并召回 / 余弦 / 聚类 / 矛盾 prompt（批次一①③ + 批次二⑤⑥）。

func TestSplitWindows(t *testing.T) {
	// ≤16 条 → 单窗
	msgs := msgsOf("m1", "m2", "m3")
	if w := splitWindows(msgs); len(w) != 1 || w[0].lastID != "m3" {
		t.Fatalf("3 条应单窗: %d", len(w))
	}
	// 35 条 → 3 窗（16+16+3），窗末锚点正确
	many := msgsOf(func() []string {
		ids := make([]string, 35)
		for i := range ids {
			ids[i] = "m" + strings.Repeat("x", i%3) + itoa(i)
		}
		return ids
	}()...)
	wins := splitWindows(many)
	if len(wins) != 3 {
		t.Fatalf("35 条应 3 窗（16/16/3），got %d", len(wins))
	}
	if len(wins[0].msgs) != 16 || len(wins[1].msgs) != 16 || len(wins[2].msgs) != 3 {
		t.Fatalf("窗尺寸不符: %d/%d/%d", len(wins[0].msgs), len(wins[1].msgs), len(wins[2].msgs))
	}
	if wins[0].lastID != wins[0].msgs[15].ID || wins[2].lastID != wins[2].msgs[2].ID {
		t.Fatalf("窗末锚点不符: %q", wins[0].lastID)
	}
	// 字符窗：单条 600 截断 + 锚点 ≈ 640 字符 → 8000 上限约 12 条触顶（先于 16 条）
	long := make([]*store.Message, 0, 20)
	for i := 0; i < 20; i++ {
		long = append(long, &store.Message{ID: itoa(i), Role: "user", Content: strings.Repeat("长", 600)})
	}
	wins2 := splitWindows(long)
	if len(wins2[0].msgs) >= 16 {
		t.Fatalf("字符上限应先于条数上限触发，首窗 %d 条", len(wins2[0].msgs))
	}
	for i, w := range wins2 {
		if n := len([]rune(renderCorpus(w.msgs))); n > windowMaxChars {
			t.Fatalf("窗 %d 超 8k: %d", i, n)
		}
	}
	// 全部窗覆盖原消息（不丢）
	total := 0
	for _, w := range wins2 {
		total += len(w.msgs)
	}
	if total != 20 {
		t.Fatalf("分窗应覆盖全部消息，got %d", total)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestBuildAlignmentSection(t *testing.T) {
	if s := buildAlignmentSection(nil); s != "" {
		t.Fatal("空清单应返回空串")
	}
	s := buildAlignmentSection([]string{"Pod 扩容", "HPA", "", "Pod 扩容"})
	for _, want := range []string{"已有实体清单", "Pod 扩容、HPA", "沿用清单原名", "不得与清单实体同义近似"} {
		if !strings.Contains(s, want) {
			t.Fatalf("对齐节缺 %q：%s", want, s)
		}
	}
	// 去重：重复标签只出现一次
	if strings.Count(s, "Pod 扩容、") != 1 && strings.Count(s, "、HPA") != 1 {
		t.Fatalf("清单应去重：%s", s)
	}
	// >80 聚类取样截断 + 注记
	var many []string
	for i := 0; i < 100; i++ {
		many = append(many, "实体"+itoa(i)+"类")
	}
	big := buildAlignmentSection(many)
	if !strings.Contains(big, "清单共 100 条") || !strings.Contains(big, "聚类取样") {
		t.Fatalf("超限注记缺失：%s", big)
	}
	// 方括号清单内条数应恰为 80
	li := strings.Index(big, "[")
	ri := strings.Index(big, "]")
	if li < 0 || ri < li {
		t.Fatal("清单方括号缺失")
	}
	if n := strings.Count(big[li:ri], "、") + 1; n != alignmentLabelCap {
		t.Fatalf("取样应恰为 80 条，got %d", n)
	}
}

func TestClusterSampleBySlug(t *testing.T) {
	// 三簇（运维/医学/通用前缀）轮转取样，覆盖面优于单簇堆叠
	labels := []string{"运维部署", "运维监控", "运维告警", "医学检验", "医学影像", "通用概念A", "通用概念B"}
	got := clusterSampleBySlug(labels, 5)
	if len(got) != 5 {
		t.Fatalf("limit=5 应取 5 条，got %d", len(got))
	}
	clusters := map[string]bool{}
	for _, g := range got {
		clusters[slugPrefixKey(g)] = true
	}
	if len(clusters) < 2 {
		t.Fatalf("轮转取样应覆盖多簇，got %v", got)
	}
	// 确定性：两次调用结果一致
	a := clusterSampleBySlug(labels, 5)
	for i := range a {
		if a[i] != got[i] {
			t.Fatal("取样应确定性")
		}
	}
}

func TestMarkAligned(t *testing.T) {
	known := []string{"Pod 扩容", "HPA"}
	cands := []*store.CompanionCandidate{
		{Kind: "concept", Name: "Pod 扩容"},                         // 命中
		{Kind: "concept", Name: "金丝雀发布"},                          // 新造
		{Kind: "relation", Name: "Pod 扩容", RelTarget: "HPA"},      // 主体命中
		{Kind: "relation", Name: "滚动更新", RelTarget: "Deployment"}, // 双未命中
		{Kind: "relation", Name: "滚动更新", RelTarget: "Pod 扩容"},     // 客体命中
	}
	markAligned(cands, known)
	want := []string{"aligned", "new", "aligned", "new", "aligned"}
	for i, w := range want {
		if cands[i].Aligned != w {
			t.Fatalf("cand[%d] aligned=%q want %q", i, cands[i].Aligned, w)
		}
	}
}

func TestAppendWindowEntities(t *testing.T) {
	known := []string{"Pod 扩容"}
	cands := []*store.CompanionCandidate{
		{Kind: "concept", Name: "金丝雀发布"},
		{Kind: "relation", Name: "金丝雀发布", RelTarget: "Deployment"},
		{Kind: "concept", Name: "Pod 扩容"}, // 已在清单不重复
	}
	got := appendWindowEntities(known, cands)
	if len(got) != 3 {
		t.Fatalf("应新增 2 实体共 3 条，got %v", got)
	}
	for _, w := range []string{"金丝雀发布", "Deployment"} {
		found := false
		for _, g := range got {
			if g == w {
				found = true
			}
		}
		if !found {
			t.Fatalf("缺 %s: %v", w, got)
		}
	}
}

func TestMergeRecall(t *testing.T) {
	// 向量优先占坑，词法补位，双中标 vector+lexical
	merged, matchOf := mergeRecall(
		[]string{"水平伸缩", "副本数"},
		[]string{"水平伸缩", "滚动更新", "HPA"},
	)
	if strings.Join(merged, "|") != "水平伸缩|副本数|滚动更新|HPA" {
		t.Fatalf("合并序不符: %v", merged)
	}
	if matchOf["水平伸缩"] != "vector+lexical" || matchOf["副本数"] != "vector" || matchOf["滚动更新"] != "lexical" {
		t.Fatalf("match 标记不符: %v", matchOf)
	}
	// 上限截断
	merged2, _ := mergeRecall([]string{"a1", "a2", "a3"}, []string{"b1", "b2", "b3"})
	if len(merged2) != maxRecallEntities {
		t.Fatalf("应截断到 %d，got %d", maxRecallEntities, len(merged2))
	}
	// 双空
	if m, _ := mergeRecall(nil, nil); len(m) != 0 {
		t.Fatal("双路空应返回空")
	}
}

func TestCosineSim(t *testing.T) {
	if sim := cosineSim([]float32{1, 0}, []float32{1, 0}); math.Abs(sim-1) > 1e-9 {
		t.Fatalf("同向应=1: %f", sim)
	}
	if sim := cosineSim([]float32{1, 0}, []float32{0, 1}); math.Abs(sim) > 1e-9 {
		t.Fatalf("正交应=0: %f", sim)
	}
	if sim := cosineSim([]float32{1, 0}, []float32{-1, 0}); math.Abs(sim+1) > 1e-9 {
		t.Fatalf("反向应=-1: %f", sim)
	}
	if sim := cosineSim(nil, nil); sim != 0 {
		t.Fatal("空向量应=0")
	}
	if sim := cosineSim([]float32{1, 2, 3}, []float32{1, 2}); sim == 0 {
		t.Fatal("维度不齐应按短边截断计算而非 0")
	}
}

func TestGroupCandidatesByEntity(t *testing.T) {
	list := []*store.CompanionCandidate{
		{ID: "1", Kind: "concept", Name: "Pod 扩容", Confidence: 0.6, Status: "pending", CreatedAt: "2026-09-29T10:00:00Z"},
		{ID: "2", Kind: "relation", Name: "Pod 扩容", RelTarget: "HPA", Confidence: 0.9, Status: "pending", CreatedAt: "2026-09-29T10:01:00Z"},
		{ID: "3", Kind: "concept", Name: "金丝雀发布", Confidence: 0.5, Status: "confirmed", CreatedAt: "2026-09-29T09:00:00Z"},
		{ID: "4", Kind: "concept", Name: "金丝雀发布", Confidence: 0.4, Status: "rejected", CreatedAt: "2026-09-29T09:30:00Z"},
	}
	groups := GroupCandidatesByEntity(list)
	if len(groups) != 2 {
		t.Fatalf("应 2 组，got %d", len(groups))
	}
	// 组序=最新时间倒序：Pod 扩容（10:01）在前
	if groups[0].Entity != "Pod 扩容" || groups[0].Count != 2 || groups[0].PendingCount != 2 {
		t.Fatalf("组 1 不符: %+v", groups[0])
	}
	if groups[0].Representative.ID != "2" { // 组内置信最高
		t.Fatalf("代表候选应为置信最高者: %+v", groups[0].Representative)
	}
	if groups[1].Entity != "金丝雀发布" || groups[1].Count != 2 || groups[1].PendingCount != 0 {
		t.Fatalf("组 2 不符: %+v", groups[1])
	}
	if g := GroupCandidatesByEntity(nil); g == nil || len(g) != 0 {
		t.Fatalf("空列表应返回空切片，got %v", g)
	}
}

func TestConflictPromptAndMatch(t *testing.T) {
	existing := []edgeAssertion{
		{Edge: "http://eino-lab/e/edge-1", RelName: "是", ObjLabel: "良性的"},
		{Edge: "http://eino-lab/e/edge-2", RelName: "分期", ObjLabel: "早期"},
	}
	p := conflictPrompt("肿瘤X", existing, "是", "恶性的")
	for _, want := range []string{"肿瘤X", "良性的", "恶性的", "unsure", "语义矛盾"} {
		if !strings.Contains(p, want) {
			t.Fatalf("矛盾 prompt 缺 %q：%s", want, p)
		}
	}
	// 指认精确命中
	if e := matchConflictEdge(existing, "是", "良性的"); e != "http://eino-lab/e/edge-1" {
		t.Fatalf("指认定位不符: %s", e)
	}
	// 宽松包含命中
	if e := matchConflictEdge(existing, "是", "良性"); e != "http://eino-lab/e/edge-1" {
		t.Fatalf("宽松定位不符: %s", e)
	}
	// 指认不中回退同关系名首条
	if e := matchConflictEdge(existing, "是", ""); e != "http://eino-lab/e/edge-1" {
		t.Fatalf("同关系名回退不符: %s", e)
	}
}

func TestSlugPrefixKey(t *testing.T) {
	if k := slugPrefixKey("运维部署A"); k != "运维" {
		t.Fatalf("CJK 前 2 字符: %q", k)
	}
	if k := slugPrefixKey("deployment-guide"); k != "deployme" {
		t.Fatalf("拉丁前 8 字符: %q", k)
	}
	if k := slugPrefixKey("  "); k != "" {
		t.Fatalf("空白标签: %q", k)
	}
}

func TestNeighborhoodSPARQLShape(t *testing.T) {
	q := SelectEntityNeighborhood("c1", "阿司匹林")
	for _, want := range []string{"BIND(1 AS ?hop)", "BIND(2 AS ?hop)", "CONCAT", "FILTER NOT EXISTS", "DISTINCT", "ORDER BY ?hop"} {
		if !strings.Contains(q, want) {
			t.Fatalf("邻域查询缺 %q：%s", want, q)
		}
	}
	s := SelectSubjectActiveEdges("c1", "肿瘤X")
	if !strings.Contains(s, "?edge") || !strings.Contains(s, "?objLabel") || !strings.Contains(s, "FILTER NOT EXISTS") {
		t.Fatalf("同主体活跃边查询不符：%s", s)
	}
}
