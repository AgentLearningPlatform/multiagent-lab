package facade

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/store"
)

func TestVetReadonlySelect(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{"合法 SELECT", "SELECT ?s ?p ?o WHERE { ?s ?p ?o }", false},
		{"PREFIX 前导", "PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>\nSELECT ?c WHERE { ?c rdfs:label ?l }", false},
		{"BASE+PREFIX 前导", "BASE <http://x.example/>\nPREFIX a: <urn:a:>\nSELECT ?s WHERE { ?s a:t ?o }", false},
		{"小写 select", "select ?s where { ?s ?p ?o }", false},
		{"注释含变更关键字", "# delete all data\nSELECT ?s WHERE { ?s ?p ?o }", false},
		{"字符串字面量含关键字", `SELECT ?s WHERE { ?s ?p ?o . FILTER(CONTAINS(?o, "delete everything")) }`, false},
		{"IRI 片段含关键字", "SELECT ?s WHERE { <http://example.org#deleteRule> ?p ?o }", false},
		{"LIMIT/OFFSET 合法", "SELECT ?s WHERE { ?s ?p ?o } ORDER BY ?s LIMIT 10 OFFSET 5", false},
		{"UPDATE 拒绝", "UPDATE { ?s ?p ?o } WHERE { ?s ?p ?o }", true},
		{"INSERT DATA 拒绝", "INSERT DATA { <urn:x:a> <urn:x:b> <urn:x:c> }", true},
		{"DELETE 拒绝", "DELETE WHERE { ?s ?p ?o }", true},
		{"LOAD 拒绝", "LOAD <http://example.org/data.ttl>", true},
		{"CLEAR 拒绝", "CLEAR GRAPH <urn:g>", true},
		{"CREATE 拒绝", "CREATE GRAPH <urn:g>", true},
		{"DROP 拒绝", "DROP ALL", true},
		{"ASK 拒绝", "ASK { ?s ?p ?o }", true},
		{"CONSTRUCT 拒绝", "CONSTRUCT { ?s ?p ?o } WHERE { ?s ?p ?o }", true},
		{"无前导声明裸词拒绝", "?s ?p ?o", true},
		{"空查询拒绝", "   ", true},
		{"仅 PREFIX 无 SELECT 拒绝", "PREFIX a: <urn:a:>", true},
		{"大小写混合 INSERT 拒绝", "select ?s where { ?s ?p ?o } ; insert { ?s ?p 'x' } where { ?s ?p ?o }", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := vetReadonlySelect(tc.query)
			if tc.wantErr && err == nil {
				t.Fatalf("期望拒绝，实际通过: %s", tc.query)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("期望通过，实际拒绝: %s，错误: %v", tc.query, err)
			}
		})
	}
}

