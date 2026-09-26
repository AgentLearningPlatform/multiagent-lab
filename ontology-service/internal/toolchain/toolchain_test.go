package toolchain

// REQ-155 工具链核心单测：只读门禁表驱动 / limit 钳制 / 结果表格投影 /
// validate+lint 复用链路。零外部依赖（query 经 httptest 桩端点）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
)

func TestVetReadonlySelect(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{"普通 SELECT", "SELECT ?s ?p ?o WHERE { ?s ?p ?o }", false},
		{"前缀声明后 SELECT", "PREFIX ex: <http://e.org#>\nSELECT ?a WHERE { ?a a ex:T }", false},
		{"BASE+PREFIX 混序", "BASE <http://b.org/>\nPREFIX ex: <http://e.org#>\nSELECT * WHERE { ?s ?p ?o }", false},
		{"字面量含 DELETE 不误伤", "SELECT ?l WHERE { ?s ?p ?l FILTER(?l = 'please DELETE me') }", false},
		{"IRI 含 delete 不误伤", "SELECT ?s WHERE { ?s ?p <http://e.org/delete> }", false},
		{"注释含 DROP 不误伤", "SELECT ?s WHERE { ?s ?p ?o } # DROP all", false},
		{"聚合 SELECT", "SELECT ?c (COUNT(?i) AS ?n) WHERE { ?i a ?c } GROUP BY ?c", false},
		{"UPDATE 开头拒绝", "UPDATE { ?s ?p ?o }", true},
		{"前缀后 INSERT 拒绝", "PREFIX ex: <http://e.org#>\nINSERT DATA { ex:a ex:b ex:c }", true},
		{"裸词 DELETE 拒绝", "SELECT ?s WHERE { ?s ?p ?o } DELETE { ?s ?p ?o }", true},
		{"LOAD 拒绝", "LOAD <http://e.org/g>", true},
		{"空查询拒绝", "   ", true},
		{"非查询关键词拒绝", "CONSTRUCT { ?s ?p ?o } WHERE { ?s ?p ?o }", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VetReadonlySelect(tc.query)
			if tc.wantErr && err == nil {
				t.Fatalf("期望被拒绝，实际通过")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("期望通过，实际被拒: %v", err)
			}
		})
	}
}

func TestClampLimit(t *testing.T) {
	if ClampLimit(0) != 50 || ClampLimit(-3) != 50 {
		t.Fatalf("缺省应为 50")
	}
	if ClampLimit(200) != 200 || ClampLimit(500) != 200 {
		t.Fatalf("上限应为 200")
	}
	if ClampLimit(7) != 7 {
		t.Fatalf("合法值应原样通过")
	}
}

func TestQueryAgainstStubEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("解析表单失败: %v", err)
		}
		if got := r.FormValue("query"); !strings.Contains(got, "SELECT") {
			t.Fatalf("转发查询缺少 SELECT: %q", got)
		}
		if accept := r.Header.Get("Accept"); accept != "application/sparql-results+json" {
			t.Fatalf("Accept 头不符: %q", accept)
		}
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_, _ = w.Write([]byte(`{"head":{"vars":["o","s"]},"results":{"bindings":[
			{"s":{"type":"uri","value":"http://e.org/i1"},"o":{"type":"literal","value":"一"}},
			{"s":{"type":"uri","value":"http://e.org/i2"},"o":{"type":"literal","value":"二"}},
			{"s":{"type":"uri","value":"http://e.org/i3"}}
		]}}`))
	}))
	defer srv.Close()

	table, err := Query(context.Background(), srv.URL, "SELECT ?s ?o WHERE { ?s ?p ?o }", 2, 0)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(table.Columns) != 2 || table.Columns[0] != "o" || table.Columns[1] != "s" {
		t.Fatalf("列应为字母序 [o s]，实际 %v", table.Columns)
	}
	if table.Total != 3 || !table.Truncated || len(table.Rows) != 2 {
		t.Fatalf("期望 total=3 截断至 2 行，实际 total=%d rows=%d truncated=%v", table.Total, len(table.Rows), table.Truncated)
	}
	if table.Rows[0][0] != "一" || table.Rows[0][1] != "http://e.org/i1" {
		t.Fatalf("首行投影不符: %v", table.Rows[0])
	}
	if table.Rows[1][0] != "二" {
		t.Fatalf("次行投影不符: %v", table.Rows[1])
	}
}

func TestQueryRejectsBeforeEndpoint(t *testing.T) {
	// 门禁拒绝时不触达端点（非法 endpoint 也不会报网络错）
	_, err := Query(context.Background(), "", "DELETE WHERE { ?s ?p ?o }", 0, 0)
	if err == nil {
		t.Fatalf("变更查询应被拒绝")
	}
}

func sampleSpec() *pkgspec.Spec {
	return &pkgspec.Spec{
		Name: "示例",
		Concepts: []pkgspec.Concept{
			{Name: "疾病", Definition: "机体在一定病因作用下出现异常生命活动"},
			{Name: "症状", Parents: []string{"疾病"}},
		},
		Relations: []pkgspec.Relation{{Name: "表现为", From: "疾病", To: "症状"}},
		Instances: []pkgspec.Instance{
			{Name: "感冒", Concept: "疾病", Relations: []pkgspec.InstanceRel{{Rel: "表现为", Target: "发热"}}},
			{Name: "发热", Concept: "症状"},
		},
	}
}

func TestValidateCleanSpec(t *testing.T) {
	v := Validate(sampleSpec(), true)
	if !v.Pass {
		t.Fatalf("合法 spec 期望 pass=true，结构错误 %v", v.Errors)
	}
	if v.Quality == nil {
		t.Fatalf("质量报告缺失")
	}
	if len(v.Errors) != 0 {
		t.Fatalf("不应有结构错误: %v", v.Errors)
	}
}

func TestValidateStructuralError(t *testing.T) {
	sp := sampleSpec()
	sp.Concepts = append(sp.Concepts, pkgspec.Concept{Name: "坏概念", Parents: []string{"不存在"}})
	v := Validate(sp, false)
	if v.Pass {
		t.Fatalf("含未定义父级引用应 pass=false")
	}
	if len(v.Errors) == 0 || !strings.Contains(v.Errors[0], "concepts[2].parents[0]") {
		t.Fatalf("结构错误应含路径定位: %v", v.Errors)
	}
}

func TestLintReportsFindings(t *testing.T) {
	sp := sampleSpec()
	// 加一个缺失定义的孤立概念，lint 应有命中
	sp.Concepts = append(sp.Concepts, pkgspec.Concept{Name: "孤立项"})
	l := Lint(sp)
	if l.Count == 0 {
		t.Fatalf("孤立且无定义的概念应产生 lint 命中")
	}
	if l.Count != len(l.Findings) {
		t.Fatalf("count 与 findings 数不一致")
	}
}
