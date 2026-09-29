package store

import (
	"path/filepath"
	"testing"
)

// KB-4①：递归 CTE 多跳测试——链式图 a-b-c-d，验证跳数可达边界与钳制。
func TestKGNeighborsMultiHop(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "cte.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.DB.Close()
	rels := []*KGRelationship{
		{KBID: "kb1", Source: "a", Target: "b", Type: "rel", Status: "approved"},
		{KBID: "kb1", Source: "b", Target: "c", Type: "rel", Status: "approved"},
		{KBID: "kb1", Source: "c", Target: "d", Type: "rel", Status: "approved"},
		{KBID: "kb1", Source: "d", Target: "e", Type: "rel", Status: "rejected"}, // 不参与遍历
	}
	for _, r := range rels {
		if _, err := st.DB.Exec(`INSERT INTO kg_relationship (id,kb_id,doc_id,source,target,rel_type,status,created_at) VALUES (?,?,?,?,?,?,?,?)`,
			NewID(), r.KBID, "doc1", r.Source, r.Target, r.Type, r.Status, now()); err != nil {
			t.Fatalf("seed rel: %v", err)
		}
	}
	key := func(rs []*KGRelationship) map[string]bool {
		m := map[string]bool{}
		for _, r := range rs {
			m[r.Source+"-"+r.Target] = true
		}
		return m
	}
	// 1 跳：仅 a-b
	one, err := st.KGNeighborsMultiHop("kb1", []string{"a"}, 1)
	if err != nil {
		t.Fatalf("hop1: %v", err)
	}
	k1 := key(one)
	if !k1["a-b"] || len(one) != 1 {
		t.Fatalf("1 跳应仅 a-b: %v", k1)
	}
	// 2 跳：a-b + b-c
	two, err := st.KGNeighborsMultiHop("kb1", []string{"a"}, 2)
	if err != nil {
		t.Fatalf("hop2: %v", err)
	}
	k2 := key(two)
	if !k2["a-b"] || !k2["b-c"] || len(two) != 2 {
		t.Fatalf("2 跳应 a-b/b-c: %v", k2)
	}
	// 3 跳：到 c-d；rejected 不入图
	three, err := st.KGNeighborsMultiHop("kb1", []string{"a"}, 3)
	if err != nil {
		t.Fatalf("hop3: %v", err)
	}
	k3 := key(three)
	if !k3["a-b"] || !k3["b-c"] || !k3["c-d"] || len(three) != 3 {
		t.Fatalf("3 跳应到 c-d 且排除 rejected: %v", k3)
	}
	// 越界钳制：hops=10 等价 3
	capped, err := st.KGNeighborsMultiHop("kb1", []string{"a"}, 10)
	if err != nil {
		t.Fatalf("hop capped: %v", err)
	}
	if len(capped) != 3 {
		t.Fatalf("hops=10 应钳制为 3: %d", len(capped))
	}
	// 多种子
	multi, err := st.KGNeighborsMultiHop("kb1", []string{"a", "c"}, 1)
	if err != nil {
		t.Fatalf("multi-seed: %v", err)
	}
	km := key(multi)
	if !km["a-b"] || !km["b-c"] || !km["c-d"] || len(multi) != 3 {
		t.Fatalf("多种子 1 跳应 a-b/b-c/c-d: %v", km)
	}
}
