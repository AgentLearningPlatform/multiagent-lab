package api

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---- KG 自存读取 + 审计决策 + PROV-O 导出（D-O15/REQ-110）----
// 零外部进程：图谱/审计数据全部读 SQLite（010_kg_audit），
// PROV-O 导出为 Go 原生 Turtle 模板（不再依赖 worker rdflib，验收 22「零 Python venv 依赖」）。

// kgRead GET /api/kg/{kbID}?limit= → 全量 KG 子图 + 规模计数（消费页图谱/审计页数据源）。
func (s *Server) kgRead(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kbID")
	if _, err := s.Store.GetKnowledgeBase(kbID); err != nil {
		writeErr(w, err)
		return
	}
	entities, rels, err := s.Store.KGByKB(kbID)
	if err != nil {
		writeErr(w, err)
		return
	}
	claims, err := s.Store.KGClaimsForSubjects(kbID, claimSubjectFilter(entities), 200)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kb_id":         kbID,
		"entities":      entities,
		"relationships": rels,
		"claims":        claims,
		"counts":        map[string]int{"entities": len(entities), "relationships": len(rels), "claims": len(claims)},
	})
}

// claimSubjectFilter 取实体名作 claim 过滤面（limit 截断防爆量）。
func claimSubjectFilter(entities []*store.KGEntity) []string {
	if len(entities) > 40 {
		entities = entities[:40]
	}
	names := make([]string, 0, len(entities))
	for _, e := range entities {
		names = append(names, e.Name)
	}
	return names
}

// listDecisions GET /api/audit/decisions?subject_kind=&subject_id=&limit=
func (s *Server) listDecisions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.Store.ListDecisions(q.Get("subject_kind"), q.Get("subject_id"), atoiDefault(q.Get("limit"), 50))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// createDecision POST /api/audit/decisions 手工/系统补录决策留痕。
func (s *Server) createDecision(w http.ResponseWriter, r *http.Request) {
	var d store.OntoDecision
	if err := decodeJSON(r, &d); err != nil {
		writeErr(w, err)
		return
	}
	created, err := s.Store.InsertDecision(&d)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// decisionChain GET /api/audit/decisions/{id}/chain → 沿 derived_from 的溯源链（32 跳封顶）。
func (s *Server) decisionChain(w http.ResponseWriter, r *http.Request) {
	chain, err := s.Store.DecisionChain(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chain)
}

// provExport GET /api/audit/prov-export?kb_id= → PROV-O Turtle（Go 原生模板，D-O15）。
// 教学口径：决策 = prov:Activity，主体（KG/本体）= prov:Entity，derived_from = prov:wasDerivedFrom。
func (s *Server) provExport(w http.ResponseWriter, r *http.Request) {
	kbID := r.URL.Query().Get("kb_id")
	kind := r.URL.Query().Get("subject_kind")
	if kind == "" && kbID != "" {
		kind = "kg" // 带 kb_id 时默认导该库 KG 抽取决策（subject_id=kb_id）
	}
	decisions, err := s.Store.ListDecisions(kind, "", 200)
	if err != nil {
		writeErr(w, err)
		return
	}
	if kbID != "" { // kb 过滤在内存做（ListDecisions 的 subject_id 与 kb 维度不同名）
		filtered := decisions[:0]
		for _, d := range decisions {
			if d.SubjectID == kbID {
				filtered = append(filtered, d)
			}
		}
		decisions = filtered
	}
	ttl := renderProvTTL(decisions)
	w.Header().Set("Content-Type", "text/turtle; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=prov-audit-%s.ttl", time.Now().Format("20060102-150405")))
	_, _ = w.Write([]byte(ttl))
}

// renderProvTTL 决策列表 → PROV-O Turtle 文本（无 rdflib 依赖，模板直拼）。
func renderProvTTL(decisions []*store.OntoDecision) string {
	var b strings.Builder
	b.WriteString("# PROV-O 审计导出（D-O15/REQ-110，Go 原生模板）\n")
	b.WriteString(fmt.Sprintf("# generated: %s\n", time.Now().UTC().Format(time.RFC3339)))
	b.WriteString(`@prefix prov: <http://www.w3.org/ns/prov#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
@prefix onto: <https://eino-multiagent-lab.local/onto/> .

`)
	// 实体排序保证输出稳定（教学可 diff）
	sorted := append([]*store.OntoDecision(nil), decisions...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, d := range sorted {
		name := provLocalName(d.ID)
		b.WriteString(fmt.Sprintf("onto:activity-%s a prov:Activity ;\n", name))
		b.WriteString(fmt.Sprintf("  rdfs:label %s ;\n", ttlLiteral(d.Title)))
		if d.Rationale != "" {
			b.WriteString(fmt.Sprintf("  rdfs:comment %s ;\n", ttlLiteral(d.Rationale)))
		}
		b.WriteString(fmt.Sprintf("  prov:startedAtTime %s ;\n", ttlLiteral(d.CreatedAt)))
		if d.SubjectID != "" {
			b.WriteString(fmt.Sprintf("  prov:generated onto:subject-%s .\n", provLocalName(d.SubjectID)))
		} else {
			b.WriteString(".\n")
		}
		if d.SubjectID != "" {
			b.WriteString(fmt.Sprintf("onto:subject-%s a prov:Entity ; rdfs:label %s .\n",
				provLocalName(d.SubjectID), ttlLiteral(d.SubjectKind+":"+d.SubjectID)))
		}
		if d.DerivedFrom != "" {
			b.WriteString(fmt.Sprintf("onto:activity-%s prov:wasDerivedFrom onto:activity-%s .\n",
				name, provLocalName(d.DerivedFrom)))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ttlLiteral Turtle 字面量转义（双引号短字符串风格）。
func ttlLiteral(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", "", "\t", `\t`)
	return `"` + r.Replace(s) + `"`
}

// provLocalName ID → 合法 Turtle 本地名（仅保留字母数字下划线连字符，其余转 _；数字开头加前缀）。
func provLocalName(id string) string {
	var b strings.Builder
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
			b.WriteRune(c)
		default:
			b.WriteRune('_')
		}
	}
	name := b.String()
	if name == "" {
		name = "anon"
	}
	if c := name[0]; c >= '0' && c <= '9' {
		name = "n" + name
	}
	// URL query 转义兜底（非 ASCII 已被过滤，此处防极端）
	return strings.Split(url.PathEscape(name), "%")[0]
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return def
	}
	return n
}
