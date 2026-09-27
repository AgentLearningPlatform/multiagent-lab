package importer

import (
	"strings"
	"testing"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
)

// REQ-157/M-O15 零依赖单测：三策略合并（replace/merge-overwrite/merge）+ 冲突字段提取 + 前缀协调。

func mergeSpecs() (target, incoming *pkgspec.Spec) {
	target = &pkgspec.Spec{
		Name: "T",
		Concepts: []pkgspec.Concept{
			{Name: "Drug", Label: "药物", Definition: "现行定义"},
			{Name: "Disease", Label: "疾病", Definition: "疾病", Parents: []string{"Drug"}},
		},
		Relations: []pkgspec.Relation{{Name: "treats", From: "Drug", To: "Disease", Definition: "现行关系"}},
		Instances: []pkgspec.Instance{{Name: "aspirin", Concept: "Drug"}},
	}
	incoming = &pkgspec.Spec{
		Name: "I",
		Concepts: []pkgspec.Concept{
			{Name: "Drug", Label: "药品", Definition: "导入定义"},                              // 冲突：label+definition
			{Name: "Symptom", Label: "症状", Definition: "症状"},                             // 新增
			{Name: "Disease", Label: "疾病", Definition: "疾病改", Parents: []string{"Drug"}}, // 冲突：definition
		},
		Relations: []pkgspec.Relation{{Name: "treats", From: "Drug", To: "Symptom", Definition: "改"}},
		Instances: []pkgspec.Instance{{Name: "aspirin", Concept: "Disease"}},
	}
	return
}

func TestBuildMergedReplace(t *testing.T) {
	target, incoming := mergeSpecs()
	pv, err := BuildMerged(target, incoming, StrategyReplace, "")
	if err != nil {
		t.Fatal(err)
	}
	if pv.Stats.TotalConflicts != 4 || len(pv.Conflicts) != 4 {
		t.Fatalf("应 4 处冲突: %+v", pv.Stats)
	}
	// Drug 被导入版整体替换
	var drug pkgspec.Concept
	for _, c := range pv.MergedSpec.Concepts {
		if c.Name == "Drug" {
			drug = c
		}
	}
	if drug.Definition != "导入定义" || drug.Label != "药品" {
		t.Fatalf("replace 应导入版整体替换: %+v", drug)
	}
	if len(pv.Added) != 1 { // 仅 Symptom 为新增（其余全部冲突）
		t.Fatalf("新增清单不符: %v", pv.Added)
	}
	if len(pv.Renamed) != 0 {
		t.Fatalf("replace 不应重命名: %v", pv.Renamed)
	}
}

func TestBuildMergedOverwrite(t *testing.T) {
	target, incoming := mergeSpecs()
	pv, _ := BuildMerged(target, incoming, StrategyMergeOverwrite, "")
	var drug pkgspec.Concept
	for _, c := range pv.MergedSpec.Concepts {
		if c.Name == "Drug" {
			drug = c
		}
	}
	if drug.Definition != "导入定义" {
		t.Fatalf("非空字段应覆盖: %+v", drug)
	}
	// 字段级冲突清单应含 label+definition
	found := false
	for _, cf := range pv.Conflicts {
		if cf.Name == "Drug" && cf.Kind == "concept" {
			found = true
			if strings.Join(cf.Fields, ",") != "definition,label" {
				t.Fatalf("冲突字段不符: %v", cf.Fields)
			}
			if cf.Resolution != "field-merged" {
				t.Fatalf("处置不符: %s", cf.Resolution)
			}
		}
	}
	if !found {
		t.Fatal("缺 Drug 冲突条目")
	}
}

func TestBuildMergedRename(t *testing.T) {
	target, incoming := mergeSpecs()
	pv, _ := BuildMerged(target, incoming, StrategyMerge, "ext")
	if len(pv.Renamed) != 4 {
		t.Fatalf("4 个冲突实体应全部重命名并入: %v", pv.Renamed)
	}
	// 原 Drug 保留现行定义；ext_Drug 存在导入定义
	var cur, ext *pkgspec.Concept
	for i := range pv.MergedSpec.Concepts {
		switch pv.MergedSpec.Concepts[i].Name {
		case "Drug":
			cur = &pv.MergedSpec.Concepts[i]
		case "ext_Drug":
			ext = &pv.MergedSpec.Concepts[i]
		}
	}
	if cur == nil || cur.Definition != "现行定义" || ext == nil || ext.Definition != "导入定义" {
		t.Fatalf("merge 应保留现行并重命名并入: %+v %+v", cur, ext)
	}
	// 实例关系/概念引用改写：ext_aspirin 应指向 ext_Disease？——原实例 aspirin 冲突重命名为 ext_aspirin，其 concept=Disease 不变（Disease 现行保留）
	var renamedInst bool
	for _, in := range pv.MergedSpec.Instances {
		if in.Name == "ext_aspirin" {
			renamedInst = true
			if in.Concept != "Disease" {
				t.Fatalf("实例类型引用应指向现行实体: %+v", in)
			}
		}
	}
	if !renamedInst {
		t.Fatal("缺 ext_aspirin")
	}
}

func TestBuildMergedPrefixCollision(t *testing.T) {
	// 冲突实体 X 的重命名基名 ext_X 已被既有实体占用 → uniquify 避让为 ext_X_2
	target := &pkgspec.Spec{Name: "T", Concepts: []pkgspec.Concept{
		{Name: "X", Definition: "现行"},
		{Name: "ext_X", Definition: "占位"},
	}}
	incoming := &pkgspec.Spec{Concepts: []pkgspec.Concept{{Name: "X", Definition: "导入"}}}
	pv, _ := BuildMerged(target, incoming, StrategyMerge, "ext")
	if !strings.Contains(strings.Join(pv.Renamed, ","), "X -> ext_X_2") {
		t.Fatalf("前缀协调应避让既有名: %v", pv.Renamed)
	}
}

func TestBuildMergedInvalid(t *testing.T) {
	target, incoming := mergeSpecs()
	if _, err := BuildMerged(target, incoming, "bad", ""); err == nil {
		t.Fatal("非法策略应报错")
	}
	if _, err := BuildMerged(nil, incoming, StrategyReplace, ""); err == nil {
		t.Fatal("nil target 应报错")
	}
}
