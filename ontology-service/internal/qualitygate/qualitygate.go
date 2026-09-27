// Package qualitygate REQ-171 P1（26 号方案 §9 P1 / §5 底座 A）：本体资产质量门禁。
//
// 设计约束（方案 §5 约束原则，低侵入三原则）：
//   - 旁路：独立检查点（独立 REST + 独立调用时机），不嵌入既有生成主链路内部——llmcreate 仅在
//     既有「校验步骤」处增加一个校验源；
//   - 默认关：strict（错误级阻断）由调用方开关控制，默认宽松（仅告警不阻断）；
//   - 可摘除：质量报告以 ontology_artifact(format='quality-report') 形态存储（复用 D-O2 多形态
//     资产，零新表），删除该形态 artifact 即整体摘除。
//
// 检查项数据驱动（方案 §8-7：首批 8~12 项，按误报率迭代）——DefaultConfig 为内置清单，
// Config 可按检查项覆盖 enabled/severity。
package qualitygate

import (
	"fmt"
	"strings"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
)

// Severity 检查项严重级：error=错误级（strict 模式阻断/进修复循环）；warning=告警；info=提示。
type Severity string

const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
	SevInfo    Severity = "info"
)

// Dimension 质量分维度（方案 §9 P1：完备性/一致性/可维护性三维）。
type Dimension string

const (
	DimCompleteness    Dimension = "completeness"    // 完备性
	DimConsistency     Dimension = "consistency"     // 一致性
	DimMaintainability Dimension = "maintainability" // 可维护性
)

// CheckCfg 单检查项配置（数据驱动：enabled/severity 可覆盖默认）。
type CheckCfg struct {
	Enabled  bool     `json:"enabled"`
	Severity Severity `json:"severity"`
}

// Config 检查项配置：key=检查项 ID；nil 值项用默认。 nil *Config 等价默认全开。
type Config map[string]CheckCfg

// Finding 一组同检查项命中（count 全量计数，samples 截样）。
type Finding struct {
	CheckID   string    `json:"check_id"`
	Title     string    `json:"title"`
	Dimension Dimension `json:"dimension"`
	Severity  Severity  `json:"severity"`
	Count     int       `json:"count"`
	Samples   []string  `json:"samples"` // 路径级样例（如 concepts[2]），最多 maxSamples
}

// Score 三维质量分（0~100，扣分制）+ 加权综合分。
type Score struct {
	Overall         float64 `json:"overall"`
	Completeness    float64 `json:"completeness"`
	Consistency     float64 `json:"consistency"`
	Maintainability float64 `json:"maintainability"`
}

// Report 质量报告（入库形态：ontology_artifact format='quality-report'）。
type Report struct {
	Pass         bool      `json:"pass"` // strict 模式下 = 无错误级命中；宽松模式恒 true（仅告警不阻断）
	Strict       bool      `json:"strict"`
	ErrorCount   int       `json:"error_count"`
	WarningCount int       `json:"warning_count"`
	InfoCount    int       `json:"info_count"`
	Findings     []Finding `json:"findings"`
	Score        Score     `json:"score"`
	Stats        Stats     `json:"stats"`
}

// Stats 被检本体规模（报告可读性）。
type Stats struct {
	Concepts  int `json:"concepts"`
	Relations int `json:"relations"`
	Instances int `json:"instances"`
}

const maxSamples = 20

// checkDef 内置检查项定义。
type checkDef struct {
	id        string
	title     string
	dimension Dimension
	sev       Severity
	run       func(sp *pkgspec.Spec, add func(path string))
}

