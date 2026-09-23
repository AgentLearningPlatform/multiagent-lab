// Package ontobuild 实现「由知识库构建本体」（O13，D-O14/REQ-108，M15）——
// 构建栏第六路径的编排层：KB（M6/M14）× LLM 能力代理（REQ-98）× 自存 KG（D-O15/REQ-110）多方粘合。
package ontobuild

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---- O13（D-O14/REQ-108）：由知识库构建本体——构建栏第六路径编排层 ----
//
// 三种抽取策略（04 §3.7）：
//   A chunk-llm：KB chunk 池 → LLM 抽 spec_json（复用 chat.GenerateStructured，REQ-82/90 CQ 引导）
//   B kg-direct：自存 KG（D-O15：store kg_* 表直读）→ 薄映射 spec_json（不做抽取）
//   C hybrid：   B 初稿 + LLM 校验补全（definition / 缺失关系）
// 本层不建 KG（复用 M14 槽位 GraphragIngest，D-O15 起为自研抽取落自存表）、只做映射。

// Service 编排依赖（Store/Box 供 chat.GenerateStructured 使用；KB 供 chunk 池与 KG 回读）。
// 独立成包的原因：chat → kb 已有依赖，本层再引 kb 会成环（api 层聚合注入）。
type Service struct {
	Store *store.Store
	Box   *secrets.Box
	KB    *kb.Service
}

// NewService 构造。
func NewService(st *store.Store, box *secrets.Box, kbSvc *kb.Service) *Service {
	return &Service{Store: st, Box: box, KB: kbSvc}
}

const (
	// 策略 A/C 的 LLM 语料预算：防 token 失控（超出截断并在 warnings 提示）
	maxLLMChunks = 120
	maxLLMChars  = 48000
)

// SelectableKB KB 选择器条目（GET /api/kbs/selectable-for-ontology-build）。
type SelectableKB struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	Mode            string `json:"mode"`
	DocCount        int    `json:"doc_count"`
	ChunkCount      int    `json:"chunk_count"`
	KGEntities      int    `json:"kg_entities"`
	KGRelationships int    `json:"kg_relationships"`
	KGReady         bool   `json:"kg_ready"` // 至少一个文档 KG 抽取成功（策略 B/C 可用性预览）
	UpdatedAt       string `json:"updated_at,omitempty"`
}

