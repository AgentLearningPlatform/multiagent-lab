package importer

import (
	"encoding/json"
	"testing"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
)

// ExportVOWLJSON（WebVOWL 对照视图数据源，2026-09-27 转换链重构）：
// spec → VOWL JSON 的映射完整性——类/继承/关系/实例/指标与空 spec 兜底。
func TestExportVOWLJSON(t *testing.T) {
	spec := &pkgspec.Spec{
		ID:          "ont_demo",
		Name:        "演示本体",
		Description: "用于单测",
		Concepts: []pkgspec.Concept{
			{Name: "疾病", Label: "疾病", Definition: "机体 disequilibrium"},
			{Name: "症状", Parents: []string{"疾病"}},
		},
		Relations: []pkgspec.Relation{
			{Name: "表现为", Label: "表现为", From: "疾病", To: "症状", Definition: "典型表征"},
		},
		Instances: []pkgspec.Instance{
			{Name: "流感", Concept: "疾病"},
			{Name: "发热", Concept: "症状"},
		},
	}
	b, err := ExportVOWLJSON(spec)
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	var out struct {
		Identifier string           `json:"identifier"`
		Header     map[string]any   `json:"header"`
		Metrics    map[string]int   `json:"metrics"`
		Class      []map[string]any `json:"class"`
		Property   []map[string]any `json:"property"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("产物非法 JSON: %v", err)
	}
	if out.Identifier != "ont_demo" {
		t.Errorf("identifier = %v", out.Identifier)
	}
	if out.Metrics["classCount"] != 2 || out.Metrics["objectPropertyCount"] != 1 || out.Metrics["individualCount"] != 2 {
		t.Errorf("metrics 不符: %v", out.Metrics)
	}
	if len(out.Class) != 2 {
		t.Fatalf("类数应 2，得到 %d", len(out.Class))
	}
	// 症状实体带实例与继承边；疾病实体带实例与定义
	byID := map[string]map[string]any{}
	for _, e := range out.Class {
		byID[e["id"].(string)] = e
	}
	inds, _ := byID["症状"]["individuals"].([]any)
	if len(inds) != 1 {
		t.Fatalf("症状个体数应 1，得到 %v", byID["症状"]["individuals"])
	}
	il, _ := inds[0].(map[string]any)["labels"].(map[string]any)
	if il["IRI-based"] != "发热" {
		t.Errorf("个体标签不符: %v", inds[0])
	}
	var sub map[string]any
	for _, p := range out.Property {
		if p["type"] == "rdfs:subClassOf" {
			sub = p
		}
	}
	if sub == nil || sub["domain"] != "症状" || sub["range"] != "疾病" {
		t.Errorf("subClassOf 边不符: %v", sub)
	}
	var op map[string]any
	for _, p := range out.Property {
		if p["type"] == "owl:ObjectProperty" {
			op = p
		}
	}
	if op == nil || op["domain"] != "疾病" || op["range"] != "症状" {
		t.Errorf("对象属性边不符: %v", op)
	}
	lbl, _ := op["label"].(map[string]any)
	if lbl == nil || lbl["IRI-based"] != "表现为" {
		t.Errorf("属性标签不符: %v", op["label"])
	}
}

func TestExportVOWLJSONEmpty(t *testing.T) {
	b, err := ExportVOWLJSON(&pkgspec.Spec{Name: "空本体"})
	if err != nil {
		t.Fatalf("空 spec 导出失败: %v", err)
	}
	var out struct {
		Metrics  map[string]int `json:"metrics"`
		Class    []any          `json:"class"`
		Property []any          `json:"property"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("产物非法 JSON: %v", err)
	}
	if len(out.Class) != 0 || len(out.Property) != 0 || out.Metrics["classCount"] != 0 {
		t.Errorf("空 spec 应产出空图: %v", out)
	}
}

func TestExportVOWLJSONNil(t *testing.T) {
	if _, err := ExportVOWLJSON(nil); err == nil {
		t.Error("nil spec 应报错")
	}
}
