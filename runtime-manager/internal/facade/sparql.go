package facade

// REQ-151（D-O17，M20）：facade 第 5 工具 sparql_query——受控开放查询面。
// 只读边界：词法级校验（跳过字符串字面量/IRI/注释后）要求前导声明后首关键词
// 为 SELECT，且全文裸词不得命中 SPARQL 1.1 Update 变更关键字；单次查询超时
// 与结果行数上限防失控查询。翻译透视（REQ-94）随 exec 照常记录。

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/store"
)

const (
	sparqlQueryTimeout = 10 * time.Second
	defaultQueryLimit  = 50
	maxQueryLimit      = 200
)

// companionGraphEndpoint 伴生图引擎查询端点基址（M28/P2a，REQ-170）：
// backend/internal/companion 自管的独立 Oxigraph 实例（固定 :9199，方案"Oxigraph 引擎 0 改动"）；
// runtimed 只读转发，引擎未起时给自助提示。环境变量 COMPANION_GRAPH_ENDPOINT 覆盖。
func companionGraphEndpoint() string {
	if v := os.Getenv("COMPANION_GRAPH_ENDPOINT"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://127.0.0.1:9199"
}

// sparqlUpdateKeywords SPARQL 1.1 Update + LOAD 等变更关键字（大小写不敏感）。
var sparqlUpdateKeywords = map[string]bool{
	"LOAD": true, "CLEAR": true, "CREATE": true, "DROP": true,
	"MOVE": true, "COPY": true, "ADD": true,
	"INSERT": true, "DELETE": true, "UPDATE": true,
}

func (f *Facade) addSparqlQueryTool(s *server.MCPServer) {
	s.AddTool(mcp.NewTool("sparql_query",
		mcp.WithDescription("对本体或伴生图执行自定义只读 SPARQL SELECT 查询（开放问题查询面，如「哪些概念没有注释」）：返回表格列与行。仅允许 SELECT，禁任何变更操作；默认返回 50 行，可用 limit 调整（上限 200）。graph=companion 时查询指定会话的伴生本体图（动态沉淀的对话知识，M28/REQ-170）——跨会话记忆查询入口。"),
		requiredString("ontology_id", "本体仓库 id（graph=companion 时可留空）"),
		mcp.WithString("query", mcp.Required(), mcp.Description("SPARQL SELECT 查询全文（可带 PREFIX 声明）")),
		mcp.WithNumber("limit", mcp.Description("返回行数上限（可选，默认 50，最大 200）")),
		mcp.WithString("graph", mcp.Description("查询目标（可选）：default=本体运行方案默认图（缺省）；companion=伴生本体图（动态薄本体，跨会话对话知识）")),
		mcp.WithString("conversation_id", mcp.Description("会话 id（graph=companion 时必填：伴生图按会话 named graph 隔离）")),
	), f.handleSparqlQuery())
}

func (f *Facade) handleSparqlQuery() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, _ := req.Params.Arguments.(map[string]any)
		oid, _ := args["ontology_id"].(string)
		q, _ := args["query"].(string)
		graph, _ := args["graph"].(string)
		convID, _ := args["conversation_id"].(string)
		if strings.TrimSpace(q) == "" {
			return mcp.NewToolResultErrorf("query 必填"), nil
		}
		if graph != "" && graph != "default" && graph != "companion" {
			return mcp.NewToolResultErrorf("graph 仅支持 default | companion，得到 %q", graph), nil
		}
		// M28/P2a：伴生图查询——转发到伴生引擎，会话 named graph 经协议参数 default-graph-uri
		// 限定（零查询改写，词法校验照旧）。本体路由不参与（伴生图不在运行方案实例里）。
		if graph == "companion" {
			return f.sparqlCompanion(ctx, oid, convID, q, args), nil
		}
		if oid == "" {
			return mcp.NewToolResultErrorf("ontology_id 必填（graph=default 时）"), nil
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

// sparqlCompanion 伴生图分支（M28/P2a）：SELECT 白名单照旧 → 转发伴生引擎
// /query?default-graph-uri=<conv-graph>（oxigraph SPARQL 协议标准参数，实测支持）。
// 语义账目：ontology_id（若有）仅作提示，路由不走方案实例；透视 ProfileID 记 companion:conv。
func (f *Facade) sparqlCompanion(ctx context.Context, oid, convID, q string, args map[string]any) *mcp.CallToolResult {
	if strings.TrimSpace(convID) == "" {
		return mcp.NewToolResultErrorf("graph=companion 时 conversation_id 必填（伴生图按会话隔离）")
	}
	if err := vetReadonlySelect(q); err != nil {
		return mcp.NewToolResultErrorf("QUERY_REJECTED: %v", err)
	}
	limit := clampLimit(args["limit"])
	ctx, cancel := context.WithTimeout(ctx, sparqlQueryTimeout)
	defer cancel()
	graphURI := fmt.Sprintf("http://eino-lab/graph/conv-%s", convID)
	endpoint := companionGraphEndpoint() + "/query?default-graph-uri=" + url.QueryEscape(graphURI)
	start := time.Now()
	bindings, err := f.query(ctx, endpoint, q)
	if f.TraceSparql {
		tr := &store.Trace{Tool: "sparql_query", ProfileID: "companion:" + convID, OntologyID: oid,
			Sparql: q, TookMS: time.Since(start).Milliseconds(), ResultCount: len(bindings), Ok: err == nil}
		if err != nil {
			tr.Error = err.Error()
		}
		_ = f.Store.SaveTrace(tr)
	}
	if err != nil {
		return mcp.NewToolResultErrorf("COMPANION_GRAPH_UNAVAILABLE: 伴生图查询失败（%v）——伴生引擎未启动（对话收尾抽取/确认入图时懒启动）或该会话尚无伴生图", err)
	}
	cols, rows, truncated := toTable(bindings, limit)
	return jsonResult(map[string]any{
		"columns": cols, "rows": rows, "count": len(rows),
		"total": len(bindings), "truncated": truncated, "limit": limit,
		"graph": graphURI,
	})
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