// SelectableKBs 列出可用于本体构建的 KB（全量 KB + mode 徽标 + chunk/KG 规模预览）。
func (s *Service) SelectableKBs() ([]*SelectableKB, error) {
	kbs, err := s.Store.ListKnowledgeBases()
	if err != nil {
		return nil, err
	}
	out := make([]*SelectableKB, 0, len(kbs))
	for _, k := range kbs {
		item := &SelectableKB{
			ID:          k.ID,
			Name:        k.Name,
			Description: k.Description,
			Mode:        k.Mode,
			UpdatedAt:   k.UpdatedAt,
		}
		docs, err := s.Store.ListKnowledgeDocs(k.ID)
		if err == nil {
			item.DocCount = len(docs)
			for _, d := range docs {
				item.ChunkCount += d.ChunkCount
			}
		}
		// KG 规模自存表直读（D-O15：kg_entity/kg_relationship 为主平台一等数据，
		// 无需外部回读，永不降级）。
		if k.Mode == "graphrag" {
			if ents, rels, err := s.Store.KGCounts(k.ID); err == nil {
				item.KGEntities = ents
				item.KGRelationships = rels
				item.KGReady = ents > 0 || rels > 0
			}
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// SpecError 对齐前端 ValidationError 形状（{path, message}）。
type SpecError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// SpecReport 本地结构校验报告（入库前由构建平面 PUT spec 做权威校验，此处为生成时快照）。
type SpecReport struct {
	OK       bool        `json:"ok"`
	Errors   []SpecError `json:"errors"`
	Warnings []string    `json:"warnings,omitempty"`
}

// ---- spec_json 草稿类型（与构建平面 Spec 契约同形；cqs 单独携带不入 Spec） ----

type buildConcept struct {
	Name       string   `json:"name"`
	Label      string   `json:"label,omitempty"`
	Definition string   `json:"definition,omitempty"`
	Parents    []string `json:"parents,omitempty"`
}

type buildRelation struct {
	Name       string `json:"name"`
	Definition string `json:"definition,omitempty"`
	From       string `json:"from"`
	To         string `json:"to"`
}

type buildInstance struct {
	Name       string            `json:"name"`
	Concept    string            `json:"concept"`
	Attributes map[string]any    `json:"attributes,omitempty"`
	Relations  []buildInstRelation `json:"relations,omitempty"`
}

type buildInstRelation struct {
	Rel    string `json:"rel"`
	Target string `json:"target"`
}

type buildSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Concepts    []buildConcept  `json:"concepts"`
	Relations   []buildRelation `json:"relations"`
	Instances   []buildInstance `json:"instances"`

	cqs []string `json:"-"` // 模型随草稿输出的能力问题（REQ-90），不入 Spec 本体，单列在 BuildResult.CQs
}

// BuildResult build-from-kb 返回（spec_json 草稿 + 校验报告 + 过程元数据）。
type BuildResult struct {
	KBID       string      `json:"kb_id"`
	KBName     string      `json:"kb_name"`
	Strategy   string      `json:"strategy"` // chunk-llm | kg-direct | hybrid
	Method     string      `json:"method,omitempty"` // KG 来源抽取方式（策略 B/C）：llm | lightweight
	CQMode     string      `json:"cq_mode"`
	CQs        []string    `json:"cqs,omitempty"` // 生成的/采用的能力问题（REQ-90）
	Rounds     int         `json:"rounds"`        // LLM 调用轮数（1 = 首轮即成；2 = 含一次校验修复）
	ChunksUsed int         `json:"chunks_used"`
	Truncated  bool        `json:"truncated,omitempty"` // 语料超预算被截断
	Spec       *buildSpec  `json:"spec_json"`
	Report     *SpecReport `json:"validation_report"`
	Warnings   []string    `json:"warnings,omitempty"`
}

// BuildFromKB 主入口：按策略从 KB 生成 spec_json 草稿（校验报告随附，不入库——预览后由前端走构建平面）。
func (s *Service) BuildFromKB(ctx context.Context, kbID, strategy, cqMode string, customCQs []string, connID string) (*BuildResult, error) {
	k, err := s.Store.GetKnowledgeBase(kbID)
	if err != nil {
		return nil, err
	}
	if strategy == "" {
		strategy = "chunk-llm"
	}
	if cqMode == "" {
		cqMode = "auto"
	}
	res := &BuildResult{KBID: kbID, KBName: k.Name, Strategy: strategy, CQMode: cqMode, CQs: []string{}}

	chunks, err := s.Store.ListKnowledgeChunksByKB(kbID)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, &store.HTTPError{Status: 400, Msg: "该知识库暂无 chunk 语料，请先导入文档"}
	}
	corpus, truncated := buildCorpus(chunks, maxLLMChunks, maxLLMChars)
	res.ChunksUsed = len(chunks)
	res.Truncated = truncated

	switch strategy {
	case "chunk-llm": // 策略 A：chunk 池 → LLM
		spec, cqs, rounds, werr := s.llmExtract(ctx, k, corpus, cqMode, customCQs, connID, nil)
		if werr != "" {
			return nil, &store.HTTPError{Status: 502, Msg: werr}
		}
		res.Spec, res.CQs, res.Rounds, res.Warnings = spec, cqs, rounds, nil
	case "kg-direct", "hybrid": // 策略 B / C
		kg, err := s.KB.GraphragKG(ctx, kbID)
		if err != nil {
			return nil, &store.HTTPError{Status: 500, Msg: "KG 读取失败（自存表异常）: " + err.Error()}
		}
		if len(kg.Entities) == 0 {
			return nil, &store.HTTPError{Status: 400, Msg: "该知识库尚无 KG（graphrag 模式导入后自动抽取；也可先 POST /api/kg/{id}/rebuild 显式重建）"}
		}
		res.Method = kg.Method
		res.Spec = MapKGToSpec(kg, k.Name)
		res.Warnings = []string{fmt.Sprintf("KG 直转共映射 %d 实体 / %d 关系（method=%s）；保真度依赖抽取质量，建议在编辑器二次调优（04 §3.7）", len(kg.Entities), len(kg.Relationships), kg.Method)}
		if strategy == "hybrid" { // 策略 C：B 初稿 → LLM 校验补全
			draftJSON, _ := json.Marshal(res.Spec)
			refined, cqs, rounds, werr := s.llmExtract(ctx, k, corpus, cqMode, customCQs, connID, draftJSON)
			if werr != "" {
				res.Warnings = append(res.Warnings, "LLM 校验补全失败，保留 KG 初稿: "+werr)
				res.Rounds = 0
			} else {
				res.Spec, res.CQs, res.Rounds = refined, cqs, rounds
			}
		}
	default:
		return nil, &store.HTTPError{Status: 400, Msg: "strategy 须为 chunk-llm | kg-direct | hybrid"}
	}

	res.Report = ValidateBuildSpec(res.Spec)
	// 生成物非法且走过 LLM：回喂错误做一轮修复（与 REQ-82 校验循环语义一致，上限 1 轮防失控）
	if !res.Report.OK && (strategy == "chunk-llm" || (strategy == "hybrid" && res.Rounds > 0)) {
		errJSON, _ := json.Marshal(res.Report.Errors)
		specJSON, _ := json.Marshal(res.Spec)
		prompt := fmt.Sprintf("%s\n\n当前草稿存在以下校验错误，请修正后重新输出完整 spec_json：\n%s\n\n原草稿：\n%s",
			ontoExtractPromptHeader(k.Name, cqMode, customCQs, corpus, true), errJSON, specJSON)
		if spec, err := s.generateSpec(ctx, prompt, connID); err == nil {
			rep := ValidateBuildSpec(spec)
			if len(rep.Errors) < len(res.Report.Errors) { // 只在更好时采纳
				res.Spec, res.Report, res.Rounds = spec, rep, res.Rounds+1
			}
		}
	}
	return res, nil
}

// ---- 策略 A/C：LLM 抽取 ----

func ontoExtractPromptHeader(kbName, cqMode string, cqs []string, corpus string, repair bool) string {
	var b strings.Builder
	if repair {
		b.WriteString("你是知识工程师。请修正本体草稿并输出完整 spec_json。\n\n")
	} else {
		b.WriteString("你是知识工程师。下面是一个知识库（" + kbName + "）的 chunk 语料。请通读后抽取领域本体：\n")
		b.WriteString("1. 概念（concepts）：语料中的核心名词类别，给出 name（中文短语）、label（英文）、definition（一句话）；有明确层级时给 parents。\n")
		b.WriteString("2. 关系（relations）：概念间有意义的关联，from/to 必须引用已有概念 name。\n")
		b.WriteString("3. 实例（instances）：语料中明确出现的具体个体，concept 引用概念 name，attributes 用语料中的事实。\n")
	}
	switch cqMode {
	case "custom":
		b.WriteString("能力问题（CQ，用户指定——本体必须能回答这些问题，围绕它们取舍概念与关系）：\n")
		for i, q := range cqs {
			fmt.Fprintf(&b, "%d. %s\n", i+1, q)
		}
	case "skip":
		// 跳过 CQ 引导
	default: // auto
		b.WriteString("先在 cqs 字段归纳 3~5 个该本体应能回答的能力问题（REQ-90），再据此抽取。\n")
	}
	b.WriteString("\n只输出 JSON。语料：\n" + corpus)
	return b.String()
}

const ontoExtractSchema = `{"name":"本体名","description":"本体描述","cqs":["能力问题1","能力问题2"],"concepts":[{"name":"概念名","label":"EnglishLabel","definition":"定义","parents":["父概念"]}],"relations":[{"name":"关系名","from":"概念A","to":"概念B","definition":"关系说明"}],"instances":[{"name":"实例名","concept":"概念名","attributes":{"属性":"值"},"relations":[{"rel":"关系名","target":"实例名"}]}]}`

// llmExtract 调 LLM 抽取（hybrid 时 draftJSON 非空 = 校验补全模式）；返回 (spec, cqs, rounds, errMsg)。
func (s *Service) llmExtract(ctx context.Context, k *store.KnowledgeBase, corpus, cqMode string, cqs []string, connID string, draftJSON []byte) (*buildSpec, []string, int, string) {
	prompt := ontoExtractPromptHeader(k.Name, cqMode, cqs, corpus, false)
	if draftJSON != nil {
		prompt = fmt.Sprintf("你是知识工程师。以下是由 KG 直转的本体初稿与知识库 chunk 语料。请校验并补全初稿：\n"+
			"- 修正明显错误的层级/关系方向；\n- 为缺 definition 的概念与关系补一句话定义；\n"+
			"- 语料中明确的新概念/关系/实例可补充，不得凭空编造；\n- 保留初稿正确的部分，输出完整 spec_json。\n\n初稿：\n%s\n\n%s", draftJSON, prompt)
	}
	spec, err := s.generateSpec(ctx, prompt, connID)
	if err != nil {
		return nil, nil, 0, err.Error()
	}
	return spec, spec.cqs, 1, ""
}

// generateSpec 调模型并解析（复用 REQ-98 chat.GenerateStructured；cqs 从草稿旁路收集）。
func (s *Service) generateSpec(ctx context.Context, prompt, connID string) (*buildSpec, error) {
	res, err := chat.GenerateStructured(ctx, s.Store, s.Box, connID, prompt, ontoExtractSchema)
	if err != nil {
		return nil, fmt.Errorf("LLM 生成失败: %w", err)
	}
	var raw struct {
		buildSpec
		CQs []string `json:"cqs"`
	}
	if err := json.Unmarshal([]byte(res.DraftJSON), &raw); err != nil {
		return nil, fmt.Errorf("模型输出解析失败（非合法 spec_json）: %w", err)
	}
	if raw.Concepts == nil {
		raw.Concepts = []buildConcept{}
	}
	if raw.Relations == nil {
		raw.Relations = []buildRelation{}
	}
	if raw.Instances == nil {
		raw.Instances = []buildInstance{}
	}
	spec := &raw.buildSpec
	spec.cqs = raw.CQs
	return spec, nil
}

// buildCorpus chunk 池 → LLM 语料（doc 标记 + seq；超预算按 doc 粒度截断）。
func buildCorpus(chunks []*store.KnowledgeChunk, maxChunks, maxChars int) (string, bool) {
	var b strings.Builder
	used := 0
	truncated := false
	for _, c := range chunks {
		if used >= maxChunks || b.Len()+len(c.Content) > maxChars {
			truncated = true
			break
		}
		fmt.Fprintf(&b, "[doc:%s #%d] %s\n", c.DocID, c.Seq, strings.TrimSpace(c.Content))
		used++
	}
	return b.String(), truncated
}

// ---- 策略 B：KG → spec_json 薄映射（不做语义抽取；04 §3.7 <300 行约定） ----

// MapKGToSpec entity→Concept（同 label 优先合并）/ relation→Relation（IS_A→parents，HAS→具有，其余按类型直转）/
// 实体 desc（轻量抽取捕获的首个描述句）→ definition。导出给 kg-to-spec-json 端点与策略 B/C 共用。
func MapKGToSpec(kg *kb.KGData, kbName string) *buildSpec {
	spec := &buildSpec{
		Name:        kbName + " 本体",
		Description: fmt.Sprintf("由知识库「%s」的 GraphRAG KG 直转生成（O13 策略 B，method=%s）；%d 实体 / %d 关系映射，保真度依赖抽取质量。",
			kbName, kg.Method, len(kg.Entities), len(kg.Relationships)),
		Concepts: []buildConcept{}, Relations: []buildRelation{}, Instances: []buildInstance{},
	}
	seen := map[string]bool{}
	for _, e := range kg.Entities { // entity→Concept（同 label 优先：先到先得，防御重名）
		name := truncateRunes(strings.TrimSpace(e.Name), 80)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		spec.Concepts = append(spec.Concepts, buildConcept{
			Name: name, Definition: truncateRunes(strings.TrimSpace(e.Desc), 200), Label: englishLabel(name),
		})
	}
	relSeen := map[string]bool{}
	for _, r := range kg.Relationships {
		from, to := strings.TrimSpace(r.Source), strings.TrimSpace(r.Target)
		if from == "" || to == "" || !seen[from] || !seen[to] {
			continue
		}
		typ := strings.ToUpper(strings.TrimSpace(r.Type))
		switch typ {
		case "IS_A", "ISA", "SUBCLASSOF": // 层级 → parents
			for i := range spec.Concepts {
				if spec.Concepts[i].Name == from {
					spec.Concepts[i].Parents = appendUnique(spec.Concepts[i].Parents, to)
				}
			}
		case "", "HAS", "HAVE": // 「A 的 B」→ 具有关系（claim→Attribute 的概念层近似；实例属性留给编辑器调优）
			key := "具有|" + from + "|" + to
			if !relSeen[key] {
				relSeen[key] = true
				spec.Relations = append(spec.Relations, buildRelation{Name: "具有", From: from, To: to, Definition: "KG「的」关系直转（HAS）"})
			}
		default:
			key := typ + "|" + from + "|" + to
			if !relSeen[key] {
				relSeen[key] = true
				spec.Relations = append(spec.Relations, buildRelation{Name: typ, From: from, To: to})
			}
		}
	}
	return spec
}

// englishLabel 中文名 → 简易英文 label（KG 实体多为中文；无词表时留空由编辑器补，非空保证 spec 可读）。
func englishLabel(name string) string {
	var b strings.Builder
	lastAlpha := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			if !lastAlpha && b.Len() > 0 {
				b.WriteRune('_')
			}
			b.WriteRune(r)
			lastAlpha = true
		} else {
			lastAlpha = false
		}
	}
	return strings.Trim(b.String(), "_")
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// ---- 本地结构校验（入库时构建平面 PUT spec 做权威校验，此处为生成侧同口径快照） ----

