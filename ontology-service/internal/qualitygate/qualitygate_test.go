package qualitygate

import (
	"strings"
	"testing"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
)

// REQ-171 P1 零依赖单测：首批 11 检查项逐项触发/配置覆盖/评分与 Pass 语义。

func specOf(concepts []pkgspec.Concept, relations []pkgspec.Relation, instances []pkgspec.Instance) *pkgspec.Spec {
	return &pkgspec.Spec{Name: "t", Concepts: concepts, Relations: relations, Instances: instances}
}

func findingOf(rep *Report, id string) *Finding {
	for i := range rep.Findings {
		if rep.Findings[i].CheckID == id {
			return &rep.Findings[i]
		}
	}
	return nil
}

func TestChecksTrigger(t *testing.T) {
	sp := specOf(
		[]pkgspec.Concept{
			{Name: "Drug"}, // 孤立 + 无定义 + 无 label
			{Name: "treats_drug", Definition: "x", Label: "治疗"},            // snake 风格（主导 camel 时命中）
			{Name: "Disease", Definition: "x", Parents: []string{"Ghost"}}, // 父不存在
			{Name: "Disease"}, // 重复名
		},
		[]pkgspec.Relation{
			{Name: "treats", From: "Drug", To: "Ghost2"}, // 端点不存在
			{Name: "noEndpoint", Definition: "x"},        // 域值域未声明
		},
		[]pkgspec.Instance{
			{Name: "aspirin", Concept: "Nope"}, // 类型不存在
			{Name: "aspirin", Concept: "Drug", Relations: []pkgspec.InstanceRel{{Rel: "ghostRel", Target: "nope-ins"}}}, // 断言悬空 + 重复名
		},
	)
	rep := Check(sp, nil)
	if rep.Stats.Concepts != 4 || rep.Stats.Relations != 2 || rep.Stats.Instances != 2 {
		t.Fatalf("stats 不符: %+v", rep.Stats)
	}
	for id, want := range map[string]bool{
		"orphan_concept": true, "missing_definition": true, "missing_label": true,
		"dangling_parent": true, "duplicate_name": true, "dangling_relation_endpoint": true,
		"relation_endpoint_unspecified": true, "instance_type_missing": true, "dangling_instance_rel": true,
	} {
		f := findingOf(rep, id)
		if want && (f == nil || f.Count == 0) {
			t.Fatalf("检查项 %s 应命中", id)
		}
	}
	if f := findingOf(rep, "dangling_relation_endpoint"); f == nil || !strings.Contains(strings.Join(f.Samples, ","), "Ghost2") {
		t.Fatalf("dangling_relation_endpoint 样例应含 Ghost2: %+v", f)
	}
	if rep.ErrorCount == 0 || rep.Pass {
		t.Fatalf("存在错误级命中时 Pass 应为 false: err=%d pass=%v", rep.ErrorCount, rep.Pass)
	}
	if rep.Score.Consistency >= 100 || rep.Score.Overall >= 100 {
		t.Fatalf("扣分制下分数应 <100: %+v", rep.Score)
	}
}

func TestHierarchyCycle(t *testing.T) {
	sp := specOf([]pkgspec.Concept{
		{Name: "A", Definition: "x", Parents: []string{"B"}},
		{Name: "B", Definition: "x", Parents: []string{"C"}},
		{Name: "C", Definition: "x", Parents: []string{"A"}},
	}, nil, nil)
	rep := Check(sp, nil)
	f := findingOf(rep, "hierarchy_cycle")
	if f == nil || f.Count != 1 || !strings.Contains(f.Samples[0], "A -> B -> C -> A") {
		t.Fatalf("应检出 A->B->C->A 环: %+v", f)
	}
}

func TestNamingStyleDominant(t *testing.T) {
	sp := specOf([]pkgspec.Concept{
		{Name: "DrugTarget", Definition: "x"}, // camel 主导
		{Name: "drug_dose", Definition: "x"},  // snake 少数派 → 命中
		{Name: "剂量上限", Definition: "x"},       // cjk 不参与风格判定
	}, nil, nil)
	rep := Check(sp, nil)
	f := findingOf(rep, "naming_style")
	if f == nil || f.Count != 1 || !strings.Contains(f.Samples[0], "drug_dose") {
		t.Fatalf("命名风格少数派应命中: %+v", f)
	}
}

func TestCleanSpecPass(t *testing.T) {
	sp := specOf([]pkgspec.Concept{
		{Name: "Drug", Label: "药物", Definition: "药物"},
		{Name: "Disease", Label: "疾病", Definition: "疾病", Parents: []string{"Drug"}},
	}, []pkgspec.Relation{
		{Name: "treats", Label: "治疗", Definition: "药物治疗疾病", From: "Drug", To: "Disease"},
	}, []pkgspec.Instance{
		{Name: "aspirin", Concept: "Drug", Relations: []pkgspec.InstanceRel{{Rel: "treats", Target: "flu"}}},
		{Name: "flu", Concept: "Disease"},
	})
	rep := Check(sp, nil)
	if len(rep.Findings) != 0 || !rep.Pass || rep.Score.Overall != 100 {
		t.Fatalf("干净本体应满分通过: %+v", rep)
	}
}

func TestConfigOverride(t *testing.T) {
	sp := specOf([]pkgspec.Concept{{Name: "X"}}, nil, nil)
	cfg := DefaultConfig()
	cfg["orphan_concept"] = CheckCfg{Enabled: false}
	cfg["missing_definition"] = CheckCfg{Enabled: true, Severity: SevError}
	rep := Check(sp, cfg)
	if findingOf(rep, "orphan_concept") != nil {
		t.Fatal("禁用项不应出现")
	}
	f := findingOf(rep, "missing_definition")
	if f == nil || f.Severity != SevError {
		t.Fatalf("severity 覆盖应生效: %+v", f)
	}
	// 宽松模式：调用方不阻断（Pass 字段语义由 Check 判定，宽松覆盖在 REST 层——此处验证默认判定的原始值）
	if rep.Pass {
		t.Fatal("错误级命中原始判定应为 false（宽松覆盖在调用方）")
	}
}

func TestErrorMessages(t *testing.T) {
	sp := specOf([]pkgspec.Concept{{Name: "A", Definition: "x", Parents: []string{"B"}}}, nil, nil)
	rep := Check(sp, nil)
	msgs := rep.ErrorMessages()
	if len(msgs) == 0 || !strings.Contains(msgs[0], "dangling_parent") {
		t.Fatalf("ErrorMessages 应含检查项 ID: %v", msgs)
	}
}
