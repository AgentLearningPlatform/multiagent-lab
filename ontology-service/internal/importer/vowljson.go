package importer

import (
	"encoding/json"
	"fmt"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"
)

// ---------------------------------------------------------------------------
// VOWL JSON 导出（2026-09-27 WebVOWL 对照视图修复，M21/VIZ-3 转换链重构）：
// 原链路 = 平台 TTL 导出 → 浏览器端 owl2vowl.js 转换 → webvowl 渲染。但 owl2vowl 为纯 Java
// 转换器、官方从未发布浏览器分发（原 tools/fetch-webvowl.sh 引用的 jsdelivr owl2vowl.js 是
// 无效链接，从未真正可用——本视图自 VIZ-3 交付起即只能诚实降级报错）。重构后：VOWL JSON 作为
// spec 资产的又一种导出形态（与 TTL 同列）在本服务内直接生成——零新依赖、零 subprocess、
// 零 sidecar 依赖（webvowl.js 仅承担渲染，经 prepare-vendor 自 npm 包分发）。
// 覆盖 spec 结构全集：概念（含多继承 → rdfs:subClassOf）/关系（owl:ObjectProperty）/
// 实例（以 individuals 挂到所属概念）；spec 无概念级数据属性（datatype property），metrics 如实计 0。
// 输出形态：WebVOWL 1.1.x 扁平 JSON（顶层 class/property 数组）。
// ---------------------------------------------------------------------------

// vowlLabel VOWL JSON 标签对象（"IRI-based" 为必选基准键，zh 为平台标签语言）。
func vowlLabel(name, label string) map[string]any {
	l := map[string]any{"IRI-based": name}
	if label != "" && label != name {
		l["zh"] = label
	}
	return l
}

// vowlEntity 图实体（类 / 数据类型），webvowl 按 id 关联属性端点。
// individuals 按解析器约定为对象数组（labels 标签对象），非纯名字串。
type vowlEntity struct {
	ID          string           `json:"id"`
	Type        string           `json:"type"`
	Label       map[string]any   `json:"label"`
	Individuals []vowlIndividual `json:"individuals,omitempty"`
	Description map[string]any   `json:"description,omitempty"`
}

// vowlIndividual 个体（实例）条目。
type vowlIndividual struct {
	Labels map[string]any `json:"labels"`
}

// vowlProperty 图属性（继承边 / 对象属性）。端点字段为 domain/range（VOWL JSON 规范，
// parser.js .domain(element.domain)/.range(element.range)），subClassOf 亦走 properties 数组。
type vowlProperty struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Domain      string         `json:"domain"`
	Range       string         `json:"range"`
	Label       map[string]any `json:"label,omitempty"`
	Description map[string]any `json:"description,omitempty"`
}

// vowlJSON WebVOWL 1.1.x 渲染输入（其 parser.parse 直读顶层 class/property 数组的扁平形态，
// 非 VOWL JSON v2 的 graph.entities 嵌套形态——实测 1.1.7 解析器只认前者，2026-09-27 真机验证）。
type vowlJSON struct {
	Identifier string         `json:"identifier"`
	Header     map[string]any `json:"header"`
	Metrics    map[string]int `json:"metrics"`
	Namespace  []any          `json:"namespace"`
	Layout     map[string]int `json:"layout"`
	Class      []vowlEntity   `json:"class"`
	Property   []vowlProperty `json:"property"`
}

// ExportVOWLJSON 把 spec 转换为 VOWL JSON 字节流（调用方经 HTTP 直出）。
func ExportVOWLJSON(spec *pkgspec.Spec) ([]byte, error) {
	if spec == nil {
		return nil, fmt.Errorf("spec 为空，无法生成 VOWL JSON")
	}
	out := vowlJSON{
		Identifier: spec.ID,
		Header: map[string]any{
			"title":       vowlLabel(spec.Name, spec.Name),
			"description": spec.Description,
			"languages":   []string{"zh"},
		},
		Metrics: map[string]int{
			"classCount":            len(spec.Concepts),
			"objectPropertyCount":   len(spec.Relations),
			"datatypePropertyCount": 0,
			"individualCount":       len(spec.Instances),
		},
		Namespace: []any{},
		Layout:    map[string]int{"mainViewWidth": 1200, "mainViewHeight": 800},
	}

	// 概念 → class 实体；实例以 individuals 挂到所属概念（webvowl 个体徽标数据源）
	individualsByConcept := map[string][]vowlIndividual{}
	for _, it := range spec.Instances {
		individualsByConcept[it.Concept] = append(individualsByConcept[it.Concept],
			vowlIndividual{Labels: vowlLabel(it.Name, it.Name)})
	}
	entities := make([]vowlEntity, 0, len(spec.Concepts)+1)
	for _, c := range spec.Concepts {
		e := vowlEntity{ID: c.Name, Type: "owl:Class", Label: vowlLabel(c.Name, c.Label)}
		e.Individuals = individualsByConcept[c.Name]
		if c.Definition != "" {
			e.Description = map[string]any{"zh": c.Definition}
		}
		entities = append(entities, e)
	}
	// 孤立实例（概念不存在——结构层已拦截，这里兜底不丢数据）：补一个外部类承载
	known := map[string]bool{}
	for _, c := range spec.Concepts {
		known[c.Name] = true
	}
	for _, it := range spec.Instances {
		if !known[it.Concept] {
			entities = append(entities, vowlEntity{
				ID: it.Concept, Type: "owl:Class", Label: vowlLabel(it.Concept, it.Concept),
				Individuals: []vowlIndividual{{Labels: vowlLabel(it.Name, it.Name)}},
			})
		}
	}

	// 父子关系 → rdfs:subClassOf（domain=子类，range=父类；多继承生成多条）
	props := make([]vowlProperty, 0, len(spec.Relations)+len(spec.Concepts))
	for _, c := range spec.Concepts {
		for _, p := range c.Parents {
			props = append(props, vowlProperty{
				ID: fmt.Sprintf("sub_%s_%s", c.Name, p), Type: "rdfs:subClassOf",
				Domain: c.Name, Range: p,
			})
		}
	}
	// 关系 → owl:ObjectProperty（domain=定义域概念，range=值域概念）
	for _, r := range spec.Relations {
		p := vowlProperty{
			ID: r.Name, Type: "owl:ObjectProperty",
			Domain: r.From, Range: r.To, Label: vowlLabel(r.Name, r.Label),
		}
		if r.Definition != "" {
			p.Description = map[string]any{"zh": r.Definition}
		}
		props = append(props, p)
	}

	out.Class = entities
	out.Property = props
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return b, nil
}