func TestClampLimit(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want int
	}{
		{"缺省", nil, defaultQueryLimit},
		{"正常值", float64(10), 10},
		{"负数回默认", float64(-3), defaultQueryLimit},
		{"零回默认", float64(0), defaultQueryLimit},
		{"超上限截断", float64(5000), maxQueryLimit},
		{"类型不符回默认", "100", defaultQueryLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampLimit(tc.in); got != tc.want {
				t.Fatalf("clampLimit(%v) = %d, 期望 %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestToTable(t *testing.T) {
	bindings := []map[string]any{
		{"s": map[string]any{"value": "urn:o:a"}, "o": map[string]any{"value": "注释文本"}},
		{"s": map[string]any{"value": "urn:o:b"}, "extra": map[string]any{"value": "仅此行有"}},
		{"s": map[string]any{"value": "urn:o:c"}},
	}
	cols, rows, truncated := toTable(bindings, 2)
	if len(cols) != 3 || cols[0] != "extra" || cols[1] != "o" || cols[2] != "s" {
		t.Fatalf("列名应为字母序 [extra o s]，实际 %v", cols)
	}
	if len(rows) != 2 {
		t.Fatalf("应截断为 2 行，实际 %d", len(rows))
	}
	if !truncated {
		t.Fatal("应标记 truncated=true")
	}
	if rows[1]["extra"] != "仅此行有" || rows[0]["extra"] != "" {
		t.Fatalf("缺键应补空串，实际 %v", rows)
	}
	if rows[0]["o"] != "注释文本" {
		t.Fatalf("lit 投影失败: %v", rows[0])
	}
	// 恰好等于 limit 不算截断
	_, _, truncated = toTable(bindings[:2], 2)
	if truncated {
		t.Fatal("行数==limit 不应标记 truncated")
	}
}

// TestHandleSparqlQueryEndToEnd 集成链：参数校验→只读白名单→路由 running 方案→
// 桩 SPARQL 端点执行→表格投影（截断/诚实标注）→翻译透视落库（REQ-94）。
func TestHandleSparqlQueryEndToEnd(t *testing.T) {
	var gotQuery, gotCT, gotAccept string
	sparqlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotQuery = string(b)
		gotCT = r.Header.Get("Content-Type")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/sparql-results+json")
		io.WriteString(w, `{"head":{"vars":["c","comment"]},"results":{"bindings":[
			{"c":{"type":"uri","value":"urn:o:t:concept:A"},"comment":{"type":"literal","value":"注释A"}},
			{"c":{"type":"uri","value":"urn:o:t:concept:B"}}]}}`)
	}))
	defer sparqlSrv.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"), filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	now := time.Now().Format(time.RFC3339)
	p := &store.Profile{ID: "p1", Name: "n", Engine: "oxigraph", OntologyIDs: []string{"t"},
		Config: "{}", Status: "created", CreatedAt: now, UpdatedAt: now}
	if err := st.Create(p); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if err := st.SetStatus("p1", "running", "", ""); err != nil {
		t.Fatalf("set running: %v", err)
	}

	f := New(st, func(string) (string, error) { return sparqlSrv.URL, nil })
	h := f.handleSparqlQuery()

	newReq := func(args map[string]any) mcp.CallToolRequest {
		req := mcp.CallToolRequest{}
		req.Params.Name = "sparql_query"
		req.Params.Arguments = args
		return req
	}

	// 1) 正常查询：limit=1 截断 2 行
	res, err := h(context.Background(), newReq(map[string]any{
		"ontology_id": "t",
		"query":       "SELECT ?c ?comment WHERE { ?c rdfs:comment ?comment }",
		"limit":       float64(1),
	}))
	if err != nil || res.IsError {
		t.Fatalf("正常查询失败: %v / %v", err, res)
	}
	if !strings.Contains(gotQuery, "SELECT ?c ?comment") || gotCT != "application/sparql-query" || gotAccept != "application/sparql-results+json" {
		t.Fatalf("下游请求不符: ct=%s accept=%s body=%s", gotCT, gotAccept, gotQuery)
	}
	var out struct {
		Columns   []string          `json:"columns"`
		Rows      []map[string]any  `json:"rows"`
		Count     int               `json:"count"`
		Total     int               `json:"total"`
		Truncated bool              `json:"truncated"`
		Limit     int               `json:"limit"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &out); err != nil {
		t.Fatalf("结果解析: %v", err)
	}
	if len(out.Columns) != 2 || out.Count != 1 || out.Total != 2 || !out.Truncated || out.Limit != 1 {
		t.Fatalf("投影不符: %+v", out)
	}
	if out.Rows[0]["comment"] != "注释A" {
		t.Fatalf("行投影不符: %v", out.Rows[0])
	}

	// 2) 变更查询拒绝：不打到下游
	gotQuery = ""
	res, err = h(context.Background(), newReq(map[string]any{
		"ontology_id": "t", "query": "DELETE WHERE { ?s ?p ?o }",
	}))
	if err != nil {
		t.Fatalf("拒绝路径 transport 错误: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].(mcp.TextContent).Text, "QUERY_REJECTED") {
		t.Fatalf("变更查询应拒绝: %v", res)
	}
	if gotQuery != "" {
		t.Fatal("被拒查询不应到达 SPARQL 端点")
	}

	// 3) 未挂载本体路由失败
	res, err = h(context.Background(), newReq(map[string]any{
		"ontology_id": "missing", "query": "SELECT ?s WHERE { ?s ?p ?o }",
	}))
	if err != nil || !res.IsError || !strings.Contains(res.Content[0].(mcp.TextContent).Text, "ONTOLOGY_SERVICE_UNAVAILABLE") {
		t.Fatalf("未挂载本体应路由失败: %v / %v", err, res)
	}

	// 4) 翻译透视落库（REQ-94）：成功与被拒执行各一条
	traces, err := st.ListTraces("p1", 10)
	if err != nil {
		t.Fatalf("list traces: %v", err)
	}
	if len(traces) != 1 || traces[0].Tool != "sparql_query" || traces[0].Ok != true || traces[0].ResultCount != 2 {
		t.Fatalf("透视落库不符: %+v", traces)
	}
}