// ValidateBuildSpec 本地结构校验（导出给 kg-to-spec-json 端点复用）。
func ValidateBuildSpec(spec *buildSpec) *SpecReport {
	rep := &SpecReport{OK: true, Errors: []SpecError{}}
	if spec == nil {
		rep.OK = false
		rep.Errors = append(rep.Errors, SpecError{Path: "spec", Message: "草稿为空"})
		return rep
	}
	fail := func(path, msg string) {
		rep.OK = false
		rep.Errors = append(rep.Errors, SpecError{Path: path, Message: msg})
	}
	if strings.TrimSpace(spec.Name) == "" {
		fail("name", "本体名必填")
	}
	if len(spec.Concepts) == 0 {
		fail("concepts", "至少需要一个概念")
	}
	cset := map[string]bool{}
	for i, c := range spec.Concepts {
		if strings.TrimSpace(c.Name) == "" {
			fail(fmt.Sprintf("concepts[%d].name", i), "概念名必填")
			continue
		}
		if cset[c.Name] {
			fail(fmt.Sprintf("concepts[%d].name", i), "概念名重复: "+c.Name)
		}
		cset[c.Name] = true
	}
	rset := map[string]bool{}
	for i, r := range spec.Relations {
		if strings.TrimSpace(r.Name) == "" {
			fail(fmt.Sprintf("relations[%d].name", i), "关系名必填")
		}
		if !cset[r.From] {
			fail(fmt.Sprintf("relations[%d].from", i), "from 引用不存在的概念: "+r.From)
		}
		if !cset[r.To] {
			fail(fmt.Sprintf("relations[%d].to", i), "to 引用不存在的概念: "+r.To)
		}
		rset[r.Name] = true
	}
	for i, c := range spec.Concepts {
		for _, p := range c.Parents {
			if !cset[p] {
				fail(fmt.Sprintf("concepts[%d].parents", i), "parents 引用不存在的概念: "+p)
			}
		}
	}
	for i, inst := range spec.Instances {
		if !cset[inst.Concept] {
			fail(fmt.Sprintf("instances[%d].concept", i), "concept 引用不存在的概念: "+inst.Concept)
		}
		for _, ir := range inst.Relations {
			if !rset[ir.Rel] {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("instances[%d] 关系 %q 未在 relations 声明（构建平面可能拒绝）", i, ir.Rel))
			}
		}
	}
	return rep
}

// ChunksToKG 显式重建某 KB 的自存 KG（POST /api/kg/{id}/rebuild；
// graphrag 模式导入时已自动做，此处供重建/强制刷新场景）。
func (s *Service) ChunksToKG(ctx context.Context, kbID string) (*store.GraphragInfo, int, error) {
	if _, err := s.Store.GetKnowledgeBase(kbID); err != nil {
		return nil, 0, err
	}
	chunks, err := s.Store.ListKnowledgeChunksByKB(kbID)
	if err != nil {
		return nil, 0, err
	}
	if len(chunks) == 0 {
		return nil, 0, &store.HTTPError{Status: 400, Msg: "该知识库暂无 chunk 语料，请先导入文档"}
	}
	info := s.KB.GraphragIngest(ctx, kbID, chunks)
	return info, len(chunks), nil
}

// truncateRunes 按 rune 截断（与 kb 包同名 helper 同语义）。
func truncateRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "…"
}