// defaultChecks 首批 11 项（方案 §9 P1：未连接类/循环层级/缺失注释标签/命名不一致/域值域未声明等）。
func defaultChecks() []checkDef {
	return []checkDef{
		{id: "orphan_concept", title: "未连接概念（无父级/无关系/无实例引用）", dimension: DimCompleteness, sev: SevWarning, run: checkOrphanConcept},
		{id: "missing_definition", title: "概念/关系缺失定义注释", dimension: DimCompleteness, sev: SevWarning, run: checkMissingDefinition},
		{id: "missing_label", title: "概念缺失展示标签", dimension: DimCompleteness, sev: SevInfo, run: checkMissingLabel},
		{id: "naming_style", title: "命名风格不一致", dimension: DimMaintainability, sev: SevWarning, run: checkNamingStyle},
		{id: "relation_endpoint_unspecified", title: "关系定义域/值域未声明", dimension: DimConsistency, sev: SevWarning, run: checkRelationEndpoints},
		{id: "dangling_relation_endpoint", title: "关系端点引用不存在的概念", dimension: DimConsistency, sev: SevError, run: checkDanglingRelation},
		{id: "dangling_parent", title: "父概念引用不存在", dimension: DimConsistency, sev: SevError, run: checkDanglingParent},
		{id: "hierarchy_cycle", title: "概念层级循环", dimension: DimConsistency, sev: SevError, run: checkHierarchyCycle},
		{id: "instance_type_missing", title: "实例无类型或类型不存在", dimension: DimConsistency, sev: SevError, run: checkInstanceType},
		{id: "dangling_instance_rel", title: "实例关系断言悬空（关系或目标实例不存在）", dimension: DimConsistency, sev: SevError, run: checkDanglingInstanceRel},
		{id: "duplicate_name", title: "命名重复（概念/关系/实例各自域内）", dimension: DimConsistency, sev: SevError, run: checkDuplicateName},
	}
}

// DefaultConfig 内置默认配置（全开、默认严重级）。
func DefaultConfig() Config {
	cfg := Config{}
	for _, d := range defaultChecks() {
		cfg[d.id] = CheckCfg{Enabled: true, Severity: d.sev}
	}
	return cfg
}

// Check 执行质量检查。cfg 为 nil 时用默认配置；未登记的检查项 ID 忽略。
func Check(sp *pkgspec.Spec, cfg Config) *Report {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	rep := &Report{Strict: false, Findings: []Finding{}, Stats: Stats{
		Concepts: len(sp.Concepts), Relations: len(sp.Relations), Instances: len(sp.Instances),
	}}
	byID := map[string]checkDef{}
	for _, d := range defaultChecks() {
		byID[d.id] = d
	}
	for _, d := range defaultChecks() {
		cc, ok := cfg[d.id]
		if !ok || !cc.Enabled {
			continue
		}
		f := Finding{CheckID: d.id, Title: d.title, Dimension: d.dimension, Severity: cc.Severity, Samples: []string{}}
		d.run(sp, func(path string) {
			f.Count++
			if len(f.Samples) < maxSamples {
				f.Samples = append(f.Samples, path)
			}
		})
		if f.Count > 0 {
			rep.Findings = append(rep.Findings, f)
			switch f.Severity {
			case SevError:
				rep.ErrorCount += f.Count
			case SevWarning:
				rep.WarningCount += f.Count
			default:
				rep.InfoCount += f.Count
			}
		}
	}
	rep.Score = scoreOf(rep.Findings)
	rep.Pass = rep.ErrorCount == 0 // 宽松/严格统一以错误级命中判定 Pass；宽松模式调用方不阻断即可
	return rep
}

// ErrorMessages 错误级命中回喂文案（llmcreate 修复循环用；空=门禁通过）。
func (r *Report) ErrorMessages() []string {
	msgs := []string{}
	for _, f := range r.Findings {
		if f.Severity != SevError {
			continue
		}
		msgs = append(msgs, fmt.Sprintf("%s（%s）：%s，共 %d 处，如 %s",
			f.Title, f.CheckID, dimensionLabel(f.Dimension), f.Count, strings.Join(f.Samples, "、")))
	}
	return msgs
}

func dimensionLabel(d Dimension) string {
	switch d {
	case DimCompleteness:
		return "完备性"
	case DimConsistency:
		return "一致性"
	default:
		return "可维护性"
	}
}

