package ontobuild

import (
	"strings"
	"testing"
)

// M-O14 P2⑤：结构化→骨架映射推导——CSV/JSON 双源 + 目标本体命中标注 + 实例采样上限
func TestInferStructuredDraftCSV(t *testing.T) {
	csv := "设备编号,设备名称,功率\nEQ-001,空压机A,75\nEQ-002,水泵B,15\n"
	targets := [][2]string{{"设备名称", "Device Name"}}
	d, err := InferStructuredDraft("devices.csv", csv, targets)
	if err != nil {
		t.Fatal(err)
	}
	if d.SourceKind != "csv" || d.MainConcept != "设备实体" {
		t.Fatalf("主概念推导不符: %s / %s", d.SourceKind, d.MainConcept)
	}
	if len(d.Mapping) != 3 || d.Mapping[0].Role != "instance-name" || d.Mapping[1].InferType != "string" || d.Mapping[2].InferType != "number" {
		t.Fatalf("映射/类型推断不符: %+v", d.Mapping)
	}
	if len(d.Mapping[1].MatchedCon) != 1 || d.Mapping[1].MatchedCon[0] != "设备名称" {
		t.Fatalf("目标概念命中不符: %+v", d.Mapping[1])
	}
	if len(d.Draft.Instances) != 2 || d.Draft.Instances[0].Name != "EQ-001" {
		t.Fatalf("实例骨架不符: %+v", d.Draft.Instances)
	}
	if d.Draft.Instances[0].Attributes["功率"] != "75" {
		t.Fatalf("属性映射不符: %+v", d.Draft.Instances[0].Attributes)
	}
}

func TestInferStructuredDraftJSON(t *testing.T) {
	js := `[{"host":"web-1","cpu":4},{"host":"db-1","cpu":16}]`
	d, err := InferStructuredDraft("hosts.json", js, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.SourceKind != "json" || d.MainConcept != "host实体" || len(d.Draft.Instances) != 2 {
		t.Fatalf("JSON 推导不符: %s / %s / %d", d.SourceKind, d.MainConcept, len(d.Draft.Instances))
	}
	if d.Mapping[1].InferType != "number" {
		t.Fatalf("number 推断不符: %+v", d.Mapping)
	}
	if !strings.Contains(d.Notes[0], "REQ-82") {
		t.Fatalf("诚实注记缺失: %v", d.Notes)
	}
}
