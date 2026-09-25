package companion

import (
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// REQ-170/M28 薄本体 SPARQL 生成（纯函数，便于零依赖单测）。
// 数据面：种子 5 骨架类（bot: 前缀）+ 会话 named graph 隔离 + 失效化而非删除。
// 实体 URI 规则：http://eino-lab/e/{slug(label)}——同名 slug 归并为同一实体（薄版口径）。
// 关系边 = bot:Relation 实例节点（bot:subject/bot:object/bot:relName），
// 矛盾（同主体+同关系名+新目标）→ 旧边节点 bot:invalidAt 标记，保留可查历史。
// ---------------------------------------------------------------------------

const (
	// BotNS 薄本体词表命名空间（方案 §五）。
	BotNS = "http://eino-lab/ontology/thin/"
	// GraphNS 会话图命名空间：GRAPH <…/graph/conv-{id}>。
	GraphNS = "http://eino-lab/graph/"
	// EntityNS 实体命名空间。
	EntityNS = "http://eino-lab/e/"
	// MsgNS 消息溯源命名空间。
	MsgNS = "http://eino-lab/msg/"
)

// GraphURI 会话图 URI。
func GraphURI(convID string) string { return fmt.Sprintf("%sconv-%s", GraphNS, convID) }

// EntityURI 实体 URI（slug 归并）。
func EntityURI(label string) string { return EntityNS + Slug(label) }

// MessageURI 消息溯源 URI。
func MessageURI(msgID string) string { return MsgNS + msgID }

// Slug 实体标签 → URI 片段（安全字符保留，其余转下划线；空串落占位）。
func Slug(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r == '_' || r == '-' || r == '.' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			b.WriteRune(r)
		case r >= 0x4e00 && r <= 0x9fff: // CJK 统一表意文字：URI 保留（oxigraph/IRI 合法）
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		out = "unnamed"
	}
	return out
}

