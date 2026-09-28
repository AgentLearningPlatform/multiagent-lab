package ontoextend

import "testing"

// M-O14 P2②：ODP 精选集自洽性——片段结构合法（概念名唯一/关系端点在册/定义齐备过完备性门禁口径）
func TestCuratedODPs(t *testing.T) {
	list := List()
	if len(list) < 10 {
		t.Fatalf("精选 ODP 应 ≥10 个，实际 %d", len(list))
	}
	seen := map[string]bool{}
	for _, o := range list {
		if o.ID == "" || o.Name == "" || o.Spec == nil {
			t.Fatalf("ODP 字段缺失: %+v", o.ID)
		}
		if seen[o.ID] {
			t.Fatalf("ODP id 重复: %s", o.ID)
		}
		seen[o.ID] = true
		names := map[string]bool{}
		for _, c := range o.Spec.Concepts {
			if c.Name == "" || c.Definition == "" {
				t.Fatalf("%s 概念缺 name/definition: %s", o.ID, c.Name)
			}
			names[c.Name] = true
		}
		for _, r := range o.Spec.Relations {
			if !names[r.From] || !names[r.To] {
				t.Fatalf("%s 关系端点悬空: %s (%s→%s)", o.ID, r.Name, r.From, r.To)
			}
		}
		if Get(o.ID) != o {
			t.Fatalf("Get(%s) 未命中", o.ID)
		}
	}
	if Get("not-exist") != nil {
		t.Fatal("Get 未知识应返回 nil")
	}
}