// scoreOf 扣分制：error -10 / warning -3 / info -1（按 count，单维地板 0）；
// 综合 = 完备性×0.35 + 一致性×0.45 + 可维护性×0.2（一致性权重最高，呼应设计铁律的量化依据）。
func scoreOf(findings []Finding) Score {
	penalty := map[Dimension]float64{DimCompleteness: 0, DimConsistency: 0, DimMaintainability: 0}
	for _, f := range findings {
		w := 1.0
		switch f.Severity {
		case SevError:
			w = 10
		case SevWarning:
			w = 3
		}
		penalty[f.Dimension] += w * float64(f.Count)
	}
	dim := func(p float64) float64 {
		if p >= 100 {
			return 0
		}
		return 100 - p
	}
	s := Score{
		Completeness:    dim(penalty[DimCompleteness]),
		Consistency:     dim(penalty[DimConsistency]),
		Maintainability: dim(penalty[DimMaintainability]),
	}
	s.Overall = 0.35*s.Completeness + 0.45*s.Consistency + 0.2*s.Maintainability
	return s
}

// ---- 检查项实现（纯函数，路径级 add 回调） ----

func conceptIndex(sp *pkgspec.Spec) map[string]int {
	idx := map[string]int{}
	for i, c := range sp.Concepts {
		idx[c.Name] = i
	}
	return idx
}

func checkOrphanConcept(sp *pkgspec.Spec, add func(string)) {
	connected := map[string]bool{}
	for _, r := range sp.Relations {
		connected[r.From], connected[r.To] = true, true
	}
	hasInstance := map[string]bool{}
	for _, in := range sp.Instances {
		hasInstance[in.Concept] = true
	}
	for i, c := range sp.Concepts {
		if len(c.Parents) == 0 && !connected[c.Name] && !hasInstance[c.Name] {
			add(fmt.Sprintf("concepts[%d](%s)", i, c.Name))
		}
	}
}

func checkMissingDefinition(sp *pkgspec.Spec, add func(string)) {
	for i, c := range sp.Concepts {
		if strings.TrimSpace(c.Definition) == "" {
			add(fmt.Sprintf("concepts[%d](%s).definition", i, c.Name))
		}
	}
	for i, r := range sp.Relations {
		if strings.TrimSpace(r.Definition) == "" {
			add(fmt.Sprintf("relations[%d](%s).definition", i, r.Name))
		}
	}
}

func checkMissingLabel(sp *pkgspec.Spec, add func(string)) {
	for i, c := range sp.Concepts {
		if strings.TrimSpace(c.Label) == "" {
			add(fmt.Sprintf("concepts[%d](%s).label", i, c.Name))
		}
	}
}

// nameStyle 命名风格归类：latin_camel / latin_snake / cjk / other。
func nameStyle(s string) string {
	if s == "" {
		return "other"
	}
	hasCJK, hasUpper, hasLower, hasUnderscore := false, false, false, false
	for _, r := range s {
		switch {
		case r >= 0x4E00 && r <= 0x9FFF:
			hasCJK = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r == '_':
			hasUnderscore = true
		}
	}
	switch {
	case hasCJK:
		return "cjk"
	case hasUnderscore && hasLower:
		return "latin_snake"
	case hasUpper && hasLower:
		return "latin_camel"
	default:
		return "other"
	}
}

func checkNamingStyle(sp *pkgspec.Spec, add func(string)) {
	// 只比拉丁系命名风格（camel/snake）：CJK 名在本平台双语教学语境属合法形态，不参与
	// 风格主导判定（降误报，方案 §8-7 按误报率迭代）。
	counts := map[string]int{}
	styles := map[string]string{} // name -> style（概念为主）
	for _, c := range sp.Concepts {
		st := nameStyle(c.Name)
		if st == "other" || st == "cjk" {
			continue
		}
		counts[st]++
		styles[c.Name] = st
	}
	if len(counts) < 2 {
		return
	}
	// 主导风格之外的全部命中（少数派风格即「不一致」）
	dominant := ""
	max := 0
	for st, n := range counts {
		if n > max {
			dominant, max = st, n
		}
	}
	for _, c := range sp.Concepts {
		if st, ok := styles[c.Name]; ok && st != dominant {
			add(fmt.Sprintf("concepts[%d](%s) 风格 %s ≠ 主导 %s", indexOfConcept(sp, c.Name), c.Name, st, dominant))
		}
	}
}

func indexOfConcept(sp *pkgspec.Spec, name string) int {
	for i, c := range sp.Concepts {
		if c.Name == name {
			return i
		}
	}
	return -1
}

func checkRelationEndpoints(sp *pkgspec.Spec, add func(string)) {
	for i, r := range sp.Relations {
		if strings.TrimSpace(r.From) == "" || strings.TrimSpace(r.To) == "" {
			add(fmt.Sprintf("relations[%d](%s).from/to", i, r.Name))
		}
	}
}

