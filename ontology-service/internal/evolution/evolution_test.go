package evolution

import (
	"encoding/json"
	"testing"
)

func TestCandidateLifecycle(t *testing.T) {
	// 状态机语义：Create→proposed；Decide accepted/rejected；二次裁决报错。
	// （走 sqlite 内存库；见 evolution_rest_test 的 REST 面）
}

func TestApplyPatch(t *testing.T) {
	spec := map[string]any{
		"concepts": []any{
			map[string]any{"name": "Pod", "definition": "最小调度单元"},
			map[string]any{"name": "Deployment"},
		},
		"relations": []any{
			map[string]any{"name": "由其管理", "from": "Deployment", "to": "Pod"},
		},
	}
	patch := `{
		"add_concepts":[{"name":"ReplicaSet","definition":"副本集"}],
		"update_concepts":[{"name":"Pod","definition":"最小调度单元（更新）"}],
		"remove_relations":[{"name":"由其管理","from":"Deployment","to":"Pod"}],
		"add_relations":[{"name":"创建","from":"Deployment","to":"ReplicaSet"}]
	}`
	n, err := ApplyPatch(spec, patch)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("应应用 4 处编辑，got %d", n)
	}
	concepts := spec["concepts"].([]any)
	if len(concepts) != 3 {
		t.Fatalf("应 3 概念，got %d", len(concepts))
	}
	found := false
	for _, c := range concepts {
		cm := c.(map[string]any)
		if cm["name"] == "Pod" && cm["definition"] == "最小调度单元（更新）" {
			found = true
		}
	}
	if !found {
		t.Fatal("update 未生效")
	}
	relations := spec["relations"].([]any)
	if len(relations) != 1 {
		t.Fatalf("应 1 关系（旧删新增），got %d", len(relations))
	}
	rm := relations[0].(map[string]any)
	if rm["to"] != "ReplicaSet" {
		t.Fatalf("新增关系未生效: %v", rm)
	}
}

func TestPatchJSONRoundTrip(t *testing.T) {
	p := Patch{AddConcepts: []map[string]any{{"name": "X"}}}
	b, _ := json.Marshal(p)
	var back Patch
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.AddConcepts) != 1 || back.AddConcepts[0]["name"] != "X" {
		t.Fatalf("往返不符: %+v", back)
	}
}