// turtleEscape Turtle 字面量转义（反斜杠/引号/换行/回车/制表）。
func turtleEscape(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`,
	)
	return r.Replace(s)
}

// xsdTime RFC3339 → xsd 时间字面量值。
func xsdTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// SeedSchema 种子 5 骨架类（方案 §五，一次性预置；幂等 INSERT）。
// 薄版 schema 冻结：类型发现 P3 前不开放（报告 R1 漂移风险的结构性免疫）。
func SeedSchema() string {
	return `PREFIX bot: <` + BotNS + `>
PREFIX owl: <http://www.w3.org/2002/07/owl#>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
INSERT DATA {
  bot:Concept a owl:Class ; rdfs:label "概念" .
  bot:Relation a owl:Class ; rdfs:label "关系陈述" .
  bot:Event a owl:Class ; rdfs:label "事件" .
  bot:Source a owl:Class ; rdfs:label "来源" .
  bot:Agent a owl:Class ; rdfs:label "参与智能体" .
}`
}

// nodeKind map 候选 kind → 种子类。
func nodeKind(kind string) string {
	switch kind {
	case "relation":
		return "bot:Relation"
	case "event":
		return "bot:Event"
	default:
		return "bot:Concept"
	}
}

// InsertNodeTriples 概念/事件入图（同名 slug 归并；自带溯源三件套）。
func InsertNodeTriples(convID, candID, kind, label, definition string, confidence float64, msgID string, at time.Time) string {
	g := GraphURI(convID)
	e := EntityURI(label)
	var b strings.Builder
	b.WriteString("PREFIX bot: <" + BotNS + ">\nPREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>\nPREFIX prov: <http://www.w3.org/ns/prov#>\n")
	fmt.Fprintf(&b, "INSERT DATA {\n  GRAPH <%s> {\n", g)
	fmt.Fprintf(&b, "    <%s> a %s ;\n      rdfs:label %q ;\n", e, nodeKind(kind), turtleEscape(label))
	if definition != "" {
		fmt.Fprintf(&b, "      bot:definition %q ;\n", turtleEscape(definition))
	}
	fmt.Fprintf(&b, "      bot:confidence %.2f ;\n      bot:extractedFrom <%s> ;\n      prov:generatedAtTime %q ;\n      prov:wasGeneratedBy <%sactivity-%s> .\n",
		confidence, MessageURI(msgID), xsdTime(at), EntityNS, candID)
	b.WriteString("  }\n}")
	return b.String()
}

// InsertRelationTriples 关系边入图：边 = bot:Relation 实例节点（subject/object/relName）。
// 冲突语义：同 subject + 同 relName + 不同 object 的旧边由调用方先发 InvalidateEdge。
func InsertRelationTriples(convID, candID, relName, sourceLabel, targetLabel, definition string, confidence float64, msgID string, at time.Time) string {
	g := GraphURI(convID)
	edge := EdgeURI(candID)
	var b strings.Builder
	b.WriteString("PREFIX bot: <" + BotNS + ">\nPREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>\nPREFIX prov: <http://www.w3.org/ns/prov#>\n")
	fmt.Fprintf(&b, "INSERT DATA {\n  GRAPH <%s> {\n", g)
	fmt.Fprintf(&b, "    <%s> a bot:Relation ;\n      rdfs:label %q ;\n      bot:subject <%s> ;\n      bot:object <%s> ;\n      bot:relName %q ;\n",
		edge, turtleEscape(relName), EntityURI(sourceLabel), EntityURI(targetLabel), turtleEscape(relName))
	if definition != "" {
		fmt.Fprintf(&b, "      bot:definition %q ;\n", turtleEscape(definition))
	}
	fmt.Fprintf(&b, "      bot:confidence %.2f ;\n      bot:extractedFrom <%s> ;\n      prov:generatedAtTime %q ;\n      prov:wasGeneratedBy <%sactivity-%s> .\n",
		confidence, MessageURI(msgID), xsdTime(at), EntityNS, candID)
	// 边两端实体不存在则薄建（label 锚定，同名归并）
	fmt.Fprintf(&b, "    <%s> a bot:Concept ; rdfs:label %q .\n", EntityURI(sourceLabel), turtleEscape(sourceLabel))
	fmt.Fprintf(&b, "    <%s> a bot:Concept ; rdfs:label %q .\n", EntityURI(targetLabel), turtleEscape(targetLabel))
	b.WriteString("  }\n}")
	return b.String()
}

// EdgeURI 关系边节点 URI。
func EdgeURI(candID string) string { return EntityNS + "edge-" + candID }

// FindActiveEdge 查同主体+同关系名且未失效的旧边（矛盾检测前置）。
func FindActiveEdge(convID, sourceLabel, relName string) string {
	return fmt.Sprintf(`PREFIX bot: <%s>
SELECT ?edge WHERE {
  GRAPH <%s> {
    ?edge a bot:Relation ; bot:subject <%s> ; bot:relName %q .
    FILTER NOT EXISTS { ?edge bot:invalidAt ?any }
  }
} LIMIT 1`, BotNS, GraphURI(convID), EntityURI(sourceLabel), turtleEscape(relName))
}

// InvalidateEdge 旧边失效化（bot:invalidAt 标记而非删除，保留可查历史）。
func InvalidateEdge(convID, edgeURI string, at time.Time) string {
	return fmt.Sprintf(`PREFIX bot: <%s>
INSERT DATA {
  GRAPH <%s> {
    <%s> bot:invalidAt %q .
  }
}`, BotNS, GraphURI(convID), edgeURI, xsdTime(at))
}

// SelectGraphTriples 会话图全量读取（确认后回显/冒烟核对用）。
func SelectGraphTriples(convID string) string {
	return fmt.Sprintf(`PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT ?s ?p ?o WHERE {
  GRAPH <%s> { ?s ?p ?o . FILTER(?p != rdfs:label) }
} ORDER BY ?s LIMIT 500`, GraphURI(convID))
}

// SelectLabels 会话图实体标签清单（状态回显用）。
func SelectLabels(convID string) string {
	return fmt.Sprintf(`PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT ?label WHERE {
  GRAPH <%s> { ?s rdfs:label ?label }
} ORDER BY ?label LIMIT 200`, GraphURI(convID))
}

// DropGraph 会话图整体摘除（低侵入三原则③；引擎数据目录随 reset 一并清理由调用方决定）。
func DropGraph(convID string) string {
	return fmt.Sprintf(`DROP SILENT GRAPH <%s>`, GraphURI(convID))
}
