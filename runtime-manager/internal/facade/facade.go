// Package facade 统一 MCP facade（方案 04 §4.4）：
// tools/list 固定返回 4 个 onto_* 工具（契约不变）；tools/call 按 ontology_id 路由到
// 所属 running 方案，将工具语义翻译为 SPARQL 在 Oxigraph 执行。Agent 侧无感运行时差异。
package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/store"
)

type Facade struct {
	Store    *store.Store
	Endpoint func(profileID string) (string, error) // Manager.ProcEndpoint
	HTTP     *http.Client
}

func New(st *store.Store, endpoint func(string) (string, error)) *Facade {
	return &Facade{Store: st, Endpoint: endpoint, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// Mount 挂载到 /mcp（Streamable HTTP，沿用 Q-14）。
func (f *Facade) Mount() *server.StreamableHTTPServer {
	s := server.NewMCPServer("ontology-facade", "1.0.0", server.WithToolCapabilities(false))
	f.addTools(s)
	return server.NewStreamableHTTPServer(s)
}

func (f *Facade) addTools(s *server.MCPServer) {
	oid := func(name, desc string) mcp.ToolOption {
		return mcp.WithString(name, mcp.Required(), mcp.Description(desc))
	}
	s.AddTool(mcp.NewTool("get_concept",
		mcp.WithDescription("查询本体中某个概念（类）的定义：标签、描述、父概念。"),
		oid("ontology_id", "本体仓库 id"),
		oid("name", "概念名（如 计算节点）"),
	), f.handleConcept())

	s.AddTool(mcp.NewTool("get_instance",
		mcp.WithDescription("查询本体中某个实例（个体）的详情：所属概念、属性、关系。"),
		oid("ontology_id", "本体仓库 id"),
		oid("name", "实例名"),
	), f.handleInstance())

	s.AddTool(mcp.NewTool("list_instances",
		mcp.WithDescription("列出本体中某概念下的全部实例名。"),
		oid("ontology_id", "本体仓库 id"),
		oid("concept", "概念名"),
	), f.handleListInstances())

	s.AddTool(mcp.NewTool("neighbors",
		mcp.WithDescription("查询某实例的关系邻居：返回 [(关系名, 目标实例名)]。"),
		oid("ontology_id", "本体仓库 id"),
		oid("name", "实例名"),
	), f.handleNeighbors())
}

// ---- SPARQL 执行与 URI 约定 ----

// query 对方案 SPARQL 端点执行 SELECT，返回 bindings（JSON 结果集）。
func (f *Facade) query(ctx context.Context, endpoint, sparql string) ([]map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(sparql))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SPARQL 执行失败: %s", resp.Status)
	}
	var res struct {
		Results struct {
			Bindings []map[string]any `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res.Results.Bindings, nil
}

// route 按 ontology_id 路由到 running 方案端点。
func (f *Facade) route(ontologyID string) (endpoint string, errResult *mcp.CallToolResult) {
	p, err := f.Store.RunningByOntology(ontologyID)
	if err != nil || p == nil {
		return "", mcp.NewToolResultErrorf("ONTOLOGY_SERVICE_UNAVAILABLE: 本体 %s 未挂载到任何运行中的方案（REQ-70 按方案隔离）", ontologyID)
	}
	ep, err := f.Endpoint(p.ID)
	if err != nil {
		return "", mcp.NewToolResultErrorf("ONTOLOGY_SERVICE_UNAVAILABLE: %v", err)
	}
	return ep, nil
}

func uriOf(ontologyID, kind, name string) string {
	switch kind {
	case "concept":
		return pkgspec.ConceptURI(ontologyID, name)
	case "relation":
		return pkgspec.RelationURI(ontologyID, name)
	case "instance":
		return pkgspec.InstanceURI(ontologyID, name)
	}
	return pkgspec.URIPrefix(ontologyID) + kind + ":" + pkgspec.Sanitize(name)
}

func lit(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Sprint(v)
	}
	if s, ok := m["value"].(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// nameFromURI 从 urn:o:{oid}:{kind}:{name} 反解 name。
func nameFromURI(u string) string {
	parts := strings.SplitN(u, ":", 5)
	if len(parts) == 5 {
		return parts[4]
	}
	return u
}

func jsonResult(v any) *mcp.CallToolResult {
	b, _ := json.Marshal(v)
	return mcp.NewToolResultText(string(b))
}

// ---- 4 工具 handler（oxigraph 翻译：get_concept/get_instance→SELECT、list_instances→rdf:type、neighbors→属性路径）----

func (f *Facade) handleConcept() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, _ := req.Params.Arguments.(map[string]any)
		oid, _ := args["ontology_id"].(string)
		name, _ := args["name"].(string)
		if oid == "" || name == "" {
			return mcp.NewToolResultErrorf("ontology_id 与 name 必填"), nil
		}
		ep, errRes := f.route(oid)
		if errRes != nil {
			return errRes, nil
		}
		u := uriOf(oid, "concept", name)
		q := fmt.Sprintf(`PREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX owl: <http://www.w3.org/2002/07/owl#>
SELECT ?label ?comment ?parent WHERE {
  <%s> rdf:type owl:Class ; rdfs:label ?label .
  OPTIONAL { <%s> rdfs:comment ?comment }
  OPTIONAL { <%s> rdfs:subClassOf ?parent }
}`, u, u, u)
		bindings, err := f.query(ctx, ep, q)
		if err != nil {
			return mcp.NewToolResultErrorf("查询失败: %v", err), nil
		}
		if len(bindings) == 0 {
			return mcp.NewToolResultErrorf("概念 %q 不存在于本体 %s", name, oid), nil
		}
		out := map[string]any{"name": name, "uri": u, "label": lit(bindings[0]["label"]), "parents": []string{}}
		if bindings[0]["comment"] != nil {
			out["definition"] = lit(bindings[0]["comment"])
		}
		seen := map[string]bool{}
		for _, b := range bindings {
			if b["parent"] != nil {
				pu := lit(b["parent"])
				if !seen[pu] {
					seen[pu] = true
					out["parents"] = append(out["parents"].([]string), nameFromURI(pu))
				}
			}
		}
		return jsonResult(out), nil
	}
}

func (f *Facade) handleInstance() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, _ := req.Params.Arguments.(map[string]any)
		oid, _ := args["ontology_id"].(string)
		name, _ := args["name"].(string)
		if oid == "" || name == "" {
			return mcp.NewToolResultErrorf("ontology_id 与 name 必填"), nil
		}
		ep, errRes := f.route(oid)
		if errRes != nil {
			return errRes, nil
		}
		u := uriOf(oid, "instance", name)
		q := fmt.Sprintf(`SELECT ?p ?o WHERE { <%s> ?p ?o }`, u)
		bindings, err := f.query(ctx, ep, q)
		if err != nil {
			return mcp.NewToolResultErrorf("查询失败: %v", err), nil
		}
		if len(bindings) == 0 {
			return mcp.NewToolResultErrorf("实例 %q 不存在于本体 %s", name, oid), nil
		}
		out := map[string]any{"name": name, "uri": u, "attributes": map[string]any{}, "relations": []any{}}
		pfx := "urn:o:" + pkgspec.Sanitize(oid) + ":"
		for _, b := range bindings {
			pu, ov := lit(b["p"]), b["o"]
			switch {
			case strings.HasSuffix(pu, "22-rdf-syntax-ns#type"):
				out["concept"] = nameFromURI(lit(ov))
			case strings.HasPrefix(pu, pfx+"attr:"):
				out["attributes"].(map[string]any)[nameFromURI(pu)] = lit(ov)
			case strings.HasPrefix(pu, pfx+"relation:"):
				out["relations"] = append(out["relations"].([]any), map[string]string{"rel": nameFromURI(pu), "target": nameFromURI(lit(ov))})
			}
		}
		return jsonResult(out), nil
	}
}

func (f *Facade) handleListInstances() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, _ := req.Params.Arguments.(map[string]any)
		oid, _ := args["ontology_id"].(string)
		concept, _ := args["concept"].(string)
		if oid == "" || concept == "" {
			return mcp.NewToolResultErrorf("ontology_id 与 concept 必填"), nil
		}
		ep, errRes := f.route(oid)
		if errRes != nil {
			return errRes, nil
		}
		cu := uriOf(oid, "concept", concept)
		q := fmt.Sprintf(`PREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#>
SELECT ?i WHERE { ?i rdf:type <%s> } ORDER BY ?i`, cu)
		bindings, err := f.query(ctx, ep, q)
		if err != nil {
			return mcp.NewToolResultErrorf("查询失败: %v", err), nil
		}
		names := []string{}
		for _, b := range bindings {
			if b["i"] != nil {
				names = append(names, nameFromURI(lit(b["i"])))
			}
		}
		return jsonResult(map[string]any{"concept": concept, "count": len(names), "instances": names}), nil
	}
}

func (f *Facade) handleNeighbors() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, _ := req.Params.Arguments.(map[string]any)
		oid, _ := args["ontology_id"].(string)
		name, _ := args["name"].(string)
		if oid == "" || name == "" {
			return mcp.NewToolResultErrorf("ontology_id 与 name 必填"), nil
		}
		ep, errRes := f.route(oid)
		if errRes != nil {
			return errRes, nil
		}
		u := uriOf(oid, "instance", name)
		q := fmt.Sprintf(`SELECT ?p ?o WHERE { <%s> ?p ?o . FILTER(isIRI(?o)) FILTER(STRSTARTS(STR(?p), "urn:o:")) }`, u)
		bindings, err := f.query(ctx, ep, q)
		if err != nil {
			return mcp.NewToolResultErrorf("查询失败: %v", err), nil
		}
		neighbors := []any{}
		for _, b := range bindings {
			pu := lit(b["p"])
			if !strings.Contains(pu, ":relation:") {
				continue
			}
			neighbors = append(neighbors, map[string]string{"rel": nameFromURI(pu), "target": nameFromURI(lit(b["o"]))})
		}
		return jsonResult(map[string]any{"name": name, "neighbors": neighbors}), nil
	}
}
