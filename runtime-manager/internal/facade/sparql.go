package facade

// REQ-151（D-O17，M20）：facade 第 5 工具 sparql_query——受控开放查询面。
// 只读边界：词法级校验（跳过字符串字面量/IRI/注释后）要求前导声明后首关键词
// 为 SELECT，且全文裸词不得命中 SPARQL 1.1 Update 变更关键字；单次查询超时
// 与结果行数上限防失控查询。翻译透视（REQ-94）随 exec 照常记录。

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	sparqlQueryTimeout = 10 * time.Second
	defaultQueryLimit  = 50
	maxQueryLimit      = 200
)

// sparqlUpdateKeywords SPARQL 1.1 Update + LOAD 等变更关键字（大小写不敏感）。
var sparqlUpdateKeywords = map[string]bool{
	"LOAD": true, "CLEAR": true, "CREATE": true, "DROP": true,
	"MOVE": true, "COPY": true, "ADD": true,
	"INSERT": true, "DELETE": true, "UPDATE": true,
}

func (f *Facade) addSparqlQueryTool(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("sparql_query",
		mcp.WithDescription("对本体执行自定义只读 SPARQL SELECT 查询（开放问题查询面，如「哪些概念没有注释」）：返回表格列与行。仅允许 SELECT，禁任何变更操作；默认返回 50 行，可用 limit 调整（上限 200）。"),
		requiredString("ontology_id", "本体仓库 id"),
		mcp.WithString("query", mcp.Required(), mcp.Description("SPARQL SELECT 查询全文（可带 PREFIX 声明）")),
		mcp.WithNumber("limit", mcp.Description("返回行数上限（可选，默认 50，最大 200）")),
	), f.handleSparqlQuery())
}

func (f *Facade) handleSparqlQuery() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, _ := req.Params.Arguments.(map[string]any)
		oid, _ := args["ontology_id"].(string)
		q, _ := args["query"].(string)
		if oid == "" || strings.TrimSpace(q) == "" {
			return mcp.NewToolResultErrorf("ontology_id 与 query 必填"), nil
		}
		if err := vetReadonlySelect(q); err != nil {
			return mcp.NewToolResultErrorf("QUERY_REJECTED: %v", err), nil
		}
		p, ep, errRes := f.route(oid)
		if errRes != nil {
			return errRes, nil
		}
		limit := clampLimit(args["limit"])
		ctx, cancel := context.WithTimeout(ctx, sparqlQueryTimeout)
		defer cancel()
		bindings, err := f.exec(ctx, "sparql_query", oid, p.ID, ep, q)
		if err != nil {
			return mcp.NewToolResultErrorf("查询失败: %v", err), nil
		}
		cols, rows, truncated := toTable(bindings, limit)
		return jsonResult(map[string]any{
			"columns": cols, "rows": rows, "count": len(rows),
			"total": len(bindings), "truncated": truncated, "limit": limit,
		}), nil
	}
}

// vetReadonlySelect 词法级只读校验：PREFIX/BASE 前导声明后首关键词必须是 SELECT，
// 全文裸词不得命中变更关键字（字符串字面量、IRI、注释中的同形词不误伤）。
func vetReadonlySelect(query string) error {
	tokens := sparqlTokens(query)
	if len(tokens) == 0 {
		return fmt.Errorf("空查询")
	}
	i := 0
	for i < len(tokens) {
		t := strings.ToUpper(tokens[i])
		if t == "PREFIX" {
			i += 2 // 前缀名 + 被词法跳过的 <IRI>
		} else if t == "BASE" {
			i++ // 被词法跳过的 <IRI>
		} else {
			break
		}
	}
	if i >= len(tokens) || !strings.EqualFold(tokens[i], "select") {
		head := tokens[min(i, len(tokens)-1)]
		return fmt.Errorf("仅允许 SELECT 查询，得到首关键词 %q（REQ-151 只读白名单）", head)
	}
	for _, t := range tokens {
		if sparqlUpdateKeywords[strings.ToUpper(t)] {
			return fmt.Errorf("含变更关键字 %q，已拒绝（REQ-151 只读白名单）", t)
		}
	}
	return nil
}

// sparqlTokens 词法扫描：收集裸词 token；字符串字面量（含 '''/""" 长串与反斜杠转义）、
// IRI <…>、注释 #… 的内容不产出 token。
func sparqlTokens(q string) []string {
	var toks []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	r := []rune(q)
	n := len(r)
	for i := 0; i < n; i++ {
		c := r[i]
		switch {
		case c == '#':
			flush()
			for i < n && r[i] != '\n' {
				i++
			}
		case c == '"' || c == '\'':
			flush()
			if i+2 < n && r[i+1] == c && r[i+2] == c {
				i += 3
				for i < n && !(r[i] == c && i+2 < n && r[i+1] == c && r[i+2] == c) {
					if r[i] == '\\' {
						i++
					}
					i++
				}
				i += 2
			} else {
				i++
				for i < n && r[i] != c {
					if r[i] == '\\' {
						i++
					}
					i++
				}
			}
		case c == '<':
			flush()
			for i < n && r[i] != '>' {
				i++
			}
		case unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_':
			cur.WriteRune(c)
		default:
			flush()
		}
	}
	flush()
	return toks
}

// clampLimit 行数上限：缺省/非正数回默认值，超 maxQueryLimit 截到上限。
func clampLimit(v any) int {
	n := defaultQueryLimit
	if f, ok := v.(float64); ok && f > 0 {
		n = int(f)
	}
	if n > maxQueryLimit {
		n = maxQueryLimit
	}
	return n
}

// toTable 把 SPARQL JSON bindings 投影为表格：列名字母序稳定，缺键补空串，
// 超出 limit 的行截断并置 truncated（诚实标注）。
func toTable(bindings []map[string]any, limit int) ([]string, []map[string]string, bool) {
	colSet := map[string]bool{}
	for _, b := range bindings {
		for k := range b {
			colSet[k] = true
		}
	}
	cols := make([]string, 0, len(colSet))
	for k := range colSet {
		cols = append(cols, k)
	}
	sort.Strings(cols)
	truncated := len(bindings) > limit
	if truncated {
		bindings = bindings[:limit]
	}
	rows := make([]map[string]string, 0, len(bindings))
	for _, b := range bindings {
		row := make(map[string]string, len(cols))
		for _, c := range cols {
			if v, ok := b[c]; ok {
				row[c] = lit(v)
			} else {
				row[c] = ""
			}
		}
		rows = append(rows, row)
	}
	return cols, rows, truncated
}