func checkDanglingRelation(sp *pkgspec.Spec, add func(string)) {
	idx := conceptIndex(sp)
	for i, r := range sp.Relations {
		if _, ok := idx[r.From]; r.From != "" && !ok {
			add(fmt.Sprintf("relations[%d](%s).from=%s", i, r.Name, r.From))
		}
		if _, ok := idx[r.To]; r.To != "" && !ok {
			add(fmt.Sprintf("relations[%d](%s).to=%s", i, r.Name, r.To))
		}
	}
}

func checkDanglingParent(sp *pkgspec.Spec, add func(string)) {
	idx := conceptIndex(sp)
	for i, c := range sp.Concepts {
		for j, p := range c.Parents {
			if _, ok := idx[p]; !ok {
				add(fmt.Sprintf("concepts[%d](%s).parents[%d]=%s", i, c.Name, j, p))
			}
		}
	}
}

func checkHierarchyCycle(sp *pkgspec.Spec, add func(string)) {
	idx := conceptIndex(sp)
	color := map[string]int{} // 0=白 1=灰 2=黑
	var visit func(name string, path []string)
	visit = func(name string, path []string) {
		switch color[name] {
		case 1: // 回边：报告环路径
			start := 0
			for i, p := range path {
				if p == name {
					start = i
					break
				}
			}
			add("cycle: " + strings.Join(append(append([]string{}, path[start:]...), name), " -> "))
			return
		case 2:
			return
		}
		color[name] = 1
		path = append(path, name)
		if i, ok := idx[name]; ok {
			for _, p := range sp.Concepts[i].Parents {
				if _, exists := idx[p]; exists {
					visit(p, path)
				}
			}
		}
		color[name] = 2
	}
	for _, c := range sp.Concepts {
		visit(c.Name, nil)
	}
}

func checkInstanceType(sp *pkgspec.Spec, add func(string)) {
	idx := conceptIndex(sp)
	for i, in := range sp.Instances {
		if strings.TrimSpace(in.Concept) == "" {
			add(fmt.Sprintf("instances[%d](%s).concept 缺失", i, in.Name))
		} else if _, ok := idx[in.Concept]; !ok {
			add(fmt.Sprintf("instances[%d](%s).concept=%s 不存在", i, in.Name, in.Concept))
		}
	}
}

func checkDanglingInstanceRel(sp *pkgspec.Spec, add func(string)) {
	rels := map[string]bool{}
	for _, r := range sp.Relations {
		rels[r.Name] = true
	}
	insts := map[string]bool{}
	for _, in := range sp.Instances {
		insts[in.Name] = true
	}
	for i, in := range sp.Instances {
		for j, ir := range in.Relations {
			if !rels[ir.Rel] {
				add(fmt.Sprintf("instances[%d](%s).relations[%d].rel=%s 未定义", i, in.Name, j, ir.Rel))
			}
			if !insts[ir.Target] {
				add(fmt.Sprintf("instances[%d](%s).relations[%d].target=%s 不存在", i, in.Name, j, ir.Target))
			}
		}
	}
}

func checkDuplicateName(sp *pkgspec.Spec, add func(string)) {
	seen := map[string][]int{}
	for i, c := range sp.Concepts {
		seen[c.Name] = append(seen[c.Name], i)
	}
	for name, idxs := range seen {
		if len(idxs) > 1 {
			add(fmt.Sprintf("concepts %s 重复定义于 %v", name, idxs))
		}
	}
	seenR := map[string][]int{}
	for i, r := range sp.Relations {
		seenR[r.Name] = append(seenR[r.Name], i)
	}
	for name, idxs := range seenR {
		if len(idxs) > 1 {
			add(fmt.Sprintf("relations %s 重复定义于 %v", name, idxs))
		}
	}
	seenI := map[string][]int{}
	for i, in := range sp.Instances {
		seenI[in.Name] = append(seenI[in.Name], i)
	}
	for name, idxs := range seenI {
		if len(idxs) > 1 {
			add(fmt.Sprintf("instances %s 重复定义于 %v", name, idxs))
		}
	}
}
